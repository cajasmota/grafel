#!/usr/bin/env bash
# Phase D MCP latency bench — measures cold + warm latency and per-call
# allocation count for the cache-backed FlatBuffers path versus the
# legacy graph.json reparse path.
#
# Inputs:
#   FIXTURE_REPO    repo to index (default: testdata/fixtures/real-world/go)
#   BIN             archigraph binary (default: build/archigraph)
#
# Outputs:
#   /tmp/bench-mcp-latency/<run>/  — go test bench output (text + json)
#
# Numbers reported:
#   ReadEntity_FBCache         ns/op + B/op + allocs/op
#   FindReferences_FBCache     ns/op + B/op + allocs/op
#   ReadEntity_JSONReparse     ns/op + B/op + allocs/op
#
# The script is idempotent — rerun after a code change and compare the
# two newest run dirs. Numbers must beat the Phase-D targets:
#
#   ReadEntity_FBCache:  <= 5 ms  /  <= 5 MB    /  few dozen allocs
#   FindReferences_FBCache: per-call alloc bytes flat at any N
#   ReadEntity_JSONReparse:  baseline (no target — informational)
set -euo pipefail

BIN="${BIN:-./build/archigraph}"
FIXTURE_REPO="${FIXTURE_REPO:-testdata/fixtures/real-world/go}"

RUN_ID="$(date +%Y%m%d-%H%M%S)"
OUT="/tmp/bench-mcp-latency/$RUN_ID"
mkdir -p "$OUT"

if [ ! -x "$BIN" ]; then
  echo "building $BIN"
  go build -o "$BIN" ./cmd/archigraph
fi

# Materialise graph.json + graph.fb for the fixture in a tempdir copy
# so we don't pollute the source tree.
WORK="$(mktemp -d /tmp/bench-mcp-XXXX)"
trap 'rm -rf "$WORK"' EXIT

cp -R "$FIXTURE_REPO" "$WORK/repo"
"$BIN" index --export-fb "$WORK/repo" >"$OUT/index.log" 2>&1

FB="$WORK/repo/.archigraph/graph.fb"
JSON="$WORK/repo/.archigraph/graph.json"

if [ ! -f "$FB" ]; then
  echo "FAIL: graph.fb not produced; see $OUT/index.log"
  exit 1
fi

echo "graph.fb   $(wc -c <"$FB") bytes"
echo "graph.json $(wc -c <"$JSON") bytes"

# Run the Go benchmarks.
ARCHIGRAPH_BENCH_FIXTURE_FB="$FB" \
ARCHIGRAPH_BENCH_FIXTURE="$JSON" \
go test ./internal/daemon/mcp/ \
  -run=^$ \
  -bench='Benchmark(ReadEntity|FindReferences)' \
  -benchmem \
  -count=3 \
  -benchtime=200x \
  2>&1 | tee "$OUT/bench.txt"

echo
echo "results in $OUT/bench.txt"
