// Package literalparity diffs two SCOPE.Enum / ConstantSet value-sets across
// two graphs (typically an oracle group and a v3-rewrite group) keyed off the
// structured members_json property emitted by the shared enum/value-set
// extractor (internal/extractor/enum_valueset.go, epic #3628 / #4420).
//
// The diff is GENERIC: it operates on the {key,value,line} member shape, not
// on any one named constant collection, so it answers the rewrite-parity
// question for ANY value-set in ANY language — "does the v3 rewrite reproduce
// the oracle's literal value-set key-for-key and value-for-value?".
//
// Three classes of difference are reported (ticket #4421, epic #4419):
//
//   - membership: keys present in only one side (OnlyInOracle / OnlyInV3).
//   - value_mismatches: the SAME aligned key carries a different literal value
//     on each side (e.g. oracle "core_admin" vs v3 "core-admin" — the _ vs -
//     separator-convention class the rewrite audit cares about).
//   - intra_v3_inconsistencies: within the v3 set alone, the value literals mix
//     separator/format conventions (e.g. "email_templates" underscore mixed
//     with "witnessing-companies" hyphen), which is a code-smell the rewrite
//     introduced regardless of the oracle.
//
// Key alignment normalises ONLY for matching (case-fold + separator-fold), so a
// key recorded as PAGE_SLUG on the oracle aligns with pageSlug on v3 — but the
// original literal VALUES are preserved untouched for the value compare. This
// normalisation helper is reused by auth_posture_diff (#4422) for slug compare.
package literalparity

import (
	"sort"
	"strings"
)

// Member is one {key,value,line} entry of a value-set, mirroring the
// constMember JSON shape emitted into the members_json property. Line is
// optional (0 when the extractor did not resolve it).
type Member struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line,omitempty"`
}

// ValueMismatch records one aligned key whose literal value differs between
// the oracle and v3 sides. Key is the ORIGINAL oracle key (or v3 key when the
// oracle had no original literal — they align by normalised form).
type ValueMismatch struct {
	Key    string `json:"key"`
	Oracle string `json:"oracle"`
	V3     string `json:"v3"`
}

// IntraInconsistency records a separator/format-convention split detected
// WITHIN a single set. Convention is the dominant convention; Outliers are the
// member keys whose value literal does not follow it.
type IntraInconsistency struct {
	Convention string   `json:"convention"`
	Outliers   []string `json:"outliers"`
	Detail     string   `json:"detail"`
}

// Result is the full diff verdict between an oracle value-set and its v3
// mirror.
type Result struct {
	Set                    string               `json:"set"`
	OnlyInOracle           []string             `json:"only_in_oracle"`
	OnlyInV3               []string             `json:"only_in_v3"`
	ValueMismatches        []ValueMismatch      `json:"value_mismatches"`
	IntraV3Inconsistencies []IntraInconsistency `json:"intra_v3_inconsistencies"`
	Verdict                string               `json:"verdict"` // "equivalent" | "drift"
}

const (
	// VerdictEquivalent means the two value-sets are key- and value-equivalent
	// AND the v3 set carries no intra-set convention split.
	VerdictEquivalent = "equivalent"
	// VerdictDrift means at least one membership, value, or intra-v3 difference
	// was detected.
	VerdictDrift = "drift"
)

// NormalizeKey folds a key to its alignment form: lowercase, with every run of
// non-alphanumeric separator characters ('_', '-', '.', ' ', '/') collapsed to
// a single '_'. Used for ALIGNMENT only — never for the value compare. So
// "PAGE_SLUG", "page-slug", "pageSlug"→ caller-supplied camelCase is NOT split
// (we do not word-split camelCase, to avoid false alignment), but separator and
// case differences fold. Exported for reuse by auth_posture_diff (#4422).
func NormalizeKey(k string) string {
	k = strings.TrimSpace(k)
	var b strings.Builder
	prevSep := false
	for _, r := range k {
		switch r {
		case '_', '-', '.', ' ', '/':
			if !prevSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			prevSep = true
		default:
			b.WriteRune(toLowerRune(r))
			prevSep = false
		}
	}
	out := b.String()
	return strings.TrimRight(out, "_")
}

func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// separatorOf classifies the separator convention of a literal value:
// "snake" (contains '_'), "kebab" (contains '-'), or "" (neither / single
// token / mixed-other). Used to detect the intra-v3 convention split. When a
// value contains BOTH '_' and '-' it is classified "mixed".
func separatorOf(v string) string {
	hasUnder := strings.Contains(v, "_")
	hasDash := strings.Contains(v, "-")
	switch {
	case hasUnder && hasDash:
		return "mixed"
	case hasUnder:
		return "snake"
	case hasDash:
		return "kebab"
	default:
		return ""
	}
}

// Diff computes the parity result between an oracle value-set and its v3
// mirror. setName is echoed into Result.Set. Inputs need not be sorted; the
// result lists are deterministically sorted.
func Diff(setName string, oracle, v3 []Member) Result {
	res := Result{
		Set:                    setName,
		OnlyInOracle:           []string{},
		OnlyInV3:               []string{},
		ValueMismatches:        []ValueMismatch{},
		IntraV3Inconsistencies: []IntraInconsistency{},
	}

	// Index each side by normalised key. On a collision (two original keys
	// folding to the same form) the first wins for value compare, but every
	// original key is still tracked for membership so we never silently drop.
	type idx struct {
		origKey string
		value   string
	}
	oracleByNorm := map[string]idx{}
	for _, m := range oracle {
		nk := NormalizeKey(m.Key)
		if _, ok := oracleByNorm[nk]; !ok {
			oracleByNorm[nk] = idx{origKey: m.Key, value: m.Value}
		}
	}
	v3ByNorm := map[string]idx{}
	for _, m := range v3 {
		nk := NormalizeKey(m.Key)
		if _, ok := v3ByNorm[nk]; !ok {
			v3ByNorm[nk] = idx{origKey: m.Key, value: m.Value}
		}
	}

	// Membership + value-mismatch.
	for nk, o := range oracleByNorm {
		v, ok := v3ByNorm[nk]
		if !ok {
			res.OnlyInOracle = append(res.OnlyInOracle, o.origKey)
			continue
		}
		// Compare ORIGINAL literal values (not normalised).
		if o.value != v.value {
			res.ValueMismatches = append(res.ValueMismatches, ValueMismatch{
				Key:    o.origKey,
				Oracle: o.value,
				V3:     v.value,
			})
		}
	}
	for nk, v := range v3ByNorm {
		if _, ok := oracleByNorm[nk]; !ok {
			res.OnlyInV3 = append(res.OnlyInV3, v.origKey)
		}
	}

	// Intra-v3 convention split: classify each v3 value literal's separator
	// convention; if more than one non-empty convention appears, report the
	// minority members as outliers against the dominant convention.
	res.IntraV3Inconsistencies = detectIntraInconsistency(v3)

	sort.Strings(res.OnlyInOracle)
	sort.Strings(res.OnlyInV3)
	sort.Slice(res.ValueMismatches, func(i, j int) bool {
		return res.ValueMismatches[i].Key < res.ValueMismatches[j].Key
	})

	if len(res.OnlyInOracle) == 0 &&
		len(res.OnlyInV3) == 0 &&
		len(res.ValueMismatches) == 0 &&
		len(res.IntraV3Inconsistencies) == 0 {
		res.Verdict = VerdictEquivalent
	} else {
		res.Verdict = VerdictDrift
	}
	return res
}

// detectIntraInconsistency finds a mixed separator/format convention within a
// single value-set. It groups members by the separator convention of their
// VALUE literal (falling back to the KEY when the value is empty, so value-less
// enum members still participate). When two or more distinct non-empty
// conventions coexist, the dominant (most-common) one is the "convention" and
// the rest are outliers. A "mixed" member (one value carrying BOTH _ and -) is
// always an outlier. Returns at most one inconsistency record per set.
func detectIntraInconsistency(members []Member) []IntraInconsistency {
	counts := map[string]int{}
	// firstSeen records declaration-order rank of the first member carrying each
	// convention, so a true count-tie breaks toward the FIRST-declared
	// convention (the established baseline) rather than alphabetically.
	firstSeen := map[string]int{}
	type classified struct {
		key  string
		conv string
	}
	cls := make([]classified, 0, len(members))
	for i, m := range members {
		probe := m.Value
		if strings.TrimSpace(probe) == "" {
			probe = m.Key
		}
		conv := separatorOf(probe)
		if conv == "" {
			// Single-token value (no separator) is convention-neutral: it is
			// compatible with any convention, so it never triggers a split on
			// its own and is not counted toward a convention.
			cls = append(cls, classified{key: m.Key, conv: ""})
			continue
		}
		counts[conv]++
		if _, ok := firstSeen[conv]; !ok {
			firstSeen[conv] = i
		}
		cls = append(cls, classified{key: m.Key, conv: conv})
	}

	// Distinct non-empty conventions present.
	distinct := make([]string, 0, len(counts))
	for c := range counts {
		distinct = append(distinct, c)
	}
	// "mixed" alone (a single member carrying both separators) is itself an
	// inconsistency even without a competing convention.
	if len(distinct) < 2 {
		if _, hasMixed := counts["mixed"]; !hasMixed {
			return []IntraInconsistency{}
		}
	}

	// Dominant convention: highest count; on a count-tie prefer the
	// FIRST-declared convention (the established baseline). "mixed" is never the
	// dominant convention.
	sort.Strings(distinct)
	dominant := ""
	best := -1
	bestFirst := 1 << 30
	for _, c := range distinct {
		if c == "mixed" {
			continue
		}
		if counts[c] > best || (counts[c] == best && firstSeen[c] < bestFirst) {
			best = counts[c]
			bestFirst = firstSeen[c]
			dominant = c
		}
	}
	if dominant == "" {
		// Only "mixed" present.
		dominant = "mixed"
	}

	outliers := []string{}
	for _, c := range cls {
		if c.conv == "" || c.conv == dominant {
			continue
		}
		outliers = append(outliers, c.key)
	}
	if len(outliers) == 0 {
		return []IntraInconsistency{}
	}
	sort.Strings(outliers)

	others := make([]string, 0, len(distinct))
	for _, c := range distinct {
		if c != dominant {
			others = append(others, c)
		}
	}
	return []IntraInconsistency{{
		Convention: dominant,
		Outliers:   outliers,
		Detail: "v3 value-set mixes separator conventions: dominant=" + dominant +
			", outliers=" + strings.Join(others, ",") + " convention",
	}}
}
