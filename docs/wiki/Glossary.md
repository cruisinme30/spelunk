# Glossary

Words the wiki and the code use, in one place.

| Term | Means |
| --- | --- |
| **Working tree** | The files as they are on disk now, including uncommitted edits. Queries without a history operator search it |
| **History** | The commits on each repo's checked-out branch: its own (first-parent) line, plus the commits merges brought in from other branches when `index.mergedBranches` is on. A renamed file is followed under each name it had. `author:`, `message:` and `type:commit`, `type:added` or `type:removed` search it |
| **Revision** | A tag, branch, commit or time that `at:` reads the files at |
| **Operator** | A `name:value` part of a query, such as `file:*.py`. Each has a full name; `file:`, `author:`, `symbol:`, `language:` and `message:` also have a short one (`f:`, `a:`, `sym:`, `lang:`, `msg:`) |
| **Global operator** | One that applies to the whole query: `type:`, `case:`, `word:`, `count:`, `order:`, `at:` |
| **Chip** | One piece of the query as the daemon read it, shown under the box |
| **Diagnostic** | An error or warning about the query, with a code from the [Error index](Error-Index) |
| **Fix-it** | A suggested edit that resolves a diagnostic; ⌘. applies the first |
| **Facet** | A count of results by repo, language, owner, folder, author or month, which narrows the results when clicked |
| **Pin**, **watch** | A query kept at the top of the empty box, and a pinned history query that reports new matching commits |
| **Daemon** | The Go process that parses, plans, searches and indexes. See [[Daemon]] |
| **Extension host** | The VS Code side: commands, settings, editors, and running the daemon. See [[Extension Host]] |
| **Webview** | The search panel, help and welcome pages. A pure view. See [[Webview]] |
| **Plan** | What the planner turns a parsed query into, for exactly one engine |
| **Engine** | `trigram` (working tree) or `history` (commits): runs a plan and returns results |
| **Shard**, **segment** | One piece of the working-tree index, and one piece of the history index. See [[Indexing]] |
| **Overlay shard** | A small shard holding files saved since the last build, masking their old copies |
| **Published snapshot** | A complete set of shards and segments that engines read; the indexer swaps in a new one in one step |
| **`ref`** | An opaque handle to a result that the host and webview pass back unchanged |
| **`seq`** | The keystroke number every message carries, so the webview drops stale responses |
| **Screen id** | The name tests use for a mock, as in `@covers screen:file-names-only`. See [[Screens]] |
| **`@covers`** | A tag on a test naming the spec items it proves; `scripts/specCoverage.mjs` lists the untested ones |
| **Oracle** | A trusted tool a test compares results with: ripgrep for files, `git log -G` for history |

---

**Next:** [[Contributing]]: make your first change.
