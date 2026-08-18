package extractors_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// #6338 — the incremental reindex path is the dominant graph-stats.json writer
// in production: the daemon runs it on every watched edit. It only ever looks
// at the files that CHANGED, so it cannot recompute the repo-wide count of
// files skipped for having no extractor.
//
// It must therefore CARRY the previous value forward. Writing a fresh sidecar
// would zero it, and `grafel doctor` would go silent again the first time
// anyone touched a file — which is the exact blind spot this field exists to
// close, restored by the fix for it.
func TestIncremental_SidecarCarriesUnsupportedExtensions(t *testing.T) {
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc OldFunc() {}\n")
	entities := []graph.Entity{
		{ID: graph.EntityID("test-repo", "SCOPE.Operation", "OldFunc", "svc/service.go"),
			Name: "OldFunc", Kind: "SCOPE.Operation", SourceFile: "svc/service.go", Language: "go"},
	}
	buildMinimalGraph(t, stateDir, entities, nil)
	seedManifest(t, repo, stateDir)

	// The full index that ran earlier recorded 672 unsupported .vb files.
	prior := &graph.GraphStatsSidecar{
		Version:               1,
		TotalEntities:         1,
		UnsupportedExtensions: map[string]int{".vb": 672},
	}
	if err := graph.WriteSidecar(graph.SidecarPath(stateDir), prior, false); err != nil {
		t.Fatalf("seed prior sidecar: %v", err)
	}

	// Someone edits one Go file. Nothing about the .vb situation changed.
	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc NewFunc() {}\n")

	res := extractors.TryIncremental(context.Background(), repo, stateDir, nil, nil)
	if !res.Done {
		t.Fatalf("TryIncremental: unexpected fallback: %s", res.FallbackReason)
	}

	side, err := graph.LoadSidecar(stateDir)
	if err != nil {
		t.Fatalf("load sidecar after incremental: %v", err)
	}
	want := map[string]int{".vb": 672}
	if !reflect.DeepEqual(side.UnsupportedExtensions, want) {
		t.Fatalf("incremental reindex lost the unsupported-extension tally:\n got  %v\n want %v",
			side.UnsupportedExtensions, want)
	}
	// The rest of the sidecar must still be freshly computed — the carry-forward
	// must not have been implemented by simply not writing a sidecar at all.
	if side.ExtractMS <= 0 {
		t.Fatalf("the sidecar was not refreshed by this pass (extract_ms=%d)", side.ExtractMS)
	}
}
