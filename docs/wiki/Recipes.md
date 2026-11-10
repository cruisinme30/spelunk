# Recipes

Ready-made queries for everyday jobs. Copy one, change the names, and press ↵. Each links to the reference for the
operator doing the work.

**Jump to:** [Find code](#find-code) · [Dig through history](#dig-through-history) ·
[Look back at a release](#look-back-at-a-release) ·
[Refactor safely](#refactor-safely) · [Review and ownership](#review-and-ownership) · [Keep watch](#keep-watch)

## Find code

| Question | Query |
| --- | --- |
| Where is `RetryPolicy` defined? | [`symbol:`](Definitions-and-Code-Shape#symbol) `symbol:RetryPolicy` |
| Every interface whose name has "Retry" | [`kind:`](Definitions-and-Code-Shape#kind) `kind:interface symbol:Retry` |
| Every Go function whose name has "parse" | [`kind:`](Definitions-and-Code-Shape#kind) `kind:function parse lang:go` |
| Where `RetryPolicy` is used, not defined | [`ref:`](Definitions-and-Code-Shape#ref) `ref:RetryPolicy` |
| Every call of `backoff`, however its arguments are laid out | [`struct:`](Definitions-and-Code-Shape#struct) `struct:"backoff(:[call], tries=:[n])"` |
| The async functions decorated with `@retry` | [Regex across lines](Words-Regex-and-Logic#regexes-across-lines) `/@retry\(.*\)\n\s*async def/` |
| A file whose name you half remember | [Fuzzy names](Words-Regex-and-Logic#fuzzy-file-names): `match:fuzzy usrsvc` finds `UserService.ts` |
| Python files with "retry" in the name | [`type:`](Commits-and-Time#type) `type:file language:python retry` |
| Lines that mention retry, without the files named after it | [`content:`](Words-Regex-and-Logic#content) `content:retry` |
| What the comments and docstrings say about retrying | [`in:`](Words-Regex-and-Logic#in) `retry in:comments` |
| The error message users see, not the code around it | [`in:`](Words-Regex-and-Logic#in) `"budget spent" in:strings` |
| The functions where retrying actually happens, not in comments or quoted text, outside tests and generated files | [`in:`](Words-Regex-and-Logic#in) `kind:function content:retry in:code -is:test -is:generated` |
| Retries outside the tests | [`is:`](Files-and-Repos#is) `retry -is:test` |
| Timeouts outside vendored code, either spelling | [Logic](Words-Regex-and-Logic#logic) `(timeout OR time_out) -file:vendor/` |

## Dig through history

| Question | Query |
| --- | --- |
| Who introduced `retry_policy`? | [`type:`](Commits-and-Time#type) `type:added retry_policy` |
| When did `retry_after` go away? | [`type:`](Commits-and-Time#type) `type:removed retry_after` |
| What did Jane change in the tests? | [`author:`](Commits-and-Time#author) `author:jane is:test` |
| Tests Jane changed this month that mention a timeout | [`since:`](Commits-and-Time#since) `author:jane since:30d file:_test\.py$ timeout` |
| Flaky-test fixes in the checkout repo | [`message:`](Commits-and-Time#message) `message:"fix flaky" repo:web` |
| Jane's Python test changes in six months, about timeouts or retries, outside vendor | `author:jane file:.*test\.py$ (timeout OR retry) -file:vendor/ since:6m` |

## Look back at a release

| Question | Query |
| --- | --- |
| Where `retry_after` was used in the v1.0.0 release, deleted files included | [`at:`](Commits-and-Time#at) `at:v1.0.0 retry_after` |
| How `RetryPolicy` looked six months ago | [`at:`](Commits-and-Time#at) `at:6m RetryPolicy` |

Open a result to see the file as it was, read-only, and **Compare with now** for what changed since.

## Refactor safely

| Question | Query |
| --- | --- |
| Rename `RetryPolicy` without touching generated or copied-in code | [`is:`](Files-and-Repos#is) `RetryPolicy -is:generated -is:vendored`, then Replace |
| TODOs in what you haven't committed yet | [`is:`](Files-and-Repos#is) `is:changed TODO` |
| Retries in the files open in your editor | [`is:`](Files-and-Repos#is) `is:open retry` |

To rename, open the Replace row (⌥⌘F), type the new name and check every match in the preview; **Replace all** makes
one undoable edit. With a `/regex/` term, `$1` in the replacement inserts what the first group matched. See
[Replace](Using-the-Panel#replace).

## Review and ownership

| Question | Query |
| --- | --- |
| Retries in the files the payments team owns | [`owner:`](Files-and-Repos#owner) `retry owner:payments-core` |
| Files nobody owns yet | [`owner:`](Files-and-Repos#owner) `type:file owner:none` |
| Results in one repo only | [`repo:`](Files-and-Repos#repo) `repo:payments retry` |

## Keep watch

- **Pin a query you run often:** the ☆ on a recent query keeps it at the top of the empty box. See
  [pinned queries](Using-the-Panel#pinned-queries).
- **Hear about new commits:** pin a query that searches commits, such as `message:"fix flaky"`, and click its bell.
  New matching commits by others get a count on the pin and a notification. See
  [query watches](Using-the-Panel#query-watches).

---

**Next:** [[Query Language]]: every operator on one screen.
