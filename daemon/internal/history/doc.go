// Package history is the Git history index and the engine that searches it.
//
// Ingest reads a repo's first-parent history newest first with git log,
// keeping for each commit its author (after .mailmap), date, message and
// the lines each file added and removed. A Store holds the commits in
// segments, each with two trigram indexes: one over the changed lines and
// one over the message. Recent history becomes searchable first, because
// the newest segment is published before the rest is read. Update brings a
// Store up to date with new commits, or reads the history again when it was
// rewritten.
//
// Search runs a history Plan over the stores of the open repos and builds
// commit results, newest first. A commit matches when one of its changed
// files satisfies the whole predicate: path and language leaves test that
// file's path, text terms its added and removed lines, and author:, msg:,
// since: and repo: the commit itself. Preview and OpenTarget turn a commit
// result into its diff, read with git show.
//
// A Store also answers what the working-tree engine and the completions
// need from history: each file's newest commit and whether it has
// uncommitted edits (for since: on current files), and the authors and
// message words of the repo (for author: and msg: suggestions).
package history
