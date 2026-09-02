package extractors

import (
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// GraphGenWriter is the signature of fbwriter.WriteGraphGenReport.
type GraphGenWriter = func(stateDir string, doc *graph.Document) (string, fbwriter.UndeclaredKindReport, error)

// SwapWriteGraphGen replaces the graph-write seam and returns both a restore
// func and the previous writer, so a test can wrap the real one rather than
// replace it. It exists only for the external extractors_test package, which
// holds this package's TryIncremental fixtures but cannot reach an unexported
// var (#6212).
func SwapWriteGraphGen(fn GraphGenWriter) (restore func(), prev GraphGenWriter) {
	prev = writeGraphGen
	writeGraphGen = fn
	return func() { writeGraphGen = prev }, prev
}
