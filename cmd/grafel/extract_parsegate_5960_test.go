package main

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/grafel/internal/indexstate"
)

// TestRunExtractSubprocessInstallsParseGate is the test that actually protects
// the wiring: it drives runExtractSubprocess and asserts the process-wide parse
// gate is no longer unbounded afterwards. Delete the
// `indexstate.SetParseConcurrency(...)` call in runExtractSubprocess and this
// fails (the cap stays 0 = unbounded) — which is the point, since the property
// tests below cannot detect that deletion.
//
// The gate is installed BEFORE any extraction work, so the run is driven with a
// deliberately unreadable batch path: the wiring executes, extract.Run then
// fails fast, and the test needs no repo fixture and parses nothing.
func TestRunExtractSubprocessInstallsParseGate(t *testing.T) {
	// Start from "unbounded" so a pass cannot be inherited from another test.
	indexstate.SetParseConcurrency(0)
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })
	if got := indexstate.ParseConcurrencyCap(); got != 0 {
		t.Fatalf("precondition: cap = %d, want 0 (unbounded)", got)
	}

	dir := t.TempDir()
	// Valid flags (so the required-arg guard passes) but a batch file that does
	// not exist, so extract.Run errors out right after the gate is installed.
	err := runExtractSubprocess([]string{
		"--repo", dir,
		"--batch", filepath.Join(dir, "does-not-exist.batch"),
		"--batch-id", "parsegate-5960",
	})
	if err == nil {
		t.Fatal("expected runExtractSubprocess to fail on a missing batch file")
	}

	got := indexstate.ParseConcurrencyCap()
	if got == 0 {
		t.Fatal("runExtractSubprocess left the parse gate UNBOUNDED (cap=0): the " +
			"per-child half of the #5960 core budget is not installed, so this " +
			"child's cgo parses would be bounded by nothing")
	}
	if want := extractParseConcurrency(); got != want {
		t.Fatalf("parse gate cap = %d, want the child's budgeted GOMAXPROCS %d", got, want)
	}
}

// TestRunExtractSubprocessGateSkippedOnBadArgv documents the one case where the
// gate is deliberately NOT installed: argv that fails validation never runs any
// extraction, so there is nothing to bound.
func TestRunExtractSubprocessGateSkippedOnBadArgv(t *testing.T) {
	indexstate.SetParseConcurrency(0)
	t.Cleanup(func() { indexstate.SetParseConcurrency(0) })

	// Missing --batch → rejected by the required-arg guard before any work.
	if err := runExtractSubprocess([]string{"--repo", t.TempDir()}); err == nil {
		t.Fatal("expected an error for missing --batch")
	}
	if got := indexstate.ParseConcurrencyCap(); got != 0 {
		t.Fatalf("cap = %d after a rejected argv, want 0 — nothing ran, nothing to bound", got)
	}
}

// TestExtractParseConcurrencyMatchesChildGOMAXPROCS pins the SIZE of the cap.
// The coordinator budgets background extraction as
// `concurrency × per-child GOMAXPROCS <= IndexCoreBudget()`, but GOMAXPROCS does
// not bound cgo (tree-sitter) parses — a goroutine in a cgo call releases its P.
// The child therefore enforces the per-child half itself with a real semaphore,
// sized to exactly the GOMAXPROCS the coordinator handed it.
func TestExtractParseConcurrencyMatchesChildGOMAXPROCS(t *testing.T) {
	if got, want := extractParseConcurrency(), runtime.GOMAXPROCS(0); got != want {
		t.Fatalf("extractParseConcurrency() = %d, want the child's GOMAXPROCS %d", got, want)
	}
}

// TestExtractParseConcurrencyNeverUnbounded guards the floor: the parse gate
// treats 0 as "no limit", so a degenerate GOMAXPROCS must not silently disable
// the bound.
func TestExtractParseConcurrencyNeverUnbounded(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	if got := extractParseConcurrency(); got < 1 {
		t.Fatalf("extractParseConcurrency() = %d, want >= 1 (0 disables the gate)", got)
	}
}
