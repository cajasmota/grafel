package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/proto"
)

// newWizardTestServer builds a Server with an isolated GRAFEL_HOME, the
// in-memory fakeStore (so CreateGroup/AddRepo don't touch ~/.grafel), and
// an injected rebuildRunner so the index job completes without a live daemon.
func newWizardTestServer(t *testing.T, runner rebuildRunner) (*httptest.Server, *Server) {
	t.Helper()
	t.Setenv("GRAFEL_HOME", t.TempDir())
	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.rebuildRunner = runner
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return ts, s
}

// writeMonorepo lays down a tiny pnpm monorepo fixture under dir.
func writeMonorepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "pnpm-workspace.yaml"), []byte("packages:\n  - packages/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a", "b"} {
		pkgDir := filepath.Join(dir, "packages", p)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"`+p+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"root"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeChildGitRepos creates a parent directory fixture with N named child
// git repos — each subdirectory contains a real .git dir (empty, just the
// marker). This simulates the multi-repo-parent pattern.
func writeChildGitRepos(t *testing.T, parentDir string, names ...string) {
	t.Helper()
	for _, name := range names {
		gitDir := filepath.Join(parentDir, name, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("writeChildGitRepos mkdir %s: %v", gitDir, err)
		}
	}
}

// TestV2ScanInspect_DetectsMonorepo verifies the scan/detect step resolves a
// real path and surfaces the stack + monorepo layout without any registry write.
func TestV2ScanInspect_DetectsMonorepo(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	repoDir := t.TempDir()
	writeMonorepo(t, repoDir)

	body := `{"path":` + jsonQuote(repoDir) + `}`
	resp, err := http.Post(ts.URL+"/api/v2/scan/inspect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST scan/inspect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var env struct {
		OK   bool               `json:"ok"`
		Data v2ScanInspectReply `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || !env.Data.Valid {
		t.Fatalf("scan should be valid: %+v", env)
	}
	if env.Data.Stack != "node" {
		t.Fatalf("stack = %q; want node", env.Data.Stack)
	}
	if env.Data.Monorepo != "pnpm" {
		t.Fatalf("monorepo = %q; want pnpm", env.Data.Monorepo)
	}
	if len(env.Data.Packages) != 2 {
		t.Fatalf("packages = %v; want 2", env.Data.Packages)
	}
	if env.Data.SuggestedGroup == "" || env.Data.SuggestedSlug == "" {
		t.Fatalf("missing suggestions: %+v", env.Data)
	}
}

// TestV2ScanInspect_InvalidPath verifies a non-existent path returns valid:false
// (200 with an error message, not an HTTP error — the wizard renders inline).
func TestV2ScanInspect_InvalidPath(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	body := `{"path":"/no/such/dir/grafel-test-xyz"}`
	resp, err := http.Post(ts.URL+"/api/v2/scan/inspect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var env struct {
		Data v2ScanInspectReply `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&env)
	if env.Data.Valid {
		t.Fatalf("expected invalid for missing path: %+v", env.Data)
	}
	if env.Data.Error == "" {
		t.Fatalf("expected error message for missing path")
	}
}

// TestV2ScanInspect_DetectsChildGitRepos verifies that pointing at a parent
// directory whose immediate children are git repos returns childGitRepos + sets
// childrenKind to "git-repos".
func TestV2ScanInspect_DetectsChildGitRepos(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	parentDir := t.TempDir()
	writeChildGitRepos(t, parentDir, "core", "frontend", "mobile")

	body := `{"path":` + jsonQuote(parentDir) + `}`
	resp, err := http.Post(ts.URL+"/api/v2/scan/inspect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST scan/inspect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var env struct {
		OK   bool               `json:"ok"`
		Data v2ScanInspectReply `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || !env.Data.Valid {
		t.Fatalf("scan should be valid: %+v", env)
	}
	if len(env.Data.ChildGitRepos) != 3 {
		t.Fatalf("childGitRepos = %v; want 3 entries (core, frontend, mobile)", env.Data.ChildGitRepos)
	}
	if env.Data.ChildrenKind != "git-repos" {
		t.Fatalf("childrenKind = %q; want git-repos", env.Data.ChildrenKind)
	}
	// Packages must be empty — child git repos take precedence.
	if len(env.Data.Packages) != 0 {
		t.Fatalf("packages = %v; want empty (child git repos take precedence)", env.Data.Packages)
	}
}

// TestV2ScanInspect_PrefersChildGitReposOverMonorepo verifies that when a
// directory has BOTH a pnpm-workspace.yaml (monorepo packages) AND child git
// repos, child git repos take precedence (childrenKind="git-repos", packages=[]).
func TestV2ScanInspect_PrefersChildGitReposOverMonorepo(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	parentDir := t.TempDir()
	// Plant a monorepo marker.
	writeMonorepo(t, parentDir)
	// Also plant child git repos (the precedence case).
	writeChildGitRepos(t, parentDir, "repo-a", "repo-b")

	body := `{"path":` + jsonQuote(parentDir) + `}`
	resp, err := http.Post(ts.URL+"/api/v2/scan/inspect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST scan/inspect: %v", err)
	}
	defer resp.Body.Close()
	var env struct {
		OK   bool               `json:"ok"`
		Data v2ScanInspectReply `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || !env.Data.Valid {
		t.Fatalf("scan should be valid: %+v", env)
	}
	if env.Data.ChildrenKind != "git-repos" {
		t.Fatalf("childrenKind = %q; want git-repos (child git repos take precedence)", env.Data.ChildrenKind)
	}
	if len(env.Data.ChildGitRepos) < 2 {
		t.Fatalf("childGitRepos = %v; want >= 2", env.Data.ChildGitRepos)
	}
	if len(env.Data.Packages) != 0 {
		t.Fatalf("packages = %v; want empty when child git repos present", env.Data.Packages)
	}
}

// TestV2CreateGroupFromScan_CreatesAndIndexes verifies the full wizard create
// path: it creates the group, registers the repo, and enqueues an index job
// that the runner drives to done.
func TestV2CreateGroupFromScan_CreatesAndIndexes(t *testing.T) {
	done := make(chan struct{}, 1)
	runner := func(args proto.RebuildArgs) (proto.RebuildReply, error) {
		if args.Group != "wiz" {
			t.Errorf("runner group = %q; want wiz", args.Group)
		}
		done <- struct{}{}
		return proto.RebuildReply{Repos: []string{"core"}, TotalEntities: 10, TotalRels: 3}, nil
	}
	ts, _ := newWizardTestServer(t, runner)
	repoDir := t.TempDir()

	body := `{"name":"wiz","repos":[{"path":` + jsonQuote(repoDir) + `,"slug":"core"}]}`
	resp, err := http.Post(ts.URL+"/api/v2/groups/from-scan", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST from-scan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d; want 202", resp.StatusCode)
	}
	var env struct {
		OK   bool     `json:"ok"`
		Data v2JobAck `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK || env.Data.JobID == "" || env.Data.Group != "wiz" {
		t.Fatalf("bad ack: %+v", env)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("index runner never fired")
	}

	// Poll the job to done.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := http.Get(ts.URL + "/api/v2/jobs/" + env.Data.JobID)
		var je struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&je)
		r.Body.Close()
		if je.Data.Status == actionJobDone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job never reached done")
}

// TestV2CreateGroupFromScan_RequiresRepos verifies an empty repo list is rejected.
func TestV2CreateGroupFromScan_RequiresRepos(t *testing.T) {
	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	resp, err := http.Post(ts.URL+"/api/v2/groups/from-scan", "application/json", strings.NewReader(`{"name":"x","repos":[]}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", resp.StatusCode)
	}
}

// TestV2DetectMCPTools returns the detected MCP-capable tools with the smart
// default (#5344). HOME is redirected to an isolated dir holding one Claude
// config so the detector sees exactly one tool, default-checked (recent).
func TestV2DetectMCPTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// A fresh ~/.claude.json (recent mtime) → detected + default-checked.
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ts, _ := newWizardTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	resp, err := http.Get(ts.URL + "/api/v2/mcp-tools/detect")
	if err != nil {
		t.Fatalf("GET mcp-tools/detect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var env struct {
		OK   bool                  `json:"ok"`
		Data v2MCPToolsDetectReply `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatalf("not ok: %+v", env)
	}
	var claude *v2MCPToolStatus
	for i := range env.Data.Tools {
		if env.Data.Tools[i].ID == "claude" {
			claude = &env.Data.Tools[i]
		}
	}
	if claude == nil {
		t.Fatalf("claude not detected; tools=%+v", env.Data.Tools)
	}
	if !claude.DefaultSelected {
		t.Errorf("claude (recent config) should be default-selected: %+v", *claude)
	}
}

// jsonQuote quotes a string for safe embedding in a JSON literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
