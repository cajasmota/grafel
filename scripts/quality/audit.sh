#!/bin/bash
# CI helper that runs the orphan/quality audit across a corpus and writes
# both a markdown summary and a machine-readable JSON sidecar under
# reports/quality/.
#
# Usage:
#   scripts/quality/audit.sh [corpus-dir]
#
# Env:
#   OUT_DIR  override the report destination (default: ./reports/quality)
set -euo pipefail

CORPUS_DIR="${1:-$HOME/Documents/Projects/grafel-corpora}"
OUT_DIR="${OUT_DIR:-./reports/quality}"
DATE="$(date +%Y-%m-%d)"

mkdir -p "$OUT_DIR"

# Build every time. `if [[ ! -x "$BIN" ]]` was the same defect fixed in
# scripts/quality/run.sh for #6283: build/ is gitignored and long-lived, so an
# existing binary was audited whatever source it came from, and the orphan
# figures this writes into reports/quality/ described that source rather than
# the tree. There is no env override on $BIN here, so this path is always ours
# to overwrite.
BIN="./build/grafel"
echo "audit.sh: building $BIN" >&2
go build -o "$BIN" ./cmd/grafel

"$BIN" quality audit-orphans --corpus "$CORPUS_DIR" --json   --output "$OUT_DIR/orphan-audit-$DATE.json"
"$BIN" quality audit-orphans --corpus "$CORPUS_DIR"          --output "$OUT_DIR/orphan-audit-$DATE.md"

echo "audit.sh: reports written to $OUT_DIR" >&2
