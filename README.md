# Unified Search

**One search box for file names, code and Git history, across every repo in your VS Code workspace.**

```text
author:jane f:.*test\.py$ (timeout OR retry) -f:vendor/ since:6m
```

That query finds Jane's commits from the last six months that changed a Python test outside `vendor/` and added or
removed `timeout` or `retry`. Drop `author:` and the same box searches current files instead.

![The search panel showing code matches for retry_policy, with a preview](docs/dev/proof/search-panel-results-and-preview.png)

> **Status: early development, no Marketplace release yet.** Searching current files, definitions and Git history
> works: file names, code lines, `sym:` definitions and commits, with the whole query language, previews and opening
> results. See [progress](docs/dev/progress.md).

## Features (planned for the first release)

- **One query language.** Combine text, `"phrases"`, `/regex/`, `AND`, `OR`, `( )` and `-exclusions` with
  operators, each with a full name and a one-letter short one: `file:` / `f:`, `repo:` / `r:`, `language:` / `l:`,
  `type:` / `t:`, `symbol:` / `s:`, `author:` / `a:`, `message:` / `m:`, `since:` / `d:`, `case:` / `c:`,
  `count:` / `n:`.
- **Results as you type,** from local indexes: a trigram index of current files, symbols, and a commit index of
  diffs, messages and authors.
- **Helpful when you're wrong:** errors with one-key fixes, completions for operators and values (authors, repos,
  languages), and notes like "3 commits hidden by `-f:vendor/`".
- **Keyboard first:** ⌘P or ⇧⌘F (the `unifiedSearch.shortcut.preset` setting), ↵ to open at the match, ⌘↵ to
  open to the side, F4 to step through results.
- Works the same in desktop VS Code and code-server.

## Repository layout

| Path | What lives there |
| --- | --- |
| [`daemon/`](daemon/) | Go search daemon: parser, planner, indexes, engines |
| [`extension/`](extension/) | VS Code extension host (TypeScript) |
| [`webview/`](webview/) | The search panel (TypeScript, no framework) |
| [`protocol/`](protocol/) | JSON Schema for every cross-process message, plus the type generator |
| [`docs/`](docs/) | User docs, decision records, and planning docs |
| [`scripts/`](scripts/) | `test-all.sh`, the spec-coverage check and the panel screenshot tool |
| [`testdata/`](testdata/) | A fixture workspace whose files match the design mockups |

The [wiki](https://github.com/cruisinme30/unified-search/wiki) explains how the pieces fit together, with diagrams
and every design mock.

## Development

```sh
npm install && npx playwright install chromium
npm test        # every test layer, including the Go daemon
```

Then open the repo in VS Code and press F5 to run the extension on the sample repos, or install it in your own VS Code:

```sh
npm run package && code --install-extension out/unified-search-*.vsix
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for conventions, and [docs/](docs/README.md) for everything else.

## License

[MIT](LICENSE)
