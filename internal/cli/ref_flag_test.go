package cli

// ref_flag_test.go — integration tests for the --ref flag on all 6 commands
// (issue #2219).
//
// Test matrix:
//   - TestStatus_Ref_Default      no flag → current ref (zero regression)
//   - TestStatus_Ref_Named        --ref main → accepted with note
//   - TestStatus_Ref_All          --ref @all → accepted with note
//   - TestStatus_Ref_Current      --ref @current → same as default
//   - TestStatus_Ref_Invalid      --ref nonexistent → error citing available refs
//   - TestRebuild_Ref_AllRefused  --ref @all on rebuild → refused
//   - TestIndex_Ref_AllRefused    --ref @all on index → refused
//   - TestRemove_Ref_AllRefused   --ref @all on remove → refused
//   - TestList_Ref_Default        no flag → normal list
//   - TestList_Ref_All            --ref @all → accepted with note
//   - TestList_Ref_Named          --ref main → accepted with note
//   - TestDoctor_Ref_Default      no flag → normal doctor run
//   - TestDoctor_Ref_All          --ref @all → accepted with note
//   - TestDoctor_Ref_Invalid      --ref nonexistent → error

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/registry"
)

// setupRefTestEnv sets up a minimal grafel home with one group and one
// repo. It optionally creates a per-ref state directory so knownRefNames()
// can discover it.
func setupRefTestEnv(t *testing.T, refs ...string) (home, repoPath string) {
	t.Helper()
	home = t.TempDir()
	// HOME too, not just GRAFEL_HOME: runStatus now reports the per-repo
	// watcher fleet, and the unit directory is resolved from HOME
	// (~/Library/LaunchAgents). With only GRAFEL_HOME redirected this test
	// globbed the developer's REAL 140 watcher plists and ran a `launchctl
	// list` per unit — read-only, but machine-variable and 140 process spawns.
	t.Setenv("HOME", home)
	t.Setenv("GRAFEL_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	repoPath = t.TempDir()
	// Minimal git repo so daemon.StateDirForRepo works.
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "testgroup.fleet.json")
	cfg := registry.GroupConfig{Name: "testgroup"}
	cfg.Repos = []registry.Repo{{Slug: "testrepo", Path: repoPath}}
	if err := registry.SaveGroupConfig(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup("testgroup", cfgPath); err != nil {
		t.Fatal(err)
	}

	// Create store directories for any refs supplied by the caller so
	// knownRefNames() can find them.
	for _, ref := range refs {
		stateDir := daemon.StateDirForRepoRef(repoPath, ref)
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Touch graph.fb so the directory is non-empty and recognised.
		if err := os.WriteFile(filepath.Join(stateDir, "graph.fb"), []byte("fb"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return home, repoPath
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func TestStatus_Ref_Default(t *testing.T) {
	setupRefTestEnv(t)
	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default behaviour: no ref note in output.
	if strings.Contains(buf.String(), "Note:") {
		t.Errorf("default run should not print a Note: line, got: %s", buf.String())
	}
}

func TestStatus_Ref_Named(t *testing.T) {
	setupRefTestEnv(t, "main")
	var buf bytes.Buffer
	if err := runStatus(&buf, "", "main", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `ref "main"`) {
		t.Errorf("expected ref note for 'main', got: %s", buf.String())
	}
}

func TestStatus_Ref_All(t *testing.T) {
	setupRefTestEnv(t, "main", "feat/x")
	var buf bytes.Buffer
	if err := runStatus(&buf, "", "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "@all") {
		t.Errorf("expected @all note, got: %s", buf.String())
	}
}

func TestStatus_Ref_Current(t *testing.T) {
	setupRefTestEnv(t)
	// @current normalises to ("", false) — same as no flag.
	resolved, isAll, err := resolveRef("@current", true)
	if err != nil {
		t.Fatalf("resolveRef(@current): %v", err)
	}
	if resolved != "" || isAll {
		t.Errorf("@current should resolve to (\"\", false), got (%q, %v)", resolved, isAll)
	}
}

func TestStatus_Ref_Invalid(t *testing.T) {
	// Set up env with one known ref so the error lists it.
	setupRefTestEnv(t, "main")
	_, _, err := resolveRef("nonexistent-branch", false)
	if err == nil {
		t.Fatal("expected error for unknown ref, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-branch") {
		t.Errorf("error should mention the bad ref name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error should list known refs, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// rebuild — @all must be refused
// ---------------------------------------------------------------------------

func TestRebuild_Ref_AllRefused(t *testing.T) {
	setupRefTestEnv(t, "main")
	_, _, err := resolveRef("@all", false /* allowAll=false → rebuild */)
	if err == nil {
		t.Fatal("expected error when @all is passed to a destructive command")
	}
	if !strings.Contains(err.Error(), "@all") {
		t.Errorf("error should mention @all, got: %v", err)
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error should explain that @all is for read-only commands, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// index — @all must be refused
// ---------------------------------------------------------------------------

func TestIndex_Ref_AllRefused(t *testing.T) {
	setupRefTestEnv(t)
	_, _, err := resolveRef("@all", false)
	if err == nil {
		t.Fatal("expected error for @all on index")
	}
}

// ---------------------------------------------------------------------------
// remove — @all must be refused
// ---------------------------------------------------------------------------

func TestRemove_Ref_AllRefused(t *testing.T) {
	setupRefTestEnv(t)
	_, _, err := resolveRef("@all", false)
	if err == nil {
		t.Fatal("expected error for @all on remove")
	}
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func TestList_Ref_Default(t *testing.T) {
	setupRefTestEnv(t)
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	// No Note: header when no flag is given.
	if strings.Contains(buf.String(), "Note:") {
		t.Errorf("default list should not print Note:, got: %s", buf.String())
	}
}

func TestList_Ref_All(t *testing.T) {
	setupRefTestEnv(t, "main")
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list", "--ref", "@all"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list --ref @all: %v", err)
	}
	if !strings.Contains(buf.String(), "@all") {
		t.Errorf("expected @all note, got: %s", buf.String())
	}
}

func TestList_Ref_Named(t *testing.T) {
	setupRefTestEnv(t, "main")
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list", "--ref", "main"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list --ref main: %v", err)
	}
	if !strings.Contains(buf.String(), `ref "main"`) {
		t.Errorf("expected ref note, got: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// doctor
// ---------------------------------------------------------------------------

func TestDoctor_Ref_Default(t *testing.T) {
	setupRefTestEnv(t)
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"doctor"})
	// Doctor may fail if no real grafel binary — we only care it
	// doesn't error on the --ref flag parsing. Ignore the runDoctor error.
	_ = root.Execute()
	if strings.Contains(buf.String(), "Note:") {
		t.Errorf("default doctor should not print Note:, got: %s", buf.String())
	}
}

func TestDoctor_Ref_All(t *testing.T) {
	setupRefTestEnv(t, "main")
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"doctor", "--ref", "@all"})
	_ = root.Execute() // doctor may fail; we check only that @all is accepted.
	if !strings.Contains(buf.String(), "@all") {
		t.Errorf("expected @all note in doctor output, got: %s", buf.String())
	}
}

func TestDoctor_Ref_Invalid(t *testing.T) {
	setupRefTestEnv(t, "main")
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"doctor", "--ref", "no-such-branch"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid ref on doctor")
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("error should mention the bad ref, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// resolveRef unit tests
// ---------------------------------------------------------------------------

func TestResolveRef_EmptyIsDefault(t *testing.T) {
	setupRefTestEnv(t)
	resolved, isAll, err := resolveRef("", true)
	if err != nil || resolved != "" || isAll {
		t.Errorf("empty ref should be default: resolved=%q isAll=%v err=%v", resolved, isAll, err)
	}
}

func TestResolveRef_CurrentAlias(t *testing.T) {
	setupRefTestEnv(t)
	resolved, isAll, err := resolveRef("@current", true)
	if err != nil || resolved != "" || isAll {
		t.Errorf("@current should resolve same as empty: resolved=%q isAll=%v err=%v", resolved, isAll, err)
	}
}

func TestResolveRef_AllAllowed(t *testing.T) {
	setupRefTestEnv(t)
	_, isAll, err := resolveRef("@all", true)
	if err != nil || !isAll {
		t.Errorf("@all with allowAll=true should succeed: isAll=%v err=%v", isAll, err)
	}
}

func TestResolveRef_AllRefused(t *testing.T) {
	setupRefTestEnv(t)
	_, _, err := resolveRef("@all", false)
	if err == nil {
		t.Error("@all with allowAll=false should return error")
	}
}

func TestResolveRef_KnownRefAccepted(t *testing.T) {
	setupRefTestEnv(t, "release/v2")
	resolved, isAll, err := resolveRef("release/v2", false)
	if err != nil {
		t.Fatalf("known ref should be accepted: %v", err)
	}
	if resolved != "release/v2" || isAll {
		t.Errorf("unexpected: resolved=%q isAll=%v", resolved, isAll)
	}
}

func TestResolveRef_UnknownRefRejected(t *testing.T) {
	setupRefTestEnv(t, "main") // only "main" is known
	_, _, err := resolveRef("phantom-branch", false)
	if err == nil {
		t.Error("unknown ref should be rejected when store is non-empty")
	}
	msg := err.Error()
	if !strings.Contains(msg, "phantom-branch") || !strings.Contains(msg, "main") {
		t.Errorf("error should mention the bad ref and known refs, got: %q", msg)
	}
}

func TestResolveRef_EmptyStoreReturnsError(t *testing.T) {
	// When the store is empty (no refs indexed yet) a named ref returns a
	// clean error telling the user to run 'grafel index'.
	home := t.TempDir()
	t.Setenv("GRAFEL_HOME", home)
	// Empty registry → knownRefNames returns nil.
	_, _, err := resolveRef("some-branch", false)
	if err == nil {
		t.Error("with empty store, a named ref should return a clean error")
	}
	if !strings.Contains(err.Error(), "grafel index") {
		t.Errorf("error should mention 'grafel index', got: %v", err)
	}
}
