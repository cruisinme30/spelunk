# 0006. Search Jupyter notebooks by their cells, and diff them through a textconv filter

- Status: Accepted
- Date: 2026-10-11

## Context

A Jupyter notebook (`.ipynb`) is JSON: each cell's source is a list of quoted, escaped lines, and its outputs (plots
as base64, printed text) are saved beside it. Searched as text, a notebook's results were JSON strings numbered by
JSON line, `symbol:` and `language:` never saw its code, a query could match inside a plot, opening a result showed the
JSON in the text editor, Replace could write invalid JSON (a replacement with a quote or a backslash), and most
notebooks with plots were over `index.maxFileSizeKB` and silently left out. Re-running a notebook changed dozens of
JSON lines in its diff.

## Options considered

1. **Leave notebooks as text.** Nothing to build; every problem above stays, including Replace corrupting files.
2. **Index each cell as its own document.** Exact, but every engine, result and ref assumes one doc per file, and
   ranking, facets and file names would count a notebook once per cell.
3. **Index a notebook as its cells' text**: each cell's source in turn, so a cell of n lines is n lines of the text,
   with the cells kept beside it. Every engine works unchanged on the text; a line maps to its cell by counting, which
   VS Code's notebook editor counts the same way, so opening and Replace can go through it.

For history: (a) diff the JSON as Git does; (b) read both blobs of every changed notebook and diff their cells in the
daemon, which needs a diff algorithm of our own; (c) give Git a textconv filter that prints the cells' text, so
`git log -p` and `git show` diff notebooks by their source with Git's own diff.

## Decision

Option 3, in `daemon/internal/notebook`, with text outputs after every cell's source when `index.notebookOutputs` is
on (so source lines number the same either way), and (c) for history, with the daemon's own executable as the filter.
It is set only on the daemon's Git commands, by `-c` options naming an attributes file that holds the user's global
attributes plus a line for `*.ipynb`; a repo's own `.gitattributes` still wins.

## Consequences

- Notebooks are searched, previewed, opened, replaced and diffed by their cells; the size limit counts only the cells'
  text, and a notebook file over 64 MB is never read.
- A file named `.ipynb` that doesn't parse as nbformat 4 is searched as text, as before.
- Git runs the daemon once per notebook blob it diffs, which makes reading history of notebook-heavy repos slower. If
  it shows, Git's `diff.<driver>.cachetextconv` could cache the text (it writes notes refs into the repo, so it isn't
  on now).
- Every test binary that reads history answers the filter's arguments first (`history.ServeNotebookText`), since Git
  runs the executable that asked for the diff.
- The shard and history store formats changed, so existing indexes are read again once.
