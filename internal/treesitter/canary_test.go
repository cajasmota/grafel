package treesitter

import (
	"math"
	"path/filepath"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestLangErrorStats_ErrorRate(t *testing.T) {
	if r := (LangErrorStats{}).ErrorRate(); r != 0 {
		t.Fatalf("empty stats rate = %v, want 0", r)
	}
	s := LangErrorStats{Files: 2, TotalNodes: 1000, ErrorNodes: 50}
	if !approx(s.ErrorRate(), 0.05) {
		t.Fatalf("rate = %v, want 0.05", s.ErrorRate())
	}
	// Zero nodes must never divide-by-zero (absence tolerant).
	z := LangErrorStats{Files: 3, TotalNodes: 0, ErrorNodes: 0}
	if r := z.ErrorRate(); r != 0 {
		t.Fatalf("zero-node rate = %v, want 0", r)
	}
}

func TestCanary_ObserveAndSnapshot(t *testing.T) {
	c := NewParseErrorCanary()
	// java: two files, weighted aggregate.
	c.Observe("java", 0.0, 1000)  // 0 error nodes
	c.Observe("java", 0.10, 1000) // 100 error nodes
	// python: one clean file.
	c.Observe("python", 0.0, 500)
	// go: a zero-node file (empty source) — counts as a file, no weight.
	c.Observe("go", 0.0, 0)

	snap := c.Snapshot()

	j := snap["java"]
	if j.Files != 2 || j.TotalNodes != 2000 {
		t.Fatalf("java files/nodes = %d/%d, want 2/2000", j.Files, j.TotalNodes)
	}
	if j.ErrorNodes != 100 {
		t.Fatalf("java error nodes = %d, want 100", j.ErrorNodes)
	}
	if !approx(j.ErrorRate(), 0.05) {
		t.Fatalf("java weighted rate = %v, want 0.05", j.ErrorRate())
	}

	if p := snap["python"]; p.ErrorNodes != 0 || !approx(p.ErrorRate(), 0) {
		t.Fatalf("python should have zero error rate, got %+v", p)
	}
	if g := snap["go"]; g.Files != 1 || g.TotalNodes != 0 || g.ErrorRate() != 0 {
		t.Fatalf("go zero-node file mishandled: %+v", g)
	}
}

func TestCanary_MergeAndMergeStats(t *testing.T) {
	a := NewParseErrorCanary()
	a.Observe("rust", 0.02, 1000) // 20 err
	b := NewParseErrorCanary()
	b.Observe("rust", 0.04, 1000) // 40 err
	a.Merge(b)
	if got := a.Snapshot()["rust"]; got.TotalNodes != 2000 || got.ErrorNodes != 60 {
		t.Fatalf("merge wrong: %+v", got)
	}

	c := NewParseErrorCanary()
	c.MergeStats(map[string]LangErrorStats{
		"rust": {Files: 1, TotalNodes: 1000, ErrorNodes: 60},
	})
	c.MergeStats(map[string]LangErrorStats{
		"rust": {Files: 1, TotalNodes: 1000, ErrorNodes: 60},
	})
	if got := c.Snapshot()["rust"]; got.Files != 2 || got.ErrorNodes != 120 {
		t.Fatalf("mergestats wrong: %+v", got)
	}
}

// TestClassify_SpikeStableEmpty is the core acceptance test: a spiking
// language trips, a stable one does not, and an empty one is tolerated.
func TestClassify_SpikeStableEmpty(t *testing.T) {
	baseline := &Baseline{
		Version: 1,
		ByLang: map[string]LangErrorStats{
			// java baseline is clean (grammar handled the old syntax).
			"java": {Files: 10, TotalNodes: 10000, ErrorNodes: 100}, // 1%
			// python baseline already a bit noisy.
			"python": {Files: 10, TotalNodes: 10000, ErrorNodes: 200}, // 2%
		},
	}

	current := map[string]LangErrorStats{
		// java SPIKE: 1% -> 8% — the "new unhandled syntax" symptom.
		"java": {Files: 10, TotalNodes: 10000, ErrorNodes: 800}, // 8%
		// python STABLE: 2% -> 2.1% (under the 2pp absolute threshold and
		// under 2x relative).
		"python": {Files: 10, TotalNodes: 10000, ErrorNodes: 210}, // 2.1%
		// go EMPTY: no nodes parsed — must never spike.
		"go": {Files: 5, TotalNodes: 0, ErrorNodes: 0},
	}

	rep := Classify(current, baseline, DefaultThresholds())

	if !rep.Spiked {
		t.Fatal("expected overall spike (java)")
	}
	spiked := rep.SpikedLanguages()
	if len(spiked) != 1 || spiked[0] != "java" {
		t.Fatalf("spiked languages = %v, want [java]", spiked)
	}

	byLang := map[string]LangSpike{}
	for _, l := range rep.Languages {
		byLang[l.Language] = l
	}
	if !byLang["java"].Spiked || byLang["java"].Reason != "abs" {
		t.Fatalf("java should spike via abs: %+v", byLang["java"])
	}
	if byLang["python"].Spiked {
		t.Fatalf("python should be stable: %+v", byLang["python"])
	}
	if byLang["go"].Spiked {
		t.Fatalf("empty-language go must never spike: %+v", byLang["go"])
	}
}

// TestClassify_RelativeTrip covers a small-baseline doubling that the absolute
// threshold would miss but the relative factor catches.
func TestClassify_RelativeTrip(t *testing.T) {
	baseline := &Baseline{ByLang: map[string]LangErrorStats{
		"ruby": {Files: 5, TotalNodes: 5000, ErrorNodes: 50}, // 1%
	}}
	current := map[string]LangErrorStats{
		// 1% -> 2.5%: delta 1.5pp < 2pp abs, but 2.5x >= 2x rel.
		"ruby": {Files: 5, TotalNodes: 5000, ErrorNodes: 125}, // 2.5%
	}
	rep := Classify(current, baseline, DefaultThresholds())
	if !rep.Spiked {
		t.Fatal("expected relative spike for ruby")
	}
	for _, l := range rep.Languages {
		if l.Language == "ruby" && l.Reason != "rel" {
			t.Fatalf("ruby spike reason = %q, want rel", l.Reason)
		}
	}
}

// TestClassify_NoBaselineFirstRun: a first-ever run (no baseline for the
// language) only trips on the ABSOLUTE test, so a low-error new language does
// not light up just because no baseline exists.
func TestClassify_NoBaselineFirstRun(t *testing.T) {
	current := map[string]LangErrorStats{
		// 1.5% error, no baseline — should NOT spike (under abs threshold,
		// and relative test is skipped without a baseline).
		"scala": {Files: 3, TotalNodes: 2000, ErrorNodes: 30},
	}
	rep := Classify(current, &Baseline{ByLang: map[string]LangErrorStats{}}, DefaultThresholds())
	if rep.Spiked {
		t.Fatalf("first-run low-error language should not spike: %+v", rep.Languages)
	}

	// But a first-run language ABOVE the absolute threshold still trips (a
	// brand-new language whose grammar already chokes is worth flagging).
	current["scala"] = LangErrorStats{Files: 3, TotalNodes: 2000, ErrorNodes: 200} // 10%
	rep = Classify(current, nil, DefaultThresholds())
	if !rep.Spiked {
		t.Fatal("first-run high-error language should spike via abs")
	}
}

// TestClassify_TinyBaselineNoRelative: a baseline with too few nodes must not
// drive a relative trip (noise protection).
func TestClassify_TinyBaselineNoRelative(t *testing.T) {
	baseline := &Baseline{ByLang: map[string]LangErrorStats{
		// only 100 nodes < minBaselineNodes (200): relative test disabled.
		"lua": {Files: 1, TotalNodes: 100, ErrorNodes: 1}, // 1%
	}}
	current := map[string]LangErrorStats{
		// 3% — 3x the tiny baseline, but delta 2pp ... right at abs. Make it
		// 2.9% so abs (2pp -> delta 1.9pp) does NOT trip; only relative would,
		// and relative is disabled by the tiny baseline.
		"lua": {Files: 1, TotalNodes: 1000, ErrorNodes: 29}, // 2.9%
	}
	rep := Classify(current, baseline, DefaultThresholds())
	if rep.Spiked {
		t.Fatalf("tiny-baseline relative trip should be suppressed: %+v", rep.Languages)
	}
}

func TestLoadBaseline_Missing(t *testing.T) {
	b, err := LoadBaseline(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing baseline should not error: %v", err)
	}
	if b == nil || b.ByLang == nil || len(b.ByLang) != 0 {
		t.Fatalf("missing baseline should be empty, got %+v", b)
	}
}

func TestSaveLoadBaseline_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	snap := map[string]LangErrorStats{
		"go": {Files: 4, TotalNodes: 8000, ErrorNodes: 16},
	}
	if err := SaveBaseline(path, snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := b.ByLang["go"]; got.TotalNodes != 8000 || got.ErrorNodes != 16 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestThresholdsFromEnv(t *testing.T) {
	t.Setenv("GRAFEL_CANARY_ABS_DELTA", "0.05")
	t.Setenv("GRAFEL_CANARY_REL_FACTOR", "3.0")
	got := ThresholdsFromEnv()
	if !approx(got.AbsDelta, 0.05) || !approx(got.RelFactor, 3.0) {
		t.Fatalf("env thresholds = %+v", got)
	}
	// Bad values fall back to defaults.
	t.Setenv("GRAFEL_CANARY_ABS_DELTA", "not-a-number")
	t.Setenv("GRAFEL_CANARY_REL_FACTOR", "-1")
	got = ThresholdsFromEnv()
	d := DefaultThresholds()
	if !approx(got.AbsDelta, d.AbsDelta) || !approx(got.RelFactor, d.RelFactor) {
		t.Fatalf("bad env should fall back to defaults, got %+v", got)
	}
}
