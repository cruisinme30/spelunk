# Mocks

The interactive mocks live on the design canvas: <https://claude.ai/artifact/GaBrpZJxgp9ZkJPKZqdq7F>

Planning docs refer to the mockups by number (and the test plan's E01–E18 scenarios follow the same order). Tests
refer to them by screen id, as in `@covers screen:file-names-only`; `scripts/specCoverage.mjs` checks every screen is
covered.

| # | Mock | Screen id | Canvas board | Milestone |
| --- | --- | --- | --- | --- |
| 1 | Plain text — names + contents | `plain-text-search` | `Main` | M1 |
| 2 | `f:` path regex + text | `path-scoped-search` | `PathScoped` | M1 |
| 3 | `author:` + `f:` + text → commit diffs | `author-history` | `AuthorHistory` | M3 |
| 4 | Empty box — recent queries + operator sheet | `empty-box` | `EmptyState` | M2 |
| 5 | Autocomplete — operators | `operator-suggestions` | `OperatorSuggest` | M2 |
| 6 | Autocomplete — author values | `value-suggestions` | `ValueSuggest` | M3 |
| 7 | AND / OR / ( ) / - / `since:` in history | `boolean-history` | `BooleanLogic` | M3 |
| 8 | `case:` + `/regex/` + `lang:` | `case-regex-language` | `Matching` | M1 |
| 9 | `sym:` definitions | `symbol-definitions` | `Symbols` | M4 |
| 10 | `since:` on current files | `since-on-files` | `SinceWorkingTree` | M4 |
| 11 | `msg:"phrase"` + `repo:` + `count:` | `message-repo-count` | `MessageCount` | M3 |
| 12 | `type:file` + `lang:` | `file-names-only` | `TypeFile` | M2 |
| 13 | Query errors with fixes | `query-errors` | `ParseError` | M1 |
| 14 | No results while indexing | `no-results-while-indexing` | `NoResults` | M4 |
| 15 | First run — pick a shortcut, index repos | `first-run` | `FirstRun` | M5 |
| 16 | Double-click opens the file at the match | `opened-file` | `OpenedFile` | M5 |
| 17 | Help guide | `help-page` | `Help` | M2 |
| 18 | Settings in VS Code | `settings` | `Settings` | M5 |
| 19 | Values — every author for `author:` | `author-values` | `AuthorValues` | M3 |
| 20 | Values — time windows for `since:` | `since-values` | `SinceValues` | M2 |
| 21 | Values — file types and folders for `f:` | `path-values` | `PathValues` | M2 |
| 22 | Values — `repo:`, `lang:`, `type:`, `case:`, `word:`, `count:`, `order:`, `is:`, `sym:`, `msg:` | `other-values` | `OtherValues` | M2 |
| 23 | Bare words match commit messages too | `words-in-messages` | `MessageWords` | M3 |
| 24 | Best match first: definitions and names first, tests last | `ranked-results` | `Ranking` | 1.1 |
| 25 | `order:path` keeps path order | `path-order` | `PathOrder` | 1.1 |
| 26 | `is:test` keeps test files | `test-files` | `TestFiles` | 1.1 |
| 27 | `author:` + `is:test` → the test changes in commits | `test-history` | `TestHistory` | 1.1 |
| 28 | `word:yes` keeps whole words | `whole-words` | `WholeWords` | 1.1 |
| 29 | `case:smart` matches case when the query has a capital | `smart-case` | `SmartCase` | 1.1 |
| 33 | Bare words find file names by their letters in order | `fuzzy-file-names` | `FuzzyNames` | 1.1 |
| 34 | `type:added` finds the commits that added a line | `added-removed-lines` | `AddedLines` | 1.1 |
| 35 | `kind:` keeps one kind of definition; `ref:` finds a name's uses | `symbol-kinds` | `SymbolKinds` | 1.1 |
