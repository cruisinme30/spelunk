# Working notes for Claude

Read the versioned [wiki source](docs/wiki/Home.md) (user guide, query reference, code map, rules and mocks)
and [CONTRIBUTING.md](CONTRIBUTING.md) (conventions) first. The GitHub wiki is its deployed mirror.
They're the source of truth; this file only adds what an agent needs on top.

- Plans live in `docs/dev/`: read `implementation-plan.md` before changing behaviour, `test-plan.md` before adding
  tests. Record milestone evidence in `docs/dev/progress.md`.
- A milestone is done only when its relevant tests and all static checks are green and evidence is recorded in
  `progress.md`. Promotion to main additionally requires the entire suite and end-to-end tests.
- Commits are small Conventional Commits with directory scopes. Each integration must pass its relevant behavioral tests
  and all static checks. `git mv` stages immediately, so reset the index before staging a selective commit.
- Name files and identifiers for what they do, never for a milestone (no `m0search.go`); see CONTRIBUTING.md.
- Code, wiki and mocks change together, in either direction: a change to one updates the others in the same piece of
  work (see "Keeping code, wiki and mocks in sync" in CONTRIBUTING.md). Never leave one describing what another
  doesn't do. Edit canonical `docs/wiki` in the code PR; `checkWikiSync.mjs` validates it in every CI run.
  A separate develop-push workflow publishes the mirror after source validation. Verify deployment after merging;
  do not treat unpublished source as live documentation. If a required external artifact cannot be updated, report it.
- Use feature branches targeting `develop`; promote batches to `main` only after full validation.
  Run `npm run presubmit -- --base origin/develop` for the develop gate and `npm run presubmit` for full validation.
  Never use `--no-verify` to bypass a failing check. An environment-blocked local test must be disclosed and pass
  in equivalent CI before merging. See CONTRIBUTING.md for the complete policy.
- Never edit `*.gen.ts` or `*_gen.go`. Change `protocol/protocol.schema.json`, then run `node protocol/gen.mjs`.
- Tag tests with `@covers <id>` for what they prove. Don't tag a test for something it doesn't actually assert.
- Offline sandbox: no npm or Go module downloads. Tools that exist: Go stdlib, system SQLite (FTS5), tsc, esbuild,
  Prettier, Playwright + Chromium, git, ripgrep. Use `extension/tsconfig.offline.json` when `@types/vscode` is missing.

- Every new changelog entry must state Issue, Example, and Fix. Maintain a note per improvement in
  `docs/dev/improvements/`: issue and concrete example, root cause, change and mechanism, actual tests and benchmarks
  with raw evidence provenance, limitations and tradeoffs, and current status. Keep standalone reports, slides and
  the website aligned with final results. Distinguish micro benchmarks from end-to-end measurements; never invent
  performance data.
