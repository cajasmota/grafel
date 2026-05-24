// Package docgen — per-kind section profile framework (issue #1875).
//
// sectionsByKind maps entity kind strings to SectionProfile values so that
// each entity kind gets an explicit, curated section list instead of the
// flat 13-section default.  This is the unifying restructure that absorbs
// the piecewise section-gate work from #1860, #1863, #1865, #1866, #1870,
// #1873, #1882, #1883, #1869, #1870, and #1871.
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

	// SkipForKinds enumerates the canonical (lower-case) kind stems for which
	// every section in this profile is suppressed.  It is informational metadata
	// used by ShouldSkipSectionForKind and the bundle gating tests; the per-kind
	// profile registry remains the primary mechanism for section selection.
	//
	// Introduced by #1860 / #1873 to make the gating contract explicit so that
	// downstream consumers (and future profile authors) can audit which sections
	// are intentionally suppressed for which seed kinds without re-deriving the
	// rule from the section lists.
	SkipForKinds []string
}

// SectionGating documents the per-section gating contract: each entry lists the
// canonical (lower-case) kind stems for which the section is suppressed.  This
// is the declarative form of the per-kind section curation expressed by the
// profile registry — keep the two in sync when adding a new profile.
//
// Wiring (#1860 / #1873 / #2017 / #1863):
//
//   - reference-deployment, reference-scripts → suppressed for leaf entity
//     kinds (view, class, function, operation) because deployment / script
//     surface is a module-aggregate concern.
//   - how-to-local-dev → suppressed for EVERY non-module kind (#1873).  Module
//     pages are the single source of truth for local-dev workflows.
//   - child-methods → suppressed for all non-class kinds (#1863).  Only
//     class-like seeds (view, class) emit a per-method table; module, model,
//     function, operation, and react kinds do not have a class_manifest to
//     render.
var SectionGating = map[string][]string{
	"reference-deployment": {"view", "class", "function", "operation"},
	"reference-scripts":    {"view", "class", "function", "operation"},
	// react_hook does not expose deployment/scripts surface — those live in
	// the owning js_module or the top-level module page.
	"how-to-local-dev": {"view", "class", "function", "operation", "model", "react_component", "react_hook"},
	// child-methods is only relevant for class-like seeds with a class_manifest.
	// All non-class-like kinds are listed here as suppressed.
	"child-methods": {"module", "model", "function", "operation", "react_component", "react_hook", "js_module"},
}

// ShouldSkipSectionForKind reports whether a given section is gated out for the
// supplied entity kind.  The kind is matched case-insensitively against the
// canonical stems in SectionGating using substring containment so dotted
// prefixes ("SCOPE.View") and compound names ("OperationHandler") resolve
// correctly.
//
// Callers that already drive section selection through ResolveSectionProfile
// do not need to consult this helper — the per-kind profile lists already omit
// the gated sections.  ShouldSkipSectionForKind exists to expose the rule for
// tests, audits, and downstream consumers that operate on the flat KnownSections
// list and want a single authoritative answer per (section, kind) pair.
func ShouldSkipSectionForKind(section, kind string) bool {
	gated, ok := SectionGating[section]
	if !ok {
		return false
	}
	k := strings.ToLower(kind)
	for _, stem := range gated {
		if strings.Contains(k, stem) {
			return true
		}
	}
	return false
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
			// #1875 — ORM Model branch: models have no callable API of their own;
			// document the natural API surface via viewsets/handlers in neighbours.
			"api": "This entity is an ORM Model (Django/SQLAlchemy/Prisma/etc.) with no callable API of its own. " +
				"Document its natural API surface in two parts:\n" +
				"1. Data interface: field names with types, associations (has-many, belongs-to, etc.), validation rules, " +
				"scopes, callbacks, and computed attributes that are part of the public contract.\n" +
				"2. Handler surface: explicitly state that the model has no direct callable API. " +
				"List the viewsets/handlers/resolvers visible in neighbour_briefs that operate on this model. " +
				"For each, note the CRUD endpoints it exposes if present in the neighbour's brief or Properties. " +
				"NEVER fabricate Contract.save() / Model.objects.filter() signatures — those are ORM internals, not the model's own API.",
			// #1859 — anti-patterns / smells in model context.
			"patterns": "Identify (a) data modelling patterns present (aggregate root, polymorphic association, STI, soft-delete, " +
				"event-sourced projection, etc.) and (b) any anti-patterns or code smells visible in the source_window " +
				"(missing db_index on high-cardinality FK fields, overly wide god-tables, nullable fields with no null-check logic, " +
				"bare except on DoesNotExist, commented-out alternate implementations, etc.). " +
				"Be specific; cite line ranges from source_window when possible. " +
				"Cite specific neighbour relationships as evidence for structural patterns.",
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
			// #1883 — Module flows: archetypal patterns + 2-3 mermaid diagrams.
			"flows": "Describe ARCHETYPAL FLOW PATTERNS observed in this module — 2-3 representative flows that cover the module's " +
				"common operations. Examples: HTTP-request-to-DB path, async-task-via-Celery/Sidekiq/Bull, scheduled-batch-job. " +
				"Use 2-3 mermaid sequence or flowchart diagrams (one per archetype). " +
				"Pick flows backed by the largest typed-edge sub-graphs visible in neighbour_briefs. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			// #1882 — Module api: catalog mode, not per-endpoint enumeration.
			"api": "Summarise the API as a CATALOG — DO NOT enumerate individual endpoints (impossible at scale). " +
				"Include:\n" +
				"- Total endpoint count (e.g. 473)\n" +
				"- Verb breakdown (e.g. 190 GET / 108 POST / 65 PUT / 42 PATCH / 36 DELETE)\n" +
				"- URL-prefix shape (e.g. /api/v1/, /api/v2/)\n" +
				"- List of top-level ViewSets/handlers/controllers with one-line descriptions\n" +
				"Source endpoint counts and ViewSet names from module_manifest.endpoints and neighbour_briefs. " +
				"Link to per-ViewSet pages for endpoint-level detail. " +
				"If no endpoint data is available, describe exported functions, event topics, or CLI flags instead.",
			// #1859 — anti-patterns / smells in module context.
			"patterns": "Identify (a) architectural and structural patterns observed across this module " +
				"(layered architecture, CQRS, event-driven, saga orchestration, hexagonal/ports-and-adapters, etc.) and " +
				"(b) any anti-patterns or code smells visible in the source_window or inferable from neighbour_briefs " +
				"(god-module with too many unrelated responsibilities, circular dependencies, missing transaction boundaries, " +
				"bare exception handlers, hardcoded magic values, commented-out alternate code paths, etc.). " +
				"Be specific; cite entity names and line ranges from source_window when possible. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"reference-config": "List every APPLICATION environment variable or configuration key this module reads, with type, default value, and effect. " +
				"Separate required from optional keys. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration. " +
				"If the module has no application config, say so in one sentence.",
			"reference-deployment": "Describe deployment concerns owned by this module: required env vars, exposed ports, " +
				"scaling constraints, health-check endpoints, and any sidecar dependencies.",
			"reference-scripts": "List all Makefile targets, npm/go scripts, or shell commands that operate on this module and explain what each does.",
			// #1858 — module_configs[] grounding for how-to-local-dev.
			"how-to-local-dev": "Provide a numbered step-by-step local development guide for this module: " +
				"clone, configure env vars, build, run tests, start the local server, and observe output. " +
				"IMPORTANT: consume `module_configs[]` to ground this section in repo-specific reality — " +
				"use actual Makefile targets, pyproject.toml/package.json scripts, and README excerpts from module_configs " +
				"instead of generic boilerplate. If module_configs includes a README excerpt, use it as the starting point.",
			// #1858 — module_configs[] grounding for module-readme.
			"module-readme": "Write a README-style introduction for this module: purpose, key entities, " +
				"quickstart build/test/run commands, and link to the full documentation page. " +
				"IMPORTANT: consume `module_configs[]` to ground this section in repo-specific reality — " +
				"use actual pyproject.toml/package.json/go.mod data and any README excerpts present in module_configs " +
				"for install/run instructions instead of generic boilerplate. " +
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
				"Show the caller → this operation → callees chain and any branching conditions. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			"patterns": "Identify structural patterns present in this operation " +
				"(guard clause, strategy delegation, saga step, command-query separation, etc.). " +
				"Cite specific callee relationships as evidence. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the function signature in full: parameters (name, type, purpose), return value(s), " +
				"and raised errors or panics. Include a minimal usage example.",
			"reference-config": "List APPLICATION configuration read inside this operation: environment variables, settings module constants, " +
				"feature flags, runtime parameters. Note which values alter branching behaviour. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration. " +
				"If the operation has no application config, say so in one sentence.",
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
			"flows": "Describe the execution path briefly. A single mermaid flowchart node or a short prose paragraph is sufficient. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			"patterns": "Note any design pattern (guard clause, delegation, pure function, etc.) in one sentence. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the function signature: parameters (name, type), return value, and any errors raised. " +
				"One-liner usage example if the call site is non-obvious.",
			"reference-config": "Note any APPLICATION config key, environment variable, or feature flag read by this helper. " +
				"DO NOT include graph-metadata Properties (framework, module, role, etc.) — those are indexer-internal and not configuration. " +
				"If the helper has no application config, say so in one sentence.",
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
				"Show the caller → this operation → all major callees and any fork/join or retry loops. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			"patterns": "Identify ALL structural and architectural patterns present " +
				"(orchestrator, saga, pipeline, strategy, command, etc.). " +
				"Cite specific neighbour relationships as evidence for each. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the full function signature: every parameter (name, type, purpose), all return values, " +
				"and every error or panic path. Include a realistic usage example showing a typical call site.",
			"reference-config": "List every APPLICATION environment variable, config key, or feature flag read by this operation. " +
				"Note which alter branching behaviour and which are required vs optional. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration. " +
				"If the operation has no application config, say so in one sentence.",
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
	// #1870: extend patterns guidance with the full frontend vocabulary —
	// container/presentational, render-props, portal, route-level, form
	// orchestrator, etc.
	// #1871: extend api guidance to surface outbound HTTP calls that some
	// components make directly via React Query / SWR hooks.
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
			// #1871 — outbound HTTP api guidance for components that issue calls directly.
			"api": "Document the props interface — NOT the generic function signature. " +
				"For each prop: name, TypeScript type (or JSDoc @param type), default value, required/optional, and one-line purpose. " +
				"If TypeScript: pull the interface or type alias from the declared Props type. " +
				"If JavaScript with JSDoc: pull from @param tags on the component function. " +
				"Show the JSX.Element return semantics (what markup is produced). " +
				"Document children semantics: are children accepted, required, or forbidden? " +
				"If this component issues outbound HTTP calls directly (via useQuery, useSWR, fetch, or axios inside the component body) " +
				"document each call: HTTP method, URL pattern, request body shape, response shape, and error/loading state handling. " +
				"Show a minimal JSX usage example. " +
				"NEVER reuse the generic function-signature template for this section.",
			// #1870 — expanded frontend pattern vocabulary for components.
			"patterns": "Identify React composition patterns present — consider all of the following: " +
				"- Container/presentational split (this component fetches or transforms data; a sibling renders it, or vice-versa) " +
				"- Render prop (accepts a function child or render prop for flexible slot rendering) " +
				"- Higher-order component wrapper (wraps another component to inject props or context) " +
				"- Controlled / uncontrolled input (manages its own state vs delegates to a parent via value+onChange) " +
				"- Context consumer (reads from a React context provider up the tree) " +
				"- Portal (renders children outside the normal DOM hierarchy via ReactDOM.createPortal) " +
				"- Route-level component (registered directly with a router; owns a URL segment and lazy-loads children) " +
				"- Form orchestrator (owns form state, validation schema, and submission lifecycle for a multi-field form). " +
				"Cite specific prop names, hook calls, or JSX structure as evidence for each identified pattern. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
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
	// react_hook — React custom hook (function whose name starts with "use").
	//
	// #1869 (reference-deployment/scripts frontend branch): hooks are consumed
	// inside components and never deployed directly; omit deployment/scripts/
	// how-to-local-dev.  The scripts guidance is still present but scoped to
	// the npm dev-server commands a hook author needs.
	// #1870 (frontend patterns vocabulary): hooks are the canonical unit of
	// logic extraction in React; guide the LLM toward hook-specific patterns.
	// #1871 (outbound HTTP api section): hooks are the primary place fetch/
	// axios/React Query calls live; document them as the outbound HTTP surface.
	// -------------------------------------------------------------------------
	"react_hook": {
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
			"overview": "Write a 2–3 sentence description of what this React custom hook provides and when it should be used. " +
				"State its single responsibility: does it manage state, side-effects, data-fetching, or form logic?",
			"capabilities": "List the observable behaviours this hook exposes. " +
				"One bullet per returned value, callback, or side-effect that callers depend on.",
			// #1871 — outbound HTTP api guidance for hooks.
			"api": "Document the hook's public interface — NOT a generic function signature. " +
				"For each parameter: name, TypeScript type, default value, required/optional, and purpose. " +
				"For each return value: name, type, and when/why a caller uses it. " +
				"If the hook issues outbound HTTP calls (fetch, axios, React Query, SWR, etc.) document each call: " +
				"HTTP method, URL pattern, request body shape, response shape, and error handling contract. " +
				"Include a minimal usage example showing the hook call and a representative return-value destructure.",
			// #1870 — frontend pattern vocabulary for hooks.
			"patterns": "Identify hook composition patterns present: " +
				"- Data-fetching hook (wraps fetch/axios/React Query/SWR for a specific resource) " +
				"- Form orchestrator (manages form state, validation, submission lifecycle) " +
				"- State machine hook (models a finite set of states with explicit transitions) " +
				"- Effect wrapper (encapsulates a useEffect with a cleanup contract) " +
				"- Context accessor (reads from a React context and re-exposes values) " +
				"- Derived-state hook (computes a memoised value from other hooks or props). " +
				"Cite the specific useState/useEffect/useCallback/useQuery calls that demonstrate each pattern. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"reference-config": "List any environment variables, feature flags, or config context values that alter this hook's behaviour " +
				"(e.g. API base URL, feature-flag toggles, timeout constants).",
			"reference-dependencies": "List direct package dependencies used inside this hook " +
				"(e.g. react-query, swr, axios, zod) and any internal context providers or service modules it calls.",
			"reference-misc": "Capture accessibility notes, known edge cases (stale-closure bugs, infinite re-render traps), " +
				"or performance considerations (memoisation strategy, query caching TTL).",
			"glossary": "Define domain terms appearing in the hook name, parameter names, or return value names. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that owns this hook. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// js_module — JavaScript/TypeScript module (non-React, non-class file).
	//
	// Covers utility modules, service layers, API client files, store slices,
	// configuration modules, and barrel index files emitted as "js_module" or
	// "module" kind by the JS/TS extractor.
	//
	// #1869 (reference-deployment/scripts frontend branch):
	//   - reference-deployment guidance asks for frontend build/hosting details
	//     (CDN, Vite preview, Next.js server) rather than backend daemon config.
	//   - reference-scripts guidance lists npm run dev / vite build / next start
	//     and similar JS-ecosystem commands, not Gradle / manage.py / make.
	// #1870 (frontend patterns vocabulary): guide the LLM toward JS module
	//   patterns (barrel re-export, singleton service, factory, observer, etc.).
	// #1871 (outbound HTTP api section): JS modules are the canonical location
	//   for API client wrappers; document every fetch/axios/React Query call.
	// -------------------------------------------------------------------------
	"js_module": {
		Sections: []string{
			"overview",
			"capabilities",
			"api",
			"patterns",
			"reference-config",
			"reference-dependencies",
			"reference-deployment",
			"reference-scripts",
			"reference-misc",
			"glossary",
			"module-readme",
		},
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this JavaScript/TypeScript module owns and its role in the frontend. " +
				"State whether it is a service layer, utility library, API client, state store slice, or barrel re-export.",
			"capabilities": "Enumerate the product capabilities this module provides, grouped by consumer. " +
				"One bullet per exported function, class, or constant that has a meaningful consumer contract.",
			// #1871 — outbound HTTP api guidance for JS modules.
			"api": "Document the full public export surface of this module. " +
				"For each exported symbol: name, TypeScript type signature, and a one-line purpose. " +
				"If this module issues outbound HTTP calls (fetch, axios, React Query, SWR, GraphQL client, etc.) document each call: " +
				"HTTP method, URL pattern (include base URL or env-var reference if present), " +
				"request body shape, response shape, and error handling contract. " +
				"Include a minimal usage example for non-trivial exports.",
			// #1870 — frontend pattern vocabulary for JS modules; #1859 — anti-patterns.
			"patterns": "Identify (a) JavaScript module patterns present: " +
				"- Barrel re-export (index file aggregating sibling modules) " +
				"- Singleton service (stateful module exporting a single shared instance) " +
				"- API client wrapper (thin layer over fetch/axios exposing typed request helpers) " +
				"- Factory function (creates and returns configured objects or instances) " +
				"- Observer/event bus (exports subscribe/publish or addEventListener abstractions) " +
				"- Configuration provider (reads env vars and exports typed config objects). " +
				"and (b) any anti-patterns or code smells visible in the source_window " +
				"(missing error handling on fetch/axios calls, uncaught promise rejections, hardcoded base URLs or API keys, " +
				"barrel files that re-export everything causing large bundle sizes, circular imports, etc.). " +
				"Be specific; cite line ranges from source_window when possible. " +
				"Cite the specific exports or import graph edges that demonstrate each pattern. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			// #1869 — frontend-specific reference-deployment.
			"reference-deployment": "Describe frontend deployment concerns owned by this module: " +
				"- Build artifact: where does Vite / webpack / Next.js emit this module's output? " +
				"- Runtime environment: is it loaded in the browser, a Node.js server (Next.js SSR / API route), or both? " +
				"- Environment variables referenced at build time (VITE_*, NEXT_PUBLIC_*) vs runtime. " +
				"- CDN or static-hosting constraints (cache headers, asset hashing, chunk splitting). " +
				"If the module has no deployment surface, say so in one sentence.",
			// #1869 — frontend-specific reference-scripts.
			"reference-scripts": "List the npm / yarn / pnpm scripts and CLI commands that operate on this module or its containing package. " +
				"Include at minimum: " +
				"- `npm run dev` (or `vite`, `next dev`) — local development server start command " +
				"- `vite build` / `next build` / `tsc` — production build " +
				"- `next start` / `vite preview` — preview the production build locally " +
				"- Test runner command (jest, vitest, playwright) " +
				"- Type-check command (tsc --noEmit). " +
				"State what each command does and when a developer should use it. " +
				"If the package.json scripts differ from the above, document the actual script names.",
			"reference-config": "List APPLICATION configuration read by this module: environment variables (VITE_*, NEXT_PUBLIC_*, etc.), " +
				"runtime config objects, or feature flags. Note which are required vs optional. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration.",
			"reference-dependencies": "List direct external package dependencies (npm packages) and internal module dependencies imported by this module. " +
				"Separate production from dev/test-only dependencies.",
			"reference-misc": "Capture bundle-size impact notes, tree-shaking behaviour, known edge cases, or links to the ADR / issue that introduced this module.",
			"glossary": "Define domain terms appearing in exported symbol names or type names. One term per row.",
			// #1858 — module_configs[] grounding for js_module module-readme.
			"module-readme": "Write a README-style introduction for this frontend module: purpose, key exports, " +
				"quickstart dev/build commands, and any important caveats. " +
				"IMPORTANT: consume `module_configs[]` to ground this section in repo-specific reality — " +
				"use actual package.json scripts, README excerpts, or tsconfig data from module_configs " +
				"for install/run instructions instead of generic boilerplate. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// view — class-kind seed (Django ViewSet, FastAPI router class, Flask View,
	// DRF APIView, Rails controller-class, etc.).
	//
	// #1860 — gate reference-deployment / reference-scripts / how-to-local-dev
	// out of the section list: these are module-aggregate concerns and produced
	// ~3 sections of "limited context" boilerplate per leaf-class page.
	// #1865 — flows section MUST short-circuit fabrication when method bodies
	// are out of the source_window (the seed window typically holds only the
	// class header).
	// #1866 — api section MUST forbid decorator/path inference when the
	// decorator parameters are not in the source_window.
	// -------------------------------------------------------------------------
	"view": {
		Sections: []string{
			"overview",
			// #1863 — child-methods table placed after overview for at-a-glance method index.
			"child-methods",
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
		SkipForKinds: []string{}, // applies TO view, not skipped FOR view
		GuidanceOverrides: map[string]string{
			"overview": "Write a 2–3 sentence description of what this view/controller class exposes and the request lifecycle it owns. " +
				"State its role in the routing layer (collection endpoint, detail endpoint, RPC-style action handler, etc.).",
			// #1863 — child-methods: view-specific guidance with HTTP columns.
			"child-methods": "Render a markdown table listing every action/handler method from class_manifest. " +
				"Columns: Method | HTTP Verb | Path | Brief Description (~10 words). " +
				"Populate HTTP Verb and Path from @action / @api_view / @router decorator metadata when present in source_window or neighbour Properties; " +
				"otherwise mark as not-in-context. " +
				"This section provides an at-a-glance index of the HTTP surface before the full api section.",
			"capabilities": "List the HTTP-facing capabilities this class provides, one bullet per action or endpoint. " +
				"Reference the action method name and the documented business outcome — not the implementation details, which live on per-method pages.",
			"flows": "You will see only the source_window for the seed class header; method bodies are NOT in scope. " +
				"Trace the DISPATCH-level flow (router → ViewSet method → serializer → response) and explicitly defer per-method internals to per-method pages. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			// #1859 — anti-patterns / smells in view context.
			"patterns": "Identify (a) class-level patterns (ViewSet mixin composition, generic-view inheritance, permission/throttle stacking, decorator-driven dispatch) and " +
				"(b) any anti-patterns or code smells visible in the source_window " +
				"(missing permission_classes, bare except handlers, fabricated decorator parameters, missing transaction.atomic on multi-write actions, etc.). " +
				"Be specific; cite line ranges from source_window when possible. " +
				"Cite specific neighbour relationships as evidence. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the HTTP surface this class exposes: one row per action method. " +
				"For each action: name, HTTP verb(s), URL path, request body schema, response schema, status codes. " +
				"If @action / @api_view / @router decorator parameters are NOT in source_window or neighbour Properties, " +
				"mark verb=not-in-context and path=not-in-context rather than inferring from method name or convention. " +
				"NEVER infer verbs or paths from naming conventions when the decorator string is unavailable.",
			"reference-config": "List APPLICATION configuration the class reads or writes: environment variables, settings module constants " +
				"(e.g. `settings.X`, `SETTINGS.Y`), feature flags, runtime parameters, permission classes referenced by name. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration. " +
				"If the class has no application config, say so in one sentence rather than inventing config from the surrounding stack.",
			"reference-dependencies": "List direct external and internal dependencies this class imports or composes " +
				"(serializers, permission classes, filter backends, services). Separate production from test-only.",
			"reference-misc": "Capture custom router wiring, ordering of mixins, or links to the ADR / issue that introduced this view.",
			"glossary": "Define domain terms appearing in the class name, action names, or serializer field names. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that owns this view. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// class — generic class seed (non-View, non-Model, non-react_component).
	//
	// Same gating as `view`: drop deployment / scripts / local-dev because they
	// are module-aggregate concerns (#1860 / #1873).  Same fabrication guards on
	// flows and api as `view` (#1865 / #1866) — method bodies typically sit
	// outside the source_window for class-kind seeds.
	// -------------------------------------------------------------------------
	"class": {
		Sections: []string{
			"overview",
			// #1863 — child-methods table placed after overview for at-a-glance method index.
			"child-methods",
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
			"overview": "Write a 2–3 sentence description of what this class represents and the responsibility it owns. " +
				"State its role in the module (service, value object, adapter, coordinator, etc.).",
			// #1863 — child-methods: generic class guidance (no HTTP columns).
			"child-methods": "Render a markdown table listing every public method from class_manifest. " +
				"Columns: Method | Signature | Visibility | Line | Brief Description (~10 words). " +
				"Visibility is one of: public / private / protected / package-private. " +
				"Brief Description should be derived from the method's own brief in neighbour_briefs or inferred from its name. " +
				"This section provides an at-a-glance index of what the class exposes before the full api section.",
			"capabilities": "List the discrete behaviours this class provides, one bullet per public method or observable contract. " +
				"Defer per-method implementation details to the per-method pages.",
			"flows": "You will see only the source_window for the seed class header; method bodies are NOT in scope. " +
				"Trace the class-level collaboration (caller → this class → collaborators) and explicitly defer per-method internals to per-method pages. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			// #1859 — anti-patterns / smells in class context.
			"patterns": "Identify (a) class-level structural patterns (template method, strategy, factory, builder, value object, etc.) and " +
				"(b) any anti-patterns or code smells visible in the source_window " +
				"(god-class with too many responsibilities, missing error handling, bare except/catch-all handlers, " +
				"commented-out alternate implementations, hardcoded magic values, etc.). " +
				"Be specific; cite line ranges from source_window when possible. " +
				"Cite specific neighbour relationships as evidence. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the public method surface of this class: one row per public method (name, signature, returns, raises). " +
				"If decorator parameters (e.g. @route, @action) are NOT in source_window or neighbour Properties, " +
				"mark verbs/paths/params as not-in-context rather than inferring from method name or convention.",
			"reference-config": "List APPLICATION configuration the class reads: environment variables, settings module constants, feature flags. " +
				"DO NOT include graph-metadata Properties (framework, module, role, language, etc.) — those are indexer-internal and not configuration. " +
				"If the class has no application config, say so in one sentence rather than inventing config from the surrounding stack.",
			"reference-dependencies": "List direct external and internal dependencies this class imports or composes. " +
				"Separate production from test-only.",
			"reference-misc": "Capture inheritance quirks, mixin ordering, known edge cases, or links to the ADR / issue that introduced this class.",
			"glossary": "Define domain terms appearing in the class name or method names. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that owns this class. " +
				"Do NOT mention sibling entities unless they appear in `module_manifest.classes`, " +
				"`module_manifest.functions`, or `neighbour_briefs`. " +
				"If you cite a sibling, name the bundle field it came from.",
		},
	},

	// -------------------------------------------------------------------------
	// function — top-level function or free-standing callable.
	//
	// #1860 — drop reference-deployment / reference-scripts / how-to-local-dev:
	// a free function is a leaf surface, not a deployment unit.
	// #2017 — flows section must respect bundle-visible entities only.
	// -------------------------------------------------------------------------
	"function": {
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
			"overview": "Write a 2–3 sentence description of what this function does and when it is called. " +
				"Highlight whether it is a leaf helper, a dispatcher, or a critical-path entry point.",
			"capabilities": "List the observable behaviours of this function in 1–3 bullets. One bullet per side-effect or return-value contract.",
			"flows": "Trace the execution flow through this function using a mermaid sequence or short flowchart. " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`. " +
				"Mermaid diagrams must only reference entities that exist in the bundle. " +
				"If thin context yields a one-step chain, render that one step honestly — do not pad with invented destinations.",
			"patterns": "Note any design pattern (guard clause, delegation, pure function, decorator, etc.). " +
				"Do NOT mention entities or edges that are not in `neighbour_briefs` or `module_manifest`.",
			"api": "Document the function signature: parameters (name, type, purpose), return value(s), raised errors. Include a minimal usage example.",
			"reference-config": "List APPLICATION configuration this function reads: environment variables, settings constants, feature flags. " +
				"DO NOT include graph-metadata Properties (framework, module, role, etc.) — those are indexer-internal and not configuration. " +
				"If the function has no application config, say so in one sentence.",
			"reference-dependencies": "List direct external and internal dependencies called by this function. Separate production from test-only.",
			"reference-misc": "Capture performance notes or edge cases specific to this function.",
			"glossary": "Define domain terms in the function or parameter names. One term per row.",
			"module-readme": "Write a brief README-style intro for the module that contains this function. " +
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

// isFrontendLanguage returns true when the language string indicates a
// JavaScript or TypeScript codebase.  Used to select frontend-aware profiles.
func isFrontendLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript", "js", "ts", "jsx", "tsx":
		return true
	}
	return false
}

// ResolveSectionProfile returns the SectionProfile for the given entity kind
// and language.  The lookup rules are:
//
//  1. Language-specific Component override for Java (#1995).
//  2. Language-aware frontend override (#1869 / #1870 / #1871): when language
//     is JavaScript/TypeScript and kind contains "module", the js_module
//     profile is used so that frontend-specific deployment/scripts/api
//     guidance replaces the generic backend module guidance.
//  3. For Operation kinds, if lineCount is provided, a size tier is applied
//     first: "operation.small" (<30 lines), "operation" (30–149 lines),
//     or "operation.large" (>=150 lines).  See #1986.
//  4. Exact case-insensitive match on kind (e.g. "Model" → "model").
//  5. Substring match — "SCOPE.Model" contains "model" → "model" profile.
//  6. Fallback to "default" when no match is found.
//
// The language parameter selects language-aware profiles where they exist
// (Java component → whole-body window; JS/TS module → frontend guidance).
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

	// 0b. Language-aware frontend module override (#1869 / #1870 / #1871).
	//     When language is JS/TS and the kind is a module (but NOT already an
	//     exact "js_module" which is caught at step 2), select the js_module
	//     profile so that frontend deployment/scripts/api guidance is used
	//     instead of the backend-oriented module profile.
	//     Exact "js_module" kind strings are handled below at the exact-match
	//     step; this branch covers plain "module" with language=javascript/typescript.
	if isFrontendLanguage(lang) && k != "js_module" && strings.Contains(k, "module") {
		if p, ok := sectionsByKind["js_module"]; ok {
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
	//    "js_module" and "react_hook" are resolved here before the substring
	//    scan can confuse them with "module" or another partial key.
	if p, ok := sectionsByKind[k]; ok {
		return p
	}

	// 3. Substring match — covers dotted prefixes ("SCOPE.Model") and
	//    compound kind names ("DataModel", "ServiceModule", "OperationHandler").
	//    Skip internal size-tier keys (e.g. "operation.small") and
	//    underscore-compound keys (e.g. "js_module", "react_hook") to avoid
	//    accidentally matching a substring when the exact match already failed.
	for key, profile := range sectionsByKind {
		if key == "default" {
			continue
		}
		// Skip dotted internal size-tier keys — only reachable via the explicit
		// tier path above.
		if strings.Contains(key, ".") {
			continue
		}
		// Skip underscore-compound keys (js_module, react_hook, react_component)
		// from the substring scan.  These are either already matched at the exact
		// step above, or must NOT bleed into unrelated kinds via partial match
		// (e.g. "module" appearing inside "js_module" would be a false positive).
		if strings.Contains(key, "_") {
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
