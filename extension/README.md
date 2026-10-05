# Spelunk

One search box for file names, code and Git history, across every repo in your workspace.

> Searching current files, definitions and Git history works: file names, code lines, `symbol:` definitions and
> commits, with the whole query language, previews and opening results.

## Getting started

1. Open a folder or workspace. The welcome page asks which key opens search and how much to index, and shows each
   repo's indexing. **Spelunk: Show Welcome** opens it again. The status bar shows indexing progress until
   the index is ready, then "Spelunk".
2. Press **⌘P** (Ctrl+P on Windows and Linux) and type. Quick Open moves to ⌥⌘P (Ctrl+Alt+P). To keep ⌘P for Quick
   Open, pick ⇧⌘F or your own key on the welcome page, or set `spelunk.shortcut.preset`.
3. Click a result to preview it, press ↵ to open it at the match, ⌘↵ to open it to the side, and F4 or ⇧F4 to step
   through results without the panel.

Type an operator such as `since:`, `file:`, `repo:`, `language:` or `author:` (or its short name: `d:`, `f:`, `r:`,
`l:`, `a:`) to see the values it takes: time windows with how many files changed in each, the file types and folders
in your workspace, your repos, their languages and their authors.

Press `?` in an empty box for every operator, or run **Spelunk: Open Help** for the full guide with examples.

## The query language

Words are matched anywhere in file names and code. Combine them with operators. Each operator has a full name and a
one-letter short name that mean the same: `file:` and `f:`, `author:` and `a:`.

| Write | To |
| --- | --- |
| `retry policy` or `retry AND policy` | Match both words in one file |
| `timeout OR time_out`, `( … )` | Match either; group terms |
| `-vendor`, `-file:vendor/` | Exclude a word or an operator |
| `"exact phrase"`, `/Retry(Policy\|Config)/` | Match a phrase or a regular expression |
| `case:yes`, `case:smart` | Match case exactly, or only when the query has a capital letter (case-insensitive by default) |
| `file:*.go`, `f:_test\.py$` | Keep files whose path matches a glob or a regex |
| `repo:web`, `language:python` | Keep one repo or one language |
| `type:file` | Show file names only |
| `symbol:RetryPolicy` | Find where classes, functions and methods whose name contains RetryPolicy are defined |
| `author:jane timeout` | Search the commits Jane wrote: their added and removed lines and their messages |
| `message:"fix flaky"`, `type:commit retry` | Search commit messages, or every commit's changes |
| `since:today`, `since:2h`, `since:2w` | Keep files (or commits) changed today, in the last two hours, or in the last two weeks |
| `count:50`, `count:all` | Show this many results per page |

A query with `author:`, `message:` or `type:commit` searches Git history: each result is a commit, newest first, and ↵
opens its diff. For current files, `since:` uses each file's last commit, and files with uncommitted edits count as
changed now.

When a query has a mistake, the panel says what is wrong and offers a one-key fix (⌘.), and the last good results
stay on screen. Likely slips get a warning: `daemon|search` matches the pipe itself, so the panel offers
`daemon OR search` or `/daemon|search/`.

In an empty box, the recent queries are on the left: remove one with its ×, or select it and press ⇧⌫.

## Settings

Search for "Spelunk" in Settings. The most useful ones:

- `spelunk.shortcut.preset`: which key opens the panel.
- `spelunk.caseSensitive`: `off`, `on`, or `smart` to match case only when the query has a capital letter.
- `spelunk.index.exclude`: glob patterns never indexed (by default `vendor`, `node_modules` and `*.min.js`).
  History keeps these files' line counts but not their lines.
- `spelunk.index.historyDepth`: how much Git history to index: `6m`, `2y` (the default) or `all`.
- `spelunk.index.symbols`: find definitions for `symbol:` while indexing (on by default).
- `spelunk.open.closeOnOpen`: hide the panel after opening a result (on by default). Opening to the side
  (⌘↵) always keeps it open.

Indexes stay on your machine. Nothing is sent anywhere.

## Troubleshooting

The extension and its search process log to the **Spelunk** output channel (View → Output). If search stops
responding, run **Spelunk: Restart Search**; **Spelunk: Rebuild Index** rebuilds the index from scratch.

## More

The [user guide](https://github.com/cruisinme30/spelunk/wiki/Getting-Started) covers every operator, key and
setting, and [how searches behave](https://github.com/cruisinme30/spelunk/wiki/How-Searches-Behave) explains
errors, empty results, indexing, folders without Git and crashes.
