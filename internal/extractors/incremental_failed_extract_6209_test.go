package extractors_test

// #6209 on the path the daemon actually takes.
//
// TryIncremental is Path A — the scheduler's per-tick incremental pass
// (cmd/grafel/daemon.go). The retry for a file whose extraction failed has to
// survive TWO independent gates here, and the first fix only cleared one:
//
//	1. diff.FilterWithGit (incremental.go, Step 2) — cleared by the retry-due
//	   union, otherwise git's "nothing changed" verdict ends it.
//	2. The Step-3 AST-hash gate — prev.SHA256 != stamp.ContentHash, over the
//	   SAME hex SHA-256 of the SAME raw bytes. A file whose extraction failed
//	   has unchanged bytes BY CONSTRUCTION, so this gate drops it straight back
//	   out, reallyChanged empties, and the pass returns Done=true without
//	   writing a manifest. The failure count is then pinned forever: the budget
//	   never spends, the file is never retried, and the scheduler does not fall
//	   back to a full index because Done was true.
//
// The cmd/grafel tests for #6209 drive Index(..., WithIncremental) — Path B, the
// CLI. Neither of them touches this function, which is how gate 2 survived them.
//
// Assertions are on TryIncremental's own Result, on manifest contents and on the
// logger's decision lines. Never on timing.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// markFailed sets rel's consecutive-failure count in the seeded manifest and
// saves it, WITHOUT touching the working tree. That combination is the whole
// point: the bytes are identical to what the manifest already records, so every
// hash-based gate in the pipeline must say "unchanged", and only the failure
// count can put the file back into the changed set.
func markFailed(t *testing.T, repo, stateDir, rel string, n int) {
	t.Helper()
	m := diff.LoadManifest(stateDir)
	e, ok := m.Files[rel]
	if !ok {
		t.Fatalf("fixture is inert: %s has no manifest entry to mark", rel)
	}
	e.Failures = n
	m.Files[rel] = e
	if err := diff.SaveManifest(stateDir, repo, m); err != nil {
		t.Fatal(err)
	}
}

// runPathA drives one real TryIncremental pass and returns its result plus the
// logger output, which is where the pass records WHICH branch it took.
func runPathA(t *testing.T, repo, stateDir string) (extractors.Result, string) {
	t.Helper()
	var logs strings.Builder
	logger := log.New(&logs, "", 0)
	res := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	return res, logs.String()
}

// TestIncremental_PathA_RetriesFailedExtractionDespiteUnchangedHash is the
// defect, on the daemon's primary path.
//
// Nothing in the working tree changes. The only thing that says this file needs
// work is the failure count the previous pass recorded, and every byte-level
// gate between here and the extractor will disagree with it.
func TestIncremental_PathA_RetriesFailedExtractionDespiteUnchangedHash(t *testing.T) {
	repo, stateDir, _ := incrementalStampFixture(t, 3)
	const victim = "victim.go"
	markFailed(t, repo, stateDir, victim, 1)

	res, logs := runPathA(t, repo, stateDir)

	if strings.Contains(logs, "had unchanged AST hash") {
		t.Fatalf("the Step-3 AST-hash gate dropped the retry. A failed extraction does not edit the "+
			"file, so its content hash MATCHES the stamp by construction — the gate can only ever "+
			"say \"unchanged\" and the file is never retried on the daemon's path (#6209).\nlogs:\n%s",
			logs)
	}
	if !res.Done {
		t.Fatalf("the pass fell back (%q) instead of re-extracting the retry-due file; the fixture's "+
			"file extracts fine, so a retry should simply succeed\nlogs:\n%s", res.FallbackReason, logs)
	}
	if res.ChangedFiles != 1 {
		t.Fatalf("the pass re-extracted %d file(s), want exactly 1 (%s). 0 means the retry never "+
			"reached the extractor and the failure count is pinned forever; more than 1 means the "+
			"retry widened into something close to a full reindex.\nlogs:\n%s",
			res.ChangedFiles, victim, logs)
	}

	// The retry SUCCEEDED, so the count must be gone — that is the budget being
	// spent productively rather than pinned.
	if got := diff.LoadManifest(stateDir).Files[victim].Failures; got != 0 {
		t.Fatalf("%s still carries %d failure(s) after a successful re-extraction on Path A, want 0. "+
			"A count that never clears is a count that never bounds anything (#6209)", victim, got)
	}
}

// TestIncremental_PathA_ExhaustedBudgetIsNotRetried is the other half of the
// bound, on this path: once the count is spent the file must stop forcing work,
// exactly as on Path B.
//
// It doubles as the reachability proof for the totalChanged == 0 branch — see
// TestIncremental_PathA_ZeroChangeBranchStaysReachable, which asserts the branch
// by its log line rather than by its effect.
func TestIncremental_PathA_ExhaustedBudgetIsNotRetried(t *testing.T) {
	repo, stateDir, _ := incrementalStampFixture(t, 3)
	const victim = "victim.go"
	markFailed(t, repo, stateDir, victim, 1+diff.MaxExtractRetries)

	res, logs := runPathA(t, repo, stateDir)

	if !res.Done {
		t.Fatalf("pass fell back (%q) with a spent budget and a clean tree\nlogs:\n%s",
			res.FallbackReason, logs)
	}
	if res.ChangedFiles != 0 {
		t.Fatalf("the pass re-extracted %d file(s) after %d consecutive deterministic failures, want 0. "+
			"On the daemon's path an unbounded retry is a per-tick parse tax forever (#6209)\nlogs:\n%s",
			res.ChangedFiles, 1+diff.MaxExtractRetries, logs)
	}
	if got := diff.LoadManifest(stateDir).Files[victim].Failures; got != 1+diff.MaxExtractRetries {
		t.Fatalf("the failure count for %s moved to %d on a pass that never re-extracted it, want it "+
			"pinned at %d", victim, got, 1+diff.MaxExtractRetries)
	}
}

// TestIncremental_PathA_ZeroChangeBranchStaysReachable guards the collateral
// damage the retry union could do.
//
// The union puts a retry-due file into the changed set, so totalChanged >= 1 and
// the zero-change branch is skipped. That branch carries two things worth
// keeping: the #5710 absent-graph guard, and the full-repo UpdateManifest sweep
// that HEALS entries a #6201 scoped reject left stale. Suppressing it must be
// BOUNDED — which it is, because a retry either succeeds (count cleared) or
// fails (count incremented by the fallback full index) and dies at
// MaxExtractRetries.
//
// The branch is asserted by the log line it does NOT emit: reaching the AST-hash
// gate at all means the changed set was non-empty.
func TestIncremental_PathA_ZeroChangeBranchStaysReachable(t *testing.T) {
	repo, stateDir, _ := incrementalStampFixture(t, 3)

	// No failure marks anywhere: the ordinary steady state.
	res, logs := runPathA(t, repo, stateDir)
	if !res.Done || res.ChangedFiles != 0 {
		t.Fatalf("fixture is inert: a clean tree should be a zero-change no-op, got Done=%v changed=%d "+
			"fallback=%q\nlogs:\n%s", res.Done, res.ChangedFiles, res.FallbackReason, logs)
	}
	if strings.Contains(logs, "had unchanged AST hash") {
		t.Fatalf("a clean tree reached the AST-hash gate, so the changed set was NOT empty and the "+
			"zero-change branch — with the #5710 absent-graph guard and the healing manifest sweep — "+
			"was skipped. The retry union must add nothing when nothing has failed.\nlogs:\n%s", logs)
	}

	// And once a budget is spent, the branch is reachable again even though the
	// count is still on the entry.
	markFailed(t, repo, stateDir, "victim.go", 1+diff.MaxExtractRetries)
	res, logs = runPathA(t, repo, stateDir)
	if !res.Done || res.ChangedFiles != 0 {
		t.Fatalf("spent budget did not return to a zero-change no-op: Done=%v changed=%d fallback=%q\nlogs:\n%s",
			res.Done, res.ChangedFiles, res.FallbackReason, logs)
	}
	if strings.Contains(logs, "had unchanged AST hash") {
		t.Fatalf("a spent budget still forced files into the changed set, so the zero-change branch is "+
			"permanently unreachable for the life of the entry\nlogs:\n%s", logs)
	}
}

// TestIncremental_PathA_RetryDoesNotReportSuccessOverAnAbsentGraph is the #5710
// protection under the union.
//
// The absent-graph guard lives INSIDE the zero-change branch, which a retry-due
// file skips. That would be a real regression if it were the only thing standing
// between an absent graph.fb and a Done=true — so pin the other one: Step 4
// cannot load a graph that is not there and falls back, which is the same
// outcome the guard produces. Silent success over an empty graph stays
// impossible.
func TestIncremental_PathA_RetryDoesNotReportSuccessOverAnAbsentGraph(t *testing.T) {
	repo, stateDir, _ := incrementalStampFixture(t, 3)
	markFailed(t, repo, stateDir, "victim.go", 1)

	if err := os.Remove(filepath.Join(stateDir, "graph.fb")); err != nil {
		t.Fatalf("fixture is inert: no graph.fb to remove: %v", err)
	}

	res, logs := runPathA(t, repo, stateDir)
	if res.Done {
		t.Fatalf("the pass reported SUCCESS over an absent graph.fb with a non-empty tree. The retry "+
			"union skips the zero-change branch where the #5710 guard lives, so the fallback here has "+
			"to come from somewhere else — and it must come from somewhere (#5710/#6209)\nlogs:\n%s", logs)
	}
	if res.FallbackReason == "" {
		t.Fatal("the pass declined without a reason, so the scheduler cannot log why it is doing a full index")
	}
}
