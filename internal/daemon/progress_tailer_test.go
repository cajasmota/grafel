package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

// collectPub is a thread-safe progress.Publisher that records every event so a
// test can assert what the tailer republished, in order.
type collectPub struct {
	mu     sync.Mutex
	events []progress.Event
}

func (c *collectPub) Publish(e progress.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collectPub) snapshot() []progress.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]progress.Event, len(c.events))
	copy(out, c.events)
	return out
}

// writeSidecarFixture hand-writes an NDJSON sidecar for slug at its
// deterministic on-disk path (the same path progress.NewSidecarReader(slug)
// reads). Overwrites (truncates) so it doubles as the "new run" rotation used
// by the offset-reset test.
func writeSidecarFixture(t *testing.T, slug string, lines []progress.SidecarLine) {
	t.Helper()
	path, err := progress.SidecarPath(slug)
	if err != nil {
		t.Fatalf("SidecarPath(%q): %v", slug, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var buf bytes.Buffer
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// TestSidecarTailer_RepublishesMultiGroup writes two group sidecars — each with
// per-module extraction events and a group-scoped terminal — and asserts the
// tailer republishes every event (per-module included), preserves order, marks
// each group terminal, and republishes NOTHING further after the terminal.
func TestSidecarTailer_RepublishesMultiGroup(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	writeSidecarFixture(t, "grp-a", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-x", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 10},
		{Seq: 2, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-y", Phase: progress.PhaseExtractAST, FilesDone: 3, FilesTotal: 8},
		{Seq: 3, GroupSlug: "grp-a", RepoSlug: "grp-a", Phase: progress.PhaseDone, Done: true},
	})
	writeSidecarFixture(t, "grp-b", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-b", RepoSlug: "svc", Module: "mod-z", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 4},
		{Seq: 2, GroupSlug: "grp-b", RepoSlug: "grp-b", Phase: progress.PhaseDone, Done: true},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, time.Hour, nil)
	tl.groupsFn = func() []string { return []string{"grp-a", "grp-b"} }

	tl.tick()
	got := pub.snapshot()

	// Every fixture line for both groups must be republished.
	if len(got) != 5 {
		t.Fatalf("republished %d events, want 5: %+v", len(got), got)
	}

	// Ordering: grp-a's three (in seq order) then grp-b's two.
	wantOrder := []struct {
		group, repo, module, phase string
	}{
		{"grp-a", "repo1", "mod-x", progress.PhaseExtractAST},
		{"grp-a", "repo1", "mod-y", progress.PhaseExtractAST},
		{"grp-a", "grp-a", "", progress.PhaseDone},
		{"grp-b", "svc", "mod-z", progress.PhaseExtractAST},
		{"grp-b", "grp-b", "", progress.PhaseDone},
	}
	for i, w := range wantOrder {
		e := got[i]
		if e.GroupSlug != w.group || e.RepoSlug != w.repo || e.Module != w.module || e.Phase != w.phase {
			t.Errorf("event[%d] = {g:%q r:%q m:%q p:%q}, want {g:%q r:%q m:%q p:%q}",
				i, e.GroupSlug, e.RepoSlug, e.Module, e.Phase, w.group, w.repo, w.module, w.phase)
		}
	}

	// Both groups marked terminal (group-scoped done seen).
	if st := tl.states["grp-a"]; st == nil || !st.terminal {
		t.Error("grp-a not marked terminal")
	}
	if st := tl.states["grp-b"]; st == nil || !st.terminal {
		t.Error("grp-b not marked terminal")
	}

	// A subsequent tick republishes NOTHING — tailer stops on terminal.
	tl.tick()
	if n := len(pub.snapshot()); n != 5 {
		t.Errorf("after terminal, republished more events: got %d, want 5", n)
	}
}

// TestSidecarTailer_OffsetResetReplay verifies that after the tailer consumes a
// run, truncating + rewriting the sidecar for a NEW (smaller) run is detected as
// a shrink and replayed from 0 — no stale-offset stall.
func TestSidecarTailer_OffsetResetReplay(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	// Run 1: two lines, no terminal (still tailing).
	writeSidecarFixture(t, "grp", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp", RepoSlug: "r", Module: "m", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 20},
		{Seq: 2, GroupSlug: "grp", RepoSlug: "r", Module: "m", Phase: progress.PhaseExtractAST, FilesDone: 15, FilesTotal: 20},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, time.Hour, nil)
	tl.groupsFn = func() []string { return []string{"grp"} }

	tl.tick()
	if n := len(pub.snapshot()); n != 2 {
		t.Fatalf("run 1 republished %d events, want 2", n)
	}
	off := tl.states["grp"].offset
	if off <= 0 {
		t.Fatalf("offset not advanced: %d", off)
	}

	// Run 2: a single, SHORTER line — the file is now smaller than off, so the
	// reader must detect the shrink and replay from 0.
	writeSidecarFixture(t, "grp", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp", RepoSlug: "r", Module: "m", Phase: progress.PhaseScan, FilesDone: 2, FilesTotal: 6},
	})
	if fi, _ := os.Stat(tl.states["grp"].reader.Path()); fi.Size() >= off {
		t.Fatalf("run 2 file not smaller than prior offset (%d >= %d); test cannot exercise shrink", fi.Size(), off)
	}

	tl.tick()
	got := pub.snapshot()
	// The new run's event must have been republished (not stalled behind the
	// stale large offset).
	last := got[len(got)-1]
	if last.Phase != progress.PhaseScan || last.FilesDone != 2 {
		t.Errorf("new-run event not republished; last = %+v", last)
	}
	// Offset re-seeded to the (smaller) new-run size.
	if tl.states["grp"].offset >= off {
		t.Errorf("offset not re-seeded after shrink: got %d, prior %d", tl.states["grp"].offset, off)
	}
}

// TestSidecarTailer_StartStopLifecycle exercises the goroutine start/stop path:
// startSidecarTailer's returned stop func must join the goroutine, and a
// running tailer must republish a fixture's events end-to-end.
func TestSidecarTailer_StartStopLifecycle(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	writeSidecarFixture(t, "live", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "live", RepoSlug: "r", Module: "m", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 2},
		{Seq: 2, GroupSlug: "live", RepoSlug: "live", Phase: progress.PhaseDone, Done: true},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, 15*time.Millisecond, nil)
	tl.groupsFn = func() []string { return []string{"live"} }
	go tl.run()

	deadline := time.After(2 * time.Second)
	for {
		if len(pub.snapshot()) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("tailer did not republish fixture in time; got %d", len(pub.snapshot()))
		case <-time.After(10 * time.Millisecond):
		}
	}
	// stop must join cleanly.
	tl.shutdown()

	// Terminal delivered.
	var sawTerminal bool
	for _, e := range pub.snapshot() {
		if e.Phase == progress.PhaseDone {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Error("terminal event never republished")
	}
}
