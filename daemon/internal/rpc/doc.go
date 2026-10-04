// Package rpc implements JSON-RPC 2.0 over a byte stream with LSP-style
// Content-Length framing, as in the Language Server Protocol. It carries
// the methods between the extension host and the daemon.
//
// A single [Conn] serves both directions. The daemon registers handlers
// with [Conn.Handle] and [Conn.OnNotify] and calls [Conn.Serve]; tests and
// tools use [Conn.Call] and [Conn.Notify]. Requests run concurrently and
// can be cancelled with $/cancelRequest; notifications run in order.
package rpc
