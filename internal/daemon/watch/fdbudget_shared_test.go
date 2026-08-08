package watch

import "testing"

// TestReserveWatchFDs_ChargesTheSameLedgerAsSubscriptions is the #6233 claim in
// one assertion: a descriptor reserved by an OUT-OF-PACKAGE watcher (the
// worktree .git/worktrees watch) must show up in the same ledger that
// FDBudgetStats reports for subscriptions. If the exported entry points get
// their own budget instead of the process-wide one, the two consumers are
// accounted separately and the reported headroom is fiction again.
func TestReserveWatchFDs_ChargesTheSameLedgerAsSubscriptions(t *testing.T) {
	w, err := NewWatcherConfig(Config{}, func(string, bool) {}, nil)
	if err != nil {
		t.Fatalf("NewWatcherConfig: %v", err)
	}
	defer w.Stop()

	before, _, _, _ := w.FDBudgetStats()

	const n = 7
	if !ReserveWatchFDs(n) {
		t.Fatalf("ReserveWatchFDs(%d) refused on an otherwise idle process", n)
	}
	during, _, _, _ := w.FDBudgetStats()
	if during-before != n {
		t.Fatalf("shared ledger did not observe the reservation: used went %d -> %d, want +%d",
			before, during, n)
	}

	ReleaseWatchFDs(n)
	after, _, _, _ := w.FDBudgetStats()
	if after != before {
		t.Fatalf("release did not return the descriptors: used %d, want %d", after, before)
	}
}

// TestDirWatchFDCost_ScalesWithDirectoryEntriesOnDarwin pins the arithmetic to
// the platform cost model rather than to a literal: on the per-file platform a
// directory holding entries entries costs more than an empty one; where the
// per-file term does not exist the cost must NOT grow with entries.
func TestDirWatchFDCost_ScalesWithDirectoryEntriesOnDarwin(t *testing.T) {
	empty := DirWatchFDCost(0)
	full := DirWatchFDCost(10)
	if empty < 1 {
		t.Fatalf("DirWatchFDCost(0) = %d, want at least the directory's own descriptor", empty)
	}
	if fdDescriptorPerFile {
		if full != empty+10 {
			t.Fatalf("DirWatchFDCost(10) = %d, want %d (one descriptor per entry)", full, empty+10)
		}
	} else if full != empty {
		t.Fatalf("DirWatchFDCost(10) = %d, want %d (no per-entry term on this platform)", full, empty)
	}
}
