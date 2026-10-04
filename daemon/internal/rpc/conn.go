package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
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

// readBufferSize is the reader's initial buffer: big enough for a typical batch.
const readBufferSize = 64 << 10

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

// WithData attaches v, as JSON, to the error and returns it. If v can't be
// marshalled the error is returned without data.
func (e *Error) WithData(v any) *Error {
	if data, err := json.Marshal(v); err == nil {
		e.Data = data
	}
	return e
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

	reader  *bufio.Reader
	writer  io.Writer
	writeMu sync.Mutex // one message at a time on the wire

	mu                   sync.Mutex
	requestHandlers      map[string]Handler
	notificationHandlers map[string]NotificationHandler
	inflight             map[string]context.CancelFunc // incoming requests, by id
	pending              map[string]chan *message      // our outgoing calls, by id
	closed               bool

	nextID          atomic.Int64
	handlersRunning sync.WaitGroup
}

// message is any JSON-RPC message as read from the wire.
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
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

// ReadMessage reads one Content-Length framed message body.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	header, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(header) == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	length, err := strconv.Atoi(strings.TrimSpace(header.Get("Content-Length")))
	if err != nil || length < 0 {
		return nil, fmt.Errorf("rpc: bad Content-Length %q", header.Get("Content-Length"))
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// WriteMessage writes body with a Content-Length header.
func WriteMessage(w io.Writer, body []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

func (c *Conn) send(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.Trace != nil {
		c.Trace("send", body)
	}
	return WriteMessage(c.writer, body)
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(&message{JSONRPC: "2.0", Method: method, Params: encoded})
}

// Call sends a request and waits for the response, decoding the result into
// out (which may be nil). Cancelling ctx sends $/cancelRequest, then waits up
// to cancelGrace for the peer's answer, which is usually RequestCancelled.
func (c *Conn) Call(ctx context.Context, method string, params any, out any) error {
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
	if err := c.send(&message{JSONRPC: "2.0", ID: &id, Method: method, Params: encoded}); err != nil {
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

// Serve reads messages until the input ends or ctx is cancelled. Requests
// run concurrently; notifications run in order on this goroutine. When Serve
// returns, every running handler has finished and pending Calls have failed.
func (c *Conn) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer c.shutdown(cancel)
	for {
		body, err := ReadMessage(c.reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.Trace != nil {
			c.Trace("recv", body)
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			_ = c.send(&errorResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: Errorf(CodeParseError, "%v", err)})
			continue
		}
		switch {
		case m.Method == "" && m.ID != nil:
			c.deliverResponse(&m)
		case m.Method == "$/cancelRequest":
			c.cancelInflight(m.Params)
		case m.Method != "" && m.ID == nil:
			c.notify(m.Method, m.Params)
		case m.Method != "":
			c.dispatch(ctx, m)
		}
	}
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
	responses := c.pending[string(*m.ID)]
	c.mu.Unlock()
	if responses != nil {
		responses <- m
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

func (c *Conn) notify(method string, params json.RawMessage) {
	c.mu.Lock()
	handler := c.notificationHandlers[method]
	c.mu.Unlock()
	if handler != nil {
		handler(params)
	}
}

// dispatch runs a request's handler on its own goroutine and sends the answer.
func (c *Conn) dispatch(connCtx context.Context, m message) {
	id := *m.ID
	requestCtx, cancel := context.WithCancel(connCtx)
	c.mu.Lock()
	handler := c.requestHandlers[m.Method]
	c.inflight[string(id)] = cancel
	c.mu.Unlock()

	c.handlersRunning.Add(1)
	go func() {
		defer c.handlersRunning.Done()
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.inflight, string(id))
			c.mu.Unlock()
		}()
		if handler == nil {
			_ = c.send(&errorResponse{JSONRPC: "2.0", ID: id, Error: Errorf(CodeMethodNotFound, "method not found: %s", m.Method)})
			return
		}
		result, err := callSafely(requestCtx, handler, m.Params)
		cancelledByPeer := requestCtx.Err() != nil && connCtx.Err() == nil
		if err == nil && cancelledByPeer {
			err = errRequestCancelled // a handler that ignores ctx still reports the cancel
		}
		if err != nil {
			_ = c.send(&errorResponse{JSONRPC: "2.0", ID: id, Error: toRPCError(err)})
			return
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			_ = c.send(&errorResponse{JSONRPC: "2.0", ID: id, Error: Errorf(CodeInternalError, "encode result: %v", err)})
			return
		}
		_ = c.send(&successResponse{JSONRPC: "2.0", ID: id, Result: encoded})
	}()
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
