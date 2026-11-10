# Decisions

Architecture decision records live in
[`docs/adr/`](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/README.md). Add one for any choice a future
contributor might question; never edit an accepted one, supersede it.

| ADR | Decision | Instead of | Status |
| --- | --- | --- | --- |
| [0001](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0001-go-daemon-over-stdio.md) | Search runs in a Go daemon over JSON-RPC on stdio | Everything in the extension host; a TCP port | Accepted |
| [0002](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0002-json-schema-as-protocol-source.md) | One JSON Schema generates all cross-process types | Hand-written types; Go as the source | Accepted |
| [0003](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0003-in-house-trigram-index.md) | In-house trigram index for the working tree | Zoekt; ripgrep per query | Accepted, revisit when modules can be fetched |
| [0004](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0004-in-house-history-index.md) | History index reuses the trigram package | SQLite FTS5; `git log` per query | Accepted |
| [0005](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0005-vscode-grammar-engine-for-syntax-colors.md) | Code is colored by VS Code's grammar engine, in the extension host | A small lexer per language; the engine in the webview | Accepted |
| [0006](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0006-search-notebooks-by-their-cells.md) | Jupyter notebooks are searched by their cells, and diffed through a textconv filter | Notebooks as text; a doc per cell; diffing cells in the daemon | Accepted |
| [0007](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0007-agents-search-through-the-daemon.md) | Agents and terminal clients search through the daemon over MCP | A separate agent index | Accepted |
| [0008](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0008-explicit-cli-indexing.md) | Explicit terminal indexing shares the writer lock | Requiring an editor or adding implicit writers | Accepted |
| [0009](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0009-cache-lexical-regions-per-snapshot.md) | Bound lexical-region caching to immutable snapshots | Unbounded caching or repeated classification | Accepted |
| [0010](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0010-bind-replacement-to-complete-source.md) | Bind replacement plans to complete source and notebook structure | Checking only target lines or match counts | Accepted |
| [0011](https://github.com/cruisinme30/spelunk/blob/main/docs/adr/0011-version-wiki-source-with-code.md) | Version canonical wiki pages with code and publish a develop mirror | A separate pre-merge wiki update or optional validation | Accepted |

> The [implementation plan](https://github.com/cruisinme30/spelunk/blob/main/docs/dev/implementation-plan.md) still
> describes Zoekt and SQLite; ADRs 0003 and 0004 replaced those parts.

---

**Next:** [[Glossary]]: the words the wiki and the code use.
