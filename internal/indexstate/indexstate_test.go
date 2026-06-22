package indexstate

import "testing"

// TestSetGet covers the in-flight transitions and the started-at stamping.
func TestSetGet(t *testing.T) {
	t.Cleanup(func() { Set(0) }) // never leak busy state to other tests

	// Idle baseline.
	Set(0)
	if s := Get(); s.IsIndexing || s.InFlight != 0 || !s.StartedAt.IsZero() {
		t.Fatalf("idle: got %+v, want not-indexing/0/zero-time", s)
	}

	// 0→1 stamps a start time and flips is_indexing.
	Set(1)
	s := Get()
	if !s.IsIndexing || s.InFlight != 1 {
		t.Fatalf("busy: got %+v, want indexing with 1 in flight", s)
	}
	if s.StartedAt.IsZero() {
		t.Fatal("busy: StartedAt should be stamped on the 0→1 edge")
	}
	started := s.StartedAt

	// 1→2 keeps the SAME start time (still one busy period).
	Set(2)
	if s := Get(); !s.IsIndexing || s.InFlight != 2 || !s.StartedAt.Equal(started) {
		t.Fatalf("ramp: got %+v, want 2 in flight, unchanged start %v", s, started)
	}

	// 2→0 clears everything.
	Set(0)
	if s := Get(); s.IsIndexing || s.InFlight != 0 || !s.StartedAt.IsZero() {
		t.Fatalf("drain: got %+v, want idle", s)
	}

	// Negative is clamped to 0.
	Set(-5)
	if s := Get(); s.IsIndexing || s.InFlight != 0 {
		t.Fatalf("negative clamp: got %+v, want idle", s)
	}
}

// TestGroupAlgoInFlight covers the group-algo busy counter (#5349 A3): a pass
// in flight flips is_indexing even with zero index jobs, stamps a start time
// from idle, and is balanced by GroupAlgoEnd.
func TestGroupAlgoInFlight(t *testing.T) {
	t.Cleanup(func() { Set(0); GroupAlgoEnd() })

	Set(0)
	if s := Get(); s.IsIndexing {
		t.Fatalf("idle baseline: got %+v", s)
	}

	GroupAlgoBegin()
	s := Get()
	if !s.IsIndexing || s.GroupAlgoInFlight != 1 {
		t.Fatalf("group-algo busy: got %+v, want indexing with 1 group-algo", s)
	}
	if s.StartedAt.IsZero() {
		t.Fatal("group-algo busy: StartedAt should be stamped from idle")
	}

	GroupAlgoEnd()
	if s := Get(); s.IsIndexing || s.GroupAlgoInFlight != 0 {
		t.Fatalf("group-algo end: got %+v, want idle", s)
	}

	// Extra End is clamped (never negative).
	GroupAlgoEnd()
	if s := Get(); s.GroupAlgoInFlight != 0 {
		t.Fatalf("group-algo end clamp: got %+v", s)
	}
}
