package docgen

import "testing"

// Issue #1995 — Java SCOPE.Component (class / interface / enum) entities
// MUST resolve to the whole-body source_window strategy so multi-method
// controller classes are not truncated to their first ~43 lines. The
// W5R2 reproducer was TransfersController (10 methods, ~250 lines) whose
// default ±20-line window clipped 9 of 10 method bodies.
//
// Profile lookup is keyed off (kind contains "component") AND
// language=="java". Non-Java Components keep the default profile so
// React component / Go struct rendering is unaffected.
func TestResolveSectionProfile_JavaComponentUsesWholeBody(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		language string
		wantWB   bool
	}{
		{"java class via SCOPE.Component", "SCOPE.Component", "java", true},
		{"java class via bare Component", "Component", "java", true},
		{"java class via lowercase", "component", "java", true},
		{"python class — NOT java, default", "SCOPE.Component", "python", false},
		{"js class — NOT java, default", "SCOPE.Component", "javascript", false},
		{"no language hint — default", "SCOPE.Component", "", false},
		// Operation kinds must NOT pick up the java-component profile even
		// when language=java — they have their own size-aware tiers.
		{"java operation does not pick up component profile", "SCOPE.Operation", "java", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ResolveSectionProfile(tc.kind, tc.language)
			gotWB := p.SourceWindowStrategy == SourceWindowStrategyWholeBody
			if gotWB != tc.wantWB {
				t.Errorf("ResolveSectionProfile(%q, %q): SourceWindowStrategy=%q, want WholeBody=%v",
					tc.kind, tc.language, p.SourceWindowStrategy, tc.wantWB)
			}
		})
	}
}
