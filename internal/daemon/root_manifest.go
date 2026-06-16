// root_manifest.go — per-root source-path manifest (issue #5267, epic #5234).
//
// # Why this exists
//
// The store-root ↔ source-path mapping is ONE-WAY: a root is
// `<store>/<slug>-<hash>` where hash = sha256(canonical(absPath))[:16] (see
// state_path.go), and roots historically did NOT self-record their source path
// on disk — graph.json carries only a human label + is_worktree, not the
// absolute path. So the orphan-root GC (#5263) could only attribute a root in
// the FORWARD direction (enumerate every KNOWN live source path, hash it, match
// the root) and KEPT everything else fail-closed.
//
// On the live 12.1GB / 355-root store that meant `store gc --dry-run` reclaimed
// 0 bytes: 307 roots were `<undeterminable> (gone) → fail-closed KEEP` because
// no currently-known repo/worktree hashed to them — their source paths had been
// removed from the registry / deleted from disk and there was nothing left to
// hash forward.
//
// # What this adds
//
// At every index write we now persist a tiny `<root>/root.json` manifest that
// records the CANONICAL ABSOLUTE source path (the exact string fed to the hash).
// orphanroot.Attribute() prefers this recorded path when present: read it →
// stat it → if gone + not-live + outside grace → ORPHAN, attributable WITHOUT
// the forward map. When the field is absent (legacy roots written before this
// change) the sweeper falls back to the existing forward-map + fail-closed
// behaviour, so legacy roots are never broken.
//
// The manifest is intentionally minimal and written best-effort (a failure to
// write it never fails an index pass — attribution simply falls back to the
// forward map for that root).
package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RootManifestName is the basename of the per-root source-path manifest written
// at the TOP LEVEL of a store root (a sibling of refs/), so the orphan-root GC
// can read it with a single stat+read while enumerating StoreRootBase.
const RootManifestName = "root.json"

// RootManifest is the on-disk `<root>/root.json` payload. It is deliberately
// tiny — its sole job is to make a root self-attributing so the GC need not
// hash a still-known source path forward to identify it.
type RootManifest struct {
	// Version is the manifest schema version (currently 1). Reserved for
	// forward-compatible field additions.
	Version int `json:"version"`
	// SourcePath is the CANONICAL ABSOLUTE source path of the repo/worktree
	// this root was created for — the exact string fed to repoStateHash (i.e.
	// canonicalizePath(filepath.Clean(filepath.Abs(repoPath)))). The GC stats
	// this directly: if it is gone (and not otherwise live / within grace) the
	// root is reapable without any forward map.
	SourcePath string `json:"source_path"`
}

// canonicalSourcePath returns the canonical absolute form of repoPath — the
// SAME transformation repoStateHash applies before hashing. Recording this
// exact string in the manifest guarantees a later stat of it round-trips to the
// path whose hash named the root.
func canonicalSourcePath(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	return canonicalizePath(filepath.Clean(abs))
}

// WriteRootManifest writes (or refreshes) `<root>/root.json` for repoPath,
// recording its canonical absolute source path so the orphan-root GC can
// attribute the root WITHOUT the forward map (#5267). It is best-effort: the
// returned error is for logging only — callers MUST NOT fail an index pass on
// it, since attribution degrades gracefully to the forward-map fallback.
//
// The write is atomic (tmp file + rename) so a concurrent reader never observes
// a torn manifest.
func WriteRootManifest(repoPath string) error {
	if repoPath == "" {
		return nil
	}
	root := RepoBaseDir(repoPath)
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	m := RootManifest{Version: 1, SourcePath: canonicalSourcePath(repoPath)}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	dst := filepath.Join(root, RootManifestName)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadRootManifest reads `<root>/root.json` for the given top-level store-root
// directory. It returns (manifest, true, nil) when the manifest exists and
// parses; (zero, false, nil) when the manifest is simply absent (a legacy root);
// and (zero, false, err) on a read/parse error other than not-exist (so the
// caller can fall back conservatively).
func ReadRootManifest(rootDir string) (RootManifest, bool, error) {
	var m RootManifest
	data, err := os.ReadFile(filepath.Join(rootDir, RootManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return m, false, nil
		}
		return m, false, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, false, err
	}
	return m, true, nil
}
