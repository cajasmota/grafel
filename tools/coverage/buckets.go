package main

import "sort"

// Bucket names. The four buckets group categories into the four
// rendering lanes used by summary.md and the per-language pages
// (issue #2725). The order here is the canonical render order:
// Frameworks → Tools → ORMs → Other.
const (
	BucketFrameworks = "Frameworks"
	BucketTools      = "Tools"
	BucketORMs       = "ORMs"
	BucketOther      = "Other"
)

// bucketOrder is the fixed display order for the four buckets. Used by
// templates and the summary pivot table.
var bucketOrder = []string{BucketFrameworks, BucketTools, BucketORMs, BucketOther}

// categoryBucket maps a registry category to its bucket. Anything not
// listed here falls into BucketOther (this matches the spec's "Other:
// everything else" rule and means new categories ship as Other until
// the maintainer classifies them explicitly).
var categoryBucket = map[string]string{
	// Frameworks
	"http_framework": BucketFrameworks,
	"web_framework":  BucketFrameworks,
	"nav_framework":  BucketFrameworks,
	"rpc_framework":  BucketFrameworks,
	"graphql":        BucketFrameworks,

	// Tools
	"build_system":    BucketTools,
	"build_tool":      BucketTools,
	"package_manager": BucketTools,
	"manifest":        BucketTools,
	"linter":          BucketTools,
	"formatter":       BucketTools,

	// ORMs
	"orm":            BucketORMs,
	"query_builder":  BucketORMs,
	"migration_tool": BucketORMs,
}

// bucketOf returns the bucket name for category. Unknown categories
// fall through to BucketOther.
func bucketOf(category string) string {
	if b, ok := categoryBucket[category]; ok {
		return b
	}
	return BucketOther
}

// bucketCapabilityKeys returns the capability columns shown in a
// bucket's per-language table. Derived from categoryCapabilities so
// any new framework/orm/tool category automatically widens the union.
//
// For BucketOther we return nil — the per-language Other section uses
// a single "Status" digest column rather than per-capability columns.
func bucketCapabilityKeys(bucket string) []string {
	if bucket == BucketOther {
		return nil
	}
	seen := map[string]struct{}{}
	for cat, b := range categoryBucket {
		if b != bucket {
			continue
		}
		for _, k := range categoryCapabilities[cat] {
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

// statusGlyph maps a capability status to its single-character glyph.
// Empty input (capability not declared on this record) renders as "—".
// The four glyphs (✅ ⚠️ ❌ —) are the rendering contract for #2725.
func statusGlyph(status string) string {
	switch status {
	case StatusFull:
		return "✅"
	case StatusPartial:
		return "⚠️"
	case StatusMissing:
		return "❌"
	case StatusNotApplicable, "":
		return "—"
	}
	return "—"
}

// digestStatus returns the worst-status across a capability map. Used
// by the "Other" bucket's single-column digest cell. Order of badness:
// missing > partial > full > not_applicable > (empty).
func digestStatus(caps map[string]Capability) string {
	rank := map[string]int{
		StatusMissing:       4,
		StatusPartial:       3,
		StatusFull:          2,
		StatusNotApplicable: 1,
		"":                  0,
	}
	worst := ""
	worstRank := -1
	for _, c := range caps {
		r := rank[c.Status]
		if r > worstRank {
			worstRank = r
			worst = c.Status
		}
	}
	return worst
}
