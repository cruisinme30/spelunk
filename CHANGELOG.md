# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `is:open`, `is:changed` and `is:test` (short form `i:`) keep the files open in editor tabs, the files with
  uncommitted changes, or the test files; `retry -is:test` leaves tests out. In a commit search `is:test` keeps the
  commits' test-file changes. Its suggestions say how many files are in each state now.

### Fixed

- A commit's preview said its other files were hidden by `f:` even when `lang:` hid them; it now says "hidden by
  filters".

## [1.0.1] - 2026-10-05

### Changed

- The README and Getting Started say to install the `.vsix` from the GitHub release instead of building it from a
  clone.

## [1.0.0] - 2026-10-05

### Added

- A plain word with a pipe, like `daemon|search`, now warns that it matches the pipe too, and offers
  `daemon OR search` or `/daemon|search/` instead.
- A plain word in a history query matches a commit's subject and body as well as its changed lines, so
  `author:jane retry` finds a commit that only says "retry" in its message. `msg:` still searches the message alone.
- Commit rows say whether a commit matched in its message, its diff or both, and the commit preview marks the
  matched words in the message.

### Changed

- Unified Search is now called Spelunk. Its settings, commands and keybinding conditions moved from
  `unifiedSearch.*` to `spelunk.*`, and the index from `~/.unified-search` to `~/.spelunk`. Settings you changed
  don't carry over, and the index rebuilds once.

### Fixed

- A recent-queries width saved in a wide panel no longer squeezes the operator cheat sheet when the panel narrows,
  and the divider between them shows from the first width where both fit.
- Screen readers announce where the divider between recent queries and the operators is.
- `?` in an empty query box brings the operator cheat sheet into view when a narrow panel stacks it below the
  recent queries.

## [0.2.0] - 2026-10-05

### Added

- Drag the line between recent queries and the operators to resize them, or focus it and press ← →. Double-click it
  to reset; the panel remembers the width.
- Every operator has a full name and a one-letter short name, such as `file:` and `f:` or `author:` and `a:`.
  Suggestions and help show the full name with the short one beside it; `lang:`, `sym:` and `msg:` still work.
- Remove a recent query with the × at the end of its row, or select it and press ⇧⌫.

### Fixed

- A query can no longer hang or crash the daemon: queries with many misplaced globals parse in linear time, a
  pasted megabyte parses in under a second, deeply nested parentheses no longer overflow the stack, and one query
  reports at most 100 problems.
- Fix-its no longer join words or land inside an unclosed quote, and closing a quote or regex that ends in a
  backslash really closes it. A fix-it, completion or toggle computed for older text is not applied to newer text,
  and ⌘Z undoes it.
- Indexing never blocks on a FIFO, never reads through a symlink out of the root, and indexes nested repos,
  submodules and roots reached through a symlink. Very long or minified lines are searched and previewed in
  bounded time and memory.
- A damaged saved index is rebuilt instead of crashing a search, saved indexes are synced before they replace the
  old ones, and temp files left by a crash are removed.
- History reads every commit whatever its message, date or the user's Git config (`diff.noPrefix`,
  `log.showSignature` and others); a failed read no longer publishes a partial history.
- A forged result ref can't open a file outside its folder through a symlink.
- The daemon refuses oversized or malformed JSON-RPC frames without allocating them, answers every malformed
  message, survives a panicking handler, stops writing once its output fails, and exits soon after VS Code goes away.
- The extension recovers from garbage on the daemon's stdout, times out a daemon that never finishes starting, stops
  at once when the binary is missing or not executable, and uses defaults for settings of the wrong type.
- The search panel drops malformed or late messages, ignores a corrupt saved state, leaves keys to an input method
  while it composes, keeps the query box in sight in a short panel, and fits the help and welcome pages in a narrow
  editor. Highlights never split an emoji.
- A pasted query of any length costs little: only its first 2000 characters are read, and it is reported as too
  long.
- A daemon that keeps crashing is restarted after a pause that doubles each time, up to 30 seconds, instead of every
  200 ms forever.
- A result after invalid UTF-8 in a file opens at the column VS Code shows: invalid bytes are replaced the way the
  editor replaces them.

### Changed

- Files are searched as the editor shows them: a UTF-8 byte order mark is dropped, UTF-16 files with a byte order
  mark are indexed (they were treated as binary), and a lone CR ends a line.
- `sym:` finds Java and C# methods by their line, whatever its indent, and finds constructors explicitly.

## [0.1.0] - 2026-10-05

### Added

- Search daemon skeleton: JSON-RPC 2.0 over stdio, initialize/shutdown lifecycle, crash restarts.
- `protocol/` JSON Schema with generated TypeScript and Go types.
- VS Code extension host: daemon supervisor, search controller and search panel.
- Search panel webview: query box with Aa / .* toggles, completions, fix-its, chips, results and preview.
- Query language: parser, diagnostics with fix-its, completions and planner for every operator.
- Working-tree search over a trigram index: file names and code, `f:`, `case:`, `/regex/`, `lang:`, `repo:`,
  `type:`, `count:` with Load more, AND / OR / NOT, and "N hidden by …" notes with an undo.
- Background indexing per workspace root, saved between sessions, with `index/status`, `index/rebuild` and progress.
- Previews highlight every term of the query that produced the result.
- Operator suggestions with descriptions and example values; results stay on the finished words while a word is
  being completed, and Enter or Esc searches it as typed.
- A repo menu that scopes the query to one repo, a "Show code too" undo for `type:file`, and "Ignore case" for
  `case:yes`.
- The help page: every operator with an example to try, worked examples, keys and settings.
- Typing an operator lists the values it takes: time windows for `since:` with when each starts and how many files
  changed, file types and folders for `f:`, repos with their index state, the workspace's languages, and what each
  `type:`, `case:` and `count:` value does.
- `f:` and `repo:` also take globs such as `*.go` and `src/**/*.ts`.
- `since:` also takes `today`, `yesterday`, minutes (`30min`) and hours (`2h`). `since:30m` (30 months) warns and
  offers `30min`.
- `npm run package` builds a `.vsix` for any platform, and F5 runs the extension on the sample repos.
- Git history search: `author:`, `msg:` and `type:commit` find commits by their added and removed lines, messages and
  authors (after `.mailmap`), newest first, with the diff in the preview. Recent commits are searchable while older
  history is still being read, and new commits within seconds. `index.historyDepth` sets how far back.
- `since:` on current files uses each file's last commit, and counts files with uncommitted edits as changed now.
- `author:` lists the workspace's authors with their commit counts, and `msg:` suggests the words and phrases of
  recent commit subjects.
- Saved, created and deleted files are searchable within a second, without rebuilding the index.
- `sym:` finds definitions (classes, interfaces, functions, methods and types) in 14 languages, suggests the
  workspace's names as you type, and offers to search a name as text with how many matches that finds. A file
  preview lists the members of the class it shows.
- A welcome page, shown once after installing and by **Unified Search: Show Welcome**: pick the search shortcut and
  how much to index, and watch each repo's indexing.
- After opening a result, the status bar says which one it is ("Opened from search · result 2 of 5"); F4 and ⇧F4
  step through the rest, and clicking it goes back to the results.
- When a search finds nothing, the panel says why (for example that `case:yes` is on) above what each filter hid.

### Fixed

- A reopened search panel no longer ignores the queries typed into it.
- A search panel opened after indexing finished shows the workspace's repos ("All repos · 0" before).
- Opening a result to the side (⌘↵) keeps the search panel open; `open.closeOnOpen` hides it only for results opened
  in its place.
- Indexing progress bars fill in VS Code: they were set with style attributes, which webviews block.
- A saved index that can't be read says so while it is rebuilt, and an index that can't be saved (a full disk) keeps
  serving with a warning.
- Case-insensitive searches no longer copy each file to lowercase it, and regex terms are found by their literal
  part first, so both are faster.
- Files inside a folder named in `unifiedSearch.index.exclude`, such as `node_modules`, are no longer indexed.
- A query whose words never occur in the same file no longer scans every file.
- Restarting the search from the panel while a crash restart was pending no longer starts a second daemon.
- A repo whose index failed no longer shows as "Indexing" in the status bar.
- The `case:` suggestion shows its description and values like the other operators.
- `sym:` queries return no results instead of wrong ones until symbol search exists.

[Unreleased]: https://github.com/cruisinme30/spelunk/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/cruisinme30/spelunk/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/cruisinme30/spelunk/compare/v0.2.0...v1.0.0
[0.2.0]: https://github.com/cruisinme30/spelunk/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/cruisinme30/spelunk/releases/tag/v0.1.0
