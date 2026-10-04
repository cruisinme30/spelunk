// Package rpc implements JSON-RPC 2.0 over a byte stream with LSP-style
// Content-Length framing (Contract 3). The same Conn serves both sides:
// the daemon registers handlers, tests and tools use Call and Notify.
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
)

// Standard and application error codes (Contract 3, "Error codes").
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
	CodeQueryInvalid     = 1001
	CodeIndexNotReady    = 1002
	CodeRefStale         = 1003
	CodeIndexCorrupt     = 1004
	CodeOverloaded       = 1005
)

// Error is a JSON-RPC error object. Handlers return it to control the code.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Errorf builds an *Error with a formatted message.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithData attaches a JSON payload to the error.
func (e *Error) WithData(v any) *Error {
	b, _ := json.Marshal(v)
	e.Data = b
	return e
}

type wireMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type wireErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *Error          `json:"error"`
}

// Handler answers a request. The context is cancelled by $/cancelRequest
// or when the connection closes.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// NotificationHandler handles a notification. It runs on the read loop,
// in arrival order, so it must not block for long.
type NotificationHandler func(params json.RawMessage)

// Tracer receives every message in both directions (UNIFIED_SEARCH_TRACE).
type Tracer func(direction string, raw []byte)

// Conn is one JSON-RPC connection.
type Conn struct {
	r   *bufio.Reader
	w   io.Writer
	wmu sync.Mutex

	mu       sync.Mutex
	handlers map[string]Handler
	notifs   map[string]NotificationHandler
	inflight map[string]context.CancelFunc
	pending  map[string]chan *wireMessage
	closed   bool

	nextID atomic.Int64
	wg     sync.WaitGroup
	Trace  Tracer
}

// NewConn wraps a reader and writer.
func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{
		r:        bufio.NewReaderSize(r, 64<<10),
		w:        w,
		handlers: map[string]Handler{},
		notifs:   map[string]NotificationHandler{},
		inflight: map[string]context.CancelFunc{},
		pending:  map[string]chan *wireMessage{},
	}
}

// Handle registers a request handler.
func (c *Conn) Handle(method string, h Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[method] = h
}

// OnNotify registers a notification handler.
func (c *Conn) OnNotify(method string, h NotificationHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifs[method] = h
}

// ReadMessage reads one framed message body.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	tp := textproto.NewReader(r)
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(hdr) == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Content-Length")))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("rpc: bad Content-Length %q", hdr.Get("Content-Length"))
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// WriteMessage writes one framed message body.
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
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.Trace != nil {
		c.Trace("send", body)
	}
	return WriteMessage(c.w, body)
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(&wireMessage{JSONRPC: "2.0", Method: method, Params: p})
}

// Call sends a request and waits for its response, decoding the result
// into out (which may be nil). Cancelling ctx sends $/cancelRequest.
func (c *Conn) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	ch := make(chan *wireMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return io.ErrClosedPipe
	}
	c.pending[string(rawID)] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, string(rawID))
		c.mu.Unlock()
	}()
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := c.send(&wireMessage{JSONRPC: "2.0", ID: &rawID, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return io.ErrClosedPipe
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-ctx.Done():
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		// Wait for the cancelled response so the id is not reused early.
		resp, ok := <-ch
		if ok && resp != nil && resp.Error != nil {
			return resp.Error
		}
		return ctx.Err()
	}
}

// Serve reads messages until EOF or ctx ends. Requests run concurrently;
// notifications run in order on this goroutine.
func (c *Conn) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.wg.Wait()
		c.mu.Lock()
		c.closed = true
		for k, ch := range c.pending {
			close(ch)
			delete(c.pending, k)
		}
		c.mu.Unlock()
	}()
	for {
		body, err := ReadMessage(c.r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if c.Trace != nil {
			c.Trace("recv", body)
		}
		var m wireMessage
		if err := json.Unmarshal(body, &m); err != nil {
			_ = c.send(&wireErrorResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &Error{Code: CodeParseError, Message: err.Error()}})
			continue
		}
		switch {
		case m.Method == "" && m.ID != nil: // response to our Call
			c.mu.Lock()
			ch := c.pending[string(*m.ID)]
			c.mu.Unlock()
			if ch != nil {
				ch <- &m
			}
		case m.Method != "" && m.ID == nil: // notification
			if m.Method == "$/cancelRequest" {
				var p struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(m.Params, &p) == nil {
					c.mu.Lock()
					if cf := c.inflight[string(p.ID)]; cf != nil {
						cf()
					}
					c.mu.Unlock()
				}
				continue
			}
			c.mu.Lock()
			h := c.notifs[m.Method]
			c.mu.Unlock()
			if h != nil {
				h(m.Params)
			}
		case m.Method != "": // request
			c.dispatch(ctx, m)
		}
	}
}

func (c *Conn) dispatch(ctx context.Context, m wireMessage) {
	id := *m.ID
	c.mu.Lock()
	h := c.handlers[m.Method]
	rctx, cancel := context.WithCancel(ctx)
	c.inflight[string(id)] = cancel
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.inflight, string(id))
			c.mu.Unlock()
		}()
		if h == nil {
			_ = c.send(&wireErrorResponse{JSONRPC: "2.0", ID: id,
				Error: Errorf(CodeMethodNotFound, "method not found: %s", m.Method)})
			return
		}
		res, err := safeCall(rctx, h, m.Params)
		if err == nil && rctx.Err() != nil && ctx.Err() == nil {
			err = Errorf(CodeRequestCancelled, "request cancelled")
		}
		if err != nil {
			var re *Error
			if !errors.As(err, &re) {
				if errors.Is(err, context.Canceled) {
					re = Errorf(CodeRequestCancelled, "request cancelled")
				} else {
					re = &Error{Code: CodeInternalError, Message: err.Error()}
				}
			}
			_ = c.send(&wireErrorResponse{JSONRPC: "2.0", ID: id, Error: re})
			return
		}
		b, mErr := json.Marshal(res)
		if mErr != nil {
			_ = c.send(&wireErrorResponse{JSONRPC: "2.0", ID: id,
				Error: &Error{Code: CodeInternalError, Message: mErr.Error()}})
			return
		}
		_ = c.send(&wireResponse{JSONRPC: "2.0", ID: id, Result: b})
	}()
}

func safeCall(ctx context.Context, h Handler, p json.RawMessage) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Code: CodeInternalError, Message: fmt.Sprintf("panic: %v", r)}
		}
	}()
	return h(ctx, p)
}
