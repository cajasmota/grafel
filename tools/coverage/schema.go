// Package main implements the coverage registry CLI.
//
// The schema mirrors docs/coverage/registry.json. Keep this file in sync
// with the documented schema in issues #2720 (foundation), #2735
// (subcategory + per-subcategory capabilities), and #2737 (capability
// groups + group-digest rendering). Pure value types: zero imports from
// internal/ packages.
package main

import (
	"bytes"
	"encoding/json"
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
	"isr":  "ISR",
	"ios":  "iOS",
	"jni":  "JNI",
	"ui":   "UI",
	"ai":   "AI",
	"rpc":  "RPC",
	"ci":   "CI",
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

// uncategorizedGroup is the synthetic group bucket for capability keys
// that do not fit any canonical group declared in subcategoryGroups for
// a record's subcategory. Migration emits a warning when it places a
// key here so the taxonomy can be extended deliberately.
const uncategorizedGroup = "Uncategorized"

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
// resources) use "multi" and surface in the summary's cross-cutting
// infrastructure pivot rather than the per-language one.
//
// Subcategory is an OPTIONAL refinement of Category. It carves a broad
// category (e.g. http_framework) into honest narrower lanes (ui_frontend,
// mobile, meta_framework, ...). When set it MUST be one of the slugs
// declared for Record.Category in subcategoryCapabilities.
//
// Capabilities and Groups are mutually exclusive carriers of the per-
// record capability cells:
//
//   - Capabilities is the legacy flat map used by records without a
//     subcategory. Loaded when the on-disk "capabilities" object's
//     values are Capability cells (have a "status" field).
//   - Groups is the nested shape (group → key → cell) introduced by
//     #2737. Loaded when the on-disk values are sub-objects whose own
//     values are Capability cells. Group names are validated against
//     subcategoryGroups for the record's (category, subcategory).
//
// Use Record.AllCapabilities to iterate every cell regardless of shape.
type Record struct {
	ID           string
	Category     string
	Subcategory  string
	Language     string
	Label        string
	Capabilities map[string]Capability
	Groups       map[string]map[string]Capability
}

// Capability is a single capability cell on a record.
type Capability struct {
	Status      string   `json:"status"`
	Cites       []string `json:"cites,omitempty"`
	VerifiedAt  string   `json:"verified_at,omitempty"`
	VerifiedSHA string   `json:"verified_sha,omitempty"`
	Issue       string   `json:"issue,omitempty"`
}

// IsGrouped reports whether the record carries grouped capabilities.
func (r *Record) IsGrouped() bool { return len(r.Groups) > 0 }

// AllCapabilities returns every capability cell on the record as a flat
// map keyed by capability slug. For grouped records keys are flattened
// across all groups (each key appears exactly once per validation).
func (r *Record) AllCapabilities() map[string]Capability {
	if !r.IsGrouped() {
		out := make(map[string]Capability, len(r.Capabilities))
		for k, v := range r.Capabilities {
			out[k] = v
		}
		return out
	}
	out := map[string]Capability{}
	for _, g := range r.Groups {
		for k, v := range g {
			out[k] = v
		}
	}
	return out
}

// MarshalJSON emits the record in either the flat or the grouped shape
// based on whether Groups is populated. Capabilities maps are serialised
// with sorted keys via an intermediate ordered structure so the result
// is deterministic — callers that need byte-stable output should still
// prefer marshalRegistry, which controls indentation as well.
func (r Record) MarshalJSON() ([]byte, error) {
	type capWire struct {
		Status      string   `json:"status"`
		Cites       []string `json:"cites,omitempty"`
		VerifiedAt  string   `json:"verified_at,omitempty"`
		VerifiedSHA string   `json:"verified_sha,omitempty"`
		Issue       string   `json:"issue,omitempty"`
	}
	toWire := func(c Capability) capWire {
		return capWire{
			Status: c.Status, Cites: c.Cites, VerifiedAt: c.VerifiedAt,
			VerifiedSHA: c.VerifiedSHA, Issue: c.Issue,
		}
	}
	out := struct {
		ID           string         `json:"id"`
		Category     string         `json:"category"`
		Subcategory  string         `json:"subcategory,omitempty"`
		Language     string         `json:"language"`
		Label        string         `json:"label"`
		Capabilities map[string]any `json:"capabilities"`
	}{
		ID: r.ID, Category: r.Category, Subcategory: r.Subcategory,
		Language: r.Language, Label: r.Label,
		Capabilities: map[string]any{},
	}
	if r.IsGrouped() {
		for g, caps := range r.Groups {
			inner := map[string]capWire{}
			for k, c := range caps {
				inner[k] = toWire(c)
			}
			out.Capabilities[g] = inner
		}
	} else {
		for k, c := range r.Capabilities {
			out.Capabilities[k] = toWire(c)
		}
	}
	return json.Marshal(out)
}

// recordJSON is the on-wire shape used by Record's marshal helpers. The
// Capabilities field is deferred to json.RawMessage so UnmarshalJSON can
// inspect the inner structure and route to the flat or grouped target.
type recordJSON struct {
	ID           string          `json:"id"`
	Category     string          `json:"category"`
	Subcategory  string          `json:"subcategory,omitempty"`
	Language     string          `json:"language"`
	Label        string          `json:"label"`
	Capabilities json.RawMessage `json:"capabilities"`
}

// UnmarshalJSON decodes a record, distinguishing the flat capability
// shape ({"key": {"status": ...}}) from the grouped shape
// ({"Group": {"key": {"status": ...}}}) by inspecting whether each
// top-level value carries a "status" field.
func (r *Record) UnmarshalJSON(data []byte) error {
	var raw recordJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	r.ID = raw.ID
	r.Category = raw.Category
	r.Subcategory = raw.Subcategory
	r.Language = raw.Language
	r.Label = raw.Label
	r.Capabilities = nil
	r.Groups = nil
	if len(raw.Capabilities) == 0 || bytes.Equal(bytes.TrimSpace(raw.Capabilities), []byte("null")) {
		return nil
	}
	grouped, err := capabilitiesAreGrouped(raw.Capabilities)
	if err != nil {
		return fmt.Errorf("record %s: capabilities: %w", raw.ID, err)
	}
	if grouped {
		var g map[string]map[string]Capability
		gdec := json.NewDecoder(bytes.NewReader(raw.Capabilities))
		gdec.DisallowUnknownFields()
		if err := gdec.Decode(&g); err != nil {
			return fmt.Errorf("record %s: decode grouped capabilities: %w", raw.ID, err)
		}
		r.Groups = g
		return nil
	}
	var flat map[string]Capability
	fdec := json.NewDecoder(bytes.NewReader(raw.Capabilities))
	fdec.DisallowUnknownFields()
	if err := fdec.Decode(&flat); err != nil {
		return fmt.Errorf("record %s: decode flat capabilities: %w", raw.ID, err)
	}
	r.Capabilities = flat
	return nil
}

// capabilitiesAreGrouped peeks at the first non-empty inner object value
// in the capabilities map: if it carries a "status" field the outer map
// is flat (key → Capability); otherwise it is grouped (group → key →
// Capability). Empty objects mean the record has no cells declared yet,
// in which case the caller treats it as flat (no-op).
func capabilitiesAreGrouped(raw json.RawMessage) (bool, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false, err
	}
	if len(probe) == 0 {
		return false, nil
	}
	// Inspect in a deterministic key order so identical inputs always
	// resolve the same way (matters only for malformed mixed shapes).
	keys := make([]string, 0, len(probe))
	for k := range probe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(probe[k], &inner); err != nil {
			return false, fmt.Errorf("capability %q: not an object", k)
		}
		if _, hasStatus := inner["status"]; hasStatus {
			return false, nil
		}
		// No status: this value is itself a map of capabilities, so the
		// outer shape is grouped.
		return true, nil
	}
	return false, nil
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
	"databases": {
		"resource_extraction",
		"dependency_attribution",
	},
	"platform": {
		"resource_extraction",
		"dependency_attribution",
		"file_parsing",
		"env_resolution",
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
	"ci_cd": {
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

// subcategoryGroups declares the canonical capability-group taxonomy
// per subcategory: subcategory → group name → ordered capability keys.
// Group names are exact strings (case + spacing preserved); they appear
// verbatim as section headings on detail pages and as column headers on
// pivot tables. Capability keys must also be declared in
// subcategoryCapabilities for their (category, subcategory) so the
// allow-list and the taxonomy stay in sync.
//
// Groups are listed in display order — render order on the detail page
// and column order in the pivot table both follow this slice.
var subcategoryGroups = map[string][]capabilityGroup{
	"ui_frontend": {
		{Name: "Structure", Keys: []string{"component_extraction", "hook_recognition", "jsx_template"}},
		{Name: "Data Flow", Keys: []string{"prop_extraction", "state_management", "data_fetching"}},
		{Name: "Navigation", Keys: []string{"router_pattern"}},
		{Name: "Type System", Keys: []string{}},
		{Name: "Lifecycle", Keys: []string{}},
		{Name: "Testing", Keys: []string{}},
	},
	"mobile": {
		{Name: "Navigation", Keys: []string{"navigation_extraction", "deep_link_extraction", "screen_detection"}},
		{Name: "Platform", Keys: []string{"platform_branching"}},
		{Name: "Native Bridge", Keys: []string{"native_module_imports"}},
		{Name: "Data Flow", Keys: []string{"state_management"}},
		{Name: "Lifecycle", Keys: []string{}},
	},
	"http_backend": {
		{Name: "Routing", Keys: []string{"endpoint_synthesis", "handler_attribution", "route_extraction"}},
		{Name: "Security", Keys: []string{"auth_coverage"}},
		{Name: "Validation", Keys: []string{"request_validation", "dto_extraction"}},
		{Name: "Middleware", Keys: []string{"middleware_coverage"}},
		{Name: "Testing", Keys: []string{"tests_linkage"}},
		{Name: "Observability", Keys: []string{}},
		{Name: "Data", Keys: []string{}},
	},
	"meta_framework": {
		{Name: "Structure", Keys: []string{"component_extraction", "hook_recognition"}},
		{Name: "Data Flow", Keys: []string{"data_loaders"}},
		{Name: "Server", Keys: []string{"server_components", "hydration_boundaries"}},
		{Name: "Routing", Keys: []string{"route_extraction", "router_pattern"}},
		{Name: "Build", Keys: []string{"static_generation"}},
	},
	"desktop": {
		{Name: "Process", Keys: []string{"ipc_extraction", "main_renderer_split"}},
		{Name: "Native", Keys: []string{"native_module_imports"}},
		{Name: "Updates", Keys: []string{}},
	},
	"rpc_framework": {
		{Name: "Schema", Keys: []string{"schema_extraction", "procedure_extraction"}},
		{Name: "Codegen", Keys: []string{"client_codegen"}},
		{Name: "Transport", Keys: []string{}},
	},
	"ai_integration": {
		{Name: "Prompts", Keys: []string{"prompt_template_extraction"}},
		{Name: "Composition", Keys: []string{"chain_composition", "tool_use_detection"}},
		{Name: "Tracking", Keys: []string{}},
	},
}

// capabilityGroup pairs a group name with its ordered capability keys.
// Render order follows the slice; templates do not re-sort.
type capabilityGroup struct {
	Name string
	Keys []string
}

// groupsForSubcategory returns the canonical group taxonomy for sub or
// nil when the subcategory has no declared taxonomy (e.g. static_site).
func groupsForSubcategory(sub string) []capabilityGroup {
	return subcategoryGroups[sub]
}

// groupForCapability returns the canonical group name that owns key in
// sub's taxonomy, or "" when the key is not declared in any group. Used
// by migration and validation to enforce capability-belongs-to-group.
func groupForCapability(sub, key string) string {
	for _, g := range subcategoryGroups[sub] {
		for _, k := range g.Keys {
			if k == key {
				return g.Name
			}
		}
	}
	return ""
}

// groupAllowedKeys returns the declared keys for (sub, group) or nil
// when group is not part of sub's taxonomy. Validation uses this to
// reject keys placed under the wrong group within a record.
func groupAllowedKeys(sub, group string) []string {
	for _, g := range subcategoryGroups[sub] {
		if g.Name == group {
			return g.Keys
		}
	}
	return nil
}

// knownGroupNames returns the declared group names for sub in canonical
// render order, or nil when sub has no group taxonomy.
func knownGroupNames(sub string) []string {
	groups := subcategoryGroups[sub]
	if len(groups) == 0 {
		return nil
	}
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

// validGroupName reports whether group is part of sub's taxonomy. The
// synthetic uncategorizedGroup is always accepted so migration can park
// keys that fall outside the canonical taxonomy without losing data.
func validGroupName(sub, group string) bool {
	if group == uncategorizedGroup {
		return true
	}
	for _, g := range subcategoryGroups[sub] {
		if g.Name == group {
			return true
		}
	}
	return false
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
