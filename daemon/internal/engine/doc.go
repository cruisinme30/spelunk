// Package engine is what the working-tree and history search engines
// share, so neither depends on the other: the page statistics a search
// returns, the time budget it runs under, and how a predicate tree narrows
// an index's candidate ids.
//
// Invariants:
//   - Id lists are ascending. nil means "any id" (the index can't narrow);
//     an empty, non-nil list means "no id".
//   - Narrowing may keep ids that don't match, but never drops one that
//     does: engines confirm every candidate.
package engine
