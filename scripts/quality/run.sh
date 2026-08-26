#!/usr/bin/env bash
# Extraction-quality benchmark runner. Iterates over every fixture under
# internal/quality/golden/ and runs `grafel quality` against each,
# writing one JSON report per fixture into reports/quality/.
#
# Exit status:
#   0 — every fixture met its must-have recall + 0 forbidden hits
#   1 — runner setup / build error
#   2 — at least one fixture regressed (must-have miss or forbidden hit)
#   3 — at least one fixture directory produced NO measurement at all, so the
#       run is incomplete and no verdict is available for it (Refs #6273)
#
# 3 is separate from 2 on purpose. "this fixture ran and missed" and "this
# fixture never ran" are different facts and demand different work, and before
# #6273 the second was not a fact anyone was told. groovy-grails-mini and
# swift-swiftui-mini carried a src/ tree and no expected.json; `grafel quality`
# rejects such a directory before indexing (LoadFixture in
# internal/quality/expected.go reads expected.json and errors when it is
# absent), the `|| true` below swallowed that, and only the strict branch
# looked at the resulting per-fixture exit — the branch this same header
# records as never green, i.e. never run. Under --ratchet the two directories
# were skipped in silence and ratchet.py recorded them as an approved
# `expectations_missing` entry, so the benchmark's denominator was 18 while
# every figure quoted from it said 20.
#
# What exit 3 does NOT see: an expected.json that parses but declares zero
# must-haves. That fixture produces a report, so it is measured — it just cannot
# fail, which is the same silence wearing a different hat. The runner has no
# view on it; TestBaselineRecordedCountsAreSane and TestGoldenSetIsFullyGraded_6273
# in internal/quality/baseline_test.go and
# internal/quality/ungraded_fixture_6273_test.go both reject a gated fixture
# whose entity_expected and relationship_expected are BOTH zero. Verified by
# reading those two tests, not assumed.
#
# Intended to wire into the verify2 channel as a separate gate. Quality is
# orthogonal to bug-rate: we report both, and either can block a release.
#
# Two gates live here (Refs #6231):
#   default (strict)  — every fixture must hit 100% must-have recall. This has
#                       never been green across the full fixture set; it is the
#                       aspiration, kept so the real gap stays visible.
#   --ratchet         — each fixture must hold the recall recorded in
#                       internal/quality/golden/baseline.json. Drops fail as
#                       regressions; rises fail too, demanding the new figure be
#                       recorded. This is the gate that can actually be enforced.
#   --update-baseline — re-record baseline.json from this run.
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
#   GRAFEL_BIN   path to an already-built grafel binary to grade. It is used
#                as given and never rebuilt, so it grades whatever source that
#                binary came from — set it only when that is what you want.
#                Unset (the default), the working tree is rebuilt into
#                build/grafel on every run and that is what is graded (#6283).
#   QUALITY_OUT_DIR  directory to write per-fixture JSON reports into
#                    (default: reports/quality relative to repo root)
#
# Diagnosability (Refs #6573): each fixture's `grafel quality` stderr is captured
# to a scratch file instead of /dev/null. It stays invisible on a normal run; if
# EVERY fixture comes back UNMEASURED — the shape an environmental failure takes,
# e.g. a half-set isolation triple tripping #6331's guard — the first fixture's
# stderr is printed in full before the exit-3 bail, so the cause is named rather
# than left to be rediscovered by re-running one fixture by hand. This changes
# what the run SAYS, never what it DECIDES: exit codes, the ratchet, and the
# order of the exit-3 bail relative to the mode dispatch are untouched.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# Parse --runs flag; leave remaining positional args untouched.
# ---------------------------------------------------------------------------
RUNS=5
MODE=strict
args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ratchet|--update-baseline)
      # A typo'd or doubled mode flag must not silently resolve to something
      # else — QUALITY_MODE=ratchett quietly running the strict gate is how a
      # gate stops gating (Refs #6231).
      want="${1#--}"
      [[ "$want" == "update-baseline" ]] && want=update
      if [[ "$MODE" != "strict" && "$MODE" != "$want" ]]; then
        echo "error: --ratchet and --update-baseline are mutually exclusive" >&2
        exit 1
      fi
      MODE="$want"
      shift
      ;;
    --runs)
      RUNS="${2:?--runs requires an integer value}"
      shift 2
      ;;
    --runs=*)
      RUNS="${1#--runs=}"
      shift
      ;;
    *)
      # This runner takes no positional arguments. Silently ignoring an
      # unrecognised one is how `--rachet` runs the strict gate and nobody
      # notices (Refs #6231).
      echo "error: unrecognised argument '$1'" >&2
      echo "usage: run.sh [--runs N] [--ratchet | --update-baseline]" >&2
      exit 1
      ;;
  esac
done
unset args

if ! [[ "$RUNS" =~ ^[0-9]+$ ]] || [[ "$RUNS" -lt 1 ]]; then
  echo "error: --runs must be a positive integer (got '$RUNS')" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Isolation pre-flight (Refs #6331, #6573).
#
# internal/envguard refuses to run when exactly ONE of GRAFEL_HOME /
# GRAFEL_DAEMON_ROOT redirects: half an isolation is worse than none, because
# the store and the daemon plane then disagree about which home they live in.
# Every fixture trips that guard identically, so a half-set environment turns
# into 26 copies of "UNMEASURED" and no named cause — which is exactly the
# incident #6573 was filed from. Say it once, up front, instead.
#
# Advisory only: this warns, it never decides. GRAFEL_ALLOW_PARTIAL_ISOLATION=1
# downgrades the guard's refusal to a warning, in which case the run proceeds
# and this notice is the only thing that would have been wrong to act on.
# envguard remains the authority on the verdict.
#
# One normalisation is applied, and only to GRAFEL_HOME: envguard's
# defaultGrafelHome is $HOME/.grafel on every platform, so a plain
# `export GRAFEL_HOME=$HOME/.grafel` in a shell profile redirects nothing and
# reads as unset there too. That is the whole of the correspondence.
#
# GRAFEL_DAEMON_ROOT deliberately gets NO such treatment, because envguard's
# defaultDaemonRoot is NOT $HOME/.grafel: it is %APPDATA%\grafel on Windows,
# and "" on Unix whenever XDG_RUNTIME_DIR is set (which makes ANY value a
# redirect). Normalising it against $HOME/.grafel would have silently dropped
# the warning on exactly the platforms where envguard refuses — the #6134 shape.
#
# The rest of envguard's logic (XDG_RUNTIME_DIR, %APPDATA%, filepath.Clean, the
# real-HOME comparison) is NOT reimplemented, so this hint is coarser than the
# guard in both directions: a trailing slash (`$HOME/.grafel/`) warns where
# envguard would Clean it to OK. Advisory-only is what makes that acceptable —
# nothing observes this warning, envguard remains the authority on the verdict,
# and a coarse hint beats a comment claiming a fidelity it does not have.
# ---------------------------------------------------------------------------
_iso_home="${GRAFEL_HOME:-}"
_iso_root="${GRAFEL_DAEMON_ROOT:-}"
[[ -n "${HOME:-}" && "$_iso_home" == "$HOME/.grafel" ]] && _iso_home=""
if [[ -n "$_iso_home" && -z "$_iso_root" ]] || [[ -z "$_iso_home" && -n "$_iso_root" ]]; then
  echo "WARNING: PARTIALLY ISOLATED environment — every fixture is likely to fail identically." >&2
  echo "  GRAFEL_HOME=${GRAFEL_HOME:-<unset>}" >&2
  echo "  GRAFEL_DAEMON_ROOT=${GRAFEL_DAEMON_ROOT:-<unset>}" >&2
  echo "  Neither variable alone is isolation (#6331). Set all three, or none:" >&2
  echo "    export HOME=\$(mktemp -d); export GRAFEL_HOME=\$HOME/.grafel; export GRAFEL_DAEMON_ROOT=\$HOME/.grafel" >&2
  echo "  (GRAFEL_ALLOW_PARTIAL_ISOLATION=1 downgrades the guard's refusal to a warning.)" >&2
fi
unset _iso_home _iso_root

# ---------------------------------------------------------------------------
# Resolve the binary to grade (Refs #6283).
#
# This used to be one `if [[ ! -x "$BIN" ]]` around the build, so a binary
# already sitting at build/grafel was graded as-is however old it was. build/ is
# gitignored and long-lived on a developer machine, which is the only place this
# gate runs (.github/workflows/quality.yml is workflow_dispatch-only, and when
# dispatched it builds explicitly and passes GRAFEL_BIN), so the stale-binary
# window was a developer-machine window and nothing else.
#
# The cost was not just wrong figures. This repo leans on mutation testing to
# find vacuous tests, and a surviving mutant is only evidence of "no test covers
# this" if the graded artifact contains the mutation. #6283 was reported from
# exactly that: a benchmark run at HEAD and a run with
# internal/quality/diff.go's isPlaceholderAnchor mutated produced byte-identical
# reports, and the mutant was read as dead. (The report is the source for that
# episode; what is verified here is the mechanism — the old branch could not
# rebuild, so no source edit of any size could reach the graded binary.)
#
# Rebuild rather than detect-and-refuse. `go build` is the only staleness test
# that cannot be wrong or go out of date: it already knows every input, and this
# binary has a lot of them. `go list -deps -json ./cmd/grafel` on this tree
# reports 225 first-party packages contributing 1856 .go files, 875 embedded
# non-Go files (internal/engine/rules, internal/dashboard/dist,
# cmd/grafel/selftestdata, and skills/ via the module-root package in
# embedassets.go), 6 cgo and 10 C files — plus go.mod/go.sum for dependency
# versions. A hand-maintained mtime comparison over "*.go under cmd/ and
# internal/" misses every one of the non-.go inputs, and the next embed added
# would silently reopen this bug.
#
# Being exact is also close to free. A staleness check that did not rot would
# have to ask `go list -deps` what the inputs are, and a no-op `go build` is
# within a small constant factor of that same query on a warm cache — both are
# seconds, against a run that indexes 20 fixtures. Deliberately no figure here:
# two machines measured this and disagreed on the ratio (~1.2x vs ~2x) while
# agreeing the absolute cost is negligible, so a precise number in this comment
# would only be something for a third machine to contradict. Measure it yourself
# if you are re-litigating the choice; the argument does not rest on the margin.
#
# It is also what the sibling runners already do: scripts/verify2/run.sh and
# scripts/verify2/run-extended.sh both build unconditionally when GRAFEL_BIN is
# unset (verified by reading their binary-resolution blocks), and this script
# was the odd one out.
#
# GRAFEL_BIN is left alone: it names a binary the caller owns and built —
# quality.yml sets it to a path its own build step produced — and overwriting
# somebody else's path is not this script's business. It is validated instead,
# which the old single `if` did not do: an unexecutable GRAFEL_BIN used to build
# build/grafel and then exec the unusable $BIN regardless.
# ---------------------------------------------------------------------------
if [[ -n "${GRAFEL_BIN:-}" ]]; then
  BIN="$GRAFEL_BIN"
  if [[ ! -x "$BIN" ]]; then
    echo "error: GRAFEL_BIN=$BIN is not an executable file" >&2
    echo "  Unset GRAFEL_BIN to have this script build and grade the working tree." >&2
    exit 1
  fi
  echo "==> grading caller-supplied binary: $BIN" >&2
  echo "    (GRAFEL_BIN is set, so this script does not rebuild it — the numbers" >&2
  echo "     below describe whatever source that binary was built from.)" >&2
else
  BIN="$ROOT/build/grafel"
  mkdir -p "$(dirname "$BIN")"
  echo "==> building grafel -> $BIN" >&2
  go build -o "$BIN" ./cmd/grafel
fi

OUT="${QUALITY_OUT_DIR:-$ROOT/reports/quality}"
mkdir -p "$OUT"

# Freshness stamp (Refs #6231). $OUT is not cleared between runs and the default
# (reports/quality) is gitignored and long-lived on a developer machine — which
# is where this gate runs, since no workflow invokes it. Without a stamp, a run
# in which every fixture failed to produce a report grades last week's JSON and
# reports OK: point GRAFEL_BIN at a broken binary and 20/20 fixtures measure
# nothing while the ratchet says it held. Every report this run writes carries
# QUALITY_RUN_STAMP, and ratchet.py rejects any report not carrying this one.
QUALITY_RUN_STAMP="$(date +%s).$$"
export QUALITY_RUN_STAMP

EXIT=0
# Fixture directories that yielded no measurement this run. Collected rather
# than failed on immediately so one run reports every one of them (Refs #6273).
# A newline-delimited string plus a counter, not an array: `set -u` is on and
# ${#arr[@]} on an empty array is an unbound-variable error under bash 3.2,
# which is the /bin/bash macOS still ships.
UNMEASURED=""
UNMEASURED_N=0
# Total fixture directories seen this run. Only needed to tell "one fixture
# broke" from "the environment broke" (Refs #6573).
FIXTURE_N=0

# Per-fixture stderr captures (Refs #6573). `grafel quality`'s stderr used to go
# to /dev/null unconditionally, so the one artifact that names WHY a fixture
# failed was destroyed before anyone could read it. It is written to a scratch
# file instead and surfaced only on the all-UNMEASURED path, which keeps the
# normal summary as quiet as it was. Nothing survives the run: the directory is
# under mktemp and removed on exit — success and exit 3 alike (measured: 0
# residue in the real temp dir; note $TMPDIR cannot be used to scope such a
# measurement, macOS mktemp ignores it).
STDERR_DIR="$(mktemp -d)"
# CUR_TMPDIR is the per-fixture scratch dir while one is live, "" otherwise.
# One cleanup function owning both directories beats a trap that is swapped in
# and out around every fixture: there is exactly one place that can leak.
CUR_TMPDIR=""
# shellcheck disable=SC2329  # invoked via trap
cleanup_all() {
  if [[ -n "$CUR_TMPDIR" ]]; then rm -rf "$CUR_TMPDIR"; fi
  rm -rf "$STDERR_DIR"
}
# The INT/TERM traps are belt-and-braces, and the honest note is that they were
# NOT observed firing on the bash this repo actually runs on. macOS ships bash
# 3.2.57, and there a shell killed by SIGINT DOES run its EXIT trap (measured
# directly: a 4-line script with `trap cleanup EXIT` and a `sleep`, interrupted,
# prints "EXIT TRAP RAN" and exits 130). Residue was 0 on every path measured —
# exit 0, exit 3, and SIGINT — both here and on ae2dae99e. They are kept because
# that behaviour is bash's, not POSIX's, and a shell where EXIT does not run on
# a fatal signal would otherwise leak both directories. They clean up, restore
# the default disposition, and re-raise, so the caller still observes
# death-by-signal (128+N) rather than a tidy exit.
# shellcheck disable=SC2329  # invoked via trap
on_signal() {
  trap - EXIT INT TERM
  cleanup_all
  kill -"$1" $$
}
trap cleanup_all EXIT
trap 'on_signal INT' INT
trap 'on_signal TERM' TERM
# The fixture whose captured stderr gets printed if EVERY fixture came back
# UNMEASURED. First one wins: an environmental failure is the same failure 26
# times, and one sample is the whole diagnosis.
FIRST_ERR_NAME=""
FIRST_ERR_FILE=""

for fix in "$ROOT"/internal/quality/golden/*/ ; do
  name="$(basename "$fix")"
  FIXTURE_N=$((FIXTURE_N + 1))

  # A directory with no expected.json cannot be graded by anything. Say so and
  # move on: `grafel quality` would refuse it anyway (LoadFixture), so indexing
  # it buys nothing but wall-clock, and skipping the index keeps this check
  # answerable without a working binary.
  if [[ ! -f "$fix/expected.json" ]]; then
    echo "  UNMEASURED: $name has src/ but no expected.json — nothing graded it" >&2
    UNMEASURED="$UNMEASURED  - $name
"
    UNMEASURED_N=$((UNMEASURED_N + 1))
    continue
  fi

  echo "==> quality: $name  (runs=$RUNS)"

  # ------------------------------------------------------------------
  # Collect per-run JSON outputs in a temporary directory.
  # ------------------------------------------------------------------
  tmpdir="$(mktemp -d)"
  # Hand it to the run-level cleanup rather than installing a second trap: the
  # traps set above already fire on exit AND on INT/TERM, and swapping them per
  # fixture is how one of the two directories ends up uncovered.
  CUR_TMPDIR="$tmpdir"

  # One capture per fixture, appended across the repeat runs so a failure that
  # only shows up on run 3 is not overwritten by run 4's silence.
  errfile="$STDERR_DIR/$name.err"
  : > "$errfile"

  run_idx=0
  while [[ $run_idx -lt $RUNS ]]; do
    rjson="$tmpdir/run${run_idx}.json"
    # `grafel quality` exits 2 on regression but still writes the JSON.
    # We capture both outcomes — the median aggregator decides pass/fail.
    # stderr is captured, not discarded (Refs #6573): it is the only place the
    # binary explains itself, and throwing it away is what made an
    # environmental failure read as 26 identical content-free lines.
    "$BIN" quality --json "$rjson" "$fix" 2>>"$errfile" || true

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
# Freshness stamp — ratchet.py rejects reports not carrying the current run's
# stamp, so a stale file left in $OUT can never be graded as a live result.
merged["run_stamp"]                      = os.environ.get("QUALITY_RUN_STAMP", "")

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

  rm -rf "$tmpdir"
  CUR_TMPDIR=""

  # The aggregator above exits 1 for "no JSON reports produced" / "all JSON
  # reports unreadable" and 2 for "ran and missed". Only the second is a recall
  # verdict; the first means the fixture was not measured, which is fatal in
  # every mode — a ratchet cannot say a figure held when no figure was taken.
  if [[ $fixture_exit -eq 1 ]]; then
    echo "  UNMEASURED: $name produced no readable report — nothing was measured" >&2
    UNMEASURED="$UNMEASURED  - $name
"
    UNMEASURED_N=$((UNMEASURED_N + 1))
    if [[ -z "$FIRST_ERR_FILE" && -s "$errfile" ]]; then
      FIRST_ERR_NAME="$name"
      FIRST_ERR_FILE="$errfile"
    fi
    continue
  fi

  # In ratchet/update mode a per-fixture miss is not by itself fatal — the
  # ratchet decides, by comparing against the recorded baseline. In strict mode
  # any miss fails the run.
  if [[ $fixture_exit -ne 0 && "$MODE" == "strict" ]]; then
    EXIT=2
  fi
done

echo
echo "quality reports written to: $OUT"

# An incomplete run is not a run whose verdict is worth having. Bail before the
# ratchet rather than after: `check` would grade the fixtures that did report
# and print "N gated fixtures held their baseline" — a true sentence that reads
# as an all-clear — and `update` would re-record the gap as the new normal,
# which is exactly how the two ungraded directories survived every re-record
# since they were added.
if [[ $UNMEASURED_N -gt 0 ]]; then
  echo >&2
  echo "==> quality FAILED — $UNMEASURED_N fixture directory/ies produced no measurement:" >&2
  printf '%s' "$UNMEASURED" >&2
  echo >&2
  echo "  A fixture that was never graded is not a fixture that passed. Give it an" >&2
  echo "  expected.json, or move the directory out of internal/quality/golden/ —" >&2
  echo "  everything under golden/ is gated, there is no ungraded category." >&2

  # All of them, not some of them (Refs #6573). One fixture failing is a fixture
  # problem and its own stderr is a scroll away in $STDERR_DIR while the run
  # lives; EVERY fixture failing is the environment, and the operator should not
  # have to re-run one by hand with stderr attached to find that out. Print one
  # sample in full — 26 copies of the same environmental message is not 26 facts.
  if [[ $FIXTURE_N -gt 0 && $UNMEASURED_N -eq $FIXTURE_N && -n "$FIRST_ERR_FILE" && -s "$FIRST_ERR_FILE" ]]; then
    echo >&2
    echo "==> EVERY fixture ($UNMEASURED_N/$FIXTURE_N) was unmeasured, which is almost always" >&2
    echo "    environmental rather than a fixture problem. Full stderr from the first" >&2
    echo "    fixture, '$FIRST_ERR_NAME', verbatim:" >&2
    echo "    ------------------------------------------------------------------" >&2
    sed 's/^/    | /' "$FIRST_ERR_FILE" >&2
    echo "    ------------------------------------------------------------------" >&2
    echo "    (Command was: $BIN quality --json <tmp> <fixture>)" >&2
  fi
  exit 3
fi

BASELINE="$ROOT/internal/quality/golden/baseline.json"
GOLDEN="$ROOT/internal/quality/golden"
case "$MODE" in
  ratchet)
    python3 "$ROOT/scripts/quality/ratchet.py" check "$OUT" "$GOLDEN" "$BASELINE" || EXIT=$?
    ;;
  update)
    python3 "$ROOT/scripts/quality/ratchet.py" update "$OUT" "$GOLDEN" "$BASELINE" || EXIT=$?
    ;;
esac

exit "$EXIT"
