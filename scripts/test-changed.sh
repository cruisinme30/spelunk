#!/usr/bin/env bash
# Validate a develop PR with all static checks and affected behavioral layers.
# Shared/unknown paths select the full suite. Ref failures fail closed.
# Usage: scripts/test-changed.sh [--base origin/develop] [--head HEAD] [--print]
set -euo pipefail
cd "$(dirname "$0")/.."
exec node scripts/changedTests.mjs "$@"
