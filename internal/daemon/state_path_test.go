package daemon

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStateDirForRepo_DefaultColocated(t *testing.T) {
	t.Setenv(EnvRoot, "")
	got := StateDirForRepo("/some/repo")
	want := filepath.Join("/some/repo", ".archigraph")
	if got != want {
		t.Fatalf("default state dir: got %q want %q", got, want)
	}
}

func TestStateDirForRepo_WithDaemonRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	got := StateDirForRepo("/some/repo")
	if !strings.HasPrefix(got, filepath.Join(root, "state")+string(filepath.Separator)) {
		t.Fatalf("expected DAEMON_ROOT-rooted path, got %q", got)
	}
	// Segment after .../state/ must be a 16-hex-char hash.
	rel, _ := filepath.Rel(filepath.Join(root, "state"), got)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(rel) {
		t.Fatalf("segment %q is not 16 hex chars", rel)
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
	// purely [0-9a-f].
	got := StateDirForRepo("/some path/with spaces & $weird?chars")
	rel, _ := filepath.Rel(filepath.Join(root, "state"), got)
	if strings.ContainsAny(rel, ` /$?&*'"\`+"`") {
		t.Fatalf("segment %q is not shell/path safe", rel)
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
