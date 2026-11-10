# Testing

Put each test in the cheapest layer that can prove the behaviour. Full detail:
[test plan](https://github.com/cruisinme30/spelunk/blob/main/docs/dev/test-plan.md).

| Layer | Proves | When |
| --- | --- | --- |
| L1 Unit | Parser, planner, diagnostics, fix-its, ranking | Affected develop changes; all main promotions |
| L2 Contract | Each side of each contract alone: engines vs. oracles, messages vs. the schema | Affected develop changes; all main promotions |
| L3 Daemon integration | The whole daemon over stdio: indexing, freshness, cancellation, crashes | Affected develop changes; all main promotions |
| L4 End to end | Every mock's behaviour in a real VS Code | Full implemented suite before main promotion |
| L5 Performance and soak | Budgets, memory, long runs | Nightly |
| L6 Release | Install and first run on clean machines | Each release candidate |

The table is a coverage plan; broader nightly soak and cross-platform targets are not all implemented.

## Integration policy

Feature branches merge into `develop` with merge commits, never squash. Draft PRs collect iterations without CI;
mark ready when code, tests, docs and the changelog are reviewable. Develop PRs run every static quality check and
the affected behavioral dependency closure. Shared or unknown paths run all layers. Promote coherent batches to
`main` only after the entire suite and real VS Code end-to-end tests pass.

Canonical wiki pages and images live in `docs/wiki/` and change in the same PR as the implementation.
`node scripts/checkWikiSync.mjs` always validates their content, links, screenshots and reference coverage offline;
missing canonical source fails the check. The GitHub wiki is a derived mirror, published only from merged
`develop` changes after checks. Mirror deployment is checked separately and never substitutes for content validation.

Every new changelog entry states **Issue → Example → Fix**. Always show a concrete example. Keep website status
explicit about local fixes, develop integration, and main release.

Release benchmarks require every named workload and at least 20 samples. They check response and total p95,
completion for no-hit searches, truncation, stable counts, build times, and a retained Go heap proxy. That proxy
is not measured daemon RSS or peak process memory.

## Oracles

- **ripgrep** checks working-tree results; **`git log -G`** checks history results.
- Fixture repos in [`testdata/workspace/`](https://github.com/cruisinme30/spelunk/blob/main/testdata/workspace) match
  the [[Screens]], and daemon tests run the mocks' queries.

Large-repository regressions also check lossless query repairs, C/C++ continuation contexts, declaration/body
distinctions, Unicode case folding, and replacement plans bound to complete source. Literal scans compare against
an independent oracle and verify that per-file cursors cannot leak between files. Report exact corpus revisions,
query families, match parity and incomplete runs alongside performance; a killed run is not a completed sample.

## The webview in Chromium

`webview/test` drives the real bundles in Chromium, in the page `webviewPage.ts` writes for VS Code and under its
content security policy, with a stand-in for VS Code's API bundled ahead of each page's code. Anything the policy
would block in the editor (an inline style or script, a resource from elsewhere) fails the test that caused it.

## Spec coverage

Tag each test with what it proves: `// @covers op:since diag:unclosed_paren`, `rpc:search/start`,
`screen:file-names-only`. `scripts/specCoverage.mjs` lists anything untested; `--strict` gates releases.

## Seeing the panel without VS Code

```sh
node scripts/screenshotPanel.mjs --query 'retry_policy' --out panel.png
```

Renders the real daemon, controller and webview in Chromium, to compare against the mocks.

---

**Next:** [[Screens]]: every mock, with the built panel beside it.
