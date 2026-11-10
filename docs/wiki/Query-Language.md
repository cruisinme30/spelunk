# Query Language

Every operator on one screen. A query is words and operators; results update as you type. Click an operator for
its full card, with what it takes, how it combines and the screen it produces.

## Words

| Write | Matches |
| --- | --- |
| `retry policy` | The lines of files whose text has **both** words, and the files whose name has both |
| `"read timeout"` | The exact phrase, spaces included |
| `/Retry(Policy\|Config)/` | A regular expression ([regex reference](Words-Regex-and-Logic#regexes)) |
| `match:fuzzy usrsvc` | Also file names with those letters in order, like `UserService.ts` ([fuzzy matching](Words-Regex-and-Logic#fuzzy-file-names)) |

Matching ignores case and matches inside longer words, unless you add `case:yes` or `word:yes`.

## Logic

| Write | Means |
| --- | --- |
| `a b` or `a AND b` | Both must match |
| `a OR b` | Either matches |
| `( … )` | Group terms |
| `-word`, `NOT word`, `-file:vendor/` | Leave out what the word or operator matches |

AND binds tighter than OR, so use parentheses whenever you mix them. [More on logic](Words-Regex-and-Logic#logic).

## Operators

Every operator has a full name. The five people reach for most also have a short name that means the same thing:
`f:` for `file:`, `a:` for `author:`, `sym:` for `symbol:`, `lang:` for `language:` and `msg:` for `message:`. The
rest are typed in full. A one-letter name that has gone (`s:`, `i:`, `d:` and the like), or `path:`, is an unknown
operator, and its fix changes it to the full name: `s:Retry` becomes `symbol:Retry`.

| Full | Short | Takes | Example | Searches |
| --- | --- | --- | --- | --- |
| [`file:`](Files-and-Repos#file) | `f:` | A regex on the path (`\.go$`) or a glob (`*.go`, `src/**/*.ts`) | `file:_test\.py$ timeout` | Both |
| [`repo:`](Files-and-Repos#repo) | | A regex (`^web`) or a glob (`web-*`) on the repo name | `repo:payments retry` | Both |
| [`language:`](Files-and-Repos#language) | `lang:` | A language or alias: `python`, `py`, `ts`, `golang` | `language:python retry` | Both |
| [`since:`](Commits-and-Time#since) | | `today`, `yesterday`, a number + `min`, `h`, `d`, `w`, `m` (months), `y`, or a date: `2026-09-30` (from that day's midnight) or `2026-09` (from the 1st) | `since:2w timeout` | Both |
| [`until:`](Commits-and-Time#until) | | `since:`'s values; keeps what is before the end of the day or month (`until:2026-09` is before October), or older than the length of time (`until:2w`) | `since:2026-09 until:2026-09 timeout` | Both |
| [`type:`](Commits-and-Time#type) | | `file` (names only), `code` (lines only), `commit` (switches to history), or `added` or `removed` (history, only the lines commits added or removed) | `type:file retry` | Both |
| [`symbol:`](Definitions-and-Code-Shape#symbol) | `sym:` | Part of a definition's name, or a `/regex/`; returns definitions only | `symbol:RetryPolicy` | Files only |
| [`kind:`](Definitions-and-Code-Shape#kind) | | `function`, `method`, `class`, `interface`, `type` or `other`; returns definitions of that kind only | `kind:class symbol:Retry` | Files only |
| [`ref:`](Definitions-and-Code-Shape#ref) | | A name, or a `"quoted name"`; returns the lines that use it as a whole word, without the lines that define it | `ref:RetryPolicy` | Files only |
| [`content:`](Words-Regex-and-Logic#content) | | A word, a `"phrase"` or a `/regex/` matched in file text only, never in file names or commit messages | `content:retry` | Both |
| [`struct:`](Definitions-and-Code-Shape#struct) | | A `"quoted template"`: code with `:[holes]`, matched by its shape in file text (see [Code by its shape](Definitions-and-Code-Shape#struct)) | `struct:"backoff(:[call], tries=:[n])"` | Files only |
| [`in:`](Words-Regex-and-Logic#in) | | `comments`, `strings` or `code`: keeps only the matches inside comments (Python docstrings too), inside quoted text, or in code outside both; `-in:` leaves that place out. Plain text and Markdown always match | `in:comments retry` | Files only |
| [`is:`](Files-and-Repos#is) | | `open` (open in an editor tab), `changed` (uncommitted: edited, added or staged), `test` (a test file), `generated` (written by a tool) or `vendored` (someone else's code, or a build's) | `RetryPolicy -is:generated -is:vendored` | Both (`test`, `generated`, `vendored`); files only (`open`, `changed`) |
| [`owner:`](Files-and-Repos#owner) | | An owner in the repo's CODEOWNERS file: a team (`@acme/payments-core`, or `payments-core` alone), a person (`@jane` or `jane`) or an email, matched whole and ignoring case; or `none`, for files no line gives an owner | `owner:payments-core retry` | Files only |
| [`excludes:`](Files-and-Repos#excludes) | | `yes` (leave out the files VS Code's `files.exclude` and `search.exclude` hide, the default) or `no` (show them too, marked excluded and last) | `excludes:no retry` | Both |
| [`author:`](Commits-and-Time#author) | `a:` | Part of a name or email (`jane`, `@acme.dev`), or an exact name in quotes (`"Jane Doe"`, not compared with emails), after `.mailmap` | `author:jane timeout` | Commits only |
| [`message:`](Commits-and-Time#message) | `m:` | Words, a `"phrase"` or a `/regex/` on the commit message | `message:"fix flaky"` | Commits only |
| [`case:`](Words-Regex-and-Logic#case) | `c:` | `yes`, `no`, or `smart` (match case only when the query has a capital letter) | `case:yes RetryPolicy` | Both |
| [`word:`](Words-Regex-and-Logic#word) | `w:` | `yes` (whole words only) or `no` (parts of words too) | `word:yes retry` | Both |
| [`match:`](Words-Regex-and-Logic#fuzzy-file-names) | | `exact` (a word matches its text only, the default) or `fuzzy` (file names by their letters too, as Quick Open finds files) | `match:fuzzy rtrypol` | Files only |
| [`count:`](Results-and-Ranking#count) | `n:` | A positive number (capped at 50,000), or `all` | `count:all retry` | Both |
| [`order:`](Results-and-Ranking#order) | | `best` (best match first, the default), `file:asc` or `file:desc` (by file path, A to Z or Z to A), or `recent:desc` or `recent:asc` (by when each file last changed, newest or oldest first) | `order:recent:desc retry` | Files only |
| [`at:`](Commits-and-Time#at) | | A tag, branch or commit (`v1.0.0`, `main`, `3f9c2e1`, `HEAD~10`), or a time as `until:` takes it (`6m`, `2026-01-31`): the files as they were then (see [Past revisions](Commits-and-Time#at)) | `at:v1.0.0 retry_after` | Files only |

`type:`, `case:`, `word:`, `match:`, `in:`, `count:`, `order:` and `at:` apply to the whole query, so each may appear
once, outside
parentheses, `OR`, `-` and `NOT`; only `in:` may take a minus, as `-in:comments`.

## Files or commits

A query searches **either** current files **or** Git history, never both. `author:`, `message:`, `type:commit`,
`type:added` and `type:removed` make it search history; everything else searches current files. Operators marked
"Both" work either way. See [Commits and time](Commits-and-Time#files-or-commits).

## Worked examples

| Find | Query |
| --- | --- |
| Tests Jane changed this month that mention a timeout | `author:jane since:30d file:_test\.py$ timeout` |
| Where `RetryPolicy` is defined | `symbol:RetryPolicy` |
| Every interface whose name has "Retry" | `kind:interface symbol:Retry` |
| Where `RetryPolicy` is used, not defined | `ref:RetryPolicy` |
| Every use of `RetryPolicy` inside a function, with the function it's in | `kind:function ref:RetryPolicy` |
| Retries outside the tests | `retry -is:test` |
| Rename `RetryPolicy` without touching generated or copied-in code | `RetryPolicy -is:generated -is:vendored` |
| Retries in the files the payments team owns | `retry owner:payments-core` |
| Files nobody owns yet | `type:file owner:none` |
| Lines that mention retry, without the files named after it | `content:retry` |
| Every call of `backoff`, however its arguments are laid out | `struct:"backoff(:[call], tries=:[n])"` |
| Where `retry_after` was used in the v1.0.0 release, deleted files included | `at:v1.0.0 retry_after` |
| TODOs in what you haven't committed yet | `is:changed TODO` |
| Notes about retrying in comments and docstrings, not the code | `retry in:comments` |
| Uses of `retry` in code, not in comments or messages | `retry in:code` |
| Python files with "retry" in the name | `type:file language:python retry` |
| Flaky-test fixes in the checkout repo | `message:"fix flaky" repo:web` |
| Timeouts outside vendored code, either spelling | `(timeout OR time_out) -file:vendor/` |
| Jane's Python test changes in six months, about timeouts or retries, outside vendor | `author:jane file:.*test\.py$ (timeout OR retry) -file:vendor/ since:6m` |

Many more, grouped by what you're trying to do, are on [[Recipes]].

---

**Next:** [[Words, regex and logic|Words Regex and Logic]] for the details of matching.
