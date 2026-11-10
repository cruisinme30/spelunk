# Contributing

Thanks for helping. This guide covers setup, how the repo is laid out, and the conventions every change follows.
Start with the [wiki](https://github.com/cruisinme30/spelunk/wiki) for the map of the code.

## Setup

You need **Go 1.25+**, **Node 22+**, **Git** and [golangci-lint](https://golangci-lint.run/) v2.

```sh
npm install                  # TypeScript, Prettier, Playwright and the Node linters
npx playwright install chromium
npm test                     # every linter and test layer: scripts/test-all.sh
npm run presubmit            # everything CI runs, with CI's tool versions; the pre-push hook runs this
```

`npm install` also points git at `.githooks/`, whose pre-push hook validates the commit being pushed
(commit or stash other changes first). It installs CI's pinned golangci-lint into `.tools/` and uses the Go version
in `daemon/go.mod`. Feature and `develop` pushes run all static checks and affected behavioral layers against
`origin/develop`; without that base, the hook runs the full suite. Pushes to `main` and release tags also run E2E;
release tags add packaging checks. Remote CI remains authoritative for its environment and the exact PR merge commit.

To try the extension, open the repo in VS Code and press **F5**. It builds the daemon and the extension, then opens
a second window running the extension on the sample repos in `testdata/workspace/`. From a terminal, the same is:

```sh
go -C daemon build -o bin/spelunk-daemon ./cmd/spelunk-daemon && npm run build
code --extensionDevelopmentPath="$PWD/extension" "$PWD/testdata/workspace"
```

To install it in your everyday VS Code, package it and install the `.vsix`:

```sh
npm run package                                   # this machine; or: npm run package -- --target linux-x64
code --install-extension out/spelunk-0.1.0-darwin-arm64.vsix
```

`npm run package` cross-compiles the daemon for the target platform, so one machine can build every platform's
package.

## Everyday commands

| Command | Does |
| --- | --- |
| `npm test` | Generator check, formatting, linters, Go, extension and webview tests |
| `npm run presubmit` | Everything CI runs, with CI's tool versions, plus the e2e tests and the wiki check |
| `npm run lint` | Every linter (below) |
| `npm run package` | A `.vsix` for this platform in `out/` (`-- --target <platform>` for another) |
| `node scripts/e2e.mjs` | The end-to-end tests in a real VS Code window of the version it pins, which it downloads once into `.vscode-test/` |
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
- **One logical change per commit**, with relevant tests and all static quality checks passing before integration.
  A schema change and its regenerated output are one change. Full validation gates promotion to `main`.

## Pull requests

Short-lived branches off `develop`, small PRs (aim for under 400 changed lines, not counting generated code), one
concern each. PRs use **Create a merge commit** so their commits stay intact. PR titles and commit subjects use
Conventional Commits. Do not squash or rebase when merging. The template asks for test evidence.

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
2. Push a tag `v<version>`. The Release workflow (`.github/workflows/release.yml`) runs only then. It runs CI on the
   tag, checks the tag matches, requires a test for every spec id (`specCoverage.mjs --strict`), benchmarks searches
   and index builds on Prometheus against the budgets of the implementation plan (`scripts/checkBudgets.mjs`),
   packages a `.vsix` per platform, and attaches them to a GitHub release.
3. With the repository secrets `VSCE_PAT` (VS Code Marketplace) and `OVSX_PAT` (Open VSX) set, it also publishes
   them; without them, those steps are skipped. Nothing is published unless every check passed, and only those two
   steps see the tokens. A run that failed part way can be re-run: it uploads the packages to the release it made.

## Keeping code, wiki and mocks in sync

The code, the [wiki](https://github.com/cruisinme30/spelunk/wiki) and the
[mocks](docs/dev/mocks.md) describe one product. A change to any of them updates the others in the same piece of
work, whichever side it starts from:

| When this changes | Also update |
| --- | --- |
| Behaviour, architecture, protocol, an invariant, an operator, a diagnostic, a setting or a command | The wiki page that describes it (an operator's card on its reference page and its row on Query Language, a diagnostic's entry on the Error Index, a setting or command on Settings and Commands), and the plan in `docs/dev/` if it changes what's planned |
| A wiki page | The code (and its tests) to match, or mark the page's text as planned |
| What a screen looks like | Its mock on the design canvas, the wiki's `images/mocks/` export and `docs/dev/proof/` screenshot |
| A mock, or a screen added or removed | The code and its `@covers screen:` tests, the `docs/dev/mocks.md` row, the screen's block on the wiki page that documents it, and its row on the wiki's Screens page |

The canonical wiki source is `docs/wiki/`, including its `images/` assets. Commit code, tests, wiki source and mocks
in the same PR. Do not edit the deployed wiki directly. `scripts/checkWikiSync.mjs` is mandatory in both CI and
presubmit; it needs no wiki clone or network and fails if the source is missing. It checks screen numbers, ids,
milestones, screen blocks, images, canvas links, built screenshots, page links and headings, the sidebar, and
operator/diagnostic/setting/command coverage. `specCoverage.mjs` reads screen ids from `docs/dev/mocks.md`, so a new
mock needs a test.

The `Publish wiki` workflow runs only on `develop` pushes that change wiki source or publication tooling. A
read-only job validates that exact commit and publication regressions; a separate job gets the ephemeral `contents:
write` token to publish the existing `spelunk.wiki.git` master branch. It copies regular Markdown pages and PNG
images, rejects symlinks and metadata, checks that `develop` still has the same wiki and publication-tool
identities, serializes runs, and uses a normal push without rewriting history. It does not run PR code with write
credentials or repeat the application suite. Main promotion does not republish documentation.

After merging, verify the workflow and published content. Publication is a deployment step: a failed deployment
leaves the tested source intact and must be reported, then rerun after the cause is fixed. A run superseded by newer
wiki or publication-tool content is skipped; the newest relevant push supplies the mirror. Unrelated code commits do
not strand a queued publication. To compare a freshly fetched wiki clone explicitly, run `node
scripts/checkWikiDeployment.mjs /path/to/spelunk.wiki`. This optional deployment check never substitutes for source
validation. The first hosted publication must verify this repository's token policy; no personal token is required
by the design and no authentication fallback silently broadens access.

## Docs

The docs follow [Diátaxis](https://diataxis.fr/). See [docs/README.md](docs/README.md) for where each kind goes.
Record any decision a future contributor might question as an [ADR](docs/adr/). Add user-visible changes to
`CHANGELOG.md` under **Unreleased**.

## Development and promotion

Work on short-lived feature branches and merge them into `develop` with a merge commit. Keep exploratory work
local or in a draft PR; mark the PR ready when the change, tests, docs, and changelog form a reviewable batch.
Draft PRs do not consume CI runs. CI runs when the PR becomes ready and whenever that ready PR changes.

- **Into develop:** run all static quality checks and the behavioral dependency closure selected by
  `scripts/test-changed.sh --base BASE --head HEAD`. A daemon change tests the daemon and both consumers; an extension
  change
  tests extension and webview; a webview-only change tests the webview. Shared configuration, protocol, fixtures,
  unknown paths, and the test infrastructure fall back to the full suite.
- **Into main:** promote a coherent batch of completed iterations through a `develop` to `main` PR. Run the
  entire suite and real VS Code end-to-end tests. Do not promote merely because an arbitrary commit count passed.
  Promote when the batch has a clear user outcome, current docs, passing checks, and no unresolved blocking defects.
- Feature and develop pushes do not repeat the application test suite. A develop push changing wiki source or
  publishing tooling runs only documentation validation and deployment. Main pushes retain a full safety run,
  including direct changes. Main promotions are intentionally less frequent.
- Never weaken lint rules, skip failing assertions, or bypass checks to make a merge green. Report an environmental
  blocker accurately and rely on a successful equivalent CI run before merging if the local environment cannot run it.
- Before fixing a bug, identify the failing invariant and whether it belongs in shared infrastructure. Prefer a
  coherent shared change over query-specific special cases. Add regression tests for neighboring behavior as well
  as the reproducer, and review memory, cancellation, concurrency, and persistence implications.

## Changelog and website maintenance

Every new changelog entry uses **Issue → Example → Fix**. Always include a concrete example that makes the issue
understandable. Do not publish implementation-only bullet points or say a change is released while it is only on
`develop`. Keep the website's architecture, benchmark methodology, fix status, and changelog aligned with the code.
Benchmark improvements must identify the baseline, corpus, measurement method, and relevant limitations.

Maintain one note per improvement in `docs/dev/improvements/`, covering the issue and a concrete example, root
cause, change and mechanism, actual tests and benchmark results with links to raw evidence and provenance,
limitations and tradeoffs, and current status. Distinguish micro benchmarks from end-to-end measurements; never
invent results or imply that a local test proves hosted deployment. Update standalone reports, slide artifacts and
the website with the final evidence, including superseded measurements and unresolved deployment steps.

Example:

- **Issue:** a string-only replacement could change an executable expression.
  **Example:** in JavaScript, `dangerCall in:strings` selected the call inside
  `` `answer ${dangerCall()}` ``.
  **Fix:** classify interpolation expressions as code and verify replacement plans preserve them.
