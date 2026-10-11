# Notebook fixture

One repo, ml-research, with a Jupyter notebook and a Python file, for the notebook screens (mock 60 in
[docs/dev/mocks.md](../../docs/dev/mocks.md)). `notebooks/churn_model.ipynb` has markdown and code cells, a cell that
printed text and one that drew a small plot.

| Query | Expected |
| --- | --- |
| `retry_policy` | 7 code matches in 2 files: 5 lines in 4 cells of the notebook, under their headings, never the printed line; 2 lines of `src/features.py` |
| `sym:load_events` | The function in cell 3 |

`node scripts/screenshotPanel.mjs --workspace "$PWD/testdata/notebooks" --query 'retry_policy' --select 3 --height 900`
draws the panel for it, from the repo root (the folder must be absolute).
