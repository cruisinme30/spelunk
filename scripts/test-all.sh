#!/usr/bin/env bash
# Runs every test layer that works without a VS Code window, fastest first,
# and stops at the first failure.
#
# Usage: scripts/test-all.sh        (or: npm test)
#
# Uses the workspace's own tools after `npm install`; falls back to tsc and
# prettier on PATH, and to tsconfig.offline.json when @types/vscode is missing.
set -euo pipefail
cd "$(dirname "$0")/.."
root=$PWD

step() { printf '\n== %s\n' "$*"; }
tool() { if [ -x "$root/node_modules/.bin/$1" ]; then echo "$root/node_modules/.bin/$1"; else echo "$1"; fi; }

step "protocol: generated types are up to date"
node protocol/gen.mjs --check

step "format: prettier --check"
if command -v "$(tool prettier)" >/dev/null; then "$(tool prettier)" --check . ; else echo "prettier not installed; skipped"; fi
(cd daemon && test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; })

step "daemon: go vet + go test"
(cd daemon && go vet ./... && go test -count=1 ./... && go build -o bin/unified-search-daemon ./cmd/unified-search-daemon)

step "extension: typecheck + node tests (real daemon)"
(
  cd extension
  if [ -d "$root/node_modules/@types/vscode" ] || [ -d node_modules/@types/vscode ]; then
    "$(tool tsc)" -p tsconfig.json
  else
    "$(tool tsc)" -p tsconfig.offline.json
  fi
  node build.mjs --tests
  node --test "dist-test/*.test.js"
)

step "webview: typecheck + Playwright contract tests"
(
  cd webview
  "$(tool tsc)" -p tsconfig.json
  node test/buildHarness.mjs
  node --test "test/*.test.mjs"
)

printf '\nAll layers passed.\n'
