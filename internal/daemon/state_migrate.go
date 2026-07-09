package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// graphArtifactNames are the generated files the daemon owns and that
// must live in the external store rather than inside the repo working
// tree (issue #1626). The committed teammate manifest `group.json` is
// deliberately NOT in this list — it stays co-located in
// `<repo>/.grafel/group.json` so it can be discovered by walking up
// from a CWD and is meant to be checked into the repo.
var graphArtifactNames = []string{
	"graph.fb",
	"graph.json",
	"graph-stats.json",
	"link-stats.json",
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
// found in the legacy `<repo>/.grafel/` directory into the external
// store (StateDirForRepo). It is a no-op when:
//   - an isolated GRAFEL_DAEMON_ROOT is in effect (the legacy
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

// cleanupLegacyDir removes the legacy `.grafel` dir when it is empty
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

// graphMetadataRef is the minimal subset of the graph metadata file we
// need for migration — just the indexed_ref field. We read .metadata.json
// or graph.json (whichever exists) to discover the ref so we can route
// the legacy flat artifacts into the correct per-ref sub-directory.
type graphMetadataRef struct {
	IndexedRef string `json:"indexed_ref"`
}

// readIndexedRefFromDir attempts to read the indexed_ref from the
// metadata stored inside baseDir. It tries, in order:
//  1. graph.fb (via fbreader — avoids JSON overhead for the common path)
//  2. graph.json (JSON fallback)
//
// Returns "" when no metadata is found or the file predates PH0 (#2088).
func readIndexedRefFromDir(baseDir string) string {
	// Try graph.json — simpler than wiring fbreader here.
	for _, name := range []string{"graph.json"} {
		p := filepath.Join(baseDir, name)
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var m graphMetadataRef
		if err := json.NewDecoder(f).Decode(&m); err == nil && m.IndexedRef != "" {
			f.Close()
			return m.IndexedRef
		}
		f.Close()
	}
	return ""
}

// MigrateToRefStore walks storeDir and moves any legacy flat-layout
// per-repo slot (one that holds graph.fb / graph.json directly, not under
// refs/) into the per-ref sub-directory introduced by PH1a (#2089).
//
// Layout before migration:
//
//	<store>/<slug>-<hash>/graph.fb
//	<store>/<slug>-<hash>/graph.json
//	<store>/<slug>-<hash>/<sidecars>
//
// Layout after migration:
//
//	<store>/<slug>-<hash>/refs/<ref-safe>/graph.fb
//	<store>/<slug>-<hash>/refs/<ref-safe>/graph.json
//	<store>/<slug>-<hash>/refs/<ref-safe>/<sidecars>
//
// The ref-safe name is derived from the indexed_ref stored in the
// metadata; "_unknown" is used when the metadata predates PH0 (#2088)
// or the HEAD was detached.
//
// Migration is idempotent: slots that already have a refs/ sub-directory
// and no top-level graph.fb are skipped.
//
// Intended to be called once from daemon startup (before the first RPC
// is served) so no caller pays the migration cost at query time.
func MigrateToRefStore(storeDir string) error {
	if storeDir == "" {
		return nil
	}
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // store not created yet — nothing to migrate
		}
		return fmt.Errorf("MigrateToRefStore: read store dir %s: %w", storeDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slotDir := filepath.Join(storeDir, e.Name())
		if err := migrateSlot(slotDir); err != nil {
			// Log but continue; partial migration leaves usable state.
			log.Printf("migration: slot %s: %v", slotDir, err)
		}
	}
	return nil
}

// migrateSlot migrates a single per-repo slot from legacy flat layout to
// the per-ref sub-directory layout. It is idempotent.
//
// It also heals the partial-migration state introduced by PR #2126 (#2130):
// when refs/_unknown/graph.fb exists alongside exactly one other
// refs/<X>/ directory (X != "_unknown") that is missing graph.fb, the
// _unknown artifacts are moved into refs/<X>/ so the daemon's read path
// resolves correctly. If multiple non-_unknown refs exist the outcome is
// ambiguous and the slot is left as-is.
func migrateSlot(slotDir string) error {
	// Fast path: slot already has a refs/ sub-directory (new layout).
	refsDir := filepath.Join(slotDir, "refs")
	if fi, err := os.Stat(refsDir); err == nil && fi.IsDir() {
		// Check for the PH1a partial-migration state (#2130):
		// refs/_unknown/graph.fb exists + exactly one other refs/<X>/ missing graph.fb.
		if healed, err := healUnknownRef(refsDir); healed || err != nil {
			return err
		}
		// Might still have a stale flat graph.fb alongside refs/ if a
		// previous partial migration crashed. Clean up the top-level
		// graph files if refs/ already holds something valid.
		if !legacyHasGraph(slotDir) {
			return nil // already clean
		}
		// top-level graph.fb exists alongside refs/ — finish the move.
	} else if !legacyHasGraph(slotDir) {
		return nil // slot has no graph at all — skip
	}

	// Determine the target ref from metadata stored in the slot.
	ref := readIndexedRefFromDir(slotDir)
	refSafe := RefSafeEncode(ref) // "" → "_unknown"

	targetDir := filepath.Join(refsDir, refSafe)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}

	// Move every graph artifact from the slot top-level to targetDir.
	var firstErr error
	moveOne := func(name string) {
		src := filepath.Join(slotDir, name)
		if _, e := os.Stat(src); e != nil {
			return // file absent — skip
		}
		dst := filepath.Join(targetDir, name)
		// Idempotency: if dst already exists, skip — the file was already moved.
		if _, e := os.Stat(dst); e == nil {
			// dst exists: remove src to finish the partial move.
			_ = os.RemoveAll(src)
			return
		}
		if e := movePath(src, dst); e != nil && firstErr == nil {
			firstErr = fmt.Errorf("move %s → %s: %w", src, dst, e)
		}
	}
	for _, name := range graphArtifactNames {
		moveOne(name)
	}
	for _, name := range graphArtifactDirs {
		moveOne(name)
	}

	if firstErr == nil {
		log.Printf("migration: %s → refs/%s", slotDir, refSafe)
	}
	return firstErr
}

// healUnknownRef resolves the partial-migration state described in #2130:
//
//	refs/_unknown/graph.fb  ← stranded from PR #2126
//	refs/<X>/enrichment-candidates.json  ← only sidecar, no graph.fb
//
// Precondition: refsDir exists.
// Returns (true, nil) if it healed the state, (false, nil) if no action was
// needed, or (false, err) on I/O failure.
//
// Safety rules (preserves data):
//   - Only acts when refs/_unknown/ holds a graph file.
//   - Only acts when exactly ONE non-_unknown ref dir exists.
//   - The target ref dir must NOT already have a graph file (no clobber).
//   - Idempotent: repeated calls are no-ops.
func healUnknownRef(refsDir string) (healed bool, err error) {
	unknownDir := filepath.Join(refsDir, "_unknown")
	if !legacyHasGraph(unknownDir) {
		return false, nil // nothing stranded in _unknown — no-op
	}

	// Enumerate sibling ref dirs.
	entries, readErr := os.ReadDir(refsDir)
	if readErr != nil {
		return false, fmt.Errorf("healUnknownRef: read %s: %w", refsDir, readErr)
	}
	var nonUnknown []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_unknown" {
			continue
		}
		nonUnknown = append(nonUnknown, e.Name())
	}

	// Ambiguous if multiple non-_unknown refs exist — leave alone.
	if len(nonUnknown) != 1 {
		return false, nil
	}

	targetDir := filepath.Join(refsDir, nonUnknown[0])
	// Don't clobber a ref dir that already has its own graph.
	if legacyHasGraph(targetDir) {
		return false, nil
	}

	// Move all artifacts from _unknown into the single known ref dir.
	var firstErr error
	moveOne := func(name string) {
		src := filepath.Join(unknownDir, name)
		if _, e := os.Stat(src); e != nil {
			return
		}
		dst := filepath.Join(targetDir, name)
		if _, e := os.Stat(dst); e == nil {
			_ = os.RemoveAll(src) // dst already there — finish partial move
			return
		}
		if e := movePath(src, dst); e != nil && firstErr == nil {
			firstErr = fmt.Errorf("healUnknownRef: move %s → %s: %w", src, dst, e)
		}
	}
	for _, name := range graphArtifactNames {
		moveOne(name)
	}
	for _, name := range graphArtifactDirs {
		moveOne(name)
	}

	if firstErr != nil {
		return false, firstErr
	}

	// Remove the now-empty _unknown dir (best-effort).
	_ = os.Remove(unknownDir)
	log.Printf("migration(#2130 heal): refs/_unknown → refs/%s in %s", nonUnknown[0], refsDir)
	return true, nil
}
