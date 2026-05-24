package docgen_test

// section_guidance_content_test.go — tests for section guidance bundle C
// (content enrichment).  Covers:
//
//   - #1858 — module-readme + how-to-local-dev guidance references module_configs[]
//     for grounding repo-specific prose (module + js_module profiles).
//   - #1859 — patterns guidance surfaces anti-patterns / code smells from
//     source_window in addition to positive structural patterns (all profiles).
//   - #1863 — child-methods section added to view + class profiles with tabular
//     method-index guidance.
//   - #1875 — model api guidance: ORM-Model-no-callable-API branch.
//   - #1882 — module api guidance: catalog mode (URL prefix + verb breakdown).
//   - #1883 — module flows guidance: 2-3 archetypal mermaid diagrams.

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contentContains(t *testing.T, guidance, label, word string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(guidance), strings.ToLower(word)) {
		t.Errorf("%s: want %q in guidance; got:\n%s", label, word, guidance)
	}
}

// ---------------------------------------------------------------------------
// #1858 — sibling-file manifest (module_configs) grounding
// ---------------------------------------------------------------------------

// TestModuleReadme_ModuleConfigsGrounding verifies that the module profile's
// module-readme guidance instructs the LLM to consume module_configs[].
func TestModuleReadme_ModuleConfigsGrounding(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "module-readme")
	lower := strings.ToLower(g)
	for _, want := range []string{"module_configs", "repo-specific"} {
		if !strings.Contains(lower, want) {
			t.Errorf("module module-readme: want %q in guidance; got:\n%s", want, g)
		}
	}
}

// TestHowToLocalDev_ModuleConfigsGrounding verifies that the module profile's
// how-to-local-dev guidance instructs the LLM to consume module_configs[].
func TestHowToLocalDev_ModuleConfigsGrounding(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "how-to-local-dev")
	lower := strings.ToLower(g)
	for _, want := range []string{"module_configs", "repo-specific"} {
		if !strings.Contains(lower, want) {
			t.Errorf("module how-to-local-dev: want %q in guidance; got:\n%s", want, g)
		}
	}
}

// TestJSModuleReadme_ModuleConfigsGrounding verifies that the js_module profile's
// module-readme guidance also instructs the LLM to consume module_configs[].
func TestJSModuleReadme_ModuleConfigsGrounding(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "module-readme")
	lower := strings.ToLower(g)
	for _, want := range []string{"module_configs", "repo-specific"} {
		if !strings.Contains(lower, want) {
			t.Errorf("js_module module-readme: want %q in guidance; got:\n%s", want, g)
		}
	}
}

// TestHowToLocalDev_NoBoilerplateInstruction verifies that the module
// how-to-local-dev guidance explicitly discourages generic boilerplate.
func TestHowToLocalDev_NoBoilerplateInstruction(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "how-to-local-dev")
	if !strings.Contains(strings.ToLower(g), "boilerplate") {
		t.Errorf("module how-to-local-dev: should discourage generic boilerplate; got:\n%s", g)
	}
}

// ---------------------------------------------------------------------------
// #1859 — anti-patterns / smells in patterns section
// ---------------------------------------------------------------------------

// TestDefaultPatterns_AntiPatternsGuidance verifies that the default patterns
// guidance in defaultSectionGuidance mentions anti-patterns/smells.
func TestDefaultPatterns_AntiPatternsGuidance(t *testing.T) {
	// Use a kind not explicitly profiled to hit defaultSectionGuidance.
	p := docgen.ResolveSectionProfile("UNKNOWN_KIND", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("default patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestModelPatterns_AntiPatternsGuidance verifies that the model profile's
// patterns guidance includes anti-patterns and smells.
func TestModelPatterns_AntiPatternsGuidance(t *testing.T) {
	p := docgen.ResolveSectionProfile("model", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("model patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestModulePatterns_AntiPatternsGuidance verifies that the module profile's
// patterns guidance includes anti-patterns and smells.
func TestModulePatterns_AntiPatternsGuidance(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("module patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestViewPatterns_AntiPatternsGuidance verifies that the view profile's
// patterns guidance includes anti-patterns and smells.
func TestViewPatterns_AntiPatternsGuidance(t *testing.T) {
	p := docgen.ResolveSectionProfile("view", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("view patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestClassPatterns_AntiPatternsGuidance verifies that the class profile's
// patterns guidance includes anti-patterns and smells.
func TestClassPatterns_AntiPatternsGuidance(t *testing.T) {
	p := docgen.ResolveSectionProfile("class", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("class patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestJSModulePatterns_AntiPatternsGuidance verifies that the js_module profile's
// patterns guidance includes anti-patterns and smells.
func TestJSModulePatterns_AntiPatternsGuidance(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)
	wantTerms := []string{"anti-pattern", "smell"}
	found := false
	for _, term := range wantTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("js_module patterns guidance: want anti-pattern/smell mention; got:\n%s", g)
	}
}

// TestPatterns_SourceWindowCitationInstruction verifies that the profiles that
// have overridden patterns guidance ask the LLM to cite line ranges.
func TestPatterns_SourceWindowCitationInstruction(t *testing.T) {
	for _, kind := range []string{"model", "module", "view", "class", "js_module"} {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			g := docgen.ResolveGuidance(p, "patterns")
			lower := strings.ToLower(g)
			if !strings.Contains(lower, "source_window") && !strings.Contains(lower, "line range") {
				t.Errorf("%s patterns guidance: should instruct to cite from source_window; got:\n%s", kind, g)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #1863 — child-methods section in view + class profiles
// ---------------------------------------------------------------------------

// TestChildMethodsInKnownSections verifies that "child-methods" is a known section.
func TestChildMethodsInKnownSections(t *testing.T) {
	if !containsSection(docgen.KnownSections, "child-methods") {
		t.Errorf("child-methods must be in KnownSections; got %v", docgen.KnownSections)
	}
}

// TestViewProfile_HasChildMethods verifies that the view profile includes
// the child-methods section.
func TestViewProfile_HasChildMethods(t *testing.T) {
	p := docgen.ResolveSectionProfile("view", "")
	if !containsSection(p.Sections, "child-methods") {
		t.Errorf("view profile: want child-methods section; got %v", p.Sections)
	}
}

// TestClassProfile_HasChildMethods verifies that the class profile includes
// the child-methods section.
func TestClassProfile_HasChildMethods(t *testing.T) {
	p := docgen.ResolveSectionProfile("class", "")
	if !containsSection(p.Sections, "child-methods") {
		t.Errorf("class profile: want child-methods section; got %v", p.Sections)
	}
}

// TestModuleProfile_NoChildMethods verifies that the module profile does NOT
// include child-methods (module pages are not class-level).
func TestModuleProfile_NoChildMethods(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	if containsSection(p.Sections, "child-methods") {
		t.Errorf("module profile: child-methods should be absent; got %v", p.Sections)
	}
}

// TestModelProfile_NoChildMethods verifies that the model profile does NOT
// include child-methods.
func TestModelProfile_NoChildMethods(t *testing.T) {
	p := docgen.ResolveSectionProfile("model", "")
	if containsSection(p.Sections, "child-methods") {
		t.Errorf("model profile: child-methods should be absent; got %v", p.Sections)
	}
}

// TestViewChildMethodsGuidance_TableColumns verifies that the view profile's
// child-methods guidance mentions HTTP-specific columns (Method, HTTP Verb, Path).
func TestViewChildMethodsGuidance_TableColumns(t *testing.T) {
	p := docgen.ResolveSectionProfile("view", "")
	g := docgen.ResolveGuidance(p, "child-methods")
	lower := strings.ToLower(g)
	for _, col := range []string{"method", "http verb", "path"} {
		if !strings.Contains(lower, col) {
			t.Errorf("view child-methods guidance: want column %q; got:\n%s", col, g)
		}
	}
}

// TestClassChildMethodsGuidance_TableColumns verifies that the class profile's
// child-methods guidance mentions generic class method columns (Signature, Visibility).
func TestClassChildMethodsGuidance_TableColumns(t *testing.T) {
	p := docgen.ResolveSectionProfile("class", "")
	g := docgen.ResolveGuidance(p, "child-methods")
	lower := strings.ToLower(g)
	for _, col := range []string{"method", "signature", "visibility"} {
		if !strings.Contains(lower, col) {
			t.Errorf("class child-methods guidance: want column %q; got:\n%s", col, g)
		}
	}
}

// TestChildMethodsGuidance_ClassManifestReference verifies that child-methods
// guidance references class_manifest or neighbour_briefs as the data source.
func TestChildMethodsGuidance_ClassManifestReference(t *testing.T) {
	for _, kind := range []string{"view", "class"} {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			g := docgen.ResolveGuidance(p, "child-methods")
			lower := strings.ToLower(g)
			if !strings.Contains(lower, "class_manifest") && !strings.Contains(lower, "neighbour_briefs") {
				t.Errorf("%s child-methods guidance: should reference class_manifest or neighbour_briefs; got:\n%s", kind, g)
			}
		})
	}
}

// TestViewChildMethodsPlacedAfterOverview verifies that child-methods appears
// right after overview in the view profile (at-a-glance index before narrative).
func TestViewChildMethodsPlacedAfterOverview(t *testing.T) {
	p := docgen.ResolveSectionProfile("view", "")
	var overviewIdx, childMethodsIdx int = -1, -1
	for i, s := range p.Sections {
		if s == "overview" {
			overviewIdx = i
		}
		if s == "child-methods" {
			childMethodsIdx = i
		}
	}
	if overviewIdx < 0 {
		t.Fatal("view profile: overview section not found")
	}
	if childMethodsIdx < 0 {
		t.Fatal("view profile: child-methods section not found")
	}
	if childMethodsIdx != overviewIdx+1 {
		t.Errorf("view profile: child-methods should immediately follow overview (idx %d), got idx %d",
			overviewIdx+1, childMethodsIdx)
	}
}

// TestClassChildMethodsPlacedAfterOverview verifies that child-methods appears
// right after overview in the class profile.
func TestClassChildMethodsPlacedAfterOverview(t *testing.T) {
	p := docgen.ResolveSectionProfile("class", "")
	var overviewIdx, childMethodsIdx int = -1, -1
	for i, s := range p.Sections {
		if s == "overview" {
			overviewIdx = i
		}
		if s == "child-methods" {
			childMethodsIdx = i
		}
	}
	if overviewIdx < 0 {
		t.Fatal("class profile: overview section not found")
	}
	if childMethodsIdx < 0 {
		t.Fatal("class profile: child-methods section not found")
	}
	if childMethodsIdx != overviewIdx+1 {
		t.Errorf("class profile: child-methods should immediately follow overview (idx %d), got idx %d",
			overviewIdx+1, childMethodsIdx)
	}
}

// ---------------------------------------------------------------------------
// #1875 — model api: ORM-Model-no-callable-API branch
// ---------------------------------------------------------------------------

// TestModelApiGuidance_ORMBranch verifies that the model profile's api guidance
// instructs the LLM to document the natural API surface via viewsets/handlers,
// NOT to fabricate ORM method signatures like Model.objects.filter().
func TestModelApiGuidance_ORMBranch(t *testing.T) {
	p := docgen.ResolveSectionProfile("model", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	// Must state the model has no direct callable API.
	if !strings.Contains(lower, "no direct callable api") && !strings.Contains(lower, "no callable api") {
		t.Errorf("model api guidance: should state model has no callable API; got:\n%s", g)
	}

	// Must instruct to document via viewsets/handlers in neighbours.
	if !strings.Contains(lower, "viewset") && !strings.Contains(lower, "handler") {
		t.Errorf("model api guidance: should reference viewsets/handlers; got:\n%s", g)
	}

	// Must NEVER instruction to prevent fabrication.
	if !strings.Contains(lower, "never") {
		t.Errorf("model api guidance: should include NEVER-fabricate instruction; got:\n%s", g)
	}
}

// TestModelApiGuidance_FieldsAndAssociations verifies that the model api guidance
// still documents the data interface (fields, associations, validations).
func TestModelApiGuidance_FieldsAndAssociations(t *testing.T) {
	p := docgen.ResolveSectionProfile("model", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	for _, want := range []string{"field", "association"} {
		if !strings.Contains(lower, want) {
			t.Errorf("model api guidance: should mention %q for data interface; got:\n%s", want, g)
		}
	}
}

// ---------------------------------------------------------------------------
// #1882 — module api: catalog mode
// ---------------------------------------------------------------------------

// TestModuleApiGuidance_CatalogMode verifies that the module profile's api
// guidance instructs catalog mode, not per-endpoint enumeration.
func TestModuleApiGuidance_CatalogMode(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	// Must mention catalog.
	if !strings.Contains(lower, "catalog") {
		t.Errorf("module api guidance: want 'catalog' keyword; got:\n%s", g)
	}

	// Must explicitly say NOT to enumerate individual endpoints.
	if !strings.Contains(lower, "do not enumerate") && !strings.Contains(lower, "not enumerate") {
		t.Errorf("module api guidance: should say DO NOT enumerate; got:\n%s", g)
	}
}

// TestModuleApiGuidance_VerbBreakdown verifies that module api guidance asks
// for verb breakdown (GET/POST/PUT/PATCH/DELETE counts).
func TestModuleApiGuidance_VerbBreakdown(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	if !strings.Contains(lower, "verb breakdown") && !strings.Contains(lower, "verb") {
		t.Errorf("module api guidance: should request verb breakdown; got:\n%s", g)
	}
}

// TestModuleApiGuidance_URLPrefix verifies that module api guidance asks for
// URL-prefix shape documentation.
func TestModuleApiGuidance_URLPrefix(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	if !strings.Contains(lower, "url-prefix") && !strings.Contains(lower, "url prefix") {
		t.Errorf("module api guidance: should request URL-prefix shape; got:\n%s", g)
	}
}

// TestModuleApiGuidance_ModuleManifestReference verifies that module api guidance
// references module_manifest.endpoints as a data source.
func TestModuleApiGuidance_ModuleManifestReference(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	if !strings.Contains(lower, "module_manifest") {
		t.Errorf("module api guidance: should reference module_manifest; got:\n%s", g)
	}
}

// ---------------------------------------------------------------------------
// #1883 — module flows: archetypal patterns + 2-3 mermaid diagrams
// ---------------------------------------------------------------------------

// TestModuleFlowsGuidance_ArchetypalPatterns verifies that the module profile's
// flows guidance asks for archetypal flow patterns, not a single primary flow.
func TestModuleFlowsGuidance_ArchetypalPatterns(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "flows")
	lower := strings.ToLower(g)

	if !strings.Contains(lower, "archetypal") {
		t.Errorf("module flows guidance: want 'archetypal' keyword; got:\n%s", g)
	}
}

// TestModuleFlowsGuidance_MultipleMermaidDiagrams verifies that the module
// flows guidance asks for 2-3 mermaid diagrams, not just one.
func TestModuleFlowsGuidance_MultipleMermaidDiagrams(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "flows")
	lower := strings.ToLower(g)

	// Must mention multiple diagrams (2-3 or "two" or "three").
	hasTwoOrThree := strings.Contains(lower, "2-3") ||
		strings.Contains(lower, "2–3") ||
		strings.Contains(lower, "two") ||
		strings.Contains(lower, "three")
	if !hasTwoOrThree {
		t.Errorf("module flows guidance: should request 2-3 mermaid diagrams; got:\n%s", g)
	}
}

// TestModuleFlowsGuidance_ExampleFlowTypes verifies that the module flows
// guidance gives concrete examples of archetypal flows to guide the LLM.
func TestModuleFlowsGuidance_ExampleFlowTypes(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "flows")
	lower := strings.ToLower(g)

	// Should mention at least one concrete archetype (HTTP, async/Celery, scheduled).
	hasExample := strings.Contains(lower, "http") ||
		strings.Contains(lower, "celery") ||
		strings.Contains(lower, "async") ||
		strings.Contains(lower, "scheduled") ||
		strings.Contains(lower, "batch")
	if !hasExample {
		t.Errorf("module flows guidance: should give concrete flow archetypes; got:\n%s", g)
	}
}

// TestModuleFlowsGuidance_StillFabricationGuarded verifies that the module flows
// guidance still has the fabrication guard (no entities outside neighbour_briefs).
func TestModuleFlowsGuidance_StillFabricationGuarded(t *testing.T) {
	p := docgen.ResolveSectionProfile("module", "")
	g := docgen.ResolveGuidance(p, "flows")
	lower := strings.ToLower(g)

	if !strings.Contains(lower, "neighbour_briefs") {
		t.Errorf("module flows guidance: should retain neighbour_briefs fabrication guard; got:\n%s", g)
	}
}
