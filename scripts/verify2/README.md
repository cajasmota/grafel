# VERIFY-2 — bug-rate / resolution-rate harness

This directory hosts the `archigraph` indexer regression harness used to
measure the **bug-rate** and **resolution-rate** required by the v1.0
ship gate (Refs issue #58).

## What it measures

For each indexed repository, `archigraph index --json-stats` emits a
per-disposition tally for every relationship endpoint the resolver
inspected:

| disposition | meaning |
| --- | --- |
| `resolved` | stub rewritten to a 16-char entity ID — fully resolved |
| `external-known` | endpoint points at `ext:<pkg>` and `<pkg>` is on the static allowlist |
| `external-unknown` | endpoint points at `ext:<pkg>` but `<pkg>` is NOT on the allowlist |
| `dynamic` | reflective / dynamic-dispatch idiom; static resolution is impossible by design |
| `bug-extractor` | a stub references a Name with 0 emitted entities — extractor missed an emit |
| `bug-resolver`  | the Name exists in the graph, but the resolver couldn't disambiguate |
| `unclassified` | catch-all; any non-zero value warrants investigation |

The aggregate metrics:

- `bug_rate = (bug-extractor + bug-resolver) / total_endpoints`
- `resolution_rate = resolved / total_endpoints`

Ship-gate target (issue #44): **`bug_rate <= 1%`** across the full
extractor matrix.

## Filesystem layout

The harness writes **outside** the archigraph repo — corpora and reports
are large and not appropriate for git tracking.

- Corpus clones: `$ARCHIGRAPH_CORPORA_DIR/<repo-name>/`
- Reports: `$ARCHIGRAPH_CORPORA_DIR/_reports/<ISO-timestamp>.md`
- Built binary (cached): `$ARCHIGRAPH_CORPORA_DIR/_bin/archigraph`

`ARCHIGRAPH_CORPORA_DIR` defaults to
`$HOME/Documents/Projects/archigraph-corpora`.

## How to run

```bash
# Default — clones every configured repo and writes a fresh report.
scripts/verify2/run.sh

# Override the corpora dir.
ARCHIGRAPH_CORPORA_DIR=/tmp/ag-corp scripts/verify2/run.sh

# Forward verbose indexer logs.
ARCHIGRAPH_VERBOSE=1 scripts/verify2/run.sh

# Reuse a pre-built binary (skip the in-script `go build`).
ARCHIGRAPH_BIN=/usr/local/bin/archigraph scripts/verify2/run.sh
```

The script prints the full report path on stdout when it completes.

## How to compare two reports

```bash
scripts/verify2/compare.sh \
  ~/Documents/Projects/archigraph-corpora/_reports/2026-04-01T00-00-00Z.md \
  ~/Documents/Projects/archigraph-corpora/_reports/2026-05-09T00-00-00Z.md
```

The output shows per-repo entity/relationship deltas plus the change in
`bug_rate` and `resolution_rate` (both as percentage-point deltas).

## How to add a new corpus repo

Edit the `REPOS` array near the top of `run.sh`. Each entry is a
pipe-separated `<name>|<git-url>|<ref>|<primary-language>` quad. The
language tag is consumed by the per-language aggregate section in the
report:

```bash
REPOS=(
  "requests|https://github.com/psf/requests.git|main|python"
  "gin|https://github.com/gin-gonic/gin.git|master|go"
  ...
  "myrepo|https://github.com/owner/repo.git|main|typescript"
)
```

**Constraint:** only public OSS repositories. Private code, vendored
client trees, and internal codenames must never appear here — the
harness output is shared and tracked.

## How to interpret a report

Each report has the following sections (Refs #88):

1. **Per-repo results** — one row per indexed repo with
   files / entities / relationships / `bug_rate` / `resolution_rate`,
   followed by an **aggregate row** (`AGGREGATE`) summing across all
   repos at the bottom of the table.
2. **Per-repo disposition breakdown** — one sub-section per repo with a
   `| disposition | count | pct |` table covering `resolved`,
   `external-known`, `external-unknown`, `dynamic`, `bug-extractor`,
   `bug-resolver`, `unclassified`.
3. **Aggregate** — corpus-wide totals plus the global `bug_rate` and
   `resolution_rate`.
4. **Aggregate disposition breakdown** — combined endpoint counts +
   percentages across the entire corpus.
5. **Per-language aggregate** — repos grouped by primary language (set
   in the `REPOS` manifest in `run.sh`) with summed file / entity /
   relationship / endpoint counts plus per-language `bug_rate` and
   `resolution_rate`.
6. **Ship-gate check** — `PASS` when aggregate `bug_rate <= 1%`,
   otherwise `FAIL`.

If the corpus is empty (no clones present, or every clone indexed 0
files) `run.sh` exits with status 1 and writes nothing — empty reports
are never produced.

When `bug_rate` regresses, the `bug-extractor` and `bug-resolver`
buckets in the breakdown identify which side of the resolver split the
new failures landed in. Pair the report with `ARCHIGRAPH_VERBOSE=1` on a
single repo to print sample stub strings for those buckets — they point
directly at the missing extraction or the ambiguous-resolution case.

The `compare.sh` helper diffs two reports across per-repo metrics,
aggregate metrics, **and** per-disposition counts so disposition drift
between runs is visible at a glance.
