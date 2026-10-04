# 0004. Index Git history in-house instead of in SQLite FTS5

- Status: Accepted
- Date: 2026-10-04

## Context

The plan proposed SQLite FTS5 with its trigram tokenizer for the commit index behind `author:`, `msg:` and
`type:commit`. The Go standard library has no SQLite driver, and the build can't fetch modules (see
[0003](0003-in-house-trigram-index.md)); the system SQLite could only be reached through cgo or a second process. The
working-tree index already has trigram posting lists, narrowing by AND / OR, and line matching that the ripgrep
differential tests guard.

## Options considered

1. **SQLite FTS5 through cgo or the `sqlite3` command.** Cgo breaks the static per-platform daemon build; a second
   process costs a round trip per query and a text protocol to parse.
2. **Reuse the trigram package for commits.** Read `git log -p -U0` newest first, keep each commit's changed lines and
   message, and index them with the same posting lists, in segments that are published as they fill.
3. **Ask Git on every query** (`git log -G`, `--author`, `--grep`). No index, but seconds per query on a large repo,
   and `git log -G` is the history engine's test oracle.

## Decision

Option 2, in `daemon/internal/history`. A segment of 200 commits is searchable within a second of starting to read;
later segments hold 2,000. Matching a commit's changed lines uses the working-tree engine's line matcher
(`trigram.LineFinder`), so a term means the same in both engines.

## Consequences

- One trigram implementation, one regex dialect and one set of case rules for current files and history.
- The index lives in memory and is saved with `gob`, like working-tree shards. On Prometheus (8,532 first-parent
  commits, all history) it takes 188 MB, reads in about 18 s, and answers in 5 to 65 ms.
- Files that `index.exclude` leaves out keep their line counts in history but not their lines, which bounds the size
  of repos that once vendored their dependencies.
- FTS5 ranking and phrase queries aren't needed: results are newest first, and phrases are regexes.
