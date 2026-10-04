// Package indexer keeps one trigram shard per workspace root up to date
// and publishes it to the search engine.
//
// Builds run one at a time on a background worker. A search only ever
// sees a complete, published shard: a new build replaces the old shard in
// one step when it finishes. Shards are saved under index.location so the
// next window has results at once while a fresh build runs.
package indexer
