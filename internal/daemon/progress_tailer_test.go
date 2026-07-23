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

// TestSidecarTailer_RepublishesMultiGroup writes two group sidecars — each
// mid-run (NOT yet terminal) on first sight, so the tailer's normal live-tail
// path applies (the #5937 first-sight-already-terminal seeding case is
// covered separately by
// TestSidecarTailer_FirstSightAlreadyTerminalSeedsWithoutPublishing) — then
// appends each group's group-scoped terminal as a genuinely NEW, live event
// and asserts the tailer republishes every event (per-module included),
// preserves order, marks each group terminal, and republishes NOTHING further
// after the terminal.
func TestSidecarTailer_RepublishesMultiGroup(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	// First sight: both groups mid-run, no terminal yet.
	writeSidecarFixture(t, "grp-a", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-x", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 10},
		{Seq: 2, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-y", Phase: progress.PhaseExtractAST, FilesDone: 3, FilesTotal: 8},
	})
	writeSidecarFixture(t, "grp-b", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-b", RepoSlug: "svc", Module: "mod-z", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 4},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, time.Hour, nil)
	tl.groupsFn = func() []string { return []string{"grp-a", "grp-b"} }

	tl.tick()
	got := pub.snapshot()
	if len(got) != 3 {
		t.Fatalf("first tick republished %d events, want 3: %+v", len(got), got)
	}
	if st := tl.states["grp-a"]; st == nil || st.terminal {
		t.Error("grp-a unexpectedly marked terminal on first (non-terminal) tick")
	}
	if st := tl.states["grp-b"]; st == nil || st.terminal {
		t.Error("grp-b unexpectedly marked terminal on first (non-terminal) tick")
	}

	// Each group's run now reaches its group-scoped terminal — appended live,
	// growing the file, so this is genuinely new content the tailer must
	// republish (distinct from the first-sight-already-terminal case).
	writeSidecarFixture(t, "grp-a", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-x", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 10},
		{Seq: 2, GroupSlug: "grp-a", RepoSlug: "repo1", Module: "mod-y", Phase: progress.PhaseExtractAST, FilesDone: 3, FilesTotal: 8},
		{Seq: 3, GroupSlug: "grp-a", RepoSlug: "grp-a", Phase: progress.PhaseDone, Done: true},
	})
	writeSidecarFixture(t, "grp-b", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "grp-b", RepoSlug: "svc", Module: "mod-z", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 4},
		{Seq: 2, GroupSlug: "grp-b", RepoSlug: "grp-b", Phase: progress.PhaseDone, Done: true},
	})

	tl.tick()
	got = pub.snapshot()

	// Every fixture line for both groups must be republished across both ticks.
	if len(got) != 5 {
		t.Fatalf("republished %d events, want 5: %+v", len(got), got)
	}

	// Ordering: the tailer processes groups independently (groupsFn order per
	// tick), and this test spans two ticks (partial content on tick 1, each
	// group's terminal appended for tick 2) — so cross-group interleaving of
	// the flat sequence is NOT a guarantee the tailer makes; grp-a's and
	// grp-b's events land in whichever tick produced them. What IS guaranteed,
	// and what actually matters, is that each group's OWN events arrive in
	// strict seq order with no gaps and no reordering relative to its sidecar.
	// So partition by group and assert per-group ordering.
	var grpAEvents, grpBEvents []progress.Event
	for _, e := range got {
		switch e.GroupSlug {
		case "grp-a":
			grpAEvents = append(grpAEvents, e)
		case "grp-b":
			grpBEvents = append(grpBEvents, e)
		default:
			t.Fatalf("republished event for unexpected group: %+v", e)
		}
	}

	wantA := []struct{ repo, module, phase string }{
		{"repo1", "mod-x", progress.PhaseExtractAST},
		{"repo1", "mod-y", progress.PhaseExtractAST},
		{"grp-a", "", progress.PhaseDone},
	}
	if len(grpAEvents) != len(wantA) {
		t.Fatalf("grp-a republished %d events, want %d: %+v", len(grpAEvents), len(wantA), grpAEvents)
	}
	for i, w := range wantA {
		e := grpAEvents[i]
		if e.RepoSlug != w.repo || e.Module != w.module || e.Phase != w.phase {
			t.Errorf("grp-a event[%d] = {r:%q m:%q p:%q}, want {r:%q m:%q p:%q}",
				i, e.RepoSlug, e.Module, e.Phase, w.repo, w.module, w.phase)
		}
	}

	wantB := []struct{ repo, module, phase string }{
		{"svc", "mod-z", progress.PhaseExtractAST},
		{"grp-b", "", progress.PhaseDone},
	}
	if len(grpBEvents) != len(wantB) {
		t.Fatalf("grp-b republished %d events, want %d: %+v", len(grpBEvents), len(wantB), grpBEvents)
	}
	for i, w := range wantB {
		e := grpBEvents[i]
		if e.RepoSlug != w.repo || e.Module != w.module || e.Phase != w.phase {
			t.Errorf("grp-b event[%d] = {r:%q m:%q p:%q}, want {r:%q m:%q p:%q}",
				i, e.RepoSlug, e.Module, e.Phase, w.repo, w.module, w.phase)
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

// TestSidecarTailer_FirstSightAlreadyTerminalSeedsWithoutPublishing verifies
// the #5937 fix: when the tailer sees a group for the FIRST time (e.g. right
// after serve starts) and that group's sidecar tail is already a group-scoped
// terminal, it must seed its offset/terminal state WITHOUT publishing any of
// those events. Those events belong to a run that ended before serve (and
// this tailer) existed; republishing them poisons the broker's retained
// terminal and closes every SSE subscriber before any live event for the
// CURRENT run arrives. Subsequently appended live events for a NEW run (after
// the file shrinks/truncates) must still be published normally.
func TestSidecarTailer_FirstSightAlreadyTerminalSeedsWithoutPublishing(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	// The sidecar already contains a full PRIOR run, terminated, before the
	// tailer ever looks at it.
	writeSidecarFixture(t, "prior-run", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "prior-run", RepoSlug: "repo1", Module: "mod-x", Phase: progress.PhaseExtractAST, FilesDone: 5, FilesTotal: 10},
		{Seq: 2, GroupSlug: "prior-run", RepoSlug: "prior-run", Phase: progress.PhaseDone, Done: true},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, time.Hour, nil)
	tl.groupsFn = func() []string { return []string{"prior-run"} }

	// First sight: must seed state but publish NOTHING.
	tl.tick()
	if n := len(pub.snapshot()); n != 0 {
		t.Fatalf("first sight of an already-terminal sidecar republished %d events, want 0: %+v", n, pub.snapshot())
	}
	st := tl.states["prior-run"]
	if st == nil {
		t.Fatal("tailer did not seed state for prior-run")
	}
	if !st.terminal {
		t.Error("tailer did not mark prior-run terminal on first sight")
	}
	if st.offset <= 0 {
		t.Errorf("tailer did not advance offset past the prior run's content: got %d", st.offset)
	}

	// A subsequent tick with no file changes still republishes nothing.
	tl.tick()
	if n := len(pub.snapshot()); n != 0 {
		t.Errorf("second tick (no new run) republished %d events, want 0", n)
	}

	// Now a NEW run starts: the file is truncated/rewritten smaller, with live
	// (non-terminal) events. These SHOULD be published — normal live tailing
	// resumes for a genuinely new run.
	writeSidecarFixture(t, "prior-run", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "prior-run", RepoSlug: "repo1", Module: "mod-x", Phase: progress.PhaseScan, FilesDone: 1, FilesTotal: 3},
	})
	if fi, _ := os.Stat(st.reader.Path()); fi.Size() >= st.offset {
		t.Fatalf("new run file not smaller than prior offset (%d >= %d); test cannot exercise shrink", fi.Size(), st.offset)
	}

	tl.tick()
	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("new run's live event not republished; got %d events: %+v", len(got), got)
	}
	if got[0].Phase != progress.PhaseScan || got[0].FilesDone != 1 {
		t.Errorf("unexpected republished event for new run: %+v", got[0])
	}
	if tl.states["prior-run"].terminal {
		t.Error("tailer still marked terminal after new run started")
	}
}

// TestSidecarTailer_StartStopLifecycle exercises the goroutine start/stop path:
// startSidecarTailer's returned stop func must join the goroutine, and a
// running tailer must republish a fixture's events end-to-end. The fixture
// starts mid-run (no terminal yet) on first sight so this exercises normal
// live tailing rather than the #5937 first-sight-already-terminal seeding
// case (covered separately); the terminal is appended afterward as a
// genuinely new, live event.
func TestSidecarTailer_StartStopLifecycle(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())

	writeSidecarFixture(t, "live", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "live", RepoSlug: "r", Module: "m", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 2},
	})

	pub := &collectPub{}
	tl := newSidecarTailer(pub, 15*time.Millisecond, nil)
	tl.groupsFn = func() []string { return []string{"live"} }
	go tl.run()

	deadline := time.After(2 * time.Second)
	for {
		if len(pub.snapshot()) >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("tailer did not republish fixture in time; got %d", len(pub.snapshot()))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Append the group-scoped terminal as new, live content.
	writeSidecarFixture(t, "live", []progress.SidecarLine{
		{Seq: 1, GroupSlug: "live", RepoSlug: "r", Module: "m", Phase: progress.PhaseExtractAST, FilesDone: 1, FilesTotal: 2},
		{Seq: 2, GroupSlug: "live", RepoSlug: "live", Phase: progress.PhaseDone, Done: true},
	})

	deadline = time.After(2 * time.Second)
	for {
		var sawTerminal bool
		for _, e := range pub.snapshot() {
			if e.Phase == progress.PhaseDone {
				sawTerminal = true
			}
		}
		if sawTerminal {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("terminal event never republished; got %d events", len(pub.snapshot()))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// stop must join cleanly.
	tl.shutdown()
}
