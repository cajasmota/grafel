package extractors_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6757 arm C — the incremental reindex path is the dominant graph-stats.json
// writer in production: the daemon runs it on every watched edit.
//
// Unlike UnsupportedExtensions (#6338) this value is NOT carried forward. The
// incremental pass re-serializes the WHOLE document — every relationship in
// the graph goes back through buildRelationship — so its tally is complete and
// fresh for the graph it just wrote. Carrying a stale one forward would be
// wrong in the other direction.
//
// What must never happen is the third option: writing a sidecar with the
// fields silently absent. That reports a graph full of undeclared kinds as
// clean, having looked at nothing — the #6534 failure this arm exists to
// avoid.
func TestIncremental_SidecarReportsUndeclaredRelationshipKinds(t *testing.T) {
	if types.IsValidRelationshipKind("OWNS") {
		t.Skip("OWNS has since been declared; pick another undeclared kind for this probe")
	}
	repo := t.TempDir()
	stateDir := t.TempDir()

	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc OldFunc() {}\n")
	// The edges live on a file this pass does NOT touch, so they survive the
	// re-extract and are re-serialized by the whole-document rewrite — which
	// is the point: the tally covers the graph that was written, not just the
	// changed files.
	writeFile(t, repo, "svc/other.go", "package svc\n\nfunc Untouched() {}\n")
	a := graph.EntityID("test-repo", "SCOPE.Operation", "Untouched", "svc/other.go")
	b := graph.EntityID("test-repo", "SCOPE.Model", "Thing", "svc/other.go")
	entities := []graph.Entity{
		{ID: a, Name: "Untouched", Kind: "SCOPE.Operation", SourceFile: "svc/other.go", Language: "go"},
		{ID: b, Name: "Thing", Kind: "SCOPE.Model", SourceFile: "svc/other.go", Language: "go"},
	}
	rels := []graph.Relationship{
		{FromID: a, ToID: b, Kind: "CALLS"}, // declared — must NOT be reported
		{FromID: b, ToID: a, Kind: "OWNS"},  // undeclared — must be reported
	}
	buildMinimalGraph(t, stateDir, entities, rels)
	seedManifest(t, repo, stateDir)

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
		t.Fatalf("the sidecar was not refreshed by this pass (extract_ms=%d)", side.ExtractMS)
	}
	if side.UndeclaredRelationshipKinds["OWNS"] == 0 {
		t.Fatalf("the incremental reindex rewrote a graph containing an OWNS edge and reported no "+
			"undeclared kinds — the write path observed nothing (sidecar: edges=%d kinds=%v)",
			side.UndeclaredRelationshipEdges, side.UndeclaredRelationshipKinds)
	}
	if _, bad := side.UndeclaredRelationshipKinds["CALLS"]; bad {
		t.Errorf("the DECLARED kind CALLS was reported as undeclared — the counter is counting every "+
			"relationship, not only the undeclared ones (%v)", side.UndeclaredRelationshipKinds)
	}
	if side.UndeclaredRelationshipKindCount < 1 {
		t.Errorf("UndeclaredRelationshipKindCount = %d, want at least 1", side.UndeclaredRelationshipKindCount)
	}
}
