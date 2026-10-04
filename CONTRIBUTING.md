# Contributing

Thanks for helping. This guide covers setup, how the repo is laid out, and the conventions every change follows.
Start with [ARCHITECTURE.md](ARCHITECTURE.md) for the map of the code.

## Setup

You need **Go 1.22+**, **Node 20+** and **Git**. universal-ctags is optional; it powers `sym:` when installed.

```sh
npm install                  # TypeScript, ESLint, Prettier, Playwright for both workspaces
npx playwright install chromium
npm test                     # every test layer: scripts/test-all.sh
```

To try the extension, open `extension/` in VS Code and press **F5**. The extension finds the daemon at
`daemon/bin/unified-search-daemon`, which `npm test` builds. You can also build it with
`cd daemon && go build -o bin/unified-search-daemon ./cmd/unified-search-daemon`.

## Everyday commands

| Command | Does |
| --- | --- |
| `npm test` | All layers: generator check, formatting, Go vet + tests, extension tests, webview tests |
| `npm run lint` | ESLint over the TypeScript (type-aware) |
| `npm run format` | Prettier over TS, JS, JSON, CSS and YAML |
| `npm run gen` | Regenerate protocol types after editing `protocol/protocol.schema.json` |
| `cd daemon && go test ./...` | Daemon tests only |
| `cd daemon && golangci-lint run` | Go lint (config in `daemon/.golangci.yml`) |

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

Never edit `*.gen.ts` or `*_gen.go`. Change `protocol/protocol.schema.json`, then run `npm run gen`. CI runs
`node protocol/gen.mjs --check`.

## Tests

- Name tests after behaviour: `TestParse/unclosed_paren_offers_closing_fix`,
  `test("stale responses with an older seq are dropped")`.
- Go: table-driven subtests, failure messages in `Parse(%q) = %v, want %v` form, golden files in `testdata/`
  refreshed with `go test ./... -update`.
- **Spec coverage:** tag each test with the spec IDs it proves, e.g. `// @covers op:since diag:unclosed_paren` or
  `rpc:search/start` or `msg:query.changed`. A check fails CI when an operator, diagnostic code, RPC method, webview
  message, setting or mock has no test.
- Put each test in the cheapest layer that can prove the behaviour (see [the test plan](docs/dev/test-plan.md)).

## Docs

The docs follow [Diátaxis](https://diataxis.fr/). See [docs/README.md](docs/README.md) for where each kind goes.
Record any decision a future contributor might question as an [ADR](docs/adr/). Add user-visible changes to
`CHANGELOG.md` under **Unreleased**.
