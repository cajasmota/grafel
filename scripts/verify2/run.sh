#!/usr/bin/env bash
# scripts/verify2/run.sh
#
# VERIFY-2 (Refs #58) — bug-rate / resolution-rate measurement harness.
#
# Clones a small set of public OSS repositories into
# $ARCHIGRAPH_CORPORA_DIR (default: $HOME/Documents/Projects/archigraph-corpora)
# and runs `archigraph index --json-stats` over each. Aggregates the
# per-disposition counts and writes a Markdown report into
# $ARCHIGRAPH_CORPORA_DIR/_reports/<ISO-timestamp>.md.
#
# This script never writes inside the archigraph repo. The corpora and
# reports live entirely outside it so we don't blow up the worktree with
# vendored third-party source.
#
# Usage:
#   scripts/verify2/run.sh
#
# Env vars:
#   ARCHIGRAPH_CORPORA_DIR   target dir for clones + reports
#                            (default: $HOME/Documents/Projects/archigraph-corpora)
#   ARCHIGRAPH_BIN           path to archigraph binary (default: ./archigraph
#                            built ad-hoc into the corpora dir)
#   ARCHIGRAPH_VERBOSE       set to 1 to forward verbose stderr from indexer
set -euo pipefail

CORPORA_DIR="${ARCHIGRAPH_CORPORA_DIR:-$HOME/Documents/Projects/archigraph-corpora}"
REPORTS_DIR="$CORPORA_DIR/_reports"
mkdir -p "$CORPORA_DIR" "$REPORTS_DIR"

# Repo list. Keep entries SHORT, public, and well-known. Each entry is:
#   <name>|<git-url>|<ref>
# Total target: ~5-8 repos, ~10-50k LOC.
REPOS=(
  "requests|https://github.com/psf/requests.git|main"
  "gin|https://github.com/gin-gonic/gin.git|master"
  "express|https://github.com/expressjs/express.git|master"
  "flask|https://github.com/pallets/flask.git|main"
  "chi|https://github.com/go-chi/chi.git|master"
  "click|https://github.com/pallets/click.git|main"
)

# Locate or build the archigraph binary. We build into the corpora dir
# (outside the repo) so this script is safe to run from any worktree.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [[ -n "${ARCHIGRAPH_BIN:-}" ]]; then
  BIN="$ARCHIGRAPH_BIN"
else
  BIN="$CORPORA_DIR/_bin/archigraph"
  mkdir -p "$(dirname "$BIN")"
  echo "==> building archigraph -> $BIN" >&2
  ( cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/archigraph )
fi

if [[ ! -x "$BIN" ]]; then
  echo "archigraph binary not executable: $BIN" >&2
  exit 1
fi

TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
REPORT="$REPORTS_DIR/$TIMESTAMP.md"
TMPDIR_AGG="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_AGG"' EXIT

# Per-disposition aggregation happens in the inline python below; bash 3.2
# (the macOS default) doesn't support associative arrays so we keep state
# entirely in the per-repo JSON files written under $TMPDIR_AGG.

# Markdown report header.
{
  echo "# VERIFY-2 bug-rate report"
  echo
  echo "- generated_at: \`$TIMESTAMP\`"
  echo "- corpora_dir: \`$CORPORA_DIR\`"
  echo "- archigraph_bin: \`$BIN\`"
  echo
  echo "## Per-repo results"
  echo
  echo "| repo | files | entities | relationships | bug_rate | resolution_rate |"
  echo "| --- | ---: | ---: | ---: | ---: | ---: |"
} >"$REPORT"

clone_or_update() {
  local name="$1" url="$2" ref="$3"
  local dest="$CORPORA_DIR/$name"
  if [[ -d "$dest/.git" ]]; then
    echo "==> updating $name" >&2
    ( cd "$dest" && git fetch --depth 1 origin "$ref" >/dev/null 2>&1 && git checkout -q FETCH_HEAD ) || true
  else
    echo "==> cloning $name @ $ref" >&2
    git clone --depth 1 --branch "$ref" "$url" "$dest" >/dev/null 2>&1 || \
      git clone --depth 1 "$url" "$dest" >/dev/null 2>&1
  fi
}

run_one() {
  local name="$1"
  local dest="$CORPORA_DIR/$name"
  local out="$TMPDIR_AGG/$name.json"
  local stderr_log="$TMPDIR_AGG/$name.stderr"
  echo "==> indexing $name" >&2
  if ! "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log"; then
    echo "  ! indexer failed; see $stderr_log" >&2
    return 1
  fi
  # Extract numbers via a small inline python (jq not assumed present).
  python3 - "$out" "$REPORT" "$name" <<'PY'
import json, sys
path, report, name = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as fh:
    d = json.load(fh)
row = "| {name} | {files} | {ent} | {rel} | {br:.2%} | {rr:.2%} |\n".format(
    name=name,
    files=d.get("files", 0),
    ent=d.get("entities", 0),
    rel=d.get("relationships", 0),
    br=d.get("bug_rate", 0.0),
    rr=d.get("resolution_rate", 0.0),
)
with open(report, "a") as fh:
    fh.write(row)
PY
}

for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref <<<"$entry"
  clone_or_update "$name" "$url" "$ref"
  if ! run_one "$name"; then
    echo "| $name | ERROR | - | - | - | - |" >>"$REPORT"
    continue
  fi
done

# Aggregate dispositions across every per-repo JSON file.
python3 - "$TMPDIR_AGG" "$REPORT" <<'PY'
import json, os, sys, glob
tmp, report = sys.argv[1], sys.argv[2]
agg = {}
totals = {"files": 0, "entities": 0, "relationships": 0}
endpoints_total = 0
endpoints_resolved = 0
endpoints_bug = 0
for p in sorted(glob.glob(os.path.join(tmp, "*.json"))):
    with open(p) as fh:
        d = json.load(fh)
    totals["files"] += d.get("files", 0)
    totals["entities"] += d.get("entities", 0)
    totals["relationships"] += d.get("relationships", 0)
    for k, v in d.get("disposition_counts", {}).items():
        agg[k] = agg.get(k, 0) + v
        endpoints_total += v
        if k == "resolved":
            endpoints_resolved += v
        if k in ("bug-extractor", "bug-resolver"):
            endpoints_bug += v
br = (endpoints_bug / endpoints_total) if endpoints_total else 0.0
rr = (endpoints_resolved / endpoints_total) if endpoints_total else 0.0
with open(report, "a") as fh:
    fh.write("\n## Aggregate\n\n")
    fh.write("| metric | value |\n| --- | ---: |\n")
    fh.write(f"| total_files | {totals['files']} |\n")
    fh.write(f"| total_entities | {totals['entities']} |\n")
    fh.write(f"| total_relationships | {totals['relationships']} |\n")
    fh.write(f"| endpoints_classified | {endpoints_total} |\n")
    fh.write(f"| bug_rate | {br:.4%} |\n")
    fh.write(f"| resolution_rate | {rr:.4%} |\n")
    fh.write("\n## Disposition breakdown\n\n")
    fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
    for k in sorted(agg):
        v = agg[k]
        pct = (v / endpoints_total) if endpoints_total else 0.0
        fh.write(f"| {k} | {v} | {pct:.2%} |\n")
    fh.write("\n## Ship-gate check (target bug_rate <= 1%)\n\n")
    status = "PASS" if br <= 0.01 else "FAIL"
    fh.write(f"- status: **{status}** (bug_rate={br:.4%})\n")
PY

echo "==> wrote report: $REPORT"
echo "$REPORT"
