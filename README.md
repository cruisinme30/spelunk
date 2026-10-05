# Spelunk

**One search box for file names, code and Git history, across every repo in your VS Code workspace.**

```text
author:jane f:.*test\.py$ (timeout OR retry) -f:vendor/ since:6m
```

That query finds Jane's commits from the last six months that changed a Python test outside `vendor/` and added or
removed `timeout` or `retry`. Drop `author:` and the same box searches current files instead.

![The search panel showing code matches for retry_policy, with a preview](docs/dev/proof/search-panel-results-and-preview.png)

> **Status: released on GitHub, not yet on the Marketplace.** Download the `.vsix` for your platform from the
> [latest release](https://github.com/cruisinme30/spelunk/releases/latest) and run **Extensions: Install from VSIX…**
> in VS Code.

## What it does

- **One query language.** Combine words, `"phrases"`, `/regex/`, `AND`, `OR`, `( )` and `-exclusions` with
  operators. Each has a full name and a one-letter short one: `file:` / `f:`, `repo:` / `r:`, `language:` / `l:`,
  `type:` / `t:`, `symbol:` / `s:`, `author:` / `a:`, `message:` / `m:`, `since:` / `d:`, `case:` / `c:`,
  `count:` / `n:`.
- **Files or commits from the same box.** A query searches current files, unless it has `author:`, `message:` or
  `type:commit`; then each result is a commit, and words match the lines it added or removed or its message.
- **Results as you type,** from local indexes that keep up with saves within a second and new commits within seconds.
- **Helpful when you're wrong:** errors with one-key fixes, warnings for likely slips such as `daemon|search`,
  completions for operators and their values, and notes like "3 commits hidden by `-file:vendor/`".
- **Keyboard first:** ⌘P or ⇧⌘F to open, ↵ to open at the match, ⌘↵ to open to the side, F4 to step through results.
- Works the same in desktop VS Code and code-server. Nothing leaves your machine.

## Documentation

The [wiki](https://github.com/cruisinme30/spelunk/wiki) has the user guide:
[Getting Started](https://github.com/cruisinme30/spelunk/wiki/Getting-Started),
[Query Language](https://github.com/cruisinme30/spelunk/wiki/Query-Language),
[Using the Panel](https://github.com/cruisinme30/spelunk/wiki/Using-the-Panel),
[How Searches Behave](https://github.com/cruisinme30/spelunk/wiki/How-Searches-Behave) and
[Settings and Commands](https://github.com/cruisinme30/spelunk/wiki/Settings-and-Commands). It also explains
how the pieces fit together, with diagrams and every design mock.

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

## Development

```sh
npm install && npx playwright install chromium
npm test        # every test layer, including the Go daemon
```

Then open the repo in VS Code and press F5 to run the extension on the sample repos, or install it in your own VS Code:

```sh
npm run package && code --install-extension out/spelunk-*.vsix
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for conventions, and [docs/](docs/README.md) for everything else.

## License

[MIT](LICENSE)
