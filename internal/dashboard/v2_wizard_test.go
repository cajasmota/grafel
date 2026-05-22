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

	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// newWizardTestServer builds a Server with an isolated ARCHIGRAPH_HOME, the
// in-memory fakeStore (so CreateGroup/AddRepo don't touch ~/.archigraph), and
// an injected rebuildRunner so the index job completes without a live daemon.
func newWizardTestServer(t *testing.T, runner rebuildRunner) (*httptest.Server, *Server) {
	t.Helper()
	t.Setenv("ARCHIGRAPH_HOME", t.TempDir())
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
	body := `{"path":"/no/such/dir/archigraph-test-xyz"}`
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

// jsonQuote quotes a string for safe embedding in a JSON literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
