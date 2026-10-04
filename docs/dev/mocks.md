# Mocks

The interactive mocks live on the design canvas: https://claude.ai/artifact/GaBrpZJxgp9ZkJPKZqdq7F

Mock numbers are referenced throughout the implementation plan, the test plan (E01–E18) and milestone exit gates.

| # | Mock | Canvas board | Milestone |
| --- | --- | --- | --- |
| 1 | Plain text — names + contents | `Main` | M1 |
| 2 | `f:` path regex + text | `PathScoped` | M1 |
| 3 | `author:` + `f:` + text → commit diffs | `AuthorHistory` | M3 |
| 4 | Empty box — recent queries + operator sheet | `EmptyState` | M2 |
| 5 | Autocomplete — operators | `OperatorSuggest` | M2 |
| 6 | Autocomplete — author values | `ValueSuggest` | M3 |
| 7 | AND / OR / ( ) / - / `since:` in history | `BooleanLogic` | M3 |
| 8 | `case:` + `/regex/` + `lang:` | `Matching` | M1 |
| 9 | `sym:` definitions | `Symbols` | M4 |
| 10 | `since:` on current files | `SinceWorkingTree` | M4 |
| 11 | `msg:"phrase"` + `repo:` + `count:` | `MessageCount` | M3 |
| 12 | `type:file` + `lang:` | `TypeFile` | M2 |
| 13 | Query errors with fixes | `ParseError` | M1 |
| 14 | No results while indexing | `NoResults` | M4 |
| 15 | First run — pick a shortcut, index repos | `FirstRun` | M5 |
| 16 | Double-click opens the file at the match | `OpenedFile` | M5 |
| 17 | Help guide | `Help` | M2 |
| 18 | Settings in VS Code | `Settings` | M5 |
