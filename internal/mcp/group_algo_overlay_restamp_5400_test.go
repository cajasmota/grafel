// group_algo_overlay_restamp_5400_test.go — #5400/#5401/#5397: the group-algo
// overlay must keep surfacing on inspect / orient / stats / clusters for a repo
// whose graph.fb is reparsed AFTER the overlay was first applied.
//
// Root cause fixed here: applyGroupAlgoOverlay memoized the stamp at the GROUP
// level by the overlay FILE's mtime. A repo's graph.fb can be rewritten (a
// reparse → fresh doc.Entities carrying the per-repo sentinel community id, e.g.
// -1) after the overlay was first applied. With the file-level memo the apply
// early-returned and that reparsed repo's entities silently reverted to
// community_id:-1 (acme-mobile, #5401) — so grafel_inspect surfaced nothing
// (#5400) and grafel_stats / clusters repo_filter reported 0 communities (#5397)
// for it. The fix re-stamps a repo whenever ITS graph.fb was reparsed since the
// last stamp, independent of the overlay file mtime.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// setupThreeRepoApplyGroup writes three on-disk graph.fb repos (a backend, a
// frontend, and a mobile-like repo whose per-repo Louvain never persisted, so
// its on-disk entities carry the sentinel community id -1), registers the group,
// and returns the State plus the mobile repo's state dir + doc so the test can
// rewrite (reparse) the mobile graph.fb mid-session.
func setupThreeRepoApplyGroup(t *testing.T) (st *State, overlayPath string, cur map[string]int64,
	beID, feID, mobID, mobileStateDir string, mobileDoc *graph.Document) {
	t.Helper()
	// #5443: isolate HOME / XDG_CONFIG_HOME / GRAFEL_HOME into a per-test
	// TempDir BEFORE resolving any config path. Without this the fleet config
	// (registry.ConfigPathFor → registry.ConfigDir) falls back to the REAL
	// ~/.config/grafel/<group>.fleet.json and SaveGroupConfig below clobbers the
	// developer's live config, repointing the group at a deleted t.TempDir.
	testsupport.IsolateHome(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	pathBE := filepath.Join(root, "backend")
	pathFE := filepath.Join(root, "frontend")
	pathMob := filepath.Join(root, "mobile")

	docBE := applyFixtureDoc("backend", "DeviceViewSet")
	docFE := applyFixtureDoc("frontend", "callApi")
	// The mobile doc carries the per-repo SENTINEL community id (-1): its per-repo
	// Louvain never persisted (the real acme-mobile situation, #5397). The overlay
	// will reassign it to a real cross-repo community.
	docMob := applyFixtureDoc("mobile", "coreMobileModule")
	docMob.Entities[0].CommunityID = ip(-1)

	for _, rp := range []struct {
		path string
		doc  *graph.Document
	}{{pathBE, docBE}, {pathFE, docFE}, {pathMob, docMob}} {
		stateDir := daemon.StateDirForRepo(rp.path)
		if err := fbwriter.WriteAtomic(filepath.Join(stateDir, "graph.fb"), rp.doc); err != nil {
			t.Fatalf("write graph.fb: %v", err)
		}
	}

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{
		{Slug: "backend", Path: pathBE},
		{Slug: "frontend", Path: pathFE},
		{Slug: "mobile", Path: pathMob},
	}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}

	inMem := &Registry{
		Path: filepath.Join(home, "registry.json"),
		Groups: map[string]RegistryGroup{
			"acme": {Repos: map[string]RegistryRepo{
				"backend":  {Path: pathBE},
				"frontend": {Path: pathFE},
				"mobile":   {Path: pathMob},
			}},
		},
	}
	st = NewState(inMem)
	// Release the per-repo graph.fb mmap handles BEFORE t.TempDir's cleanup
	// deletes the directory. On Windows a memory-mapped file cannot be deleted
	// while the mapping/view is open, so t.TempDir's RemoveAll fails with
	// "Access is denied" on graph.fb unless State.Close() unmaps first. Because
	// t.Cleanup runs LIFO and t.TempDir registered its cleanup first (above),
	// registering Close here guarantees the unmap happens before the delete.
	t.Cleanup(st.Close)

	overlayPath, err = groupalgo.OverlayPath("acme")
	if err != nil {
		t.Fatalf("overlay path: %v", err)
	}
	cur, err = groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes: %v", err)
	}
	return st, overlayPath, cur, "backend:DeviceViewSet", "frontend:callApi",
		"mobile:coreMobileModule", daemon.StateDirForRepo(pathMob), docMob
}

// TestApplyOverlay_ReStampsReparsedMobileRepo reproduces #5400/#5401/#5397: after
// the overlay is applied, the mobile repo's graph.fb is rewritten (reparse).
// Without the per-repo re-stamp fix, the mobile entity reverts to community_id:-1
// and inspect/stats/clusters drop it. With the fix it keeps overlay community 80.
func TestApplyOverlay_ReStampsReparsedMobileRepo(t *testing.T) {
	// OFF-path pin (ADR-0027 mmap default-on flip): the re-stamp-on-reparse logic is
	// flag-independent, but this test asserts the stamped community by reading
	// lr.Doc.Entities via entityByID and re-writing the mobile repo's graph.fb to
	// force a reparse. Under flag-ON the Doc is header-only-empty by design (overlay
	// values surface through the Reader + side-table), so the Doc read is empty.
	forceServeFromMMap(t, false)
	st, overlayPath, cur, beID, feID, mobID, mobStateDir, mobDoc := setupThreeRepoApplyGroup(t)

	// Overlay assigns the mobile module to a real cross-repo community (80),
	// the backend god-node to 39, the frontend to 203 — mirroring the live acme
	// overlay shape from the validation report.
	ov := &groupalgo.Overlay{
		Group:        "acme",
		SourceMtimes: cur,
		Results: map[string]groupalgo.EntityOverlay{
			beID:  {CommunityID: 39, PageRank: 0.0065, Centrality: 10415.99, IsGodNode: true},
			feID:  {CommunityID: 203, PageRank: 0.004, Centrality: 16521.23, IsGodNode: true},
			mobID: {CommunityID: 80, PageRank: 0.001, Centrality: 6706, IsArticulationPoint: true},
		},
		Communities: []graph.CommunityResult{
			{ID: 39, Size: 2, AutoName: "serializer-serializers-core"},
			{ID: 203, Size: 1, AutoName: "use-hooks-actions"},
			{ID: 80, Size: 1, AutoName: "features-types-props"},
		},
	}
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	// Pin the overlay file mtime so that after we rewrite it below it keeps the
	// SAME mtime — this is what makes the (buggy) group-level memo skip the
	// re-stamp. The real daemon rewrites the overlay in place on its schedule;
	// what must NOT gate the per-repo re-stamp is the overlay FILE mtime.
	pinned := time.Now().Add(-time.Hour)
	if err := os.Chtimes(overlayPath, pinned, pinned); err != nil {
		t.Fatalf("pin overlay mtime: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v1: %v", err)
	}
	// Sanity: mobile stamped to 80 on the first apply.
	if mob := entityByID(st.Group("acme"), mobID); mob == nil || mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("v1: mobile not stamped to overlay community 80: %v", mob)
	}

	// --- Now reparse ONLY the mobile repo: rewrite its graph.fb with a later
	// mtime, keeping the SENTINEL community id -1 (a fresh per-repo parse). The
	// overlay file is UNCHANGED. This is exactly the live acme-mobile race.
	time.Sleep(10 * time.Millisecond)
	mobDoc.Entities[0].CommunityID = ip(-1) // back to the per-repo sentinel
	// Change the BYTES (add a new entity) so the #3377 content-hash skip does not
	// short-circuit the reparse — we need a genuine fresh parse that rebuilds
	// doc.Entities (and the LabelIndex) carrying the sentinel community ids.
	mobDoc.Entities = append(mobDoc.Entities, graph.Entity{
		ID: "mobile:useMemo", Name: "useMemo", Kind: "function",
		SourceFile: "mobile/hooks.tsx", Language: "ts", CommunityID: ip(-1),
	})
	if err := fbwriter.WriteAtomic(filepath.Join(mobStateDir, "graph.fb"), mobDoc); err != nil {
		t.Fatalf("rewrite mobile graph.fb: %v", err)
	}
	future := time.Now().Add(time.Second)
	_ = os.Chtimes(filepath.Join(mobStateDir, "graph.fb"), future, future)

	// Recompute current mtimes and REWRITE the overlay's recorded source_mtimes so
	// it is not considered stale (the daemon's scheduler keeps the overlay fresh;
	// the bug is the apply MEMO skipping the re-stamp, not staleness). Keep the
	// overlay FILE mtime unchanged-ish so the group-level memo would have skipped.
	cur2, err := groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes v2: %v", err)
	}
	ov.SourceMtimes = cur2
	if err := groupalgo.WriteOverlayTo(overlayPath, ov); err != nil {
		t.Fatalf("rewrite overlay: %v", err)
	}
	// Restore the SAME (pinned) overlay file mtime so the group-level memo would
	// treat the overlay as unchanged — isolating the per-repo re-stamp behavior.
	if err := os.Chtimes(overlayPath, pinned, pinned); err != nil {
		t.Fatalf("re-pin overlay mtime: %v", err)
	}

	if _, err := st.Reload(); err != nil {
		t.Fatalf("reload v2: %v", err)
	}
	grp := st.Group("acme")

	// (#5401) mobile must STILL carry overlay community 80, not the sentinel -1.
	mob := entityByID(grp, mobID)
	if mob == nil {
		t.Fatalf("mobile entity missing after reparse")
	}
	if mob.CommunityID == nil || *mob.CommunityID != 80 {
		t.Fatalf("#5401: reparsed mobile reverted to community_id=%v; want overlay 80 (re-stamp missed)", mob.CommunityID)
	}
	if !mob.IsArticulationPt {
		t.Errorf("#5401: reparsed mobile lost overlay is_articulation_point flag")
	}

	// (#5400) inspect surfaces the mobile entity's overlay community/pagerank.
	inspectOut := inspectAlgo(t, st, "mobile::"+mobID)
	if cid, ok := inspectOut["community_id"].(float64); !ok || int(cid) != 80 {
		t.Errorf("#5400: inspect did not surface mobile overlay community_id=80, got %v", inspectOut["community_id"])
	}
	if pr, ok := inspectOut["pagerank"].(float64); !ok || pr == 0 {
		t.Errorf("#5400: inspect did not surface mobile overlay pagerank, got %v", inspectOut["pagerank"])
	}

	// backend still surfaces its overlay community via inspect too.
	beOut := inspectAlgo(t, st, "backend::"+beID)
	if cid, ok := beOut["community_id"].(float64); !ok || int(cid) != 39 {
		t.Errorf("#5400: inspect did not surface backend overlay community_id=39, got %v", beOut["community_id"])
	}

	// (#5397) grafel_stats reports a non-zero community count for the mobile repo
	// (recovered from the overlay-stamped per-entity CommunityID, not the empty
	// per-repo Doc.Communities).
	mobileComm := statsCommunitiesForRepo(t, st, "mobile")
	if mobileComm < 1 {
		t.Errorf("#5397: grafel_stats reports %d communities for mobile; want >=1 (overlay community 80)", mobileComm)
	}

	// (#5397) clusters repo_filter=mobile surfaces community 80.
	if !clustersHasCommunityForRepoFilter(t, st, "mobile", 80) {
		t.Errorf("#5397: clusters repo_filter=mobile dropped overlay community 80")
	}
}

// --- small helpers (local to this test file) ---

// inspectAlgo runs grafel_inspect with include=community,pagerank,centrality and
// returns the decoded JSON object.
func inspectAlgo(t *testing.T, st *State, prefixedID string) map[string]any {
	t.Helper()
	srv := &Server{State: st}
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"group":     "acme",
		"entity_id": prefixedID,
		"include":   "community,pagerank,centrality",
	}
	res, err := srv.handleGetNode(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGetNode error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("inspect tool error: %v", res)
	}
	return extractResultJSON(t, res)
}

// statsCommunitiesForRepo runs grafel_stats and returns the community count
// reported for the named repo.
func statsCommunitiesForRepo(t *testing.T, st *State, repo string) int {
	t.Helper()
	srv := &Server{State: st}
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"group": "acme"}
	res, err := srv.handleGraphStats(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGraphStats error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("stats tool error: %v", res)
	}
	out := extractResultJSON(t, res)
	repos, _ := out["repos"].([]any)
	for _, ri := range repos {
		m, _ := ri.(map[string]any)
		if m == nil {
			continue
		}
		if m["repo"] == repo {
			if c, ok := m["communities"].(float64); ok {
				return int(c)
			}
		}
	}
	return -1
}

// clustersHasCommunityForRepoFilter runs grafel_clusters repo_filter=[repo] and
// reports whether community id is present.
func clustersHasCommunityForRepoFilter(t *testing.T, st *State, repo string, id int) bool {
	t.Helper()
	srv := &Server{State: st}
	items := clustersResult2(t, srv, map[string]any{"min_size": 1, "repo_filter": []any{repo}})
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m == nil {
			continue
		}
		if cidF, ok := m["id"].(float64); ok && int(cidF) == id {
			return true
		}
	}
	return false
}

// clustersResult2 mirrors clustersResult but targets the "acme" test group.
func clustersResult2(t *testing.T, srv *Server, args map[string]any) []any {
	t.Helper()
	req := mcpapi.CallToolRequest{}
	if args == nil {
		args = map[string]any{}
	}
	args["group"] = "acme"
	req.Params.Arguments = args
	res, err := srv.handleListCommunities(context.Background(), req)
	if err != nil {
		t.Fatalf("handleListCommunities error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("clusters tool error: %v", res)
	}
	var arr []any
	if err := json.Unmarshal([]byte(extractResultText(t, res)), &arr); err != nil {
		t.Fatalf("clusters: unmarshal array failed: %v", err)
	}
	return arr
}
