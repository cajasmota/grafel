package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/process"
)

// indexMemLimitEnv is the operator escape hatch for the Go soft memory limit
// the `index-internal` child applies to itself. It accepts a byte size, either
// plain bytes ("2621440000") or a MiB/GiB suffix ("2500MiB", "3GiB"). The
// values "0", "off" and "disabled" mean "apply no limit at all" (leave the Go
// runtime default). A malformed value is ignored (logged, never fatal) and the
// adaptive policy applies instead. Added for #5954.
const indexMemLimitEnv = "GRAFEL_INDEX_MEMLIMIT"

// memLimitUnset is the sentinel for "do not call debug.SetMemoryLimit at all".
const memLimitUnset int64 = 0

// indexMemLimitSafeFloorBytes is the smallest soft limit we will ever apply.
// It is ~1.15x the measured ~2.7GB live-heap ceiling of a full corpus index,
// so the runtime always has room to actually REACH the target.
//
// This floor is the anti-thrash guarantee. GOMEMLIMIT is SOFT: set BELOW the
// live heap, the runtime cannot honor it and instead runs repeated full-heap
// mark cycles. Three things make that regime unbounded rather than merely
// slow, and none of them are obvious:
//
//  1. Go's 50% GC CPU limiter does NOT bound the damage. It throttles
//     mutator ASSISTS, but the mark cycles themselves are cache/TLB/
//     page-fault bound and are not accounted to GC CPU.
//  2. madvdontneed=1 (which we set on this child, see withMadvDontNeed)
//     COMPOUNDS it: freed pages are returned to the OS and then immediately
//     re-faulted on the next cycle.
//  3. The tree-sitter CGo arenas are invisible to GOMEMLIMIT, so the runtime
//     keeps GC'ing toward a target that non-Go memory guarantees it can
//     never reach.
//
// Measured on the real corpus (#5954): GOMEMLIMIT=1200MiB against a ~2.7GB
// live heap made `index-internal` >4x slower (>16 min vs 235 s) and never
// completed. So the trade is ASYMMETRIC — a too-low limit costs unbounded,
// non-terminating time; no limit at all costs a bounded +823MB. Never take
// the former to avoid the latter.
const indexMemLimitSafeFloorBytes int64 = 3 << 30 // 3 GiB

// indexMemLimitMinAvailableBytes is the amount of available RAM below which we
// do not attempt to bound the indexer AT ALL. Under 4GiB available there is no
// limit that is simultaneously above the safe floor and actually useful, so the
// honest answer is "unset": degrade to today's unbounded behavior rather than
// strangle the process. This is the small-host escape valve that the previous
// max(2GiB, available/2) policy got wrong — it kept returning a limit no matter
// how little RAM there was.
const indexMemLimitMinAvailableBytes uint64 = 4 << 30 // 4 GiB

// resolveIndexMemLimit implements the adaptive policy (#5954):
//
//	unset                            if available == 0 or available < 4GiB
//	max(3GiB, availableRAM * 3/4)    otherwise
//
// availableBytes == 0 means "available RAM could not be determined"; we return
// memLimitUnset rather than guessing, leaving the Go runtime default in place.
//
// Why 3/4 rather than 1/2: on the measurement host (4560MB available) 3/4
// yields 3420MB — safely ABOVE the ~2.7GB live heap while still ~600MB below
// the 4026MB unlimited baseline, so it trims the reclaimable arena without
// entering the thrash regime. 1/2 yielded 2280MB there, i.e. BELOW the live
// heap: the harmful case was the DEFAULT outcome on that machine, not an edge
// case. On a 4GB-available host the policy degrades to today's behavior; on a
// 32GB host it is non-binding, which is correct — the limit exists to trim the
// reclaimable arena, not to act as a quota.
//
// Integer math throughout: a float multiply here would be a needless source of
// rounding surprise in a value that gates a thrash cliff.
func resolveIndexMemLimit(availableBytes uint64) int64 {
	if availableBytes == 0 || availableBytes < indexMemLimitMinAvailableBytes {
		return memLimitUnset
	}
	// Overflow care: prefer the exact form, fall back to divide-first on
	// absurdly large inputs so the multiply cannot wrap.
	var target uint64
	if availableBytes <= math.MaxUint64/3 {
		target = availableBytes * 3 / 4
	} else {
		target = availableBytes / 4 * 3
	}
	if target > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	if limit := int64(target); limit > indexMemLimitSafeFloorBytes {
		return limit
	}
	return indexMemLimitSafeFloorBytes
}

// parseIndexMemLimitEnv parses the GRAFEL_INDEX_MEMLIMIT escape hatch.
//
// decided reports whether the operator expressed a usable intent:
//   - (n, true)              — apply n bytes verbatim (operator's explicit choice)
//   - (memLimitUnset, true)  — "0"/"off"/"disabled": apply no limit at all
//   - (0, false)             — unset or malformed: fall through to the adaptive policy
func parseIndexMemLimitEnv(raw string) (limit int64, decided bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	switch strings.ToLower(raw) {
	case "0", "off", "disabled", "false", "none":
		return memLimitUnset, true
	}

	mult := int64(1)
	digits := raw
	switch {
	case hasSuffixFold(raw, "gib"), hasSuffixFold(raw, "gb"):
		mult = 1 << 30
		digits = raw[:len(raw)-suffixLen(raw, "gib", "gb")]
	case hasSuffixFold(raw, "mib"), hasSuffixFold(raw, "mb"):
		mult = 1 << 20
		digits = raw[:len(raw)-suffixLen(raw, "mib", "mb")]
	case hasSuffixFold(raw, "b"):
		digits = raw[:len(raw)-1]
	}
	digits = strings.TrimSpace(digits)
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	if n == 0 {
		return memLimitUnset, true
	}
	if n > math.MaxInt64/mult {
		return 0, false
	}
	return n * mult, true
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

// suffixLen returns the length of whichever candidate suffix s actually ends
// with (longest first), so "4GiB" strips 3 chars and "4GB" strips 2.
func suffixLen(s string, candidates ...string) int {
	for _, c := range candidates {
		if hasSuffixFold(s, c) {
			return len(c)
		}
	}
	return 0
}

// indexMemLimitDecision combines the escape hatch and the adaptive policy.
// The env override always wins when it is well-formed; a malformed value falls
// back to the adaptive policy (and never panics). The returned source string is
// operator-facing — it goes straight into the one-shot log line.
func indexMemLimitDecision(rawEnv string, availableBytes uint64) (limit int64, source string) {
	if n, decided := parseIndexMemLimitEnv(rawEnv); decided {
		return n, indexMemLimitEnv + "=" + strings.TrimSpace(rawEnv) + " (env)"
	}
	if strings.TrimSpace(rawEnv) != "" {
		return resolveIndexMemLimit(availableBytes),
			"adaptive (ignored malformed " + indexMemLimitEnv + "=" + strings.TrimSpace(rawEnv) + ")"
	}
	return resolveIndexMemLimit(availableBytes), "adaptive (max(3GiB, 0.75*available); unset under 4GiB available)"
}

// applyIndexMemoryLimit sets the Go soft memory limit for THIS process (the
// `index-internal` child) before indexing starts, and logs the decision once so
// a support case can see it immediately (#5954).
//
// Measured on the real corpus: 4026MB -> 3203MB peak RSS for +2 s (+1.7%).
// See indexMemLimitSafeFloorBytes for why the adaptive policy would rather
// apply NO limit than apply a low one.
func applyIndexMemoryLimit() {
	availMB := process.AvailableMemoryMB()
	var availBytes uint64
	if availMB > 0 {
		availBytes = uint64(availMB) * 1024 * 1024
	}
	limit, source := indexMemLimitDecision(os.Getenv(indexMemLimitEnv), availBytes)
	if limit <= memLimitUnset {
		fmt.Fprintf(os.Stderr, "index-internal: Go soft memory limit not applied (#5954) source=%s available_mb=%d\n",
			source, availMB)
		return
	}
	debug.SetMemoryLimit(limit)
	fmt.Fprintf(os.Stderr, "index-internal: applied Go soft memory limit (#5954) limit_mb=%d available_mb=%d source=%s\n",
		limit/(1024*1024), availMB, source)
}

// gcThrashCPUThreshold is the cumulative GC CPU fraction above which a run is
// reported as suspicious. A healthy corpus index sits in the low single digits.
const gcThrashCPUThreshold = 0.30

// gcThrashWarning returns an operator-facing warning when a run finished with
// an implausible share of CPU spent in GC while a soft memory limit was in
// force — the fingerprint of the thrash regime described on
// indexMemLimitSafeFloorBytes. It returns "" when there is nothing to say.
//
// This is DIAGNOSIS ONLY and deliberately so: there is no runtime valve that
// re-tunes the limit mid-run. With the corrected policy (never a limit below
// the safe floor, and no limit at all under 4GiB available) there is nothing
// for a valve to rescue, and one would add a goroutine, thresholds, hysteresis
// and nondeterminism to the hot path for no measured benefit.
//
// When no limit was applied (limit == math.MaxInt64, the runtime default) we
// stay quiet: high GC CPU then has nothing to do with our tuning and pointing
// at GRAFEL_INDEX_MEMLIMIT would send a support case down the wrong path.
//
// The advice names BOTH knobs, and names the GC-percent one first, because
// since the GOGC cap landed that is the likelier cause. The soft limit is
// unreachable on every host that gets one at all (the adaptive policy returns
// at least 0.75*4GiB = 3GiB, always above the safe floor), whereas the GOGC
// cap binds on every background index by design and roughly doubles the number
// of mark cycles. Telling an operator to raise GRAFEL_INDEX_MEMLIMIT when
// GRAFEL_INDEX_GOGC is what is pacing the collector would waste their time.
func gcThrashWarning(limit int64, gcFraction float64) string {
	if limit <= 0 || limit == math.MaxInt64 {
		return ""
	}
	if gcFraction < gcThrashCPUThreshold {
		return ""
	}
	return fmt.Sprintf(
		"index-internal: WARNING gc_cpu_fraction=%.2f exceeded %.2f with go_soft_memory_limit_mb=%d (#5954). "+
			"Most likely the GC-percent cap is pacing the collector too aggressively for this repo: "+
			"raise GRAFEL_INDEX_GOGC (default %d), or set it to 'off'. "+
			"Less likely, the soft memory limit is below this repo's live heap, which makes the runtime "+
			"GC continuously: set GRAFEL_INDEX_MEMLIMIT to a larger value, or to 'off' to disable it entirely.",
		gcFraction, gcThrashCPUThreshold, limit/(1024*1024), indexGCPercentDefault)
}

// reportGCThrash samples GC CPU once, after indexing has finished, and logs a
// warning if the run looks like it fought the soft memory limit. One
// ReadMemStats at process end is cheap (a single stop-the-world on a process
// that is about to exit) and adds no goroutine.
func reportGCThrash() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if w := gcThrashWarning(debug.SetMemoryLimit(-1), ms.GCCPUFraction); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
}
