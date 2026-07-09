package extractors_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// TestIncremental_SidecarCarriesExtractMS is the phase-timing observability
// test for #5692. The incremental reindex path (the scheduler's fast index)
// is the dominant graph-stats.json writer in production; after it runs, the
// sidecar must carry a non-zero extract_ms so `grafel feedback` can report
// where indexing time goes.
func TestIncremental_SidecarCarriesExtractMS(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc OldFunc() {}\n")
	entities := []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "OldFunc", "svc/service.go"),
			Name: "OldFunc", Kind: "SCOPE.Operation", SourceFile: "svc/service.go", Language: "go"},
	}
	buildMinimalGraph(t, stateDir, entities, nil)
	seedManifest(t, repo, stateDir)

	// Edit the file so the incremental path re-extracts and rewrites the sidecar.
	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc NewFunc() {}\n")

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental: unexpected fallback: %s", res.FallbackReason)
	}

	side, err := graph.LoadSidecar(stateDir)
	if err != nil {
		t.Fatalf("load sidecar after incremental: %v", err)
	}
	if side.ExtractMS <= 0 {
		t.Fatalf("expected extract_ms > 0 after incremental reindex, got %d", side.ExtractMS)
	}
	// link_ms is NOT written to graph-stats.json (it lives in link-stats.json,
	// owned solely by the link pass), so the incremental writer never sets it.
	t.Logf("extract_ms=%d", side.ExtractMS)
}
