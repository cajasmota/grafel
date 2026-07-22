// flows_overlay_test.go — FLOW side-table read-merge on the dashboard load seam
// (#5904 PR-b). loadGroupForRef REPLACE-merges <stateDir>/flows.json onto each
// repo's dr.Doc so the flow handlers (handlers_flows / handlers_event_flows /
// v2_flows), which loop dr.Doc.Entities/Relationships by Kind, see the
// cross-repo-aware flows — not doubled, not intra-only.
package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/registry"
)

// buildFlowDashFixture registers a one-repo group whose graph.json carries a
// BAKED intra-repo SCOPE.Process, and returns the group name + repo state dir.
func buildFlowDashFixture(t *testing.T) (group, stateDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, "store"))

	group = "flowgrp"
	repoPath := filepath.Join(home, "myrepo")
	_ = os.MkdirAll(repoPath, 0o755)

	cfg := &registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	cfgDir := filepath.Join(home, "groups")
	_ = os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	raw, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, raw, 0o644)
	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": group, "config_path": cfgPath}},
	})
	_ = os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	stateDir = daemon.StateDirForRepo(repoPath)
	_ = os.MkdirAll(stateDir, 0o755)
	doc := &graph.Document{
		Version: 1, Repo: "svc",
		Entities: []graph.Entity{
			{ID: "fn1", Name: "handleSubmit", Kind: "Function"},
			{ID: "fn2", Name: "callService", Kind: "Function"},
			graph.Entity{ID: "baked-proc", Name: "BakedIntraFlow", Kind: "SCOPE.Process"}.
				WithProperties(map[string]string{"step_count": "2", "cross_stack": "false"}),
		},
		Relationships: []graph.Relationship{
			{ID: "c1", FromID: "fn1", ToID: "fn2", Kind: "CALLS"},
			graph.Relationship{ID: "s1", FromID: "baked-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
			graph.Relationship{ID: "s2", FromID: "baked-proc", ToID: "fn2", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "1"}),
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return group, stateDir
}

func procKinds(doc *graph.Document) []string {
	var out []string
	for i := range doc.Entities {
		if doc.Entities[i].Kind == "SCOPE.Process" {
			out = append(out, doc.Entities[i].ID)
		}
	}
	return out
}

// TestDashFlowOverlay_ReplaceNotDoubled: with a fresh sidecar the loaded dr.Doc
// (what the flow handlers loop over) shows exactly ONE SCOPE.Process — the
// cross-repo one — replacing the baked intra flow.
func TestDashFlowOverlay_ReplaceNotDoubled(t *testing.T) {
	group, stateDir := buildFlowDashFixture(t)
	ents := []graph.Entity{
		graph.Entity{ID: "xrepo-proc", Name: "CrossRepoFlow", Kind: "SCOPE.Process"}.
			WithProperties(map[string]string{"step_count": "3", "cross_stack": "true"}),
	}
	rels := []graph.Relationship{
		graph.Relationship{ID: "xs0", FromID: "xrepo-proc", ToID: "fn1", Kind: "STEP_IN_PROCESS"}.WithProperties(map[string]string{"step_index": "0"}),
		graph.Relationship{ID: "ph1", FromID: "fn2", ToID: "remote::ep", Kind: "CALLS"}.WithProperties(map[string]string{"cross_repo": "true"}),
	}
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	c := NewGraphCache(60 * time.Second)
	grp, err := c.loadGroupForRef(group, "")
	if err != nil {
		t.Fatalf("loadGroupForRef: %v", err)
	}
	dr := grp.Repos["svc"]
	if dr == nil || dr.Doc == nil {
		t.Fatal("svc repo not loaded")
	}
	procs := procKinds(dr.Doc)
	if len(procs) != 1 || procs[0] != "xrepo-proc" {
		t.Fatalf("REPLACE violated: want exactly [xrepo-proc], got %v", procs)
	}
	// The phantom CALLS edge must be present (added), the baked step edges gone.
	var phantom, bakedStep int
	for i := range dr.Doc.Relationships {
		r := &dr.Doc.Relationships[i]
		if r.Kind == "CALLS" && r.PropGet("cross_repo") == "true" {
			phantom++
		}
		if r.Kind == "STEP_IN_PROCESS" && r.FromID == "baked-proc" {
			bakedStep++
		}
	}
	if phantom != 1 {
		t.Errorf("want 1 phantom CALLS edge, got %d", phantom)
	}
	if bakedStep != 0 {
		t.Errorf("baked STEP edges survived REPLACE: %d", bakedStep)
	}
}

// TestDashFlowOverlay_EventFlowReplace: the dashboard load seam REPLACE-merges
// SCOPE.EventFlow entities too — a baked event flow (+ its SEED/STEP edges) is
// suppressed and substituted by the sidecar's cross-repo event flow, so the
// event-flow handler (which loops Doc.Entities by Kind==SCOPE.EventFlow) sees the
// merged set, not doubled or dangling.
func TestDashFlowOverlay_EventFlowReplace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, "store"))
	group := "efgrp"
	repoPath := filepath.Join(home, "myrepo")
	_ = os.MkdirAll(repoPath, 0o755)
	cfg := &registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	cfgDir := filepath.Join(home, "groups")
	_ = os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	raw, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgPath, raw, 0o644)
	regRaw, _ := json.Marshal(map[string]any{"version": 1, "groups": []map[string]any{{"name": group, "config_path": cfgPath}}})
	_ = os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644)

	stateDir := daemon.StateDirForRepo(repoPath)
	_ = os.MkdirAll(stateDir, 0o755)
	doc := &graph.Document{
		Version: 1, Repo: "svc",
		Entities: []graph.Entity{
			{ID: "chan1", Name: "orders.topic", Kind: "message_channel"},
			{ID: "consumer1", Name: "onOrder", Kind: "Function"},
			graph.Entity{ID: "baked-ef", Name: "BakedEventFlow", Kind: "SCOPE.EventFlow"}.
				WithProperties(map[string]string{"cross_stack": "false"}),
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "seed-b", FromID: "chan1", ToID: "baked-ef", Kind: "SEED_OF_EVENT_FLOW"}.WithProperties(map[string]string{}),
			graph.Relationship{ID: "efs-b", FromID: "baked-ef", ToID: "consumer1", Kind: "STEP_IN_EVENT_FLOW"}.WithProperties(map[string]string{"step_index": "0"}),
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(stateDir, "graph.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Sidecar: a cross-repo SCOPE.EventFlow replacing the baked one.
	ents := []graph.Entity{
		graph.Entity{ID: "xrepo-ef", Name: "CrossRepoEventFlow", Kind: "SCOPE.EventFlow"}.
			WithProperties(map[string]string{"cross_stack": "true"}),
	}
	rels := []graph.Relationship{
		graph.Relationship{ID: "seed-x", FromID: "chan1", ToID: "xrepo-ef", Kind: "SEED_OF_EVENT_FLOW"}.WithProperties(map[string]string{}),
		graph.Relationship{ID: "efs-x", FromID: "xrepo-ef", ToID: "consumer1", Kind: "STEP_IN_EVENT_FLOW"}.WithProperties(map[string]string{"step_index": "0"}),
	}
	if err := flows.Upsert(stateDir, ents, rels); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	c := NewGraphCache(60 * time.Second)
	grp, err := c.loadGroupForRef(group, "")
	if err != nil {
		t.Fatalf("loadGroupForRef: %v", err)
	}
	d := grp.Repos["svc"].Doc
	var efs []string
	for i := range d.Entities {
		if d.Entities[i].Kind == "SCOPE.EventFlow" {
			efs = append(efs, d.Entities[i].ID)
		}
	}
	if len(efs) != 1 || efs[0] != "xrepo-ef" {
		t.Fatalf("event-flow REPLACE violated on dashboard seam: want [xrepo-ef], got %v", efs)
	}
	for i := range d.Relationships {
		r := &d.Relationships[i]
		if r.FromID == "baked-ef" || r.ToID == "baked-ef" {
			t.Errorf("dangling baked event-flow edge survived on dashboard seam: %s %s->%s", r.Kind, r.FromID, r.ToID)
		}
	}
}

// TestDashFlowOverlay_NoSidecarParity: no sidecar → baked intra flow shown as-is.
func TestDashFlowOverlay_NoSidecarParity(t *testing.T) {
	group, _ := buildFlowDashFixture(t)
	c := NewGraphCache(60 * time.Second)
	grp, err := c.loadGroupForRef(group, "")
	if err != nil {
		t.Fatalf("loadGroupForRef: %v", err)
	}
	procs := procKinds(grp.Repos["svc"].Doc)
	if len(procs) != 1 || procs[0] != "baked-proc" {
		t.Fatalf("parity: want the baked intra flow [baked-proc], got %v", procs)
	}
}
