#!/usr/bin/env bash
# Runs every test layer that works without VS Code. Exit code is non-zero
# on the first failure. Usage: scripts/test-all.sh
set -euo pipefail
cd "$(dirname "$0")/.."
step() { printf '\n== %s\n' "$*"; }

step "protocol: generated types are up to date"
node protocol/gen.mjs --check

step "daemon: go vet + go test"
(cd daemon && go vet ./... && go test -count=1 ./... && go build -o bin/unified-search-daemon ./cmd/unified-search-daemon)

step "extension: typecheck + node tests (real daemon)"
cd extension
if [ -d node_modules/@types/vscode ]; then npx tsc -p tsconfig.json --noEmit; else tsc -p tsconfig.offline.json --noEmit; fi
node build.mjs --tests
node --test "dist-test/*.test.js"
cd ..

step "webview: typecheck + Playwright contract tests"
cd webview
tsc -p tsconfig.json
node test/build-harness.mjs
node --test "test/*.test.mjs"
cd ..

printf '\nAll layers passed.\n'
