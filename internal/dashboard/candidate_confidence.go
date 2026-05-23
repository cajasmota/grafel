// candidate_confidence.go — surface-side candidate quality bar (#1129).
//
// Background. Per-surface enrichment OPS (#1666 / #1103) — merge / disqualify
// / rank / group — apply ON TOP of whatever a surface emits. That means
// trivial noise (container-node referencers, builtin-method targets,
// near-duplicate intra-module trivial calls, label-only entries with no
// source file) still pollutes the default UI lists until a human (or LLM)
// explicitly disqualifies each one.
//
// This module adds a confidence floor that runs BEFORE the ops pass:
//
//  1. ComputeCandidateConfidence(entry) → score in [0, 1] + signals[].
//  2. Per-surface floors (env-overridable) decide whether an entry belongs
//     to the default list or the low_confidence bucket.
//  3. FilterByConfidence partitions a candidate slice into (kept, low) and
//     reports a noise-rejected count. Disqualified entries (ops) always lose
//     to disqualify; a non-zero explicit rank (ops) always wins over the
//     floor — so explicit human/LLM signal overrides the heuristic.
//
// Toggle. Set ARCHIGRAPH_CONFIDENCE_FLOOR=off (or "0", "false") to disable
// filtering entirely; the score is still computed and surfaced so the UI can
// show "would-be-rejected" candidates in dev mode without losing them.
//
// Per-surface env overrides:
//
//	ARCHIGRAPH_CONFIDENCE_FLOOR_FLOWS    (default 0.35)
//	ARCHIGRAPH_CONFIDENCE_FLOOR_TOPOLOGY (default 0.45)
//	ARCHIGRAPH_CONFIDENCE_FLOOR_PATHS    (default 0.30)

package dashboard

import (
	"os"
	"strconv"
	"strings"
)

// Surface is a tag identifying which candidate-list surface a filter applies
// to. Floors are calibrated per-surface because the cost of a false-positive
// (a noisy entry that slips through) and a false-negative (a useful entry
// that gets buried in low_confidence) differ across the three surfaces.
type Surface string

const (
	SurfaceFlows    Surface = "flows"
	SurfaceTopology Surface = "topology"
	SurfacePaths    Surface = "paths"
)

// defaultFloors are the calibrated baseline confidence floors per surface.
// Reasoning:
//   - Topology has the strictest floor (0.45) because the surface is a small
//     overview ribbon — every visible entry must be a "real" broker artifact.
//   - Flows is mid (0.35) — process flows already get hard-filtered by step
//     count upstream (#1639), so the noise that remains tends to be
//     entry-point heuristics; the floor catches the worst of these.
//   - Paths is the loosest (0.30) — HTTP endpoints come from structured route
//     declarations and have low intrinsic noise; the floor catches synthetic
//     client-side-only entries that lack a corresponding handler.
var defaultFloors = map[Surface]float64{
	SurfaceFlows:    0.35,
	SurfaceTopology: 0.45,
	SurfacePaths:    0.30,
}

// FloorFor returns the active confidence floor for a surface. The order of
// resolution is: per-surface env override → master toggle → built-in default.
// When the master toggle ARCHIGRAPH_CONFIDENCE_FLOOR is "off"/"0"/"false",
// the returned floor is 0 (every candidate is kept in the default list).
func FloorFor(s Surface) float64 {
	if isFloorDisabled() {
		return 0
	}
	var envKey string
	switch s {
	case SurfaceFlows:
		envKey = "ARCHIGRAPH_CONFIDENCE_FLOOR_FLOWS"
	case SurfaceTopology:
		envKey = "ARCHIGRAPH_CONFIDENCE_FLOOR_TOPOLOGY"
	case SurfacePaths:
		envKey = "ARCHIGRAPH_CONFIDENCE_FLOOR_PATHS"
	}
	if envKey != "" {
		if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				if f < 0 {
					f = 0
				}
				if f > 1 {
					f = 1
				}
				return f
			}
		}
	}
	return defaultFloors[s]
}

// isFloorDisabled returns true when the master toggle is off. Accepts
// "off", "0", "false" (case-insensitive). Default behaviour (unset) is ON.
func isFloorDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ARCHIGRAPH_CONFIDENCE_FLOOR")))
	return v == "off" || v == "0" || v == "false" || v == "no"
}

// ConfidenceHints carries optional graph-context hints that the score function
// uses when present. All fields are optional — zero values are interpreted as
// "no signal" and never penalise an entry.
type ConfidenceHints struct {
	// OutboundEdges is the number of outgoing relationships from the entity.
	OutboundEdges int
	// InboundEdges is the number of incoming relationships to the entity.
	InboundEdges int
	// HasCrossRepoEdge is true when at least one inbound or outbound edge
	// crosses repository boundaries (strong "real artifact" signal).
	HasCrossRepoEdge bool
	// ResolvedRefs is the number of references on this entry that were
	// fully resolved (e.g. handlers for an endpoint, producers+consumers
	// for a topic). Higher = more confidence the entry is real.
	ResolvedRefs int
}

// noisyContainerNames are entry/handler names that almost always indicate a
// generic container/builtin reference rather than a real artifact. Hits
// trigger a strong negative modifier in ComputeCandidateConfidence.
var noisyContainerNames = map[string]bool{
	"":          true,
	"<module>":  true,
	"<lambda>":  true,
	"__init__":  true,
	"__main__":  true,
	"_":         true,
	"main":      true,
	"anonymous": true,
	"callback":  true,
	"handler":   true,
	"fn":        true,
	"cb":        true,
}

// builtinMethodFragments are substrings that, when matched in a qualified
// name or handler name, almost always indicate a builtin/stdlib target
// rather than a domain entity. Each fragment match is a small negative.
var builtinMethodFragments = []string{
	"builtins.", "stdlib.", "internal._",
	".__str__", ".__repr__", ".__init__", ".__call__",
	".tostring", ".valueof", ".tojson",
}

// trivialFlowTerminals are step / terminal names that almost always indicate
// a flow whose sink is an intra-component React state or hook call rather
// than a real downstream effect. Hits trigger a strong negative on the
// Flows surface — these are the dominant noise source on a React-heavy
// frontend codebase (see upvate audit, 2026-05-23, where >80% of
// short-chain flows terminate in one of these).
//
// The match is case-insensitive and is applied to the LAST segment after
// the final " → " separator in the flow label, which is the natural
// "terminal" position emitted by process_flow.go.
var trivialFlowTerminals = map[string]bool{
	"useeffect":      true,
	"usestate":       true,
	"usecallback":    true,
	"usememo":        true,
	"useref":         true,
	"usecontext":     true,
	"usequery":       true,
	"usemutation":    true,
	"usenavigate":    true,
	"uselocation":    true,
	"useparams":      true,
	"usedispatch":    true,
	"useselector":    true,
	"usetranslation": true,
	"useform":        true,
	// Generic JS/lodash plumbing routinely surfacing as a sink.
	"map":            true,
	"filter":         true,
	"foreach":        true,
	"push":           true,
	"slice":          true,
	"keys":           true,
	"values":         true,
	"entries":        true,
	"json.stringify": true,
	"json.parse":     true,
}

// ComputeCandidateConfidence scores a wire-format candidate entry on a 0..1
// scale and returns the contributing signals (for debugging + UI tooltips).
//
// The function reads keys defensively — every key is optional. Common keys it
// looks at, by surface:
//
//	Flows:    label, entry_name, step_count, cross_stack, source_file,
//	          chain_labels, is_cross_repo
//	Topology: label, broker_canonical, producers, consumers, owning_service,
//	          framework
//	Paths:    path, handler, verb, frameworks, repo, source_file,
//	          handlers_count, auth, is_webhook
//
// Hints are optional graph-level context (degree, cross-repo). Pass nil when
// you don't have it; the score still computes from entry shape alone.
//
// Scoring formula:
//
//	base = 0.5
//	+ source_file present:           +0.10
//	+ name length >= 6:              +0.05
//	+ qualified-name segments >= 2:  +0.05
//	+ ResolvedRefs >= 1:              +0.05 (cap +0.15)
//	+ HasCrossRepoEdge:               +0.10
//	+ framework hint present:         +0.05
//	+ cross_stack / is_cross_repo:    +0.10
//	+ OutboundEdges >= 3:              +0.05
//	+ explicit auth flag (paths):     +0.03
//	- noisy container name:           -0.30
//	- builtin method fragment:        -0.15 (each, cap -0.30)
//	- name length <= 2:               -0.15
//	- step_count == 1 (flows):        -0.10
//	- producers == 0 && consumers == 0 (topology): -0.15
//	- handlers_count == 0 (paths):    -0.20
//
// Result is clamped to [0, 1].
func ComputeCandidateConfidence(surface Surface, entry map[string]any, hints *ConfidenceHints) (score float64, signals []string) {
	score = 0.5
	signals = []string{"base:0.50"}
	if entry == nil {
		return 0, []string{"nil_entry"}
	}

	name := firstNonEmptyString(entry, "label", "entry_name", "handler", "name", "path")
	qualifiedName := firstNonEmptyString(entry, "qualified_name", "qualifiedName", "controller")
	sourceFile := firstNonEmptyString(entry, "source_file", "sourceFile", "file")
	framework := firstNonEmptyString(entry, "framework", "broker_canonical")

	add := func(delta float64, tag string) {
		score += delta
		signals = append(signals, tag)
	}

	// ── positive signals ────────────────────────────────────────────────
	if sourceFile != "" {
		add(0.10, "+source_file:0.10")
	}
	if l := len(name); l >= 6 {
		add(0.05, "+name_len_ge_6:0.05")
	} else if l > 0 && l <= 2 {
		add(-0.15, "-name_len_le_2:-0.15")
	}
	if qn := qualifiedName; qn != "" && strings.Count(qn, ".") >= 1 {
		add(0.05, "+qualified_name:0.05")
	}
	if framework != "" {
		add(0.05, "+framework:0.05")
	}

	// Cross-stack / cross-repo flags surfaced by the upstream collector.
	if asBool(entry["cross_stack"]) || asBool(entry["is_cross_repo"]) {
		add(0.10, "+cross_repo:0.10")
	}

	// ── hint-driven signals (optional) ──────────────────────────────────
	if hints != nil {
		if hints.HasCrossRepoEdge {
			add(0.10, "+hint_cross_repo_edge:0.10")
		}
		if hints.OutboundEdges >= 3 {
			add(0.05, "+hint_outbound_ge_3:0.05")
		}
		resolved := hints.ResolvedRefs
		if resolved > 3 {
			resolved = 3
		}
		if resolved > 0 {
			add(0.05*float64(resolved), "+hint_resolved_refs")
		}
	}

	// ── surface-specific signals ───────────────────────────────────────
	switch surface {
	case SurfaceFlows:
		sc := asInt(entry["step_count"])
		if sc == 1 {
			add(-0.10, "-flow_single_step:-0.10")
		} else if sc >= 5 {
			add(0.05, "+flow_steps_ge_5:0.05")
		}

		// Trivial-terminal discriminator (#1129). Flow labels emit as
		// "Entry → Terminal" — when the terminal is a React hook
		// (useEffect / useCallback / useMemo / …), a generic
		// JS/lodash plumbing call (map/filter/foreach/…), or a setState
		// setter (setX), the flow is almost always low-signal intra-
		// component glue rather than a real cross-stack process flow.
		terminal := extractFlowTerminal(name)
		if terminal != "" {
			lt := strings.ToLower(terminal)
			if trivialFlowTerminals[lt] {
				add(-0.35, "-flow_trivial_terminal:-0.35")
			} else if isSetterName(terminal) {
				add(-0.35, "-flow_setter_terminal:-0.35")
			}
		}

	case SurfaceTopology:
		producers := sliceLen(entry["producers"])
		consumers := sliceLen(entry["consumers"])
		if producers == 0 && consumers == 0 {
			add(-0.15, "-topology_no_edges:-0.15")
		} else if producers > 0 && consumers > 0 {
			add(0.10, "+topology_both_sides:0.10")
		}

	case SurfacePaths:
		if hc := asInt(entry["handlers_count"]); hc == 0 {
			add(-0.20, "-paths_no_handler:-0.20")
		} else if hc >= 1 {
			add(0.05, "+paths_has_handler:0.05")
		}
		if asBool(entry["auth"]) {
			add(0.03, "+paths_auth:0.03")
		}
	}

	// ── negative signals (noise) ───────────────────────────────────────
	lowerName := strings.ToLower(name)
	if noisyContainerNames[lowerName] {
		add(-0.30, "-noisy_container_name:-0.30")
	}

	if qualifiedName != "" {
		hits := 0
		for _, frag := range builtinMethodFragments {
			if strings.Contains(strings.ToLower(qualifiedName), frag) {
				hits++
			}
		}
		if hits > 0 {
			penalty := -0.15 * float64(hits)
			if penalty < -0.30 {
				penalty = -0.30
			}
			add(penalty, "-builtin_method_hit")
		}
	}

	// Clamp.
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, signals
}

// FilterResult is the partition output of FilterByConfidence.
type FilterResult struct {
	// Kept is the default visible list, sorted preserving caller's order.
	Kept []map[string]any
	// LowConfidence holds the entries that fell below the floor and were
	// not lifted by an explicit ops rank. The UI hides these by default but
	// exposes them via a "low_confidence" bucket / query parameter.
	LowConfidence []map[string]any
	// NoiseRejectedCount is len(LowConfidence) — duplicated as a top-level
	// counter so the UI can show a "N hidden as low signal" badge without
	// iterating the slice.
	NoiseRejectedCount int
	// FloorApplied is the floor value actually used (after env overrides).
	// Surfaced so the UI can label it ("Showing items with confidence >= 0.35").
	FloorApplied float64
}

// FilterByConfidence partitions `entries` into (kept, low_confidence) using
// the per-surface floor. Each entry is annotated in-place with:
//
//	"confidence":         float64  (0..1, always set)
//	"confidence_signals": []string (always set)
//	"low_confidence":     true     (only on entries that fell below the floor)
//
// Override semantics (interaction with #1666 ops):
//
//   - If ops marked the entry "disqualified": untouched — already partitioned
//     to the rejected bucket by ApplyToEntries. (We never see disqualified
//     entries here; FilterByConfidence runs on the kept slice.)
//   - If ops set an explicit "rank" > 0 on the entry: the rank "lifts" the
//     candidate above the floor regardless of its score. This is the
//     explicit-override path requested in #1129 do-step 5.
//
// hintsFor is an optional callback that returns hints for a given entry. Pass
// nil when no graph context is available (paths surface today).
func FilterByConfidence(
	surface Surface,
	entries []map[string]any,
	hintsFor func(entry map[string]any) *ConfidenceHints,
) FilterResult {
	floor := FloorFor(surface)
	result := FilterResult{
		Kept:          make([]map[string]any, 0, len(entries)),
		LowConfidence: []map[string]any{},
		FloorApplied:  floor,
	}

	for _, e := range entries {
		var hints *ConfidenceHints
		if hintsFor != nil {
			hints = hintsFor(e)
		}
		score, signals := ComputeCandidateConfidence(surface, e, hints)
		e["confidence"] = roundConfidence(score)
		e["confidence_signals"] = signals

		// Explicit rank lift: any non-zero rank wins over the floor.
		if r := asFloat(e["rank"]); r > 0 {
			result.Kept = append(result.Kept, e)
			continue
		}

		if floor <= 0 || score >= floor {
			result.Kept = append(result.Kept, e)
			continue
		}
		e["low_confidence"] = true
		result.LowConfidence = append(result.LowConfidence, e)
	}
	result.NoiseRejectedCount = len(result.LowConfidence)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Small helpers — kept private and unit-testable through FilterByConfidence.
// ─────────────────────────────────────────────────────────────────────────────

func firstNonEmptyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	}
	return false
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func sliceLen(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case []string:
		return len(x)
	case []map[string]any:
		return len(x)
	}
	return 0
}

// roundConfidence rounds the float to two decimal places so the JSON payload
// stays compact and the UI doesn't render noise digits.
func roundConfidence(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// extractFlowTerminal returns the right-hand side of the "Entry → Terminal"
// label pattern produced by the process-flow engine. Returns "" when no
// arrow separator is present.
//
// The separator is the unicode "right-arrow" character (U+2192) emitted in
// process_flow.go via the canonical " → " sequence.
func extractFlowTerminal(label string) string {
	const sep = " → "
	if i := strings.LastIndex(label, sep); i >= 0 {
		return strings.TrimSpace(label[i+len(sep):])
	}
	return ""
}

// isSetterName returns true for camelCase setter names of the shape
// "setX..." where X is upper-case. These are almost always React state
// setters which represent intra-component glue, not real downstream
// effects.
func isSetterName(name string) bool {
	if len(name) < 4 {
		return false
	}
	if !strings.HasPrefix(name, "set") {
		return false
	}
	c := name[3]
	return c >= 'A' && c <= 'Z'
}
