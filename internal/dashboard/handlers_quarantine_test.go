package dashboard

// handlers_quarantine_test.go — tests for the Q2 quarantine endpoints (#5617).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/registry"
)

// seedQuarantineRegistry wires a temp GRAFEL_HOME registry with one group +
// repo and writes a quarantine.json into the repo. Returns the repo path.
func seedQuarantineRegistry(t *testing.T, group, repoSlug string, dirs ...watch.QuarantineReason) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	cfgHome := filepath.Join(home, "config")
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	cfgDir := filepath.Join(cfgHome, "grafel")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(home, "repos", repoSlug)
	if err := os.MkdirAll(filepath.Join(repoPath, ".grafel"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{Name: group, Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}}}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.Registry{Version: 1, Groups: []registry.GroupRef{{Name: group, ConfigPath: cfgPath}}}
	regData, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regData, 0o644); err != nil {
		t.Fatal(err)
	}

	qf := struct {
		Version int                      `json:"version"`
		Dirs    []watch.QuarantineReason `json:"dirs"`
	}{Version: 1, Dirs: dirs}
	qData, _ := json.MarshalIndent(qf, "", "  ")
	if err := os.WriteFile(filepath.Join(repoPath, ".grafel", "quarantine.json"), qData, 0o644); err != nil {
		t.Fatal(err)
	}
	return repoPath
}

func newQuarantineServer(t *testing.T) (string, func()) {
	t.Helper()
	return newTestServer(t, newFakeStore(), DefaultConfig())
}

func TestQuarantineListEndpoint(t *testing.T) {
	seedQuarantineRegistry(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", Detail: "47 events", At: time.Now(), Pinned: true},
	)
	url, done := newQuarantineServer(t)
	defer done()

	resp, err := http.Get(url + "/api/groups/demo/quarantine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var reply QuarantineListReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Entries) != 1 {
		t.Fatalf("want 1 entry; got %+v", reply.Entries)
	}
	e := reply.Entries[0]
	if e.Repo != "api" || e.Path != "app/dist" || !e.Pinned || e.Signal != "churn" {
		t.Fatalf("unexpected entry: %+v", e)
	}
}

func TestQuarantineListUnknownGroup(t *testing.T) {
	seedQuarantineRegistry(t, "demo", "api")
	url, done := newQuarantineServer(t)
	defer done()
	resp, err := http.Get(url + "/api/groups/nope/quarantine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404; got %d", resp.StatusCode)
	}
}

func TestQuarantineUnquarantineEndpoint(t *testing.T) {
	repoPath := seedQuarantineRegistry(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", At: time.Now()},
	)
	url, done := newQuarantineServer(t)
	defer done()

	body, _ := json.Marshal(map[string]any{"rel": "app/dist"})
	resp, err := http.Post(url+"/api/groups/demo/repos/api/quarantine/unquarantine",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var reply QuarantineActionReply
	json.NewDecoder(resp.Body).Decode(&reply)
	if !reply.Changed || reply.Action != "unquarantine" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
	got, _ := watch.ReadQuarantineFile(repoPath)
	if len(got) != 0 {
		t.Fatalf("dir should be removed; got %+v", got)
	}
}

func TestQuarantinePinEndpoint(t *testing.T) {
	repoPath := seedQuarantineRegistry(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", At: time.Now()},
	)
	url, done := newQuarantineServer(t)
	defer done()

	body, _ := json.Marshal(map[string]any{"rel": "app/dist", "pinned": true})
	resp, err := http.Post(url+"/api/groups/demo/repos/api/quarantine/pin",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var reply QuarantineActionReply
	json.NewDecoder(resp.Body).Decode(&reply)
	if !reply.Changed || reply.Action != "pin" {
		t.Fatalf("unexpected reply: %+v", reply)
	}
	got, _ := watch.ReadQuarantineFile(repoPath)
	if len(got) != 1 || !got[0].Pinned {
		t.Fatalf("dir should be pinned; got %+v", got)
	}
}

func TestQuarantineActionMissingRel(t *testing.T) {
	seedQuarantineRegistry(t, "demo", "api")
	url, done := newQuarantineServer(t)
	defer done()
	resp, err := http.Post(url+"/api/groups/demo/repos/api/quarantine/pin",
		"application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400; got %d", resp.StatusCode)
	}
}
