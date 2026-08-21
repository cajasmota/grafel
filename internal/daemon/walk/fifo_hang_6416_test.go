//go:build unix

package walk

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/gitmeta"
)

// TestWalkRepoFifoDoesNotBlockDownstreamRead is a LIVENESS test for #6416.
//
// WalkRepo branched only on d.IsDir(), so anything that is not a directory was
// returned as a candidate file. A FIFO named `Hang.vb` is a perfectly good
// directory entry, and os.ReadFile on one blocks in open(2) until a writer
// appears — forever, if none ever does. The indexing worker that picks the path
// up never finishes: no timeout, no error, no log line.
//
// The test therefore does what the indexer does — walk, then READ every path
// the walk handed back. Asserting only "the fifo is absent from files" would go
// green against a walker that still hands the path over, because WalkRepo
// itself never opens anything; the hang lives one step downstream. Reverting
// the fix must make this test TIME OUT, which is the only way it pins the bug.
//
// The walk+read runs on its own goroutine against a deadline so a regression
// FAILS the suite instead of hanging it. A hung goroutine is leaked
// deliberately: there is no way to interrupt a blocking open(2).
func TestWalkRepoFifoDoesNotBlockDownstreamRead(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"a bare fifo", "fifo"},
		{"a symlink to a fifo", "symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "Real.vb"), []byte("Class Real\nEnd Class\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			fifo := filepath.Join(root, "Hang.vb")
			if tc.kind == "symlink" {
				fifo = filepath.Join(root, "real_fifo")
			}
			if err := syscall.Mkfifo(fifo, 0o644); err != nil {
				t.Skipf("cannot create a fifo here: %v", err)
			}
			if tc.kind == "symlink" {
				if err := os.Symlink(fifo, filepath.Join(root, "Hang.vb")); err != nil {
					t.Skipf("symlinks unavailable here: %v", err)
				}
			}

			type result struct {
				files   []string
				skipped []SkipEntry
			}
			done := make(chan result, 1)
			var buf strings.Builder
			go func() {
				files, skipped, err := WalkRepo(root, &Options{PrintSkipped: &buf})
				if err != nil {
					t.Errorf("WalkRepo: %v", err)
				}
				// This is the indexer's next move, and the step that hangs.
				for _, f := range files {
					_, _ = os.ReadFile(filepath.Join(root, f))
				}
				done <- result{files: files, skipped: skipped}
			}()

			var got result
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("walk+read blocked on a fifo named Hang.vb; the walker must skip anything that is not a regular file")
			}

			for _, f := range got.files {
				if f == "Hang.vb" {
					t.Errorf("walker returned the fifo %q as a candidate file", f)
				}
			}
			if len(got.files) != 1 || got.files[0] != "Real.vb" {
				t.Errorf("legitimate files lost: got %v, want [Real.vb]", got.files)
			}

			var reported bool
			for _, s := range got.skipped {
				if strings.HasSuffix(s.AbsPath, "Hang.vb") && strings.HasPrefix(s.Rule, "irregular:") {
					reported = true
				}
			}
			if !reported {
				t.Errorf("the fifo was skipped SILENTLY; want a SkipEntry with an irregular: rule, got %+v", got.skipped)
			}
			if !strings.Contains(buf.String(), "Hang.vb") {
				t.Errorf("PrintSkipped never mentioned the fifo; got %q", buf.String())
			}
		})
	}
}

// TestWalkRepoSymlinkToRegularFileIsStillIndexed guards the other side of the
// symlink decision: rejecting every symlink would fix the hang by losing
// legitimate coverage. filepath.WalkDir reports a symlink-to-file as a symlink
// entry, and the indexer does mint a file entity for it, so it must survive.
func TestWalkRepoSymlinkToRegularFileIsStillIndexed(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Target.vb")
	if err := os.WriteFile(target, []byte("Class Target\nEnd Class\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "Link.vb")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	files, _, err := WalkRepo(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sawLink bool
	for _, f := range files {
		if f == "Link.vb" {
			sawLink = true
		}
	}
	if !sawLink {
		t.Errorf("symlink to a regular file was dropped from the walk: got %v", files)
	}
}

// TestIrregularGateRunsBeforeTheSparseFilter pins the gate's PLACEMENT, which
// the review found was argued for in prose and asserted nowhere: moving the
// block below the sparse filter passed the whole package.
//
// It has to be before. A FIFO outside the sparse pattern set is still present
// on disk and is still the thing wedging a read, so it must be reported as a
// hazard rather than absorbed into the sparse path's deliberate silence — that
// path drops files with no error precisely because a sparse checkout is
// EXPECTED to be missing them, which is the wrong story for a planted FIFO.
func TestIrregularGateRunsBeforeTheSparseFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Real.vb"), []byte("Class Real\nEnd Class\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "Hang.vb"), 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	// A sparse pattern set that covers Real.vb and NOT Hang.vb: without the
	// ordering, the sparse filter would return nil first and the fifo would
	// vanish unreported.
	sparse := &gitmeta.SparseInfo{IsSparse: true, Patterns: []string{"/Real.vb"}}

	files, skipped, err := WalkRepo(root, &Options{Sparse: sparse})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "Hang.vb" {
			t.Fatalf("fifo returned as a candidate: %v", files)
		}
	}
	var reported bool
	for _, s := range skipped {
		if strings.HasSuffix(s.AbsPath, "Hang.vb") && strings.HasPrefix(s.Rule, "irregular:") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("the fifo was absorbed by the sparse filter and reported nowhere; skipped=%+v", skipped)
	}
}
