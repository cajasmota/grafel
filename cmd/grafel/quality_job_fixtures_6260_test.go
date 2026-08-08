package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/quality"
)

// Absolute must-have recall for the background-job golden fixtures.
//
// WHY ABSOLUTE, NOT "the ratchet is green". scripts/quality/ratchet.py grades
// each fixture against internal/quality/golden/baseline.json, and its own
// `update` subcommand rewrites that baseline from the current run. A test that
// only asserted `ratchet check` exits 0 therefore survives a revert of
// qualityIndexOptions' WithCustomExtractors(true): the recorded floor would
// simply be re-recorded lower and the gate would still pass. These assertions
// name a number, so the revert fails them.
//
// The numbers below are full recall for four of the five job fixtures. They
// are emitted only by the internal/custom/** extractors, which run for
// `grafel quality` solely because qualityIndexOptions passes
// WithCustomExtractors(true) — see the comment on that function.
//
// java-quartz-mini is deliberately absent: two of its seven must-haves come
// from the base Java extractor and five from the custom path, and its post-gate
// figure is recorded in baseline.json rather than pinned here.
func TestJobFixturesAbsoluteRecall_6260(t *testing.T) {
	// Pin the env gate OFF so the numbers below can only come from
	// qualityIndexOptions' WithCustomExtractors(true).
	//
	// inProcCustomExtractors reads GRAFEL_INPROC_CUSTOM_EXTRACTORS and the call
	// site in classifyAndReadWithProgress ORs it with the per-Index option, so
	// with the variable set in the ambient environment this test passes whether
	// or not the option is passed — the mutation that matters most here
	// survives, and the assertion silently stops testing anything. Verified: the
	// WithCustomExtractors(false) mutant is ok under
	// GRAFEL_INPROC_CUSTOM_EXTRACTORS=1 without this line and FAILs with it.
	// Every sibling test touching this gate pins it the same way — see
	// inproc_custom_extractors_5989_test.go, custom_extractor_spans_6118_test.go
	// and custom_extractor_dangling_6105_test.go.
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")

	cases := []struct {
		fixture      string
		wantFound    int
		wantExpected int
	}{
		{"csharp-hangfire-mini", 5, 5},
		{"csharp-quartz-net-mini", 5, 5},
		{"python-dramatiq-mini", 4, 4},
		{"python-rq-mini", 3, 3},
	}

	goldenDir, err := filepath.Abs(filepath.Join("..", "..", "internal", "quality", "golden"))
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			fixtureDir := filepath.Join(goldenDir, tc.fixture)
			fix, err := quality.LoadFixture(fixtureDir)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}

			graphPath := filepath.Join(t.TempDir(), "graph.json")
			// Same entry point and same option list as runQuality.
			if err := Index(quality.SourceDir(fixtureDir), graphPath, fix.Name,
				nil /*skip*/, false /*pretty*/, false, /*jsonStats*/
				qualityIndexOptions()...); err != nil {
				t.Fatalf("index fixture: %v", err)
			}
			if _, serr := os.Stat(graphPath); serr != nil {
				t.Fatalf("indexer wrote no graph.json: %v", serr)
			}

			doc, err := loadDocument(graphPath)
			if err != nil {
				t.Fatalf("load graph: %v", err)
			}
			rep := quality.Evaluate(fix, doc)

			if rep.EntityExpected != tc.wantExpected {
				t.Fatalf("expected-entity count = %d, want %d (expected.json changed; update this test deliberately)",
					rep.EntityExpected, tc.wantExpected)
			}
			if rep.EntityFound != tc.wantFound {
				var missing []string
				for _, r := range rep.EntityResults {
					if r.Expected.MustExist && !r.Found {
						missing = append(missing, r.Expected.Kind+" "+r.Expected.Name)
					}
				}
				sort.Strings(missing)
				t.Fatalf("%s must-have recall = %d/%d, want %d/%d; missing: %q",
					tc.fixture, rep.EntityFound, rep.EntityExpected,
					tc.wantFound, tc.wantExpected, missing)
			}
		})
	}
}
