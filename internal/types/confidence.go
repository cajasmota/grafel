// Package types — confidence overlay (Phase 1C, issue #2769).
//
// Every entity and relationship the indexer emits carries a confidence value
// in [0.0, 1.0] reflecting how certain the extraction is. The taxonomy below
// is universal: the same rules apply across every supported language.
//
// Storage:
//   - On EntityRecord / RelationshipRecord (extractor emission contract):
//     a Confidence float64 field with JSON tag "confidence,omitempty".
//   - On graph.Entity / graph.Relationship (on-disk schema): same field name.
//   - Default value (zero / unset) is interpreted by readers as 1.0 — see
//     EffectiveConfidence below.
//
// Propagation passes (Phase 0 constant propagation, Phase 1A effect
// propagation, future passes) MUST call DecayConfidence with the appropriate
// source so each additional hop reduces the value geometrically.
//
// Filtering:
//   - MCP tools accept a `min_confidence` argument (float, default 0.0). When
//     non-zero, results whose effective confidence falls below the threshold
//     are dropped. See internal/mcp/tools.go::argFloat and the per-tool
//     wiring in internal/mcp/server.go.
package types

import "math"

// ConfidenceSource enumerates how a record was produced. The numeric value of
// each source is its base confidence at hop=0.
type ConfidenceSource string

const (
	// SourceDirectAST is a tree-sitter / parser-level structural match. The
	// extractor saw the exact syntactic form (a function declaration, a class
	// body, an import statement). Confidence: 1.0.
	SourceDirectAST ConfidenceSource = "direct_ast"

	// SourceRegexPattern is a regex-based extraction. The extractor matched a
	// textual pattern without confirming AST structure. Used by a handful of
	// engine producers where tree-sitter does not yet expose the grammar
	// (e.g. Rails routes DSL, Gorm tag literals). Confidence: 0.7.
	SourceRegexPattern ConfidenceSource = "regex_pattern"

	// SourceYAMLRulePattern is a rule-pack match — the YAML in
	// internal/engine/rules/*.yaml drives the emission. Stronger than raw
	// regex because the rule pack is curated and reviewed. Confidence: 0.85.
	SourceYAMLRulePattern ConfidenceSource = "yaml_rule_pattern"

	// SourceInferredViaCallsHop is the result of walking the CALLS graph
	// from a high-confidence seed. Each hop multiplies confidence by 0.95.
	SourceInferredViaCallsHop ConfidenceSource = "inferred_via_calls_hop"

	// SourceInferredViaImportHop is the result of walking the IMPORTS graph.
	// Each hop multiplies confidence by 0.9.
	SourceInferredViaImportHop ConfidenceSource = "inferred_via_import_hop"

	// SourceResolvedViaConstantPropagation is the result of Phase 0
	// constant-binding propagation (#2761/#2781). Each propagation hop
	// multiplies confidence by 0.85.
	SourceResolvedViaConstantPropagation ConfidenceSource = "resolved_via_constant_propagation"

	// SourceFallbackSpeculation is a low-confidence guess made when no
	// better signal exists (e.g. inferred class shadow). Confidence: 0.4.
	// Filtering callers should set min_confidence > 0.4 to exclude these.
	SourceFallbackSpeculation ConfidenceSource = "fallback_speculation"
)

// BaseConfidence returns the per-source base confidence at hop=0.
// For sources without a hop dimension this is the final value.
func BaseConfidence(src ConfidenceSource) float64 {
	switch src {
	case SourceDirectAST:
		return 1.0
	case SourceRegexPattern:
		return 0.7
	case SourceYAMLRulePattern:
		return 0.85
	case SourceInferredViaCallsHop:
		return 0.95
	case SourceInferredViaImportHop:
		return 0.9
	case SourceResolvedViaConstantPropagation:
		return 0.85
	case SourceFallbackSpeculation:
		return 0.4
	default:
		// Unknown source — treat as the most pessimistic well-defined value.
		// Callers should never pass an unknown source; this branch exists so a
		// typo in a new extractor cannot silently promote noise to 1.0.
		return 0.4
	}
}

// DecayConfidence computes the confidence after N hops from a seed of value
// `seed` along the given propagation source. For non-propagation sources
// (direct_ast, regex_pattern, yaml_rule_pattern, fallback_speculation), hops
// is ignored and the base value is returned.
//
//	DecayConfidence(SourceInferredViaCallsHop, 1.0, 3) = 1.0 * 0.95^3 ≈ 0.857
//	DecayConfidence(SourceResolvedViaConstantPropagation, 0.9, 2) = 0.9 * 0.85^2 = 0.65
func DecayConfidence(src ConfidenceSource, seed float64, hops int) float64 {
	if hops < 0 {
		hops = 0
	}
	if seed <= 0 {
		seed = 1.0
	}
	if seed > 1.0 {
		seed = 1.0
	}
	switch src {
	case SourceInferredViaCallsHop:
		return clamp01(seed * math.Pow(0.95, float64(hops)))
	case SourceInferredViaImportHop:
		return clamp01(seed * math.Pow(0.9, float64(hops)))
	case SourceResolvedViaConstantPropagation:
		return clamp01(seed * math.Pow(0.85, float64(hops)))
	default:
		// Non-propagation sources: hop is meaningless; return base.
		return clamp01(BaseConfidence(src))
	}
}

// EffectiveConfidence returns the read-side confidence value for a record.
// A zero (unset) value is interpreted as 1.0 so legacy graphs and extractors
// that never call StampConfidence keep working — the issue explicitly says
// "default if not specified: 1.0 for direct AST matches".
//
// Negative and >1.0 values are clamped to [0, 1].
func EffectiveConfidence(stored float64) float64 {
	if stored == 0 {
		return 1.0
	}
	return clamp01(stored)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// StampEntity sets the Confidence field on an EntityRecord from a
// ConfidenceSource. Helper kept here so extractors don't import math.
func (e *EntityRecord) StampConfidence(src ConfidenceSource) {
	e.Confidence = BaseConfidence(src)
}

// StampConfidence sets the Confidence field on a RelationshipRecord.
func (r *RelationshipRecord) StampConfidence(src ConfidenceSource) {
	r.Confidence = BaseConfidence(src)
}

// StampConfidence sets the Confidence field on the SQS-message Relationship.
func (r *Relationship) StampConfidence(src ConfidenceSource) {
	r.Confidence = BaseConfidence(src)
}
