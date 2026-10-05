# Unified Search

One search box for file names, code and Git history, across every repo in your workspace.

> **Early development.** Searching current files, definitions and Git history works: file names, code lines, `sym:`
> definitions and commits, with the whole query language, previews and opening results.

## Getting started

1. Open a folder or workspace. The welcome page asks which key opens search and how much to index, and shows each
   repo's indexing. **Unified Search: Show Welcome** opens it again. The status bar shows indexing progress until
   the index is ready, then "Unified Search".
2. Press **⌘P** (Ctrl+P on Windows and Linux) and type. Quick Open moves to ⌥⌘P (Ctrl+Alt+P). To keep ⌘P for Quick
   Open, pick ⇧⌘F or your own key on the welcome page, or set `unifiedSearch.shortcut.preset`.
3. Click a result to preview it, press ↵ to open it at the match, ⌘↵ to open it to the side, and F4 or ⇧F4 to step
   through results without the panel.

Type an operator such as `since:`, `f:`, `repo:`, `lang:` or `author:` to see the values it takes: time windows with
how many files changed in each, the file types and folders in your workspace, your repos, their languages and their
authors.

Press `?` in an empty box for every operator, or run **Unified Search: Open Help** for the full guide with examples.

## The query language

Words are matched anywhere in file names and code. Combine them with operators:

| Write | To |
| --- | --- |
| `retry policy` or `retry AND policy` | Match both words in one file |
| `timeout OR time_out`, `( … )` | Match either; group terms |
| `-vendor`, `-f:vendor/` | Exclude a word or an operator |
| `"exact phrase"`, `/Retry(Policy\|Config)/` | Match a phrase or a regular expression |
| `case:yes` | Match case exactly (case-insensitive by default) |
| `f:*.go`, `f:_test\.py$` | Keep files whose path matches a glob or a regex |
| `repo:web`, `lang:python` | Keep one repo or one language |
| `type:file` | Show file names only |
| `sym:RetryPolicy` | Find where classes, functions and methods whose name contains RetryPolicy are defined |
| `author:jane timeout` | Search the commits Jane wrote: their added and removed lines |
| `msg:"fix flaky"`, `type:commit retry` | Search commit messages, or every commit's changes |
| `since:today`, `since:2h`, `since:2w` | Keep files (or commits) changed today, in the last two hours, or in the last two weeks |
| `count:50`, `count:all` | Show this many results per page |

A query with `author:`, `msg:` or `type:commit` searches Git history: each result is a commit, newest first, and ↵
opens its diff. For current files, `since:` uses each file's last commit, and files with uncommitted edits count as
changed now.

When a query has a mistake, the panel says what is wrong and offers a one-key fix (⌘.).

## Settings

Search for "Unified Search" in Settings. The most useful ones:

- `unifiedSearch.shortcut.preset`: which key opens the panel.
- `unifiedSearch.caseSensitive`: match case by default.
- `unifiedSearch.index.exclude`: glob patterns never indexed (by default `vendor`, `node_modules` and `*.min.js`).
  History keeps these files' line counts but not their lines.
- `unifiedSearch.index.historyDepth`: how much Git history to index: `6m`, `2y` (the default) or `all`.
- `unifiedSearch.index.symbols`: find definitions for `sym:` while indexing (on by default).
- `unifiedSearch.open.closeOnOpen`: hide the panel after opening a result (on by default). Opening to the side
  (⌘↵) always keeps it open.

Indexes stay on your machine. Nothing is sent anywhere.

## Troubleshooting

The extension and its search process log to the **Unified Search** output channel (View → Output). If search stops
responding, run **Unified Search: Restart Search**; **Unified Search: Rebuild Index** rebuilds the index from scratch.
