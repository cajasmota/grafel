package progress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setHome points GRAFEL_HOME at a fresh temp dir for the duration of a test so
// sidecar files never touch the real ~/.grafel.
func setHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GRAFEL_HOME", dir)
	return dir
}

// drainReader reads every complete line currently on disk for a group and
// returns the reconstructed events.
func drainReader(t *testing.T, groupSlug string) []Event {
	t.Helper()
	r, err := NewSidecarReader(groupSlug)
	if err != nil {
		t.Fatalf("NewSidecarReader: %v", err)
	}
	evs, _, err := r.ReadFrom(0)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	return evs
}

func projectFields(e Event) Event {
	// Only the fields the sidecar line carries survive a round-trip.
	return Event{
		GroupSlug:     e.GroupSlug,
		RepoSlug:      e.RepoSlug,
		Module:        e.Module,
		Phase:         e.Phase,
		FilesDone:     e.FilesDone,
		FilesTotal:    e.FilesTotal,
		EntitiesSoFar: e.EntitiesSoFar,
		CurrentFile:   e.CurrentFile,
		TS:            e.TS,
		Error:         e.Error,
	}
}

// TestSidecarPathDerivation asserts the group file lives under
// GRAFEL_HOME/progress and is deterministically derived (writer and a future
// reader agree with zero coordination).
func TestSidecarPathDerivation(t *testing.T) {
	home := setHome(t)
	p1, err := SidecarPath("group-a")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := SidecarPath("group-a")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("path not deterministic: %q vs %q", p1, p2)
	}
	wantDir := filepath.Join(home, "progress")
	if filepath.Dir(p1) != wantDir {
		t.Fatalf("path dir = %q, want %q", filepath.Dir(p1), wantDir)
	}
	if !strings.HasSuffix(p1, ".ndjson") {
		t.Fatalf("path %q does not end in .ndjson", p1)
	}
	pOther, err := SidecarPath("group-b")
	if err != nil {
		t.Fatal(err)
	}
	if pOther == p1 {
		t.Fatal("distinct groups mapped to the same file")
	}
}

// TestWriteReadRoundTripTwoGroups writes events across two groups and multiple
// modules, then asserts each group file contains only its own events and that
// events reconstruct via the reader.
func TestWriteReadRoundTripTwoGroups(t *testing.T) {
	setHome(t)

	wA, err := NewSidecarWriter("grp-a", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wB, err := NewSidecarWriter("grp-b", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	aEvents := []Event{
		{GroupSlug: "grp-a", RepoSlug: "repo1", Module: "services/auth", Phase: PhaseExtractAST, FilesDone: 3, FilesTotal: 10, EntitiesSoFar: 5, CurrentFile: "a.go", TS: 100},
		{GroupSlug: "grp-a", RepoSlug: "repo1", Module: "packages/ui", Phase: PhaseExtractAST, FilesDone: 1, FilesTotal: 4, TS: 101},
		{GroupSlug: "grp-a", RepoSlug: "repo2", Module: "", Phase: PhaseDone, FilesDone: 10, FilesTotal: 10, EntitiesSoFar: 42, TS: 102},
	}
	bEvents := []Event{
		{GroupSlug: "grp-b", RepoSlug: "svc", Module: "cmd", Phase: PhaseScan, FilesTotal: 7, TS: 200},
	}
	for _, e := range aEvents {
		wA.Publish(e)
	}
	for _, e := range bEvents {
		wB.Publish(e)
	}
	if err := wA.Close(); err != nil {
		t.Fatalf("close wA: %v", err)
	}
	if err := wB.Close(); err != nil {
		t.Fatalf("close wB: %v", err)
	}

	gotA := drainReader(t, "grp-a")
	if len(gotA) != len(aEvents) {
		t.Fatalf("group a: got %d lines, want %d: %+v", len(gotA), len(aEvents), gotA)
	}
	// Terminal + two distinct-module non-terminal lines, all present.
	for i, e := range aEvents {
		want := projectFields(e)
		found := false
		for _, g := range gotA {
			if projectFields(g) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("group a event %d not reconstructed: %+v", i, want)
		}
	}
	gotB := drainReader(t, "grp-b")
	if len(gotB) != 1 {
		t.Fatalf("group b: got %d lines, want 1", len(gotB))
	}
	if projectFields(gotB[0]) != projectFields(bEvents[0]) {
		t.Errorf("group b round-trip mismatch:\n got %+v\nwant %+v", projectFields(gotB[0]), projectFields(bEvents[0]))
	}

	// seq must be assigned and strictly increasing within a group file.
	assertSeqAscending(t, "grp-a")
}

func assertSeqAscending(t *testing.T, groupSlug string) {
	t.Helper()
	p, err := SidecarPath(groupSlug)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	for _, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if ln == "" {
			continue
		}
		var l SidecarLine
		if err := json.Unmarshal([]byte(ln), &l); err != nil {
			t.Fatalf("bad line %q: %v", ln, err)
		}
		if l.Seq <= last {
			t.Fatalf("seq not ascending: %d after %d", l.Seq, last)
		}
		last = l.Seq
	}
	if last == 0 {
		t.Fatal("no seq assigned")
	}
}

// TestCoalescing feeds many rapid ticks for the same (repo,module) within a
// single flush interval and asserts the flushed line count is far below the
// event count, and the latest state wins.
func TestCoalescing(t *testing.T) {
	setHome(t)
	// A very long flush interval means the only flush is the one Close forces:
	// a single interval => a single line per (repo,module).
	w, err := NewSidecarWriter("coal", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	const n = 2000
	for i := 0; i < n; i++ {
		w.Publish(Event{
			GroupSlug: "coal", RepoSlug: "r", Module: "m",
			Phase: PhaseExtractAST, FilesDone: i, FilesTotal: n,
			CurrentFile: "f.go", TS: int64(i),
		})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := drainReader(t, "coal")
	if len(got) != 1 {
		t.Fatalf("coalescing failed: got %d lines for %d events, want 1", len(got), n)
	}
	if got[0].FilesDone != n-1 {
		t.Fatalf("latest state did not win: FilesDone=%d, want %d", got[0].FilesDone, n-1)
	}
}

// TestTerminalFlushesPromptly asserts a terminal event is flushed without
// waiting for the ticker, even under a long flush interval.
func TestTerminalFlushesPromptly(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("term", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Publish(Event{GroupSlug: "term", RepoSlug: "r", Phase: PhaseDone, FilesDone: 9, FilesTotal: 9, EntitiesSoFar: 3, TS: 1})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := drainReader(t, "term")
		if len(got) == 1 && got[0].Phase == PhaseDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("terminal event was not flushed promptly")
}

// TestTerminalSurvivesCoalescing asserts that a terminal event coexists with a
// coalesced non-terminal line for a different module and is never dropped.
func TestTerminalSurvivesCoalescing(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("mix", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		w.Publish(Event{GroupSlug: "mix", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
	}
	w.Publish(Event{GroupSlug: "mix", RepoSlug: "r", Module: "m2", Phase: PhaseError, Error: "boom", TS: 999})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := drainReader(t, "mix")
	var sawTerminal bool
	for _, e := range got {
		if e.Phase == PhaseError && e.Error == "boom" {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Fatalf("terminal event lost among %d lines", len(got))
	}
}

// TestPartialLastLineTolerated asserts the reader skips a torn trailing line
// and returns the complete lines before it.
func TestPartialLastLineTolerated(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("torn", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	w.Publish(Event{GroupSlug: "torn", RepoSlug: "r", Module: "m", Phase: PhaseScan, FilesTotal: 5, TS: 1})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := SidecarPath("torn")
	if err != nil {
		t.Fatal(err)
	}
	// Append a truncated (no trailing newline, invalid JSON) line.
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":99,"group_slug":"torn","phase":"extr`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, err := NewSidecarReader("torn")
	if err != nil {
		t.Fatal(err)
	}
	got, off, err := r.ReadFrom(0)
	if err != nil {
		t.Fatalf("ReadFrom tolerating torn line: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (torn line must be skipped)", len(got))
	}
	// Offset must stop before the torn line so a later append completing it is
	// picked up on the next read.
	st, _ := os.Stat(p)
	if off >= st.Size() {
		t.Fatalf("offset %d should be before EOF %d (torn tail not yet consumed)", off, st.Size())
	}
}

// TestReaderAppendAware asserts ReadFrom can be called repeatedly to pick up
// newly-appended lines from the reported offset.
func TestReaderAppendAware(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("tail", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	w.Publish(Event{GroupSlug: "tail", RepoSlug: "r", Module: "m", Phase: PhaseScan, TS: 1})
	// Publish a terminal for a different module to force an immediate flush of
	// the pending non-terminal line too.
	w.Publish(Event{GroupSlug: "tail", RepoSlug: "r", Module: "z", Phase: PhaseDone, TS: 2})

	r, err := NewSidecarReader("tail")
	if err != nil {
		t.Fatal(err)
	}
	var off int64
	var total int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && total < 2 {
		evs, newOff, err := r.ReadFrom(off)
		if err != nil {
			t.Fatal(err)
		}
		total += len(evs)
		off = newOff
		if total < 2 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if total != 2 {
		t.Fatalf("append-aware read got %d events, want 2", total)
	}
	// A further read from the same offset yields nothing new.
	evs, _, err := r.ReadFrom(off)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("re-read from tail offset returned %d events, want 0", len(evs))
	}
	w.Close()
}

// TestTruncateOnNewRun asserts a second writer for the same group starts a
// fresh stream (the old run's lines are gone).
func TestTruncateOnNewRun(t *testing.T) {
	setHome(t)
	w1, err := NewSidecarWriter("run", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		w1.Publish(Event{GroupSlug: "run", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
	}
	w1.Publish(Event{GroupSlug: "run", RepoSlug: "r", Phase: PhaseDone, TS: 9})
	w1.Close()

	before := drainReader(t, "run")
	if len(before) == 0 {
		t.Fatal("expected first run to write lines")
	}

	w2, err := NewSidecarWriter("run", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Immediately after construction (before any publish) the file must be
	// empty — the new run truncated it.
	mid := drainReader(t, "run")
	if len(mid) != 0 {
		t.Fatalf("new run did not truncate: %d stale lines remain", len(mid))
	}
	w2.Publish(Event{GroupSlug: "run", RepoSlug: "r", Module: "m", Phase: PhaseScan, FilesTotal: 3, TS: 100})
	w2.Close()
	after := drainReader(t, "run")
	if len(after) != 1 {
		t.Fatalf("second run: got %d lines, want 1", len(after))
	}
	if after[0].TS != 100 {
		t.Fatalf("second run did not start fresh: TS=%d", after[0].TS)
	}
}

// TestCompactionShrinksAndRetains crafts a file with many duplicate-key lines
// plus terminals, runs compaction, and asserts the file shrinks while the
// latest-per-key and terminal lines survive in seq order.
func TestCompactionShrinksAndRetains(t *testing.T) {
	setHome(t)
	p, err := SidecarPath("comp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	var seq int64
	writeLine := func(l SidecarLine) {
		seq++
		l.Seq = seq
		b, _ := json.Marshal(l)
		f.Write(b)
		f.Write([]byte("\n"))
	}
	// 100 updates to two live keys.
	for i := 0; i < 100; i++ {
		writeLine(SidecarLine{GroupSlug: "comp", RepoSlug: "r", Module: "m1", Phase: PhaseExtractAST, FilesDone: i})
		writeLine(SidecarLine{GroupSlug: "comp", RepoSlug: "r", Module: "m2", Phase: PhaseExtractAST, FilesDone: i})
	}
	// A terminal line that must be retained.
	writeLine(SidecarLine{GroupSlug: "comp", RepoSlug: "r", Module: "m3", Phase: PhaseDone, FilesDone: 1, Done: true})
	f.Close()

	stBefore, _ := os.Stat(p)

	if err := compactSidecarFile(p); err != nil {
		t.Fatalf("compactSidecarFile: %v", err)
	}

	stAfter, _ := os.Stat(p)
	if stAfter.Size() >= stBefore.Size() {
		t.Fatalf("compaction did not shrink: before=%d after=%d", stBefore.Size(), stAfter.Size())
	}

	got := drainReader(t, "comp")
	// Expect exactly 3 lines: latest of m1, latest of m2, terminal m3.
	if len(got) != 3 {
		t.Fatalf("compacted line count = %d, want 3: %+v", len(got), got)
	}
	byMod := map[string]Event{}
	for _, e := range got {
		byMod[e.Module] = e
	}
	if byMod["m1"].FilesDone != 99 || byMod["m2"].FilesDone != 99 {
		t.Fatalf("latest-per-key not retained: %+v", byMod)
	}
	if byMod["m3"].Phase != PhaseDone {
		t.Fatal("terminal line not retained after compaction")
	}
	assertSeqAscending(t, "comp")
}

// TestWriterCompactsAtSizeCap asserts the running writer compacts its own file
// once it crosses the configured byte cap, keeping latest-per-key + terminal.
func TestWriterCompactsAtSizeCap(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("cap",
		WithFlushInterval(5*time.Millisecond),
		WithMaxBytes(700),
	)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := SidecarPath("cap")
	// Keep updating a single key across many flush intervals so the file
	// accumulates duplicate lines and eventually crosses the cap.
	for i := 0; i < 400; i++ {
		w.Publish(Event{GroupSlug: "cap", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
		if i%20 == 0 {
			time.Sleep(6 * time.Millisecond)
		}
	}
	w.Publish(Event{GroupSlug: "cap", RepoSlug: "r", Phase: PhaseDone, FilesDone: 400, TS: 400})
	w.Close()

	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	// After compaction the file must be well under a pathological unbounded size.
	if st.Size() > 5000 {
		t.Fatalf("file not compacted under cap pressure: size=%d", st.Size())
	}
	got := drainReader(t, "cap")
	var sawDone bool
	for _, e := range got {
		if e.Phase == PhaseDone {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("terminal lost after cap compaction")
	}
}

// TestDropOldestUnderFullBuffer floods the writer far faster than the (blocked)
// flush loop can drain and asserts Publish never blocks and Close still joins.
func TestDropOldestUnderFullBuffer(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("flood",
		WithFlushInterval(time.Hour), // never ticks during the test
		WithBufferSize(4),
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100000; i++ {
			w.Publish(Event{GroupSlug: "flood", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked under a full buffer (drop-oldest failed)")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close did not join cleanly: %v", err)
	}
}

// TestPruneTerminalSidecars asserts stale terminal'd files are deleted while a
// live (non-terminal, fresh) file is kept.
func TestPruneTerminalSidecars(t *testing.T) {
	setHome(t)

	// A terminated, old file.
	wOld, err := NewSidecarWriter("old", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wOld.Publish(Event{GroupSlug: "old", RepoSlug: "r", Phase: PhaseDone, TS: 1})
	wOld.Close()
	oldPath, _ := SidecarPath("old")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, past, past); err != nil {
		t.Fatal(err)
	}

	// A live, non-terminal, fresh file.
	wLive, err := NewSidecarWriter("live", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wLive.Publish(Event{GroupSlug: "live", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: 1, TS: 2})
	wLive.Close()
	livePath, _ := SidecarPath("live")

	n, err := PruneTerminalSidecars(time.Minute)
	if err != nil {
		t.Fatalf("PruneTerminalSidecars: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d files, want 1", n)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("stale terminal'd file was not deleted")
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatal("live file was wrongly deleted")
	}
}

// TestTerminalSurvivesBufferEviction is the B1 regression: a terminal event
// published BEFORE a flood of >bufSize newer events must reach disk. Before the
// fix the terminal rode the lossy drop-oldest channel and, once it became the
// FIFO head, was silently evicted by the flood — leaving sawTerminal=false and
// a wizard bar stuck forever.
func TestTerminalSurvivesBufferEviction(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("evict",
		WithFlushInterval(time.Hour), // ticker never fires; only wake/quit flush
		WithBufferSize(8),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Fast repo finishes first...
	w.Publish(Event{GroupSlug: "evict", RepoSlug: "fast", Phase: PhaseDone, FilesDone: 5, FilesTotal: 5, TS: 1})
	// ...then slow repos bury it under far more than bufSize newer events.
	for i := 0; i < 50000; i++ {
		w.Publish(Event{GroupSlug: "evict", RepoSlug: "slow", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i + 2)})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := drainReader(t, "evict")
	var sawTerminal bool
	for _, e := range got {
		if e.RepoSlug == "fast" && e.Phase == PhaseDone {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Fatalf("terminal evicted under buffer pressure (B1): %d lines on disk, none terminal", len(got))
	}
}

// TestTerminalSurvivesConcurrentProducers is the harder B1 variant: many
// goroutines (as a TeePublisher fan-out would drive) hammer the writer while a
// terminal is in flight. The terminal must still land.
func TestTerminalSurvivesConcurrentProducers(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("evict2",
		WithFlushInterval(time.Hour),
		WithBufferSize(4),
	)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20000; i++ {
				w.Publish(Event{GroupSlug: "evict2", RepoSlug: "slow", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
			}
		}(g)
	}
	// Publish the terminal concurrently with the flood.
	w.Publish(Event{GroupSlug: "evict2", RepoSlug: "fast", Phase: PhaseError, Error: "kaboom", TS: 999})
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := drainReader(t, "evict2")
	var sawTerminal bool
	for _, e := range got {
		if e.Phase == PhaseError && e.Error == "kaboom" {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Fatalf("terminal lost under concurrent producers (B1): %d lines on disk", len(got))
	}
}

// TestReadFromAfterTruncate is the B2 regression: a reader holding a stale
// offset from run 1 must pick up run 2 after NewSidecarWriter truncates the
// file. Before the fix ReadFrom seeked past EOF and returned nothing forever.
func TestReadFromAfterTruncate(t *testing.T) {
	setHome(t)
	// Run 1: write several lines so the reader carries a non-trivial offset.
	w1, err := NewSidecarWriter("re", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		w1.Publish(Event{GroupSlug: "re", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
	}
	w1.Publish(Event{GroupSlug: "re", RepoSlug: "r", Phase: PhaseDone, TS: 9})
	w1.Close()

	r, err := NewSidecarReader("re")
	if err != nil {
		t.Fatal(err)
	}
	evs1, off1, err := r.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs1) == 0 || off1 == 0 {
		t.Fatalf("run 1 read got %d events at offset %d", len(evs1), off1)
	}

	// Run 2: a fresh writer truncates the file and writes a distinct run.
	w2, err := NewSidecarWriter("re", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	w2.Publish(Event{GroupSlug: "re", RepoSlug: "r", Module: "m", Phase: PhaseScan, FilesTotal: 3, TS: 100})
	w2.Publish(Event{GroupSlug: "re", RepoSlug: "r", Phase: PhaseDone, TS: 101})
	w2.Close()

	// Reading from the carried (now-stale, too-large) offset must reset and
	// return run 2 in full.
	evs2, off2, err := r.ReadFrom(off1)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs2) != 2 {
		t.Fatalf("post-truncate read got %d events, want 2 (B2 reset failed)", len(evs2))
	}
	if off2 >= off1 {
		t.Fatalf("reset not surfaced: new offset %d should be < stale offset %d", off2, off1)
	}
	// The run-2 fresh-start marker must be present.
	var sawScan bool
	for _, e := range evs2 {
		if e.Phase == PhaseScan && e.TS == 100 {
			sawScan = true
		}
	}
	if !sawScan {
		t.Fatal("post-truncate read did not return the new run's opening event")
	}
}

// TestReadFromAfterCompaction is the second B2 case: compaction shrinks the file
// below the reader's saved offset. The reader must reset and replay.
func TestReadFromAfterCompaction(t *testing.T) {
	setHome(t)
	p, err := SidecarPath("rc")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	var seq int64
	writeLine := func(l SidecarLine) {
		seq++
		l.Seq = seq
		b, _ := json.Marshal(l)
		f.Write(b)
		f.Write([]byte("\n"))
	}
	for i := 0; i < 200; i++ {
		writeLine(SidecarLine{GroupSlug: "rc", RepoSlug: "r", Module: "m", Phase: PhaseExtractAST, FilesDone: i})
	}
	writeLine(SidecarLine{GroupSlug: "rc", RepoSlug: "r", Module: "done", Phase: PhaseDone, Done: true})
	f.Close()

	r, err := NewSidecarReader("rc")
	if err != nil {
		t.Fatal(err)
	}
	_, offBig, err := r.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}

	if err := compactSidecarFile(p); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Size() >= offBig {
		t.Fatalf("precondition: compacted file (%d) must be smaller than saved offset (%d)", st.Size(), offBig)
	}

	// Reading from the stale offset must reset and return the compacted content
	// (latest m + terminal done = 2 lines).
	evs, offNew, err := r.ReadFrom(offBig)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("post-compaction read got %d events, want 2 (B2 reset failed)", len(evs))
	}
	if offNew >= offBig {
		t.Fatalf("reset not surfaced after compaction: new offset %d should be < %d", offNew, offBig)
	}
}

// TestResetClearsPendingState is the medium fix: Reset must produce a genuinely
// fresh stream — seq restarts and no stale coalesced/terminal state leaks in.
func TestResetClearsPendingState(t *testing.T) {
	setHome(t)
	w, err := NewSidecarWriter("rs", WithFlushInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Bury some non-terminal state that has NOT been flushed yet.
	for i := 0; i < 100; i++ {
		w.Publish(Event{GroupSlug: "rs", RepoSlug: "r", Module: "old", Phase: PhaseExtractAST, FilesDone: i, TS: int64(i)})
	}
	if err := w.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	// After Reset the file is empty and the buried "old" state is gone.
	if got := drainReader(t, "rs"); len(got) != 0 {
		t.Fatalf("Reset did not clear stream: %d lines remain", len(got))
	}
	// A fresh event flushes with seq restarting at 1.
	w.Publish(Event{GroupSlug: "rs", RepoSlug: "r", Module: "new", Phase: PhaseScan, FilesTotal: 4, TS: 500})
	w.Close()

	pth, _ := SidecarPath("rs")
	data, err := os.ReadFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("fresh stream got %d lines, want 1: %q", len(lines), string(data))
	}
	var l SidecarLine
	if err := json.Unmarshal([]byte(lines[0]), &l); err != nil {
		t.Fatal(err)
	}
	if l.Seq != 1 {
		t.Fatalf("seq did not restart after Reset: got %d, want 1", l.Seq)
	}
	if l.Module != "new" {
		t.Fatalf("stale pre-Reset state leaked: module=%q", l.Module)
	}
}

// TestLineFromEventRoundTrip exercises the marshal/unmarshal helpers directly.
func TestLineFromEventRoundTrip(t *testing.T) {
	e := Event{
		GroupSlug: "g", RepoSlug: "r", Module: "m",
		Phase: PhaseExtractAST, FilesDone: 7, FilesTotal: 9,
		EntitiesSoFar: 11, CurrentFile: "x.go", TS: 123,
	}
	l := lineFromEvent(e)
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var l2 SidecarLine
	if err := json.Unmarshal(b, &l2); err != nil {
		t.Fatal(err)
	}
	if got := l2.toEvent(); projectFields(got) != projectFields(e) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", projectFields(got), projectFields(e))
	}
	// A terminal event marks Done.
	term := lineFromEvent(Event{GroupSlug: "g", Phase: PhaseDone})
	if !term.Done {
		t.Fatal("terminal event did not set Done")
	}
	// Verify the on-the-wire JSON keys match the documented schema.
	if !strings.Contains(string(b), `"files_done"`) || !strings.Contains(string(b), `"entities"`) {
		t.Fatalf("unexpected JSON schema: %s", b)
	}
}
