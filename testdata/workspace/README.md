# Fixture workspace

Three small repos whose contents match the design mockups ([docs/dev/mocks.md](../../docs/dev/mocks.md)), so a
search over them gives the results the mockups show:

| Query | Mockup | Expected |
| --- | --- | --- |
| `retry_policy` | 1 | 3 file names; 5 code matches in 3 files |
| `f:.*test\.py$ timeout` | 2 | 10 matches in 4 files, only paths ending `test.py` |
| `case:yes /Retry(Policy\|Config)/ lang:python` | 8 | 7 matches in 4 files; 1 match hidden by `case:yes` |
| `struct:"backoff(:[call], tries=:[n])"` | 40 | 3 matches in `shared-libs/http/webhooks.py`, over three lines, two and one; not the comment or the string that look like one |
| `retry owner:payments-core` | 42 | 2 file names; 10 code matches in 4 files, all in payments-api, whose `.github/CODEOWNERS` gives `src/payments/` and `tests/` to `@acme/payments-core`; 9 matches hidden by `owner:payments-core` |

`scripts/screenshotPanel.mjs` renders the panel for a query over this workspace. The daemon tests use it too.
