//go:build !windows

package engine

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/types"
)

// #6450 review, BLOCKING 2a — the fold re-opens source files BY PATH, after
// the walk's irregular-file filter (internal/daemon/walk/irregular.go) has
// already run and decided what was safe. That makes it a TOCTOU/symlink hole
// unless it uses internal/safeio, which every other by-path reader in this
// tree does (#6416).
//
// With plain os.ReadFile a FIFO named `config.js` parks open(2) waiting for a
// writer that never comes, and the whole index hangs — measured at >5s and
// never returning. safeio stats first, sees a named pipe, and refuses without
// opening it.
//
// Windows has no mkfifo, hence the build tag; the safeio machinery itself is
// covered cross-platform by internal/safeio's own tests.
func TestFoldConsumerHTTPBaseURLs_FIFODoesNotBlock_6450(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "client")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	api := "import { BASE } from './config';\nfetch(`${BASE}/things`);\n"
	if err := os.WriteFile(filepath.Join(dir, "api.js"), []byte(api), 0o644); err != nil {
		t.Fatal(err)
	}
	// The declaring "module" is a named pipe with no writer.
	fifo := filepath.Join(dir, "config.js")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unavailable on this platform/filesystem: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(fifo) })

	recs := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "config", SourceFile: "client/config.js"},
		callRecord("http:GET:/{BASE}/things", "/{BASE}/things", "dynamic_baseurl"),
	}

	type outcome struct {
		res FoldConsumerHTTPBaseURLResult
		dur time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		start := time.Now()
		r := FoldConsumerHTTPBaseURLs(recs, root, nil)
		done <- outcome{res: r, dur: time.Since(start)}
	}()

	select {
	case got := <-done:
		if got.dur > 5*time.Second {
			t.Errorf("fold took %v on a FIFO — safeio should refuse it without "+
				"opening, so this must be near-instant", got.dur)
		}
		if got.res.Candidates != 1 {
			t.Errorf("candidates=%d, want 1", got.res.Candidates)
		}
		if got.res.Folded != 0 {
			t.Errorf("folded=%d, want 0 — a named pipe is not a source file",
				got.res.Folded)
		}
		if recs[1].Properties["path"] != "/{BASE}/things" {
			t.Errorf("path mutated to %q from an unreadable declaring module",
				recs[1].Properties["path"])
		}
	case <-time.After(20 * time.Second):
		// Deliberately fatal rather than a hang: with os.ReadFile this arm is
		// what fires, and a test binary that hangs forever tells nobody why.
		t.Fatal("FoldConsumerHTTPBaseURLs blocked on a FIFO — it is not using " +
			"internal/safeio (#6416)")
	}
}
