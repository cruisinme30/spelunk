# Architecture decision records

One file per decision that a future contributor might question: `NNNN-title-with-dashes.md`, numbered in order and
never reused. When a decision changes, add a new ADR and mark the old one **Superseded by NNNN**; don't edit history.
Start from [template.md](template.md) (a short form of [MADR](https://adr.github.io/madr/)).

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](0001-go-daemon-over-stdio.md) | Searching runs in a Go daemon spoken to over JSON-RPC on stdio | Accepted |
| [0002](0002-json-schema-as-protocol-source.md) | JSON Schema is the single source for cross-process types | Accepted |
| [0003](0003-in-house-trigram-index.md) | The working-tree index is an in-house trigram index, not Zoekt | Accepted |
| [0004](0004-in-house-history-index.md) | The history index reuses the trigram package instead of SQLite FTS5 | Accepted |
