package daemon

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// #1626: when no isolated ARCHIGRAPH_DAEMON_ROOT is set, the state
// directory now lives in the EXTERNAL store under ARCHIGRAPH_HOME/store,
// NOT inside the repo working tree. This is the change that keeps repos
// clean and breaks the fb-vs-json reindex loop.
//
// PH1a (#2089): StateDirForRepo now returns the per-ref sub-directory
// (<store>/<slug>-<hash>/refs/<ref>/). The base slug-hash segment is
// tested via StateDirForRepoRef which is the primitive; StateDirForRepo
// is tested to be under the store and not in the repo.
func TestStateDirForRepo_DefaultStore(t *testing.T) {
	t.Setenv(EnvRoot, "")
	home := t.TempDir()
	t.Setenv("ARCHIGRAPH_HOME", home)

	got := StateDirForRepo("/some/repo")

	storePrefix := filepath.Join(home, "store") + string(filepath.Separator)
	if !strings.HasPrefix(got, storePrefix) {
		t.Fatalf("default state dir not under store: got %q want prefix %q", got, storePrefix)
	}
	// Must NOT be inside the repo.
	if strings.HasPrefix(got, "/some/repo") {
		t.Fatalf("state dir leaked into repo tree: %q", got)
	}
	// PH1a: path must contain a refs/ segment.
	if !strings.Contains(got, "/refs/") {
		t.Fatalf("PH1a: state dir %q does not contain /refs/ segment", got)
	}
	// Base slug segment (before /refs/) must be "<slug>-<16hex>".
	rel, _ := filepath.Rel(filepath.Join(home, "store"), got)
	parts := strings.SplitN(rel, string(filepath.Separator), 2) // ["<slug>-<hash>", "refs/..."]
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]+-[0-9a-f]{16}$`).MatchString(parts[0]) {
		t.Fatalf("store base segment %q is not <slug>-<hash>", parts[0])
	}
}

func TestLegacyInRepoStateDir(t *testing.T) {
	got := LegacyInRepoStateDir("/some/repo")
	want := filepath.Join("/some/repo", ".archigraph")
	if got != want {
		t.Fatalf("legacy dir: got %q want %q", got, want)
	}
}

func TestStateDirForRepo_WithDaemonRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	got := StateDirForRepo("/some/repo")
	if !strings.HasPrefix(got, filepath.Join(root, "state")+string(filepath.Separator)) {
		t.Fatalf("expected DAEMON_ROOT-rooted path, got %q", got)
	}
	// PH1a: path must contain a refs/ segment.
	if !strings.Contains(got, "/refs/") {
		t.Fatalf("PH1a: path %q does not contain /refs/ segment", got)
	}
	// Base segment after .../state/ must be a 16-hex-char hash.
	rel, _ := filepath.Rel(filepath.Join(root, "state"), got)
	parts := strings.SplitN(rel, string(filepath.Separator), 2) // ["<hash>", "refs/..."]
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(parts[0]) {
		t.Fatalf("base hash segment %q is not 16 hex chars", parts[0])
	}
}

func TestStateDirForRepo_Deterministic(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	a := StateDirForRepo("/some/repo")
	b := StateDirForRepo("/some/repo")
	if a != b {
		t.Fatalf("non-deterministic: %q != %q", a, b)
	}
}

func TestStateDirForRepo_DistinctReposDistinctSegments(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	a := StateDirForRepo("/some/repo-a")
	b := StateDirForRepo("/some/repo-b")
	if a == b {
		t.Fatalf("expected distinct paths for distinct repos; both = %q", a)
	}
}

func TestStateDirForRepo_PathSafe(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	// Even with shell-metachar-laden input the hash segment must be
	// purely [0-9a-f]. Use StateDirForRepoRef to test just the base
	// segment without gitmeta (which won't run on a non-git path).
	got := StateDirForRepoRef("/some path/with spaces & $weird?chars", "main")
	rel, _ := filepath.Rel(filepath.Join(root, "state"), got)
	// The rel now looks like "<hash>/refs/main" — check the whole thing
	// except for the "/" separators which are path components, not
	// embedded in names. Strip the path seps.
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		if strings.ContainsAny(p, ` $?&*'"\`+"`") {
			t.Fatalf("path component %q in %q is not shell/path safe", p, rel)
		}
	}
}

func TestGraphPathForRepo_RoutesThroughStateDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	got := GraphPathForRepo("/some/repo")
	if filepath.Base(got) != "graph.json" {
		t.Fatalf("expected graph.json basename, got %q", got)
	}
	if filepath.Dir(got) != StateDirForRepo("/some/repo") {
		t.Fatalf("graph path %q not under StateDirForRepo", got)
	}
}

func TestStateDirForRepo_EmptyInput(t *testing.T) {
	if got := StateDirForRepo(""); got != "" {
		t.Fatalf("expected empty for empty input, got %q", got)
	}
}

// TestStateDirForRepo_TwoDaemonRootsSameRepoIsolated is the regression
// test for issue #745: two daemons with different ARCHIGRAPH_DAEMON_ROOTs
// indexing the same fixture path must resolve to DIFFERENT state
// directories (so they cannot race) while sharing the SAME hash segment
// (so the mapping is deterministic per repo).
func TestStateDirForRepo_TwoDaemonRootsSameRepoIsolated(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	const repo = "/shared/fixture-X"

	t.Setenv(EnvRoot, rootA)
	a := StateDirForRepo(repo)
	t.Setenv(EnvRoot, rootB)
	b := StateDirForRepo(repo)

	if a == b {
		t.Fatalf("daemon A and B resolved to the same state dir: %q", a)
	}
	if !strings.HasPrefix(a, rootA) {
		t.Fatalf("daemon A state dir %q not under root A %q", a, rootA)
	}
	if !strings.HasPrefix(b, rootB) {
		t.Fatalf("daemon B state dir %q not under root B %q", b, rootB)
	}
	// Same repo path → same hash segment under each root.
	if filepath.Base(a) != filepath.Base(b) {
		t.Fatalf("hash segments differ: %q vs %q (should match for same repo)",
			filepath.Base(a), filepath.Base(b))
	}
}

func TestFindGraphFile_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvRoot, tmpDir)

	path, modtime := FindGraphFile("/nonexistent/repo")
	if path != "" {
		t.Fatalf("expected empty path when neither graph file exists, got %q", path)
	}
	if modtime != 0 {
		t.Fatalf("expected modtime 0 when neither file exists, got %d", modtime)
	}
}

func TestFindGraphFile_OnlyJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvRoot, tmpDir)
	repo := t.TempDir()

	stateDir := StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	jsonPath := filepath.Join(stateDir, "graph.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	path, modtime := FindGraphFile(repo)
	if path != jsonPath {
		t.Fatalf("expected json path %q, got %q", jsonPath, path)
	}
	if modtime == 0 {
		t.Fatal("expected non-zero modtime for json file")
	}
}

func TestFindGraphFile_OnlyFB(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvRoot, tmpDir)
	repo := t.TempDir()

	stateDir := StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fbPath := filepath.Join(stateDir, "graph.fb")
	if err := os.WriteFile(fbPath, []byte("fb"), 0644); err != nil {
		t.Fatalf("write fb: %v", err)
	}

	path, modtime := FindGraphFile(repo)
	if path != fbPath {
		t.Fatalf("expected fb path %q, got %q", fbPath, path)
	}
	if modtime == 0 {
		t.Fatal("expected non-zero modtime for fb file")
	}
}

func TestFindGraphFile_BothFiles_FBNewer(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvRoot, tmpDir)
	repo := t.TempDir()

	stateDir := StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	jsonPath := filepath.Join(stateDir, "graph.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	// Write json first, then fb with a newer mtime
	time.Sleep(10 * time.Millisecond)

	fbPath := filepath.Join(stateDir, "graph.fb")
	if err := os.WriteFile(fbPath, []byte("fb"), 0644); err != nil {
		t.Fatalf("write fb: %v", err)
	}

	path, modtime := FindGraphFile(repo)
	if path != fbPath {
		t.Fatalf("expected fb path %q when both exist, got %q", fbPath, path)
	}
	if modtime == 0 {
		t.Fatal("expected non-zero modtime for fb file")
	}
}

func TestFindGraphFile_BothFiles_JSONNewer(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvRoot, tmpDir)
	repo := t.TempDir()

	stateDir := StateDirForRepo(repo)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fbPath := filepath.Join(stateDir, "graph.fb")
	if err := os.WriteFile(fbPath, []byte("fb"), 0644); err != nil {
		t.Fatalf("write fb: %v", err)
	}

	// Write fb first, then json with a newer mtime
	time.Sleep(10 * time.Millisecond)

	jsonPath := filepath.Join(stateDir, "graph.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	path, modtime := FindGraphFile(repo)
	if path != jsonPath {
		t.Fatalf("expected json path %q when json is newer, got %q", jsonPath, path)
	}
	if modtime == 0 {
		t.Fatal("expected non-zero modtime for json file")
	}
}
