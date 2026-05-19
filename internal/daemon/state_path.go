// Per-repo state path resolution for issue #745.
//
// Background: ADR-0007 co-locates per-repo state in `<repo>/.archigraph/`.
// That default is preserved for ordinary user installs. When multiple
// agents run with isolated daemons via ARCHIGRAPH_DAEMON_ROOT, the
// daemon socket and registry are already isolated, but the per-repo
// state directory is shared — two agents indexing the same fixture
// race on `<repo>/.archigraph/graph.json` and corrupt each other's
// results.
//
// When ARCHIGRAPH_DAEMON_ROOT is set, StateDirForRepo returns a
// daemon-private state directory at
//
//	$ARCHIGRAPH_DAEMON_ROOT/state/<sha256(abs_repo_path)[:16]>/
//
// instead of `<repo>/.archigraph/`. The fixture's own `.archigraph/`
// directory is never touched by the daemon under this mode, so a
// pristine read-only corpus stays pristine even across many parallel
// agents.
//
// Identifier choice: sha256 of the absolute repo path, first 16 hex
// chars. Reasons:
//   - Deterministic (same input → same output across processes & hosts).
//   - Filesystem-safe (16 hex chars, no separators or shell metachars).
//   - Collision-resistant (2^64 namespace; far above any realistic
//     fixture count on a single host).
//   - Opaque (does not leak repo paths into shared tmp).
//
// Group-level config that lives co-located by design (group.json
// manifests written by the wizard) is NOT routed through this helper —
// it stays at `<repo>/.archigraph/group.json` so it can be discovered
// by walking up from a CWD regardless of which daemon is running.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// repoStateHash returns a deterministic, path-safe identifier for a
// repo path. Callers MUST pass an absolute, lexically-clean path
// (filepath.Abs + filepath.Clean) so that the same repo always maps
// to the same hash regardless of how the caller addressed it.
func repoStateHash(absRepoPath string) string {
	sum := sha256.Sum256([]byte(absRepoPath))
	return hex.EncodeToString(sum[:8]) // 16 hex chars
}

// StateDirForRepo returns the directory that holds per-repo state
// (graph.json, repair.json, enrichment-*.json, …) for repoPath.
//
// When ARCHIGRAPH_DAEMON_ROOT is set, the directory is
// `$ARCHIGRAPH_DAEMON_ROOT/state/<hash>/`. Otherwise it is the
// ADR-0007 default of `<repoPath>/.archigraph/`.
//
// The directory is NOT created here; callers that write should
// os.MkdirAll the returned path.
func StateDirForRepo(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	root := os.Getenv(EnvRoot)
	if root == "" {
		return filepath.Join(repoPath, ".archigraph")
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	abs = filepath.Clean(abs)
	return filepath.Join(root, "state", repoStateHash(abs))
}

// GraphPathForRepo is a convenience wrapper that returns the
// canonical graph.json path inside the per-repo state directory.
func GraphPathForRepo(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), "graph.json")
}

// GraphFBPathForRepo returns the path to graph.fb (FlatBuffers binary format)
// inside the per-repo state directory.
func GraphFBPathForRepo(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), "graph.fb")
}

// FindGraphFile checks for the newest graph file (graph.fb preferred over
// graph.json) and returns its path and modification time. Returns ("", 0)
// if neither file exists. The returned modtime is in nanoseconds since epoch.
func FindGraphFile(repoPath string) (path string, modtime int64) {
	stateDir := StateDirForRepo(repoPath)

	fbPath := filepath.Join(stateDir, "graph.fb")
	jsonPath := filepath.Join(stateDir, "graph.json")

	fbInfo, fbErr := os.Stat(fbPath)
	jsonInfo, jsonErr := os.Stat(jsonPath)

	// Check graph.fb first (preferred)
	if fbErr == nil {
		fbMtime := fbInfo.ModTime().UnixNano()
		// If both exist, return whichever is newer
		if jsonErr == nil {
			jsonMtime := jsonInfo.ModTime().UnixNano()
			if fbMtime >= jsonMtime {
				return fbPath, fbMtime
			}
			return jsonPath, jsonMtime
		}
		return fbPath, fbMtime
	}

	// Fall back to graph.json if graph.fb doesn't exist
	if jsonErr == nil {
		return jsonPath, jsonInfo.ModTime().UnixNano()
	}

	return "", 0
}
