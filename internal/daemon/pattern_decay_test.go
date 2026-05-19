package daemon

// pattern_decay_test.go — unit tests for buildPatternDecayJob (γ).
//
// These tests call buildPatternDecayJob directly so they exercise the decay
// logic without waiting 6 hours for the scheduler to tick.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
)

// makePatternDecayDir creates a temp dir and saves a set of patterns into it.
// Returns the directory path.
func makePatternDecayDir(t *testing.T, patterns []agentpatterns.Pattern) string {
	t.Helper()
	dir := t.TempDir()
	if err := agentpatterns.Save(dir, patterns); err != nil {
		t.Fatalf("save patterns: %v", err)
	}
	return dir
}

// patternWithLastApplied returns a pattern with the given ID, confidence, and
// last_applied unix timestamp.
func patternWithLastApplied(id string, confidence float64, lastApplied int64) agentpatterns.Pattern {
	return agentpatterns.Pattern{
		ID:          id,
		Kind:        "AgentPattern",
		Trigger:     agentpatterns.Trigger{NaturalLanguage: id + " trigger"},
		Steps:       []string{"step one"},
		Category:    agentpatterns.CategoryCode,
		Confidence:  confidence,
		LastApplied: lastApplied,
		IsCandidate: false,
		Exemplars:   []string{"repo::e1"},
	}
}

// nowSec is a fixed "now" for tests: 2026-05-20 00:00:00 UTC.
const nowSec = int64(1747699200)

// TestPatternDecayJob_OldPatternDecays verifies that a pattern with
// last_applied > 30 days ago loses confidence by 0.05 on each decay cycle,
// with 3 cycles totalling 0.15 decrease and eventually flooring at 0.2.
func TestPatternDecayJob_OldPatternDecays(t *testing.T) {
	// last_applied = 90 days ago — well past the 30-day threshold.
	lastApplied := nowSec - 90*86400
	p := patternWithLastApplied("decay-test", 0.6, lastApplied)
	dir := makePatternDecayDir(t, []agentpatterns.Pattern{p})

	groupDirs := map[string]string{"g": dir}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	// Each cycle decrements by DecayDeltaPer30Day = 0.05.
	// Cycle 1: 0.6 → 0.55
	// Cycle 2: 0.55 → 0.50
	// Cycle 3: 0.50 → 0.45
	expected := []float64{0.55, 0.50, 0.45}

	for cycle, want := range expected {
		job(nowSec)
		pts, err := agentpatterns.Load(dir)
		if err != nil {
			t.Fatalf("load after cycle %d: %v", cycle+1, err)
		}
		pN := agentpatterns.ByID(pts, "decay-test")
		if pN == nil {
			t.Fatalf("pattern not found after cycle %d", cycle+1)
		}
		if abs64(pN.Confidence-want) > 1e-9 {
			t.Errorf("cycle %d: expected confidence=%.4f, got %.4f", cycle+1, want, pN.Confidence)
		}
	}
}

// TestPatternDecayJob_Floor verifies that repeated decay cycles do not push
// confidence below 0.2 (ConfidenceFloor).
//
// Starting at 0.5 and decrementing 0.05 per cycle: (0.5-0.2)/0.05 = 6 cycles
// to hit the floor. We run 10 cycles to ensure no further decrease.
func TestPatternDecayJob_Floor(t *testing.T) {
	lastApplied := nowSec - 365*86400 // > 30 days ago
	p := patternWithLastApplied("floor-test", 0.5, lastApplied)
	dir := makePatternDecayDir(t, []agentpatterns.Pattern{p})

	groupDirs := map[string]string{"g": dir}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	// Run 10 cycles — more than enough to hit the floor (needs 6).
	for i := 0; i < 10; i++ {
		job(nowSec)
	}

	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result := agentpatterns.ByID(patterns, "floor-test")
	if result == nil {
		t.Fatal("pattern not found")
	}
	if result.Confidence < agentpatterns.ConfidenceFloor {
		t.Errorf("floor breached: got %.4f, want >= %.4f", result.Confidence, agentpatterns.ConfidenceFloor)
	}
	if result.Confidence != agentpatterns.ConfidenceFloor {
		t.Errorf("expected confidence at floor %.4f after 10 cycles, got %.4f", agentpatterns.ConfidenceFloor, result.Confidence)
	}
}

// TestPatternDecayJob_RecentPatternNotDecayed verifies that a pattern applied
// within the last 30 days is untouched.
func TestPatternDecayJob_RecentPatternNotDecayed(t *testing.T) {
	lastApplied := nowSec - 10*86400 // 10 days ago — within grace window
	p := patternWithLastApplied("recent-test", 0.8, lastApplied)
	dir := makePatternDecayDir(t, []agentpatterns.Pattern{p})

	groupDirs := map[string]string{"g": dir}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	job(nowSec)
	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result := agentpatterns.ByID(patterns, "recent-test")
	if result == nil {
		t.Fatal("pattern not found")
	}
	if result.Confidence != 0.8 {
		t.Errorf("recently-applied pattern should not decay: got %.4f, want 0.8", result.Confidence)
	}
}

// TestPatternDecayJob_NeverAppliedPatternNotDecayed verifies patterns that
// have never been applied (last_applied == 0) are skipped.
func TestPatternDecayJob_NeverAppliedPatternNotDecayed(t *testing.T) {
	p := patternWithLastApplied("never-applied", 0.4, 0) // last_applied = 0
	dir := makePatternDecayDir(t, []agentpatterns.Pattern{p})

	groupDirs := map[string]string{"g": dir}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	job(nowSec)
	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result := agentpatterns.ByID(patterns, "never-applied")
	if result == nil {
		t.Fatal("pattern not found")
	}
	if result.Confidence != 0.4 {
		t.Errorf("never-applied pattern should not decay: got %.4f", result.Confidence)
	}
}

// TestPatternDecayJob_AlreadyAtFloor verifies that a pattern already at
// confidence floor is skipped (no file write needed).
func TestPatternDecayJob_AlreadyAtFloor(t *testing.T) {
	lastApplied := nowSec - 365*86400
	p := patternWithLastApplied("at-floor", agentpatterns.ConfidenceFloor, lastApplied)
	dir := makePatternDecayDir(t, []agentpatterns.Pattern{p})

	// Record file mtime before.
	statBefore, _ := os.Stat(filepath.Join(dir, "patterns.json"))
	mtimeBefore := statBefore.ModTime()

	groupDirs := map[string]string{"g": dir}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	job(nowSec)

	statAfter, _ := os.Stat(filepath.Join(dir, "patterns.json"))
	if statAfter.ModTime().After(mtimeBefore) {
		// File was rewritten; verify content unchanged.
		patterns, _ := agentpatterns.Load(dir)
		result := agentpatterns.ByID(patterns, "at-floor")
		if result.Confidence < agentpatterns.ConfidenceFloor {
			t.Errorf("floor breached: got %.4f", result.Confidence)
		}
	}
	// Either way, confidence must still be at floor.
	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result := agentpatterns.ByID(patterns, "at-floor")
	if result.Confidence < agentpatterns.ConfidenceFloor {
		t.Errorf("floor breached: %.4f", result.Confidence)
	}
}

// TestPatternDecayJob_MultiGroup verifies that multiple groups are all processed.
func TestPatternDecayJob_MultiGroup(t *testing.T) {
	lastApplied := nowSec - 60*86400 // 2 periods
	p1 := patternWithLastApplied("grp1-p", 0.7, lastApplied)
	p2 := patternWithLastApplied("grp2-p", 0.8, lastApplied)

	dir1 := makePatternDecayDir(t, []agentpatterns.Pattern{p1})
	dir2 := makePatternDecayDir(t, []agentpatterns.Pattern{p2})

	groupDirs := map[string]string{"g1": dir1, "g2": dir2}
	job := buildPatternDecayJob(func() map[string]string { return groupDirs }, nil)

	job(nowSec)

	// One cycle = one tick: each pattern loses exactly DecayDeltaPer30Day.
	for _, tc := range []struct {
		dir, id string
		start   float64
	}{
		{dir1, "grp1-p", 0.7},
		{dir2, "grp2-p", 0.8},
	} {
		pts, err := agentpatterns.Load(tc.dir)
		if err != nil {
			t.Fatalf("load %s: %v", tc.dir, err)
		}
		result := agentpatterns.ByID(pts, tc.id)
		if result == nil {
			t.Errorf("pattern %s not found", tc.id)
			continue
		}
		expected := tc.start - agentpatterns.DecayDeltaPer30Day
		if abs64(result.Confidence-expected) > 1e-9 {
			t.Errorf("%s: expected %.4f, got %.4f", tc.id, expected, result.Confidence)
		}
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
