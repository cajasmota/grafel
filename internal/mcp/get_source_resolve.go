package mcp

// get_source_resolve.go — entity resolution for grafel_get_source (#4272).
//
// get_source is the busiest MCP tool; its ~8% live error rate was dominated by
// resolution failures on WELL-FORMED args, not by genuinely-missing entities.
// The legacy inline resolver in handleGetNodeSource:
//
//   - handled a "<repo>::<localid>" prefix only via r.LabelIndex.ByID[localid];
//     a prefixed arg that resolved by qualified_name or label (not by raw id)
//     fell through, and the cross-repo fallback then ran LookupAll on the
//     STILL-PREFIXED string — so it matched nothing and returned a bare
//     "node not found" (the same prefix gap fixed for effective_contract in
//     #4243); and
//   - on a true miss returned only "node not found: <arg>" with no hint, so the
//     caller could not self-correct and often re-errored.
//
// resolveSourceEntity hardens this: it tries id → qualified_name → label →
// qualified-name suffix across the group for BOTH the raw arg and its
// prefix-stripped local form, and on a genuine miss returns a clearer error
// (attempted forms + nearest did-you-mean) WITHOUT masking real not-founds.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// sourceResolution is the outcome of resolveSourceEntity. Exactly one of
// {entity, ambiguous, notFound} is populated.
type sourceResolution struct {
	entity *graph.Entity
	repo   *LoadedRepo

	// ambiguous holds >1 distinct matches for a label that resolved in multiple
	// places; the handler renders the clarifier list.
	ambiguous []sourceCandidate

	// notFound carries a clearer error message (attempted forms + did-you-mean)
	// for a genuine miss.
	notFound string
}

type sourceCandidate struct {
	ent  *graph.Entity
	repo *LoadedRepo
}

// resolveSourceEntity resolves nodeID to a single entity across lg, accepting an
// id, a qualified_name, or a label — each optionally carrying a "<repo>::"
// prefix. Resolution order, evaluated for both the raw arg and its
// prefix-stripped local form:
//
//  1. exact entity id (prefixed branch first: if the "<repo>::" prefix names a
//     loaded repo, prefer that repo's id/qname/label match);
//  2. exact qualified_name / label across all repos (LookupAll);
//  3. qualified_name SUFFIX match (a dotted arg matching the tail of an
//     entity's qualified_name) — lets a caller pass a partially-qualified name.
//
// A label that resolves to >1 distinct entity yields an ambiguous result. A true
// miss yields notFound with attempted forms and a did-you-mean nearest match.
func resolveSourceEntity(lg *LoadedGroup, nodeID string) sourceResolution {
	rp, local := splitPrefixed(nodeID)

	// Candidate lookup keys, most-specific first: the prefix-stripped local form
	// (the form the LabelIndex is keyed by, per ADR-0009) then the raw arg.
	// De-duplicated; order preserved.
	keys := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, k := range []string{local, nodeID} {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}

	// Phase 1 — if the prefix names a loaded repo, prefer a match within it for
	// any candidate key (id, then qname/label via LookupAll). This keeps a
	// fully-prefixed arg pinned to its own repo even when another repo carries a
	// same-named entity.
	if rp != "" {
		if r, ok := lg.Repos[rp]; ok && r.Doc != nil && r.LabelIndex != nil {
			if e := r.LabelIndex.ByID(local); e != nil {
				return sourceResolution{entity: e, repo: r}
			}
			for _, key := range keys {
				if hits := r.LabelIndex.LookupAll(key); len(hits) == 1 {
					return sourceResolution{entity: hits[0], repo: r}
				} else if len(hits) > 1 {
					return ambiguousFrom(hits, r)
				}
			}
		}
	}

	// Phase 2 — exact id/qname/label across the whole group, for every key.
	var cands []sourceCandidate
	for _, r := range lg.Repos {
		if r.Doc == nil || r.LabelIndex == nil {
			continue
		}
		for _, key := range keys {
			for _, hit := range r.LabelIndex.LookupAll(key) {
				cands = appendUniqueCandidate(cands, sourceCandidate{ent: hit, repo: r})
			}
		}
	}
	if len(cands) == 1 {
		return sourceResolution{entity: cands[0].ent, repo: cands[0].repo}
	}
	if len(cands) > 1 {
		return sourceResolution{ambiguous: cands}
	}

	// Phase 3 — qualified_name suffix match. A dotted arg (with or without the
	// "<repo>::" prefix) that is a trailing segment of exactly one entity's
	// qualified_name resolves to it; >1 is ambiguous.
	if sc := suffixMatch(lg, keys); len(sc) == 1 {
		return sourceResolution{entity: sc[0].ent, repo: sc[0].repo}
	} else if len(sc) > 1 {
		return sourceResolution{ambiguous: sc}
	}

	// Genuine miss — build a clearer error so the caller can self-correct.
	return sourceResolution{notFound: notFoundMessage(lg, nodeID, keys)}
}

func ambiguousFrom(hits []*graph.Entity, r *LoadedRepo) sourceResolution {
	cands := make([]sourceCandidate, 0, len(hits))
	for _, h := range hits {
		cands = append(cands, sourceCandidate{ent: h, repo: r})
	}
	if len(cands) == 1 {
		return sourceResolution{entity: cands[0].ent, repo: cands[0].repo}
	}
	return sourceResolution{ambiguous: cands}
}

// appendUniqueCandidate appends c unless an equivalent candidate is already
// present. Equivalence is keyed by (repo, entity ID), NOT by *graph.Entity
// pointer identity: ADR-0027 Cutover PR2 made LabelIndex.LookupAll materialize
// a fresh copy per hit, so the same entity resolved via two candidate keys now
// yields two DISTINCT pointers. Dedup by ID keeps a single logical match from
// being mis-reported as ambiguous (PR2 retained-pointer audit fix).
func appendUniqueCandidate(cands []sourceCandidate, c sourceCandidate) []sourceCandidate {
	for _, x := range cands {
		if x.repo == c.repo && x.ent.ID == c.ent.ID {
			return cands
		}
	}
	return append(cands, c)
}

// suffixMatch finds entities whose qualified_name ends with ".<key>" (or equals
// key) for any candidate key. Only dotted keys are considered to avoid matching
// every short label.
func suffixMatch(lg *LoadedGroup, keys []string) []sourceCandidate {
	var out []sourceCandidate
	for _, key := range keys {
		if !strings.Contains(key, ".") {
			continue
		}
		needle := strings.ToLower(key)
		for _, r := range lg.Repos {
			if r.Doc == nil {
				continue
			}
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				qn := strings.ToLower(e.QualifiedName)
				if qn == "" {
					continue
				}
				if qn == needle || strings.HasSuffix(qn, "."+needle) {
					out = appendUniqueCandidate(out, sourceCandidate{ent: e, repo: r})
				}
			}
		}
	}
	return out
}

// notFoundMessage builds the clearer not-found error: it names the attempted
// resolution forms and, when a near match exists, offers a did-you-mean.
func notFoundMessage(lg *LoadedGroup, nodeID string, keys []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "node not found: %s", nodeID)
	fmt.Fprintf(&b, " (tried id, qualified_name, label, and qualified-name suffix for: %s)",
		strings.Join(keys, ", "))
	if sug := didYouMean(lg, keys); sug != "" {
		fmt.Fprintf(&b, "; did you mean %q?", sug)
	}
	return b.String()
}

// didYouMean returns the single closest entity label/qualified_name to any
// candidate key by normalised Levenshtein distance, or "" when nothing is close
// enough (threshold: edit distance ≤ 1/3 of the longer string).
func didYouMean(lg *LoadedGroup, keys []string) string {
	type scored struct {
		name string
		dist int
		max  int
	}
	var best *scored
	consider := func(target, candidate string) {
		if candidate == "" {
			return
		}
		d := levenshteinDist(strings.ToLower(target), strings.ToLower(candidate))
		m := len(target)
		if len(candidate) > m {
			m = len(candidate)
		}
		if m == 0 || d*3 > m { // require >2/3 similarity
			return
		}
		if best == nil || d < best.dist {
			best = &scored{name: candidate, dist: d, max: m}
		}
	}
	for _, key := range keys {
		leaf := key
		if i := strings.LastIndex(key, "."); i >= 0 {
			leaf = key[i+1:]
		}
		for _, r := range lg.Repos {
			if r.Doc == nil {
				continue
			}
			for i := range r.Doc.Entities {
				e := &r.Doc.Entities[i]
				consider(key, e.QualifiedName)
				consider(leaf, e.Name)
			}
		}
	}
	if best == nil {
		return ""
	}
	return best.name
}

// levenshteinDist is the standard edit distance between two strings. Local to
// the mcp package to avoid a cross-package dependency for one small helper.
func levenshteinDist(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// ambiguousMatches renders an ambiguous candidate list into the stable clarifier
// payload get_source returns so the caller can pick one id.
func ambiguousMatches(cands []sourceCandidate, query string) map[string]any {
	out := make([]map[string]any, 0, len(cands))
	for _, c := range cands {
		out = append(out, map[string]any{
			"id":             prefixedID(c.repo.Repo, c.ent.ID),
			"qualified_name": c.ent.QualifiedName,
			"label":          c.ent.Name,
			"repo":           c.repo.Repo,
			"source_file":    c.ent.SourceFile,
			"start_line":     c.ent.StartLine,
			"kind":           stripScopePrefix(c.ent.Kind),
		})
	}
	// Deterministic ordering for stable output / tests.
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["id"]) < fmt.Sprint(out[j]["id"])
	})
	return map[string]any{
		"ambiguous": true,
		"query":     query,
		"count":     len(out),
		"matches":   out,
		"note":      "multiple entities match this label; call again with one of the ids above.",
	}
}
