package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// indexGCPercentEnv is the operator escape hatch for the GC pacing the
// `index-internal` child applies to itself. It accepts a positive integer
// percentage, optionally with a trailing "%" ("50", "35", "60%"). The values
// "0", "off", "disabled", "none" and "false" mean "apply no cap at all" (leave
// whatever the Go runtime already has). A malformed value is ignored (logged,
// never fatal) and the policy default applies instead. Added for #5954,
// naming and precedence mirror GRAFEL_INDEX_MEMLIMIT.
const indexGCPercentEnv = "GRAFEL_INDEX_GOGC"

// gcPercentUnset is the sentinel for "do not call debug.SetGCPercent at all".
// It is distinct from a GOGC of 0 (which the runtime would read as "collect
// continuously"); we never apply that, and the parser maps "0" to this
// sentinel precisely so the two cannot be confused.
const gcPercentUnset = 0

// backgroundIndexCommand is the argv[1] of the hidden subprocess-indexer
// entrypoint the daemon fork-execs. It is the ONLY command whose GC pacing we
// cap: background indexing is a batch job nobody is watching, so trading GC
// CPU for RSS is free there. Interactive/foreground work stays uncapped by
// standing product decision — a user waiting on a command should not pay GC
// time to save memory they are not short of.
const backgroundIndexCommand = "index-internal"

// indexGCPercentDefault is the GC pacing applied to the background index
// child: next_gc = live * 1.5 instead of the default live * 2.
//
// WHY GOGC AND NOT GOMEMLIMIT — this is the crux, and the two are not
// interchangeable. GOMEMLIMIT is an ABSOLUTE soft target. Set below the
// workload's live heap it is unsatisfiable, and the runtime answers by running
// mark cycles back to back forever; this repo measured GOMEMLIMIT=1200MiB
// against a ~1.7GB live heap running >4x slower and never completing, which is
// the entire reason indexMemLimitSafeFloorBytes exists. GOGC is a RELATIVE
// target — next_gc = live * (1 + GOGC/100) — so it is defined in terms of the
// live heap and therefore CANNOT fall below it, at any value, on any repo.
// The death-spiral regime is unreachable by construction, not by tuning. That
// structural difference is why the cap belongs on GOGC.
//
// WHY 50 — measured over 6 full corpus runs (.claude/dev/perf/runs, field
// next_gc in child.ndjson): the live-heap peak is ~1707MB, in `materializing`.
// Live heap is next_gc/2 at GOGC=100, NOT heap_inuse (3363-3449MB) and not
// post-GC heap_alloc (up to 2285MB) — both of those include uncollected
// garbage, and reading a "live heap" off them is the category error that set
// the old 3GiB safe floor. At GOGC=100 that ~1707MB live heap carried a
// 3679MB heap_sys; at 50 the steady-state target is ~2560MB. It also matches
// the value the daemon already runs at (see runDaemonMode), so the two
// long-lived Go processes in the product are paced the same way.
const indexGCPercentDefault = 50

// parseIndexGCPercentEnv parses the GRAFEL_INDEX_GOGC escape hatch.
//
// decided reports whether the operator expressed a usable intent:
//   - (n, true)               — apply n percent verbatim
//   - (gcPercentUnset, true)  — "0"/"off"/"disabled": apply no cap at all
//   - (0, false)              — unset or malformed: fall through to the policy
//
// A negative value is deliberately MALFORMED rather than passed through.
// debug.SetGCPercent(-1) disables the collector entirely; a knob whose whole
// purpose is to lower peak RSS must not be a way to switch GC off by typo.
func parseIndexGCPercentEnv(raw string) (percent int, decided bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	switch strings.ToLower(raw) {
	case "0", "off", "disabled", "false", "none":
		return gcPercentUnset, true
	}
	digits := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	if n == 0 {
		return gcPercentUnset, true
	}
	return n, true
}

// indexGCPercentDecision combines the path guard, the escape hatch and the
// policy default. The returned source string is operator-facing — it goes
// straight into the one-shot log line.
//
// Precedence, highest first:
//
//  1. command is not the background index child -> never capped. This gate is
//     first on purpose: GRAFEL_INDEX_GOGC tunes the background cap, it does
//     not create one on the interactive path.
//  2. GRAFEL_INDEX_GOGC, when well-formed -> the operator's explicit choice.
//  3. GOGC set explicitly in the environment -> leave the runtime alone. The
//     runtime has already applied it; overriding would silently discard an
//     instruction the operator gave in the most standard way there is.
//  4. otherwise -> indexGCPercentDefault.
func indexGCPercentDecision(command, rawEnv, rawGOGC string) (percent int, source string) {
	if command != backgroundIndexCommand {
		return gcPercentUnset, "interactive/foreground path (" + command + ") — GC pacing is not capped"
	}
	if n, decided := parseIndexGCPercentEnv(rawEnv); decided {
		return n, indexGCPercentEnv + "=" + strings.TrimSpace(rawEnv) + " (env)"
	}
	malformed := ""
	if strings.TrimSpace(rawEnv) != "" {
		malformed = " (ignored malformed " + indexGCPercentEnv + "=" + strings.TrimSpace(rawEnv) + ")"
	}
	if strings.TrimSpace(rawGOGC) != "" {
		return gcPercentUnset, "GOGC=" + strings.TrimSpace(rawGOGC) + " set by operator, left as-is" + malformed
	}
	return indexGCPercentDefault, "policy default" + malformed
}

// applyIndexGCPercent sets the GC pacing for THIS process before indexing
// starts, and logs the decision once so a support case can see it immediately
// (#5954).
//
// Scope is the WHOLE child rather than just the memory-heavy phases
// (materializing / computing_flows). Per-phase scoping would need a
// set-and-restore pair threaded through Index() for a process-wide runtime
// side effect, and would buy nothing: the child does one indexing run and then
// exits, every earlier phase also allocates heavily (extracting_ast peaks at
// ~1.9GB RSS), and a tighter GOGC in those phases is just as welcome. There is
// no "after" to restore to, so there is no restore path.
func applyIndexGCPercent(command, rawEnv, rawGOGC string) {
	percent, source := indexGCPercentDecision(command, rawEnv, rawGOGC)
	if percent == gcPercentUnset {
		fmt.Fprintf(os.Stderr, "index-internal: GC percent not capped (#5954) source=%s\n", source)
		return
	}
	prev := debug.SetGCPercent(percent)
	fmt.Fprintf(os.Stderr, "index-internal: applied GC percent (#5954) gogc=%d previous=%d source=%s\n",
		percent, prev, source)
}
