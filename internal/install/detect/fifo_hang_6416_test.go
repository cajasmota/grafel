//go:build unix

package detect

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestDetectMonorepoDoesNotBlockOnFifoManifest is the second liveness probe for
// #6416, and it is the one the walker fix alone does NOT close.
//
// cmd/grafel's Indexer.Run calls DetectMonorepo immediately after the walk.
// DetectMonorepo os.ReadFile's package.json / lerna.json / pnpm-workspace.yaml
// by NAME — it never goes through the walker — so `mkfifo package.json` at the
// repo root wedged `grafel index` forever even with the walker's entry-type
// gate in place. The review that found this reproduced it on the branch that
// claimed to fix #6416.
//
// Each manifest runs on its own goroutine against a deadline so a regression
// FAILS the suite instead of hanging it; a blocked goroutine is leaked
// deliberately, because there is no way to interrupt a blocking open(2).
func TestDetectMonorepoDoesNotBlockOnFifoManifest(t *testing.T) {
	for _, manifest := range []string{"package.json", "lerna.json", "pnpm-workspace.yaml"} {
		t.Run(manifest, func(t *testing.T) {
			repo := t.TempDir()
			if err := syscall.Mkfifo(filepath.Join(repo, manifest), 0o644); err != nil {
				t.Skipf("cannot create a fifo here: %v", err)
			}
			// lerna/pnpm are only consulted when the marker exists; package.json
			// is read unconditionally. Give the others a reason to be read.
			if manifest != "package.json" {
				if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"workspaces":["pkg/*"]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			done := make(chan struct{})
			go func() {
				_, _ = DetectMonorepo(repo)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("DetectMonorepo blocked on a fifo named %s; manifest reads must not open a non-regular file", manifest)
			}
		})
	}
}

// TestDetectMonorepoStillReadsSymlinkedManifest is the other half of the
// symlink decision: a symlinked package.json is an ordinary monorepo shape, so
// the guard must follow the link rather than reject every symlink.
func TestDetectMonorepoStillReadsSymlinkedManifest(t *testing.T) {
	repo := t.TempDir()
	real := filepath.Join(repo, "real-package.json")
	if err := os.WriteFile(real, []byte(`{"workspaces":["pkg/*"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(repo, "package.json")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "pkg", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pkg", "a", "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mono, err := DetectMonorepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(mono.Packages) == 0 {
		t.Errorf("symlinked package.json was not read: %+v", mono)
	}
}
