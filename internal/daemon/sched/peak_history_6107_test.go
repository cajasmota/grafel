package sched

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// #6107 review follow-ups — what may be PERSISTED, and what a persisted value
// then does to admission.
//
// RSSHistory is not a log: Record is a moving max that is read back by
// predictedFor on every admission decision, forever. Two properties have to
// hold or the series is worse than no series at all:
//
//  1. Only ONE kind of measurement may enter it. An absolute child high-water
//     RSS and a whole-daemon delta are different numbers against different
//     baselines; merging them into one field makes the maximum meaningless.
//  2. A single sample must not be able to pin the series permanently. Record
//     over-estimates on purpose, but an un-decaying, never-expiring max means
//     one outlier governs admission for the life of the file.
// ---------------------------------------------------------------------------

// TestInProcessRunDoesNotFeedRSSHistory pins property (1) from the side that
// can actually cause harm. The in-process index path measures the DAEMON, not
// the repo: with Workers>1, a concurrent job, or an MCP query faulting in
// graph.fb, the growth attributed to "this repo" is whatever else happened to
// be running. Because Record is a permanent moving max, one such sample poisons
// the repo's admission input for good. Nothing measured against the daemon
// baseline may be persisted.
func TestInProcessRunDoesNotFeedRSSHistory(t *testing.T) {
	const repo = "/repo-inprocess-nohistory-6107"
	path := filepath.Join(t.TempDir(), "rss-history.json")
	h := LoadRSSHistory(path)

	var buf syncBuf
	s := newPeakTestSchedulerWithHistory(&buf, h, func(context.Context, string, string) error {
		// The in-process arm: real work, real allocation, NO child. Whatever
		// the daemon's RSS does here is not this repo's number.
		blocks := touchMB(testTouchMBs)
		runtime.KeepAlive(blocks)
		return nil
	})

	s.runIndex(jobToken{repoPath: repo, ref: "main", commit: "c0"})

	if got := h.Predict(repo); got != 0 {
		t.Fatalf("RSSHistory.Predict(%s)=%d after an in-process run; a daemon-baselined "+
			"sample must never enter the series a child_maxrss prediction is read from", repo, got)
	}
	if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), repo) {
		t.Fatalf("in-process run persisted an entry for %s:\n%s", repo, b)
	}
}

// TestChildPeakFeedsRSSHistory is the positive control for the gate above: the
// child high-water mark IS the measurement the predictor always wanted, and it
// must still get through. Without this, "gate the record" could be satisfied by
// recording nothing at all.
func TestChildPeakFeedsRSSHistory(t *testing.T) {
	const repo = "/repo-childpeak-history-6107"
	const childMB int64 = 812
	path := filepath.Join(t.TempDir(), "rss-history.json")
	h := LoadRSSHistory(path)

	var buf syncBuf
	s := newPeakTestSchedulerWithHistory(&buf, h, func(context.Context, string, string) error {
		recordChildPeakRSSMB(repo, childMB)
		return nil
	})

	s.runIndex(jobToken{repoPath: repo, ref: "main", commit: "c0"})

	if got := h.Predict(repo); got != childMB {
		t.Fatalf("RSSHistory.Predict(%s)=%d; want the child's %d MiB high-water RSS", repo, got, childMB)
	}
}

// TestRecordedChildPeakDrivesAdmissionSolo closes the coverage gap the review
// found: every other admission test injects a prediction directly, so nothing
// exercised runIndex -> History.Record -> predictedFor -> tryAdmit. That whole
// chain is what turns a measurement into a scheduling decision.
//
// It also PINS the consequence, which is a throughput cliff and not an
// abstraction: once a repo's recorded peak exceeds BudgetMB, predictedFor
// returns a figure no ledger state can accommodate, so tryAdmit will only ever
// release it through the solo `admit_oversize` path — serialised against every
// other repo. That is the correct OOM-safe behaviour, but it must be a KNOWN
// property with a test on it, not an emergent surprise (epic #5954 exists to
// raise concurrency on exactly this kind of host).
func TestRecordedChildPeakDrivesAdmissionSolo(t *testing.T) {
	const big = "/repo-oversize-6107"
	const small = "/repo-small-6107"
	const budgetMB int64 = 2048
	// Over budget: the shape a Darwin ru_maxrss reading of a sawtooth
	// extractor workload produces on a 16 GiB host.
	const bigPeakMB int64 = 2100

	path := filepath.Join(t.TempDir(), "rss-history.json")
	h := LoadRSSHistory(path)

	var buf syncBuf
	s := newPeakTestSchedulerWithHistory(&buf, h, func(_ context.Context, repo string, _ string) error {
		if repo == big {
			recordChildPeakRSSMB(big, bigPeakMB)
		}
		return nil
	})
	s.cfg.BudgetMB = budgetMB
	s.cfg.Predict = func(string) int64 { return 100 }

	// One completed run of the big repo is all it takes.
	s.runIndex(jobToken{repoPath: big, ref: "main", commit: "c0"})

	if got := s.predictedFor(big); got != bigPeakMB {
		t.Fatalf("predictedFor(%s)=%d after one recorded run; want the recorded %d "+
			"(history must beat the source-bytes predictor)", big, got, bigPeakMB)
	}
	if s.predictedFor(big) <= budgetMB {
		t.Fatalf("test premise broken: %d must exceed budget %d", s.predictedFor(big), budgetMB)
	}

	// With another job already in flight, the oversize repo cannot be admitted:
	// it waits for an empty ledger. This is head-of-line blocking by design.
	s.mu.Lock()
	s.inflight[small] = 100
	s.usedMB = 100
	s.pendingQ = []string{big}
	s.pendingRefs[big] = "main"
	s.pendingCommits[big] = "c0"
	s.mu.Unlock()

	s.tryAdmit()

	s.mu.Lock()
	stillQueued := len(s.pendingQ)
	s.mu.Unlock()
	if stillQueued != 1 {
		t.Fatalf("oversize repo was admitted alongside an in-flight job (pendingQ=%d); "+
			"a recorded peak above BudgetMB must gate on an empty ledger", stillQueued)
	}
	if !strings.Contains(buf.String(), "admit_defer") && !hasEvent(s, "admit_defer") {
		t.Logf("note: no admit_defer event observed; log=%s", buf.String())
	}

	// Ledger empty: the solo path is the ONLY way it ever runs again.
	s.mu.Lock()
	delete(s.inflight, small)
	s.usedMB = 0
	s.mu.Unlock()

	s.tryAdmit()

	select {
	case tok := <-s.jobs:
		if tok.repoPath != big {
			t.Fatalf("admitted %s; want %s", tok.repoPath, big)
		}
		if tok.predictedMB != bigPeakMB {
			t.Fatalf("admitted with predictedMB=%d; want the recorded %d", tok.predictedMB, bigPeakMB)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("oversize repo was never admitted even with an empty ledger — a single " +
			"recorded sample above budget has starved it completely")
	}
	if !hasEvent(s, "admit_oversize") {
		t.Fatalf("expected an admit_oversize event pinning the solo path")
	}
}

// TestRSSHistoryRelaxesTowardRecentPeaks pins property (2). Record is a moving
// max so a spike still sets the budget immediately, but a max with no decay
// means one outlier — a contended host, a pathological commit, a
// MADV_FREE-inflated Darwin reading — governs admission until someone deletes
// the file by hand. Later, smaller measurements must be able to walk it back.
func TestRSSHistoryRelaxesTowardRecentPeaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rss-history.json")
	h := LoadRSSHistory(path)
	const repo = "/repo-decay-6107"

	h.Record(repo, 400)
	if got := h.Predict(repo); got != 400 {
		t.Fatalf("Predict=%d after first record; want 400", got)
	}
	// A spike must take effect at once — under-estimating is the dangerous
	// direction.
	h.Record(repo, 2100)
	if got := h.Predict(repo); got != 2100 {
		t.Fatalf("Predict=%d after a spike; want it adopted immediately (2100)", got)
	}
	// ...and must then decay as reality disagrees with it.
	prev := h.Predict(repo)
	for i := 0; i < 8; i++ {
		h.Record(repo, 400)
		got := h.Predict(repo)
		if got > prev {
			t.Fatalf("Predict rose to %d from %d while observing 400s", got, prev)
		}
		if got < 400 {
			t.Fatalf("Predict fell to %d, below every observation (400) — decay must "+
				"never under-estimate past the evidence", got)
		}
		prev = got
	}
	if prev >= 2100 {
		t.Fatalf("Predict is still %d after 8 consecutive 400 MiB runs; one sample is "+
			"permanently pinning the series", prev)
	}
	if prev > 800 {
		t.Fatalf("Predict=%d after 8 consecutive 400 MiB runs; decay is too slow to be "+
			"a guard at all", prev)
	}
}

// TestRSSHistoryIgnoresExpiredEntries pins the other half of (2). RSSHistoryEntry
// has always carried LastIndex and nothing has ever read it, so there is no
// expiry path at all: an entry written by a build from months ago, against code
// that no longer exists, is still consulted ahead of the live predictor. Beyond
// a TTL the honest answer is "no history", which falls back to PredictRSS.
func TestRSSHistoryIgnoresExpiredEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rss-history.json")
	stale := time.Now().UTC().Add(-2 * rssHistoryTTL)
	fresh := time.Now().UTC().Add(-time.Hour)
	body := `{
  "/repo-stale": {"peak_rss_mb": 1593, "last_index": "` + stale.Format(time.RFC3339Nano) + `"},
  "/repo-fresh": {"peak_rss_mb": 1593, "last_index": "` + fresh.Format(time.RFC3339Nano) + `"}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h := LoadRSSHistory(path)
	if got := h.Predict("/repo-fresh"); got != 1593 {
		t.Fatalf("Predict(/repo-fresh)=%d; a recent measurement must still be used", got)
	}
	if got := h.Predict("/repo-stale"); got != 0 {
		t.Fatalf("Predict(/repo-stale)=%d; an entry older than the %s TTL must fall back "+
			"to the live predictor, not govern admission indefinitely", got, rssHistoryTTL)
	}
}
