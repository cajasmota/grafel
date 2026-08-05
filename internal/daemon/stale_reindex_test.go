package daemon

// stale_reindex_test.go — #5907 FIX 2: proves the loop-guard on the auto-reindex
// action arm. The engine recomputes ReindexRequired on EVERY heartbeat, so the
// load-bearing property is that a stale-format repo enqueues EXACTLY ONE reindex
// request across many heartbeats (never a storm), a current repo enqueues none,
// and the guard self-clears once the graph is current again.

import (
	"fmt"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// countPendingReindex returns how many KindReindex requests are queued for
// repoPath's control-plane requests dir.
func countPendingReindex(t *testing.T, repoPath string) int {
	t.Helper()
	recs, err := requests.ListPending(requestsDirForRepo(repoPath))
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	n := 0
	for _, r := range recs {
		if r.Kind == requests.KindReindex && r.RepoPath == repoPath {
			n++
		}
	}
	return n
}

// TestStaleReindexGuard_ExactlyOnceAcrossHeartbeats is the core proof: a stale
// repo observed on N successive heartbeats (same stale generation ⇒ same
// fingerprint) writes exactly ONE reindex request, not N.
func TestStaleReindexGuard_ExactlyOnceAcrossHeartbeats(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	const repo = "/repo/stale"
	g := newStaleReindexGuard()

	// A single stale generation: mtime + reason are stable across heartbeats.
	fp := staleFingerprint(1_700_000_000, "graph format v3 incompatible with v4 — reindex required")

	firing := 0
	for i := 0; i < 12; i++ {
		if g.maybeEnqueue(repo, true, fp, nil) {
			firing++
		}
	}
	if firing != 1 {
		t.Errorf("maybeEnqueue returned true %d times across 12 heartbeats, want exactly 1", firing)
	}
	if got := countPendingReindex(t, repo); got != 1 {
		t.Fatalf("pending reindex requests = %d, want exactly 1 (no storm)", got)
	}
}

// TestStaleReindexGuard_CurrentRepo_NoRequest is the parity guard: a
// current-format repo (required=false) never enqueues.
func TestStaleReindexGuard_CurrentRepo_NoRequest(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	const repo = "/repo/current"
	g := newStaleReindexGuard()

	for i := 0; i < 5; i++ {
		if g.maybeEnqueue(repo, false, staleFingerprint(0, ""), nil) {
			t.Fatalf("current-format repo must never enqueue (heartbeat %d)", i)
		}
	}
	if got := countPendingReindex(t, repo); got != 0 {
		t.Fatalf("pending reindex requests = %d, want 0 for a current repo", got)
	}
}

// TestStaleReindexGuard_SelfClearsAfterReindex proves the full lifecycle: stale
// → one request; heartbeats while the reindex is in-flight (same fingerprint)
// add none; the graph goes current (required=false) and the guard self-clears;
// a genuinely NEW stale generation later (distinct fingerprint) fires exactly
// one fresh request. This is the anti-#5891 loop-guard end to end.
func TestStaleReindexGuard_SelfClearsAfterReindex(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	const repo = "/repo/lifecycle"
	g := newStaleReindexGuard()

	fp1 := staleFingerprint(100, "graph format v3 incompatible with v4 — reindex required")

	// 1) Stale detected → one request.
	if !g.maybeEnqueue(repo, true, fp1, nil) {
		t.Fatal("first stale observation should enqueue")
	}
	// 2) Reindex in-flight: the stale file is untouched, so the fingerprint is
	//    unchanged across many heartbeats → NO additional requests.
	for i := 0; i < 8; i++ {
		if g.maybeEnqueue(repo, true, fp1, nil) {
			t.Fatalf("in-flight heartbeat %d must not re-enqueue (same stale generation)", i)
		}
	}
	if got := countPendingReindex(t, repo); got != 1 {
		t.Fatalf("after in-flight heartbeats, pending = %d, want 1", got)
	}

	// 3) Reindex completed, graph is current → guard self-clears.
	if g.maybeEnqueue(repo, false, staleFingerprint(0, ""), nil) {
		t.Fatal("a now-current repo must not enqueue")
	}

	// 4) A genuinely NEW stale generation (distinct fingerprint) may fire once.
	fp2 := staleFingerprint(200, "graph format v3 incompatible with v4 — reindex required")
	if !g.maybeEnqueue(repo, true, fp2, nil) {
		t.Fatal("a new stale generation after self-clear should enqueue exactly once")
	}
	for i := 0; i < 4; i++ {
		if g.maybeEnqueue(repo, true, fp2, nil) {
			t.Fatalf("second-generation heartbeat %d must not re-enqueue", i)
		}
	}
	if got := countPendingReindex(t, repo); got != 2 {
		t.Fatalf("total pending across two stale generations = %d, want 2 (one per generation)", got)
	}
}

// ---------------------------------------------------------------------------
// #6167 — format-version migration must be survivable at 140 repos.
//
// The per-repo loop guard above bounds "one request per stale generation PER
// REPO". It does NOT bound how many repos migrate at once, and writeAll()
// (statuswriter.go:521) walks EVERY registered repo on every 5s heartbeat — so
// the first heartbeat after a fbversion bump writes one reindex request per
// stale repo, all of them, at once. The tests below assert the batching policy
// that bounds that: at most staleReindexBatchSize outstanding auto-reindexes,
// and an idle window between batches so the scheduler's heavy write stages can
// acquire the stage-gate token (sched/stagegate.go:489 blocks them for as long
// as any index job is in flight).
// ---------------------------------------------------------------------------

// fakeClock is a deterministic, manually-advanced clock for the batching tests.
// No sleeps: every timing assertion below is exact.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestGuard builds a guard with an injected clock and explicit policy knobs.
func newTestGuard(clk *fakeClock, batch int, cooldown, ttl time.Duration) *staleReindexGuard {
	g := newStaleReindexGuard()
	g.now = clk.now
	g.batchSize = batch
	g.cooldown = cooldown
	g.slotTTL = ttl
	return g
}

// heartbeat simulates one statusWriter.writeAll() pass over repos: every repo
// still in stale is offered to the guard, in order. Returns the repos admitted
// on this pass.
func heartbeat(g *staleReindexGuard, repos []string, stale map[string]bool) []string {
	var admitted []string
	for _, r := range repos {
		required := stale[r]
		fp := staleFingerprint(1, "graph.fb format version 4 is older than required version 5")
		if !required {
			fp = staleFingerprint(0, "")
		}
		if g.maybeEnqueue(r, required, fp, nil) {
			admitted = append(admitted, r)
		}
	}
	return admitted
}

// TestStaleReindexGuard_FirstHeartbeatDoesNotStampede is the core defect proof:
// 140 stale repos observed on ONE heartbeat must not produce 140 reindex
// requests. Pre-fix this admits all 140.
func TestStaleReindexGuard_FirstHeartbeatDoesNotStampede(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 2, 30*time.Second, 15*time.Minute)

	repos := make([]string, 140)
	stale := map[string]bool{}
	for i := range repos {
		repos[i] = fmt.Sprintf("/repo/%03d", i)
		stale[repos[i]] = true
	}

	admitted := heartbeat(g, repos, stale)
	if len(admitted) != 2 {
		t.Fatalf("first heartbeat admitted %d of 140 stale repos, want at most the batch size 2 (stampede)", len(admitted))
	}

	total := 0
	for _, r := range repos {
		total += countPendingReindex(t, r)
	}
	if total != 2 {
		t.Fatalf("durable pending reindex requests after one heartbeat = %d, want 2", total)
	}
}

// TestStaleReindexGuard_RefusedRepoIsRetriedNotDropped proves the refusal is a
// DEFERRAL, not a drop: a repo turned away because the batch was full must be
// admitted on a later heartbeat once the batch drains. Otherwise throttling
// would silently strand 138 repos forever.
func TestStaleReindexGuard_RefusedRepoIsRetriedNotDropped(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 2, 30*time.Second, 15*time.Minute)

	repos := []string{"/repo/a", "/repo/b", "/repo/c", "/repo/d"}
	stale := map[string]bool{"/repo/a": true, "/repo/b": true, "/repo/c": true, "/repo/d": true}

	first := heartbeat(g, repos, stale)
	if len(first) != 2 || first[0] != "/repo/a" || first[1] != "/repo/b" {
		t.Fatalf("first batch = %v, want [/repo/a /repo/b]", first)
	}
	// Batch still in flight: no further admissions, however many heartbeats.
	for i := 0; i < 5; i++ {
		clk.advance(5 * time.Second)
		if got := heartbeat(g, repos, stale); len(got) != 0 {
			t.Fatalf("heartbeat %d admitted %v while the batch was in flight, want none", i, got)
		}
	}
	// Batch completes (both repos go current), then the cooldown elapses.
	stale["/repo/a"] = false
	stale["/repo/b"] = false
	clk.advance(5 * time.Second)
	heartbeat(g, repos, stale) // observes the two completions, drains the batch
	clk.advance(31 * time.Second)

	second := heartbeat(g, repos, stale)
	if len(second) != 2 || second[0] != "/repo/c" || second[1] != "/repo/d" {
		t.Fatalf("second batch = %v, want [/repo/c /repo/d] — deferred repos must be retried", second)
	}
}

// TestStaleReindexGuard_IdleWindowBetweenBatches is the stage-gate argument in
// test form: after a batch drains, the guard must refuse to admit the next one
// until the cooldown has elapsed. That gap is the only window in which
// sched/stagegate.go can hand the heavy-write token to group-algo/links —
// stageBlockReasonLocked returns "N index job(s) in flight" for as long as any
// index is running (stagegate.go:489).
func TestStaleReindexGuard_IdleWindowBetweenBatches(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 1, 30*time.Second, 15*time.Minute)

	repos := []string{"/repo/a", "/repo/b"}
	stale := map[string]bool{"/repo/a": true, "/repo/b": true}

	if got := heartbeat(g, repos, stale); len(got) != 1 {
		t.Fatalf("first batch = %v, want 1", got)
	}
	stale["/repo/a"] = false
	heartbeat(g, repos, stale) // batch drains here

	// Inside the cooldown: nothing may be admitted, even though a slot is free.
	clk.advance(29 * time.Second)
	if got := heartbeat(g, repos, stale); len(got) != 0 {
		t.Fatalf("admitted %v inside the cooldown, want none — the idle window is what un-starves the stage gate", got)
	}
	// Past the cooldown: the next batch goes.
	clk.advance(2 * time.Second)
	if got := heartbeat(g, repos, stale); len(got) != 1 || got[0] != "/repo/b" {
		t.Fatalf("after cooldown admitted %v, want [/repo/b]", got)
	}
}

// TestStaleReindexGuard_StuckSlotExpires guards the deadlock the batching
// introduces if left unbounded: a repo whose reindex never completes (crash,
// permanent index failure) holds its slot forever and the remaining 138 repos
// never migrate. The slot TTL releases it. The wedged repo must NOT be
// re-enqueued (its fingerprint is still recorded) — releasing the slot buys
// progress for the others, it does not retry the failure.
func TestStaleReindexGuard_StuckSlotExpires(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 1, 30*time.Second, 15*time.Minute)

	repos := []string{"/repo/wedged", "/repo/next"}
	stale := map[string]bool{"/repo/wedged": true, "/repo/next": true}

	if got := heartbeat(g, repos, stale); len(got) != 1 || got[0] != "/repo/wedged" {
		t.Fatalf("first batch = %v, want [/repo/wedged]", got)
	}
	// /repo/wedged never goes current. Before the TTL, nothing else moves.
	clk.advance(14 * time.Minute)
	if got := heartbeat(g, repos, stale); len(got) != 0 {
		t.Fatalf("admitted %v before the slot TTL, want none", got)
	}
	// Crossing the TTL reclaims the slot. That drains the batch, so the normal
	// cooldown applies from the forfeit — a wedged slot is not a licence to
	// skip the idle window.
	clk.advance(2 * time.Minute)
	if got := heartbeat(g, repos, stale); len(got) != 0 {
		t.Fatalf("admitted %v on the forfeit heartbeat, want none — the cooldown starts when the batch drains", got)
	}
	// After the TTL forfeit AND the cooldown, the migration continues.
	clk.advance(31 * time.Second)
	got := heartbeat(g, repos, stale)
	if len(got) != 1 || got[0] != "/repo/next" {
		t.Fatalf("after the slot TTL admitted %v, want [/repo/next] — a wedged repo must not block the migration", got)
	}
	if n := countPendingReindex(t, "/repo/wedged"); n != 1 {
		t.Fatalf("wedged repo has %d pending requests, want exactly 1 — TTL expiry must not re-enqueue a known failure", n)
	}
}

// TestStaleReindexGuard_140RepoUpgrade_Simulation is the end-to-end shape of the
// user's report: 140 repos go stale at once on a version bump. It asserts the
// three properties that make the migration survivable — bounded outstanding
// work at every instant, at least one idle window per batch, and eventual
// completion with exactly one reindex per repo.
func TestStaleReindexGuard_140RepoUpgrade_Simulation(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	const batch = 2
	g := newTestGuard(clk, batch, 30*time.Second, 15*time.Minute)

	repos := make([]string, 140)
	stale := map[string]bool{}
	for i := range repos {
		repos[i] = fmt.Sprintf("/repo/%03d", i)
		stale[repos[i]] = true
	}

	admittedCount := map[string]int{}
	idleWindows := 0
	// 5s heartbeats. Each admitted repo takes 60s of simulated indexing.
	finishAt := map[string]time.Time{}
	for tick := 0; tick < 20000; tick++ {
		// Complete any reindex whose simulated run is done.
		for r, at := range finishAt {
			if !clk.now().Before(at) {
				stale[r] = false
				delete(finishAt, r)
			}
		}
		outstanding := len(finishAt)
		if outstanding > batch {
			t.Fatalf("tick %d: %d reindexes outstanding, want <= %d", tick, outstanding, batch)
		}
		if outstanding == 0 {
			idleWindows++
		}
		for _, r := range heartbeat(g, repos, stale) {
			admittedCount[r]++
			finishAt[r] = clk.now().Add(60 * time.Second)
		}
		done := true
		for _, r := range repos {
			if stale[r] {
				done = false
				break
			}
		}
		if done {
			break
		}
		clk.advance(5 * time.Second)
	}

	for _, r := range repos {
		if admittedCount[r] != 1 {
			t.Fatalf("repo %s admitted %d times, want exactly 1 (migration must complete, exactly once per repo)", r, admittedCount[r])
		}
	}
	if idleWindows < 70 {
		t.Fatalf("only %d idle heartbeats across the migration, want >= 70 (one per batch) — the stage gate needs them", idleWindows)
	}
}
