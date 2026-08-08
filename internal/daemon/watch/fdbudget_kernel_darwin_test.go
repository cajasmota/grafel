//go:build darwin

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// #6268 — the kernel oracle.
//
// Every other test in this package asserts grafel's ledger against grafel's own
// arithmetic, restated. That can only show the fix is self-consistent. This
// file asserts it against the KERNEL: /dev/fd on darwin lists the calling
// process's open descriptors, so counting its entries before and after an
// operation measures what fsnotify actually opened or closed, with no model in
// between.
//
// darwin-only because /dev/fd is where this process's descriptors are
// enumerable and because the per-descriptor kqueue backend is the platform the
// budget exists for (backend_kqueue.go carries `//go:build ... || darwin`).
// ---------------------------------------------------------------------------

// openFDs counts this process's open descriptors.
//
// Readdirnames, not os.ReadDir: ReadDir lstats every entry, and a kqueue
// descriptor — which is exactly what fsnotify's backend holds — fails fstatat
// with EBADF, so ReadDir returns an error instead of a count. Names are all
// this needs.
//
// The directory handle itself is one open descriptor while the read is in
// flight, so every reading is inflated by the same constant one. Harmless:
// only differences between readings are ever used.
func openFDs(t *testing.T) int {
	t.Helper()
	f, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatalf("open /dev/fd: %v", err)
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatalf("read /dev/fd: %v", err)
	}
	return len(names)
}

// TestOpenFDsOracleIsSound is the premise for this file: if /dev/fd did not
// move when a descriptor is opened and closed, every comparison below would be
// vacuously satisfied by a ledger that never moved either.
func TestOpenFDsOracleIsSound(t *testing.T) {
	before := openFDs(t)
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	during := openFDs(t)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	after := openFDs(t)
	if during != before+1 {
		t.Fatalf("/dev/fd did not see an open: %d -> %d", before, during)
	}
	if after != before {
		t.Fatalf("/dev/fd did not see the close: %d -> %d", before, after)
	}
}

// settledFDs waits until the process descriptor count stops moving, then
// returns it. Needed because a previous test's Watcher.Stop closes fsnotify,
// whose readEvents goroutine closes the kqueue and closepipe descriptors in a
// deferred block AFTER Close returns — so descriptors keep disappearing into
// the next test and a raw baseline read would be measured against a moving
// floor.
func settledFDs(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	last, stable := -1, 0
	for time.Now().Before(deadline) {
		n := openFDs(t)
		if n == last {
			if stable++; stable >= 10 {
				return n
			}
		} else {
			last, stable = n, 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process descriptor count never settled (last read %d)", last)
	return 0
}

// waitFDs polls the kernel count until it reads want, or fails. The wait is
// needed because fsnotify opens and closes on its readEvents goroutine.
// The count must HOLD for ledgerStableReads consecutive polls: a process still
// opening descriptors passes through the expected value on its way past it, and
// returning on the first match would call that success.
func waitFDs(t *testing.T, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	got, held := -1, 0
	for time.Now().Before(deadline) {
		got = openFDs(t)
		if got == want {
			if held++; held >= ledgerStableReads {
				return
			}
		} else {
			held = 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: kernel read %d open descriptors (held %d of %d polls), want a settled %d",
		what, got, held, ledgerStableReads, want)
}

// TestSubscriptionLedgerEqualsKernelDescriptors is the claim the whole change
// is for: after subscribing a repo with a pruned subdirectory, the number of
// descriptors the process actually gained equals the number the ledger charged.
// Before the fix the ledger was one short here — the pruned node_modules
// directory, opened by root's watchDirectoryFiles and charged by nobody.
func TestSubscriptionLedgerEqualsKernelDescriptors(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, 10000)
	// Settle before the baseline, so neither this watcher's own kqueue +
	// closepipe descriptors nor a previous test's asynchronous teardown is
	// counted as subscription cost.
	baseFDs := settledFDs(t)

	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	charged, _ := w.fdb.snapshot()
	gained := openFDs(t) - baseFDs

	if gained == 0 {
		t.Fatal("premise broken: subscribing opened no descriptors at all")
	}
	if charged != gained {
		t.Fatalf("ledger charged %d descriptors, kernel opened %d", charged, gained)
	}
}

// TestEventChurnLedgerTracksKernelDescriptors is (A) and (B) against the
// kernel. A file appearing in a watched directory makes fsnotify open a
// descriptor grafel never asked for; deleting it makes fsnotify close one
// without telling grafel. Both must show up in the ledger, and the ledger must
// return to where it started when the kernel does.
func TestEventChurnLedgerTracksKernelDescriptors(t *testing.T) {
	root := makePrunedTree(t)
	w := newBudgetedWatcher(t, 10000)
	if _, err := w.AddRepo(root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	baseFDs := settledFDs(t)
	baseLedger, _ := w.fdb.snapshot()

	// Kept under defaultChurnThreshold/2 (quarantine.go:68) so the quarantine
	// tracker does not create <repo>/.grafel mid-measurement; that directory
	// would be a genuine extra open, but not the one under test.
	const n = 12
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(root, "src", "k"+string(rune('a'+i))+".go")
		if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		paths = append(paths, p)
	}
	waitFDs(t, baseFDs+n, "n files created in a watched directory")
	if got, _ := w.fdb.snapshot(); got != baseLedger+n {
		t.Fatalf("kernel opened %d descriptors for the new files, ledger recorded %d", n, got-baseLedger)
	}

	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}
	waitFDs(t, baseFDs, "the same files deleted again")
	if got, _ := w.fdb.snapshot(); got != baseLedger {
		t.Fatalf("kernel returned to %d open descriptors, ledger stayed at %d (baseline %d)",
			baseFDs, got, baseLedger)
	}
	if _, err := os.Stat(filepath.Join(root, ".grafel")); err == nil {
		t.Fatal("premise broken: the churn tripped the quarantine tracker and created .grafel")
	}
}
