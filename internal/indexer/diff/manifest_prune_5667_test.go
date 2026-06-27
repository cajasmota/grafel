package diff_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// TestUpdateManifest_PrunesStaleEntries is a regression test for #5667.
//
// UpdateManifest must reconcile the manifest to the walked set — entries for
// files no longer in the walk (e.g. a file that became gitignored) must be
// dropped. Before the fix UpdateManifest was add-only, so a now-ignored file's
// entry was immortal and was reported as "deleted" on every pass, perpetually
// tripping the too-many-changed full-reindex fallback and pinning the daemon.
func TestUpdateManifest_PrunesStaleEntries(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &diff.Manifest{Version: 1, Files: map[string]diff.FileEntry{
		"real.go":         {},                  // in the walk — kept (re-hashed)
		"ios/Pods/x.lock": {SHA256: "stale-1"}, // now gitignored — must be pruned
		"android/.cxx/y":  {SHA256: "stale-2"}, // now gitignored — must be pruned
	}}

	diff.UpdateManifest(repo, []string{"real.go"}, m)

	if _, ok := m.Files["real.go"]; !ok {
		t.Errorf("walked file real.go must be present")
	}
	for _, stale := range []string{"ios/Pods/x.lock", "android/.cxx/y"} {
		if _, ok := m.Files[stale]; ok {
			t.Errorf("stale entry %q must be pruned — it would loop the reindex (#5667)", stale)
		}
	}
	if len(m.Files) != 1 {
		t.Errorf("manifest must equal the walked set (1 file), got %d entries", len(m.Files))
	}
}
