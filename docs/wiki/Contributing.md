# Contributing

Make your first change, from clone to a passing presubmit. The full guide, with every convention, is
[CONTRIBUTING.md](https://github.com/cruisinme30/spelunk/blob/main/CONTRIBUTING.md).

## 1. Set up

You need **Go 1.25+**, **Node 22+**, **Git** and golangci-lint v2.

```sh
npm install
npx playwright install chromium
npm test
```

`npm install` also installs a pre-push hook that runs the presubmit.

## 2. Run it

Press **F5** in VS Code. It builds the daemon and the extension and opens a second window on the sample repos in
`testdata/workspace/`.

## 3. Find where the change goes

[[Architecture Overview]] says which process owns what; the code map ([[Daemon]], [[Extension Host]], [[Webview]])
says which file. The [[Invariants]] are the rules a change must not break.

## 4. Test it in the cheapest layer

Put each test in the cheapest layer that can prove the behaviour, and tag it with what it proves:
`// @covers op:since diag:unclosed_paren`. See [[Testing]].

## 5. Keep code, wiki and mocks in sync

| Change | Also update |
| --- | --- |
| A cross-process message | [[Protocol]] schema first |
| A decision someone might question | A new ADR ([[Decisions]]) |
| An operator | Its card on the reference page, and its row on [[Query Language]] |
| A diagnostic | Its entry on the [Error index](Error-Index) |
| A setting or command | [[Settings and Commands]] |
| Anything user-visible | `CHANGELOG.md` under Unreleased |
| A screen's behaviour | Its `@covers screen:…` tests, and its screen block on the page that documents it ([[Screens]]) |

Edit the canonical `docs/wiki/` pages and images in the code repository, in the same PR as the implementation.
The mandatory offline `checkWikiSync.mjs` check validates the content against code, mocks and screenshots.
After a relevant `develop` push, a separate workflow validates the exact source commit and publishes its mirror
here using an ephemeral Actions token. It never force-pushes or runs PR code with write credentials. Verify that
publication succeeded after merging; until then the versioned source is reviewed, but this mirror may be older.
See the repository's CONTRIBUTING guide for the publication and recovery policy.

Keep a note per improvement in `docs/dev/improvements/`: issue and example, root cause, change, tests and benchmark
results with raw provenance, limitations, tradeoffs and status. Update standalone reports and the website with final
results; distinguish micro benchmarks from end-to-end measurements.

## 6. Check it

| Command | Does |
| --- | --- |
| `npm test` | Everything CI runs |
| `npm run presubmit` | Everything CI runs with CI's tool versions, plus the e2e tests; the pre-push hook runs it |
| `npm run lint` | Every linter, zero warnings allowed |
| `npm run gen` | Regenerate protocol types after editing the schema |
| `node scripts/e2e.mjs` | End-to-end tests in a real VS Code, of the version the script pins |
| `npm run package` | A `.vsix` for this platform in `out/` |
| `cd daemon && go test ./...` | Daemon tests only |

---

**Next:** [[Testing]]: the test layers and oracles.
