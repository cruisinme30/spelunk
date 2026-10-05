// Package rpc implements JSON-RPC 2.0 over a byte stream, framed with
// Content-Length headers as in the Language Server Protocol. It carries the
// methods between the extension host and the daemon, and knows nothing
// about search.
//
// A single [Conn] serves both directions. The daemon registers handlers
// with [Conn.Handle] and [Conn.OnNotify] and calls [Conn.Serve]; tests and
// tools use [Conn.Call] and [Conn.Notify]. [ReadMessage] and
// [WriteMessage] do the framing on their own.
//
// Invariants:
//   - Requests run concurrently, each on its own goroutine with a context
//     that $/cancelRequest or the end of the connection cancels.
//     Notifications run one at a time, in arrival order, on the read loop.
//   - Every request gets exactly one response. A handler returns an [*Error]
//     to choose the code the caller sees; any other error, or a panic,
//     becomes CodeInternalError. A request the peer cancelled is answered
//     with CodeRequestCancelled even if its handler ignores the cancel.
//   - Framing errors end the connection, because the stream cannot be
//     resynchronised: a header without a valid Content-Length, input that
//     ends inside a message, or a header or body over the size limits. A
//     declared length is checked before anything is allocated for it.
//   - Messages are written whole, one at a time, so concurrent handlers
//     never interleave bytes on the wire.
package rpc
