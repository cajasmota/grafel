#!/usr/bin/env bash
# Extraction-quality benchmark runner. Iterates over every fixture under
# internal/quality/golden/ and runs `archigraph quality` against each,
# writing one JSON report per fixture into reports/quality/.
#
# Exit status:
#   0 — every fixture met its must-have recall + 0 forbidden hits
#   1 — runner setup / build error
#   2 — at least one fixture regressed (must-have miss or forbidden hit)
#
# Intended to wire into the verify2 channel as a separate gate. Quality is
# orthogonal to bug-rate: we report both, and either can block a release.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

BIN="${ARCHIGRAPH_BIN:-$ROOT/build/archigraph}"
if [[ ! -x "$BIN" ]]; then
  echo "build/archigraph not found — building..." >&2
  mkdir -p build
  go build -o build/archigraph ./cmd/archigraph
fi

OUT="$ROOT/reports/quality"
mkdir -p "$OUT"

EXIT=0
for fix in "$ROOT"/internal/quality/golden/*/ ; do
  name="$(basename "$fix")"
  json="$OUT/$name.json"
  echo "==> quality: $name"
  if ! "$BIN" quality --json "$json" "$fix"; then
    EXIT=2
  fi
done

echo
echo "quality reports written to: $OUT"
exit "$EXIT"
