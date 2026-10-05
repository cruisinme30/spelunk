// Package server wires the daemon's JSON-RPC methods, as listed in
// protocol/protocol.schema.json, to its parser, planner, engines and indexer.
//
// It owns request lifecycle only: search IDs, cancellation and batching.
// It never reads or writes an index itself.
package server
