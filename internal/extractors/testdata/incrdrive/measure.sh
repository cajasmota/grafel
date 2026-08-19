#!/bin/bash
# #6199 measurement matrix driver.
#
# ---------------------------------------------------------------------------
# Recipe (this is the whole thing; run it from the repo root)
#
#   SB=${TMPDIR:-/tmp}/grafel-6199
#   mkdir -p "$SB/home" "$SB/gh" "$SB/gdr"
#   export HOME=$SB/home GRAFEL_HOME=$SB/gh GRAFEL_DAEMON_ROOT=$SB/gdr GOMAXPROCS=4
#
#   go run internal/extractors/testdata/incrfixture/main.go \
#       -out "$SB/fixture3k" -files 3000 -pkgs 60 -seed 1 -force
#   go build -o bin/grafel ./cmd/grafel
#
#   # full reindex -> baseline time AND the state dir the incremental path needs
#   bin/grafel index-internal --repo "$SB/fixture3k" --out "$SB/state-full/graph.fb"
#
#   # prime the manifest: TryIncremental writes file-index.json on its first pass
#   cp -R "$SB/state-full" "$SB/state-work"
#   internal/extractors/testdata/incrdrive/measure.sh --prime
#   cp -R "$SB/state-work" "$SB/state-primed"
#
#   REPS=7 internal/extractors/testdata/incrdrive/measure.sh n0:0 n1:1 n10:10 n50:50 n200:200
#   GRAFEL_INCREMENTAL_MAX_FILES=500 REPS=7 \
#       internal/extractors/testdata/incrdrive/measure.sh n200forced:200
#
# ALL THREE of HOME, GRAFEL_HOME and GRAFEL_DAEMON_ROOT must be isolated, and
# none of them is sufficient alone: GRAFEL_DAEMON_ROOT moves the socket/pid/log,
# GRAFEL_HOME moves the store, and HOME is what everything else falls back to.
# Dropping HOME lets a measurement run write into the operator's real ~/.grafel
# or disturb a running daemon. The script re-exports all three below, but the
# `bin/grafel index-internal` step above runs outside it, so the export must be
# in the operator's shell too — that is why it is the first line of the recipe.
# ---------------------------------------------------------------------------
set -euo pipefail

# Sandbox root. Overridable so this is not pinned to one machine's scratchpad.
SB=${SB:-${TMPDIR:-/tmp}/grafel-6199}
REPO=${REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}
mkdir -p "$SB/home" "$SB/gh" "$SB/gdr" "$SB/bin"
export HOME=$SB/home GRAFEL_HOME=$SB/gh GRAFEL_DAEMON_ROOT=$SB/gdr GOMAXPROCS=4
FIX=${FIX:-$SB/fixture3k}
PRIMED=${PRIMED:-$SB/state-primed}
WORK=$SB/state-work
BACKUP=$SB/filebackup
TRACE=${TRACE:-$SB/trace-matrix.jsonl}
RSSLOG=${RSSLOG:-$SB/rss-matrix.tsv}
REPS=${REPS:-5}
DRIVER=$SB/bin/incrdrive

die() { printf "measure.sh: %b\n" "$*" >&2; exit 1; }

# --- the driver ------------------------------------------------------------
# One process = one TryIncremental pass, deliberately: a fresh process gives
# each pass a clean heap, so the GRAFEL_PHASE_TRACE heap peak is that pass's own
# and not the high-water mark of every pass before it. /usr/bin/time -l reads
# maximum RSS off the same process, which is why this cannot be a `go test`.
#
# The source is materialised and built HERE rather than checked in, because a
# checked-in file under testdata/ is never compiled by `go build ./...`,
# `go vet ./...` or the test suite, and would rot silently the first time
# TryIncremental's signature moved. Built at measurement time, that same change
# is a loud compile error in front of the person doing the measuring.
build_driver() {
  local src=$REPO/internal/extractors/testdata/incrdrive/.incrdrive_gen
  rm -rf "$src"; mkdir -p "$src"
  cat > "$src/main.go" <<'GO'
// Command incrdrive runs exactly one extractors.TryIncremental pass against an
// already-indexed repo and prints the Result as JSON (issue #6199).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cajasmota/grafel/internal/extractors"
)

func main() {
	repo := flag.String("repo", "", "repository path")
	state := flag.String("state", "", "state dir holding graph.fb + file-index.json")
	quiet := flag.Bool("quiet", true, "discard the incremental logger's own output")
	flag.Parse()
	if *repo == "" || *state == "" {
		fmt.Fprintln(os.Stderr, "incrdrive: -repo and -state are required")
		os.Exit(2)
	}
	sink := io.Writer(os.Stderr)
	if *quiet {
		sink = io.Discard
	}
	res := extractors.TryIncremental(context.Background(), *repo, *state, log.New(sink, "", 0), nil)
	b, _ := json.Marshal(map[string]any{
		"done":            res.Done,
		"fallback_reason": res.FallbackReason,
		"changed_files":   res.ChangedFiles,
		"duration_ms":     res.Duration.Milliseconds(),
	})
	fmt.Println(string(b))
}
GO
  local rc=0
  ( cd "$REPO" && go build -o "$DRIVER" "$src/main.go" ) || rc=$?
  rm -rf "$src"
  [ "$rc" -eq 0 ] || die "driver build failed — TryIncremental's signature probably moved"
  [ -x "$DRIVER" ] || die "driver was not produced at $DRIVER"
}

build_driver

# --prime: build the driver and run a single pass to write file-index.json.
if [ "${1:-}" = "--prime" ]; then
  [ -d "$FIX" ] || die "fixture $FIX does not exist (run the incrfixture step first)"
  [ -d "$WORK" ] || die "$WORK does not exist (cp -R \$SB/state-full \$SB/state-work first)"
  "$DRIVER" -repo "$FIX" -state "$WORK"
  exit 0
fi

[ $# -gt 0 ] || die "usage: measure.sh --prime | measure.sh label:N [label:N ...]"
[ -d "$FIX" ] || die "fixture $FIX does not exist (run the incrfixture step first)"
[ -d "$PRIMED" ] || die "primed state $PRIMED does not exist (run measure.sh --prime first)"

# Deterministic, package-spread mutation target list.
LISTFILE=$SB/.allgo.txt
(cd "$FIX" && find pkg -name 'mod*.go' ! -name '*_test.go' | sort) > "$LISTFILE"
TOTAL=$(wc -l < "$LISTFILE" | tr -d ' ')
[ "$TOTAL" -gt 0 ] || die "no mutation targets found under $FIX/pkg"

pick() { # pick N -> indices spread evenly across the file list (and packages)
  local n=$1
  [ "$n" -eq 0 ] && return 0
  awk -v n="$n" -v total="$TOTAL" 'BEGIN{step=int(total/n); if(step<1)step=1}
    (NR-1)%step==0 && c<n {print; c++}' "$LISTFILE"
}

mutate() {
  local f
  mkdir -p "$BACKUP"
  while read -r f; do
    [ -z "$f" ] && continue
    local safe=${f//\//__}
    cp "$FIX/$f" "$BACKUP/$safe"
    printf '\n// mutation for #6199 measurement\nfunc MeasureMutation%s(x int) int {\n\treturn x + 1\n}\n' \
      "$(echo "$f" | tr -cd '0-9')" >> "$FIX/$f"
  done
}

restore_files() {
  local b safe f
  [ -d "$BACKUP" ] || return 0
  for b in "$BACKUP"/*; do
    [ -e "$b" ] || continue
    safe=$(basename "$b"); f=${safe//__/\/}
    cp "$b" "$FIX/$f"
  done
  rm -rf "$BACKUP"
}
# The fixture must be put back even if a run dies mid-matrix, or every later
# label measures a fixture carrying the previous label's mutations.
trap 'restore_files' EXIT

run_one() { # label N rep
  local label=$1 n=$2 rep=$3
  rm -rf "$WORK"; cp -R "$PRIMED" "$WORK"
  pick "$n" | mutate
  export GRAFEL_PHASE_TRACE=$TRACE GRAFEL_PHASE_TRACE_LABEL="$label/rep$rep"
  local raw t
  # Do NOT let a failed pass write a blank RSS row and carry on: an empty
  # column read as "0 kB" is how a harness reports success on a run that never
  # happened.
  raw=$( { /usr/bin/time -l "$DRIVER" -repo "$FIX" -state "$WORK" >/dev/null; } 2>&1 ) \
    || die "$label rep$rep: the driver exited non-zero:\n$raw"
  t=$(echo "$raw" | awk '/maximum resident set size/{print $1}')
  [ -n "$t" ] || die "$label rep$rep: no 'maximum resident set size' in /usr/bin/time output:\n$raw"
  printf '%s\t%s\t%s\t%s\n' "$label" "$n" "$rep" "$t" >> "$RSSLOG"
  restore_files
}

: > "$RSSLOG"
for spec in "$@"; do
  label=${spec%%:*}; n=${spec##*:}
  for ((r=1;r<=REPS;r++)); do
    echo "== $label n=$n rep=$r" >&2
    run_one "$label" "$n" "$r"
  done
done
echo "done: $(wc -l < "$RSSLOG" | tr -d ' ') rows in $RSSLOG, phase traces in $TRACE" >&2
