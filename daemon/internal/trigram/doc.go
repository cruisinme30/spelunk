// Package trigram is the working-tree index and engine (see
// docs/adr/0003-in-house-trigram-index.md).
//
// A Shard holds one repo's text files and, for every trigram (three
// consecutive bytes, ASCII letters folded to lowercase), the files that
// contain it. A search turns each term into the trigrams any match must
// contain, intersects their posting lists to find candidate files, and
// confirms candidates with the term's RE2 regex. Shards are immutable once
// built; the indexer swaps in a new one when files change.
package trigram
