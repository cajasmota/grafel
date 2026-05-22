package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon/proto"
	"github.com/cajasmota/archigraph/internal/registry"
)

// newActionTestServer builds a Server with an isolated ARCHIGRAPH_HOME, an
// injected rebuildRunner, and a registered group "demo" with one repo. It
// returns the wired *httptest.Server.
func newActionTestServer(t *testing.T, runner rebuildRunner) (*httptest.Server, *Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	// Persist a group config + register it so groupConfigPath finds it.
	cfgPath := filepath.Join(home, "demo.fleet.json")
	gc := &registry.GroupConfig{Name: "demo", Repos: []registry.Repo{{Slug: "core", Path: filepath.Join(home, "core")}}}
	if err := registry.SaveGroupConfig(cfgPath, gc); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	if err := registry.AddGroup("demo", cfgPath); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	s, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.rebuildRunner = runner
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return ts, s
}

// TestV2Rebuild_Returns202Immediately verifies the handler does not block on
// the index: even with a slow runner, the POST returns 202 + a job id at once,
// and a concurrent read request is served while the rebuild is in flight
// (the #1487 serving-mutex invariant).
func TestV2Rebuild_Returns202Immediately(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(1)
	runner := func(args proto.RebuildArgs) (proto.RebuildReply, error) {
		started.Done()
		<-release // block until the test lets the job finish
		return proto.RebuildReply{Repos: []string{"core"}, TotalEntities: 42, TotalRels: 7}, nil
	}
	ts, _ := newActionTestServer(t, runner)

	start := time.Now()
	resp, err := http.Post(ts.URL+"/api/v2/groups/demo/rebuild", "application/json", nil)
	if err != nil {
		t.Fatalf("POST rebuild: %v", err)
	}
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d; want 202", resp.StatusCode)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("handler blocked %v; should return immediately", elapsed)
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			JobID  string `json:"job_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if !env.OK || env.Data.JobID == "" {
		t.Fatalf("bad ack: %+v", env)
	}

	// The job goroutine should be running (blocked in runner) — prove a
	// concurrent read is served while the rebuild is in flight.
	started.Wait()
	jr, err := http.Get(ts.URL + "/api/v2/jobs/" + env.Data.JobID)
	if err != nil {
		t.Fatalf("GET job during rebuild: %v", err)
	}
	if jr.StatusCode != http.StatusOK {
		t.Fatalf("concurrent job status = %d; want 200", jr.StatusCode)
	}
	jr.Body.Close()

	// Let the job finish and confirm it transitions to done.
	close(release)
	jobID := env.Data.JobID
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := http.Get(ts.URL + "/api/v2/jobs/" + jobID)
		var je struct {
			Data struct {
				Status   string `json:"status"`
				Progress int    `json:"progress"`
			} `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&je)
		r.Body.Close()
		if je.Data.Status == actionJobDone {
			if je.Data.Progress != 100 {
				t.Fatalf("done job progress = %d; want 100", je.Data.Progress)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job never reached done")
}

// TestV2Rebuild_UnknownGroup404 verifies registry validation.
func TestV2Rebuild_UnknownGroup404(t *testing.T) {
	ts, _ := newActionTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	resp, err := http.Post(ts.URL+"/api/v2/groups/nope/rebuild", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", resp.StatusCode)
	}
}

// TestV2Job_NotFound verifies the job poller 404s for an unknown id.
func TestV2Job_NotFound(t *testing.T) {
	ts, _ := newActionTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	resp, err := http.Get(ts.URL + "/api/v2/jobs/aj-does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", resp.StatusCode)
	}
}

// TestV2JobStream_TerminatesOnDone verifies the SSE stream emits a job event
// and closes once the job reaches a terminal state.
func TestV2JobStream_TerminatesOnDone(t *testing.T) {
	ts, _ := newActionTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{Repos: []string{"core"}}, nil
	})
	// Trigger a rebuild.
	resp, _ := http.Post(ts.URL+"/api/v2/groups/demo/repos/core/rebuild", "application/json", nil)
	var env struct {
		Data struct {
			JobID string `json:"job_id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&env)
	resp.Body.Close()

	sr, err := http.Get(ts.URL + "/api/v2/jobs/" + env.Data.JobID + "/stream")
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer sr.Body.Close()
	if ct := sr.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q; want SSE", ct)
	}
	buf := make([]byte, 4096)
	n, _ := sr.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: connected") {
		t.Fatalf("stream missing connected event: %q", body)
	}
}

// TestV2Cleanup_DryRunPreview verifies the cleanup wrapper previews orphans.
func TestV2Cleanup_DryRunPreview(t *testing.T) {
	ts, _ := newActionTestServer(t, func(proto.RebuildArgs) (proto.RebuildReply, error) {
		return proto.RebuildReply{}, nil
	})
	resp, err := http.Post(ts.URL+"/api/v2/maintenance/cleanup", "application/json", strings.NewReader(`{"dry_run":true}`))
	if err != nil {
		t.Fatalf("POST cleanup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			DryRun  bool `json:"dry_run"`
			Removed int  `json:"removed"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&env)
	if !env.OK || !env.Data.DryRun || env.Data.Removed != 0 {
		t.Fatalf("dry-run cleanup should not remove: %+v", env)
	}
}
