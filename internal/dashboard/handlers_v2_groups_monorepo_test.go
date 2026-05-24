package dashboard

// handlers_v2_groups_monorepo_test.go — M3 (#2180) monorepo entity tests.
//
// Verifies that GET /api/v2/groups returns the correct monorepos map when
// a group has a repo with declared modules (polyglot-platform style with
// N sub-paths registered as ONE Repo with N Modules).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// buildModulePaths returns N synthetic module paths like "services/mod-0", …
func buildModulePaths(n int) []string {
	paths := make([]string, n)
	for i := range paths {
		paths[i] = fmt.Sprintf("services/mod-%d", i)
	}
	return paths
}

// TestV2Groups_MonorepoShape verifies that a group with a single 36-module
// repo is returned with:
//   - repos: ["client-fixture-x"]
//   - monorepos: {"client-fixture-x": [36 module paths]}
func TestV2Groups_MonorepoShape(t *testing.T) {
	const repoSlug = "client-fixture-x"
	modules := buildModulePaths(36)

	st := newFakeStore()
	st.groups[repoSlug] = GroupSummary{
		Name:        "polyglot-fleet",
		ConfigPath:  "/tmp/polyglot-fleet.json",
		Repos:       []string{repoSlug},
		EntityCount: 12000,
		LastIndexed: time.Now().UTC().Format(time.RFC3339),
		Monorepos:   map[string][]string{repoSlug: modules},
	}

	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET /api/v2/groups: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	type wireGroup struct {
		ID        string              `json:"id"`
		Repos     []string            `json:"repos"`
		Monorepos map[string][]string `json:"monorepos"`
	}
	var body struct {
		OK   bool        `json:"ok"`
		Data []wireGroup `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Error("ok: want true")
	}
	if len(body.Data) != 1 {
		t.Fatalf("want 1 group, got %d", len(body.Data))
	}

	g := body.Data[0]

	// repos slice must contain exactly the single parent slug.
	if len(g.Repos) != 1 || g.Repos[0] != repoSlug {
		t.Errorf("repos: want [%q], got %v", repoSlug, g.Repos)
	}

	// monorepos must be present and contain the slug key.
	if g.Monorepos == nil {
		t.Fatal("monorepos: want non-nil map")
	}
	gotModules, ok := g.Monorepos[repoSlug]
	if !ok {
		t.Fatalf("monorepos: key %q missing; got keys %v", repoSlug, g.Monorepos)
	}
	if len(gotModules) != 36 {
		t.Errorf("monorepos[%q]: want 36 module paths, got %d", repoSlug, len(gotModules))
	}
	// Spot-check first and last module path.
	if gotModules[0] != "services/mod-0" {
		t.Errorf("module[0]: want %q, got %q", "services/mod-0", gotModules[0])
	}
	if gotModules[35] != "services/mod-35" {
		t.Errorf("module[35]: want %q, got %q", "services/mod-35", gotModules[35])
	}
}

// TestV2Groups_NoModules_MonoreposOmitted verifies that repos without modules
// do NOT include a monorepos key in the response (omitempty contract).
func TestV2Groups_NoModules_MonoreposOmitted(t *testing.T) {
	st := newFakeStore()
	st.groups["standalone"] = GroupSummary{
		Name:        "standalone",
		ConfigPath:  "/tmp/standalone.json",
		Repos:       []string{"repo-a", "repo-b"},
		EntityCount: 500,
		LastIndexed: time.Now().UTC().Format(time.RFC3339),
		// No Monorepos field
	}

	srv, err := NewServer(DefaultConfig(), st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v2/groups")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK   bool `json:"ok"`
		Data []struct {
			ID        string              `json:"id"`
			Monorepos map[string][]string `json:"monorepos"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) == 0 {
		t.Fatal("want at least 1 group")
	}
	if body.Data[0].Monorepos != nil {
		t.Errorf("monorepos: want nil (omitted) for standalone repo, got %v", body.Data[0].Monorepos)
	}
}
