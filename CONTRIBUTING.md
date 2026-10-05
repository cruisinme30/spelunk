# Contributing

Thanks for helping. This guide covers setup, how the repo is laid out, and the conventions every change follows.
Start with the [wiki](https://github.com/cruisinme30/unified-search/wiki) for the map of the code.

## Setup

You need **Go 1.22+**, **Node 20+**, **Git** and [golangci-lint](https://golangci-lint.run/) v2.

```sh
npm install                  # TypeScript, Prettier, Playwright and the Node linters
npx playwright install chromium
npm test                     # every linter and test layer: scripts/test-all.sh
```

To try the extension, open the repo in VS Code and press **F5**. It builds the daemon and the extension, then opens
a second window running the extension on the sample repos in `testdata/workspace/`. From a terminal, the same is:

```sh
go -C daemon build -o bin/unified-search-daemon ./cmd/unified-search-daemon && npm run build
code --extensionDevelopmentPath="$PWD/extension" "$PWD/testdata/workspace"
```

To install it in your everyday VS Code, package it and install the `.vsix`:

```sh
npm run package                                   # this machine; or: npm run package -- --target linux-x64
code --install-extension out/unified-search-0.1.0-darwin-arm64.vsix
```

`npm run package` cross-compiles the daemon for the target platform, so one machine can build every platform's
package.

## Everyday commands

| Command | Does |
| --- | --- |
| `npm test` | Everything CI runs: generator check, formatting, linters, Go, extension and webview tests |
| `npm run lint` | Every linter (below) |
| `npm run package` | A `.vsix` for this platform in `out/` (`-- --target <platform>` for another) |
| `node scripts/e2e.mjs` | The end-to-end tests in a real VS Code window, which it downloads once into `.vscode-test/` |
| `npm run format` | Prettier over TS, JS, JSON, CSS and YAML |
| `npm run gen` | Regenerate protocol types after editing `protocol/protocol.schema.json` |
| `cd daemon && go test ./...` | Daemon tests only |
| `cd daemon && golangci-lint run` | Go lint (config in `daemon/.golangci.yml`) |

## Linters

Every linter runs in CI with zero warnings allowed, and locally as part of `npm test` once `npm install` has
installed it. Fix what a linter reports rather than silencing it; a suppression needs a comment saying why.

| Tool | Checks | Config |
| --- | --- | --- |
| [golangci-lint](https://golangci-lint.run/) | Go: vet, staticcheck, gosec, revive, complexity and size limits, doc comments | `daemon/.golangci.yml` |
| [ESLint](https://eslint.org/) | TypeScript: typescript-eslint strict and stylistic (type-aware), unicorn, JSDoc on exports, size limits | `eslint.config.mjs` |
| [Stylelint](https://stylelint.io/) | CSS: the standard config | `stylelint.config.mjs` |
| [markdownlint](https://github.com/DavidAnson/markdownlint-cli2) | Markdown structure and line length | `.markdownlint-cli2.jsonc` |
| [cspell](https://cspell.org/) | Spelling in code, comments and docs | `cspell.config.yaml` |
| [knip](https://knip.dev/) | Unused files, exports and dependencies | `knip.jsonc` |
| [Prettier](https://prettier.io/) and gofmt | Formatting | `.prettierrc.json` |

## Commits

We use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
feat(daemon): add since: to the planner

Body: what changed and why, wrapped at 72 columns. Not how; the diff
shows that.

Fixes #12
```

- **Types:** `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`, `chore`, `style`.
- **Scopes** are top-level directories: `daemon`, `extension`, `webview`, `protocol`, `testdata`, `docs`,
  `scripts`, `repo`. Leave the scope out when a change spans several.
- **Subject:** imperative mood ("add", not "added"), lowercase after the colon, no full stop, 72 characters at most
  (50 is better).
- **Breaking protocol change:** `feat(protocol)!: …` plus a `BREAKING CHANGE:` footer.
- **One logical change per commit**, and every commit passes `npm test`. A schema change and its regenerated
  output are one change.

## Pull requests

Short-lived branches off `main`, small PRs (aim for under 400 changed lines, not counting generated code), one
concern each. PRs are squash-merged, so **the PR title becomes the commit subject** and must be a Conventional
Commit. The template asks for test evidence.

## Code conventions

### Naming, everywhere

Name files, types, functions and tests for **what they do**, in words that stay true as the code grows: `search.go`,
`lineRef`, `TestSearchPreviewAndOpenRoundTrip`. Never name them after the plan stage, ticket or person that introduced
them (`m0search.go`, `handleM1`, `newJaneParser`), and don't use version or sequence suffixes (`parser2.go`). A comment
can say what is temporary and what replaces it; the name shouldn't.

### Go (`daemon/`)

Follow [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) and the
[Google Go Style Guide](https://google.github.io/styleguide/go/). In practice:

- `gofmt` everything. There's no line limit; refactor long lines instead of wrapping them.
- Packages live under `internal/` and have short, lowercase names that say what they do (`query`, `trigram`), never
  `util` or `common`. Each package has a `doc.go` with a `// Package x …` comment.
- Don't stutter: `query.Parse`, not `query.ParseQuery`. Initialisms keep their case: `RepoID`, `SHA`, `URL`.
- Every exported identifier has a doc comment that starts with its name. Say what a reader can't assume:
  concurrency, ownership, errors.
- Error strings are lowercase with no final punctuation. Wrap with `%w` at the end: `fmt.Errorf("open shard: %w", err)`.
- Receivers are one or two letters, the same on every method of the type.
- Define an interface in the package that uses it, not the one that implements it.

### TypeScript (`extension/`, `webview/`)

Follow the [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) and
[VS Code's coding guidelines](https://github.com/microsoft/vscode/wiki/Coding-Guidelines):

- File names are camelCase (`commitDocuments.ts`), as in VS Code. Generated files end in `.gen.ts`.
- Use named exports only, never `export default`. Use `import type` for type-only imports (`verbatimModuleSyntax`
  enforces it).
- Use `interface` for object shapes and `type` for unions. Prefer string-literal unions to enums, and never write
  `const enum`.
- Use whole words in names (`daemon`, not `d`). The exceptions are loop indices and conventional short names.
- Every file starts with a comment saying what it is for.
- Webview renderers take state and callbacks and write DOM. Only `SearchPanel` changes state, and only `host.ts`
  talks to VS Code.

### Generated code

Never edit `*.gen.ts` or `*_gen.go`. Change `protocol/protocol.schema.json`, then run `npm run gen`. `npm test`
(and so CI) fails when the generated files are out of date.

## Tests

- Name tests after behaviour: `TestParse/unclosed_paren_offers_closing_fix`,
  `test("stale responses with an older seq are dropped")`.
- Go: table-driven subtests, failure messages in `Parse(%q) = %v, want %v` form, golden files in `testdata/`
  refreshed with `go test ./... -update`.
- **Spec coverage:** tag each test with the spec IDs it proves, e.g. `// @covers op:since diag:unclosed_paren` or
  `rpc:search/start` or `msg:query.changed`. `scripts/specCoverage.mjs` lists every operator, diagnostic code, RPC
  method, webview message, setting, command, screen and failure mode with no test. It fails on unknown ids now, and
  `--strict` (any gap fails) is the release gate.
- Put each test in the cheapest layer that can prove the behaviour (see [the test plan](docs/dev/test-plan.md)).

## Releasing

1. Move the **Unreleased** entries of `CHANGELOG.md` under a new version heading, and set the same version in
   `extension/package.json`.
2. Push a tag `v<version>`. The Release workflow (`.github/workflows/release.yml`) runs only then. It checks the
   tag matches, requires a test for every spec id (`specCoverage.mjs --strict`), benchmarks searches and index
   builds on Prometheus against the budgets of the implementation plan (`scripts/checkBudgets.mjs`), packages a
   `.vsix` per platform, and attaches them to a GitHub release.
3. With the repository secrets `VSCE_PAT` (VS Code Marketplace) and `OVSX_PAT` (Open VSX) set, it also publishes
   them; without them, those steps are skipped. Nothing is published unless every check passed.

## Docs

The docs follow [Diátaxis](https://diataxis.fr/). See [docs/README.md](docs/README.md) for where each kind goes.
Record any decision a future contributor might question as an [ADR](docs/adr/). Add user-visible changes to
`CHANGELOG.md` under **Unreleased**.
