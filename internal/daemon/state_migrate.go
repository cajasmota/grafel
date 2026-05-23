package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// graphArtifactNames are the generated files the daemon owns and that
// must live in the external store rather than inside the repo working
// tree (issue #1626). The committed teammate manifest `group.json` is
// deliberately NOT in this list — it stays co-located in
// `<repo>/.archigraph/group.json` so it can be discovered by walking up
// from a CWD and is meant to be checked into the repo.
var graphArtifactNames = []string{
	"graph.fb",
	"graph.json",
	"graph-stats.json",
	".metadata.json",
	"file-index.json",
	"repair.json",
	"enrichment-candidates.json",
	"enrichment-resolutions.json",
	"enrichment-rejections.json",
	"fitness.yaml",
}

// graphArtifactDirs are subdirectories of generated state that should
// move into the store wholesale when present.
var graphArtifactDirs = []string{
	"enrichments",
	"links",
	"logs",
}

// MigrateInRepoState relocates any pre-#1626 generated graph artifacts
// found in the legacy `<repo>/.archigraph/` directory into the external
// store (StateDirForRepo). It is a no-op when:
//   - an isolated ARCHIGRAPH_DAEMON_ROOT is in effect (the legacy
//     co-located dir was never the source of truth in that mode), or
//   - the legacy directory does not exist, or
//   - the store already holds a graph (already migrated / freshly indexed).
//
// The committed `group.json` manifest is left in place. After a
// successful migration the legacy directory is removed if nothing but
// `group.json` (or nothing at all) remains, so the repo working tree
// ends up clean.
//
// Migration is move-based (rename, falling back to copy+remove across
// filesystems) and best-effort: any per-file failure is returned so the
// caller can log it, but partial success still leaves a usable store.
func MigrateInRepoState(repoPath string) (migrated bool, err error) {
	if repoPath == "" {
		return false, nil
	}
	// Isolated-daemon mode never used the in-repo dir as truth.
	if os.Getenv(EnvRoot) != "" {
		return false, nil
	}

	legacy := LegacyInRepoStateDir(repoPath)
	if fi, statErr := os.Stat(legacy); statErr != nil || !fi.IsDir() {
		return false, nil
	}

	store := StateDirForRepo(repoPath)
	// If the store already has a graph, treat as already-migrated and
	// leave the legacy dir alone (it may hold only group.json).
	if _, mt := FindGraphFile(repoPath); mt != 0 {
		return false, nil
	}

	// Only migrate if the legacy dir actually holds a generated graph;
	// otherwise there's nothing to relocate (e.g. only group.json).
	if !legacyHasGraph(legacy) {
		return false, nil
	}

	if mkErr := os.MkdirAll(store, 0o755); mkErr != nil {
		return false, fmt.Errorf("migrate: mkdir store %s: %w", store, mkErr)
	}

	var firstErr error
	moveOne := func(name string) {
		src := filepath.Join(legacy, name)
		if _, e := os.Stat(src); e != nil {
			return
		}
		dst := filepath.Join(store, name)
		if e := movePath(src, dst); e != nil && firstErr == nil {
			firstErr = e
		} else if e == nil {
			migrated = true
		}
	}
	for _, name := range graphArtifactNames {
		moveOne(name)
	}
	for _, name := range graphArtifactDirs {
		moveOne(name)
	}

	// Clean up the legacy dir if only group.json (or nothing) remains so
	// the repo working tree is left pristine.
	cleanupLegacyDir(legacy)

	return migrated, firstErr
}

// legacyHasGraph reports whether the legacy dir contains a generated
// graph file (graph.fb or graph.json).
func legacyHasGraph(legacy string) bool {
	for _, n := range []string{"graph.fb", "graph.json"} {
		if _, err := os.Stat(filepath.Join(legacy, n)); err == nil {
			return true
		}
	}
	return false
}

// cleanupLegacyDir removes the legacy `.archigraph` dir when it is empty
// or holds only the committed group.json manifest (which is kept).
func cleanupLegacyDir(legacy string) {
	entries, err := os.ReadDir(legacy)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == "group.json" {
			continue
		}
		// Something else remains (unknown file/dir) — leave it untouched.
		return
	}
	if len(entries) == 0 {
		_ = os.Remove(legacy)
	}
	// If only group.json remains, intentionally keep the dir + manifest.
}

// movePath moves src to dst, preferring an atomic rename and falling
// back to recursive copy+remove when src and dst are on different
// filesystems (rename returns EXDEV).
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyPath(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
