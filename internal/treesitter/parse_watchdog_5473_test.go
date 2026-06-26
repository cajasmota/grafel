package treesitter_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// bigGoSource builds a large but syntactically valid Go file whose parse takes
// far longer than a sub-millisecond budget, so a tiny GRAFEL_PARSE_TIMEOUT
// halts it deterministically (the C deadline is checked periodically from the
// main parse loop, which a 600KB+ input cycles through many times). This is the
// "controllable input" stand-in for a real runaway — we make the parse provably
// exceed the deadline without depending on a pathological grammar/input.
func bigGoSource(terms int) []byte {
	var b strings.Builder
	b.WriteString("package p\n\nvar x = 1")
	for i := 0; i < terms; i++ {
		b.WriteString(" + 1")
	}
	b.WriteString("\n")
	return []byte(b.String())
}

// TestParse_WatchdogHaltsRunawayAndReleasesLock proves the #5473 safety net:
//  1. a parse that exceeds the per-parse deadline returns a BOUNDED, sentinel
//     error (official.ErrParseDeadlineExceeded) instead of hanging, and
//  2. parseMu is released afterwards — a subsequent normal parse succeeds
//     (a leaked lock would deadlock here, which the timed wait surfaces as a
//     clear failure rather than a hang).
func TestParse_WatchdogHaltsRunawayAndReleasesLock(t *testing.T) {
	// Arm the watchdog tightly. 1ms is comfortably above tree-sitter's deadline
	// granularity (the binding's own timeout tests use 1000µs) yet far below the
	// time to parse the large input below, so the halt is deterministic.
	t.Setenv("GRAFEL_PARSE_TIMEOUT", "1ms")

	f := treesitter.NewParserFactory(nil)
	big := bigGoSource(200_000)

	// The runaway parse must return promptly with the bounded sentinel error,
	// never hang. Run it in a goroutine and fail loudly if it does not return.
	type result struct {
		res *treesitter.ParseResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := f.Parse(context.Background(), big, "go")
		done <- result{res, err}
	}()

	var got result
	select {
	case got = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("watchdog parse did not return — the deadline did not halt the parse (hang)")
	}
	if got.err == nil {
		t.Fatalf("expected a bounded watchdog error, got nil (res=%+v)", got.res)
	}
	if !errors.Is(got.err, official.ErrParseDeadlineExceeded) {
		t.Fatalf("expected error to wrap official.ErrParseDeadlineExceeded, got: %v", got.err)
	}
	if got.res != nil {
		t.Fatalf("watchdog halt must yield no ParseResult, got: %+v", got.res)
	}

	// Lock-release invariant: with a normal budget restored, a subsequent parse
	// must succeed. If parseMu had leaked, this would block forever.
	t.Setenv("GRAFEL_PARSE_TIMEOUT", "20s")
	next := make(chan result, 1)
	go func() {
		res, err := f.Parse(context.Background(), []byte("package main\nfunc main() {}\n"), "go")
		next <- result{res, err}
	}()
	select {
	case got = <-next:
	case <-time.After(15 * time.Second):
		t.Fatal("subsequent parse hung — parseMu was not released after the watchdog halt")
	}
	if got.err != nil {
		t.Fatalf("subsequent normal parse failed: %v", got.err)
	}
	if got.res == nil || got.res.TSTree == nil {
		t.Fatalf("subsequent normal parse produced no tree: %+v", got.res)
	}
	got.res.TSTree.Close()
}

// TestParse_NormalParseUnaffectedByWatchdog confirms a healthy parse completes
// well within the deadline and is not disturbed by the watchdog: no error, a
// real tree, and elapsed time far below the configured budget.
func TestParse_NormalParseUnaffectedByWatchdog(t *testing.T) {
	t.Setenv("GRAFEL_PARSE_TIMEOUT", "20s")

	f := treesitter.NewParserFactory(nil)
	src := []byte("package main\n\nfunc add(a, b int) int { return a + b }\n")

	start := time.Now()
	res, err := f.Parse(context.Background(), src, "go")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("normal parse returned an error: %v", err)
	}
	if res == nil || res.TSTree == nil {
		t.Fatalf("normal parse produced no tree: %+v", res)
	}
	defer res.TSTree.Close()
	if res.NodeCount == 0 {
		t.Fatalf("normal parse produced an empty tree (NodeCount=0)")
	}
	if elapsed >= 20*time.Second {
		t.Fatalf("normal parse took %v — should be well under the deadline", elapsed)
	}
}
