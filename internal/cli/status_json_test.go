package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestResolveRepoForCwd_LongestPrefixMatch is the RED test for the
// #5725/#5729-W1 poll-safe `grafel status --json` cwd resolver: given a
// registered repo and a cwd that is a subdirectory of it, the resolver must
// return the repo's root path — not merely an exact match — since a
// statusline is typically invoked from wherever the shell's cwd happens to
// be inside the repo tree.
func TestResolveRepoForCwd_LongestPrefixMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

	repoRoot := t.TempDir()
	sub := filepath.Join(repoRoot, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(tmpHome, ".config", "grafel")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(configDir, "g.fleet.json")
	cfg := &registry.GroupConfig{
		Name:  "g",
		Repos: []registry.Repo{{Slug: "r", Path: repoRoot}},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{
		Version: 1,
		Groups:  []registry.GroupRef{{Name: "g", ConfigPath: cfgPath}},
	}
	if err := registry.Save(reg); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRepoForCwd(sub)
	if err != nil {
		t.Fatalf("resolveRepoForCwd: %v", err)
	}
	wantAbs, _ := filepath.EvalSymlinks(repoRoot)
	gotAbs, _ := filepath.EvalSymlinks(got)
	if gotAbs != wantAbs {
		t.Errorf("resolveRepoForCwd(%q) = %q, want %q", sub, got, repoRoot)
	}
}

// TestResolveRepoForCwd_NoMatch confirms an unregistered cwd (not inside any
// known repo) surfaces a clear error rather than a hang or a panic.
func TestResolveRepoForCwd_NoMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

	reg := &registry.Registry{Version: 1}
	if err := registry.Save(reg); err != nil {
		t.Fatal(err)
	}

	elsewhere := t.TempDir()
	if _, err := resolveRepoForCwd(elsewhere); err == nil {
		t.Fatal("expected an error for a cwd outside any registered repo")
	}
}

// TestRunStatusJSON_ReadsFileNotSocket is the RED test proving `grafel
// status --json` is poll-safe: it must return the on-disk statusfile.File
// contents WITHOUT dialing the daemon — verified here by there being no
// daemon socket at all (client.Dial would fail/hang-detect if invoked; this
// test simply never starts one, so any success here proves runStatusJSON
// didn't need one).
func TestRunStatusJSON_ReadsFileNotSocket(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

	repoRoot := t.TempDir()
	configDir := filepath.Join(tmpHome, ".config", "grafel")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(configDir, "g.fleet.json")
	cfg := &registry.GroupConfig{
		Name:  "g",
		Repos: []registry.Repo{{Slug: "r", Path: repoRoot}},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{
		Version: 1,
		Groups:  []registry.GroupRef{{Name: "g", ConfigPath: cfgPath}},
	}
	if err := registry.Save(reg); err != nil {
		t.Fatal(err)
	}

	want := &statusfile.File{
		EnginePID:     12345,
		HeartbeatAt:   time.Now().UTC(),
		Version:       "test-version",
		RepoPath:      repoRoot,
		IndexedCommit: "deadbeef",
		Entities:      7,
		Relationships: 3,
	}
	if err := statusfile.Write(repoRoot, want); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runStatusJSON(&buf, repoRoot) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runStatusJSON: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runStatusJSON did not return promptly — must never block on a daemon RPC")
	}

	var got statusfile.File
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if got.IndexedCommit != "deadbeef" || got.Entities != 7 {
		t.Errorf("got = %+v, want IndexedCommit=deadbeef Entities=7", got)
	}
}

// TestRunStatusJSON_UnknownFallback confirms a repo with no status file yet
// (engine never touched it, or is down) returns a well-formed "unknown"
// result rather than an error/hang.
func TestRunStatusJSON_UnknownFallback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("GRAFEL_HOME", tmpHome)

	repoRoot := t.TempDir()
	configDir := filepath.Join(tmpHome, ".config", "grafel")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(configDir, "g.fleet.json")
	cfg := &registry.GroupConfig{
		Name:  "g",
		Repos: []registry.Repo{{Slug: "r", Path: repoRoot}},
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{
		Version: 1,
		Groups:  []registry.GroupRef{{Name: "g", ConfigPath: cfgPath}},
	}
	if err := registry.Save(reg); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runStatusJSON(&buf, repoRoot); err != nil {
		t.Fatalf("runStatusJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if s, _ := out["status"].(string); s != "unknown" {
		t.Errorf(`out["status"] = %v, want "unknown"`, out["status"])
	}
}
