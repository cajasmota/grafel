package main

import "sort"

// Bucket names. The four buckets group categories into the four
// rendering lanes used by summary.md and the per-language pages
// (issue #2725). Bucket → category membership and render order are
// declared in capability-dictionary.yaml (#2752).
const (
	BucketFrameworks = "Frameworks"
	BucketTools      = "Tools"
	BucketORMs       = "ORMs"
	BucketOther      = "Other"
)

// bucketOrder snapshots the dictionary's bucket render order at package
// init so the existing call sites in generate.go can iterate it
// directly (`for _, b := range bucketOrder`). Going through dict() at
// init time is safe because the dictionary's embedded fallback is
// always available — the loader never fails for a well-formed binary.
var bucketOrder = dict().BucketOrder()

// bucketOf returns the bucket name for category. Unknown categories
// fall through to BucketOther (matches the spec's "Other: everything
// else" rule so new categories ship as Other until they are explicitly
// classified in the dictionary).
func bucketOf(category string) string {
	return dict().BucketForCategory(category)
}

// bucketCapabilityKeys returns the capability columns shown in a
// bucket's per-language table. Derived from the dictionary's bucket →
// categories mapping so any new framework/orm/tool category
// automatically widens the union.
//
// For BucketOther we return nil — the per-language Other section uses
// a single "Status" digest column rather than per-capability columns.
func bucketCapabilityKeys(bucket string) []string {
	if bucket == BucketOther {
		return nil
	}
	seen := map[string]struct{}{}
	d := dict()
	b, ok := d.Buckets[bucket]
	if !ok {
		return nil
	}
	for _, cat := range b.Categories {
		for _, k := range d.CategoryCapabilities(cat) {
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

// statusGlyph maps a capability status to its per-cell support glyph,
// aligned with the group-level supportGlyph palette so the whole board
// speaks one visual language: ✅ comprehensive (full) · 🟢 supported,
// i.e. extracted heuristically (partial) · 🔴 not extracted (missing) ·
// — not applicable / not declared. Empty input renders as "—".
func statusGlyph(status string) string {
	switch status {
	case StatusFull:
		return "✅"
	case StatusPartial:
		return "🟢"
	case StatusMissing:
		return "🔴"
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
