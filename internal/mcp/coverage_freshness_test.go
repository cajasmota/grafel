// coverage_freshness_test.go — fixtures for the coverage-freshness signal
// (#5068): stale when the ingested coverage measurement predates the latest
// index; fresh when at/after; graceful when timestamps are absent.
package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/coverage"
	"github.com/cajasmota/grafel/internal/graph"
)

// fixtureGroup builds a one-repo LoadedGroup whose document was indexed at
// indexedAt and (optionally) carries a coverage_source/measured_at stamp.
func fixtureGroup(indexedAt time.Time, source, measuredAt string) *LoadedGroup {
	ent := graph.Entity{ID: "e1", Name: "Svc"}
	if source != "" {
		ent.PropsReplace(map[string]string{coverage.PropCoverageSource: source})
		if measuredAt != "" {
			ent.PropSet(coverage.PropCoverageMeasAt, measuredAt)
		}
	}
	return &LoadedGroup{
		Name: "g",
		Repos: map[string]*LoadedRepo{
			"r": {
				Repo: "r",
				Doc: &graph.Document{
					GeneratedAt: indexedAt,
					Entities:    []graph.Entity{ent},
				},
			},
		},
	}
}

func TestCoverageFreshness_Stale(t *testing.T) {
	idx := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	measured := "2026-06-10T00:00:00Z" // older than index → STALE
	f := computeCoverageFreshness(fixtureGroup(idx, coverage.SourceLCOV, measured))
	if !f.ingested {
		t.Fatal("expected ingested=true")
	}
	if !f.haveVerdict || !f.stale {
		t.Fatalf("expected STALE verdict, got %+v", f)
	}
	out := renderCoverageFreshness(f)
	if !strings.Contains(out, "STALE") || !strings.Contains(out, "reingest") {
		t.Fatalf("render missing stale guidance:\n%s", out)
	}
}

func TestCoverageFreshness_Fresh(t *testing.T) {
	idx := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	measured := "2026-06-13T12:00:00Z" // newer than index → FRESH
	f := computeCoverageFreshness(fixtureGroup(idx, coverage.SourceLCOV, measured))
	if !f.haveVerdict || f.stale {
		t.Fatalf("expected FRESH verdict, got %+v", f)
	}
	if out := renderCoverageFreshness(f); !strings.Contains(out, "FRESH") {
		t.Fatalf("render missing FRESH:\n%s", out)
	}
}

func TestCoverageFreshness_NoIngestion(t *testing.T) {
	idx := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	f := computeCoverageFreshness(fixtureGroup(idx, "", ""))
	if f.ingested {
		t.Fatalf("expected ingested=false, got %+v", f)
	}
	out := renderCoverageFreshness(f)
	if !strings.Contains(out, "No coverage report ingested") {
		t.Fatalf("render should state no ingestion:\n%s", out)
	}
}

func TestCoverageFreshness_IngestedNoMeasuredAt(t *testing.T) {
	idx := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	f := computeCoverageFreshness(fixtureGroup(idx, coverage.SourceLCOV, ""))
	if !f.ingested || f.haveVerdict {
		t.Fatalf("expected ingested w/o verdict, got %+v", f)
	}
	if out := renderCoverageFreshness(f); !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("render should be UNKNOWN when no measured_at:\n%s", out)
	}
}

func TestCoverageFreshness_NoIndexTimestamp(t *testing.T) {
	// Zero GeneratedAt → no index time to compare against.
	f := computeCoverageFreshness(fixtureGroup(time.Time{}, coverage.SourceLCOV, "2026-06-10T00:00:00Z"))
	if f.haveVerdict {
		t.Fatalf("expected no verdict without index time, got %+v", f)
	}
	if out := renderCoverageFreshness(f); !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("render should be UNKNOWN without index time:\n%s", out)
	}
}

func TestCoverageFreshness_NilGroup(t *testing.T) {
	f := computeCoverageFreshness(nil)
	if f.ingested {
		t.Fatal("nil group must be non-ingested")
	}
	if out := renderCoverageFreshness(f); !strings.Contains(out, "No coverage report ingested") {
		t.Fatalf("nil group should degrade gracefully:\n%s", out)
	}
}
