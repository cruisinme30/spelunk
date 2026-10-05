// Package indexer keeps the working-tree index and the history index of
// every workspace root up to date, and is the only writer to the index
// directory.
//
// Working-tree builds and history reads each run on their own worker. A
// search only ever sees a complete, published snapshot: a new build replaces
// the old one in one step. Saved files are re-read into a small overlay shard
// that masks their old copies until the next build folds them in, and HEAD
// is polled so new commits become searchable within seconds. Indexes are
// saved under index.location so the next window has results at once.
package indexer
