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
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
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

	// mk builds a ref graph. sigA perturbs svc:A's signature, which feeds
	// graph.DiffDocs' sourceWindowHash and so classifies it as MODIFIED (and
	// which load.go explicitly round-trips through graph.fb); added names an
	// entity that exists only on this ref.
	mk := func(sigA string, added string) *graph.Document {
		a := prImpact6006Ent("svc:A")
		a.Signature = sigA
		d := &graph.Document{Version: 1, Repo: "svc",
			Entities: []graph.Entity{a, prImpact6006Ent("svc:B")}}
		if added != "" {
			d.Entities = append(d.Entities, prImpact6006Ent(added))
			d.Relationships = []graph.Relationship{{FromID: added, ToID: "svc:A", Kind: "CALLS"}}
		}
		return d
	}
	refs := map[string]*graph.Document{
		"main": mk("func A()", ""),
		// Two refs that MODIFY a pre-existing, overlay-covered entity.
		"modA":  mk("func A(x int)", ""),
		"modA2": mk("func A(y string)", ""),
		// Two refs that only ADD entities. These are the #6006 D1 shape: the
		// overlay is computed from the indexed group union, so svc:NewA / svc:NewB
		// CANNOT be in it. A fixture that stamps them into the overlay is testing a
		// state production cannot produce.
		"addOnly1": mk("func A()", "svc:NewA"),
		"addOnly2": mk("func A()", "svc:NewB"),
	}
	for ref, doc := range refs {
		dir := daemon.StateDirForRepoRef(repoPath, ref)
		if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
			t.Fatalf("write graph.fb for %s: %v", ref, err)
		}
	}
	// The HEAD-ref graph.fb the overlay's staleness check keys off.
	if err := fbwriter.WriteAtomic(
		filepath.Join(daemon.StateDirForRepo(repoPath), "graph.fb"), mk("func A()", "")); err != nil {
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

// writeOverlay writes the overlay in the PRODUCTION shape: it covers exactly the
// entities that exist in the indexed group union (svc:A, svc:B) and nothing
// else. Entities that live only on a feature ref are deliberately absent —
// stamping them in would fabricate a state the group-algo pass cannot produce
// and would make the add-only test below vacuously pass.
func (e prImpact6006Env) writeOverlay(t *testing.T) {
	t.Helper()
	res := map[string]groupalgo.EntityOverlay{}
	for _, id := range []string{"svc:A", "svc:B"} {
		res[id] = groupalgo.EntityOverlay{CommunityID: 7, PageRank: 0.1}
	}
	ov := &groupalgo.Overlay{
		AlgoVersion:  groupalgo.OverlayAlgoVersion,
		Group:        "acme",
		ComputedAt:   time.Now().UTC().Add(-time.Hour),
		SourceMtimes: e.curMtimes,
		Results:      res,
		Communities:  []graph.CommunityResult{{ID: 7, Size: 2, AutoName: "core"}},
	}
	if err := groupalgo.WriteOverlayTo(e.overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

// conflictsArgs asks the merge-risk question about two refs that MODIFY a
// pre-existing, overlay-covered entity.
func conflictsArgs() map[string]any {
	return map[string]any{
		"group": "acme", "repo": "svc",
		"base": "main",
		"refs": []any{"modA", "modA2"},
	}
}

// addOnlyConflictsArgs asks it about two refs that only ADD entities — the shape
// the overlay cannot cover.
func addOnlyConflictsArgs() map[string]any {
	return map[string]any{
		"group": "acme", "repo": "svc",
		"base": "main",
		"refs": []any{"addOnly1", "addOnly2"},
	}
}

// prImpact6006Payload is the union of the fields these tests assert on.
type prImpact6006Payload struct {
	RiskyPairCount         int    `json:"risky_pair_count"`
	CommunityDataAvailable bool   `json:"community_data_available"`
	CommunityDataStale     bool   `json:"community_data_stale"`
	OverlayComputedAt      string `json:"overlay_computed_at"`
	CommunityCount         int    `json:"impacted_community_count"`
	ChangedCount           int    `json:"changed_count"`
	ChangedUncovered       int    `json:"changed_entities_without_community"`
	RiskPairs              []struct {
		SharedCommunities []int `json:"shared_communities"`
	} `json:"risk_pairs"`
}

func mustPayload(t *testing.T, res *mcpapi.CallToolResult) prImpact6006Payload {
	t.Helper()
	if res == nil || res.IsError {
		t.Fatalf("expected a successful result, got: %s", resultText(res))
	}
	var p prImpact6006Payload
	if err := json.Unmarshal([]byte(resultText(res)), &p); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, resultText(res))
	}
	return p
}

// Control arm: with the group overlay present, the two refs DO conflict. This is
// what proves the fixture can exhibit a conflict at all — without it, the
// unavailable arm below would be indistinguishable from a fixture that simply
// has nothing to find.
func TestPRImpact6006_ConflictsDetectedWithOverlay(t *testing.T) {
	env := setupPRImpact6006(t)
	env.writeOverlay(t)

	p := mustPayload(t, callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs()))
	if !p.CommunityDataAvailable {
		t.Errorf("overlay covers the changed entity; community_data_available = false")
	}
	if p.CommunityDataStale {
		t.Errorf("overlay records the current graph.fb mtimes; community_data_stale = true")
	}
	if p.OverlayComputedAt == "" {
		t.Errorf("overlay_computed_at missing — callers cannot judge the partition's age")
	}
	if p.RiskyPairCount != 1 {
		t.Fatalf("expected 1 risky pair from the shared community, got %d", p.RiskyPairCount)
	}
	if len(p.RiskPairs) != 1 || len(p.RiskPairs[0].SharedCommunities) != 1 ||
		p.RiskPairs[0].SharedCommunities[0] != 7 {
		t.Errorf("expected shared community [7], got %+v", p.RiskPairs)
	}
}

// D1 — the regression the first fix missed. The overlay is present, fresh, and
// covers the repo; the two refs only ADD entities, so nothing they changed is in
// it. Before this fix the tool returned risky_pair_count:0 alongside
// community_data_available:true — an affirmative all-clear on a change nothing
// was known about, which is worse than the original bug.
func TestPRImpact6006_AddOnlyRefsAreNotReportedAsNoConflicts(t *testing.T) {
	env := setupPRImpact6006(t)
	env.writeOverlay(t)

	res := callHandlerResult(t, env.srv.handlePRImpact, addOnlyConflictsArgs())
	if res == nil {
		t.Fatal("nil result")
	}
	text := resultText(res)
	if !res.IsError {
		t.Fatalf("no changed entity is covered by the overlay, so merge risk was not computed; "+
			"must not return a payload: %s", text)
	}
	// The remedy differs from the absent-overlay case: telling a user to run a
	// group index when the overlay exists and is fresh is actively misleading.
	if !strings.Contains(text, "does not cover the changed entities") {
		t.Errorf("error must name the real cause (overlay does not cover the changed entities), got: %s", text)
	}
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) == nil {
		if v, ok := payload["risky_pair_count"]; ok {
			t.Errorf("uncomputed merge risk still emitted risky_pair_count=%v", v)
		}
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
	// The absent case must NOT be described as staleness — the remedy differs.
	if !strings.Contains(text, "absent, corrupt") {
		t.Errorf("error should name the absent/corrupt overlay specifically, got: %s", text)
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

// D2 — a STALE overlay must still ANSWER, flagged. The staleness key is the
// currently-checked-out ref's graph.fb, so refusing on stale turned "I am
// standing on a feature branch" — the only posture from which anyone asks this
// question — into a hard error, and made the tool unusable for ~50 minutes after
// any reindex. A community-level verdict from a slightly older partition is
// worth far more than a refusal.
func TestPRImpact6006_StaleOverlayStillAnswersButIsFlagged(t *testing.T) {
	env := setupPRImpact6006(t)
	// Record mtimes that match nothing on disk → IsOverlayStale.
	env.curMtimes = map[string]int64{"svc": 1}
	env.writeOverlay(t)

	p := mustPayload(t, callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs()))
	if p.RiskyPairCount != 1 {
		t.Fatalf("a stale partition still places these refs in community 7; "+
			"expected the conflict to be reported, got %d", p.RiskyPairCount)
	}
	if !p.CommunityDataStale {
		t.Errorf("overlay IS stale but community_data_stale = false — the caller cannot tell")
	}
	if !p.CommunityDataAvailable {
		t.Errorf("stale is not unavailable: community_data_available should be true")
	}
	if p.OverlayComputedAt == "" {
		t.Errorf("overlay_computed_at missing — the only way to judge how stale")
	}
}

// D4 — the overlay is parsed once per (path, mtime), not once per call. On the
// live corpus the file is 31.9 MB: ~326 ms and ~76 MB of transient heap per
// read, and concurrent MCP agents are exactly the workload that cannot afford it.
func TestPRImpact6006_OverlayReadIsMemoized(t *testing.T) {
	env := setupPRImpact6006(t)
	env.writeOverlay(t)

	// Prime the cache, then measure hits across two further calls.
	_ = callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs())
	overlayCacheMu.Lock()
	before := overlayCacheHits
	overlayCacheMu.Unlock()

	for i := 0; i < 2; i++ {
		_ = mustPayload(t, callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs()))
	}

	overlayCacheMu.Lock()
	got := overlayCacheHits - before
	overlayCacheMu.Unlock()
	if got != 2 {
		t.Errorf("expected 2 cache hits across 2 repeat calls, got %d (overlay re-parsed per call)", got)
	}

	// A rewritten overlay must invalidate: same path, new content.
	time.Sleep(10 * time.Millisecond) // ensure a distinct mtime
	env.writeOverlay(t)
	overlayCacheMu.Lock()
	before = overlayCacheHits
	overlayCacheMu.Unlock()
	_ = mustPayload(t, callHandlerResult(t, env.srv.handlePRImpact, conflictsArgs()))
	overlayCacheMu.Lock()
	got = overlayCacheHits - before
	overlayCacheMu.Unlock()
	if got != 0 {
		t.Errorf("a rewritten overlay must invalidate the cache, got %d hits", got)
	}
}

// Single mode keeps returning its payload (the blast radius does not depend on
// communities) but must label the community data so an empty
// impacted_communities is explicable.
func TestPRImpact6006_SingleModeLabelsCommunityData(t *testing.T) {
	env := setupPRImpact6006(t)
	parse := func(t *testing.T, head string) prImpact6006Payload {
		t.Helper()
		p := mustPayload(t, callHandlerResult(t, env.srv.handlePRImpact,
			map[string]any{"group": "acme", "repo": "svc", "base": "main", "head": head}))
		if p.ChangedCount == 0 {
			t.Fatalf("fixture produced no changed entities for head=%s — it cannot test anything", head)
		}
		return p
	}

	// No overlay at all.
	_ = os.Remove(env.overlayPath)
	if p := parse(t, "modA"); p.CommunityDataAvailable || p.CommunityCount != 0 {
		t.Errorf("no overlay: want available=false / 0 communities, got %v/%d",
			p.CommunityDataAvailable, p.CommunityCount)
	}

	// Overlay present, change touches a covered entity.
	env.writeOverlay(t)
	p := parse(t, "modA")
	if !p.CommunityDataAvailable || p.CommunityCount != 1 {
		t.Errorf("with overlay: want available=true / 1 community, got %v/%d",
			p.CommunityDataAvailable, p.CommunityCount)
	}
	if p.ChangedUncovered != 0 {
		t.Errorf("the modified entity IS covered; changed_entities_without_community = %d", p.ChangedUncovered)
	}

	// D1 in single mode: same fresh overlay, add-only change. Single mode still
	// returns its payload (the blast radius is valid) but must NOT claim the
	// community data covered this change.
	add := parse(t, "addOnly1")
	if add.CommunityDataAvailable {
		t.Errorf("add-only change is uncovered by the overlay; community_data_available must be false")
	}
	if add.ChangedUncovered != 1 {
		t.Errorf("changed_entities_without_community = %d, want 1", add.ChangedUncovered)
	}
}
