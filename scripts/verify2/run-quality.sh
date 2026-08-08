#!/usr/bin/env bash
# scripts/verify2/run-quality.sh
#
# VERIFY-2 quality channel (Refs #607, #482) — extraction-quality gate.
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
# NOTE (Refs #6231): the default gate demands 100% must-have recall from every
# fixture and has not been green since the fixture set grew past the original
# five. Pass --ratchet (or QUALITY_MODE=ratchet) for the gate that is actually
# enforceable: hold the per-fixture floor recorded in
# internal/quality/golden/baseline.json.
#
# New fixtures are picked up automatically: scripts/quality/run.sh iterates
# over internal/quality/golden/*/ — no explicit registration required.
#
# Usage:
#   scripts/verify2/run-quality.sh [--runs N]
#
# Env vars:
#   GRAFEL_BIN   path to grafel binary (default: auto-built)
#   QUALITY_OUT_DIR  directory to write per-fixture JSON reports into
#                    (default: reports/quality)
#   QUALITY_RUNS     number of runs per fixture for median measurement
#                    (default: 5; overridden by --runs flag if both are set,
#                    the flag wins)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Allow the caller to redirect JSON artifacts; the default keeps them under
# reports/ which is already in .gitignore.
export QUALITY_OUT_DIR="${QUALITY_OUT_DIR:-$REPO_ROOT/reports/quality}"
mkdir -p "$QUALITY_OUT_DIR"

# Pass GRAFEL_BIN through if set; otherwise let run.sh auto-build.
if [[ -n "${GRAFEL_BIN:-}" ]]; then
  export GRAFEL_BIN
fi

# Resolve --runs: command-line flag beats QUALITY_RUNS env var beats default.
RUNS_ARG=""
if [[ -n "${QUALITY_RUNS:-}" ]]; then
  RUNS_ARG="--runs $QUALITY_RUNS"
fi
# Gate selection (Refs #6231): --ratchet / --update-baseline pass straight
# through. Default is the strict 100%-recall gate, which does NOT pass on the
# full fixture set today — callers wanting an enforceable gate want --ratchet.
MODE_ARG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --runs)             RUNS_ARG="--runs ${2:?--runs requires a value}"; shift 2 ;;
    --runs=*)           RUNS_ARG="--runs ${1#--runs=}"; shift ;;
    --ratchet)          MODE_ARG="--ratchet"; shift ;;
    --update-baseline)  MODE_ARG="--update-baseline"; shift ;;
    *)                  shift ;;
  esac
done
# QUALITY_MODE is validated, not pasted through: a typo must fail loudly rather
# than quietly select a different gate (Refs #6231).
if [[ -z "$MODE_ARG" && -n "${QUALITY_MODE:-}" ]]; then
  case "$QUALITY_MODE" in
    strict)           MODE_ARG="" ;;
    ratchet)          MODE_ARG="--ratchet" ;;
    update-baseline)  MODE_ARG="--update-baseline" ;;
    *)
      echo "error: QUALITY_MODE must be strict, ratchet or update-baseline (got '$QUALITY_MODE')" >&2
      exit 1
      ;;
  esac
fi

echo "==> verify2/quality: running extraction-quality gate"
echo "==> artifacts: $QUALITY_OUT_DIR"

# Delegate — inherits exit code 0 (all fixtures pass) or 2 (regression).
# shellcheck disable=SC2086
exec "$REPO_ROOT/scripts/quality/run.sh" $RUNS_ARG $MODE_ARG
