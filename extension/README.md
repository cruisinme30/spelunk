# Unified Search

One search box for file names, code and Git history, across every repo in your workspace.

> **Early development.** Searching current files works: file names and code lines, with the whole query language,
> previews and opening results. History search (`author:`, `msg:`, `type:commit`) and symbol search (`sym:`) are
> understood but return no results yet.

## Getting started

1. Open a folder or workspace. The status bar shows indexing progress until the index is ready, then "Unified
   Search".
2. Press **⌘P** (Ctrl+P on Windows and Linux) and type. Quick Open moves to ⌥⌘P (Ctrl+Alt+P). To keep ⌘P for Quick
   Open, set `unifiedSearch.shortcut.preset` to `findInFiles` (⇧⌘F) or `none`.
3. Click a result to preview it, press ↵ to open it at the match, ⌘↵ to open it to the side, and F4 or ⇧F4 to step
   through results without the panel.

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
| `f:_test\.py$` | Keep files whose path matches a regex |
| `repo:web`, `lang:python` | Keep one repo or one language |
| `type:file` | Show file names only |
| `since:2w` | Keep files changed in the last two weeks |
| `count:50`, `count:all` | Show this many results per page |

When a query has a mistake, the panel says what is wrong and offers a one-key fix (⌘.).

## Settings

Search for "Unified Search" in Settings. The most useful ones:

- `unifiedSearch.shortcut.preset`: which key opens the panel.
- `unifiedSearch.caseSensitive`: match case by default.
- `unifiedSearch.index.exclude`: glob patterns never indexed (by default `vendor`, `node_modules` and `*.min.js`).
- `unifiedSearch.open.closeOnOpen`: hide the panel after opening a result (on by default).

Indexes stay on your machine. Nothing is sent anywhere.

## Troubleshooting

The extension and its search process log to the **Unified Search** output channel (View → Output). If search stops
responding, run **Unified Search: Restart Search**; **Unified Search: Rebuild Index** rebuilds the index from scratch.
