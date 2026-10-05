#!/usr/bin/env bash
# Runs every test layer that works without a VS Code window and stops at the
# first failure: generated protocol types, formatting, linters, the daemon
# (which also builds the binary the extension tests run), the extension host,
# the webview in Chromium, and finally the spec-coverage report.
#
# Usage: scripts/test-all.sh        (or: npm test)
#
# Uses the workspace's own tools after `npm install`; falls back to tsc and
# prettier on PATH, and to tsconfig.offline.json when @types/vscode is missing.
set -euo pipefail
cd "$(dirname "$0")/.."
root=$PWD

# Prints a step heading.
step() { printf '\n== %s\n' "$*"; }
# The workspace's copy of a Node tool if `npm install` put one there, else the one on PATH.
tool() { if [ -x "$root/node_modules/.bin/$1" ]; then echo "$root/node_modules/.bin/$1"; else echo "$1"; fi; }

step "protocol: generated types are up to date"
node protocol/gen.mjs --check

step "format: prettier --check, gofmt"
prettier=$(tool prettier)
if command -v "$prettier" >/dev/null; then
  "$prettier" --check .
else
  echo "prettier not installed; skipped"
fi
(cd daemon && test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; })

step "lint: golangci-lint, ESLint, Stylelint, markdownlint, cspell, knip"
# A linter that isn't installed is skipped locally, but fails the run in CI
# (where the CI variable is set), so CI always runs every one of them.
missing() {
  if [ -n "${CI:-}" ]; then
    echo "$1 is not installed" >&2
    exit 1
  fi
  echo "$1 not installed; skipped (npm install adds the Node linters)"
}
# Linters whose configs load plugins run only from the workspace's node_modules.
workspace_lint() {
  local bin="$root/node_modules/.bin/$1"
  shift
  if [ -x "$bin" ]; then "$bin" "$@"; else missing "$(basename "$bin")"; fi
}
if command -v golangci-lint >/dev/null; then (cd daemon && golangci-lint run ./...); else missing golangci-lint; fi
workspace_lint eslint --max-warnings 0 .
workspace_lint stylelint "webview/src/**/*.css"
markdownlint=$(tool markdownlint-cli2)
if command -v "$markdownlint" >/dev/null; then "$markdownlint"; else missing markdownlint-cli2; fi
workspace_lint cspell --no-progress --gitignore .
workspace_lint knip

step "daemon: go vet, go test, build the binary"
(cd daemon && go vet ./... && go test -count=1 ./... && go build -o bin/spelunk-daemon ./cmd/spelunk-daemon)

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

step "spec coverage (fails only on unknown @covers ids; gaps are listed, not failed)"
node scripts/specCoverage.mjs

printf '\nAll layers passed.\n'
