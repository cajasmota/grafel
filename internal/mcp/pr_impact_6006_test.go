// pr_impact_6006_test.go — issue #6006: grafel_pr_impact must never answer a
// merge-risk question with a silent "no conflicts" when the community data the
// answer depends on was never computed.
//
// The fixtures below are deliberately IDENTICAL except for the presence of the
// group-algo overlay: same repo, same refs, same diff. With the overlay the tool
// finds a real conflict; without it the tool must say so explicitly rather than
// returning an empty risky-pair list.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

// prImpact6006Env is the on-disk world a grafel_pr_impact call needs: a
// registered group, a repo with per-ref graph.fb files, and a resolved overlay
// path the test can populate or leave empty.
type prImpact6006Env struct {
	srv         *Server
	overlayPath string
	curMtimes   map[string]int64
}

func prImpact6006Ent(id string) graph.Entity {
	return graph.Entity{ID: id, Name: id, Kind: "function", SourceFile: id + ".go", Language: "go"}
}

// setupPRImpact6006 writes three ref graphs for one repo:
//
//	main     : A, B
//	featureA : A, B, NewA   (NewA CALLS A)
//	featureB : A, B, NewB   (NewB CALLS A)
//
// NONE of the entities carry a CommunityID — that is exactly what graph.fb looks
// like today, since the per-repo Pass-4 algo pass was removed in favour of the
// group-scope pass that writes <group>-algo.json.
func setupPRImpact6006(t *testing.T) prImpact6006Env {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoPath := filepath.Join(root, "svc")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	base := []graph.Entity{prImpact6006Ent("svc:A"), prImpact6006Ent("svc:B")}
	mk := func(extra string) *graph.Document {
		d := &graph.Document{Version: 1, Repo: "svc"}
		d.Entities = append(d.Entities, base...)
		if extra != "" {
			d.Entities = append(d.Entities, prImpact6006Ent(extra))
			d.Relationships = []graph.Relationship{{FromID: extra, ToID: "svc:A", Kind: "CALLS"}}
		}
		return d
	}
	refs := map[string]*graph.Document{
		"main":     mk(""),
		"featureA": mk("svc:NewA"),
		"featureB": mk("svc:NewB"),
	}
	for ref, doc := range refs {
		dir := daemon.StateDirForRepoRef(repoPath, ref)
		if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
			t.Fatalf("write graph.fb for %s: %v", ref, err)
		}
	}
	// The HEAD-ref graph.fb the overlay's staleness check keys off.
	if err := fbwriter.WriteAtomic(
		filepath.Join(daemon.StateDirForRepo(repoPath), "graph.fb"), mk("")); err != nil {
		t.Fatalf("write HEAD graph.fb: %v", err)
	}

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	overlayPath, err := groupalgo.OverlayPath("acme")
	if err != nil {
		t.Fatalf("overlay path: %v", err)
	}
	cur, err := groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes: %v", err)
	}
	return prImpact6006Env{
		srv:         newTestServer(t, &graph.Document{Repo: "svc"}),
		overlayPath: overlayPath,
		curMtimes:   cur,
	}
}

// writeOverlay6006 stamps every fixture entity into ONE community (7), so the
// two feature refs genuinely conflict.
func (e prImpact6006Env) writeOverlay(t *testing.T) {
	t.Helper()
	res := map[string]groupalgo.EntityOverlay{}
	for _, id := range []string{"svc:A", "svc:B", "svc:NewA", "svc:NewB"} {
		res[id] = groupalgo.EntityOverlay{CommunityID: 7, PageRank: 0.1}
	}
	ov := &groupalgo.Overlay{
		AlgoVersion:  groupalgo.OverlayAlgoVersion,
		Group:        "acme",
		SourceMtimes: e.curMtimes,
		Results:      res,
		Communities:  []graph.CommunityResult{{ID: 7, Size: 4, AutoName: "core"}},
	}
	if err := groupalgo.WriteOverlayTo(e.overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

func conflictsArgs() map[string]any {
	return map[string]any{
		"group": "acme", "repo": "svc",
		"base": "main",
		"refs": []any{"featureA", "featureB"},
	}
}

// Control arm: with the group overlay present, the two refs DO conflict. This is
// what proves the fixture can exhibit a conflict at all — without it, the
// unavailable arm below would be indistinguishable from a fixture that simply
// has nothing to find.
func TestPRImpact6006_ConflictsDetectedWithOverlay(t *testing.T) {
	env := setupPRImpact6006(t)
	env.writeOverlay(t)

	res := callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs())
	if res == nil || res.IsError {
		t.Fatalf("expected a successful result, got: %v / %s", res, resultText(res))
	}
	var payload struct {
		RiskyPairCount         int  `json:"risky_pair_count"`
		CommunityDataAvailable bool `json:"community_data_available"`
		RiskPairs              []struct {
			SharedCommunities []int `json:"shared_communities"`
		} `json:"risk_pairs"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, resultText(res))
	}
	if !payload.CommunityDataAvailable {
		t.Errorf("overlay is present and fresh; community_data_available = false")
	}
	if payload.RiskyPairCount != 1 {
		t.Fatalf("expected 1 risky pair from the shared community, got %d: %s",
			payload.RiskyPairCount, resultText(res))
	}
	if len(payload.RiskPairs) != 1 || len(payload.RiskPairs[0].SharedCommunities) != 1 ||
		payload.RiskPairs[0].SharedCommunities[0] != 7 {
		t.Errorf("expected shared community [7], got %+v", payload.RiskPairs)
	}
}

// THE #6006 test. Identical call, overlay removed. The tool must NOT hand back a
// payload whose risky_pair_count is 0 — that reads as "safe to merge".
func TestPRImpact6006_NoOverlayIsNotReportedAsNoConflicts(t *testing.T) {
	env := setupPRImpact6006(t)
	_ = os.Remove(env.overlayPath)

	res := callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs())
	if res == nil {
		t.Fatal("nil result")
	}
	text := resultText(res)
	if !res.IsError {
		t.Fatalf("merge risk with no community data must be an explicit error, not a result; got: %s", text)
	}
	low := strings.ToLower(text)
	for _, want := range []string{"community", "not", "conflict"} {
		if !strings.Contains(low, want) {
			t.Errorf("error message should explain the unavailability and that it is NOT a no-conflicts answer; missing %q in: %s", want, text)
		}
	}
	// Belt and braces: whatever shape the message takes, it must not be a JSON
	// payload that an agent could parse as a clean zero-risk answer.
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) == nil {
		if v, ok := payload["risky_pair_count"]; ok {
			t.Errorf("unavailable merge risk still emitted risky_pair_count=%v", v)
		}
	}
}

// A STALE overlay is treated exactly like an absent one — it must not silently
// supply a partition that no longer matches the graphs being compared.
func TestPRImpact6006_StaleOverlayIsNotReportedAsNoConflicts(t *testing.T) {
	env := setupPRImpact6006(t)
	// Record mtimes that do not match anything on disk → IsOverlayStale.
	env.curMtimes = map[string]int64{"svc": 1}
	env.writeOverlay(t)

	res := callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs())
	if res == nil || !res.IsError {
		t.Fatalf("stale overlay must be reported, not silently treated as no-conflicts; got: %s", resultText(res))
	}
}

// Single mode keeps returning its payload (the blast radius does not depend on
// communities) but must label the community data so an empty
// impacted_communities is explicable.
func TestPRImpact6006_SingleModeLabelsCommunityData(t *testing.T) {
	env := setupPRImpact6006(t)
	args := map[string]any{"group": "acme", "repo": "svc", "base": "main", "head": "featureA"}

	parse := func(t *testing.T) (bool, int) {
		t.Helper()
		res := callHandlerResult(t, env.srv.handlePRImpact, args)
		if res == nil || res.IsError {
			t.Fatalf("single mode should succeed, got: %s", resultText(res))
		}
		var p struct {
			CommunityDataAvailable bool `json:"community_data_available"`
			CommunityCount         int  `json:"impacted_community_count"`
			ChangedCount           int  `json:"changed_count"`
		}
		if err := json.Unmarshal([]byte(resultText(res)), &p); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, resultText(res))
		}
		if p.ChangedCount == 0 {
			t.Fatalf("fixture produced no changed entities — it cannot test anything: %s", resultText(res))
		}
		return p.CommunityDataAvailable, p.CommunityCount
	}

	_ = os.Remove(env.overlayPath)
	if avail, count := parse(t); avail || count != 0 {
		t.Errorf("no overlay: want community_data_available=false and 0 communities, got %v/%d", avail, count)
	}

	env.writeOverlay(t)
	if avail, count := parse(t); !avail || count != 1 {
		t.Errorf("with overlay: want community_data_available=true and 1 community, got %v/%d", avail, count)
	}
}
