package docgen_test

import (
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
// all 13 sections (the one kind that legitimately uses the full suite).
func TestSectionsForEntityKind_ModuleHasFullSuite(t *testing.T) {
	secs := docgen.SectionsForEntityKind("module")
	if len(secs) != len(docgen.KnownSections) {
		t.Errorf("module: want all %d sections, got %d: %v", len(docgen.KnownSections), len(secs), secs)
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
