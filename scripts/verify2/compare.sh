#!/usr/bin/env bash
# scripts/verify2/compare.sh
#
# Diff two VERIFY-2 Markdown reports and print per-repo / aggregate /
# per-disposition deltas (entities, relationships, bug_rate,
# resolution_rate, plus per-disposition count deltas).
#
# Usage:
#   scripts/verify2/compare.sh <baseline.md> <current.md>
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <baseline.md> <current.md>" >&2
  exit 2
fi

BASE="$1"
CUR="$2"

for f in "$BASE" "$CUR"; do
  if [[ ! -f "$f" ]]; then
    echo "report not found: $f" >&2
    exit 1
  fi
done

python3 - "$BASE" "$CUR" <<'PY'
import re, sys

DISPOSITIONS = [
    "resolved",
    "external-known",
    "external-unknown",
    "dynamic",
    "bug-extractor",
    "bug-resolver",
    "unclassified",
]

def parse_report(path):
    """Return (per_repo_dict, aggregate_dict, dispositions_dict) parsed
    from a Markdown report written by run.sh. Resilient to trailing
    whitespace and missing rows."""
    repos = {}
    agg = {}
    dispo = {}
    with open(path) as fh:
        text = fh.read()
    # Per-repo rows look like:
    # | repo | files | entities | relationships | bug_rate | resolution_rate |
    pr_section = re.search(r"## Per-repo results\s*\n\n(\|.+?)\n\n", text, re.S)
    if pr_section:
        for line in pr_section.group(1).splitlines():
            parts = [p.strip().strip('*') for p in line.strip().strip('|').split('|')]
            if len(parts) != 6:
                continue
            name, files, ent, rel, br, rr = parts
            if name in ("repo", "---", "AGGREGATE") or name.startswith("---"):
                continue
            try:
                repos[name] = {
                    "files": int(files),
                    "entities": int(ent),
                    "relationships": int(rel),
                    "bug_rate": float(br.rstrip('%')) / 100,
                    "resolution_rate": float(rr.rstrip('%')) / 100,
                }
            except ValueError:
                continue
    # Aggregate metric table (corpus-wide).
    ag_section = re.search(r"## Aggregate\s*\n\n(\|.+?)\n\n", text, re.S)
    if ag_section:
        for line in ag_section.group(1).splitlines():
            parts = [p.strip() for p in line.strip().strip('|').split('|')]
            if len(parts) != 2 or parts[0] in ("metric", "---") or parts[0].startswith("---"):
                continue
            metric, val = parts
            if val.endswith('%'):
                try:
                    agg[metric] = float(val.rstrip('%')) / 100
                except ValueError:
                    pass
            else:
                try:
                    agg[metric] = int(val)
                except ValueError:
                    pass
    # Aggregate disposition breakdown.
    dp_section = re.search(r"## Aggregate disposition breakdown\s*\n\n(\|.+?)\n\n", text, re.S)
    if dp_section:
        for line in dp_section.group(1).splitlines():
            parts = [p.strip().strip('*') for p in line.strip().strip('|').split('|')]
            if len(parts) != 3 or parts[0] in ("disposition", "---", "total") or parts[0].startswith("---"):
                continue
            k, v, _pct = parts
            try:
                dispo[k] = int(v)
            except ValueError:
                continue
    return repos, agg, dispo

base_repos, base_agg, base_dispo = parse_report(sys.argv[1])
cur_repos, cur_agg, cur_dispo = parse_report(sys.argv[2])

print(f"baseline: {sys.argv[1]}")
print(f"current : {sys.argv[2]}")
print()
print("## Per-repo deltas")
print(f"{'repo':<24} {'Δent':>10} {'Δrel':>10} {'Δbug%':>10} {'Δres%':>10}")
all_repos = sorted(set(base_repos) | set(cur_repos))
for r in all_repos:
    b = base_repos.get(r, {})
    c = cur_repos.get(r, {})
    de = c.get("entities", 0) - b.get("entities", 0)
    dr = c.get("relationships", 0) - b.get("relationships", 0)
    db = (c.get("bug_rate", 0.0) - b.get("bug_rate", 0.0)) * 100
    dq = (c.get("resolution_rate", 0.0) - b.get("resolution_rate", 0.0)) * 100
    print(f"{r:<24} {de:>+10} {dr:>+10} {db:>+9.2f}% {dq:>+9.2f}%")

print()
print("## Aggregate deltas")
for k in sorted(set(base_agg) | set(cur_agg)):
    bv = base_agg.get(k, 0)
    cv = cur_agg.get(k, 0)
    if isinstance(bv, float) or isinstance(cv, float):
        print(f"{k:<24} {bv:.4%} -> {cv:.4%}  (Δ {(cv-bv)*100:+.4f} pp)")
    else:
        print(f"{k:<24} {bv} -> {cv}  (Δ {cv-bv:+d})")

print()
print("## Per-disposition deltas")
print(f"{'disposition':<20} {'baseline':>10} {'current':>10} {'Δ':>10}")
for k in DISPOSITIONS:
    bv = base_dispo.get(k, 0)
    cv = cur_dispo.get(k, 0)
    print(f"{k:<20} {bv:>10d} {cv:>10d} {cv-bv:>+10d}")
PY
