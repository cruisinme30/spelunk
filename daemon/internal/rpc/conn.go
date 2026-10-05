package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// JSON-RPC 2.0 error codes, plus LSP's RequestCancelled. Application codes
// (QueryInvalid, RefStale, ...) are generated into package protocol.
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
)

// jsonrpcVersion is the "jsonrpc" field of every message sent.
const jsonrpcVersion = "2.0"

// readBufferSize is the reader's initial buffer: big enough for a typical batch.
const readBufferSize = 64 << 10

// maxHeaderSize bounds the header block of one message. Real headers are a
// few dozen bytes; the bound keeps a peer that never ends its header (or
// its header line) from filling memory.
const maxHeaderSize = 8 << 10

// maxBodySize bounds one message body. The buffer is allocated before the
// body arrives, so a Content-Length beyond it is refused unread rather than
// trusted. A variable so tests can shrink it.
var maxBodySize = 64 << 20

// ErrMessageTooLarge is returned by ReadMessage for a header block or a
// Content-Length over the limit. The stream cannot be resynchronised after
// it, so Serve stops.
var ErrMessageTooLarge = errors.New("rpc: message too large")

// cancelGrace bounds how long Call waits for the peer to answer a cancelled
// request before giving up on it. A variable so tests can shorten it.
var cancelGrace = 2 * time.Second

// Error is a JSON-RPC error object. Handlers return one to choose the code
// the caller sees; any other error becomes CodeInternalError.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Errorf builds an *Error with a formatted message.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

var errRequestCancelled = Errorf(CodeRequestCancelled, "request cancelled")

// Handler answers a request. Its context is cancelled by $/cancelRequest or
// when the connection closes.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// NotificationHandler handles a notification. Notifications run on the read
// loop in arrival order, so a handler must not block for long.
type NotificationHandler func(params json.RawMessage)

// Conn is one JSON-RPC connection. It is safe for concurrent use.
type Conn struct {
	// Trace, if non-nil, receives every message body sent ("send") or
	// received ("recv"). Set it before calling Serve.
	Trace func(direction string, body []byte)

	reader      *bufio.Reader
	writer      io.Writer
	writeMu     sync.Mutex            // one message at a time on the wire
	writeErr    atomic.Pointer[error] // the first failed write; set under writeMu
	writeBroken chan struct{}         // closed when writeErr is set, so Serve stops without new input

	mu                   sync.Mutex
	requestHandlers      map[string]Handler
	notificationHandlers map[string]NotificationHandler
	inflight             map[string]context.CancelFunc // incoming requests, by id
	pending              map[string]chan *message      // our outgoing calls, by id
	closed               bool

	nextID          atomic.Int64
	handlersRunning sync.WaitGroup
}

// message is any JSON-RPC message as read from the wire. ID is nil when
// the member is absent (a notification) and "null" when it is null (a
// request the peer could not number, which still gets a response).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// successResponse always carries "result", even when it is null.
type successResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type errorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *Error          `json:"error"`
}

// NewConn returns a connection that reads from r and writes to w.
func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{
		reader:               bufio.NewReaderSize(r, readBufferSize),
		writer:               w,
		requestHandlers:      map[string]Handler{},
		notificationHandlers: map[string]NotificationHandler{},
		inflight:             map[string]context.CancelFunc{},
		pending:              map[string]chan *message{},
		writeBroken:          make(chan struct{}),
	}
}

// Handle registers the handler for requests to method.
func (c *Conn) Handle(method string, h Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestHandlers[method] = h
}

// OnNotify registers the handler for notifications of method.
func (c *Conn) OnNotify(method string, h NotificationHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notificationHandlers[method] = h
}

// ReadMessage reads one Content-Length framed message body. It returns
// io.EOF only when the input ends cleanly between messages; input that ends
// inside a message is io.ErrUnexpectedEOF. A header block or body over the
// size limits is ErrMessageTooLarge, and is never buffered or allocated.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length, err := readContentLength(r)
	if err != nil {
		return nil, err
	}
	if length > maxBodySize {
		return nil, fmt.Errorf("%w: Content-Length %d is over %d", ErrMessageTooLarge, length, maxBodySize)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF // the header promised a body
		}
		return nil, err
	}
	return body, nil
}

// readContentLength reads a header block up to its blank line and returns
// its Content-Length. Input that ends inside the block is io.ErrUnexpectedEOF.
func readContentLength(r *bufio.Reader) (int, error) {
	length := -1
	headerBytes := 0
	for {
		line, err := readHeaderLine(r, &headerBytes)
		if err != nil {
			if errors.Is(err, io.EOF) && headerBytes > 0 {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return 0, fmt.Errorf("rpc: malformed header line %q", line)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue // Content-Type and anything else carry nothing we need
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || (length >= 0 && n != length) {
			return 0, fmt.Errorf("rpc: bad Content-Length %q", strings.TrimSpace(value))
		}
		length = n
	}
	if length < 0 {
		return 0, errors.New("rpc: header without Content-Length")
	}
	return length, nil
}

// readHeaderLine reads one header line without its line ending, adding its
// size to *total and failing once *total passes maxHeaderSize.
func readHeaderLine(r *bufio.Reader, total *int) (string, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		*total += len(chunk)
		if *total > maxHeaderSize {
			return "", fmt.Errorf("%w: header over %d bytes", ErrMessageTooLarge, maxHeaderSize)
		}
		line = append(line, chunk...)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			return "", err
		}
		return strings.TrimRight(string(line), "\r\n"), nil
	}
}

// WriteMessage writes body with a Content-Length header.
func WriteMessage(w io.Writer, body []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// send writes one message. After a write fails, the stream may hold part
// of a message, so every later send fails with the same error instead of
// writing after it, and Serve stops.
func (c *Conn) send(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.writeFailed(); err != nil {
		return err
	}
	if c.Trace != nil {
		c.Trace("send", body)
	}
	if err := WriteMessage(c.writer, body); err != nil {
		err = fmt.Errorf("rpc: write: %w", err)
		c.writeErr.Store(&err)
		close(c.writeBroken)
		return err
	}
	return nil
}

// writeFailed returns the error that broke the output stream, if any. It
// never waits for writeMu: the read loop calls it, and a writer holding
// writeMu may itself be waiting for the peer to read.
func (c *Conn) writeFailed() error {
	if err := c.writeErr.Load(); err != nil {
		return *err
	}
	return nil
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(&message{JSONRPC: jsonrpcVersion, Method: method, Params: encoded})
}

// Call sends a request and waits for the response, decoding the result into
// out (which may be nil). Cancelling ctx sends $/cancelRequest, then waits up
// to cancelGrace for the peer's answer, which is usually RequestCancelled.
func (c *Conn) Call(ctx context.Context, method string, params, out any) error {
	id := json.RawMessage(strconv.FormatInt(c.nextID.Add(1), 10))
	responses := make(chan *message, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return io.ErrClosedPipe
	}
	c.pending[string(id)] = responses
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, string(id))
		c.mu.Unlock()
	}()

	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := c.send(&message{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: encoded}); err != nil {
		return err
	}

	select {
	case response, ok := <-responses:
		return decodeResponse(response, ok, out)
	case <-ctx.Done():
	}
	_ = c.Notify("$/cancelRequest", map[string]json.RawMessage{"id": id})
	// Prefer the peer's own answer: it says whether the work really stopped.
	select {
	case response, ok := <-responses:
		if ok && response != nil && response.Error != nil {
			return response.Error
		}
	case <-time.After(cancelGrace):
	}
	return ctx.Err()
}

func decodeResponse(response *message, ok bool, out any) error {
	if !ok || response == nil {
		return io.ErrClosedPipe
	}
	if response.Error != nil {
		return response.Error
	}
	if out != nil && len(response.Result) > 0 {
		return json.Unmarshal(response.Result, out)
	}
	return nil
}

// Serve reads messages until the input ends, the input breaks its framing,
// the output fails, or ctx is cancelled. Requests run concurrently;
// notifications run in order on this goroutine. When Serve returns, every
// running handler has finished and pending Calls have failed.
func (c *Conn) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer c.shutdown(cancel)
	// Reads happen on their own goroutine so that a failed write or a
	// cancelled ctx stops Serve even while the peer sends nothing.
	reads := make(chan readResult)
	stopped := make(chan struct{})
	defer close(stopped)
	go c.readLoop(reads, stopped)
	for {
		var read readResult
		select {
		case read = <-reads:
		case <-c.writeBroken:
			return c.writeFailed()
		case <-ctx.Done():
			return ctx.Err()
		}
		if errors.Is(read.err, io.EOF) {
			return nil
		}
		if read.err != nil {
			return read.err
		}
		if c.Trace != nil {
			c.Trace("recv", read.body)
		}
		c.route(ctx, read.body)
		if err := c.writeFailed(); err != nil {
			return err
		}
	}
}

// readResult is one ReadMessage result, handed from readLoop to Serve.
type readResult struct {
	body []byte
	err  error
}

// readLoop reads messages for Serve until a read fails or Serve has
// stopped. A read already blocked when Serve stops ends when the input does.
func (c *Conn) readLoop(reads chan<- readResult, stopped <-chan struct{}) {
	for {
		body, err := ReadMessage(c.reader)
		select {
		case reads <- readResult{body, err}:
		case <-stopped:
			return
		}
		if err != nil {
			return
		}
	}
}

// nullID is the id of a response to a message whose own id is unknown.
var nullID = json.RawMessage("null")

// route decodes one message body and hands it to whatever handles it.
// Anything that is not a JSON-RPC message (invalid JSON, a batch array, a
// bare value, an id that is neither a string nor a number) is answered
// with an error carrying a null id, as JSON-RPC 2.0 requires.
func (c *Conn) route(ctx context.Context, body []byte) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		code := CodeInvalidRequest // valid JSON, but not a message object
		if !json.Valid(body) {
			code = CodeParseError
		}
		c.sendError(nullID, Errorf(code, "%v", err))
		return
	}
	if m.ID != nil && !validID(m.ID) {
		c.sendError(nullID, Errorf(CodeInvalidRequest, "id must be a string, a number or null, not %s", m.ID))
		return
	}
	switch {
	case m.Method == "" && m.ID != nil && (m.Result != nil || m.Error != nil):
		c.deliverResponse(&m)
	case m.Method == "":
		c.sendError(nullID, Errorf(CodeInvalidRequest, "message has no method and is not a response"))
	case m.ID == nil && m.Method == "$/cancelRequest":
		c.cancelInflight(m.Params)
	case m.ID == nil:
		c.handleNotification(m.Method, m.Params)
	default:
		c.dispatch(ctx, m)
	}
}

// validID reports whether id is a JSON string, number or null.
func validID(id json.RawMessage) bool {
	switch {
	case string(id) == "null":
		return true
	case id[0] == '"':
		return true
	case id[0] == '-' || id[0] >= '0' && id[0] <= '9':
		return true
	}
	return false
}

// sendError answers the request with id with err. A failed write is
// remembered by send and stops Serve, so the error is not returned.
func (c *Conn) sendError(id json.RawMessage, err *Error) {
	_ = c.send(&errorResponse{JSONRPC: jsonrpcVersion, ID: id, Error: err})
}

func (c *Conn) shutdown(cancel context.CancelFunc) {
	cancel()
	c.handlersRunning.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, responses := range c.pending {
		close(responses)
		delete(c.pending, id)
	}
}

// deliverResponse hands a response to the Call waiting for it.
func (c *Conn) deliverResponse(m *message) {
	c.mu.Lock()
	responses := c.pending[string(m.ID)]
	c.mu.Unlock()
	if responses == nil {
		return
	}
	select {
	case responses <- m:
	default: // a second response for the same id: the first one counts
	}
}

// cancelInflight cancels the context of the request named in params.
func (c *Conn) cancelInflight(params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.inflight[string(p.ID)]; cancel != nil {
		cancel()
	}
}

// handleNotification calls the handler registered for a notification, if
// any. A notification has no response to carry an error, so a panicking
// handler is recovered and the notification dropped: the read loop, and
// with it the connection, must survive.
func (c *Conn) handleNotification(method string, params json.RawMessage) {
	c.mu.Lock()
	handler := c.notificationHandlers[method]
	c.mu.Unlock()
	if handler == nil {
		return
	}
	defer func() { _ = recover() }()
	handler(params)
}

// dispatch runs a request's handler on its own goroutine and sends the
// answer. A request whose id is already in flight is refused: its cancel
// and its response could not be told apart from the first one's.
func (c *Conn) dispatch(connCtx context.Context, m message) {
	id := m.ID
	c.mu.Lock()
	if _, busy := c.inflight[string(id)]; busy {
		c.mu.Unlock()
		c.sendError(id, Errorf(CodeInvalidRequest, "request id %s is already in flight", id))
		return
	}
	requestCtx, cancel := context.WithCancel(connCtx)
	handler := c.requestHandlers[m.Method]
	c.inflight[string(id)] = cancel
	c.mu.Unlock()

	c.handlersRunning.Add(1)
	go func() {
		defer c.handlersRunning.Done()
		response := respond(connCtx, requestCtx, handler, m)
		// The id is free once the peer can see the answer, so it is
		// released before the answer is sent: a peer that reuses it at
		// once must not be told it is still in flight.
		cancel()
		c.mu.Lock()
		delete(c.inflight, string(id))
		c.mu.Unlock()
		_ = c.send(response)
	}()
}

// respond calls the handler for request m and returns the response to send.
func respond(connCtx, requestCtx context.Context, handler Handler, m message) any {
	if handler == nil {
		return &errorResponse{JSONRPC: jsonrpcVersion, ID: m.ID, Error: Errorf(CodeMethodNotFound, "method not found: %s", m.Method)}
	}
	result, err := callSafely(requestCtx, handler, m.Params)
	cancelledByPeer := requestCtx.Err() != nil && connCtx.Err() == nil
	if err == nil && cancelledByPeer {
		err = errRequestCancelled // a handler that ignores ctx still reports the cancel
	}
	if err != nil {
		return &errorResponse{JSONRPC: jsonrpcVersion, ID: m.ID, Error: toRPCError(err)}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return &errorResponse{JSONRPC: jsonrpcVersion, ID: m.ID, Error: Errorf(CodeInternalError, "encode result: %v", err)}
	}
	return &successResponse{JSONRPC: jsonrpcVersion, ID: m.ID, Result: encoded}
}

// toRPCError maps a handler error to the JSON-RPC error the caller sees.
func toRPCError(err error) *Error {
	var rpcErr *Error
	switch {
	case errors.As(err, &rpcErr):
		return rpcErr
	case errors.Is(err, context.Canceled):
		return errRequestCancelled
	default:
		return Errorf(CodeInternalError, "%v", err)
	}
}

// callSafely runs a handler, turning a panic into CodeInternalError.
func callSafely(ctx context.Context, handler Handler, params json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = Errorf(CodeInternalError, "panic: %v", r)
		}
	}()
	return handler(ctx, params)
}
