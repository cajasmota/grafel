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
#
# Flag:
#   --runs N   Run each fixture N times and take the median entity_recall,
#              relationship_recall, and forbidden_hits before deciding pass/fail.
#              N=1 restores single-shot behaviour.  Default: 5.
#              Short-circuit: once 3+ runs finish, if entity_recall and
#              relationship_recall are all within 0.5pp across those runs the
#              remaining runs are skipped (avoids 5x wall-clock on stable
#              fixtures).
#
# Env vars:
#   ARCHIGRAPH_BIN   path to archigraph binary (default: auto-built)
#   QUALITY_OUT_DIR  directory to write per-fixture JSON reports into
#                    (default: reports/quality relative to repo root)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# Parse --runs flag; leave remaining positional args untouched.
# ---------------------------------------------------------------------------
RUNS=5
args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --runs)
      RUNS="${2:?--runs requires an integer value}"
      shift 2
      ;;
    --runs=*)
      RUNS="${1#--runs=}"
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done
set -- "${args[@]+"${args[@]}"}"

if ! [[ "$RUNS" =~ ^[0-9]+$ ]] || [[ "$RUNS" -lt 1 ]]; then
  echo "error: --runs must be a positive integer (got '$RUNS')" >&2
  exit 1
fi

BIN="${ARCHIGRAPH_BIN:-$ROOT/build/archigraph}"
if [[ ! -x "$BIN" ]]; then
  echo "build/archigraph not found — building..." >&2
  mkdir -p build
  go build -o build/archigraph ./cmd/archigraph
fi

OUT="${QUALITY_OUT_DIR:-$ROOT/reports/quality}"
mkdir -p "$OUT"

EXIT=0
for fix in "$ROOT"/internal/quality/golden/*/ ; do
  name="$(basename "$fix")"
  echo "==> quality: $name  (runs=$RUNS)"

  # ------------------------------------------------------------------
  # Collect per-run JSON outputs in a temporary directory.
  # ------------------------------------------------------------------
  tmpdir="$(mktemp -d)"
  # Cleanup on subshell exit; we reset the trap once we're done with $tmpdir.
  cleanup_tmpdir() { rm -rf "$tmpdir"; }
  trap cleanup_tmpdir EXIT

  run_idx=0
  while [[ $run_idx -lt $RUNS ]]; do
    rjson="$tmpdir/run${run_idx}.json"
    # `archigraph quality` exits 2 on regression but still writes the JSON.
    # We capture both outcomes — the median aggregator decides pass/fail.
    "$BIN" quality --json "$rjson" "$fix" 2>/dev/null || true

    # Short-circuit: once 3+ runs are available, check if they are stable
    # (entity_recall and relationship_recall all within 0.5pp of each other).
    run_idx=$((run_idx + 1))
    if [[ $run_idx -ge 3 ]]; then
      stable="$(python3 - "$tmpdir" <<'PY'
import json, glob, sys, os
tmp = sys.argv[1]
paths = sorted(glob.glob(os.path.join(tmp, "run*.json")))
recalls = []
for p in paths:
    try:
        with open(p) as fh:
            d = json.load(fh)
        recalls.append((d.get("entity_recall", 0.0), d.get("relationship_recall", 0.0)))
    except Exception:
        pass
if len(recalls) < 3:
    print("no"); sys.exit(0)
er = [r[0] for r in recalls]
rr = [r[1] for r in recalls]
if max(er) - min(er) <= 0.005 and max(rr) - min(rr) <= 0.005:
    print("yes")
else:
    print("no")
PY
)"
      if [[ "$stable" == "yes" ]]; then
        echo "    short-circuit: $run_idx runs stable (±0.5pp), skipping remaining" >&2
        break
      fi
    fi
  done

  # ------------------------------------------------------------------
  # Median aggregation — write canonical per-fixture JSON report and
  # determine pass/fail from median metrics.
  # ------------------------------------------------------------------
  json="$OUT/$name.json"
  fixture_exit=0
  python3 - "$tmpdir" "$json" "$name" <<'PY' || fixture_exit=$?
import json, glob, sys, os, statistics

tmp, out_path, fixture_name = sys.argv[1], sys.argv[2], sys.argv[3]

paths = sorted(glob.glob(os.path.join(tmp, "run*.json")))
if not paths:
    print(f"  quality: no JSON reports produced for {fixture_name}", file=sys.stderr)
    sys.exit(1)

reports = []
for p in paths:
    try:
        with open(p) as fh:
            reports.append(json.load(fh))
    except Exception:
        pass

if not reports:
    print(f"  quality: all JSON reports unreadable for {fixture_name}", file=sys.stderr)
    sys.exit(1)

def med(key, default=0.0):
    return statistics.median(float(r.get(key, default)) for r in reports)

def med_int(key, default=0):
    return int(statistics.median(int(r.get(key, default)) for r in reports))

# Use the last successful run's detail arrays (missing entities/rels) as the
# canonical sample for human inspection; median scalars are the gate metrics.
base = reports[-1]

median_entity_recall       = med("entity_recall")
median_relationship_recall = med("relationship_recall")
median_forbidden_hits      = med_int("forbidden_hits")
runs_executed              = len(reports)

merged = dict(base)
merged["entity_recall"]                  = median_entity_recall
merged["entity_recall_min"]              = min(float(r.get("entity_recall", 0)) for r in reports)
merged["entity_recall_max"]              = max(float(r.get("entity_recall", 0)) for r in reports)
merged["entity_found"]                   = med_int("entity_found")
merged["relationship_recall"]            = median_relationship_recall
merged["relationship_recall_min"]        = min(float(r.get("relationship_recall", 0)) for r in reports)
merged["relationship_recall_max"]        = max(float(r.get("relationship_recall", 0)) for r in reports)
merged["relationship_found"]             = med_int("relationship_found")
merged["forbidden_hits"]                 = median_forbidden_hits
merged["runs_executed"]                  = runs_executed

with open(out_path, "w") as fh:
    json.dump(merged, fh, indent=2)
    fh.write("\n")

# Gate on median — any must-have miss OR any forbidden hit fails the fixture.
entity_expected = int(base.get("entity_expected", 0))
rel_expected    = int(base.get("relationship_expected", 0))
regressed = False
if entity_expected > 0 and med_int("entity_found") < entity_expected:
    regressed = True
if rel_expected > 0 and med_int("relationship_found") < rel_expected:
    regressed = True
if median_forbidden_hits > 0:
    regressed = True
if regressed:
    sys.exit(2)
PY

  trap - EXIT
  cleanup_tmpdir

  if [[ $fixture_exit -ne 0 ]]; then
    EXIT=2
  fi
done

echo
echo "quality reports written to: $OUT"
exit "$EXIT"
