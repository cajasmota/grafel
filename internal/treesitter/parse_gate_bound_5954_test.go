package treesitter_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/treesitter"
)

// TestParse_IsBoundedByParseGate proves that the #5630 parse gate is a real
// ceiling on ParserFactory.Parse — i.e. that removing parseMu (#5954) did not
// leave concurrent ts_parser_parse unbounded.
//
// WHY A DEDICATED TEST. The gate's own semantics are pinned in
// internal/indexstate (TestParseGateCap), and TestParse_RegistersBusyCounter
// pins that Parse touches the accounting. Neither proves the property that
// actually matters here: that the semaphore is held ACROSS the cgo parse, so a
// held slot genuinely blocks a parse from starting. That is the whole safety
// argument for the removal — GOMAXPROCS cannot bound cgo (a goroutine inside a
// cgo call parks in _Gsyscall and yields its P), so if Parse were not gated,
// nothing would cap concurrent C parsing on the non-daemon index path.
//
// The assertion is a blocking one, not a timing-of-work one, so it is not
// flaky: with cap=1 and the only slot held by the test, Parse must not
// complete; once released, it must.
func TestParse_IsBoundedByParseGate(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	indexstate.SetParseConcurrency(1)
	if got := indexstate.ParseConcurrencyCap(); got != 1 {
		t.Fatalf("cap = %d, want 1", got)
	}

	// Hold the single slot.
	indexstate.AcquireParseSlot()

	f := treesitter.NewParserFactory(nil)
	done := make(chan struct{})
	go func() {
		res, err := f.Parse(context.Background(), []byte("package p\nfunc F() int { return 1 }\n"), "go")
		if err == nil && res != nil && res.TSTree != nil {
			res.TSTree.Close()
		}
		close(done)
	}()

	select {
	case <-done:
		indexstate.ReleaseParseSlot()
		t.Fatal("Parse completed while the only parse slot was held — the gate does " +
			"NOT bound ts_parser_parse, so concurrent C parsing is uncapped (#5954)")
	case <-time.After(200 * time.Millisecond):
		// Expected: blocked in AcquireParseSlot, before the cgo parse.
	}

	indexstate.ReleaseParseSlot()
	select {
	case <-done:
		// Expected: the freed slot let the parse run.
	case <-time.After(15 * time.Second):
		t.Fatal("Parse did not proceed after the slot was released")
	}
}

// TestParse_UnboundedGateStillParses guards the other direction: with no cap
// installed (a bare library caller / most tests) Parse must not block. The gate
// is a ceiling, never a dependency.
func TestParse_UnboundedGateStillParses(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })
	indexstate.SetParseConcurrency(0)

	f := treesitter.NewParserFactory(nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			res, err := f.Parse(context.Background(), []byte("package p\nfunc F() int { return 1 }\n"), "go")
			if err != nil {
				t.Errorf("parse %d failed: %v", i, err)
				return
			}
			if res != nil && res.TSTree != nil {
				res.TSTree.Close()
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("parses hung with an unbounded gate")
	}
}
