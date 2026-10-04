# Fixture workspace

Three small repos whose contents match the design mockups ([docs/dev/mocks.md](../../docs/dev/mocks.md)), so a
search over them gives the results the mockups show:

| Query | Mockup | Expected |
| --- | --- | --- |
| `retry_policy` | 1 | 3 file names; 5 code matches in 3 files |
| `f:.*test\.py$ timeout` | 2 | 10 matches in 4 files, only paths ending `test.py` |
| `case:yes /Retry(Policy\|Config)/ lang:python` | 8 | 7 matches in 4 files; 1 match hidden by `case:yes` |

`scripts/screenshotPanel.mjs` renders the panel for a query over this workspace. The daemon tests use it too.
