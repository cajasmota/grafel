package mcp

import (
	"sync/atomic"
	"time"
)

// nowFunc is the clock a handler reads when it needs the current instant.
type nowFunc func() time.Time

// nowOverride holds the TEST-ONLY clock seam. nil (the zero value) means "use
// the real clock"; production never stores anything here.
//
// #6073: handlers that stamp a wall-clock value into their result cannot be
// asserted byte-for-byte against each other, because two sequential calls can
// straddle a second boundary (the format used by handleSaveResult's filename is
// second-resolution). Injecting a clock lets a test pin both calls to the SAME
// instant, which makes the dispatch comparison exact and total instead of
// normalising the field away — a normalised field is checked in neither
// direction.
//
// #6056/#6059: this MUST stay an atomic. MCP tool handlers run on live,
// concurrent request goroutines, so a test's t.Cleanup restore would race any
// in-flight handler reading a plain package var. atomic.Pointer removes the
// unsynchronised access form entirely, so the protection cannot be silently
// dropped by a caller that forgets to quiesce the server first. See
// internal/daemon/supervise.go (engineChildCommandOverride) and
// internal/dashboard/graphstate.go (backgroundAlgoGate) for the same shape.
var nowOverride atomic.Pointer[nowFunc]

// serverNow resolves the effective clock: the injected test clock when one is
// installed, else the real wall clock. Always UTC, so callers can format
// directly without re-normalising the location.
func serverNow() time.Time {
	if fn := nowOverride.Load(); fn != nil {
		return (*fn)().UTC()
	}
	return time.Now().UTC()
}

// setNowOverride installs (fn != nil) or clears (fn == nil) the clock seam.
// TEST-ONLY; never called from production. Tests should prefer the
// t.Cleanup-scoped wrapper setFixedNowForTest (clock_test.go).
func setNowOverride(fn func() time.Time) {
	if fn == nil {
		nowOverride.Store(nil)
		return
	}
	f := nowFunc(fn)
	nowOverride.Store(&f)
}
