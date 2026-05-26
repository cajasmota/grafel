package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestVerify1620Pipeline is the isolated end-to-end verification for #1620.
// Gated behind ARCHIGRAPH_VERIFY_REPO so it never runs in normal CI; point it
// at a real repo (e.g. upvate_core) to measure community counts at every layer
// of the pipeline: sidecar -> graph.fb -> loaded Document, plus the fast
// reactive (graph-algo skipped) re-index. Never touches the live daemon.
//
//	ARCHIGRAPH_VERIFY_REPO=/path/to/upvate_core go test ./cmd/archigraph/ \
//	  -run TestVerify1620Pipeline -v -count=1 -timeout=20m
func TestVerify1620Pipeline(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	repo := os.Getenv("ARCHIGRAPH_VERIFY_REPO")
	if repo == "" {
		t.Skip("set ARCHIGRAPH_VERIFY_REPO to a real repo to run the #1620 e2e verification")
	}
	tmp := t.TempDir()
	out := filepath.Join(tmp, "graph.json")

	// --- Layer 0: full index (Pass 4 runs). Writes graph.fb + sidecar. ---
	if err := Index(repo, out, "verify-repo", nil, false, false); err != nil {
		t.Fatalf("full index: %v", err)
	}

	// Sidecar community count.
	sidePath := filepath.Join(filepath.Dir(out), "graph-stats.json")
	sideBytes, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var side graph.GraphStatsSidecar
	if err := json.Unmarshal(sideBytes, &side); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	t.Logf("[sidecar]      communities=%d modularity=%.4f godNodes=%d",
		side.Communities, side.Modularity, side.GodNodes)
	if side.Communities == 0 {
		t.Fatalf("sidecar reports 0 communities — algo pass did not run")
	}

	// --- Layer 1: load through graph.fb (the daemon's preferred store). ---
	fbDoc, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("load graph.fb: %v", err)
	}
	withCID := 0
	for i := range fbDoc.Entities {
		if fbDoc.Entities[i].CommunityID != nil {
			withCID++
		}
	}
	t.Logf("[graph.fb]     communities=%d entities=%d entitiesWithCommunityID=%d",
		len(fbDoc.Communities), len(fbDoc.Entities), withCID)
	if len(fbDoc.Communities) == 0 {
		t.Fatalf("graph.fb round-trip lost the aggregate communities list (the #1620 bug)")
	}
	if withCID == 0 {
		t.Fatalf("graph.fb round-trip lost per-entity community_id (the #1620 bug)")
	}
	if fbDoc.AlgorithmStats == nil || fbDoc.AlgorithmStats.NumCommunities != len(fbDoc.Communities) {
		t.Fatalf("graph.fb AlgorithmStats mismatch: %+v vs %d communities", fbDoc.AlgorithmStats, len(fbDoc.Communities))
	}
	fullCommunities := len(fbDoc.Communities)

	// --- Layer 2: fast reactive re-index (graph-algo SKIPPED). ---
	// This is the path daemonSchedulerIndex uses. Without the carry-forward
	// fix it would overwrite graph.fb community-free.
	if err := Index(repo, out, "verify-repo", []string{"graph-algo"}, false, false); err != nil {
		t.Fatalf("fast re-index: %v", err)
	}
	fastDoc, err := graph.LoadGraphFromDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("reload after fast index: %v", err)
	}
	fastWithCID := 0
	for i := range fastDoc.Entities {
		if fastDoc.Entities[i].CommunityID != nil {
			fastWithCID++
		}
	}
	t.Logf("[fast reindex] communities=%d entities=%d entitiesWithCommunityID=%d",
		len(fastDoc.Communities), len(fastDoc.Entities), fastWithCID)
	if len(fastDoc.Communities) == 0 {
		t.Fatalf("fast reactive re-index STRIPPED the communities (the #1620 bug)")
	}
	if fastWithCID == 0 {
		t.Fatalf("fast reactive re-index stripped per-entity community_id")
	}
	if len(fastDoc.Communities) != fullCommunities {
		t.Logf("WARNING: community count drifted across fast reindex: full=%d fast=%d (carry-forward by-ID; acceptable if entities changed)",
			fullCommunities, len(fastDoc.Communities))
	}
}
