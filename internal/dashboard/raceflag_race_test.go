//go:build race

package dashboard

// raceEnabled reports whether this test binary was built with -race.
//
// TestBackgroundAlgoGate_ClearedBetweenCheckAndUse needs it because the two
// modes sample the check-then-use window at ~30x different rates AND with
// different per-traversal hit probabilities: -race widens the window (so it
// needs far fewer traversals) while also making each traversal ~30x slower.
// A single traversal target therefore cannot serve both — too low and the
// no-race arm stops detecting, too high and the -race arm takes 30+ seconds.
//
// This is a test-only build flag. It does not weaken, skip, or suppress
// anything: both modes still run the same assertions to the same conclusion.
const raceEnabled = true
