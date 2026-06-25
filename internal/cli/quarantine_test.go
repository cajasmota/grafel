package cli

// quarantine_test.go — end-to-end tests for `grafel quarantine list/remove/pin`.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/watch"
	"github.com/cajasmota/grafel/internal/registry"
)

// seedQuarantineEnv wires a temp GRAFEL_HOME + XDG config with one group
// containing one repo, and writes a quarantine.json into that repo. Returns the
// repo's absolute path.
func seedQuarantineEnv(t *testing.T, group, repoSlug string, dirs ...watch.QuarantineReason) string {
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
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(cfgDir, group+".fleet.json")
	cfg := registry.GroupConfig{
		Name:  group,
		Repos: []registry.Repo{{Slug: repoSlug, Path: repoPath}},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgData, 0o644); err != nil {
		t.Fatal(err)
	}

	reg := registry.Registry{Version: 1, Groups: []registry.GroupRef{{Name: group, ConfigPath: cfgPath}}}
	regData, _ := json.MarshalIndent(reg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "registry.json"), regData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write the quarantine set via the production write helper.
	qf := struct {
		Version int                      `json:"version"`
		Dirs    []watch.QuarantineReason `json:"dirs"`
	}{Version: 1, Dirs: dirs}
	qData, _ := json.MarshalIndent(qf, "", "  ")
	gdir := filepath.Join(repoPath, ".grafel")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "quarantine.json"), qData, 0o644); err != nil {
		t.Fatal(err)
	}
	return repoPath
}

func runQuarantine(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newQuarantineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("quarantine %v: %v\noutput: %s", args, err, buf.String())
	}
	return buf.String()
}

func TestQuarantineListTable(t *testing.T) {
	at := time.Now().Add(-5 * time.Minute)
	seedQuarantineEnv(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", Detail: "47 events in 2m0s", At: at, Pinned: true},
	)

	out := runQuarantine(t, "list")
	for _, want := range []string{"demo", "api", "app/dist", "churn", "47 events", "yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q\n%s", want, out)
		}
	}
}

func TestQuarantineListJSON(t *testing.T) {
	at := time.Now().Add(-1 * time.Hour)
	seedQuarantineEnv(t, "demo", "api",
		watch.QuarantineReason{Rel: "build/out", Signal: "churn", Detail: "d", At: at},
	)
	out := runQuarantine(t, "list", "--json")
	var rows []quarantineRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0].Path != "build/out" || rows[0].Repo != "api" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestQuarantineListEmpty(t *testing.T) {
	seedQuarantineEnv(t, "demo", "api") // no dirs
	out := runQuarantine(t, "list")
	if !strings.Contains(out, "No quarantined") {
		t.Errorf("expected empty message; got %q", out)
	}
}

func TestQuarantineRemove(t *testing.T) {
	repoPath := seedQuarantineEnv(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", At: time.Now()},
	)
	out := runQuarantine(t, "remove", "api", "app/dist")
	if !strings.Contains(out, "un-quarantined") {
		t.Errorf("expected success message; got %q", out)
	}
	got, _ := watch.ReadQuarantineFile(repoPath)
	if len(got) != 0 {
		t.Fatalf("dir should be removed; got %+v", got)
	}

	// Removing again → "was not quarantined".
	out = runQuarantine(t, "remove", "api", "app/dist")
	if !strings.Contains(out, "was not quarantined") {
		t.Errorf("expected not-quarantined message; got %q", out)
	}
}

func TestQuarantinePinUnpin(t *testing.T) {
	repoPath := seedQuarantineEnv(t, "demo", "api",
		watch.QuarantineReason{Rel: "app/dist", Signal: "churn", At: time.Now()},
	)
	out := runQuarantine(t, "pin", "api", "app/dist")
	if !strings.Contains(out, "pinned api/app/dist") {
		t.Errorf("expected pinned message; got %q", out)
	}
	got, _ := watch.ReadQuarantineFile(repoPath)
	if len(got) != 1 || !got[0].Pinned {
		t.Fatalf("dir should be pinned; got %+v", got)
	}

	out = runQuarantine(t, "unpin", "api", "app/dist")
	if !strings.Contains(out, "unpinned api/app/dist") {
		t.Errorf("expected unpinned message; got %q", out)
	}
	got, _ = watch.ReadQuarantineFile(repoPath)
	if got[0].Pinned {
		t.Fatal("dir should be unpinned")
	}
}

func TestQuarantineRemoveUnknownRepo(t *testing.T) {
	seedQuarantineEnv(t, "demo", "api")
	cmd := newQuarantineCmd()
	cmd.SetArgs([]string{"remove", "nope", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown repo")
	}
}
