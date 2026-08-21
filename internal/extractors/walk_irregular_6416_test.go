//go:build unix

package extractors

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestWalkSourceFilesReportsIrregularSkips closes the review's MEDIUM-3 gap:
// the DAEMON path used to drop the walker's skip report on the floor.
//
// walkSourceFiles is what a watcher-triggered reindex uses, and it reached the
// same walker as `grafel index` while discarding `skipped` — so on the path
// most users actually hit, a FIFO vanished from the index with no stderr line,
// no doctor entry, nowhere. That is the exact failure #6338 exists to prevent,
// and it made the foreground report a half-measure.
func TestWalkSourceFilesReportsIrregularSkips(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(repo, "hang.go"), 0o644); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	type res struct {
		files  []string
		report string
		err    error
	}
	ch := make(chan res, 1)
	go func() {
		f, rep, err := walkSourceFiles(repo)
		// The daemon reads every file this returns; do what it does, so a
		// regression times out here rather than in production.
		for _, p := range f {
			_, _ = os.ReadFile(filepath.Join(repo, p))
		}
		ch <- res{f, rep, err}
	}()

	var got res
	select {
	case got = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("walkSourceFiles + read blocked on a fifo; the daemon path is exposed to #6416 too")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, f := range got.files {
		if f == "hang.go" {
			t.Errorf("fifo returned as a source file: %v", got.files)
		}
	}
	if !strings.Contains(got.report, "hang.go") || !strings.Contains(got.report, "non-regular file") {
		t.Errorf("daemon path reported nothing for the skipped fifo; report=%q", got.report)
	}
}

// TestWalkSourceFilesReportIsEmptyForACleanRepo keeps the daemon log quiet in
// the ordinary case: a report that fires on every poll is a report nobody
// reads.
func TestWalkSourceFilesReportIsEmptyForACleanRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "real.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, report, err := walkSourceFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report != "" {
		t.Errorf("clean repo produced a report: %q", report)
	}
}
