// Package server wires the daemon's JSON-RPC methods (initialize, query/parse,
// search/start, preview/get, open/resolve, index/*) to the daemon's
// parser, planner, engines and indexer.
//
// It owns request lifecycle only: search IDs, cancellation and batching.
// It never reads or writes an index itself.
package server
