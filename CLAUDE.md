# Working notes for Claude

Read the [wiki](https://github.com/cruisinme30/spelunk/wiki) (code map, invariants and mocks; clone
`spelunk.wiki.git` to read it locally) and [CONTRIBUTING.md](CONTRIBUTING.md) (conventions) first.
They're the source of truth; this file only adds what an agent needs on top.

- Plans live in `docs/dev/`: read `implementation-plan.md` before changing behaviour, `test-plan.md` before adding
  tests. Record milestone evidence in `docs/dev/progress.md`.
- A milestone is done only when its exit gate is proven by tests that ran green (`npm test`) and the evidence is in
  `progress.md`. Show it to the user before starting the next milestone.
- Commits are small Conventional Commits with directory scopes. Each one must pass `scripts/test-all.sh` on its own.
  `git mv` stages immediately, so reset the index before staging a selective commit.
- Name files and identifiers for what they do, never for a milestone (no `m0search.go`); see CONTRIBUTING.md.
- Code, wiki and mocks change together, in either direction: a change to one updates the others in the same piece of
  work (see "Keeping code, wiki and mocks in sync" in CONTRIBUTING.md). Never leave one describing what another
  doesn't do; if you can't update one (the design canvas, say), stop and tell the user what's out of date.
- Before calling work done, `npm run presubmit` must pass: it runs everything the GitHub workflows run, with CI's tool
  versions, plus the wiki check. The pre-push hook runs it too; never push with `--no-verify`.
- Never edit `*.gen.ts` or `*_gen.go`. Change `protocol/protocol.schema.json`, then run `node protocol/gen.mjs`.
- Tag tests with `@covers <id>` for what they prove. Don't tag a test for something it doesn't actually assert.
- Offline sandbox: no npm or Go module downloads. Tools that exist: Go stdlib, system SQLite (FTS5), tsc, esbuild,
  Prettier, Playwright + Chromium, git, ripgrep. Use `extension/tsconfig.offline.json` when `@types/vscode` is missing.
