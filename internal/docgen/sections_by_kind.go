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
package docgen

import "strings"

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
				"and how to run the associated tests.",
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
				"quickstart build/test/run commands, and link to the full documentation page.",
		},
	},

	// -------------------------------------------------------------------------
	// Operation — method, function, handler, endpoint, command.
	//
	// R1 dogfood findings: Operation pages (class/method) are self-contained
	// units of behaviour.  They do NOT need reference-deployment/scripts/
	// how-to-local-dev (module-level concerns) reducing boilerplate by ~3 sections.
	// They DO need flows, patterns, and api for signature documentation.
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
				"purpose and key sibling operations.",
		},
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

// ResolveSectionProfile returns the SectionProfile for the given entity kind
// and language.  The lookup rules are:
//
//  1. Exact case-insensitive match on kind (e.g. "Model" → "model").
//  2. Substring match — "SCOPE.Model" contains "model" → "model" profile.
//  3. Fallback to "default" when no match is found.
//
// The language parameter is accepted for future language-aware profiles
// (e.g. Component kind differs for Java vs React); it is currently unused.
// Pass an empty string when language is not available.
func ResolveSectionProfile(kind, _ string) SectionProfile {
	k := strings.ToLower(kind)

	// 1. Exact match (after lower-casing).
	if p, ok := sectionsByKind[k]; ok {
		return p
	}

	// 2. Substring match — covers dotted prefixes ("SCOPE.Model") and
	//    compound kind names ("DataModel", "ServiceModule", "OperationHandler").
	for key, profile := range sectionsByKind {
		if key == "default" {
			continue
		}
		if strings.Contains(k, key) {
			return profile
		}
	}

	// 3. Fallback to default — preserves current 13-section behaviour.
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
