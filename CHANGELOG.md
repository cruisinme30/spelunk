# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Search daemon skeleton: JSON-RPC 2.0 over stdio, initialize/shutdown lifecycle, crash restarts.
- `protocol/` JSON Schema with generated TypeScript and Go types.
- VS Code extension host: daemon supervisor, search controller and search panel.
- Search panel webview: query box with Aa / .* toggles, completions, fix-its, chips, results and preview.
