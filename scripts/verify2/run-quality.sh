#!/usr/bin/env bash
# scripts/verify2/run-quality.sh
#
# VERIFY-2 quality channel (Refs #607) — extraction-quality gate.
#
# Thin wrapper that delegates to scripts/quality/run.sh and surfaces the
# per-fixture JSON reports as a CI artifact. Designed to run as a separate
# CI step (see .github/workflows/quality.yml) so quality failures are
# reported independently from the bug-rate harness.
#
# A PR that regresses any must-have entity / relationship, or introduces a
# forbidden-relationship hit, will cause this script to exit 2 with a clear
# per-fixture miss report on stderr.
#
# New fixtures are picked up automatically: scripts/quality/run.sh iterates
# over internal/quality/golden/*/ — no explicit registration required.
#
# Usage:
#   scripts/verify2/run-quality.sh
#
# Env vars:
#   ARCHIGRAPH_BIN   path to archigraph binary (default: auto-built)
#   QUALITY_OUT_DIR  directory to write per-fixture JSON reports into
#                    (default: reports/quality)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Allow the caller to redirect JSON artifacts; the default keeps them under
# reports/ which is already in .gitignore.
export QUALITY_OUT_DIR="${QUALITY_OUT_DIR:-$REPO_ROOT/reports/quality}"
mkdir -p "$QUALITY_OUT_DIR"

# Pass ARCHIGRAPH_BIN through if set; otherwise let run.sh auto-build.
if [[ -n "${ARCHIGRAPH_BIN:-}" ]]; then
  export ARCHIGRAPH_BIN
fi

echo "==> verify2/quality: running extraction-quality gate"
echo "==> artifacts: $QUALITY_OUT_DIR"

# Delegate — inherits exit code 0 (all fixtures pass) or 2 (regression).
exec "$REPO_ROOT/scripts/quality/run.sh"
