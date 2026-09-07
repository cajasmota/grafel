package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #6985 — the oversized-file report and its end-of-index total, observed on
// the PRODUCTION index path rather than in a classifier unit test.
//
// This test exists because the summary call is one line of wiring in
// Indexer.Run: deleting it left every classifier-package test green, so
// nothing graded whether a user actually gets the total. The named lines are
// asserted here too, for the same reason — they are emitted from inside the
// worker pool, which no unit test exercises.
//
// NOTE: this test swaps the process-global os.Stderr handle and therefore MUST
// NOT call t.Parallel(), for the reason spelled out on
// TestExternalSynthesis_VerboseCounter.
func TestOversizedFilesAreNamedAndTotalledByTheIndexer_6985(t *testing.T) {
	repo := t.TempDir()

	// One indexable file so the run has real work, plus enough oversized ones
	// to overflow the report's cap.
	if err := os.WriteFile(filepath.Join(repo, "app.js"),
		[]byte("export function handler(req, res) { return res.send('ok'); }\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	const oversizedCount = 9
	padding := strings.Repeat("x", 1<<20) // one byte of prelude puts it over 1 MiB
	for i := range oversizedCount {
		name := fmt.Sprintf("bundle_%d.js", i)
		body := fmt.Sprintf("// %s\n/*%s*/\n", name, padding)
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if st, err := os.Stat(filepath.Join(repo, name)); err != nil || st.Size() <= 1<<20 {
			t.Fatalf("fixture degenerate: %s is %v bytes (err %v), want > 1 MiB", name, st.Size(), err)
		}
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf []byte
		tmp := make([]byte, 4096)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()

	idx := newTestIndexer(t, "oversized_repo", nil, "")
	_, runErr := idx.Run(context.Background(), repo)

	os.Stderr = origStderr
	w.Close()
	got := <-done
	if runErr != nil {
		t.Fatalf("Run: %v\nstderr:\n%s", runErr, got)
	}

	// Exactly the cap's worth of files is named…
	named := 0
	for i := range oversizedCount {
		if strings.Contains(got, fmt.Sprintf("bundle_%d.js is ", i)) {
			named++
		}
	}
	if named != 8 {
		t.Errorf("indexer named %d oversized files, want 8 (the cap); stderr:\n%s", named, got)
	}
	if !strings.Contains(got, "was NOT indexed") {
		t.Errorf("no oversized-skip line on the production path; stderr:\n%s", got)
	}
	// …and the end-of-index line says how many it did not name. Without the
	// ReportOversizedSummary call in Run this is the assertion that fails.
	if !strings.Contains(got, "1 further oversized-file skips were not named") {
		t.Errorf("no end-of-index total for the suppressed oversized files; stderr:\n%s", got)
	}
	if !strings.Contains(got, "9 over the 1048576-byte limit in total") {
		t.Errorf("the total does not say how many files were over the limit; stderr:\n%s", got)
	}
}
