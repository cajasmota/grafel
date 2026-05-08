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

# Repo list. Keep entries public and well-known. Each entry is:
#   <name>|<git-url>|<ref>|<primary-language>[|<sparse-path>]
#
# The optional 5th field selects a single sub-tree via partial clone +
# git sparse-checkout (cone mode). This is REQUIRED for monorepos where
# a full clone exceeds ~200 MB at HEAD on the chosen ref — the comment
# next to each entry records the rough estimate at the time of authoring.
#
# Coverage targets the full 32-language extractor matrix + frameworks +
# ORMs + manifests + tools. Stack-characteristic diversity (Refs #87):
#   - ORM-heavy             rails (actionpack/activerecord), django
#   - HTTP routing          gin, chi, express, actix-web, vapor
#   - microservice / RPC    etcd, kafka
#   - CLI tool              click
#   - config-heavy          pandas (mixed), spring-boot autoconfigure
REPOS=(
  # --- Python ---
  "requests|https://github.com/psf/requests.git|main|python"                                       # ~6 MB
  "flask|https://github.com/pallets/flask.git|main|python"                                         # ~6 MB
  "click|https://github.com/pallets/click.git|main|python"                                         # ~7 MB
  "django|https://github.com/django/django.git|main|python"                                        # ~80 MB
  "pandas|https://github.com/pandas-dev/pandas.git|main|python|pandas/core"                        # full ~400 MB; sparse subset
  # --- Go ---
  "gin|https://github.com/gin-gonic/gin.git|master|go"                                             # ~3 MB
  "chi|https://github.com/go-chi/chi.git|master|go"                                                # ~2 MB
  "etcd|https://github.com/etcd-io/etcd.git|main|go|server/etcdserver"                             # full ~250 MB; sparse subset
  # --- JavaScript / TypeScript ---
  "express|https://github.com/expressjs/express.git|master|javascript"                             # ~4 MB
  "nestjs|https://github.com/nestjs/nest.git|master|typescript|packages/core"                      # ~120 MB full; sparse subset
  "nextjs|https://github.com/vercel/next.js.git|canary|typescript|packages/next/src/server"        # >1 GB full; sparse subset
  # --- Java ---
  "spring-boot|https://github.com/spring-projects/spring-boot.git|main|java|spring-boot-project/spring-boot-actuator-autoconfigure"  # >300 MB full; sparse
  "kafka|https://github.com/apache/kafka.git|trunk|java|clients/src/main/java/org/apache/kafka/clients"                              # >200 MB full; sparse
  # --- Kotlin ---
  "exposed|https://github.com/JetBrains/Exposed.git|main|kotlin"                                   # ~15 MB
  "ktor|https://github.com/ktorio/ktor.git|main|kotlin|ktor-server/ktor-server-core"               # >200 MB full; sparse
  # --- Ruby ---
  "rails-actionpack|https://github.com/rails/rails.git|main|ruby|actionpack"                       # >150 MB full; sparse
  "sidekiq|https://github.com/sidekiq/sidekiq.git|main|ruby"                                       # ~20 MB
  # --- PHP ---
  "laravel-routing|https://github.com/laravel/framework.git|11.x|php|src/Illuminate/Routing"       # >100 MB full; sparse
  "symfony-routing|https://github.com/symfony/symfony.git|7.2|php|src/Symfony/Component/Routing"   # >300 MB full; sparse
  # --- Rust ---
  "tokio|https://github.com/tokio-rs/tokio.git|master|rust|tokio/src"                              # ~60 MB full; sparse
  "actix-web|https://github.com/actix/actix-web.git|master|rust|actix-web/src"                     # ~30 MB full; sparse
  # --- Swift ---
  "vapor|https://github.com/vapor/vapor.git|main|swift|Sources/Vapor"                              # ~10 MB full; sparse to be safe
  # --- C# ---
  "aspnetcore-mvc|https://github.com/dotnet/aspnetcore.git|main|csharp|src/Mvc/Mvc.Core"           # >500 MB full; sparse
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

# Optional per-repo wall-clock cap (seconds). Set ARCHIGRAPH_VERIFY2_TIMEOUT=0
# to disable. Uses gtimeout (coreutils) if available, then timeout, then
# silently skips capping on systems with neither.
PER_REPO_TIMEOUT="${ARCHIGRAPH_VERIFY2_TIMEOUT:-600}"
TIMEOUT_BIN=""
if command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_BIN="gtimeout"
elif command -v timeout >/dev/null 2>&1; then
  TIMEOUT_BIN="timeout"
fi

# Write the language manifest used by the per-language aggregation step.
LANG_MANIFEST="$TMPDIR_AGG/_languages.tsv"
: >"$LANG_MANIFEST"
for entry in "${REPOS[@]}"; do
  IFS='|' read -r name url ref lang sparse <<<"$entry"
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
  local name="$1" url="$2" ref="$3" sparse="${4:-}"
  local dest="$CORPORA_DIR/$name"
  if [[ -d "$dest/.git" ]]; then
    echo "==> updating $name" >&2
    ( cd "$dest" && git fetch --depth 1 origin "$ref" >/dev/null 2>&1 && git checkout -q FETCH_HEAD ) || true
    return 0
  fi
  if [[ -n "$sparse" ]]; then
    echo "==> sparse-cloning $name @ $ref (subset: $sparse)" >&2
    # Blob-less partial clone + cone-mode sparse checkout. We deliberately
    # do not pass --depth here because partial clones with --depth+--branch
    # are flaky on older git versions; the blob filter alone keeps the
    # working set small.
    if ! git clone --filter=blob:none --no-checkout --branch "$ref" "$url" "$dest" >/dev/null 2>&1; then
      git clone --filter=blob:none --no-checkout "$url" "$dest" >/dev/null 2>&1
    fi
    ( cd "$dest" \
      && git sparse-checkout init --cone >/dev/null 2>&1 \
      && git sparse-checkout set "$sparse" >/dev/null 2>&1 \
      && git checkout -q "$ref" 2>/dev/null \
        || git checkout -q FETCH_HEAD 2>/dev/null \
        || git checkout -q ) || true
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
  local rc=0
  if [[ -n "$TIMEOUT_BIN" && "$PER_REPO_TIMEOUT" != "0" ]]; then
    "$TIMEOUT_BIN" --foreground "${PER_REPO_TIMEOUT}s" \
      "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  else
    "$BIN" index --json-stats "$dest" >"$out" 2>"$stderr_log" || rc=$?
  fi
  if [[ $rc -ne 0 ]]; then
    if [[ -n "$TIMEOUT_BIN" && $rc -eq 124 ]]; then
      echo "  ! indexer timed out after ${PER_REPO_TIMEOUT}s for $name" >&2
    else
      echo "  ! indexer failed (rc=$rc); see $stderr_log" >&2
    fi
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
  IFS='|' read -r name url ref lang sparse <<<"$entry"
  clone_or_update "$name" "$url" "$ref" "${sparse:-}"
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
