package main

import (
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/process"
)

// TestIndexParseCoreBudget_Is25PercentOfMachine pins the indexing core budget
// policy: 25% of machine capacity, minimum 1.
//
// The rule it replaces was a static "never more than 3 cores", which protected
// a small laptop but pinned a large machine to the same 3. Proportional keeps
// the "must not make the box feel busy" property while letting bigger machines
// index faster; on a 12-core host it evaluates to 3, i.e. the old rule exactly.
func TestIndexParseCoreBudget_Is25PercentOfMachine(t *testing.T) {
	got := indexParseCoreBudget()
	want := runtime.NumCPU() / 4
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Errorf("indexParseCoreBudget() = %d, want max(1, NumCPU/4) = %d (NumCPU=%d)",
			got, want, runtime.NumCPU())
	}
	if got < 1 {
		t.Errorf("budget = %d — must never be < 1, and must never be 0, "+
			"since parsegate.go treats cap<=0 as UNBOUNDED", got)
	}
	if n := runtime.NumCPU(); n >= 4 && got > n/4 {
		t.Errorf("budget %d exceeds 25%% of %d cores", got, n)
	}
	// #5960: there must be exactly ONE definition of the 25% policy in the
	// tree. This call site is a named wrapper over the canonical helper; if
	// someone re-inlines a private copy here, the two can silently diverge.
	if want := process.IndexCoreBudget(); got != want {
		t.Errorf("indexParseCoreBudget() = %d but process.IndexCoreBudget() = %d — "+
			"the 25%% policy has been duplicated instead of shared", got, want)
	}
}

// TestEnsureParseConcurrencyDefault_InstallsCapOffDaemon pins the #5954
// blocker fix: the non-daemon background index path must install an in-process
// parse concurrency cap.
//
// Before this, indexstate.SetParseConcurrency had exactly ONE production
// caller (the daemon), and parsegate treats cap<=0 as unbounded — so
// `grafel index-internal`, the child the scheduler/rebuild path fork-execs,
// ran with AcquireParseSlot as a no-op. With parseMu gone that would have
// allowed i.workers (8) concurrent ts_parser_parse on 8 OS threads, with
// GRAFEL_EXTRACT_GOMAXPROCS offering no protection because GOMAXPROCS does
// not bound cgo.
func TestEnsureParseConcurrencyDefault_InstallsCapOffDaemon(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	indexstate.SetParseConcurrency(0) // the non-daemon starting state
	if got := indexstate.ParseConcurrencyCap(); got != 0 {
		t.Fatalf("precondition: cap = %d, want 0 (unbounded)", got)
	}

	got, installed := ensureParseConcurrencyDefault(false)
	want := indexParseCoreBudget()
	if !installed {
		t.Error("installed = false — the background index path must install a cap when none exists")
	}
	if got != want {
		t.Errorf("returned cap = %d, want the core budget %d", got, want)
	}
	if installed := indexstate.ParseConcurrencyCap(); installed != want {
		t.Errorf("installed cap = %d, want %d — the parse gate is still unbounded, "+
			"so concurrent ts_parser_parse is uncapped on the non-daemon path", installed, want)
	}
	if installed := indexstate.ParseConcurrencyCap(); installed <= 0 {
		t.Error("cap <= 0 means UNBOUNDED in parsegate.go — the gate must actually bind")
	}
}

// TestEnsureParseConcurrencyDefault_InteractiveIsExempt pins the explicit
// product decision that a foreground, user-awaited rebuild may use the whole
// machine. Capping it would only make the human wait; the budget exists to
// protect a developer from work that runs UNASKED.
func TestEnsureParseConcurrencyDefault_InteractiveIsExempt(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	indexstate.SetParseConcurrency(0)
	if got, installed := ensureParseConcurrencyDefault(true); got != 0 || installed {
		t.Errorf("interactive cap = %d installed=%v, want 0/false (uncapped)", got, installed)
	}
	if installed := indexstate.ParseConcurrencyCap(); installed != 0 {
		t.Errorf("interactive run installed cap %d — foreground rebuilds must stay uncapped", installed)
	}
}

// TestEnsureParseConcurrencyDefault_NeverOverridesExisting pins the other half:
// the daemon installs its own cap at startup (and may deliberately tighten it
// via GRAFEL_DAEMON_GOMAXPROCS). An in-process index inside the daemon must not
// silently widen that back out — not even an interactive one.
func TestEnsureParseConcurrencyDefault_NeverOverridesExisting(t *testing.T) {
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	for _, interactive := range []bool{false, true} {
		indexstate.SetParseConcurrency(2) // pretend the daemon capped us at 2
		got, installed := ensureParseConcurrencyDefault(interactive)
		if got != 2 {
			t.Errorf("interactive=%v: returned cap = %d, want the pre-existing 2", interactive, got)
		}
		if installed {
			t.Errorf("interactive=%v: reported installing a cap when one already existed", interactive)
		}
		if installed := indexstate.ParseConcurrencyCap(); installed != 2 {
			t.Errorf("interactive=%v: cap = %d, want 2 — an existing (daemon) cap must never be changed",
				interactive, installed)
		}
	}
}
