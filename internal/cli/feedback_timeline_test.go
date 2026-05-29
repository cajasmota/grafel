package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// readTimelineDoc runs `feedback timeline` and returns the parsed timeline.json
// plus the timeline.md bytes.
func readTimelineDoc(t *testing.T, outDir string) (timelineDoc, string) {
	t.Helper()
	jb, err := os.ReadFile(filepath.Join(outDir, "timeline.json"))
	if err != nil {
		t.Fatalf("read timeline.json: %v", err)
	}
	var doc timelineDoc
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("unmarshal timeline.json: %v", err)
	}
	mb, err := os.ReadFile(filepath.Join(outDir, "timeline.md"))
	if err != nil {
		t.Fatalf("read timeline.md: %v", err)
	}
	return doc, string(mb)
}

func runTimeline(t *testing.T, opts timelineOpts) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	if err := runFeedbackTimeline(cmd, opts); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	return out, errBuf
}

// TestTimeline_MergeOrderingAndTrailerCorrelation builds feedback events + a
// fake rpc-log + a temp git repo with two commits (one carrying an
// Archigraph-Phase trailer) and asserts chronological merge order and exact
// trailer correlation.
func TestTimeline_MergeOrderingAndTrailerCorrelation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, "events")
	outDir := filepath.Join(tmp, "out")

	// Feedback events: a planning checkpoint, then a port:auth checkpoint.
	writeEventsFile(t, eventsDir, "feedback-events-2026-05-30.jsonl", []string{
		`{"ts":"2026-05-30T08:00:00Z","group":"new-backend","phase":"planning","outcome":"milestone","note":"kickoff: target language chosen"}`,
		`{"ts":"2026-05-30T12:00:00Z","group":"new-backend","phase":"port:auth","capability":"auth","outcome":"helped"}`,
	})

	// Fake rpc-log: one done line at 09:00 (within planning window), doubled.
	rpcLine := `time=2026-05-30T09:00:00Z level=INFO msg=mcp_rpc phase=done tool=archigraph_find elapsed_ms=12 wire_bytes=400 payload_token_estimate=100 repo=/x/new-backend`
	rpcPath := filepath.Join(tmp, "daemon.log")
	if err := os.WriteFile(rpcPath, []byte(rpcLine+"\n"+rpcLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Temp git repo with two commits: one with the trailer (force phase), one
	// without (falls into nearest-preceding feedback phase window).
	repo := newTestRepo(t, tmp)
	// Commit A at 10:00Z with explicit Archigraph-Phase: planning trailer.
	gitCommit(t, repo, "2026-05-30T10:00:00+00:00", "scaffold project", "Archigraph-Phase: planning")
	// Commit B at 13:00Z with NO trailer → should fall into port:auth window
	// (nearest preceding feedback checkpoint at 12:00Z).
	gitCommit(t, repo, "2026-05-30T13:00:00+00:00", "implement login", "")

	out, _ := runTimeline(t, timelineOpts{
		outDir:    outDir,
		eventsDir: eventsDir,
		rpcLog:    rpcPath,
		repos:     []string{repo},
	})
	if !strings.Contains(out.String(), "built timeline") {
		t.Errorf("expected summary line, got %q", out.String())
	}

	doc, _ := readTimelineDoc(t, outDir)

	// Chronological order by TS.
	for i := 1; i < len(doc.Entries); i++ {
		if doc.Entries[i-1].TS > doc.Entries[i].TS {
			t.Fatalf("entries not chronological: %s before %s", doc.Entries[i-1].TS, doc.Entries[i].TS)
		}
	}

	// Find the two commits and assert phase correlation.
	var scaffold, login *timelineEntry
	for i := range doc.Entries {
		e := &doc.Entries[i]
		if e.Kind != "commit" {
			continue
		}
		if strings.Contains(e.Summary, "scaffold project") {
			scaffold = e
		}
		if strings.Contains(e.Summary, "implement login") {
			login = e
		}
	}
	if scaffold == nil || login == nil {
		t.Fatalf("expected both commits in timeline; entries=%+v", doc.Entries)
	}
	// Exact trailer correlation.
	if scaffold.Phase != "planning" {
		t.Errorf("scaffold commit phase = %q, want planning (from trailer)", scaffold.Phase)
	}
	// Time-window fallback: 13:00 commit > 12:00 port:auth checkpoint.
	if login.Phase != "port:auth" {
		t.Errorf("login commit phase = %q, want port:auth (time-window fallback)", login.Phase)
	}

	// The rpc query at 09:00 should fall into the planning window (after the
	// 08:00 planning checkpoint, before the 12:00 port:auth one).
	var foundQuery bool
	for _, e := range doc.Entries {
		if e.Kind == "query" {
			foundQuery = true
			if e.Phase != "planning" {
				t.Errorf("query phase = %q, want planning", e.Phase)
			}
			if e.TokenEst != 100 {
				t.Errorf("query token_est = %d, want 100", e.TokenEst)
			}
		}
	}
	if !foundQuery {
		t.Error("expected the rpc query to appear once (deduped from the double-log)")
	}
	// Dedup: exactly one query entry despite the doubled log line.
	queries := 0
	for _, e := range doc.Entries {
		if e.Kind == "query" {
			queries++
		}
	}
	if queries != 1 {
		t.Errorf("expected exactly 1 deduped query, got %d", queries)
	}
}

// TestTimeline_PlanningFirst asserts the "planning" chapter renders before
// other phases in timeline.md regardless of timestamps.
func TestTimeline_PlanningFirst(t *testing.T) {
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, "events")
	outDir := filepath.Join(tmp, "out")

	// port:auth happens FIRST in time; planning later — yet planning must be
	// rendered first.
	writeEventsFile(t, eventsDir, "feedback-events-2026-05-30.jsonl", []string{
		`{"ts":"2026-05-30T08:00:00Z","phase":"port:auth","outcome":"helped"}`,
		`{"ts":"2026-05-30T09:00:00Z","phase":"planning","outcome":"milestone","note":"retro decision"}`,
	})

	runTimeline(t, timelineOpts{outDir: outDir, eventsDir: eventsDir, rpcLog: filepath.Join(tmp, "nope.log")})
	doc, md := readTimelineDoc(t, outDir)

	if len(doc.GeneratedPhases) == 0 || doc.GeneratedPhases[0] != "planning" {
		t.Fatalf("planning must be first phase; got %v", doc.GeneratedPhases)
	}
	planIdx := strings.Index(md, "## planning")
	authIdx := strings.Index(md, "## port:auth")
	if planIdx < 0 || authIdx < 0 {
		t.Fatalf("both chapters must render; planIdx=%d authIdx=%d", planIdx, authIdx)
	}
	if planIdx > authIdx {
		t.Errorf("planning chapter must render before port:auth (planIdx=%d authIdx=%d)", planIdx, authIdx)
	}
}

// TestTimeline_MilestoneRendering asserts a milestone feedback event appears
// prominently in the narrative and does NOT appear in any rollup fix-queue.
func TestTimeline_MilestoneRendering(t *testing.T) {
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, "events")
	outDir := filepath.Join(tmp, "out")

	writeEventsFile(t, eventsDir, "feedback-events-2026-05-30.jsonl", []string{
		`{"ts":"2026-05-30T09:00:00Z","phase":"port:inspections","capability":"data_access","outcome":"milestone","note":"inspections reaches parity"}`,
	})

	runTimeline(t, timelineOpts{outDir: outDir, eventsDir: eventsDir, rpcLog: filepath.Join(tmp, "nope.log")})
	_, md := readTimelineDoc(t, outDir)
	if !strings.Contains(md, "MILESTONE") || !strings.Contains(md, "inspections reaches parity") {
		t.Errorf("milestone must render prominently in narrative; md=\n%s", md)
	}

	// Same event through the rollup → must NOT produce a fix-queue item.
	rollupOut := filepath.Join(tmp, "rollup")
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	if err := runFeedbackRollup(cmd, "", rollupOut, eventsDir); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	jb, err := os.ReadFile(filepath.Join(rollupOut, "rollup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r feedbackRollup
	if err := json.Unmarshal(jb, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.FixQueue) != 0 {
		t.Errorf("milestone must not appear in fix queue; got %v", r.FixQueue)
	}
}

// TestTimeline_NoData asserts a friendly message and no error on empty dirs.
func TestTimeline_NoData(t *testing.T) {
	tmp := t.TempDir()
	out, _ := runTimeline(t, timelineOpts{
		outDir:    filepath.Join(tmp, "out"),
		eventsDir: filepath.Join(tmp, "events"), // does not exist
		rpcLog:    filepath.Join(tmp, "nope.log"),
	})
	if !strings.Contains(out.String(), "no timeline events") {
		t.Errorf("expected 'no timeline events' message; got %q", out.String())
	}
	// Outputs are still written (empty narrative) — should parse cleanly.
	doc, md := readTimelineDoc(t, filepath.Join(tmp, "out"))
	if len(doc.Entries) != 0 {
		t.Errorf("expected zero entries; got %d", len(doc.Entries))
	}
	if !strings.Contains(md, "No timeline events") {
		t.Errorf("expected empty-narrative note; got %q", md)
	}
}

// ---- git test helpers ------------------------------------------------------

func newTestRepo(t *testing.T, parent string) string {
	t.Helper()
	repo := filepath.Join(parent, "newrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if outB, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, outB)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("config", "commit.gpgsign", "false")
	return repo
}

// gitCommit makes an empty commit at the given author/committer date with an
// optional trailer appended to the message. Pass an explicit timezone offset in
// isoDate (e.g. ...+00:00) so the UTC-normalized timeline is runner-TZ-stable.
func gitCommit(t *testing.T, repo, isoDate, subject, trailer string) {
	t.Helper()
	msg := subject
	if trailer != "" {
		msg += "\n\n" + trailer
	}
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-q", "-m", msg)
	// Force deterministic author + committer dates.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+isoDate,
		"GIT_COMMITTER_DATE="+isoDate,
	)
	if outB, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, outB)
	}
}
