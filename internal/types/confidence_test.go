package types

import (
	"encoding/json"
	"math"
	"testing"
)

func TestBaseConfidence(t *testing.T) {
	tests := []struct {
		src  ConfidenceSource
		want float64
	}{
		{SourceDirectAST, 1.0},
		{SourceRegexPattern, 0.7},
		{SourceYAMLRulePattern, 0.85},
		{SourceInferredViaCallsHop, 0.95},
		{SourceInferredViaImportHop, 0.9},
		{SourceResolvedViaConstantPropagation, 0.85},
		{SourceFallbackSpeculation, 0.4},
		{ConfidenceSource("unknown"), 0.4},
	}
	for _, tt := range tests {
		got := BaseConfidence(tt.src)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("BaseConfidence(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}

func TestDecayConfidence(t *testing.T) {
	tests := []struct {
		name  string
		src   ConfidenceSource
		seed  float64
		hops  int
		want  float64
		delta float64
	}{
		// CALLS: 0.95^hops
		{"calls_0_hops", SourceInferredViaCallsHop, 1.0, 0, 1.0, 1e-9},
		{"calls_3_hops", SourceInferredViaCallsHop, 1.0, 3, 0.857375, 1e-6},
		// IMPORTS: 0.9^hops
		{"imports_2_hops", SourceInferredViaImportHop, 1.0, 2, 0.81, 1e-9},
		{"imports_3_hops", SourceInferredViaImportHop, 1.0, 3, 0.729, 1e-9},
		// Constant propagation: 0.85^hops
		{"cprop_2_hops", SourceResolvedViaConstantPropagation, 0.9, 2, 0.65025, 1e-6},
		// Non-propagation sources: hops ignored.
		{"ast_hops_ignored", SourceDirectAST, 1.0, 5, 1.0, 1e-9},
		{"regex_hops_ignored", SourceRegexPattern, 1.0, 5, 0.7, 1e-9},
		// Negative hops clamp to zero.
		{"negative_hops", SourceInferredViaCallsHop, 1.0, -3, 1.0, 1e-9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecayConfidence(tt.src, tt.seed, tt.hops)
			if math.Abs(got-tt.want) > tt.delta {
				t.Errorf("DecayConfidence(%q, %v, %d) = %v, want %v (±%v)", tt.src, tt.seed, tt.hops, got, tt.want, tt.delta)
			}
		})
	}
}

func TestEffectiveConfidence(t *testing.T) {
	// Zero/unset reads as 1.0 (default for direct AST extractors).
	if got := EffectiveConfidence(0); got != 1.0 {
		t.Errorf("EffectiveConfidence(0) = %v, want 1.0", got)
	}
	// Mid-range value passes through.
	if got := EffectiveConfidence(0.5); got != 0.5 {
		t.Errorf("EffectiveConfidence(0.5) = %v, want 0.5", got)
	}
	// Negative clamps to 0.
	if got := EffectiveConfidence(-0.5); got != 0 {
		t.Errorf("EffectiveConfidence(-0.5) = %v, want 0", got)
	}
	// >1 clamps to 1.
	if got := EffectiveConfidence(1.5); got != 1.0 {
		t.Errorf("EffectiveConfidence(1.5) = %v, want 1.0", got)
	}
}

func TestStampConfidence_Entity(t *testing.T) {
	e := EntityRecord{Kind: "function", Name: "f", SourceFile: "a.go"}
	e.StampConfidence(SourceRegexPattern)
	if e.Confidence != 0.7 {
		t.Fatalf("stamp regex: got %v, want 0.7", e.Confidence)
	}
}

func TestStampConfidence_Relationship(t *testing.T) {
	r := RelationshipRecord{FromID: "a", ToID: "b", Kind: "CALLS"}
	r.StampConfidence(SourceYAMLRulePattern)
	if r.Confidence != 0.85 {
		t.Fatalf("stamp yaml rule: got %v, want 0.85", r.Confidence)
	}
}

// TestEntityJSONOmitsEmptyConfidence guarantees zero-confidence records keep
// byte-identical JSON output to the pre-#2769 schema. This protects
// cmd/archigraph/determinism_test.go from breaking when extractors do not
// stamp confidence (the issue's "default if not specified" semantics).
func TestEntityJSONOmitsEmptyConfidence(t *testing.T) {
	e := EntityRecord{Kind: "function", Name: "f", SourceFile: "a.go"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); contains(got, `"confidence"`) {
		t.Errorf("zero confidence should be omitted from JSON, got %s", got)
	}
}

func TestRelationshipJSONOmitsEmptyConfidence(t *testing.T) {
	r := RelationshipRecord{FromID: "a", ToID: "b", Kind: "CALLS"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); contains(got, `"confidence"`) {
		t.Errorf("zero confidence should be omitted from JSON, got %s", got)
	}
}

func TestEntityValidate_ConfidenceRange(t *testing.T) {
	e := EntityRecord{Kind: "function", Name: "f", SourceFile: "a.go", Confidence: 1.5}
	if err := e.Validate(); err == nil {
		t.Fatal("expected validation error for confidence > 1.0")
	}
	e.Confidence = -0.1
	if err := e.Validate(); err == nil {
		t.Fatal("expected validation error for confidence < 0.0")
	}
	e.Confidence = 0.7
	if err := e.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// contains is a tiny strings.Contains shim (avoid the extra import on the test
// file for a single use).
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
