package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// registerRefFixture wires GRAFEL_HOME + GRAFEL_DAEMON_ROOT, a one-repo group,
// and returns the repo path so the caller can populate per-ref state dirs.
func registerRefFixture(t *testing.T, groupName, repoSlug string) (repoPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, "store"))

	repoPath = filepath.Join(home, "myrepo")
	_ = os.MkdirAll(repoPath, 0o755)

	cfg := &registry.GroupConfig{
		Name:  groupName,
		Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}},
	}
	cfgDir := filepath.Join(home, "groups")
	_ = os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, groupName+".fleet.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	regRaw, _ := json.Marshal(map[string]any{
		"version": 1,
		"groups":  []map[string]any{{"name": groupName, "config_path": cfgPath}},
	})
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	return repoPath
}

// TestKnownRefsForGroup_GenOnlyRefIncluded is the #5891 regression for the
// missed reader in knownRefsForGroup: a ref indexed under the gen layout has
// `current` + graph.<gen>.fb and NO flat graph.fb (and, by default, no
// graph.json). The pre-fix hardcoded os.Stat(graph.fb) dropped such a ref, so
// resolveRefParam(?ref=<branch>) returned HTTP 400 and the branch vanished from
// `available`. Assert the gen-only ref (and a json-only ref) are BOTH included.
func TestKnownRefsForGroup_GenOnlyRefIncluded(t *testing.T) {
	const group, slug = "gengroup", "genrepo"
	repoPath := registerRefFixture(t, group, slug)

	// Ref "main": gen-only layout (current pointer + graph.<gen>.fb, NO flat
	// graph.fb, NO graph.json).
	genRefDir := daemon.StateDirForRepoRef(repoPath, "main")
	if err := os.MkdirAll(genRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.WriteGenGraph(genRefDir, []byte("gen-bytes")); err != nil {
		t.Fatalf("WriteGenGraph: %v", err)
	}
	if _, err := os.Stat(filepath.Join(genRefDir, "graph.fb")); err == nil {
		t.Fatal("precondition: a flat graph.fb exists — gen layout must not create it")
	}

	// Ref "feat/json-only": json-only layout (no fb of any kind).
	jsonRefDir := daemon.StateDirForRepoRef(repoPath, "feat/json-only")
	if err := os.MkdirAll(jsonRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jsonRefDir, "graph.json"), []byte(`{"entities":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	known := knownRefsForGroup(group)
	has := func(want string) bool {
		for _, k := range known {
			if k == want {
				return true
			}
		}
		return false
	}
	if !has("main") {
		t.Fatalf("gen-only ref 'main' dropped from knownRefsForGroup=%v (would 400 on ?ref=main)", known)
	}
	if !has("feat/json-only") {
		t.Fatalf("json-only ref dropped from knownRefsForGroup=%v", known)
	}

	// End-to-end: resolveRefParam must accept ?ref=main (ok=true, no 400).
	req := httptest.NewRequest(http.MethodGet, "/api/groups/"+group+"/stats?ref=main", nil)
	rec := httptest.NewRecorder()
	ref, isAll, ok := resolveRefParam(rec, req, group)
	if !ok {
		t.Fatalf("resolveRefParam(?ref=main) rejected with %d (gen-only ref treated as invalid)", rec.Code)
	}
	if ref != "main" || isAll {
		t.Fatalf("resolveRefParam(?ref=main) = (%q, isAll=%v), want (main, false)", ref, isAll)
	}
}

// TestKnownRefsForGroup_SegmentSetRefIncluded is the RED test for #5915 J2
// slice-2: a ref indexed as a SEGMENT-SET (graph.<gen>/ dir + manifest.json,
// no flat graph.fb, no graph.json) must be reported present by
// knownRefsForGroup. The pre-fix os.Stat(graph.CurrentGraphPath(refDir))
// gate only ever resolves a flat .fb path, so it dropped the ref (400 on
// ?ref=<branch>).
func TestKnownRefsForGroup_SegmentSetRefIncluded(t *testing.T) {
	const group, slug = "seggroup", "segrepo"
	repoPath := registerRefFixture(t, group, slug)

	segRefDir := daemon.StateDirForRepoRef(repoPath, "wip-seg")
	if err := os.MkdirAll(segRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDashboardSegmentSetFixture(t, segRefDir, 3, time.Time{})
	if _, err := os.Stat(filepath.Join(segRefDir, "graph.fb")); err == nil {
		t.Fatal("precondition: a flat graph.fb exists -- segment-set layout must not create it")
	}
	if _, err := os.Stat(filepath.Join(segRefDir, "graph.json")); err == nil {
		t.Fatal("precondition: a graph.json exists -- must not mask the segment-set-only case")
	}

	known := knownRefsForGroup(group)
	has := func(want string) bool {
		for _, k := range known {
			if k == want {
				return true
			}
		}
		return false
	}
	if !has("wip-seg") {
		t.Fatalf("segment-set ref 'wip-seg' dropped from knownRefsForGroup=%v (would 400 on ?ref=wip-seg)", known)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/groups/"+group+"/stats?ref=wip-seg", nil)
	rec := httptest.NewRecorder()
	ref, isAll, ok := resolveRefParam(rec, req, group)
	if !ok {
		t.Fatalf("resolveRefParam(?ref=wip-seg) rejected with %d (segment-set ref treated as invalid)", rec.Code)
	}
	if ref != "wip-seg" || isAll {
		t.Fatalf("resolveRefParam(?ref=wip-seg) = (%q, isAll=%v), want (wip-seg, false)", ref, isAll)
	}
}

// TestAllRefsForRepo_SegmentSetRefIncluded guards the allRefsForRepo sibling
// (used by the group-refs aggregation path) against the same #5915 J2
// slice-2 gap: a segment-set-only ref must be included.
func TestAllRefsForRepo_SegmentSetRefIncluded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(home, "store"))

	repoPath := filepath.Join(home, "myrepo")
	_ = os.MkdirAll(repoPath, 0o755)

	segRefDir := daemon.StateDirForRepoRef(repoPath, "wip-seg")
	if err := os.MkdirAll(segRefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDashboardSegmentSetFixture(t, segRefDir, 3, time.Time{})

	refs := allRefsForRepo(repoPath)
	found := false
	for _, r := range refs {
		if r == "wip-seg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("segment-set ref 'wip-seg' dropped from allRefsForRepo=%v", refs)
	}
}
