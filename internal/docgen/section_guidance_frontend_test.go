package docgen_test

// section_guidance_frontend_test.go — tests for the section guidance bundle B
// (frontend support).  Covers:
//
//   - #1869 — reference-deployment and reference-scripts language/stack branch
//     (frontend gets npm run dev / vite build / next start; backend unaffected)
//   - #1870 — patterns section with full frontend vocabulary for react_component,
//     react_hook, and js_module profiles
//   - #1871 — api section for frontend entities: outbound HTTP calls
//     (fetch/axios/React Query/SWR) as explicit api surface

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/docgen"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func frontendContains(t *testing.T, guidance, label, word string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(guidance), strings.ToLower(word)) {
		t.Errorf("%s: want %q in guidance; got:\n%s", label, word, guidance)
	}
}

// ---------------------------------------------------------------------------
// #1869 — reference-deployment and reference-scripts frontend branch
// ---------------------------------------------------------------------------

// TestJSModule_DeploymentGuidanceIsFrontend verifies that js_module
// reference-deployment guidance asks about CDN / Vite / Next.js concerns,
// NOT generic backend daemon configuration.
func TestJSModule_DeploymentGuidanceIsFrontend(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "reference-deployment")
	lower := strings.ToLower(g)

	frontendTerms := []string{"vite", "next"}
	found := false
	for _, term := range frontendTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("js_module reference-deployment: want at least one of %v; got:\n%s", frontendTerms, g)
	}
}

// TestJSModule_ScriptsGuidanceHasNpmCommands verifies that js_module
// reference-scripts guidance lists npm-ecosystem commands (npm run dev,
// vite build, next start, or vite preview).
func TestJSModule_ScriptsGuidanceHasNpmCommands(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "reference-scripts")
	lower := strings.ToLower(g)

	required := []string{"npm", "vite", "next"}
	for _, term := range required {
		if !strings.Contains(lower, term) {
			t.Errorf("js_module reference-scripts: want %q; got:\n%s", term, g)
		}
	}

	// Must mention a local-dev start command.
	if !strings.Contains(lower, "dev") {
		t.Errorf("js_module reference-scripts: want dev command reference; got:\n%s", g)
	}

	// Must mention a production build command.
	if !strings.Contains(lower, "build") {
		t.Errorf("js_module reference-scripts: want build command reference; got:\n%s", g)
	}
}

// TestJSModule_ScriptsGuidanceMentionsNextStart verifies "next start" or
// "vite preview" appears in js_module reference-scripts guidance so that the
// preview-the-production-build step is covered.
func TestJSModule_ScriptsGuidanceMentionsNextStart(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "reference-scripts")
	lower := strings.ToLower(g)

	// Either "next start" or "vite preview" counts.
	if !strings.Contains(lower, "next start") && !strings.Contains(lower, "vite preview") {
		t.Errorf("js_module reference-scripts: want 'next start' or 'vite preview'; got:\n%s", g)
	}
}

// TestJSModule_HasDeploymentAndScriptsSections verifies js_module profile
// includes both reference-deployment and reference-scripts sections.
func TestJSModule_HasDeploymentAndScriptsSections(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	for _, sec := range []string{"reference-deployment", "reference-scripts"} {
		if !containsSection(p.Sections, sec) {
			t.Errorf("js_module: missing section %q; got %v", sec, p.Sections)
		}
	}
}

// TestJSModule_NoHowToLocalDev verifies js_module does not include
// how-to-local-dev (that lives on the parent module page).
func TestJSModule_NoHowToLocalDev(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	if containsSection(p.Sections, "how-to-local-dev") {
		t.Errorf("js_module: how-to-local-dev should be absent; got %v", p.Sections)
	}
}

// TestModuleWithJSLanguage_ResolvesToJSModuleProfile verifies that a plain
// "module" kind with language="javascript" resolves to the js_module profile
// (frontend-aware guidance) rather than the generic backend module profile.
func TestModuleWithJSLanguage_ResolvesToJSModuleProfile(t *testing.T) {
	for _, lang := range []string{"javascript", "typescript", "js", "ts"} {
		t.Run(lang, func(t *testing.T) {
			p := docgen.ResolveSectionProfile("module", lang)
			g := docgen.ResolveGuidance(p, "reference-scripts")
			lower := strings.ToLower(g)
			// The js_module profile references npm/vite; the generic module
			// profile references Makefile/go.  npm presence confirms the override.
			if !strings.Contains(lower, "npm") && !strings.Contains(lower, "vite") {
				t.Errorf("module (lang=%s) reference-scripts: want npm/vite guidance (js_module profile), got:\n%s", lang, g)
			}
		})
	}
}

// TestModuleWithBackendLanguage_KeepsGenericProfile verifies that a plain
// "module" kind with a backend language retains the generic module profile
// (not the js_module profile).
func TestModuleWithBackendLanguage_KeepsGenericProfile(t *testing.T) {
	for _, lang := range []string{"go", "ruby", "python", "java", ""} {
		t.Run(lang, func(t *testing.T) {
			p := docgen.ResolveSectionProfile("module", lang)
			// The generic module profile includes how-to-local-dev;
			// js_module does NOT.  Use that as the discriminator.
			if !containsSection(p.Sections, "how-to-local-dev") {
				t.Errorf("module (lang=%s): expected how-to-local-dev (generic module profile); got %v", lang, p.Sections)
			}
		})
	}
}

// TestReactHook_NoDeploymentScripts verifies that react_hook does not include
// deployment or how-to-local-dev sections (hook-level concern is leaf-only).
func TestReactHook_NoDeploymentScripts(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_hook", "")
	absent := []string{"reference-deployment", "reference-scripts", "how-to-local-dev"}
	for _, sec := range absent {
		if containsSection(p.Sections, sec) {
			t.Errorf("react_hook: section %q should be absent; got %v", sec, p.Sections)
		}
	}
}

// ---------------------------------------------------------------------------
// #1870 — frontend patterns vocabulary
// ---------------------------------------------------------------------------

// TestReactComponent_PatternsGuidanceHasFrontendVocabulary verifies the
// react_component patterns guidance mentions the full frontend pattern set.
func TestReactComponent_PatternsGuidanceHasFrontendVocabulary(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)

	required := []string{
		"container",    // container/presentational
		"render prop",  // render-prop pattern
		"portal",       // React portal
		"route",        // route-level component
		"form",         // form orchestrator
		"controlled",   // controlled/uncontrolled
	}
	for _, term := range required {
		if !strings.Contains(lower, term) {
			t.Errorf("react_component patterns: want %q; got:\n%s", term, g)
		}
	}
}

// TestReactHook_PatternsGuidanceHasHookPatterns verifies the react_hook
// patterns guidance mentions hook-specific patterns.
func TestReactHook_PatternsGuidanceHasHookPatterns(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_hook", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)

	required := []string{
		"data-fetching", // data-fetching hook
		"form",          // form orchestrator hook
		"effect",        // effect wrapper
		"context",       // context accessor
		"derived",       // derived-state hook
	}
	for _, term := range required {
		if !strings.Contains(lower, term) {
			t.Errorf("react_hook patterns: want %q; got:\n%s", term, g)
		}
	}
}

// TestJSModule_PatternsGuidanceHasModulePatterns verifies the js_module
// patterns guidance mentions JS module-level patterns.
func TestJSModule_PatternsGuidanceHasModulePatterns(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "patterns")
	lower := strings.ToLower(g)

	required := []string{
		"barrel",    // barrel re-export
		"singleton", // singleton service
		"api client", // API client wrapper
		"factory",   // factory function
	}
	for _, term := range required {
		if !strings.Contains(lower, term) {
			t.Errorf("js_module patterns: want %q; got:\n%s", term, g)
		}
	}
}

// TestFrontendProfiles_PatternsHasBundleGuard verifies that all three frontend
// profiles' patterns guidance includes the neighbour_briefs fabrication guard.
func TestFrontendProfiles_PatternsHasBundleGuard(t *testing.T) {
	kinds := []string{"react_component", "react_hook", "js_module"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			g := docgen.ResolveGuidance(p, "patterns")
			lower := strings.ToLower(g)
			if !strings.Contains(lower, "neighbour_briefs") {
				t.Errorf("%s patterns: missing neighbour_briefs fabrication guard; got:\n%s", kind, g)
			}
			if !strings.Contains(lower, "module_manifest") {
				t.Errorf("%s patterns: missing module_manifest fabrication guard; got:\n%s", kind, g)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// #1871 — api section for frontend entities: outbound HTTP calls
// ---------------------------------------------------------------------------

// TestReactComponent_ApiGuidanceCoversOutboundHTTP verifies that
// react_component api guidance asks about outbound HTTP calls (React Query,
// SWR, fetch, or axios) made directly inside the component.
func TestReactComponent_ApiGuidanceCoversOutboundHTTP(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_component", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	// Must still cover props interface.
	frontendContains(t, lower, "react_component api", "props")

	// Must also ask about outbound HTTP.
	httpTerms := []string{"usequery", "useswr", "fetch", "axios"}
	found := false
	for _, term := range httpTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("react_component api: want at least one outbound HTTP term %v; got:\n%s", httpTerms, g)
	}

	// Must ask for HTTP method and URL.
	for _, term := range []string{"http method", "url"} {
		if !strings.Contains(lower, term) {
			t.Errorf("react_component api: want %q; got:\n%s", term, g)
		}
	}
}

// TestReactHook_ApiGuidanceCoversOutboundHTTP verifies that react_hook api
// guidance explicitly asks about outbound HTTP calls (fetch/axios/React Query).
func TestReactHook_ApiGuidanceCoversOutboundHTTP(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_hook", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	// Must be hook-centric (not generic function signature).
	frontendContains(t, lower, "react_hook api", "parameter")

	// Must document return values.
	frontendContains(t, lower, "react_hook api", "return")

	// Must ask about outbound HTTP — hooks are the primary location for data calls.
	httpTerms := []string{"fetch", "axios", "react query", "swr"}
	found := false
	for _, term := range httpTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("react_hook api: want at least one HTTP client term %v; got:\n%s", httpTerms, g)
	}

	// Must ask for request/response shape documentation.
	for _, term := range []string{"request", "response"} {
		if !strings.Contains(lower, term) {
			t.Errorf("react_hook api: want %q shape documentation; got:\n%s", term, g)
		}
	}
}

// TestJSModule_ApiGuidanceCoversOutboundHTTP verifies that js_module api
// guidance explicitly asks about outbound HTTP calls — JS modules are the
// canonical location for API client wrappers.
func TestJSModule_ApiGuidanceCoversOutboundHTTP(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	g := docgen.ResolveGuidance(p, "api")
	lower := strings.ToLower(g)

	// Must cover the public export surface.
	frontendContains(t, lower, "js_module api", "export")

	// Must ask about outbound HTTP.
	httpTerms := []string{"fetch", "axios", "react query", "swr", "graphql"}
	found := false
	for _, term := range httpTerms {
		if strings.Contains(lower, term) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("js_module api: want at least one HTTP client term %v; got:\n%s", httpTerms, g)
	}

	// Must ask for HTTP method, URL, request body, response shape.
	for _, term := range []string{"http method", "url", "request", "response"} {
		if !strings.Contains(lower, term) {
			t.Errorf("js_module api: want %q; got:\n%s", term, g)
		}
	}
}

// ---------------------------------------------------------------------------
// Profile completeness
// ---------------------------------------------------------------------------

// TestReactHook_ProfileHasCoreAndApiSections verifies react_hook profile
// includes the essential sections and no deployment sections.
func TestReactHook_ProfileHasCoreAndApiSections(t *testing.T) {
	p := docgen.ResolveSectionProfile("react_hook", "")
	if len(p.Sections) == 0 {
		t.Fatal("react_hook: expected non-empty Sections")
	}
	if p.GuidanceOverrides == nil {
		t.Fatal("react_hook: expected GuidanceOverrides to be non-nil")
	}
	mustPresent := []string{"overview", "capabilities", "api", "patterns", "module-readme"}
	for _, sec := range mustPresent {
		if !containsSection(p.Sections, sec) {
			t.Errorf("react_hook: missing required section %q; got %v", sec, p.Sections)
		}
	}
}

// TestJSModule_ProfileHasCoreAndDeploymentSections verifies js_module profile
// includes all expected sections.
func TestJSModule_ProfileHasCoreAndDeploymentSections(t *testing.T) {
	p := docgen.ResolveSectionProfile("js_module", "")
	if len(p.Sections) == 0 {
		t.Fatal("js_module: expected non-empty Sections")
	}
	if p.GuidanceOverrides == nil {
		t.Fatal("js_module: expected GuidanceOverrides to be non-nil")
	}
	mustPresent := []string{
		"overview", "capabilities", "api", "patterns",
		"reference-deployment", "reference-scripts",
		"reference-config", "reference-dependencies",
		"reference-misc", "glossary", "module-readme",
	}
	for _, sec := range mustPresent {
		if !containsSection(p.Sections, sec) {
			t.Errorf("js_module: missing required section %q; got %v", sec, p.Sections)
		}
	}
}

// TestFrontendProfiles_NoDuplicateSections verifies no section appears twice
// in any frontend profile.
func TestFrontendProfiles_NoDuplicateSections(t *testing.T) {
	for _, kind := range []string{"react_component", "react_hook", "js_module"} {
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

// TestFrontendProfiles_ModuleReadmeBoundaryGuidance verifies all three
// frontend profiles include the module-readme sibling-boundary instruction.
func TestFrontendProfiles_ModuleReadmeBoundaryGuidance(t *testing.T) {
	for _, kind := range []string{"react_component", "react_hook", "js_module"} {
		t.Run(kind, func(t *testing.T) {
			p := docgen.ResolveSectionProfile(kind, "")
			if !containsSection(p.Sections, "module-readme") {
				t.Skipf("%s: profile does not include module-readme", kind)
			}
			g := docgen.ResolveGuidance(p, "module-readme")
			lower := strings.ToLower(g)
			for _, term := range []string{"module_manifest", "neighbour_briefs", "do not mention"} {
				if !strings.Contains(lower, term) {
					t.Errorf("%s module-readme: missing %q boundary; got:\n%s", kind, term, g)
				}
			}
		})
	}
}

// TestReactHook_ExactMatchDoesNotBleedIntoOtherKinds verifies that the
// exact-match path fires for "react_hook" and does not accidentally resolve
// to any other profile via substring.
func TestReactHook_ExactMatchDoesNotBleedIntoOtherKinds(t *testing.T) {
	hookProfile := docgen.ResolveSectionProfile("react_hook", "")
	// react_hook must NOT resolve to the same profile as a generic function
	// (deployment sections absent in both, but guidance should differ).
	funcProfile := docgen.ResolveSectionProfile("function", "")
	hookAPI := docgen.ResolveGuidance(hookProfile, "api")
	funcAPI := docgen.ResolveGuidance(funcProfile, "api")
	if hookAPI == funcAPI {
		t.Error("react_hook api guidance equals generic function api guidance — profiles are not distinct")
	}
}

// TestJSModule_ExactMatchDoesNotFallThroughToModule verifies that js_module
// resolves to the js_module profile (not the generic backend module profile).
func TestJSModule_ExactMatchDoesNotFallThroughToModule(t *testing.T) {
	jsModProfile := docgen.ResolveSectionProfile("js_module", "")
	backendModProfile := docgen.ResolveSectionProfile("module", "go")

	// js_module must NOT have how-to-local-dev (generic module has it).
	if containsSection(jsModProfile.Sections, "how-to-local-dev") {
		t.Error("js_module: should not include how-to-local-dev (that is the generic module profile)")
	}
	// Generic module must have how-to-local-dev.
	if !containsSection(backendModProfile.Sections, "how-to-local-dev") {
		t.Error("module(go): should include how-to-local-dev")
	}

	// reference-scripts guidance must differ.
	jsScripts := docgen.ResolveGuidance(jsModProfile, "reference-scripts")
	beScripts := docgen.ResolveGuidance(backendModProfile, "reference-scripts")
	if jsScripts == beScripts {
		t.Error("js_module and module(go) reference-scripts guidance should differ")
	}
}
