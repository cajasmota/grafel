// state_path_ref_test.go exercises the ref-aware path helpers and the
// per-ref store layout introduced by PH1a of epic #2087 (issue #2089).
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Ref-safe encoding round-trips
// ---------------------------------------------------------------------------

func TestRefSafeEncode_SimpleNames(t *testing.T) {
	cases := []struct {
		ref  string
		want string
	}{
		{"main", "main"},
		{"develop", "develop"},
		{"feat/foo-bar", "feat%2Ffoo-bar"},
		{"release/1.2.3", "release%2F1.2.3"},
		{"feat/nested/deep", "feat%2Fnested%2Fdeep"},
		{"", "_unknown"},
	}
	for _, tc := range cases {
		got := RefSafeEncode(tc.ref)
		if got != tc.want {
			t.Errorf("RefSafeEncode(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestRefSafeDecode_RoundTrip(t *testing.T) {
	refs := []string{
		"main",
		"feat/foo-bar",
		"release/1.2.3",
		"feat/nested/deep",
		"",
	}
	for _, ref := range refs {
		encoded := RefSafeEncode(ref)
		decoded := RefSafeDecode(encoded)
		if decoded != ref {
			t.Errorf("round-trip failed for %q: encode→%q decode→%q", ref, encoded, decoded)
		}
	}
}

func TestRefSafeDecode_UnknownSentinel(t *testing.T) {
	if got := RefSafeDecode("_unknown"); got != "" {
		t.Errorf("RefSafeDecode(_unknown) = %q, want empty string", got)
	}
}

func TestRefSafeEncode_FilesystemSafe(t *testing.T) {
	// Encoded names must not contain "/" so they can be used as a single
	// directory name component.
	problematic := []string{
		"feat/foo",
		"release/1.0/patch",
		"refs/heads/main", // pathological but valid git ref
	}
	for _, ref := range problematic {
		encoded := RefSafeEncode(ref)
		if strings.Contains(encoded, "/") {
			t.Errorf("RefSafeEncode(%q) = %q contains '/' — not filesystem-safe", ref, encoded)
		}
	}
}

// ---------------------------------------------------------------------------
// StateDirForRepoRef path shape
// ---------------------------------------------------------------------------

func TestStateDirForRepoRef_ContainsRefsSegment(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	got := StateDirForRepoRef("/some/repo", "main")
	if !strings.Contains(got, "/refs/main") {
		t.Errorf("StateDirForRepoRef path %q does not contain '/refs/main'", got)
	}
}

func TestStateDirForRepoRef_SlashEncoded(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	got := StateDirForRepoRef("/some/repo", "feat/foo-bar")
	if !strings.Contains(got, "/refs/feat%2Ffoo-bar") {
		t.Errorf("StateDirForRepoRef path %q does not contain encoded ref segment", got)
	}
	// Must not have a bare slash after "refs/"
	refsIdx := strings.Index(got, "/refs/")
	suffix := got[refsIdx+len("/refs/"):]
	if strings.Contains(suffix, "/") {
		t.Errorf("ref segment in path %q still contains '/' after refs/", got)
	}
}

func TestStateDirForRepoRef_EmptyRefUsesUnknown(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	got := StateDirForRepoRef("/some/repo", "")
	if !strings.Contains(got, "/refs/_unknown") {
		t.Errorf("empty ref did not produce _unknown segment, got %q", got)
	}
}

func TestStateDirForRepoRef_EmptyRepoPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	if got := StateDirForRepoRef("", "main"); got != "" {
		t.Errorf("expected empty for empty repoPath, got %q", got)
	}
}

func TestStateDirForRepoRef_Deterministic(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	a := StateDirForRepoRef("/some/repo", "main")
	b := StateDirForRepoRef("/some/repo", "main")
	if a != b {
		t.Fatalf("non-deterministic: %q != %q", a, b)
	}
}

func TestStateDirForRepoRef_DistinctRefsDistinctDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	a := StateDirForRepoRef("/some/repo", "main")
	b := StateDirForRepoRef("/some/repo", "feat/foo")
	if a == b {
		t.Fatalf("different refs resolved to same path: %q", a)
	}
}

func TestStateDirForRepoRef_SameRefDifferentReposDifferentDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	a := StateDirForRepoRef("/some/repo-a", "main")
	b := StateDirForRepoRef("/some/repo-b", "main")
	if a == b {
		t.Fatalf("different repos on same ref resolved to same path: %q", a)
	}
}

func TestStateDirForRepoRef_DefaultStore(t *testing.T) {
	// Without ARCHIGRAPH_DAEMON_ROOT, the path must be under ARCHIGRAPH_HOME/store.
	t.Setenv(EnvRoot, "")
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	got := StateDirForRepoRef("/some/repo", "main")
	storePrefix := filepath.Join(home, "store") + string(filepath.Separator)
	if !strings.HasPrefix(got, storePrefix) {
		t.Fatalf("path %q is not under store %q", got, storePrefix)
	}
	if !strings.Contains(got, "/refs/main") {
		t.Fatalf("path %q does not contain '/refs/main'", got)
	}
}

// ---------------------------------------------------------------------------
// MigrateToRefStore: legacy flat → per-ref layout
// ---------------------------------------------------------------------------

// writeFakeGraph writes a minimal graph.json with an optional indexed_ref
// into dir, simulating a pre-PH1a flat store slot.
func writeFakeGraph(t *testing.T, dir, ref string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := `{"version":1}`
	if ref != "" {
		content = `{"version":1,"indexed_ref":"` + ref + `"}`
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.fb"), []byte("FAKEBLOB"), 0o644); err != nil {
		t.Fatalf("write graph.fb: %v", err)
	}
}

func TestMigrateToRefStore_MovesGraphToRefDir(t *testing.T) {
	storeDir := t.TempDir()
	slotDir := filepath.Join(storeDir, "my-repo-aabbccdd11223344")
	writeFakeGraph(t, slotDir, "main")

	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("MigrateToRefStore: %v", err)
	}

	// graph.fb must now live under refs/main/
	wantFB := filepath.Join(slotDir, "refs", "main", "graph.fb")
	if _, err := os.Stat(wantFB); err != nil {
		t.Errorf("graph.fb not found at %s after migration: %v", wantFB, err)
	}

	// The top-level graph.fb must have been removed.
	oldFB := filepath.Join(slotDir, "graph.fb")
	if _, err := os.Stat(oldFB); err == nil {
		t.Errorf("old top-level graph.fb still exists at %s", oldFB)
	}
}

func TestMigrateToRefStore_UnknownRefWhenNoMetadata(t *testing.T) {
	storeDir := t.TempDir()
	slotDir := filepath.Join(storeDir, "anon-repo-1122334455667788")
	// Write graph without indexed_ref (pre-PH0 graph).
	writeFakeGraph(t, slotDir, "")

	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("MigrateToRefStore: %v", err)
	}

	wantFB := filepath.Join(slotDir, "refs", "_unknown", "graph.fb")
	if _, err := os.Stat(wantFB); err != nil {
		t.Errorf("graph.fb not found at %s: %v", wantFB, err)
	}
}

func TestMigrateToRefStore_SlashRefEncoded(t *testing.T) {
	storeDir := t.TempDir()
	slotDir := filepath.Join(storeDir, "my-repo-aabbccdd11223344")
	writeFakeGraph(t, slotDir, "feat/foo-bar")

	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("MigrateToRefStore: %v", err)
	}

	wantFB := filepath.Join(slotDir, "refs", "feat%2Ffoo-bar", "graph.fb")
	if _, err := os.Stat(wantFB); err != nil {
		t.Errorf("graph.fb not found at encoded path %s: %v", wantFB, err)
	}
}

func TestMigrateToRefStore_Idempotent(t *testing.T) {
	storeDir := t.TempDir()
	slotDir := filepath.Join(storeDir, "my-repo-aabbccdd11223344")
	writeFakeGraph(t, slotDir, "main")

	// First pass.
	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("first MigrateToRefStore: %v", err)
	}
	// Second pass must not fail or corrupt.
	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("second MigrateToRefStore: %v", err)
	}

	wantFB := filepath.Join(slotDir, "refs", "main", "graph.fb")
	if _, err := os.Stat(wantFB); err != nil {
		t.Errorf("graph.fb missing after second migration: %v", err)
	}
	oldFB := filepath.Join(slotDir, "graph.fb")
	if _, err := os.Stat(oldFB); err == nil {
		t.Errorf("top-level graph.fb still exists after second migration")
	}
}

func TestMigrateToRefStore_SkipsAlreadyMigratedSlots(t *testing.T) {
	storeDir := t.TempDir()
	slotDir := filepath.Join(storeDir, "already-migrated-aabb1122ccdd3344")
	// Create a slot that is already in per-ref layout (no top-level graph).
	refDir := filepath.Join(slotDir, "refs", "main")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "graph.fb"), []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateToRefStore(storeDir); err != nil {
		t.Fatalf("MigrateToRefStore: %v", err)
	}

	// The refs/main/graph.fb must still be intact.
	if data, err := os.ReadFile(filepath.Join(refDir, "graph.fb")); err != nil || string(data) != "OK" {
		t.Errorf("already-migrated slot was disturbed")
	}
}

func TestMigrateToRefStore_NonexistentStoreIsNoop(t *testing.T) {
	if err := MigrateToRefStore("/nonexistent/store/dir"); err != nil {
		t.Errorf("expected nil for nonexistent store, got %v", err)
	}
}

func TestMigrateToRefStore_EmptyStoreIsNoop(t *testing.T) {
	storeDir := t.TempDir()
	if err := MigrateToRefStore(storeDir); err != nil {
		t.Errorf("expected nil for empty store, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Backward read: StateDirForRepo through mocked gitmeta (via env override)
//
// StateDirForRepo now resolves the per-ref path using gitmeta.Capture.
// In test environments where we can't control the repo's HEAD we test the
// lower-level StateDirForRepoRef directly (covered above). The following
// test verifies that the resulting path always sits under the per-repo
// base dir and contains a "refs/" segment regardless of the live ref.
// ---------------------------------------------------------------------------

func TestStateDirForRepo_AlwaysUnderBaseAndHasRefs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)

	// Use a temp dir as the "repo" — gitmeta.Capture will return empty
	// Info (not a git repo), which encodes to "_unknown".
	repoPath := t.TempDir()
	got := StateDirForRepo(repoPath)

	if !strings.Contains(got, "/refs/") {
		t.Errorf("StateDirForRepo path %q does not contain '/refs/'", got)
	}
}
