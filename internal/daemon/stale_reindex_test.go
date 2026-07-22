package daemon

// stale_reindex_test.go — #5907 FIX 2: proves the loop-guard on the auto-reindex
// action arm. The engine recomputes ReindexRequired on EVERY heartbeat, so the
// load-bearing property is that a stale-format repo enqueues EXACTLY ONE reindex
// request across many heartbeats (never a storm), a current repo enqueues none,
// and the guard self-clears once the graph is current again.

import (
	"testing"

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
