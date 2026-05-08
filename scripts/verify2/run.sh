#!/usr/bin/env bash
# scripts/verify2/run.sh
#
# VERIFY-2 (Refs #58, #88) — bug-rate / resolution-rate measurement harness.
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
#   <name>|<git-url>|<ref>|<primary-language>
# Total target: ~5-8 repos, ~10-50k LOC.
REPOS=(
  "requests|https://github.com/psf/requests.git|main|python"
  "gin|https://github.com/gin-gonic/gin.git|master|go"
  "express|https://github.com/expressjs/express.git|master|javascript"
  "flask|https://github.com/pallets/flask.git|main|python"
  "chi|https://github.com/go-chi/chi.git|master|go"
  "click|https://github.com/pallets/click.git|main|python"
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

# Write the language manifest used by the per-language aggregation step.
LANG_MANIFEST="$TMPDIR_AGG/_languages.tsv"
: >"$LANG_MANIFEST"
for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang <<<"$entry"
  printf '%s\t%s\n' "$name" "$lang" >>"$LANG_MANIFEST"
done

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
  IFS='|' read -r name url ref lang <<<"$entry"
  clone_or_update "$name" "$url" "$ref"
  if ! run_one "$name"; then
    echo "| $name | ERROR | - | - | - | - |" >>"$REPORT"
    continue
  fi
done

# Fail-fast: if no per-repo JSON files were produced, or every produced
# JSON had files=0, exit 1 instead of writing an empty report. This guards
# against silent corpus drift (e.g., clone failures, every clone empty).
python3 - "$TMPDIR_AGG" <<'PY' || { echo "VERIFY-2: empty corpus — no repos indexed or all repos had files=0" >&2; exit 1; }
import json, os, sys, glob
tmp = sys.argv[1]
paths = sorted(glob.glob(os.path.join(tmp, "*.json")))
if not paths:
    sys.exit(1)
total_files = 0
for p in paths:
    with open(p) as fh:
        d = json.load(fh)
    total_files += d.get("files", 0)
if total_files == 0:
    sys.exit(1)
sys.exit(0)
PY

# Aggregate dispositions across every per-repo JSON file. Adds:
#   - aggregate row inside the per-repo table
#   - per-repo disposition table for each repo
#   - corpus-wide aggregate metric table
#   - corpus-wide disposition breakdown
#   - per-language aggregate (using the LANG_MANIFEST written above)
#   - ship-gate check
python3 - "$TMPDIR_AGG" "$REPORT" "$LANG_MANIFEST" <<'PY'
import json, os, sys, glob
tmp, report, manifest = sys.argv[1], sys.argv[2], sys.argv[3]

DISPOSITIONS = [
    "resolved",
    "external-known",
    "external-unknown",
    "dynamic",
    "bug-extractor",
    "bug-resolver",
    "unclassified",
]

# Load language manifest: name -> language.
lang_of = {}
with open(manifest) as fh:
    for line in fh:
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            continue
        lang_of[parts[0]] = parts[1]

per_repo = {}
for p in sorted(glob.glob(os.path.join(tmp, "*.json"))):
    with open(p) as fh:
        d = json.load(fh)
    name = os.path.splitext(os.path.basename(p))[0]
    per_repo[name] = d

# Aggregate row in the per-repo table (still inside ## Per-repo results).
totals = {"files": 0, "entities": 0, "relationships": 0}
endpoints_total = 0
endpoints_resolved = 0
endpoints_bug = 0
agg_dispo = {k: 0 for k in DISPOSITIONS}
for name, d in per_repo.items():
    totals["files"] += d.get("files", 0)
    totals["entities"] += d.get("entities", 0)
    totals["relationships"] += d.get("relationships", 0)
    for k, v in d.get("disposition_counts", {}).items():
        agg_dispo[k] = agg_dispo.get(k, 0) + v
        endpoints_total += v
        if k == "resolved":
            endpoints_resolved += v
        if k in ("bug-extractor", "bug-resolver"):
            endpoints_bug += v
agg_br = (endpoints_bug / endpoints_total) if endpoints_total else 0.0
agg_rr = (endpoints_resolved / endpoints_total) if endpoints_total else 0.0

with open(report, "a") as fh:
    # Aggregate row at the bottom of the per-repo table.
    fh.write("| **AGGREGATE** | **{f}** | **{e}** | **{r}** | **{br:.2%}** | **{rr:.2%}** |\n".format(
        f=totals["files"], e=totals["entities"], r=totals["relationships"],
        br=agg_br, rr=agg_rr))

    # Per-repo disposition tables.
    fh.write("\n## Per-repo disposition breakdown\n")
    for name in sorted(per_repo):
        d = per_repo[name]
        counts = d.get("disposition_counts", {})
        repo_total = sum(counts.get(k, 0) for k in DISPOSITIONS)
        fh.write(f"\n### {name}\n\n")
        fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
        for k in DISPOSITIONS:
            v = counts.get(k, 0)
            pct = (v / repo_total) if repo_total else 0.0
            fh.write(f"| {k} | {v} | {pct:.2%} |\n")
        fh.write(f"| **total** | **{repo_total}** | **100.00%** |\n")

    # Corpus-wide aggregate metric table.
    fh.write("\n## Aggregate\n\n")
    fh.write("| metric | value |\n| --- | ---: |\n")
    fh.write(f"| total_files | {totals['files']} |\n")
    fh.write(f"| total_entities | {totals['entities']} |\n")
    fh.write(f"| total_relationships | {totals['relationships']} |\n")
    fh.write(f"| endpoints_classified | {endpoints_total} |\n")
    fh.write(f"| bug_rate | {agg_br:.4%} |\n")
    fh.write(f"| resolution_rate | {agg_rr:.4%} |\n")

    # Corpus-wide disposition breakdown.
    fh.write("\n## Aggregate disposition breakdown\n\n")
    fh.write("| disposition | count | pct |\n| --- | ---: | ---: |\n")
    for k in DISPOSITIONS:
        v = agg_dispo.get(k, 0)
        pct = (v / endpoints_total) if endpoints_total else 0.0
        fh.write(f"| {k} | {v} | {pct:.2%} |\n")
    fh.write(f"| **total** | **{endpoints_total}** | **100.00%** |\n")

    # Per-language aggregate.
    fh.write("\n## Per-language aggregate\n\n")
    fh.write("| language | repos | files | entities | relationships | endpoints | bug_rate | resolution_rate |\n")
    fh.write("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
    by_lang = {}
    for name, d in per_repo.items():
        lang = lang_of.get(name, "unknown")
        bucket = by_lang.setdefault(lang, {
            "repos": 0, "files": 0, "entities": 0, "relationships": 0,
            "endpoints": 0, "resolved": 0, "bug": 0,
        })
        bucket["repos"] += 1
        bucket["files"] += d.get("files", 0)
        bucket["entities"] += d.get("entities", 0)
        bucket["relationships"] += d.get("relationships", 0)
        for k, v in d.get("disposition_counts", {}).items():
            bucket["endpoints"] += v
            if k == "resolved":
                bucket["resolved"] += v
            if k in ("bug-extractor", "bug-resolver"):
                bucket["bug"] += v
    for lang in sorted(by_lang):
        b = by_lang[lang]
        br = (b["bug"] / b["endpoints"]) if b["endpoints"] else 0.0
        rr = (b["resolved"] / b["endpoints"]) if b["endpoints"] else 0.0
        fh.write(f"| {lang} | {b['repos']} | {b['files']} | {b['entities']} | "
                 f"{b['relationships']} | {b['endpoints']} | {br:.4%} | {rr:.4%} |\n")

    # Ship-gate check.
    fh.write("\n## Ship-gate check (target bug_rate <= 1%)\n\n")
    status = "PASS" if agg_br <= 0.01 else "FAIL"
    fh.write(f"- status: **{status}** (bug_rate={agg_br:.4%})\n")
PY

echo "==> wrote report: $REPORT"
echo "$REPORT"
