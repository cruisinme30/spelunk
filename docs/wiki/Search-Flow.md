# Search Flow

<!-- Mermaid sequence-diagram keyword. -->
<!-- cspell:words autonumber -->

When you type, the panel waits briefly for a pause, then sends your query through the extension to the daemon.
The daemon checks the query and streams results back in batches. A new query cancels the previous search.

For the component map, start with [[Architecture Overview]]. The diagrams below show the messages used by
contributors working on search, preview or editor integration.

## From typing to results

```mermaid
sequenceDiagram
  autonumber
  participant W as Webview
  participant H as Extension host
  participant D as Daemon
  participant E as Engine

  W->>W: debounce (typingDelayMs)
  W->>H: query.changed {text, cursor, seq}
  H->>D: query/parse {text, cursor, openFiles}
  D-->>H: ParsedQuery + completions
  H-->>W: parse.result {seq}
  Note over W: chips, errors, autocomplete
  H->>D: $/cancelRequest (previous search)
  H->>D: search/start {searchId, text, openFiles}
  D->>D: re-parse text, plan
  D->>E: run Plan on published snapshot
  loop up to 200 items per batch
    E-->>D: ResultItems
    D-->>H: search/batch
    H-->>W: search.batch {seq}
  end
  D-->>H: search/start result {total, truncated, ms}
  H-->>W: search.done {seq}
```

## What keeps it correct

- **Stale responses are dropped.** Every message carries the keystroke's `seq`; the webview ignores anything older
  than the newest it sent.
- **Every keystroke cancels the last search.** Daemon loops check `ctx.Done()`.
- **The daemon re-parses.** `search/start` takes text, not an AST, so the daemon never trusts a parse it didn't make.
- **Searches read snapshots.** The indexer publishes complete indexes in one step, so a search never sees a
  half-written one.
- **Pages never overlap.** In best-match order the engine sorts candidate files by what it knows before reading
  them (definitions, file names, open and recently changed files, tests, vendored and generated code), then scans them
  in that order,
  so the Nth result is the same whichever page asks for it. How well lines match only reorders files within a page,
  which is held until it is full or 50 ms have passed.

## Selecting and opening

```mermaid
sequenceDiagram
  participant W as Webview
  participant H as Extension host
  participant D as Daemon
  W->>H: result.select {ref}
  H->>D: preview/get {ref}
  D-->>H: Preview
  H-->>W: preview.result
  W->>H: result.open {ref, where, position?}
  H->>D: open/resolve {ref}
  D-->>H: path + line, or repo + sha
  H->>H: open editor or commit diff
```

`ref` is opaque outside the daemon: hosts and webviews pass it back unchanged.

---

**Next:** [[Indexing]]: how the indexes are built and kept fresh.
