// Package main implements the coverage registry CLI.
//
// The schema mirrors docs/coverage/registry.json. Keep this file in sync
// with the documented schema in issues #2720 (foundation) and #2735
// (subcategory + per-subcategory capabilities). Pure value types: zero
// imports from internal/ packages.
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// acronyms is the exception map used by prettyKey to keep canonical
// uppercase spellings in column headers ("DTO Extraction" rather than
// "Dto Extraction"). Add a slug here when a new acronym surfaces in
// capability keys; matching is case-insensitive against the original
// snake_case segment.
var acronyms = map[string]string{
	"dto":  "DTO",
	"jsx":  "JSX",
	"ipc":  "IPC",
	"http": "HTTP",
	"api":  "API",
	"orm":  "ORM",
	"sdk":  "SDK",
}

// prettyKey converts a snake_case capability or subcategory slug into
// a human-readable label: split on underscores, Title-case each segment,
// substitute known acronyms, and re-join with spaces. Empty input
// returns "". prettyKey is exposed to templates as the `prettyKey`
// helper (see templateFuncs in generate.go).
func prettyKey(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if a, ok := acronyms[strings.ToLower(p)]; ok {
			parts[i] = a
			continue
		}
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// SchemaVersion is the current registry schema version. Bump when the
// on-disk JSON shape changes incompatibly.
const SchemaVersion = 1

// Status enum values for a capability cell.
const (
	StatusFull          = "full"
	StatusPartial       = "partial"
	StatusMissing       = "missing"
	StatusNotApplicable = "not_applicable"
)

// validStatuses lists the allowed status enum values.
var validStatuses = map[string]struct{}{
	StatusFull:          {},
	StatusPartial:       {},
	StatusMissing:       {},
	StatusNotApplicable: {},
}

// idPattern matches stable dotted slug IDs:
//
//	lang.python.framework.django-drf
var idPattern = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)

// Registry is the root JSON document persisted at docs/coverage/registry.json.
type Registry struct {
	SchemaVersion int      `json:"$schema_version"`
	Records       []Record `json:"records"`
}

// Record is a single coverage row keyed by Record.ID.
//
// Language is a short language slug ("python", "go", "java", ...). The
// canonical slug for the JavaScript family is "jsts": archigraph's
// JS/TS extractor is shared across .js, .ts, .jsx, .tsx, .mjs and .cjs
// sources, so a single tag covers them all. Records that span multiple
// language ecosystems (build systems, observability vendors, infra
// resources) use "multi" and render under the "Uncategorized" pivot row.
//
// Subcategory is an OPTIONAL refinement of Category. It carves a broad
// category (e.g. http_framework) into honest narrower lanes (ui_frontend,
// mobile, meta_framework, ...). When set it MUST be one of the slugs
// declared for Record.Category in subcategoryCapabilities, and the
// record's capability keys are then validated against the union of
// (subcategory keys + category keys) rather than the category-wide list.
// Records without Subcategory keep the legacy category-only validation
// and render in their bucket's default column set.
type Record struct {
	ID           string                `json:"id"`
	Category     string                `json:"category"`
	Subcategory  string                `json:"subcategory,omitempty"`
	Language     string                `json:"language"`
	Label        string                `json:"label"`
	Capabilities map[string]Capability `json:"capabilities"`
}

// Capability is a single capability cell on a record.
type Capability struct {
	Status      string   `json:"status"`
	Cites       []string `json:"cites,omitempty"`
	VerifiedAt  string   `json:"verified_at,omitempty"`
	VerifiedSHA string   `json:"verified_sha,omitempty"`
	Issue       string   `json:"issue,omitempty"`
}

// categoryCapabilities maps each registry category to the set of
// capability keys that are valid for that category. The validate
// subcommand rejects unknown keys per category.
var categoryCapabilities = map[string][]string{
	"language": {
		"call_line_precision",
		"discriminates_on",
		"navigates_to",
		"core_extraction",
	},
	"http_framework": {
		"endpoint_synthesis",
		"handler_attribution",
		"auth_coverage",
		"middleware_coverage",
	},
	"orm": {
		"model_extraction",
		"query_attribution",
		"migration_parsing",
	},
	"message_broker": {
		"producer_extraction",
		"consumer_extraction",
		"topic_attribution",
	},
	"observability": {
		"trace_extraction",
		"metric_extraction",
		"log_extraction",
	},
	"build_system": {
		"target_extraction",
		"dependency_graph",
	},
	"package_manager": {
		"manifest_parsing",
		"lockfile_parsing",
	},
	"infrastructure": {
		"resource_extraction",
		"dependency_attribution",
	},
	"security": {
		"auth_policy",
		"secret_detection",
		"sql_injection",
	},
	"protocol": {
		"service_extraction",
		"method_attribution",
		"cross_repo_linkage",
	},
	"configuration": {
		"file_parsing",
		"env_resolution",
	},
}

// validCapabilityKey reports whether key is declared for category.
func validCapabilityKey(category, key string) bool {
	keys, ok := categoryCapabilities[category]
	if !ok {
		return false
	}
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// subcategoryCapabilities maps (category → subcategory → capability keys).
// Records that opt in to a subcategory validate against the union of
// that subcategory's keys and the category-wide keys, allowing fine-
// grained capability vocabulary (e.g. React's component_extraction) to
// coexist with the broad category lane (http_framework).
//
// Adding a new subcategory: drop a line here, list its capability keys,
// re-run validate. The slugs are surfaced verbatim as section headers
// in the by-language and by-category pages via prettyKey.
var subcategoryCapabilities = map[string]map[string][]string{
	"http_framework": {
		"http_backend": {
			"endpoint_synthesis",
			"handler_attribution",
			"auth_coverage",
			"middleware_coverage",
			"route_extraction",
			"request_validation",
			"tests_linkage",
			"dto_extraction",
		},
		"ui_frontend": {
			"component_extraction",
			"prop_extraction",
			"hook_recognition",
			"state_management",
			"data_fetching",
			"router_pattern",
			"jsx_template",
		},
		"meta_framework": {
			"server_components",
			"data_loaders",
			"route_extraction",
			"hydration_boundaries",
			"static_generation",
			"component_extraction",
			"hook_recognition",
			"router_pattern",
		},
		"mobile": {
			"navigation_extraction",
			"native_module_imports",
			"platform_branching",
			"screen_detection",
			"deep_link_extraction",
			"state_management",
		},
		"desktop": {
			"ipc_extraction",
			"main_renderer_split",
			"native_module_imports",
		},
		"rpc_framework": {
			"procedure_extraction",
			"schema_extraction",
			"client_codegen",
		},
		"static_site": {
			"build_extraction",
			"template_extraction",
		},
		"ai_integration": {
			"prompt_template_extraction",
			"chain_composition",
			"tool_use_detection",
		},
	},
}

// subcategoryOrder is the canonical render order for subcategory
// sub-sections under a bucket. Entries not listed here render
// alphabetically after the canonical ones (see orderedSubcategories).
var subcategoryOrder = map[string][]string{
	"http_framework": {
		"http_backend",
		"ui_frontend",
		"meta_framework",
		"mobile",
		"desktop",
		"rpc_framework",
		"static_site",
		"ai_integration",
	},
}

// subcategoryDisplay maps a subcategory slug to its human-facing section
// heading. Slugs without an entry fall back to prettyKey of the slug.
var subcategoryDisplay = map[string]string{
	"http_backend":   "Backend HTTP",
	"ui_frontend":    "UI Frontend",
	"meta_framework": "Meta Framework",
	"mobile":         "Mobile",
	"desktop":        "Desktop",
	"rpc_framework":  "RPC Framework",
	"static_site":    "Static Site",
	"ai_integration": "AI Integration",
}

// knownSubcategories returns the sorted subcategory slugs declared for
// category. Empty slice if category has no subcategory map.
func knownSubcategories(category string) []string {
	m, ok := subcategoryCapabilities[category]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validSubcategory reports whether sub is declared for category.
func validSubcategory(category, sub string) bool {
	m, ok := subcategoryCapabilities[category]
	if !ok {
		return false
	}
	_, ok = m[sub]
	return ok
}

// validCapabilityKeyForSubcategory reports whether key is in the union
// of (category keys + subcategory keys) for (category, sub). Callers
// SHOULD verify (category, sub) is a valid pair via validSubcategory
// before relying on a positive result.
func validCapabilityKeyForSubcategory(category, sub, key string) bool {
	if validCapabilityKey(category, key) {
		return true
	}
	m, ok := subcategoryCapabilities[category]
	if !ok {
		return false
	}
	keys, ok := m[sub]
	if !ok {
		return false
	}
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// subcategoryRenderKeys returns the sorted capability keys to render
// as columns for a subcategory-scoped table. Only the subcategory's
// own keys appear — category-wide keys (e.g. auth_coverage on a UI
// Frontend record) are deliberately excluded from the column set so
// each lane shows the vocabulary appropriate to it. Records may still
// carry category-level cells; those are surfaced on the per-record
// detail page but suppressed in the per-language summary table.
func subcategoryRenderKeys(category, sub string) []string {
	m, ok := subcategoryCapabilities[category]
	if !ok {
		return nil
	}
	keys, ok := m[sub]
	if !ok {
		return nil
	}
	out := make([]string, len(keys))
	copy(out, keys)
	sort.Strings(out)
	return out
}

// subcategoryCapabilityKeys returns the merged sorted capability key
// set for (category, sub) — union of subcategory keys and category-wide
// keys, de-duplicated. Used by validation (not rendering) so cells
// declared at either level pass the allow-list check.
func subcategoryCapabilityKeys(category, sub string) []string {
	seen := map[string]struct{}{}
	for _, k := range categoryCapabilities[category] {
		seen[k] = struct{}{}
	}
	if m, ok := subcategoryCapabilities[category]; ok {
		for _, k := range m[sub] {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// orderedSubcategories returns the subcategory slugs that have records
// in subs, ordered by subcategoryOrder[category] first then alphabetical
// for any extras. Slugs in order but absent from subs are dropped.
func orderedSubcategories(category string, subs map[string]bool) []string {
	canon := subcategoryOrder[category]
	out := make([]string, 0, len(subs))
	for _, s := range canon {
		if subs[s] {
			out = append(out, s)
		}
	}
	extras := make([]string, 0)
	known := map[string]bool{}
	for _, s := range canon {
		known[s] = true
	}
	for s := range subs {
		if !known[s] {
			extras = append(extras, s)
		}
	}
	sort.Strings(extras)
	return append(out, extras...)
}

// subcategoryHeading returns the human-facing heading for sub. Falls
// back to prettyKey when no display string is registered so brand-new
// subcategories render reasonably without code changes.
func subcategoryHeading(sub string) string {
	if d, ok := subcategoryDisplay[sub]; ok {
		return d
	}
	return prettyKey(sub)
}

// knownCategories returns sorted category names. Used by views and
// validation error messages.
func knownCategories() []string {
	out := make([]string, 0, len(categoryCapabilities))
	for k := range categoryCapabilities {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validateID returns nil if id matches the stable-slug pattern.
func validateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid id %q: must match %s", id, idPattern.String())
	}
	return nil
}
