# Working notes for Claude

Read [ARCHITECTURE.md](ARCHITECTURE.md) (code map and invariants) and [CONTRIBUTING.md](CONTRIBUTING.md)
(conventions) first. They're the source of truth; this file only adds what an agent needs on top.

- Plans live in `docs/dev/`: read `implementation-plan.md` before changing behaviour, `test-plan.md` before adding
  tests. Record milestone evidence in `docs/dev/progress.md`.
- A milestone is done only when its exit gate is proven by tests that ran green (`npm test`) and the evidence is in
  `progress.md`. Show it to the user before starting the next milestone.
- Commits are small Conventional Commits with directory scopes. Each one must pass `scripts/test-all.sh` on its own.
  `git mv` stages immediately, so reset the index before staging a selective commit.
- Name files and identifiers for what they do, never for a milestone (no `m0search.go`); see CONTRIBUTING.md.
- Never edit `*.gen.ts` or `*_gen.go`. Change `protocol/protocol.schema.json`, then run `node protocol/gen.mjs`.
- Tag tests with `@covers <id>` for what they prove. Don't tag a test for something it doesn't actually assert.
- Offline sandbox: no npm or Go module downloads. Tools that exist: Go stdlib, system SQLite (FTS5), tsc, esbuild,
  Prettier, Playwright + Chromium, git, ripgrep. Use `extension/tsconfig.offline.json` when `@types/vscode` is missing.
