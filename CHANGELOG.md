# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Search daemon skeleton: JSON-RPC 2.0 over stdio, initialize/shutdown lifecycle, crash restarts.
- `protocol/` JSON Schema with generated TypeScript and Go types.
- VS Code extension host: daemon supervisor, search controller and search panel.
- Search panel webview: query box with Aa / .* toggles, completions, fix-its, chips, results and preview.
- Query language: parser, diagnostics with fix-its, completions and planner for every operator.
- Working-tree search over a trigram index: file names and code, `f:`, `case:`, `/regex/`, `lang:`, `repo:`,
  `type:`, `count:` with Load more, AND / OR / NOT, and "N hidden by …" notes with an undo.
- Background indexing per workspace root, saved between sessions, with `index/status`, `index/rebuild` and progress.
- Previews highlight every term of the query that produced the result.
- Operator suggestions with descriptions and example values; results stay on the finished words while a word is
  being completed, and Enter or Esc searches it as typed.
- A repo menu that scopes the query to one repo, a "Show code too" undo for `type:file`, and "Ignore case" for
  `case:yes`.
- The help page: every operator with an example to try, worked examples, keys and settings.

### Fixed

- A reopened search panel no longer ignores the queries typed into it.
