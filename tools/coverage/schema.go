// Package main implements the coverage registry CLI.
//
// The schema mirrors docs/coverage/registry.json. Keep this file in sync
// with the documented schema in issue #2720. Pure value types: zero
// imports from internal/ packages.
package main

import (
	"fmt"
	"regexp"
	"sort"
)

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
type Record struct {
	ID           string                `json:"id"`
	Category     string                `json:"category"`
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
