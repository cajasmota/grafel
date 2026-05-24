package docgen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// ResolveSectionProfile — profile selection
// ---------------------------------------------------------------------------

// TestResolveSectionProfile_ExactMatch checks that canonical kind strings
// (lower-case exact keys) resolve to their dedicated profiles.
func TestResolveSectionProfile_ExactMatch(t *testing.T) {
	cases := []struct {
		kind        string
		wantSection string // a section that must be present in the profile
		wantAbsent  string // a section that must NOT be present in the profile
	}{
		{
			kind:        "model",
			wantSection: "overview",
			wantAbsent:  "how-to-local-dev", // model pages don't need local-dev
		},
		{
			kind:        "module",
			wantSection: "how-to-local-dev", // module pages include full suite
			wantAbsent:  "",
		},
		{
			kind:        "operation",
			wantSection: "flows",
			wantAbsent:  "reference-deployment", // operation pages drop deployment
		},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(tc.kind, "")
			if len(p.Sections) == 0 {
				t.Fatalf("kind %q: expected non-empty Sections", tc.kind)
			}
			if !containsSection(p.Sections, tc.wantSection) {
				t.Errorf("kind %q: want section %q present; got %v", tc.kind, tc.wantSection, p.Sections)
			}
			if tc.wantAbsent != "" && containsSection(p.Sections, tc.wantAbsent) {
				t.Errorf("kind %q: section %q should be absent; got %v", tc.kind, tc.wantAbsent, p.Sections)
			}
		})
	}
}

// TestResolveSectionProfile_CaseInsensitive ensures that PascalCase and
// UPPER-CASE kind strings resolve to the same profile as the lower-case key.
func TestResolveSectionProfile_CaseInsensitive(t *testing.T) {
	cases := []string{"Model", "MODEL", "Module", "MODULE", "Operation", "OPERATION"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			// Must not return the default (all-13-section) profile for these known kinds.
			if len(p.Sections) == len(docgen.KnownSections) {
				// Verify it is not the default by checking that a kind-specific
				// section is absent or a non-default guidance override exists.
				// If the profile has 13 sections AND no overrides, it is the default.
				if p.GuidanceOverrides == nil {
					t.Errorf("kind %q: resolved to default profile (no overrides, 13 sections)", kind)
				}
			}
		})
	}
}

// TestResolveSectionProfile_DottedPrefix covers graph kinds like "SCOPE.Model"
// and "SCOPE.Module" that the extractor may emit.
func TestResolveSectionProfile_DottedPrefix(t *testing.T) {
	cases := []struct {
		kind        string
		wantAbsent  string
	}{
		{"SCOPE.Model", "how-to-local-dev"},
		{"SCOPE.Module", ""},
		{"SCOPE.Operation", "reference-deployment"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(tc.kind, "")
			if len(p.Sections) == 0 {
				t.Fatalf("kind %q: expected non-empty Sections", tc.kind)
			}
			if tc.wantAbsent != "" && containsSection(p.Sections, tc.wantAbsent) {
				t.Errorf("kind %q: section %q should be absent; got %v", tc.kind, tc.wantAbsent, p.Sections)
			}
			// Must have at least overview.
			if !containsSection(p.Sections, "overview") {
				t.Errorf("kind %q: missing required section %q", tc.kind, "overview")
			}
		})
	}
}

// TestResolveSectionProfile_FallbackToDefault verifies that unknown kinds
// return the default profile (all KnownSections, no overrides).
func TestResolveSectionProfile_FallbackToDefault(t *testing.T) {
	unknownKinds := []string{"", "Widget", "ThingamaBob", "UNKNOWN_KIND", "xyz"}
	for _, kind := range unknownKinds {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			if len(p.Sections) != len(docgen.KnownSections) {
				t.Errorf("unknown kind %q: want %d sections (default), got %d: %v",
					kind, len(docgen.KnownSections), len(p.Sections), p.Sections)
			}
			for _, ks := range docgen.KnownSections {
				if !containsSection(p.Sections, ks) {
					t.Errorf("unknown kind %q: default profile missing section %q", kind, ks)
				}
			}
		})
	}
}

// TestResolveSectionProfile_NoDuplicateSections verifies no section appears
// twice in any profile's Sections list.
func TestResolveSectionProfile_NoDuplicateSections(t *testing.T) {
	for _, kind := range []string{"model", "module", "operation", "", "UNKNOWN"} {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			seen := make(map[string]int)
			for _, s := range p.Sections {
				seen[s]++
			}
			for s, count := range seen {
				if count > 1 {
					t.Errorf("kind %q: duplicate section %q (count=%d)", kind, s, count)
				}
			}
		})
	}
}

// TestResolveSectionProfile_LanguageParamIgnored checks that the language
// parameter does not affect profile selection in Wave 1 (language-aware
// profiles are a follow-up feature).
func TestResolveSectionProfile_LanguageParamIgnored(t *testing.T) {
	for _, lang := range []string{"", "go", "ruby", "python", "typescript"} {
		p1 := docgen.ResolveSectionProfile("model", lang)
		p2 := docgen.ResolveSectionProfile("model", "")
		if len(p1.Sections) != len(p2.Sections) {
			t.Errorf("language %q changed section count for model: %d vs %d", lang, len(p1.Sections), len(p2.Sections))
		}
	}
}

// ---------------------------------------------------------------------------
// SectionProfile.GuidanceOverrides — kind-specific guidance
// ---------------------------------------------------------------------------

// TestResolveGuidance_OverrideTakesPrecedence verifies that a kind-specific
// guidance override wins over defaultSectionGuidance for known profiles.
func TestResolveGuidance_OverrideTakesPrecedence(t *testing.T) {
	// Model "overview" guidance is overridden — it must mention "data model" or
	// "persisted", distinguishing it from the generic overview text.
	modelProfile := docgen.ResolveSectionProfile("model", "")
	g := docgen.ResolveGuidance(modelProfile, "overview")
	if !strings.Contains(strings.ToLower(g), "model") && !strings.Contains(strings.ToLower(g), "persist") {
		t.Errorf("model overview guidance should be model-specific; got: %q", g)
	}

	// Module "how-to-local-dev" is overridden — it must mention "module".
	moduleProfile := docgen.ResolveSectionProfile("module", "")
	g = docgen.ResolveGuidance(moduleProfile, "how-to-local-dev")
	if !strings.Contains(strings.ToLower(g), "module") && !strings.Contains(strings.ToLower(g), "step") {
		t.Errorf("module how-to-local-dev guidance should be module-specific; got: %q", g)
	}

	// Operation "flows" is overridden — must mention "operation" or "execution".
	opProfile := docgen.ResolveSectionProfile("operation", "")
	g = docgen.ResolveGuidance(opProfile, "flows")
	if !strings.Contains(strings.ToLower(g), "operation") && !strings.Contains(strings.ToLower(g), "execut") {
		t.Errorf("operation flows guidance should be operation-specific; got: %q", g)
	}
}

// TestResolveGuidance_FallsBackToDefault verifies that when a profile has no
// override for a section, the default guidance is returned.
func TestResolveGuidance_FallsBackToDefault(t *testing.T) {
	defaultProfile := docgen.ResolveSectionProfile("UNKNOWN_KIND", "")
	for _, sec := range docgen.KnownSections {
		g := docgen.ResolveGuidance(defaultProfile, sec)
		if g == "" {
			t.Errorf("default profile section %q returned empty guidance", sec)
		}
		if g == "_No guidance available for this section type._" {
			t.Errorf("default profile section %q fell through to sentinel; all KnownSections should have guidance", sec)
		}
	}
}

// TestResolveGuidance_SentinelForUnknownSection verifies the sentinel value is
// returned for completely unknown section names.
func TestResolveGuidance_SentinelForUnknownSection(t *testing.T) {
	defaultProfile := docgen.ResolveSectionProfile("UNKNOWN_KIND", "")
	g := docgen.ResolveGuidance(defaultProfile, "this-section-does-not-exist")
	const sentinel = "_No guidance available for this section type._"
	if g != sentinel {
		t.Errorf("unknown section: want %q, got %q", sentinel, g)
	}
}

// ---------------------------------------------------------------------------
// SectionsForEntityKind — backward-compatibility bridge
// ---------------------------------------------------------------------------

// TestSectionsForEntityKind_DelegatesProfile verifies that SectionsForEntityKind
// returns the same section list as ResolveSectionProfile for the same kind.
func TestSectionsForEntityKind_DelegatesProfile(t *testing.T) {
	for _, kind := range []string{"model", "module", "operation", "SCOPE.Module", "", "Unknown"} {
		t.Run(kind, func(t *testing.T) {
			want := docgen.ResolveSectionProfile(kind, "").Sections
			got := docgen.SectionsForEntityKind(kind)
			if len(want) != len(got) {
				t.Errorf("kind %q: want %d sections, got %d", kind, len(want), len(got))
				return
			}
			for i := range want {
				if want[i] != got[i] {
					t.Errorf("kind %q: section[%d] want %q got %q", kind, i, want[i], got[i])
				}
			}
		})
	}
}

// TestSectionsForEntityKind_ModuleHasFullSuite verifies that Module kind returns
// all KnownSections except "child-methods" (which is a class/view-level concern).
// Module pages are the entry point and legitimately use the full reference suite
// (deployment, scripts, how-to-local-dev), but they do not have a per-method table.
func TestSectionsForEntityKind_ModuleHasFullSuite(t *testing.T) {
	secs := docgen.SectionsForEntityKind("module")
	// child-methods is intentionally absent from the module profile.
	if containsSection(secs, "child-methods") {
		t.Errorf("module sections should NOT include child-methods (class/view concern); got %v", secs)
	}
	// All other KnownSections must be present.
	for _, ks := range docgen.KnownSections {
		if ks == "child-methods" {
			continue
		}
		if !containsSection(secs, ks) {
			t.Errorf("module: missing expected section %q; got %v", ks, secs)
		}
	}
}

// TestSectionsForEntityKind_ModelDropsDeploymentSections verifies that
// Model kind does NOT include reference-deployment, reference-scripts, or
// how-to-local-dev (those are module-level concerns).
func TestSectionsForEntityKind_ModelDropsDeploymentSections(t *testing.T) {
	secs := docgen.SectionsForEntityKind("model")
	mustAbsent := []string{"reference-deployment", "reference-scripts", "how-to-local-dev"}
	for _, s := range mustAbsent {
		if containsSection(secs, s) {
			t.Errorf("model sections should not include %q (module-level concern); got %v", s, secs)
		}
	}
}

// TestSectionsForEntityKind_OperationDropsDeploymentSections verifies that
// Operation kind does NOT include reference-deployment, reference-scripts, or
// how-to-local-dev.
func TestSectionsForEntityKind_OperationDropsDeploymentSections(t *testing.T) {
	secs := docgen.SectionsForEntityKind("operation")
	mustAbsent := []string{"reference-deployment", "reference-scripts", "how-to-local-dev"}
	for _, s := range mustAbsent {
		if containsSection(secs, s) {
			t.Errorf("operation sections should not include %q (module-level concern); got %v", s, secs)
		}
	}
}

// TestSectionsForEntityKind_UnknownFallsToAllSections confirms backward compat:
// unknown kinds return the full 13-section list.
func TestSectionsForEntityKind_UnknownFallsToAllSections(t *testing.T) {
	for _, kind := range []string{"", "Widget", "ThingamaBob"} {
		t.Run(kind, func(t *testing.T) {
			secs := docgen.SectionsForEntityKind(kind)
			if len(secs) != len(docgen.KnownSections) {
				t.Errorf("unknown kind %q: want %d sections, got %d", kind, len(docgen.KnownSections), len(secs))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #1986 — size-aware Operation profiles
// ---------------------------------------------------------------------------

// TestResolveSectionProfile_OperationSmall verifies that an operation with
// fewer than 30 lines resolves to the "small" profile which omits the three
// heavy infrastructure sections.
func TestResolveSectionProfile_OperationSmall(t *testing.T) {
	p := docgen.ResolveSectionProfile("operation", "", 15)
	mustAbsent := []string{"reference-deployment", "how-to-local-dev", "reference-scripts"}
	for _, s := range mustAbsent {
		if containsSection(p.Sections, s) {
			t.Errorf("operation(small): section %q should be absent; got %v", s, p.Sections)
		}
	}
	// Must still have core sections.
	mustPresent := []string{"overview", "capabilities", "api", "flows"}
	for _, s := range mustPresent {
		if !containsSection(p.Sections, s) {
			t.Errorf("operation(small): missing required section %q; got %v", s, p.Sections)
		}
	}
}

// TestResolveSectionProfile_OperationSmallGuidanceTerse verifies that the
// small-tier capabilities guidance is shorter / tighter than the medium tier.
func TestResolveSectionProfile_OperationSmallGuidanceTerse(t *testing.T) {
	small := docgen.ResolveSectionProfile("operation", "", 10)
	medium := docgen.ResolveSectionProfile("operation", "", 50)
	gs := docgen.ResolveGuidance(small, "capabilities")
	gm := docgen.ResolveGuidance(medium, "capabilities")
	if gs == gm {
		t.Errorf("operation small/medium capabilities guidance should differ; both returned %q", gs)
	}
}

// TestResolveSectionProfile_OperationMedium verifies that the medium tier
// (30–149 lines) returns the baseline operation profile without deployment
// sections but without the stripped-down small profile either.
func TestResolveSectionProfile_OperationMedium(t *testing.T) {
	p := docgen.ResolveSectionProfile("operation", "", 75)
	mustAbsent := []string{"reference-deployment", "how-to-local-dev", "reference-scripts"}
	for _, s := range mustAbsent {
		if containsSection(p.Sections, s) {
			t.Errorf("operation(medium): section %q should be absent; got %v", s, p.Sections)
		}
	}
	// Medium has reference-dependencies; small drops it.
	if !containsSection(p.Sections, "reference-dependencies") {
		t.Errorf("operation(medium): want reference-dependencies; got %v", p.Sections)
	}
}

// TestResolveSectionProfile_OperationLarge verifies that an operation with
// 150+ lines gets the full template including deployment and local-dev.
func TestResolveSectionProfile_OperationLarge(t *testing.T) {
	p := docgen.ResolveSectionProfile("operation", "", 200)
	mustPresent := []string{
		"overview", "capabilities", "flows", "patterns", "api",
		"reference-config", "reference-dependencies",
		"reference-deployment", "reference-scripts", "how-to-local-dev",
		"reference-misc", "glossary", "module-readme",
	}
	for _, s := range mustPresent {
		if !containsSection(p.Sections, s) {
			t.Errorf("operation(large): missing section %q; got %v", s, p.Sections)
		}
	}
}

// TestResolveSectionProfile_OperationLargeGuidanceDeep verifies that the large
// operation overview guidance explicitly mentions orchestration or critical-path.
func TestResolveSectionProfile_OperationLargeGuidanceDeep(t *testing.T) {
	p := docgen.ResolveSectionProfile("operation", "", 300)
	g := docgen.ResolveGuidance(p, "overview")
	if !strings.Contains(strings.ToLower(g), "orchestrat") && !strings.Contains(strings.ToLower(g), "critical") {
		t.Errorf("operation(large) overview guidance should mention orchestration or critical-path; got %q", g)
	}
}

// TestResolveSectionProfile_OperationNoLineCount verifies that when no
// lineCount is passed, the medium (baseline) Operation profile is returned.
func TestResolveSectionProfile_OperationNoLineCount(t *testing.T) {
	withoutCount := docgen.ResolveSectionProfile("operation", "")
	withZero := docgen.ResolveSectionProfile("operation", "", 0)
	medium := docgen.ResolveSectionProfile("operation", "", 50)
	for _, p := range []docgen.SectionProfile{withoutCount, withZero} {
		if len(p.Sections) != len(medium.Sections) {
			t.Errorf("operation without lineCount: want medium section count %d, got %d",
				len(medium.Sections), len(p.Sections))
		}
	}
}

// TestResolveSectionProfile_OperationBoundary_29_30_149_150 verifies the exact
// boundary values for tier transitions.
func TestResolveSectionProfile_OperationBoundary(t *testing.T) {
	cases := []struct {
		lines        int
		wantSmall    bool // expects "operation.small" (no reference-deployment)
		wantLarge    bool // expects "operation.large" (has reference-deployment)
	}{
		{29, true, false},
		{30, false, false},
		{149, false, false},
		{150, false, true},
	}
	for _, tc := range cases {
		p := docgen.ResolveSectionProfile("operation", "", tc.lines)
		hasDeployment := containsSection(p.Sections, "reference-deployment")
		hasRefDeps := containsSection(p.Sections, "reference-dependencies")

		if tc.wantSmall {
			// small: no reference-deployment, no reference-dependencies
			if hasDeployment {
				t.Errorf("lines=%d: small tier should not have reference-deployment", tc.lines)
			}
			if hasRefDeps {
				t.Errorf("lines=%d: small tier should not have reference-dependencies", tc.lines)
			}
		} else if tc.wantLarge {
			// large: has reference-deployment
			if !hasDeployment {
				t.Errorf("lines=%d: large tier should have reference-deployment", tc.lines)
			}
		} else {
			// medium: no reference-deployment, has reference-dependencies
			if hasDeployment {
				t.Errorf("lines=%d: medium tier should not have reference-deployment", tc.lines)
			}
			if !hasRefDeps {
				t.Errorf("lines=%d: medium tier should have reference-dependencies", tc.lines)
			}
		}
	}
}

// TestResolveSectionProfile_OperationScopedKind verifies that dotted-prefix
// kinds like "SCOPE.Operation" still participate in size-tier selection.
func TestResolveSectionProfile_OperationScopedKind(t *testing.T) {
	small := docgen.ResolveSectionProfile("SCOPE.Operation", "", 10)
	if containsSection(small.Sections, "reference-deployment") {
		t.Errorf("SCOPE.Operation(small): reference-deployment should be absent")
	}
	large := docgen.ResolveSectionProfile("SCOPE.Operation", "", 200)
	if !containsSection(large.Sections, "reference-deployment") {
		t.Errorf("SCOPE.Operation(large): reference-deployment should be present")
	}
}

// ---------------------------------------------------------------------------
// #1970 — react_component api section guidance
// ---------------------------------------------------------------------------

// TestResolveSectionProfile_ReactComponent_ExactMatch verifies that
// "react_component" resolves to a dedicated profile.
func TestResolveSectionProfile_ReactComponent_ExactMatch(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	if len(p.Sections) == 0 {
		t.Fatal("react_component: expected non-empty Sections")
	}
	if p.GuidanceOverrides == nil {
		t.Fatal("react_component: expected GuidanceOverrides to be set")
	}
	mustPresent := []string{"overview", "capabilities", "api"}
	for _, s := range mustPresent {
		if !containsSection(p.Sections, s) {
			t.Errorf("react_component: missing required section %q; got %v", s, p.Sections)
		}
	}
	// React components don't need deployment sections.
	mustAbsent := []string{"reference-deployment", "how-to-local-dev", "reference-scripts"}
	for _, s := range mustAbsent {
		if containsSection(p.Sections, s) {
			t.Errorf("react_component: section %q should be absent; got %v", s, p.Sections)
		}
	}
}

// TestResolveSectionProfile_ReactComponent_ApiGuidanceIsPropsInterface verifies
// that the api guidance for react_component specifically references props,
// not the generic function-signature template.
func TestResolveSectionProfile_ReactComponent_ApiGuidanceIsPropsInterface(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)
	mustContain := []string{"props", "jsx.element", "children"}
	for _, word := range mustContain {
		if !strings.Contains(lower, word) {
			t.Errorf("react_component api guidance: want %q; guidance was: %q", word, g)
		}
	}
	if strings.Contains(lower, "never reuse the generic function-signature") {
		// The instruction must be in the guidance.
	} else if !strings.Contains(lower, "never") {
		t.Errorf("react_component api guidance: should include NEVER-reuse instruction; got: %q", g)
	}
}

// TestResolveSectionProfile_ReactComponent_TypeScriptAndJSDoc verifies that
// the api guidance mentions both TypeScript interface and JSDoc @param sources.
func TestResolveSectionProfile_ReactComponent_TypeScriptAndJSDoc(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)
	if !strings.Contains(lower, "typescript") && !strings.Contains(lower, "ts") {
		t.Errorf("react_component api guidance: should reference TypeScript; got: %q", g)
	}
	if !strings.Contains(lower, "jsdoc") && !strings.Contains(lower, "@param") {
		t.Errorf("react_component api guidance: should reference JSDoc/@param; got: %q", g)
	}
}

// TestResolveSectionProfile_ReactComponent_NotDefaultProfile verifies that
// react_component does NOT resolve to the default profile.
func TestResolveSectionProfile_ReactComponent_NotDefaultProfile(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	if len(p.Sections) == len(docgen.KnownSections) && p.GuidanceOverrides == nil {
		t.Error("react_component resolved to default profile (all 13 sections, no overrides)")
	}
}

// ---------------------------------------------------------------------------
// #1992 — module-readme boundary guidance (no fabricated siblings)
// ---------------------------------------------------------------------------

// TestModuleReadmeBoundaryGuidance_AllProfiles verifies that every explicit
// profile's module-readme guidance contains the sibling-boundary instruction.
func TestModuleReadmeBoundaryGuidance_AllProfiles(t *testing.T) {
	profileKinds := []string{"model", "module", "operation", "react_component"}
	// Also check size-aware tiers.
	operationWithSizes := []struct {
		kind      string
		lineCount int
	}{
		{"operation", 10},  // small
		{"operation", 50},  // medium
		{"operation", 200}, // large
	}

	check := func(t *testing.T, label string, p docgen.SectionProfile) {
		t.Helper()
		if !containsSection(p.Sections, "module-readme") {
			t.Skipf("%s: profile does not include module-readme section", label)
		}
		g := docgen.ResolveGuidance(p, "module-readme")
		lower := strings.ToLower(g)
		if !strings.Contains(lower, "module_manifest") {
			t.Errorf("%s module-readme guidance: missing module_manifest boundary; got: %q", label, g)
		}
		if !strings.Contains(lower, "neighbour_briefs") {
			t.Errorf("%s module-readme guidance: missing neighbour_briefs boundary; got: %q", label, g)
		}
		if !strings.Contains(lower, "do not mention") {
			t.Errorf("%s module-readme guidance: missing 'do not mention' instruction; got: %q", label, g)
		}
	}

	for _, kind := range profileKinds {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			check(t, kind, p)
		})
	}
	for _, tc := range operationWithSizes {
		label := fmt.Sprintf("%s(lines=%d)", tc.kind, tc.lineCount)
		t.Run(label, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(tc.kind, "", tc.lineCount)
			check(t, label, p)
		})
	}
}

// TestModuleReadmeBoundaryGuidance_CitationInstruction verifies that the
// boundary guidance also instructs the LLM to cite the bundle field when a
// sibling is mentioned.
func TestModuleReadmeBoundaryGuidance_CitationInstruction(t *testing.T) {
	for _, kind := range []string{"model", "module", "operation", "react_component"} {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			if !containsSection(p.Sections, "module-readme") {
				t.Skipf("%s: no module-readme section", kind)
			}
			g := docgen.ResolveGuidance(p, "module-readme")
			lower := strings.ToLower(g)
			if !strings.Contains(lower, "bundle field") && !strings.Contains(lower, "came from") {
				t.Errorf("%s module-readme guidance: missing citation instruction; got: %q", kind, g)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsSection(sections []string, s string) bool {
	for _, sec := range sections {
		if sec == s {
			return true
		}
	}
	return false
}
