// Package docgen — per-kind section profile framework (issue #1875).
//
// sectionsByKind maps entity kind strings to SectionProfile values so that
// each entity kind gets an explicit, curated section list instead of the
// flat 13-section default.  This is the unifying restructure that absorbs
// the piecewise section-gate work from #1860, #1863, #1865, #1866, #1870,
// #1873, #1882, and #1883.
//
// # Design
//
//   - SectionProfile.Sections is the ordered list that replaces the ad-hoc
//     switch in sectionsForEntityKind (tier1.go).
//   - SectionProfile.GuidanceOverrides lets a kind supply its own prompt text
//     for a section name without touching defaultSectionGuidance.  The override
//     takes effect in BuildBundle (llm_bundle.go) when the key matches.
//   - ResolveSectionProfile canonicalises the kind string and looks it up in
//     sectionsByKind, falling back to the "default" profile — which preserves
//     exactly the current 13-section behaviour for any kind not yet profiled.
//
// # Scope of this PR (#1875 Wave 1)
//
// Model, Module, and Operation profiles are landed here.
// View, Component, and Class profiles are follow-up PRs so that individual
// section-list decisions can be reviewed in isolation.
//
// # Section lists
//
// All Sections values must be drawn from KnownSections.  Sections that
// represent entirely new surface area (e.g. a dedicated "schema" section for
// Model) are introduced in follow-up PRs after KnownSections is extended.
// The GuidanceOverrides for the existing sections already carry Model-specific
// framing so the LLM output is correctly scoped even before a dedicated section
// is introduced.
//
// # Size-aware Operation profiles (#1986)
//
// ResolveSectionProfile accepts an optional lineCount variadic argument.
// When provided for an Operation kind, the profile is chosen from three tiers:
//
//   - small  (< 30 lines): skips reference-deployment, how-to-local-dev,
//     reference-scripts; uses shorter capabilities/api guidance.
//   - medium (30–149 lines): current guidance (baseline Operation profile).
//   - large  (>= 150 lines): full deep-context template with all sections.
package docgen

import "strings"

// SourceWindowStrategyDefault is the baseline strategy: emit ±sourceWindowHalfLines
// lines around entity.StartLine (the original behaviour pre-#1876).
const SourceWindowStrategyDefault = ""

// SourceWindowStrategyWholeBody causes BuildBundle to emit the entire class body
// from entity.StartLine to entity.EndLine (inclusive), capped at
// SourceWindowWholeBodyMaxLines with a truncation annotation.  Intended for
// Model entities where every field declaration carries semantic information.
const SourceWindowStrategyWholeBody = "whole-body"

// SourceWindowWholeBodyMaxLines is the safety cap applied when
// SourceWindowStrategyWholeBody is in effect.  Models larger than this limit
// are clipped and a "truncated_at_line" comment is appended to the window.
const SourceWindowWholeBodyMaxLines = 400

// SectionProfile pairs an ordered section list with optional per-kind guidance
// overrides.  Both fields are safe to read concurrently after init.
type SectionProfile struct {
	// Sections is the ordered list of section names that apply to this kind.
	// Every entry must be present in KnownSections; unknown names are silently
	// ignored by tier1 assemblePage (which iterates KnownSections for order
	// determinism) but are still passed to the LLM bundle builder.
	Sections []string

	// GuidanceOverrides maps a section name to kind-specific prompt text.
	// When non-empty for a section, BuildBundle uses it instead of the
	// corresponding entry in defaultSectionGuidance.
	GuidanceOverrides map[string]string

	// SourceWindowStrategy controls how BuildBundle constructs the source_window
	// excerpt for entities matching this profile.  Use the SourceWindowStrategy*
	// constants.  Empty string selects SourceWindowStrategyDefault.
	SourceWindowStrategy string
}

// sectionsByKind is the authoritative per-kind profile registry.
//
// Key convention: use the canonical lower-case kind stem that the graph emits
// (e.g. "model", "module", "operation").  ResolveSectionProfile folds the
// lookup key to lower-case and does a substring match so minor variations
// ("SCOPE.Model", "datamodel", "Model") all resolve to the right profile.
// The "default" key is the catch-all and must always be present.
//
// Profiles are ordered to mirror a natural reading flow:
//
//	overview → capabilities → domain-specific behaviour →
//	cross-cutting reference → glossary → module-readme.
var sectionsByKind = map[string]SectionProfile{

	// -------------------------------------------------------------------------
	// Model — database entity / ORM model / schema object.
	//
	// R3 dogfood findings: Model pages need data-model framing for overview,
	// capabilities, api (schema surface), and reference-dependencies (gem/
	// package dependencies on persistence libraries).  They do NOT need
	// reference-deployment, reference-scripts, or how-to-local-dev — those are
	// module-level concerns that add ~3 boilerplate sections on every leaf page.
	// -------------------------------------------------------------------------
	"model": {
		Sections: []string{
			"overview",
			"capabilities",
			"api",
			"patterns",
			"reference-dependencies",
			"reference-config",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		// SourceWindowStrategyWholeBody: for Model entities the class body IS the
		// semantic — every field declaration, association, and Meta class must be
		// visible to the LLM.  The ±20-line default clips mid-class and loses all
		// field declarations (#1876).
		SourceWindowStrategy: SourceWindowStrategyWholeBody,
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this data model represents and where it is persisted. " +
				"Note if it is a core aggregate, a join table, or a read-model projection. " +
				"Highlight god-node or articulation-point status if applicable.",
			"capabilities": "List the business capabilities this model supports: which features read it, which write it, " +
				"and any invariants it enforces at the persistence layer. Group by business outcome, not by field name.",
			"api": "Document the model's public interface: field names with types, associations (has-many, belongs-to, etc.), " +
				"and validation rules. Include any scopes, callbacks, or computed attributes that are part of the public contract.",
			"patterns": "Identify data modelling patterns present (aggregate root, polymorphic association, STI, soft-delete, " +
				"event-sourced projection, etc.).  Cite specific neighbour relationships as evidence.",
			"reference-dependencies": "List external libraries this model depends on " +
				"(e.g. Devise, ActiveStorage, Paperclip, soft-delete gems, ORM adapters). " +
				"Separate production from test-only dependencies.",
			"reference-config": "List every configuration key, environment variable, or feature flag that changes this model's " +
				"storage behaviour (e.g. table-name overrides, encryption keys, tenant discriminator columns).",
			"reference-misc": "Capture migration history highlights, known technical debt, or links to the ADR that introduced this model.",
			"glossary": "Define domain terms that appear in field names, association names, or enum values. One term per row.",
			"module-readme": "Write a README-style intro for the module that owns this model: purpose, key sibling models, " +
				"and how to run the associated tests. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// Module — package, directory-level module, Go package, npm package, etc.
	//
	// R4 dogfood findings: Module pages need the FULL reference suite including
	// reference-deployment, reference-scripts, and how-to-local-dev because
	// modules are the unit of deployment and local-dev entry points.  This is
	// the one kind that legitimately uses all 13 sections.
	// -------------------------------------------------------------------------
	"module": {
		Sections: []string{
			"overview",
			"capabilities",
			"flows",
			"patterns",
			"api",
			"reference-config",
			"reference-dependencies",
			"reference-deployment",
			"reference-scripts",
			"how-to-local-dev",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this module owns, its primary responsibility in the system, " +
				"and any cross-cutting concerns it addresses. Highlight if it is a god node or articulation point.",
			"capabilities": "Enumerate the product capabilities this module owns, grouped by business outcome. " +
				"Reference the key entities, handlers, or service objects that implement each capability.",
			"flows": "Trace the primary request or event flow through this module using a mermaid sequence or flowchart. " +
				"Show the entry point, internal orchestration, and outbound calls to external modules or services.",
			"api": "Document the full public API surface of this module: exported functions, HTTP endpoints, event topics, or CLI flags. " +
				"Include method signatures and a one-line usage note for each.",
			"reference-config": "List every environment variable or configuration key this module reads, with type, default value, and effect. " +
				"Separate required from optional keys.",
			"reference-deployment": "Describe deployment concerns owned by this module: required env vars, exposed ports, " +
				"scaling constraints, health-check endpoints, and any sidecar dependencies.",
			"reference-scripts": "List all Makefile targets, npm/go scripts, or shell commands that operate on this module and explain what each does.",
			"how-to-local-dev": "Provide a numbered step-by-step local development guide for this module: " +
				"clone, configure env vars, build, run tests, start the local server, and observe output.",
			"module-readme": "Write a README-style introduction for this module: purpose, key entities, " +
				"quickstart build/test/run commands, and link to the full documentation page. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// Operation — method, function, handler, endpoint, command.
	//
	// R1 dogfood findings: Operation pages (class/method) are self-contained
	// units of behaviour.  They do NOT need reference-deployment/scripts/
	// how-to-local-dev (module-level concerns) reducing boilerplate by ~3 sections.
	// They DO need flows, patterns, and api for signature documentation.
	//
	// This is the "medium" (30–149 line) baseline profile.  ResolveSectionProfile
	// selects "operation.small" or "operation.large" when a lineCount is supplied.
	// -------------------------------------------------------------------------
	"operation": {
		Sections: []string{
			"overview",
			"capabilities",
			"flows",
			"patterns",
			"api",
			"reference-config",
			"reference-dependencies",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this operation does and why it exists. " +
				"Highlight its role in the call graph: is it a leaf, a dispatcher, or a critical-path entry point?",
			"capabilities": "List the discrete behaviours this operation provides. " +
				"One bullet per observable side-effect or return-value contract.",
			"flows": "Trace the execution flow through this operation using a mermaid sequence or flowchart. " +
				"Show the caller → this operation → callees chain and any branching conditions.",
			"patterns": "Identify structural patterns present in this operation " +
				"(guard clause, strategy delegation, saga step, command-query separation, etc.). " +
				"Cite specific callee relationships as evidence.",
			"api": "Document the function signature in full: parameters (name, type, purpose), return value(s), " +
				"and raised errors or panics. Include a minimal usage example.",
			"reference-config": "List any environment variables, feature flags, or config keys read inside this operation. " +
				"Note which values alter branching behaviour.",
			"reference-dependencies": "List direct external and internal dependencies called by this operation. " +
				"Separate production callees from test-only callees.",
			"reference-misc": "Capture performance notes, known edge cases, or links to the issue/ADR that introduced this operation.",
			"glossary": "Define domain terms appearing in the function name, parameter names, or return type. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that contains this operation: " +
				"purpose and key sibling operations. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// Operation.small — helper function / method under 30 lines.
	//
	// #1986: small operations are leaf helpers; padded deployment/dev sections
	// produce generic boilerplate and degrade quality scores.  Skip the three
	// heavy infrastructure sections and use tighter capabilities/api guidance.
	// -------------------------------------------------------------------------
	"operation.small": {
		Sections: []string{
			"overview",
			"capabilities",
			"flows",
			"patterns",
			"api",
			"reference-config",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write 1–2 sentences describing what this small helper does and when it is called. " +
				"State its single responsibility clearly.",
			"capabilities": "List the observable behaviours of this helper in 1–3 bullets. " +
				"Keep it concise — small operations have narrow contracts.",
			"flows": "Describe the execution path briefly. A single mermaid flowchart node or a short prose paragraph is sufficient.",
			"patterns": "Note any design pattern (guard clause, delegation, pure function, etc.) in one sentence.",
			"api": "Document the function signature: parameters (name, type), return value, and any errors raised. " +
				"One-liner usage example if the call site is non-obvious.",
			"reference-config": "Note any config key or feature flag read by this helper, if any. Omit section if none.",
			"reference-misc": "Capture edge cases or performance notes specific to this helper, if any.",
			"glossary": "Define any non-obvious domain term in the function or parameter names. Omit if all names are self-evident.",
			"module-readme": "One sentence positioning this helper within its module. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// Operation.large — service-level function / method 150+ lines.
	//
	// #1986: large operations act more like service entry-points and warrant the
	// full template including deployment context, script references, and a
	// comprehensive local-dev guide.
	// -------------------------------------------------------------------------
	"operation.large": {
		Sections: []string{
			"overview",
			"capabilities",
			"flows",
			"patterns",
			"api",
			"reference-config",
			"reference-dependencies",
			"reference-deployment",
			"reference-scripts",
			"how-to-local-dev",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–4 sentence description of what this large operation does, why it is a critical-path entry point, " +
				"and what subsystems it orchestrates. " +
				"Highlight god-node or articulation-point status if applicable.",
			"capabilities": "Enumerate all discrete business capabilities this operation provides. " +
				"Group by outcome category. One bullet per observable contract.",
			"flows": "Trace the full execution flow using a mermaid sequence diagram. " +
				"Show the caller → this operation → all major callees and any fork/join or retry loops.",
			"patterns": "Identify ALL structural and architectural patterns present " +
				"(orchestrator, saga, pipeline, strategy, command, etc.). " +
				"Cite specific neighbour relationships as evidence for each.",
			"api": "Document the full function signature: every parameter (name, type, purpose), all return values, " +
				"and every error or panic path. Include a realistic usage example showing a typical call site.",
			"reference-config": "List every environment variable, config key, or feature flag read by this operation. " +
				"Note which alter branching behaviour and which are required vs optional.",
			"reference-dependencies": "List all external and internal dependencies called by this operation. " +
				"Separate required production dependencies from optional/test-only dependencies.",
			"reference-deployment": "Describe deployment concerns relevant to this operation: " +
				"required env vars, scaling constraints, health signals, and sidecar or infrastructure dependencies.",
			"reference-scripts": "List Makefile targets, scripts, or shell commands that exercise or deploy this operation.",
			"how-to-local-dev": "Provide a numbered guide for running this operation locally: " +
				"environment setup, build steps, test execution, and observability hooks.",
			"reference-misc": "Capture performance benchmarks, known edge cases, concurrency considerations, " +
				"or links to the ADR / issue that introduced this operation.",
			"glossary": "Define every domain term appearing in the function name, parameters, or return types. One term per row.",
			"module-readme": "Write a README-style intro for the module that contains this operation. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// react_component — React UI component (TSX/JSX).
	//
	// #1970: the generic function-signature template produces a state-table
	// workaround instead of a proper props-interface doc.  React components
	// have a distinct public surface: props (not raw parameters), JSX.Element
	// return semantics, and children/slot patterns.
	// -------------------------------------------------------------------------
	"react_component": {
		Sections: []string{
			"overview",
			"capabilities",
			"api",
			"patterns",
			"reference-config",
			"reference-dependencies",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this React component renders and when it is used. " +
				"State its primary responsibility in the UI (layout, data display, interaction, form control, etc.).",
			"capabilities": "List the visual/interactive capabilities this component provides. " +
				"One bullet per user-facing behaviour or observable output.",
			"api": "Document the props interface — NOT the generic function signature. " +
				"For each prop: name, TypeScript type (or JSDoc @param type), default value, required/optional, and one-line purpose. " +
				"If TypeScript: pull the interface or type alias from the declared Props type. " +
				"If JavaScript with JSDoc: pull from @param tags on the component function. " +
				"Show the JSX.Element return semantics (what markup is produced). " +
				"Document children semantics: are children accepted, required, or forbidden? " +
				"Show a minimal JSX usage example. " +
				"NEVER reuse the generic function-signature template for this section.",
			"patterns": "Identify React composition patterns present " +
				"(compound component, render prop, higher-order component, controlled/uncontrolled, context consumer, etc.). " +
				"Cite specific prop names or hook calls as evidence.",
			"reference-config": "List any environment variables, feature flags, or config context values that alter this component's behaviour.",
			"reference-dependencies": "List direct package dependencies (hooks, context providers, UI library components) used by this component.",
			"reference-misc": "Capture accessibility notes (ARIA roles, keyboard nav), known edge cases, or performance considerations.",
			"glossary": "Define any domain terms appearing in prop names or type names. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that owns this component. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// component.java — Java class / interface (SCOPE.Component, language=java).
	//
	// #1995: Quarkus controller / JAX-RS handler / Spring controller classes
	// commonly contain 5-15 method bodies on a single class page.  The default
	// ±20-line source_window clips at the class header + first method stub,
	// hiding 90% of the surface the LLM needs to write capabilities, flows,
	// and api sections accurately.  Apply SourceWindowStrategyWholeBody so the
	// whole class body (capped at SourceWindowWholeBodyMaxLines) is visible —
	// the same treatment Model entities receive under #1876.
	//
	// Profile is selected explicitly via the (kind, language) pair inside
	// ResolveSectionProfile; substring lookup intentionally skips dotted keys
	// so this only activates for Java.
	//
	// Sections mirror the default 13-section list (no curation in this PR) —
	// the change here is strictly about source_window completeness so the
	// existing section guidance keeps working.
	// -------------------------------------------------------------------------
	"component.java": {
		Sections:             KnownSections,
		SourceWindowStrategy: SourceWindowStrategyWholeBody,
	},

	// -------------------------------------------------------------------------
	// default — catch-all that preserves 100% backward-compatible behaviour for
	// any kind not yet explicitly profiled.  Sections == KnownSections (all 13).
	// -------------------------------------------------------------------------
	"default": {
		Sections:          KnownSections,
		GuidanceOverrides: nil,
	},
}

// operationLineTier classifies a line count into one of three Operation tiers.
// It returns the key suffix to append to "operation" for profile lookup.
//
//   - lineCount < 30  → "small"
//   - lineCount < 150 → ""      (medium — the default operation profile)
//   - lineCount >= 150 → "large"
func operationLineTier(lineCount int) string {
	if lineCount < 30 {
		return "small"
	}
	if lineCount < 150 {
		return "" // medium — use the bare "operation" profile
	}
	return "large"
}

// ResolveSectionProfile returns the SectionProfile for the given entity kind
// and language.  The lookup rules are:
//
//  1. For Operation kinds, if lineCount is provided, a size tier is applied
//     first: "operation.small" (<30 lines), "operation" (30–149 lines),
//     or "operation.large" (>=150 lines).  See #1986.
//  2. Exact case-insensitive match on kind (e.g. "Model" → "model").
//  3. Substring match — "SCOPE.Model" contains "model" → "model" profile.
//  4. Fallback to "default" when no match is found.
//
// The language parameter is accepted for future language-aware profiles
// (e.g. Component kind differs for Java vs React); it is currently unused.
// Pass an empty string when language is not available.
//
// lineCount is the optional entity line span (end_line - start_line).  Pass
// zero or omit the argument when the line count is unavailable; the medium
// Operation profile is used in that case.
func ResolveSectionProfile(kind, language string, lineCount ...int) SectionProfile {
	k := strings.ToLower(kind)
	lang := strings.ToLower(language)

	// 0. Language-specific Component override (#1995).
	//    Java class / interface entities need WholeBody source_window so the
	//    full controller surface is visible to the LLM.  The default profile
	//    used by every other Component kind still produces a ±20-line window;
	//    only Java triggers the override.  No size gate — Quarkus controllers
	//    routinely span 200-400 lines and even smaller classes benefit from
	//    seeing every method body.
	if lang == "java" && strings.Contains(k, "component") {
		if p, ok := sectionsByKind["component.java"]; ok {
			return p
		}
	}

	// 1. Size-aware Operation tier selection (#1986).
	//    Check whether the lowercased kind contains "operation" and a lineCount
	//    was provided.
	if len(lineCount) > 0 && lineCount[0] > 0 && strings.Contains(k, "operation") {
		tier := operationLineTier(lineCount[0])
		profileKey := "operation"
		if tier != "" {
			profileKey = "operation." + tier
		}
		if p, ok := sectionsByKind[profileKey]; ok {
			return p
		}
	}

	// 2. Exact match (after lower-casing).
	if p, ok := sectionsByKind[k]; ok {
		return p
	}

	// 3. Substring match — covers dotted prefixes ("SCOPE.Model") and
	//    compound kind names ("DataModel", "ServiceModule", "OperationHandler").
	//    Skip internal size-tier keys (e.g. "operation.small") to avoid
	//    accidentally matching "operation" when kind is unrelated.
	for key, profile := range sectionsByKind {
		if key == "default" {
			continue
		}
		// Skip dotted internal size-tier keys from the substring scan —
		// they are only reachable via the explicit tier path above.
		if strings.Contains(key, ".") {
			continue
		}
		if strings.Contains(k, key) {
			return profile
		}
	}

	// 4. Fallback to default — preserves current 13-section behaviour.
	return sectionsByKind["default"]
}

// ResolveGuidance returns the guidance text for a section within a profile.
// It checks GuidanceOverrides first; if no override is set for the section it
// falls back to defaultSectionGuidance, and finally to a sentinel string.
// This is the single authoritative lookup used by BuildBundle so that
// kind-specific overrides take effect without touching defaultSectionGuidance.
func ResolveGuidance(profile SectionProfile, section string) string {
	if profile.GuidanceOverrides != nil {
		if g, ok := profile.GuidanceOverrides[section]; ok {
			return g
		}
	}
	if g, ok := defaultSectionGuidance[section]; ok {
		return g
	}
	return "_No guidance available for this section type._"
}
