package main

import (
	"fmt"
	"math"
	"os"
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

// indexMemLimitFloorBytes is the smallest soft limit the ADAPTIVE policy will
// ever return. This floor is the anti-thrash guarantee and it is not
// negotiable on small-RAM hosts.
//
// GOMEMLIMIT is a SOFT limit: when it sits BELOW the workload's live heap the
// runtime cannot honor it and instead GCs continuously — a death spiral.
// Measured on the real corpus (#5954): GOMEMLIMIT=1200MiB against a ~2.7GB
// live heap made `index-internal` >4x slower (>16 min vs 235 s, never
// completed). By contrast 2500MiB cut peak RSS 4026MB -> 3203MB for +2 s
// (+1.7%). So a low-RAM machine must degrade to "generous limit" rather than
// to "strangled process": we never hand the indexer a limit under 2GiB.
const indexMemLimitFloorBytes int64 = 2 << 30 // 2 GiB

// indexMemLimitFraction is the share of AVAILABLE (not total) host RAM used as
// the soft limit when no override is set. On the measurement machine (~6GB
// available) 0.5 resolves to 3GiB — just above the measured 2500MiB sweet spot
// and comfortably above the ~2.7GB live heap, so the runtime trims the
// reclaimable arena without ever GC-thrashing.
const indexMemLimitFraction = 2 // divisor, i.e. 1/2 — integer math avoids float overflow on huge hosts

// resolveIndexMemLimit implements the adaptive policy:
//
//	max(2GiB, 0.5 * availableRAM)
//
// availableBytes == 0 means "available RAM could not be determined"; we return
// memLimitUnset rather than guessing, leaving the Go runtime default in place.
func resolveIndexMemLimit(availableBytes uint64) int64 {
	if availableBytes == 0 {
		return memLimitUnset
	}
	half := availableBytes / indexMemLimitFraction
	if half > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	if limit := int64(half); limit > indexMemLimitFloorBytes {
		return limit
	}
	return indexMemLimitFloorBytes
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
	return resolveIndexMemLimit(availableBytes), "adaptive (max(2GiB, 0.5*available))"
}

// applyIndexMemoryLimit sets the Go soft memory limit for THIS process (the
// `index-internal` child) before indexing starts, and logs the decision once so
// a support case can see it immediately (#5954).
//
// Measured on the real corpus: 4026MB -> 3203MB peak RSS for +2 s (+1.7%).
// See indexMemLimitFloorBytes for why the adaptive policy never goes low.
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
