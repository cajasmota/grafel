package agentpatterns_test

import (
	"context"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
)

// ---------------------------------------------------------------------------
// NewPatternConfidence
// ---------------------------------------------------------------------------

func TestNewPatternConfidence(t *testing.T) {
	c := agentpatterns.NewPatternConfidence()
	if c != 0.4 {
		t.Errorf("initial confidence = %v, want 0.4", c)
	}
}

// ---------------------------------------------------------------------------
// ApplyConfidenceDelta — each event type
// ---------------------------------------------------------------------------

func TestApplyConfidenceDelta_ApplySuccess(t *testing.T) {
	got := agentpatterns.ApplyConfidenceDelta(0.5, agentpatterns.EventApplySuccess)
	want := 0.6
	if abs(got-want) > 1e-9 {
		t.Errorf("ApplySuccess: got %v want %v", got, want)
	}
}

func TestApplyConfidenceDelta_ApplyFailure(t *testing.T) {
	got := agentpatterns.ApplyConfidenceDelta(0.5, agentpatterns.EventApplyFailure)
	want := 0.35
	if abs(got-want) > 1e-9 {
		t.Errorf("ApplyFailure: got %v want %v", got, want)
	}
}

func TestApplyConfidenceDelta_Reject(t *testing.T) {
	got := agentpatterns.ApplyConfidenceDelta(0.5, agentpatterns.EventReject)
	want := 0.2
	if abs(got-want) > 1e-9 {
		t.Errorf("Reject: got %v want %v", got, want)
	}
}

func TestApplyConfidenceDelta_Refine(t *testing.T) {
	got := agentpatterns.ApplyConfidenceDelta(0.5, agentpatterns.EventRefine)
	if abs(got-0.5) > 1e-9 {
		t.Errorf("Refine should be neutral: got %v want 0.5", got)
	}
}

func TestApplyConfidenceDelta_CeilingClamped(t *testing.T) {
	got := agentpatterns.ApplyConfidenceDelta(0.95, agentpatterns.EventApplySuccess)
	if got > agentpatterns.ConfidenceCeiling {
		t.Errorf("ceiling not enforced: got %v", got)
	}
	if got != agentpatterns.ConfidenceCeiling {
		t.Errorf("expected %v, got %v", agentpatterns.ConfidenceCeiling, got)
	}
}

func TestApplyConfidenceDelta_FloorClamped_Reject(t *testing.T) {
	// Start at 0.4 and reject — floor must be 0.2.
	got := agentpatterns.ApplyConfidenceDelta(0.4, agentpatterns.EventReject)
	if got < agentpatterns.ConfidenceFloor {
		t.Errorf("floor not enforced: got %v", got)
	}
	if got != agentpatterns.ConfidenceFloor {
		t.Errorf("expected floor %v, got %v", agentpatterns.ConfidenceFloor, got)
	}
}

func TestApplyConfidenceDelta_FloorClamped_MultipleFailures(t *testing.T) {
	c := agentpatterns.InitialConfidence
	for i := 0; i < 20; i++ {
		c = agentpatterns.ApplyConfidenceDelta(c, agentpatterns.EventApplyFailure)
	}
	if c < agentpatterns.ConfidenceFloor {
		t.Errorf("floor breached: got %v", c)
	}
	if c != agentpatterns.ConfidenceFloor {
		t.Errorf("expected floor %v after repeated failures, got %v", agentpatterns.ConfidenceFloor, c)
	}
}

// ---------------------------------------------------------------------------
// Time decay
// ---------------------------------------------------------------------------

func TestApplyTimeDecay_ZeroDays(t *testing.T) {
	got := agentpatterns.ApplyTimeDecay(0.8, 0)
	if got != 0.8 {
		t.Errorf("0 days decay should not change confidence: got %v", got)
	}
}

func TestApplyTimeDecay_30Days(t *testing.T) {
	got := agentpatterns.ApplyTimeDecay(0.8, 30)
	want := 0.75
	if abs(got-want) > 1e-9 {
		t.Errorf("30-day decay: got %v want %v", got, want)
	}
}

func TestApplyTimeDecay_60Days(t *testing.T) {
	got := agentpatterns.ApplyTimeDecay(0.8, 60)
	want := 0.7
	if abs(got-want) > 1e-9 {
		t.Errorf("60-day decay: got %v want %v", got, want)
	}
}

func TestApplyTimeDecay_FloorAt02(t *testing.T) {
	// Simulate 3 years of decay from floor+small nudge above — must clamp at 0.2.
	got := agentpatterns.ApplyTimeDecay(0.4, 1095) // 3 years = 36.5 periods × 0.05 = 1.825 delta
	if got < agentpatterns.ConfidenceFloor {
		t.Errorf("time decay floor breached: got %v, want >= %v", got, agentpatterns.ConfidenceFloor)
	}
	if got != agentpatterns.ConfidenceFloor {
		t.Errorf("expected floor %v after large decay, got %v", agentpatterns.ConfidenceFloor, got)
	}
}

func TestApplyTimeDecayFromUnix_NeverApplied(t *testing.T) {
	now := int64(1716200000)
	got := agentpatterns.ApplyTimeDecayFromUnix(0.8, 0, now)
	if got != 0.8 {
		t.Errorf("never-applied should not decay: got %v", got)
	}
}

func TestApplyTimeDecayFromUnix_30DaysAgo(t *testing.T) {
	now := int64(1716200000)
	last := now - 30*86400
	got := agentpatterns.ApplyTimeDecayFromUnix(0.8, last, now)
	want := 0.75
	if abs(got-want) > 1e-9 {
		t.Errorf("30-day decay via unix: got %v want %v", got, want)
	}
}

func TestApplyTimeDecayFromUnix_FloorAt02(t *testing.T) {
	now := int64(1716200000)
	last := now - 365*86400*3 // 3 years ago
	got := agentpatterns.ApplyTimeDecayFromUnix(0.5, last, now)
	if got < agentpatterns.ConfidenceFloor {
		t.Errorf("floor breached: got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Decay scheduler skeleton — smoke test
// ---------------------------------------------------------------------------

func TestDecayScheduler_RunAndCancel(t *testing.T) {
	// Deterministic tick assertion: rather than counting how many ticks land
	// inside a wall-clock deadline (which is flaky under parallel CPU
	// contention because time.Ticker coalesces/drops ticks when the receiving
	// goroutine is starved), we cancel from inside the job once we've observed
	// the target number of ticks. The scheduler is therefore guaranteed to run
	// at least wantTicks times before Run returns, regardless of scheduling
	// pressure. Run executes the job inline, so calls is not racy.
	const wantTicks = 3
	calls := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := func(nowUnix int64) {
		calls++
		if calls >= wantTicks {
			cancel()
		}
	}
	// A short interval keeps the test fast; correctness no longer depends on
	// it being short relative to any deadline.
	sched := agentpatterns.NewDecayScheduler(1*time.Millisecond, job)

	sched.Run(ctx)

	if calls < wantTicks {
		t.Errorf("expected >= %d scheduler ticks, got %d", wantTicks, calls)
	}
}

func TestDecayScheduler_StopsOnCancel(t *testing.T) {
	calls := 0
	job := func(int64) { calls++ }
	sched := agentpatterns.NewDecayScheduler(5*time.Millisecond, job)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sched.Run(ctx)
	// After an immediate cancel, the scheduler should exit without ticking.
	if calls != 0 {
		t.Errorf("cancelled scheduler should not tick, got %d calls", calls)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
