#!/bin/bash
# #6199 measurement matrix driver.
set -u
# Sandbox root. Overridable so this is not pinned to one machine's scratchpad.
SB=${SB:-${TMPDIR:-/tmp}/grafel-6199}
WT=${WT:-$SB/wt-incrmeasure}
mkdir -p "$SB/home" "$SB/gh" "$SB/gdr"
# ALL THREE are required for isolation, and none of them is sufficient alone:
# GRAFEL_DAEMON_ROOT moves the socket/pid/log, GRAFEL_HOME moves the store, and
# HOME is what everything else falls back to. Dropping HOME here would let a
# measurement run write into the operator's real ~/.grafel.
export HOME=$SB/home GRAFEL_HOME=$SB/gh GRAFEL_DAEMON_ROOT=$SB/gdr GOMAXPROCS=4
FIX=${FIX:-$SB/fixture3k}
PRIMED=${PRIMED:-$SB/state-primed}
WORK=$SB/state-work
BACKUP=$SB/filebackup
TRACE=${TRACE:-$SB/trace-matrix.jsonl}
RSSLOG=${RSSLOG:-$SB/rss-matrix.tsv}
REPS=${REPS:-5}

# Deterministic, package-spread mutation target list.
LISTFILE=$SB/.allgo.txt
(cd "$FIX" && find pkg -name 'mod*.go' ! -name '*_test.go' | sort) > "$LISTFILE"
TOTAL=$(wc -l < "$LISTFILE" | tr -d ' ')

pick() { # pick N -> indices spread evenly across the file list (and packages)
  local n=$1
  [ "$n" -eq 0 ] && return
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
  [ -d "$BACKUP" ] || return
  for b in "$BACKUP"/*; do
    [ -e "$b" ] || continue
    safe=$(basename "$b"); f=${safe//__/\/}
    cp "$b" "$FIX/$f"
  done
  rm -rf "$BACKUP"
}

run_one() { # label N rep
  local label=$1 n=$2 rep=$3
  rm -rf "$WORK"; cp -R "$PRIMED" "$WORK"
  pick "$n" | mutate
  export GRAFEL_PHASE_TRACE=$TRACE GRAFEL_PHASE_TRACE_LABEL="$label/rep$rep"
  local t
  t=$( { /usr/bin/time -l "$WT/bin/incrdrive" -repo "$FIX" -state "$WORK" >/dev/null; } 2>&1 \
        | awk '/maximum resident set size/{print $1}' )
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
echo "done" >&2

# ---------------------------------------------------------------------------
# #6199 measurement recipe (what produced the matrix in the issue thread)
#
#   go run internal/extractors/testdata/incrfixture/main.go \
#       -out $SB/fixture3k -files 3000 -pkgs 60 -seed 1 -force
#   go build -o bin/grafel ./cmd/grafel
#   go build -o bin/incrdrive internal/extractors/testdata/incrdrive/main.go
#
#   # full reindex -> baseline time AND the state dir the incremental path needs
#   GRAFEL_HOME=$SB/gh GRAFEL_DAEMON_ROOT=$SB/gdr GOMAXPROCS=4 \
#     bin/grafel index-internal --repo $SB/fixture3k --out $SB/state-full/graph.fb
#
#   # prime the manifest: TryIncremental writes file-index.json on its first pass
#   cp -R $SB/state-full $SB/state-work
#   bin/incrdrive -repo $SB/fixture3k -state $SB/state-work
#   cp -R $SB/state-work $SB/state-primed
#
#   REPS=7 ./measure.sh n0:0 n1:1 n10:10 n50:50 n200:200
#   GRAFEL_INCREMENTAL_MAX_FILES=500 REPS=7 ./measure.sh n200forced:200
#
# ALWAYS use an isolated GRAFEL_HOME/GRAFEL_DAEMON_ROOT: none of this should go
# anywhere near a real ~/.grafel or a running daemon.
# ---------------------------------------------------------------------------
