#!/usr/bin/env bash
# Runs locally what the GitHub workflows run, with the same tool versions, so
# a push that passes here passes CI. The pre-push hook (.githooks/pre-push,
# installed by `npm install`) runs it before every push.
#
#   1. golangci-lint at the version ci.yml pins, installed into .tools/ once
#   2. Go at the version daemon/go.mod names (GOTOOLCHAIN), as setup-go does
#   3. CI's "test" job: scripts/test-all.sh with CI=1, so a missing linter fails
#   4. CI's "e2e" job: build the daemon and extension, run scripts/e2e.mjs
#   5. the wiki matches the mocks and is pushed (scripts/checkWikiSync.mjs)
#   6. with --tag vX.Y.Z, the Release workflow's checks: the tag is the
#      extension's version, every spec id has a test, every platform packages
#
# The release benchmarks (a 40-minute run on a Prometheus clone) are the one
# workflow step left to CI.
#
# Usage: scripts/presubmit.sh [--clean] [--tag vX.Y.Z]
#   --clean  fail if the working tree has changes, so what's tested is exactly
#            what's committed (the hook passes this)
set -euo pipefail
cd "$(dirname "$0")/.."
root=$PWD

clean=""
tag=""
while [ $# -gt 0 ]; do
  case $1 in
    --clean) clean=1 ;;
    --tag) tag=$2; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

# Prints a step heading.
step() { printf '\n== presubmit: %s\n' "$*"; }

if [ -n "$clean" ] && [ -n "$(git status --porcelain)" ]; then
  git status --short >&2
  echo "presubmit: commit or stash the changes above first; they would be tested but not pushed." >&2
  exit 1
fi

step "tool versions match CI"
lint_version=$(sed -n 's/.*golangci-lint.*install\.sh.* \(v[0-9][0-9.]*\)$/\1/p' .github/workflows/ci.yml)
[ -n "$lint_version" ] || { echo "can't find golangci-lint's version in ci.yml" >&2; exit 1; }
lint_dir="$root/.tools/golangci-lint-$lint_version"
if [ ! -x "$lint_dir/golangci-lint" ]; then
  echo "installing golangci-lint $lint_version into .tools/"
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$lint_dir" "$lint_version"
fi
export PATH="$lint_dir:$PATH"
golangci-lint --version

go_version=$(sed -n 's/^go \([0-9.]*\)$/\1/p' daemon/go.mod)
# setup-go installs the newest patch of go.mod's version; the .0 release has the same language and library.
case $go_version in *.*.*) export GOTOOLCHAIN="go$go_version" ;; *) export GOTOOLCHAIN="go$go_version.0" ;; esac
go version

node_major=$(node -p 'process.versions.node.split(".")[0]')
ci_node=$(sed -n 's/.*node-version: *\([0-9]*\).*/\1/p' .github/workflows/ci.yml | head -n 1)
[ "$node_major" = "$ci_node" ] || echo "warning: Node $node_major here, CI uses Node $ci_node"

step "CI test job: scripts/test-all.sh"
CI=1 scripts/test-all.sh

step "CI e2e job: the commands in a real VS Code window"
(cd daemon && go build -o bin/unified-search-daemon ./cmd/unified-search-daemon)
npm run build
if [ "$(uname)" = Linux ] && [ -z "${DISPLAY:-}" ]; then xvfb-run -a node scripts/e2e.mjs; else node scripts/e2e.mjs; fi

step "the wiki matches the mocks"
node scripts/checkWikiSync.mjs

if [ -n "$tag" ]; then
  step "Release workflow checks for $tag"
  version=$(node -p "require('./extension/package.json').version")
  [ "v$version" = "$tag" ] || { echo "tag $tag is not the extension's version v$version" >&2; exit 1; }
  node scripts/specCoverage.mjs --strict
  for target in darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-arm64 win32-x64; do
    node scripts/packageExtension.mjs --target "$target"
  done
fi

printf '\nPresubmit passed: this push should pass CI.\n'
