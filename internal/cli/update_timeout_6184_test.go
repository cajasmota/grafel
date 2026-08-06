package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// #6184: refreshWatcherUnitsWithNewBinary used cmd.CombinedOutput() with no
// deadline. At 140 repos the child makes hundreds of serial launchctl calls;
// with a wedged launchd (a documented failure mode), `grafel update` blocks
// indefinitely with no output at all, because CombinedOutput buffers until
// exit. This pins that a deadline is enforced and that a timeout is reported
// explicitly instead of inheriting silence.
//
// runWatcherRefreshCmd stands in for the wedged child: it blocks until either
// the context is cancelled (the fix working) or a bounded 5s "the child never
// died" ceiling, whichever comes first. That keeps this test's own worst case
// bounded even if the deadline enforcement regresses — no infinite runaway.
func TestRefreshWatcherUnits_TimesOutInsteadOfHangingForever(t *testing.T) {
	prevTimeout := watcherRefreshTimeout
	prevRun := runWatcherRefreshCmd
	watcherRefreshTimeout = 50 * time.Millisecond
	runWatcherRefreshCmd = func(ctx context.Context, bin string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return nil, nil
		}
	}
	t.Cleanup(func() {
		watcherRefreshTimeout = prevTimeout
		runWatcherRefreshCmd = prevRun
	})

	var buf bytes.Buffer
	start := time.Now()
	refreshWatcherUnitsWithNewBinary(&buf)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("refreshWatcherUnitsWithNewBinary did not return promptly on a wedged child "+
			"(#6184): took %s, want well under the 5s simulated hang", elapsed)
	}
	if !strings.Contains(strings.ToLower(buf.String()), "timed out") {
		t.Fatalf("expected an explicit timeout explanation instead of silence (#6184), got %q",
			buf.String())
	}
}
