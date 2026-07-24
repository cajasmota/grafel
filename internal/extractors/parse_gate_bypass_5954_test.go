package extractors_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestExtract_InlineParseIsGated proves that the extractor-level parses which
// BYPASS treesitter.ParserFactory are nonetheless bounded by the #5630 parse
// gate.
//
// WHY THIS MATTERS (#5954). The in-process parse ceiling is only real if every
// path to ts_parser_parse goes through AcquireParseSlot — GOMAXPROCS cannot
// bound cgo, so an ungated path is an unbounded path. Two families bypassed the
// factory (and therefore also bypassed the old parseMu):
//
//   - each extractor's `if file.TSTree == nil { NewParser… }` inline parse,
//     which fires in production whenever the pipeline's own parse failed — i.e.
//     exactly on the pathological/oversized files that most need capping;
//   - internal/extractors/yaml/helm.go, which re-parses every Helm template's
//     stripped content unconditionally.
//
// The assertion is blocking, not timing-based: with cap=1 and the only slot
// held, Extract must not finish; once released, it must.
func TestExtract_InlineParseIsGated(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	indexstate.SetParseConcurrency(1)
	indexstate.AcquireParseSlot() // hold the only slot

	// TSTree deliberately nil so the extractor takes its inline-parse path.
	file := extractor.FileInput{
		Path:     "svc/models.py",
		Content:  []byte("class Foo:\n    def bar(self):\n        return 1\n"),
		Language: "python",
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = extractors.Extract(context.Background(), file)
	}()

	select {
	case <-done:
		indexstate.ReleaseParseSlot()
		t.Fatal("extractor inline parse completed while the only parse slot was held — " +
			"this path bypasses the parse gate, so the in-process parse ceiling is illusory (#5954)")
	case <-time.After(200 * time.Millisecond):
		// Expected: blocked in AcquireParseSlot, before the cgo parse.
	}

	indexstate.ReleaseParseSlot()
	select {
	case <-done:
		// Expected.
	case <-time.After(20 * time.Second):
		t.Fatal("extractor did not proceed after the slot was released")
	}
}
