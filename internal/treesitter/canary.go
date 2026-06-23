package treesitter

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// A4 — runtime per-language parse-error-node canary (#5414, epic #5359).
//
// grafel's tree-sitter grammars ride on a single pinned dependency with no
// per-language freshness tracking. As languages evolve, the grammar stops
// recognising new syntax and tree-sitter (error-tolerant by design) silently
// emits ERROR nodes instead of failing the index. The direct, version-agnostic
// symptom is a RISE in the per-language ERROR-node rate.
//
// A per-parse ErrorRatio (error_nodes/total_nodes) already exists on
// ParseResult and is used today only as a per-file gate + an OTel span
// attribute. The canary AGGREGATES that ratio per language across an index run
// (weighted by node count), compares the aggregate against a committed/loaded
// baseline, and raises a spike flag when a language's rate exceeds its baseline
// by a tunable threshold. The flag rides along in the stats output so the
// dashboard / a future cron can read it; a WARN line is logged at detection.

// LangErrorStats is the per-language accumulator. Counts are node-weighted so
// the aggregate ErrorRate reflects large files proportionally rather than
// treating a 3-node file the same as a 30k-node file.
type LangErrorStats struct {
	// Files is the number of files parsed for this language.
	Files int `json:"files"`
	// TotalNodes is the sum of NodeCount across every parsed file.
	TotalNodes int `json:"total_nodes"`
	// ErrorNodes is the sum of ERROR nodes across every parsed file.
	ErrorNodes int `json:"error_nodes"`
}

// ErrorRate is the node-weighted aggregate error-node fraction for the
// language. Returns 0 when no nodes were parsed (absence/zero tolerant — a
// language with no files can never spike).
func (s LangErrorStats) ErrorRate() float64 {
	if s.TotalNodes <= 0 {
		return 0
	}
	return float64(s.ErrorNodes) / float64(s.TotalNodes)
}

// ParseErrorCanary accumulates per-language ERROR-node stats across an index
// run from the existing per-parse ErrorRatio + NodeCount. The zero value is
// ready to use. It is NOT safe for concurrent use — callers serialise updates
// (the parse path already holds a lock around the node walk, and the subprocess
// path accumulates single-threaded per batch).
type ParseErrorCanary struct {
	byLang map[string]*LangErrorStats
}

// NewParseErrorCanary returns an empty canary.
func NewParseErrorCanary() *ParseErrorCanary {
	return &ParseErrorCanary{byLang: map[string]*LangErrorStats{}}
}

// Observe folds a single parse result into the per-language accumulator.
// errorRatio and nodeCount come straight off ParseResult. Files with zero
// nodes still count toward Files but contribute no node weight. A negative
// nodeCount is clamped to zero (defensive; should not happen).
func (c *ParseErrorCanary) Observe(language string, errorRatio float64, nodeCount int) {
	if c == nil {
		return
	}
	if c.byLang == nil {
		c.byLang = map[string]*LangErrorStats{}
	}
	if nodeCount < 0 {
		nodeCount = 0
	}
	s := c.byLang[language]
	if s == nil {
		s = &LangErrorStats{}
		c.byLang[language] = s
	}
	s.Files++
	s.TotalNodes += nodeCount
	// Reconstruct the integer error-node count from the ratio. The parser
	// computes errorRatio = errNodes/total, so errNodes = round(ratio*total).
	// Rounding avoids drift from float storage; for the weighted aggregate the
	// per-file rounding error is negligible.
	if nodeCount > 0 && errorRatio > 0 {
		s.ErrorNodes += int(errorRatio*float64(nodeCount) + 0.5)
	}
}

// ObserveResult is a convenience wrapper folding a ParseResult directly.
func (c *ParseErrorCanary) ObserveResult(pr *ParseResult) {
	if c == nil || pr == nil {
		return
	}
	c.Observe(pr.Language, pr.ErrorRatio, pr.NodeCount)
}

// Merge folds another canary's per-language stats into this one. Used by the
// subprocess coordinator to sum per-batch canaries into the run total.
func (c *ParseErrorCanary) Merge(other *ParseErrorCanary) {
	if c == nil || other == nil {
		return
	}
	if c.byLang == nil {
		c.byLang = map[string]*LangErrorStats{}
	}
	for lang, os := range other.byLang {
		s := c.byLang[lang]
		if s == nil {
			s = &LangErrorStats{}
			c.byLang[lang] = s
		}
		s.Files += os.Files
		s.TotalNodes += os.TotalNodes
		s.ErrorNodes += os.ErrorNodes
	}
}

// MergeStats folds a serialised per-language map (e.g. decoded from a
// subprocess BatchStats) into the canary.
func (c *ParseErrorCanary) MergeStats(byLang map[string]LangErrorStats) {
	if c == nil || len(byLang) == 0 {
		return
	}
	if c.byLang == nil {
		c.byLang = map[string]*LangErrorStats{}
	}
	for lang, os := range byLang {
		s := c.byLang[lang]
		if s == nil {
			s = &LangErrorStats{}
			c.byLang[lang] = s
		}
		s.Files += os.Files
		s.TotalNodes += os.TotalNodes
		s.ErrorNodes += os.ErrorNodes
	}
}

// Snapshot returns a copy of the per-language stats keyed by language. Safe to
// serialise. Returns an empty (non-nil) map when nothing was observed.
func (c *ParseErrorCanary) Snapshot() map[string]LangErrorStats {
	out := map[string]LangErrorStats{}
	if c == nil {
		return out
	}
	for lang, s := range c.byLang {
		out[lang] = *s
	}
	return out
}

// --- Baseline + spike detection ------------------------------------------

// defaultAbsThreshold is the absolute error-rate increase (current - baseline)
// that trips the canary. 0.02 = the language's aggregate error-node fraction
// rose by 2 percentage points vs baseline.
const defaultAbsThreshold = 0.02

// defaultRelFactor is the relative multiplier that ALSO trips the canary when
// the baseline is non-trivial. current >= baseline * factor. 2.0 = the rate
// doubled. The two are OR'd so we catch both "small-baseline doubling" and
// "large absolute jump".
const defaultRelFactor = 2.0

// minBaselineNodes is the node count a language's baseline must have to be
// trusted for relative comparison; below it we only apply the absolute test
// (a baseline built from a handful of nodes is statistically meaningless).
const minBaselineNodes = 200

// Baseline maps language -> baseline aggregate error-node stats. It is the
// committed/loaded source-of-truth the current run is compared against. The
// JSON shape matches Snapshot() so a prior run's Snapshot can be persisted and
// reloaded as the next run's baseline.
type Baseline struct {
	Version int                       `json:"version"`
	ByLang  map[string]LangErrorStats `json:"by_lang"`
}

// LoadBaseline reads a baseline file. A missing file is NOT an error — it
// returns an empty baseline so a first-ever run simply records without
// spiking.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Baseline{Version: 1, ByLang: map[string]LangErrorStats{}}, nil
		}
		return nil, fmt.Errorf("treesitter: read baseline %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("treesitter: parse baseline %s: %w", path, err)
	}
	if b.ByLang == nil {
		b.ByLang = map[string]LangErrorStats{}
	}
	return &b, nil
}

// SaveBaseline writes the canary's current per-language stats as a baseline.
// Used to refresh the committed baseline after a known-good index run.
func SaveBaseline(path string, snap map[string]LangErrorStats) error {
	b := Baseline{Version: 1, ByLang: snap}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("treesitter: encode baseline: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("treesitter: write baseline %s: %w", path, err)
	}
	return nil
}

// SpikeThresholds carries the tunable detection parameters. Use
// ThresholdsFromEnv to populate from the environment with sensible defaults.
type SpikeThresholds struct {
	// AbsDelta: trip when current - baseline >= AbsDelta.
	AbsDelta float64
	// RelFactor: trip when baseline has enough weight and
	// current >= baseline * RelFactor.
	RelFactor float64
}

// DefaultThresholds returns the built-in tuning.
func DefaultThresholds() SpikeThresholds {
	return SpikeThresholds{AbsDelta: defaultAbsThreshold, RelFactor: defaultRelFactor}
}

// ThresholdsFromEnv reads GRAFEL_CANARY_ABS_DELTA and GRAFEL_CANARY_REL_FACTOR,
// falling back to the defaults when unset or unparseable.
func ThresholdsFromEnv() SpikeThresholds {
	t := DefaultThresholds()
	if v := os.Getenv("GRAFEL_CANARY_ABS_DELTA"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			t.AbsDelta = f
		}
	}
	if v := os.Getenv("GRAFEL_CANARY_REL_FACTOR"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			t.RelFactor = f
		}
	}
	return t
}

// LangSpike describes one language's current-vs-baseline comparison.
type LangSpike struct {
	Language     string  `json:"language"`
	CurrentRate  float64 `json:"current_rate"`
	BaselineRate float64 `json:"baseline_rate"`
	Delta        float64 `json:"delta"`
	Files        int     `json:"files"`
	TotalNodes   int     `json:"total_nodes"`
	ErrorNodes   int     `json:"error_nodes"`
	// Spiked is true when this language tripped the threshold.
	Spiked bool `json:"spiked"`
	// Reason is a short human string ("abs", "rel", or "") explaining the trip.
	Reason string `json:"reason,omitempty"`
}

// CanaryReport is the per-run output: every observed language's comparison plus
// a top-level Spiked flag. It is JSON-serialisable for the stats sidecar.
type CanaryReport struct {
	Thresholds SpikeThresholds `json:"thresholds"`
	// Spiked is true when ANY language tripped the threshold.
	Spiked bool `json:"spiked"`
	// Languages is sorted by language name for deterministic output.
	Languages []LangSpike `json:"languages"`
}

// SpikedLanguages returns the names of languages that tripped, in sorted order.
func (r CanaryReport) SpikedLanguages() []string {
	var out []string
	for _, l := range r.Languages {
		if l.Spiked {
			out = append(out, l.Language)
		}
	}
	return out
}

// Classify compares a current per-language snapshot against a baseline and
// returns a CanaryReport. It is absence/zero tolerant:
//   - a language with zero parsed nodes never spikes;
//   - a missing/zero baseline only trips on the ABSOLUTE test (we cannot do a
//     meaningful relative comparison against zero, and a first-ever run should
//     not light up just because a baseline does not exist yet);
//   - the relative test only applies once the baseline carries minBaselineNodes
//     of weight, so noise on tiny baselines does not raise false alarms.
func Classify(current map[string]LangErrorStats, baseline *Baseline, t SpikeThresholds) CanaryReport {
	if t.AbsDelta <= 0 {
		t.AbsDelta = defaultAbsThreshold
	}
	if t.RelFactor <= 0 {
		t.RelFactor = defaultRelFactor
	}
	rep := CanaryReport{Thresholds: t}

	langs := make([]string, 0, len(current))
	for lang := range current {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	var baseByLang map[string]LangErrorStats
	if baseline != nil {
		baseByLang = baseline.ByLang
	}

	for _, lang := range langs {
		cur := current[lang]
		curRate := cur.ErrorRate()

		base := baseByLang[lang]
		baseRate := base.ErrorRate()

		ls := LangSpike{
			Language:     lang,
			CurrentRate:  curRate,
			BaselineRate: baseRate,
			Delta:        curRate - baseRate,
			Files:        cur.Files,
			TotalNodes:   cur.TotalNodes,
			ErrorNodes:   cur.ErrorNodes,
		}

		// Zero-tolerance: no nodes parsed => never spike.
		if cur.TotalNodes > 0 {
			// Absolute test always applies.
			if curRate-baseRate >= t.AbsDelta {
				ls.Spiked = true
				ls.Reason = "abs"
			}
			// Relative test only when the baseline is statistically meaningful
			// AND non-zero (avoids divide-by-meaning on a fresh language).
			if !ls.Spiked && base.TotalNodes >= minBaselineNodes && baseRate > 0 {
				if curRate >= baseRate*t.RelFactor {
					ls.Spiked = true
					ls.Reason = "rel"
				}
			}
		}

		if ls.Spiked {
			rep.Spiked = true
		}
		rep.Languages = append(rep.Languages, ls)
	}
	return rep
}
