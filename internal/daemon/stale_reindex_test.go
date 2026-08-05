package daemon

// stale_reindex_test.go — #5907 FIX 2: proves the loop-guard on the auto-reindex
// action arm. The engine recomputes ReindexRequired on EVERY heartbeat, so the
// load-bearing property is that a stale-format repo enqueues EXACTLY ONE reindex
// request across many heartbeats (never a storm), a current repo enqueues none,
// and the guard self-clears once the graph is current again.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon/requests"
	"github.com/cajasmota/grafel/internal/indexstate"
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
	// Deterministic by default: no jitter, and no scheduler signal (so the
	// hard ceiling governs) unless a test injects one.
	g.retryJitter = 0
	g.activeFn = func() map[string]bool { return nil }
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
	g := newTestGuard(clk, batch, 45*time.Second, 15*time.Minute)

	repos := make([]string, 140)
	stale := map[string]bool{}
	for i := range repos {
		repos[i] = fmt.Sprintf("/repo/%03d", i)
		stale[repos[i]] = true
	}

	admittedCount := map[string]int{}
	idleWindows := 0
	consecutiveIdle := 0
	// The window must be at least as long as the cooldown to be usable by a
	// stage gate that retries every stageGateRetryDefault (15s).
	const tickSeconds = 5
	idleTicksNeeded := int((45 * time.Second) / (tickSeconds * time.Second))
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
		// Review finding 4: counting BARE idle ticks was satisfied by the
		// single batch-drain transition tick, so a plain size-2 semaphore
		// passed this test — the very design the batch policy exists to
		// reject. What the stage gate actually needs is a RUN of idle ticks
		// at least as long as the cooldown, so measure consecutive runs and
		// only count one that is long enough to be a usable window.
		if outstanding == 0 {
			consecutiveIdle++
			if consecutiveIdle == idleTicksNeeded {
				idleWindows++
			}
		} else {
			consecutiveIdle = 0
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
	// 140 repos / batch 2 = 70 batches, but the loop exits the moment the last
	// repo goes current, so the final batch's window is never observed — 69 is
	// the exact expected count, not a fudge factor.
	if idleWindows < 69 {
		t.Fatalf("only %d usable idle windows (>= %d consecutive idle ticks) across the migration, want >= 69 — one per batch, each long enough for the stage gate to retry into", idleWindows, idleTicksNeeded)
	}
}

// ---------------------------------------------------------------------------
// #6167 review round 2. Three blocking findings from adversarial review, each
// measured against the real production maybeEnqueue.
// ---------------------------------------------------------------------------

// realRepoDirs creates n real on-disk repo directories and returns their paths.
// Registered repos exist on disk, and the guard drops durable markers for repos
// that do not — so any test that exercises the reconcile path must use real
// paths or it passes for the wrong reason.
func realRepoDirs(t *testing.T, names ...string) []string {
	t.Helper()
	base := t.TempDir()
	out := make([]string, 0, len(names))
	for _, n := range names {
		p := filepath.Join(base, n)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		out = append(out, p)
	}
	return out
}

// restartGuard builds a guard that shares nothing in-memory with a previous
// one — the exact state a daemon restart produces — but sees the SAME durable
// requests dirs. Used to prove the migration resumes rather than restarts.
func restartGuard(clk *fakeClock, batch int, cooldown, ttl time.Duration, _ []string) *staleReindexGuard {
	// No injection: the recovery path under test is the real one, reading the
	// durable migration markers out of the sandboxed store.
	return newTestGuard(clk, batch, cooldown, ttl)
}

// TestStaleReindexGuard_RestartDoesNotDuplicateRequests is review finding 1.
// seen/inflight are in-memory, so a fresh guard used to re-admit every repo
// whose reindex was still pending — graph.fb has not been rewritten, so
// ReindexRequired is still true — writing a duplicate full reindex (42s–4m53s
// each) per repo per restart, unbounded across restarts. The migration must
// RESUME from the durable queue, not restart.
func TestStaleReindexGuard_RestartDoesNotDuplicateRequests(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := realRepoDirs(t, "a", "b", "c", "d")
	stale := map[string]bool{}
	for _, r := range repos {
		stale[r] = true
	}
	dirs := make([]string, 0, len(repos))
	for _, r := range repos {
		dirs = append(dirs, requestsDirForRepo(r))
	}

	g1 := restartGuard(clk, 2, 45*time.Second, 15*time.Minute, dirs)
	pre := heartbeat(g1, repos, stale)
	if len(pre) != 2 {
		t.Fatalf("pre-restart admits = %v, want 2", pre)
	}

	// Restart: brand-new guard, same durable store. Nothing has completed, so
	// every repo is still stale.
	for restart := 1; restart <= 6; restart++ {
		g := restartGuard(clk, 2, 45*time.Second, 15*time.Minute, dirs)
		clk.advance(5 * time.Second)
		if got := heartbeat(g, repos, stale); len(got) != 0 {
			t.Fatalf("restart %d re-admitted %v — the two pending requests already saturate the batch", restart, got)
		}
	}

	total := 0
	for _, r := range repos {
		total += countPendingReindex(t, r)
	}
	if total != 2 {
		t.Fatalf("durable requests after 6 restarts = %d, want 2 (progress must survive restart, not reset)", total)
	}
}

// TestStaleReindexGuard_LaggardDoesNotStallWholeMigration is review finding 2.
// Once the rest of the batch drains, a wedged repo must forfeit its slot at the
// SHORT laggard grace, not the full hard TTL. Measured before this fix: 15m25s
// with nothing admitted at all.
func TestStaleReindexGuard_LaggardDoesNotStallWholeMigration(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	// /repo/fast is genuinely indexing; /repo/wedged never reaches the
	// scheduler at all, which is what makes it forfeitable early.
	g.activeFn = func() map[string]bool { return map[string]bool{"/repo/fast": true} }

	repos := []string{"/repo/fast", "/repo/wedged", "/repo/next", "/repo/next2"}
	stale := map[string]bool{}
	for _, r := range repos {
		stale[r] = true
	}

	if got := heartbeat(g, repos, stale); len(got) != 2 {
		t.Fatalf("first batch = %v, want 2", got)
	}
	// /repo/fast completes quickly; /repo/wedged never does.
	stale["/repo/fast"] = false
	clk.advance(60 * time.Second)
	heartbeat(g, repos, stale)

	// The laggard must forfeit within laggardGrace + cooldown of its batch-mate
	// finishing — NOT the 15m hard TTL.
	deadline := clk.now().Add(5*time.Minute + 45*time.Second + 10*time.Second)
	var resumedAt time.Time
	for clk.now().Before(deadline) {
		clk.advance(5 * time.Second)
		if got := heartbeat(g, repos, stale); len(got) > 0 {
			resumedAt = clk.now()
			break
		}
	}
	if resumedAt.IsZero() {
		t.Fatalf("migration still stalled after %v — a wedged repo must not hold the batch for the full TTL", 5*time.Minute+45*time.Second)
	}
}

// TestStaleReindexGuard_FailedRepoIsRetriedThenReported is the other half of
// finding 2: a forfeited slot used to leave the repo in `seen` forever, a
// silent permanent drop. It must get a bounded number of retries and then be
// explicitly reported.
func TestStaleReindexGuard_FailedRepoIsRetriedThenReported(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	g := newTestGuard(clk, 1, 45*time.Second, 10*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.maxAttempts = 3

	repos := []string{"/repo/broken"}
	stale := map[string]bool{"/repo/broken": true}

	admits := 0
	for tick := 0; tick < 4000; tick++ {
		if got := heartbeat(g, repos, stale); len(got) > 0 {
			admits++
		}
		if g.migrationFailed("/repo/broken") {
			break
		}
		clk.advance(5 * time.Second)
	}
	if admits != 3 {
		t.Errorf("broken repo admitted %d times, want exactly maxAttempts=3 (bounded retry, not one-shot drop)", admits)
	}
	if !g.migrationFailed("/repo/broken") {
		t.Fatal("broken repo must be reported as migration-failed, not silently dropped")
	}
	// And once failed it must never be admitted again.
	before := countPendingReindex(t, "/repo/broken")
	for i := 0; i < 50; i++ {
		clk.advance(time.Minute)
		heartbeat(g, repos, stale)
	}
	if got := countPendingReindex(t, "/repo/broken"); got != before {
		t.Errorf("failed repo re-admitted after giving up: %d -> %d", before, got)
	}
}

// TestStaleReindexGuard_RestartWithPartialBatchDoesNotDuplicate closes the gap
// mutation testing exposed in TestStaleReindexGuard_RestartDoesNotDuplicateRequests:
// that test restarts with a FULL batch pending, so the batch bound alone is
// enough to refuse re-admission and the inflight check is never exercised.
// With only ONE request pending and batchSize 2, the recovered batch still has
// a free slot — so the repo that already has a queued reindex must be refused
// by identity, and the free slot must go to a DIFFERENT repo.
func TestStaleReindexGuard_RestartWithPartialBatchDoesNotDuplicate(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := realRepoDirs(t, "a", "b")
	repoA, repoB := repos[0], repos[1]
	stale := map[string]bool{repoA: true, repoB: true}
	dirs := []string{requestsDirForRepo(repoA), requestsDirForRepo(repoB)}

	// Pre-restart: batch size 1, so exactly one request lands for repo A.
	g1 := restartGuard(clk, 1, 45*time.Second, 15*time.Minute, dirs)
	if got := heartbeat(g1, repos, stale); len(got) != 1 || got[0] != repoA {
		t.Fatalf("pre-restart admits = %v, want [%s]", got, repoA)
	}

	// Restart with batch size 2: the recovered batch holds /repo/a and has one
	// slot free.
	clk.advance(5 * time.Second)
	g2 := restartGuard(clk, 2, 45*time.Second, 15*time.Minute, dirs)
	got := heartbeat(g2, repos, stale)

	for _, r := range got {
		if r == repoA {
			t.Errorf("re-admitted %s, which already has a queued reindex — duplicate full reindex", repoA)
		}
	}
	if n := countPendingReindex(t, repoA); n != 1 {
		t.Fatalf("repo A pending requests = %d, want 1 (no duplicate across restart)", n)
	}
	if n := countPendingReindex(t, repoB); n != 1 {
		t.Fatalf("repo B pending requests = %d, want 1 (the free slot must still be usable)", n)
	}
}

// ---------------------------------------------------------------------------
// #6167 review round 3. Round 2's remedies each closed a narrower window than
// the defect occupied; these model the real timeline instead.
// ---------------------------------------------------------------------------

// drainAndAck models what the engine's drain loop actually does within ~2s of a
// request being written: apply it and remove the request file. After this,
// requests.ListPending reports NOTHING for the repo, which is precisely why a
// ListPending-based reconcile could not see an in-progress migration.
func drainAndAck(t *testing.T, repoPath string) {
	t.Helper()
	dir := requestsDirForRepo(repoPath)
	recs, err := requests.ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	for _, r := range recs {
		if err := requests.WriteAck(dir, r.ID, requests.Ack{ID: r.ID, Status: requests.StatusOK, AppliedAt: time.Now()}); err != nil {
			t.Fatalf("WriteAck: %v", err)
		}
	}
	// ListPending removes an acked request as catch-up cleanup.
	if _, err := requests.ListPending(dir); err != nil {
		t.Fatalf("ListPending after ack: %v", err)
	}
}

// TestStaleReindexGuard_RestartMidIndexDoesNotDuplicate is review round-3
// blocker 1. The user restarts BECAUSE the daemon is busy indexing — 42s–4m53s
// after admission — by which time the 2s drain has long since acked and removed
// the request. A ListPending-based reconcile sees an empty queue and re-admits
// everything: measured 12 duplicate reindexes across 6 mid-index restarts, the
// same magnitude as before any fix. The outstanding set must survive DISPATCH,
// not just the 2-second window before it.
func TestStaleReindexGuard_RestartMidIndexDoesNotDuplicate(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := realRepoDirs(t, "a", "b", "c", "d")
	stale := map[string]bool{}
	for _, r := range repos {
		stale[r] = true
	}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	if got := heartbeat(g, repos, stale); len(got) != 2 {
		t.Fatalf("initial admits = %v, want 2", got)
	}
	// The drain applies and acks both within ~2s. The indexes themselves are
	// still running — nothing has gone current.
	clk.advance(2 * time.Second)
	drainAndAck(t, repos[0])
	drainAndAck(t, repos[1])

	// Now the user restarts, repeatedly, mid-index.
	for restart := 1; restart <= 6; restart++ {
		clk.advance(60 * time.Second) // mid-index, well past the drain
		g = newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
		if got := heartbeat(g, repos, stale); len(got) != 0 {
			t.Fatalf("mid-index restart %d re-admitted %v — the migration must resume, not restart", restart, got)
		}
	}

	// Exactly one resume request per orphaned repo — the scheduler forgot them
	// when the daemon died, so they must be re-told, but only once however many
	// restarts happen. Zero would mean the repos are orphaned forever; more
	// than two would mean the recovery path rebuilt the backlog.
	total := 0
	for _, r := range repos {
		total += countPendingReindex(t, r)
	}
	if total != 2 {
		t.Fatalf("reindex requests after 6 mid-index restarts = %d, want exactly 2 (one resume per orphaned repo)", total)
	}
}

// TestStaleReindexGuard_TwoWedgedReposDoNotStall is review round-3 blocker 2.
// Round 2 applied the short laggard grace only when len(inflight) <
// admittedInBatch — never true when BOTH slot-holders are wedged, so the hard
// 15m TTL governed again and the measured stall was 15m45s, marginally WORSE
// than before the fix. The forfeit decision has to be per-repo.
func TestStaleReindexGuard_TwoWedgedReposDoNotStall(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := []string{"/repo/wedged1", "/repo/wedged2", "/repo/ok1", "/repo/ok2"}
	stale := map[string]bool{}
	for _, r := range repos {
		stale[r] = true
	}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	// Neither wedged repo ever reaches the scheduler — no indexstate entry at
	// all, which is exactly what a dropped request or a failed dispatch looks
	// like. A non-empty snapshot for OTHER repos is what makes "absent" mean
	// "not running" rather than "scheduler hasn't published yet".
	g.activeFn = func() map[string]bool { return map[string]bool{"/repo/other": true} }

	if got := heartbeat(g, repos, stale); len(got) != 2 {
		t.Fatalf("first batch = %v, want 2", got)
	}

	// Both slot-holders are wedged. The migration must resume well inside the
	// hard TTL — the stalled grace plus a cooldown, not 15m45s.
	deadline := clk.now().Add(5 * time.Minute)
	var resumed bool
	for clk.now().Before(deadline) {
		clk.advance(5 * time.Second)
		if got := heartbeat(g, repos, stale); len(got) > 0 {
			resumed = true
			break
		}
	}
	if !resumed {
		t.Fatal("two concurrently wedged repos stalled the whole migration for over 5 minutes")
	}
}

// TestStaleReindexGuard_ActivelyIndexingRepoIsNotForfeited is the other side of
// blocker 2 and of MEDIUM 3: a repo that really is indexing must be protected
// for as long as it keeps working, however slow it is. Round 2's 5m laggard
// grace left 7 seconds of margin against the documented 4m53s worst case, on a
// machine that is MORE loaded during a migration than during the capture.
func TestStaleReindexGuard_ActivelyIndexingRepoIsNotForfeited(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := []string{"/repo/slow", "/repo/next"}
	stale := map[string]bool{"/repo/slow": true, "/repo/next": true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.activeFn = func() map[string]bool { return map[string]bool{"/repo/slow": true} }

	if got := heartbeat(g, repos, stale); len(got) != 1 || got[0] != "/repo/slow" {
		t.Fatalf("first batch = %v, want [/repo/slow]", got)
	}
	// Ten minutes of genuine indexing — twice the observed worst case.
	for i := 0; i < 120; i++ {
		clk.advance(5 * time.Second)
		heartbeat(g, repos, stale)
	}
	if g.attemptsFor("/repo/slow") != 0 {
		t.Errorf("a repo that is actively indexing was forfeited: attempts = %d, want 0", g.attemptsFor("/repo/slow"))
	}
	if g.migrationFailed("/repo/slow") {
		t.Error("an actively-indexing repo must never be reported as unmigratable")
	}
}

// TestStaleReindexGuard_AttemptsResetOnSuccess is review round-3 MEDIUM 3.
// attempts was never cleared, so a repo forfeited once per generation but
// always eventually SUCCEEDING accumulated attempts across generations and was
// eventually reported to the user as unmigratable.
func TestStaleReindexGuard_AttemptsResetOnSuccess(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/slow-but-fine"
	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.activeFn = func() map[string]bool { return nil }

	for gen := 0; gen < 4; gen++ {
		fp := staleFingerprint(int64(gen+1), "stale")
		// Admitted, forfeited (looks stalled), then the index lands anyway.
		g.maybeEnqueue(repo, true, fp, nil)
		clk.advance(20 * time.Minute)
		g.maybeEnqueue(repo, true, fp, nil) // sweep forfeits here
		g.maybeEnqueue(repo, false, "", nil)
		clk.advance(20 * time.Minute)

		if g.migrationFailed(repo) {
			t.Fatalf("generation %d: an always-succeeding repo was reported unmigratable", gen)
		}
		if n := g.attemptsFor(repo); n != 0 {
			t.Fatalf("generation %d: attempts = %d after success, want 0 (must reset)", gen, n)
		}
	}
}

// TestStaleReindexGuard_ReconcileRetriesAfterTransientError is review round-3
// MEDIUM 4: `reconciled` was set BEFORE the fallible call, so one transient
// glob failure silently disabled the restart remedy for the whole process.
func TestStaleReindexGuard_ReconcileRetriesAfterTransientError(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	calls := 0
	g.reconcileStatesFn = func() ([]migrationState, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient: store busy")
		}
		return nil, nil
	}

	g.maybeEnqueue("/repo/a", true, staleFingerprint(1, "stale"), nil)
	g.maybeEnqueue("/repo/b", true, staleFingerprint(1, "stale"), nil)

	if calls < 2 {
		t.Fatalf("reconcile was attempted %d time(s) — a transient error must not disable it permanently", calls)
	}
}

// TestStaleReindexGuard_RetryBackoffIsJittered closes the gap mutation testing
// exposed: every other test disables jitter for determinism, so a uniform
// backoff survived. Round-3 blocker 2 noted that a uniform backoff makes two
// repos that forfeit together wait together and re-pair on the next batch,
// reproducing the stall each cycle. Their retry times must diverge.
func TestStaleReindexGuard_RetryBackoffIsJittered(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	g.retryBackoff = 10 * time.Minute
	g.retryJitter = 5 * time.Minute
	g.stalledGrace = 90 * time.Second
	g.activeFn = func() map[string]bool { return map[string]bool{"/repo/other": true} }

	repos := []string{"/repo/wedged1", "/repo/wedged2"}
	stale := map[string]bool{"/repo/wedged1": true, "/repo/wedged2": true}

	// Both admitted together, both wedged, so both forfeit on the same sweep.
	if got := heartbeat(g, repos, stale); len(got) != 2 {
		t.Fatalf("first batch = %v, want 2", got)
	}
	clk.advance(2 * time.Minute)
	heartbeat(g, repos, stale)

	g.mu.Lock()
	a, okA := g.retryAfter["/repo/wedged1"]
	b, okB := g.retryAfter["/repo/wedged2"]
	g.mu.Unlock()
	if !okA || !okB {
		t.Fatalf("both wedged repos should be in backoff (a=%v b=%v)", okA, okB)
	}
	if a.Equal(b) {
		t.Fatalf("both repos retry at the same instant (%v) — they will re-pair every cycle; backoff must be jittered per repo", a)
	}

	// And the spread must stay inside the declared window.
	spread := a.Sub(b)
	if spread < 0 {
		spread = -spread
	}
	if spread > 5*time.Minute {
		t.Errorf("jitter spread %v exceeds the declared %v window", spread, 5*time.Minute)
	}
}

// ---------------------------------------------------------------------------
// #6167 review round 4. The durable marker restored the GUARD's slot but not
// the SCHEDULER's queue, so a restart left the guard holding a slot for a repo
// the scheduler had never heard of — and !active[repo] then read as "stalled"
// and burned an attempt. The axis that matters:
//
//     !active[repo] conflates "stalled" with "never enqueued",
//     and only the first deserves an attempt penalty.
// ---------------------------------------------------------------------------

// activeAlways models a scheduler that has indexed something (so the snapshot
// is permanently non-empty, per publishRepoStatesLocked unioning in
// indexedRepos) but has never heard of the repos under test.
func activeElsewhere() map[string]bool { return map[string]bool{"/repo/unrelated": true} }

// TestStaleReindexGuard_RestartDoesNotBurnAttempts is round-4 blocker 1. A
// mid-index restart orphans the repo: the request was acked and removed ~2s
// after admission, so nothing re-enqueues it and the scheduler will never index
// it. Measured before this fix: 2 attempts burned per restart (an off-by-one
// double count), so TWO restarts marked a never-broken repo Failed forever —
// and Failed is durable and blocks admission permanently.
func TestStaleReindexGuard_RestartDoesNotBurnAttempts(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/healthy"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	for restart := 0; restart < 4; restart++ {
		g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
		g.stalledGrace = 90 * time.Second
		g.activeFn = activeElsewhere // never dispatched, snapshot non-empty
		heartbeat(g, repos, stale)

		// Sit through several stalled-grace windows, then "restart".
		for i := 0; i < 60; i++ {
			clk.advance(30 * time.Second)
			heartbeat(g, repos, stale)
		}
		if g.migrationFailed(repo) {
			t.Fatalf("restart cycle %d: a never-broken repo was marked permanently unmigratable", restart)
		}
		if n := g.attemptsFor(repo); n != 0 {
			t.Fatalf("restart cycle %d: attempts = %d, want 0 — a repo the scheduler was never told about must not burn an attempt", restart, n)
		}
	}

	st, ok := readMigrationStateAt(migrationStatePath(repo))
	if ok && st.Failed {
		t.Fatalf("durable marker records Failed for a healthy repo: %+v", st)
	}
}

// TestStaleReindexGuard_ReconcileReEnqueuesRecoveredRepos is the other half of
// blocker 1: restoring the guard's slot without restoring the scheduler's queue
// leaves the repo orphaned forever. The recovered marker must produce a fresh
// request so the scheduler is actually told to do the work.
func TestStaleReindexGuard_ReconcileReEnqueuesRecoveredRepos(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/orphaned"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	if got := heartbeat(g, repos, stale); len(got) != 1 {
		t.Fatalf("initial admit = %v, want 1", got)
	}
	// The drain applies and removes the request; the index dies with the daemon.
	clk.advance(2 * time.Second)
	drainAndAck(t, repo)
	if n := countPendingReindex(t, repo); n != 0 {
		t.Fatalf("precondition: pending = %d, want 0 after the drain acked", n)
	}

	clk.advance(60 * time.Second)
	g2 := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	heartbeat(g2, repos, stale)

	if n := countPendingReindex(t, repo); n != 1 {
		t.Fatalf("pending requests after restart = %d, want 1 — a recovered slot must re-tell the scheduler", n)
	}
}

// TestStaleReindexGuard_NeverEnqueuedRepoIsNotMarkedFailed is round-4 blocker 2.
// EnqueueRefCommit drops linked worktrees of indexed primaries by design
// (sched/scheduler.go:976, #3680) without ever touching indexstate. Such a repo
// is never `active`, so the old rule forfeited it three times and published
// Failed in ~23 minutes — advising a manual reindex for a repo the scheduler
// declines to index on purpose.
func TestStaleReindexGuard_NeverEnqueuedRepoIsNotMarkedFailed(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repos := []string{"/repo/worktree", "/repo/ok"}
	stale := map[string]bool{"/repo/worktree": true, "/repo/ok": true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	// The scheduler is busy with other work but silently drops /repo/worktree.
	g.activeFn = activeElsewhere

	migrated := false
	for i := 0; i < 400; i++ {
		clk.advance(30 * time.Second)
		for _, r := range heartbeat(g, repos, stale) {
			if r == "/repo/ok" {
				migrated = true
			}
		}
	}
	if g.migrationFailed("/repo/worktree") {
		t.Error("a repo the scheduler declines to index BY DESIGN must not be reported as unmigratable")
	}
	if n := g.attemptsFor("/repo/worktree"); n != 0 {
		t.Errorf("never-enqueued repo burned %d attempts, want 0", n)
	}
	if !migrated {
		t.Error("the undroppable repo must not block the rest of the migration")
	}
}

// TestStaleReindexGuard_StalledRepoStillBurnsAttempts is the control for the
// two tests above: the genuine stall path — the repo WAS dispatched and then
// stopped without completing — must still burn attempts and still end in a
// reported give-up. Otherwise the never-enqueued fix would silently disable
// failure detection altogether.
func TestStaleReindexGuard_StalledRepoStillBurnsAttempts(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	const repo = "/repo/crashy"
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
	g.stalledGrace = 90 * time.Second
	g.retryJitter = 0
	// Dispatched and running for a while, then it disappears mid-index.
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
		// Each time it is admitted it runs briefly, then dies.
		dispatched = !dispatched
	}
	if !g.migrationFailed(repo) {
		t.Fatal("a repo that IS dispatched and then repeatedly dies mid-index must still be reported as unmigratable")
	}
	if n := g.attemptsFor(repo); n < 3 {
		t.Errorf("stalled repo attempts = %d, want >= 3", n)
	}
}

// TestStaleReindexGuard_RetryJitterSpansItsDeclaredWindow is round-4 MEDIUM 3.
// The previous implementation computed `Sum32() % retryJitter`, and Sum32 maxes
// at 4.29e9 while retryJitter is 3e11 ns — so the modulo was a no-op and the
// real spread was 0-4.295s, not 0-5m. The old test asserted only a != b and
// spread <= window, which a ONE-NANOSECOND spread satisfies. Assert the SCALE.
func TestStaleReindexGuard_RetryJitterSpansItsDeclaredWindow(t *testing.T) {
	g := newStaleReindexGuard()
	g.retryBackoff = 10 * time.Minute
	g.retryJitter = 5 * time.Minute

	var min, max time.Duration = 1<<62 - 1, 0
	buckets := map[int]bool{}
	const n = 500
	for i := 0; i < n; i++ {
		d := g.retryDelayFor(fmt.Sprintf("/repo/%04d", i))
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
		if d < g.retryBackoff || d >= g.retryBackoff+g.retryJitter {
			t.Fatalf("delay %v outside [%v, %v)", d, g.retryBackoff, g.retryBackoff+g.retryJitter)
		}
		buckets[int((d-g.retryBackoff)/(30*time.Second))] = true
	}

	spread := max - min
	// The whole point is to separate repos across HEARTBEATS (5s), so the
	// spread must be a real fraction of the declared window, not nanoseconds.
	if spread < 4*time.Minute {
		t.Errorf("jitter spread across %d repos = %v, want >= 4m of the declared 5m window (min=%v max=%v)", n, spread, min, max)
	}
	// And it must actually be spread out, not clustered at the extremes.
	if len(buckets) < 8 {
		t.Errorf("jitter occupied only %d of 10 30s buckets — not usefully distributed", len(buckets))
	}
}

// TestSchedulerActiveRepos_ReadsIndexstate covers the production activeFn,
// which every guard test stubs out — round-4 LOW 4 noted it was the one
// coupling with no end-to-end coverage.
func TestSchedulerActiveRepos_ReadsIndexstate(t *testing.T) {
	t.Cleanup(func() { indexstate.SetRepoStates(nil) })

	indexstate.SetRepoStates(nil)
	if got := schedulerActiveRepos(); got != nil {
		t.Errorf("empty snapshot must report nil (UNKNOWN), got %v", got)
	}

	indexstate.SetRepoStates([]indexstate.RepoState{
		{Path: "/repo/queued", State: indexstate.StateQueued},
		{Path: "/repo/indexing", State: indexstate.StateIndexing},
		{Path: "/repo/dirty", State: indexstate.StateDirty},
		{Path: "/repo/current", State: indexstate.StateCurrent},
	})
	got := schedulerActiveRepos()
	for _, want := range []string{"/repo/queued", "/repo/indexing", "/repo/dirty"} {
		if !got[want] {
			t.Errorf("%s should count as active", want)
		}
	}
	if got["/repo/current"] {
		t.Error("a current repo is not active work")
	}
}

// TestStaleReindexGuard_MarkerForDeletedRepoIsDropped is round-4 LOW 5: a repo
// unregistered after admission never reaches maybeEnqueue(required=false), so
// its marker leaked and a later re-registration resurrected stale attempts.
func TestStaleReindexGuard_MarkerForDeletedRepoIsDropped(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	gone := filepath.Join(t.TempDir(), "removed-repo")
	if err := writeMigrationState(gone, migrationState{Attempts: 2}); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	g := newTestGuard(clk, 2, 45*time.Second, 15*time.Minute)
	g.maybeEnqueue("/repo/other", true, staleFingerprint(1, "stale"), nil)

	if _, ok := readMigrationStateAt(migrationStatePath(gone)); ok {
		t.Error("marker for a repo that no longer exists on disk must be dropped, not carried forward")
	}
	if n := g.attemptsFor(gone); n != 0 {
		t.Errorf("stale attempts resurrected for a deleted repo: %d", n)
	}
}

// TestStaleReindexGuard_AttemptsSurviveRestartWithoutInflating closes the gap
// mutation testing exposed: with never-dispatched repos no longer penalised,
// none of the restart tests traverse the penalty path any more, so restoring
// the admission off-by-one (writing Attempts+1, which reconcile then loads and
// the next forfeit increments again) survived the whole suite. The double count
// is still reachable for a GENUINELY stalled repo across a restart, and it
// still costs it its cap: the durable count must equal the real number of
// failures, no more.
func TestStaleReindexGuard_AttemptsSurviveRestartWithoutInflating(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvRoot, t.TempDir())
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}

	repo := realRepoDirs(t, "stally")[0]
	repos := []string{repo}
	stale := map[string]bool{repo: true}

	dispatched := true
	mk := func() *staleReindexGuard {
		g := newTestGuard(clk, 1, 45*time.Second, 15*time.Minute)
		g.stalledGrace = 90 * time.Second
		g.retryJitter = 0
		g.retryBackoff = time.Minute
		g.activeFn = func() map[string]bool {
			if dispatched {
				return map[string]bool{repo: true, "/repo/unrelated": true}
			}
			return activeElsewhere()
		}
		return g
	}

	// One real failure: dispatched, then it dies mid-index and stalls out.
	g := mk()
	heartbeat(g, repos, stale)
	clk.advance(30 * time.Second)
	heartbeat(g, repos, stale) // observed active
	dispatched = false
	clk.advance(2 * time.Minute)
	heartbeat(g, repos, stale) // stalled -> forfeit, one attempt

	if n := g.attemptsFor(repo); n != 1 {
		t.Fatalf("after one genuine stall, attempts = %d, want 1", n)
	}

	// It is admitted again, and the daemon restarts while that attempt is live.
	clk.advance(2 * time.Minute)
	dispatched = true
	heartbeat(g, repos, stale)
	clk.advance(30 * time.Second)

	g2 := mk()
	heartbeat(g2, repos, stale)
	if n := g2.attemptsFor(repo); n != 1 {
		t.Fatalf("after a restart mid-attempt, attempts = %d, want 1 — the durable count must record real failures, not inflate per restart", n)
	}

	st, ok := readMigrationStateAt(migrationStatePath(repo))
	if !ok {
		t.Fatal("expected a durable marker for an admitted repo")
	}
	if st.Attempts != 1 {
		t.Errorf("durable marker Attempts = %d, want 1", st.Attempts)
	}
}
