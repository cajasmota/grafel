package daemon

// stale_reindex_6175_test.go — #6175: the THIRD migration state.
//
// #6167 gave the format migration two outcomes: "in progress" and "could not be
// migrated". A repo the SCHEDULER declines to accept has neither. It is
// abandoned without penalty (abandonLocked), stands aside for the undispatched
// backoff, and is offered again — forever — while `grafel status` keeps
// counting it as in-progress. Measured in the issue: 23 admissions per repo per
// 12 simulated hours, each burning a batch slot for the 90s stalled grace.
//
// These tests pin both halves: the repo is never admitted again once the
// scheduler's own SkipEnqueue predicate says it will be dropped (capacity), and
// it is reported under its own state instead of as in-progress (visibility).

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/statusfile"
)

// TestStaleReindexGuard_DeclinedRepoIsNotRetriedForever is the capacity half of
// the defect, at the cadence the issue measured: a repo the scheduler drops by
// design must stop consuming admissions and batch slots entirely.
func TestStaleReindexGuard_DeclinedRepoIsNotRetriedForever(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const declined = "/repo/worktree"
	const ok = "/repo/ok"
	repos := []string{declined, ok}
	stale := map[string]bool{declined: true, ok: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	// The scheduler is busy with other work and silently drops `declined`.
	g.activeFn = activeElsewhere
	g.declinedFn = func(p string) bool { return p == declined }

	admissions := 0
	othersMigrated := false
	// 12 simulated hours at the issue's 30s sampling.
	for i := 0; i < 1440; i++ {
		clk.advance(30 * time.Second)
		for _, r := range heartbeat(g, repos, stale) {
			if r == declined {
				admissions++
			}
			if r == ok {
				othersMigrated = true
			}
		}
	}

	if admissions != 0 {
		t.Errorf("a repo the scheduler declines to accept was admitted %d times over 12 simulated hours, want 0", admissions)
	}
	if n := countPendingReindex(t, declined); n != 0 {
		t.Errorf("wrote %d reindex request(s) for a repo whose enqueue the scheduler drops, want 0", n)
	}
	if !othersMigrated {
		t.Error("the declined repo must not stop the rest of the migration")
	}
}

// TestStaleReindexGuard_DeclineIsReevaluatedNotLatched is the un-stick path.
// The decline is a LIVE property of the scheduler's gate — a linked worktree
// stops being declined the moment its primary leaves the watched set — so the
// state must never be latched. Nothing durable is written, and the very next
// heartbeat after the gate opens admits the repo normally.
func TestStaleReindexGuard_DeclineIsReevaluatedNotLatched(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/formerly-declined"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.activeFn = activeElsewhere
	declining := true
	g.declinedFn = func(string) bool { return declining }

	for i := 0; i < 100; i++ {
		clk.advance(30 * time.Second)
		if len(heartbeat(g, repos, stale)) != 0 {
			t.Fatal("admitted a declined repo")
		}
	}
	if !g.notAcceptedByIndexer(repo) {
		t.Fatal("a declined repo must report the not-accepted state")
	}

	// The primary is unregistered / the worktree becomes a repo in its own
	// right: the gate opens.
	declining = false
	clk.advance(30 * time.Second)
	if got := heartbeat(g, repos, stale); len(got) != 1 {
		t.Fatalf("after the gate opened the repo must be admitted again, admitted %v", got)
	}
	if g.notAcceptedByIndexer(repo) {
		t.Error("not-accepted must clear the moment the scheduler stops declining the repo")
	}
	if n := g.attemptsFor(repo); n != 0 {
		t.Errorf("a declined repo must accrue no attempt history; attempts = %d, want 0", n)
	}
}

// TestStaleReindexGuard_DeclinedSlotHolderIsReleased covers the transition the
// other direction: a repo admitted BEFORE the gate started declining it holds a
// batch slot for work that will now never be dispatched. It must give the slot
// back rather than sit on it until the stalled grace expires.
func TestStaleReindexGuard_DeclinedSlotHolderIsReleased(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/late-decline"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.activeFn = activeElsewhere
	declining := false
	g.declinedFn = func(string) bool { return declining }

	clk.advance(30 * time.Second)
	if got := heartbeat(g, repos, stale); len(got) != 1 {
		t.Fatalf("setup: expected the repo to be admitted, admitted %v", got)
	}
	if _, held := g.inflight[repo]; !held {
		t.Fatal("setup: expected the repo to hold a batch slot")
	}

	declining = true
	clk.advance(30 * time.Second) // well inside the 90s stalled grace
	heartbeat(g, repos, stale)

	if _, held := g.inflight[repo]; held {
		t.Error("a repo the scheduler now declines still holds a batch slot for work that will never be dispatched")
	}
}

// TestStaleReindexGuard_DeclinedThenReacceptedRepoIsReadmitted is the
// COMPOSITION the other two decline tests each miss half of: admitted while the
// gate was open, declined, then accepted again.
//
// `seen` is keyed on a fingerprint of the graph.fb mtime, and a repo that is
// never indexed never changes it — so a `seen` entry left behind by the
// admission that preceded the decline suppresses every future admission for the
// life of the process. That is not hypothetical: reposToWatch() is re-read on
// every gate call, so registering a primary and then unregistering it walks
// exactly this path.
func TestStaleReindexGuard_DeclinedThenReacceptedRepoIsReadmitted(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/round-trip"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.activeFn = activeElsewhere
	declining := false
	g.declinedFn = func(string) bool { return declining }

	// 1) Accepted: admitted normally, recording a fingerprint in `seen`.
	clk.advance(30 * time.Second)
	if got := heartbeat(g, repos, stale); len(got) != 1 {
		t.Fatalf("setup: expected an admission while the gate was open, got %v", got)
	}

	// 2) The primary is registered: the gate closes on this repo.
	declining = true
	for i := 0; i < 10; i++ {
		clk.advance(30 * time.Second)
		if got := heartbeat(g, repos, stale); len(got) != 0 {
			t.Fatalf("admitted a declined repo: %v", got)
		}
	}

	// 3) The primary is unregistered: the gate opens again. The repo's graph.fb
	//    never changed, so its fingerprint is identical to the one step 1
	//    recorded — nothing about the repo distinguishes it from an in-flight
	//    admission except the guard having let go of it.
	declining = false
	readmitted := false
	for i := 0; i < 200 && !readmitted; i++ {
		clk.advance(30 * time.Second)
		if len(heartbeat(g, repos, stale)) == 1 {
			readmitted = true
		}
	}
	if !readmitted {
		t.Error("a repo that was admitted, then declined, then accepted again was never re-admitted — it is stranded until the daemon restarts")
	}
}

// TestStaleReindexGuard_FailedRepoIsNotAlsoReportedNotAccepted keeps the three
// states mutually exclusive. A repo that WAS dispatched and died repeatedly is
// Failed and needs a manual reindex; if it were also reported as not-accepted,
// `grafel status` would subtract it from the in-progress count twice and would
// print both the actionable and the "nothing to do" line for the same repo.
func TestStaleReindexGuard_FailedRepoIsNotAlsoReportedNotAccepted(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/crashy"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.retryJitter = 0
	dispatched := true
	g.activeFn = func() map[string]bool {
		if dispatched {
			return map[string]bool{repo: true, "/repo/unrelated": true}
		}
		return activeElsewhere()
	}

	for i := 0; i < 600 && !g.migrationFailed(repo); i++ {
		clk.advance(30 * time.Second)
		heartbeat(g, repos, stale)
		dispatched = !dispatched
	}
	if !g.migrationFailed(repo) {
		t.Fatal("setup: expected the repo to reach the give-up state")
	}

	// It is now ALSO behind the scheduler's decline gate.
	g.declinedFn = func(string) bool { return true }
	if g.notAcceptedByIndexer(repo) {
		t.Error("a repo already reported as unmigratable must not also be reported as not-accepted")
	}
}

// makeLinkedWorktreeFixture builds a real primary/linked-worktree pair on disk
// of the shape worktree.ClassifyRoot recognises: the primary has a `.git`
// DIRECTORY, the worktree has a `.git` FILE pointing at
// <primary>/.git/worktrees/<name>. Returns (primary, worktree).
func makeLinkedWorktreeFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	wt := filepath.Join(root, "wt")
	gitDir := filepath.Join(primary, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "worktrees", "wt"), 0o755); err != nil {
		t.Fatalf("mkdir primary: %v", err)
	}
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	pointer := "gitdir: " + filepath.Join(gitDir, "worktrees", "wt") + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(pointer), 0o644); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	return primary, wt
}

// TestInstallWorktreeEnqueueGate_FeedsTheMigrationGuard pins the WIRING: the
// scheduler's enqueue-drop predicate and the migration guard's decline gate must
// be the same function, or the guard goes on re-admitting repos the scheduler
// silently drops. Exercised against a real linked-worktree pair rather than a
// stub, so the classifier is in the loop too.
func TestInstallWorktreeEnqueueGate_FeedsTheMigrationGuard(t *testing.T) {
	primary, wt := makeLinkedWorktreeFixture(t)

	restoreDeclineGate(t)
	gate := installWorktreeEnqueueGate(func() []string { return []string{primary} })

	if gate == nil {
		t.Fatal("setup: expected a gate for a non-nil reposToWatch")
	}
	if !gate(wt) {
		t.Fatal("setup: the scheduler gate must drop a linked worktree of a watched primary (#3680)")
	}
	if !defaultStaleReindexGuard.notAcceptedByIndexer(wt) {
		t.Error("the migration guard was not given the scheduler's enqueue gate — it will keep re-admitting a repo the scheduler drops")
	}
	if defaultStaleReindexGuard.notAcceptedByIndexer(primary) {
		t.Error("the primary is indexed normally; it must not be reported as not-accepted")
	}
}

// TestStartEnginePlane_InstallsTheDeclineGate grades the PRODUCTION call site.
//
// The two tests above call installWorktreeEnqueueGate themselves, so they pin
// the helper and not its use: reverting engineplane.go to
// `SkipEnqueue: makeWorktreeEnqueueGate(cfg.ReposToWatch)` removes the whole fix
// from the shipping binary and leaves them both green. This one starts the
// engine plane the way the daemon does and asserts the guard came out of it
// holding the scheduler's gate.
func TestStartEnginePlane_InstallsTheDeclineGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvRoot, root)
	t.Setenv("GRAFEL_HOME", root)
	t.Setenv(EnvDisableSelfDefense, "1")

	primary, wt := makeLinkedWorktreeFixture(t)

	// Start from a guard that has NO gate, so a pass cannot come from one an
	// earlier test left behind.
	restoreDeclineGate(t)
	defaultStaleReindexGuard.setDeclineGate(nil)
	if defaultStaleReindexGuard.notAcceptedByIndexer(wt) {
		t.Fatal("setup: expected no decline gate installed before startEnginePlane")
	}

	cfg := Config{
		Layout:         Layout{Root: root},
		ReposToWatch:   func() []string { return []string{primary} },
		SchedulerIndex: func(context.Context, string, string) error { return nil },
	}
	ep := startEnginePlane(context.Background(), cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(ep.shutdown)

	if !defaultStaleReindexGuard.notAcceptedByIndexer(wt) {
		t.Error("startEnginePlane did not give the migration guard the scheduler's enqueue gate — the guard will keep re-admitting repos the scheduler drops")
	}
	if defaultStaleReindexGuard.notAcceptedByIndexer(primary) {
		t.Error("the watched primary is indexed normally; it must not be reported as not-accepted")
	}
}

// TestWriteRepoStatusFile_NotAcceptedByIndexer is the visibility half, end to
// end and through the REAL gate: a stale-format linked worktree of a watched
// primary — the exact case EnqueueRefCommit drops (#3680) — must publish the
// third state to the status plane, alongside (not instead of) ReindexRequired.
func TestWriteRepoStatusFile_NotAcceptedByIndexer(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())

	primary, wt := makeLinkedWorktreeFixture(t)
	writeOldFormatGraphFBAt(t, StateDirForRepo(wt), 2)
	writeOldFormatGraphFBAt(t, StateDirForRepo(primary), 2)

	restoreDeclineGate(t)
	installWorktreeEnqueueGate(func() []string { return []string{primary} })

	writeRepoStatusFile(wt, nil)
	got, err := statusfile.Read(wt)
	if err != nil {
		t.Fatalf("statusfile.Read(worktree): %v", err)
	}
	if !got.ReindexRequired {
		t.Fatal("setup: the worktree's graph.fb is an old format, so ReindexRequired must be true")
	}
	if !got.ReindexNotAccepted {
		t.Error("a stale linked worktree of a watched primary is dropped by the scheduler — the status plane must say so instead of counting it as in-progress forever")
	}
	if got.ReindexMigrationFailed {
		t.Error("nothing is broken about this repo — it must not be reported as a failed migration")
	}

	// Control: the primary itself is a normal repo the scheduler accepts, so
	// the flag must NOT be set for it. Without this the assertion above would
	// pass against an implementation that sets the flag unconditionally.
	writeRepoStatusFile(primary, nil)
	gotPrimary, err := statusfile.Read(primary)
	if err != nil {
		t.Fatalf("statusfile.Read(primary): %v", err)
	}
	if !gotPrimary.ReindexRequired {
		t.Fatal("setup: the primary's graph.fb is an old format too")
	}
	if gotPrimary.ReindexNotAccepted {
		t.Error("an ordinary stale repo the scheduler will happily index must not be reported as not-accepted")
	}
}

// restoreDeclineGate saves and restores the process-wide guard's decline gate
// so a test that installs one cannot leak it into the rest of the package.
func restoreDeclineGate(t *testing.T) {
	t.Helper()
	defaultStaleReindexGuard.mu.Lock()
	prev := defaultStaleReindexGuard.declinedFn
	defaultStaleReindexGuard.mu.Unlock()
	t.Cleanup(func() {
		defaultStaleReindexGuard.mu.Lock()
		defaultStaleReindexGuard.declinedFn = prev
		defaultStaleReindexGuard.mu.Unlock()
	})
}
