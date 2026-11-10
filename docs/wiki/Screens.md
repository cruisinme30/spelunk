# Screens

The 56 design mocks from the [design canvas](https://claude.ai/artifact/GaBrpZJxgp9ZkJPKZqdq7F), grouped by the wiki
page that documents each one. Tests cite them by screen id,
e.g. `@covers screen:file-names-only` ([mocks.md](https://github.com/cruisinme30/spelunk/blob/main/docs/dev/mocks.md)).

Each screen's block on that page shows the mock and, under **The built panel**, a screenshot of the real panel. Screens
16 and 18 are
VS Code's own editor and Settings, which the screenshot tool can't draw, so they have no screenshot.

## [[Getting Started]]

Installing and the first run.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 15 | [First run: pick a shortcut, index repos](Getting-Started#first-run) | `first-run` | M5 | ✓ screenshot |

## [[Words, regex and logic|Words Regex and Logic]]

How words, regexes, case and logic match.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 1 | [Plain text: names and contents](Words-Regex-and-Logic#words) | `plain-text-search` | M1 | ✓ screenshot |
| 7 | [AND / OR / ( ) / - / `since:` in history](Words-Regex-and-Logic#logic) | `boolean-history` | M3 | ✓ screenshot |
| 8 | [`case:` + `/regex/` + `lang:`](Words-Regex-and-Logic#regexes) | `case-regex-language` | M1 | ✓ screenshot |
| 28 | [`word:yes` keeps whole words](Words-Regex-and-Logic#word) | `whole-words` | 1.1 | ✓ screenshot |
| 29 | [`case:smart` matches case when the query has a capital](Words-Regex-and-Logic#case) | `smart-case` | 1.1 | ✓ screenshot |
| 33 | [`match:fuzzy` finds file names by their letters in order; without it, a note offers them](Words-Regex-and-Logic#fuzzy-file-names) | `fuzzy-file-names` | 2.1 | ✓ screenshot |
| 39 | [Multiline regex: a regex with `\n` matches across lines, one result per match](Words-Regex-and-Logic#regexes-across-lines) | `multiline-regex` | 1.2 | ✓ screenshot |
| 57 | [Comments, quoted text or code: `in:comments`, `in:strings` and `in:code` keep each match by where it is; `-in:` leaves one place out](Words-Regex-and-Logic#in) | `comments-strings` | 2.1 | ✓ screenshot |

## [[Files and repos|Files and Repos]]

Narrowing by path, repo, language, file state, owner and VS Code's excludes.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 2 | [`f:` path regex + text](Files-and-Repos#file) | `path-scoped-search` | M1 | ✓ screenshot |
| 12 | [`type:file` + `lang:`](Files-and-Repos#names-only-or-code-only) | `file-names-only` | M2 | ✓ screenshot |
| 26 | [`is:test` keeps test files](Files-and-Repos#is) | `test-files` | 1.1 | ✓ screenshot |
| 37 | [`-is:generated` / `-is:vendored` leave tool-written and third-party files out](Files-and-Repos#is) | `generated-vendored` | 1.2 | ✓ screenshot |
| 42 | [`owner:` keeps the files a team owns in CODEOWNERS](Files-and-Repos#owner) | `owner-filter` | 1.2 | ✓ screenshot |
| 52 | [VS Code's `files.exclude` and `search.exclude` hide results too; a note counts them, and Show them adds `excludes:no`](Files-and-Repos#excludes) | `editor-excludes` | 2.1 | ✓ screenshot |

## [[Definitions and code shape|Definitions and Code Shape]]

Definitions, uses and code by its shape.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 9 | [`sym:` definitions](Definitions-and-Code-Shape#symbol) | `symbol-definitions` | M4 | ✓ screenshot |
| 35 | [`kind:` keeps one kind of definition; `ref:` finds a name's uses](Definitions-and-Code-Shape#ref) | `symbol-kinds` | 1.1 | ✓ screenshot |
| 45 | [Uses inside definitions: `kind:` with `ref:`, `content:` or `struct:` lists each line inside the definitions it keeps, naming the one it is in](Definitions-and-Code-Shape#inside-definitions) | `uses-in-definitions` | 2.1 | ✓ screenshot |
| 40 | [`struct:` finds code by its shape: a template's holes take any code, over lines](Definitions-and-Code-Shape#struct) | `structural-search` | 1.2 | ✓ screenshot |

## [[Commits and time|Commits and Time]]

Commits, time windows and past revisions.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 3 | [`author:` + `f:` + text → commit diffs](Commits-and-Time#author) | `author-history` | M3 | ✓ screenshot |
| 10 | [`since:` on current files](Commits-and-Time#since) | `since-on-files` | M4 | ✓ screenshot |
| 11 | [`msg:"phrase"` + `repo:` + `count:`](Commits-and-Time#message) | `message-repo-count` | M3 | ✓ screenshot |
| 23 | [Bare words match commit messages too](Commits-and-Time#message) | `words-in-messages` | M3 | ✓ screenshot |
| 27 | [`author:` + `is:test` → the test changes in commits](Commits-and-Time#author) | `test-history` | 1.1 | ✓ screenshot |
| 34 | [`type:added` finds the commits that added a line](Commits-and-Time#type) | `added-removed-lines` | 1.1 | ✓ screenshot |
| 41 | [`at:` searches the files as they were at a tag, branch, commit or time](Commits-and-Time#at) | `past-revision` | 1.2 | ✓ screenshot |

## [[Results and ranking|Results and Ranking]]

Order, counts, facets, context lines and syntax colors.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 24 | [Best match first: definitions and names first, tests last](Results-and-Ranking#best-match-first) | `ranked-results` | 1.1 | ✓ screenshot |
| 25 | [`order:file` keeps path order](Results-and-Ranking#order) | `path-order` | 1.1 | ✓ screenshot |
| 30 | [Facets — counts by repo, language and folder (author and month for commits) narrow the results](Results-and-Ranking#facets) | `result-facets` | 1.1 | ✓ screenshot |
| 36 | [Context lines: a few lines around each code match, merged when they touch](Results-and-Ranking#context-lines) | `context-lines` | 1.2 | ✓ screenshot |
| 43 | [Syntax colors and whitespace: code in the theme's colors, by VS Code's grammars](Results-and-Ranking#syntax-colors-and-whitespace) | `syntax-colors` | 2.1 | ✓ screenshot |
| 54 | [Recently changed first: the order name above the results is a menu, with an arrow for the direction; each code result says when its file changed](Results-and-Ranking#order) | `recent-order` | 2.1 | ✓ screenshot |

## [[Error index|Error Index]]

Query errors and their fixes.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 13 | [Query errors with fixes](Error-Index#how-errors-look) | `query-errors` | M1 | ✓ screenshot |

## [[Using the Panel]]

The empty box, pins, watches, opening, Replace and Help.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 4 | [Empty box: recent queries + operator sheet](Using-the-Panel#the-empty-box) | `empty-box` | M2 | ✓ screenshot |
| 16 | [Double-click opens the file at the match](Using-the-Panel#opening-a-result) | `opened-file` | M5 | VS Code's own UI |
| 17 | [Help guide](Using-the-Panel#help) | `help-page` | M2 | ✓ screenshot |
| 31 | [Pinned queries: star a recent query, name it, keep it on top](Using-the-Panel#pinned-queries) | `pinned-queries` | 1.1 | ✓ screenshot |
| 32 | [Replace in current files: preview every edit, then one undoable change](Using-the-Panel#replace) | `replace-in-files` | 1.1 | ✓ screenshot |
| 38 | [Query watches: a bell on a pinned history query; new matching commits get a count, a notification and a New tag](Using-the-Panel#query-watches) | `query-watches` | 1.2 | ✓ screenshot |
| 44 | [Outline: the previewed file's definitions, members under their type, closed until opened](Using-the-Panel#the-outline) | `file-outline` | 2.1 | ✓ screenshot |
| 50 | [Search from your code: the editor's selection, or the name at the cursor, starts the search](Using-the-Panel#searching-from-your-code) | `seed-from-editor` | 2.1 | ✓ screenshot |
| 51 | [Search in Folder: right-click a folder in the Explorer to search only inside it](Using-the-Panel#searching-a-folder) | `search-in-folder` | 2.1 | ✓ screenshot |
| 55 | [Jump to a definition by name: ⌘T finds functions and classes in every repo and language; Go to Definition and Find References can fall back to Spelunk](Using-the-Panel#go-to-symbol-and-go-to-definition) | `editor-navigation` | 2.1 | ✓ screenshot |
| 53 | [Result menu: right-click a result to copy it, its path, its link on GitHub (or GitLab, Bitbucket, Gitea, Azure DevOps), every result, or a link to the search](Using-the-Panel#sharing-a-result) | `share-result` | 2.1 | ✓ screenshot |
| 59 | [History through renames: a file's commits under every name it had, renames marked, and the name each file has now](Commits-and-Time#renames-and-merged-branches) | `history-renames` | 2.1 | ✓ screenshot |
| 60 | [Jupyter notebooks: searched by their cells, each result under its cell, opened at that cell](Using-the-Panel#jupyter-notebooks) | `notebook-cells` | 2.1 | ✓ screenshot |
| 61 | [Hover labels: rest the mouse on an icon and its name shows in a third of a second](Using-the-Panel#names-on-hover) | `hover-labels` | 2.1 | ✓ screenshot |
| 62 | [Ask in plain words: a question ending in ?, or the Generate query button, becomes a query shown under the box, which runs](Using-the-Panel#asking-in-plain-words) | `ask-in-words` | 2.1 | ✓ screenshot |

## [[Autocomplete]]

What the box suggests after each operator.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 5 | [Autocomplete: operators](Autocomplete#operators) | `operator-suggestions` | M2 | ✓ screenshot |
| 6 | [Autocomplete: author values](Autocomplete#authors) | `value-suggestions` | M3 | ✓ screenshot |
| 19 | [Values: every author for `author:`](Autocomplete#authors) | `author-values` | M3 | ✓ screenshot |
| 20 | [Values: time windows for `since:`](Autocomplete#time-windows) | `since-values` | M2 | ✓ screenshot |
| 21 | [Values: file types and folders for `f:`](Autocomplete#file-types-and-folders) | `path-values` | M2 | ✓ screenshot |
| 22 | [Values: `repo:`, `lang:`, `type:`, `kind:`, `case:`, `word:`, `count:`, `order:`, `is:`, `sym:`, `msg:`](Autocomplete#every-other-operator) | `other-values` | M2 | ✓ screenshot |

## [[Agents and the Terminal]]

Claude Code, Copilot and the Terminal searching with Spelunk.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 56 | [Agents and the terminal: the `spelunk` command and Claude Code or Copilot search the same index, live from VS Code or from the saved index](Agents-and-the-Terminal#where-answers-come-from) | `agent-access` | 2.1 | ✓ screenshot |

## [[Settings and Commands]]

Settings in VS Code.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 18 | [Settings in VS Code](Settings-and-Commands#how-it-looks-in-settings) | `settings` | M5 | VS Code's own UI |

## [[Troubleshooting]]

While a repo is still indexing.

| # | Screen | Screen id | Milestone | Built |
| --- | --- | --- | --- | --- |
| 14 | [No results while indexing](Troubleshooting#why-are-some-results-missing-while-a-repo-indexes) | `no-results-while-indexing` | M4 | ✓ screenshot |

---

**Next:** [[Contributing]]: make your first change.
