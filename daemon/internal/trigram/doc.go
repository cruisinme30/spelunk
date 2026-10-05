// Package trigram is the working-tree index and search engine (see
// docs/adr/0003-in-house-trigram-index.md).
//
// ListFiles picks the files of a repo to index and Build reads them into a
// Shard: each file's text as a Doc, plus, for every trigram (three
// consecutive bytes, ASCII letters folded to lowercase), the files that
// contain it. Save and Load keep a shard on disk between sessions.
//
// Search runs a query.Plan over a list of Repo values, each a repo's
// published shard. For each text term it intersects the posting lists of
// the trigrams any match must contain to find candidate files, then
// confirms every candidate line with the term's RE2 regex. Each result
// carries a Ref, as an opaque string the client hands back unchanged;
// ParseRef decodes it so that Preview can show the lines around the
// result, and so the server can say where opening it goes.
//
// Invariants:
//   - A Shard is immutable once built; the indexer publishes a new one
//     rather than changing it, so searches need no locks.
//   - The trigram prefilter may let through files that don't match, but
//     never drops one that does: results equal a plain regex scan, line by
//     line, as ripgrep would report them, of each file's text as an editor
//     shows it (see decodeText: no byte order mark, UTF-16 converted, and a
//     lone "\r" ending a line).
//   - Only regular files are read, without following symlinks or blocking
//     on a FIFO, and never past the size limit, even when a file changed
//     after it was listed (see readText).
//   - Every offset in a result (hits, ref columns) is in UTF-16 code units.
package trigram
