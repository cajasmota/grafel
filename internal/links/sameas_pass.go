package links

import (
	"sort"
	"strings"
)

// sameas_pass.go implements P8 — the cross-language SAME_AS linker for
// shared-library domain models.
//
// Problem (deferred from #1501): a domain model defined once per language
// in the shared libs — e.g. `Order` in libs/py-shared/py_shared/models.py
// and `Order` in libs/js-shared/src/types.ts — represents ONE concept but
// has no edge connecting the two. Cross-language entity resolution (and
// any "where is this model used across the platform" query) therefore
// can't see that they are the same thing.
//
// This pass emits an undirected, symmetric `SAME_AS` edge (method=same_as,
// relation=same_as) between such models. It is intentionally CONSERVATIVE:
// a same-named `Config` struct living in two unrelated services must NOT
// be merged. Three gates must all pass:
//
//	(a) Domain-model kind — the entity is a class/struct/interface-like
//	    type (kind in Component/Model/Schema; see isDomainModelKind).
//	(b) Shared-lib location — the owning repo is a shared contract / model
//	    library (py-shared, js-shared, go-shared, contracts, *-common, …;
//	    see isSharedLibRepo).
//	(c) Structural overlap — the two models share at least
//	    sameAsMinFieldOverlap of their normalized field names (Jaccard),
//	    so two coincidentally same-named shells don't link.
//
// Both endpoints must be in DIFFERENT repos (cross-language is the whole
// point) and share the same canonical (normalized) name.
//
// Idempotency: method-segregated on MethodSameAs. Re-running P8 rewrites
// only same_as entries; output from P1–P7 is preserved.

// MethodSameAs identifies this pass's emissions in links.json.
const MethodSameAs = "same_as"

const (
	// sameAsMinFieldOverlap is the minimum Jaccard overlap of normalized
	// field-name sets required to emit a SAME_AS link. Below this (but
	// above zero) the pair is recorded as a candidate, not a link.
	sameAsMinFieldOverlap = 0.5

	// sameAsCandidateFloor is the field-overlap floor below which a pair
	// is dropped entirely (not even a candidate). A name-only match with
	// no structural support is too weak to surface.
	sameAsCandidateFloor = 0.25
)

// sharedLibRepoMarkers are substrings (lowercased) that identify a repo as
// a shared contract / domain-model library. Matched against the repo slug.
var sharedLibRepoMarkers = []string{
	"shared",
	"contracts",
	"contract",
	"common",
	"commons",
	"proto",
	"protos",
	"schemas",
	"domain-models",
	"models-shared",
}

// isSharedLibRepo reports whether a repo slug looks like a shared
// contract / domain-model library. Gate (b).
func isSharedLibRepo(repo string) bool {
	r := strings.ToLower(repo)
	for _, m := range sharedLibRepoMarkers {
		if r == m || strings.Contains(r, m) {
			return true
		}
	}
	return false
}

// domainModelKinds are the entity kinds that represent a structured
// domain type eligible for SAME_AS linking. Compared case-insensitively
// against both the bare kind ("Model") and the SCOPE.* form
// ("SCOPE.Component", "SCOPE.Schema"). Gate (a).
var domainModelKinds = map[string]bool{
	"component": true,
	"model":     true,
	"schema":    true,
}

// modelSubtypes are the entity subtypes that confirm a structured type
// (vs. a field, enum-member, or other leaf). When an entity carries one
// of these subtypes it is treated as a model regardless of kind; when it
// carries a leaf subtype (field/member/...) it is rejected.
var modelSubtypes = map[string]bool{
	"class":     true,
	"struct":    true,
	"interface": true,
	"type":      true,
	"record":    true,
	"protocol":  true,
	"trait":     true,
	"dataclass": true,
	"":          true, // bare Model entities have no subtype
}

// leafSubtypes are subtypes that mark a leaf member (a field of a model,
// an enum value, …) and must never be linked as a model in their own
// right — this is what stops `Order.id` ↔ `Order.id` false links.
var leafSubtypes = map[string]bool{
	"field":       true,
	"member":      true,
	"value":       true,
	"enum_member": true,
	"property":    true,
	"attribute":   true,
	"constant":    true,
}

// isDomainModelKind applies gate (a): the entity must be a structured
// model type, not a field / enum member / function. It accepts the bare
// kind, the SCOPE.* kind tail, and the subtype.
func isDomainModelKind(kind, subtype string) bool {
	sub := strings.ToLower(strings.TrimSpace(subtype))
	if leafSubtypes[sub] {
		return false
	}
	k := strings.ToLower(strings.TrimSpace(kind))
	// Strip a leading "scope." namespace ("SCOPE.Component" → "component").
	if i := strings.LastIndex(k, "."); i >= 0 {
		k = k[i+1:]
	}
	if !domainModelKinds[k] {
		return false
	}
	// A recognised model kind plus a non-leaf subtype passes. Reject odd
	// subtypes that aren't in modelSubtypes to stay conservative.
	return modelSubtypes[sub]
}

// canonicalModelName normalizes a model's type name for cross-language
// matching: case-folded, whitespace-trimmed, with line-number artefacts
// dropped (see #511). Unlike normalizeLabel (P2) it deliberately does NOT
// apply the generic-noise stoplist — legitimate shared domain models are
// frequently named `User`, `Config`, `Item`, `Order`, etc., and the P8
// pass relies on the shared-lib-location and structural-field-overlap
// gates (not a name blocklist) to avoid false merges. Suffix-stripping
// (`OrderDTO` ↔ `Order`) is intentionally omitted too: it would let
// structurally-different shells collide on a coincidental stem.
func canonicalModelName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	if lineNumberSuffix.MatchString(s) {
		return ""
	}
	return strings.ToLower(s)
}

// normalizeFieldName lowercases and strips non-alphanumerics so that
// snake_case and camelCase variants of the same field collapse together
// (`user_id` and `userId` → `userid`). This is what lets a Python model
// and its TypeScript twin be recognised as structurally identical.
func normalizeFieldName(name string) string {
	// Drop any "Owner." prefix (field entities are named "Order.user_id").
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

// modelEntity is a chosen representative model for a (repo, canonicalName)
// plus its derived normalized field-name set.
type modelEntity struct {
	repo   string
	node   entityNode
	fields map[string]bool
}

// fieldsForModel derives the normalized field-name set for a model from
// two possible representations the extractors emit:
//
//   - a `fields` property holding a comma-separated list (TypeScript
//     interfaces, some schema extractors); or
//   - child field entities reachable via CONTAINS edges whose name is
//     "<Model>.<field>" and whose subtype is a leaf (Python classes, Go
//     structs).
//
// The union of both is returned (a model may use either or both).
func fieldsForModel(g repoGraph, model entityNode, childrenByParent map[string][]entityNode) map[string]bool {
	out := map[string]bool{}

	// Representation 1: comma-separated `fields` property.
	if model.Properties != nil {
		if raw, ok := model.Properties.Lookup("fields"); ok && raw != "" {
			for _, f := range strings.Split(raw, ",") {
				if n := normalizeFieldName(f); n != "" {
					out[n] = true
				}
			}
		}
	}

	// Representation 2: child field entities (CONTAINS → leaf).
	for _, child := range childrenByParent[model.ID] {
		sub := strings.ToLower(strings.TrimSpace(child.Subtype))
		if !leafSubtypes[sub] {
			continue
		}
		// Only count children that belong to this model: their name is
		// "<ModelName>.<field>" (defensive — the CONTAINS edge already
		// scopes this, but module-level containers also use CONTAINS).
		if !strings.HasPrefix(child.Name, model.Name+".") {
			continue
		}
		if n := normalizeFieldName(child.Name); n != "" {
			out[n] = true
		}
	}
	return out
}

// jaccard returns |a ∩ b| / |a ∪ b| for two string sets. Empty/empty → 0.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// runSameAsPass implements P8.
func runSameAsPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "same_as"}

	if len(graphs) < 2 {
		// Keep idempotency clean even with nothing to do.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodSameAs), nil, rejects)
		if err != nil {
			return res, err
		}
		_, _, err = replaceByMethod(paths.Candidates, newMethodSet(MethodSameAs), nil, rejects)
		return res, err
	}

	// Collect candidate model entities per shared-lib repo, indexed by
	// canonical name. Only entities passing gates (a) + (b) are kept.
	// byName: canonicalName → []modelEntity.
	byName := map[string][]modelEntity{}

	for _, g := range graphs {
		if !isSharedLibRepo(g.Repo) {
			continue // gate (b)
		}

		// Build CONTAINS child index for this repo (parent ID → children).
		childrenByParent := map[string][]entityNode{}
		nodeByID := map[string]entityNode{}
		for _, e := range g.Entities {
			nodeByID[e.ID] = e
		}
		for _, edge := range g.Edges {
			if !strings.EqualFold(edge.Kind, "CONTAINS") {
				continue
			}
			if child, ok := nodeByID[edge.ToID]; ok {
				childrenByParent[edge.FromID] = append(childrenByParent[edge.FromID], child)
			}
		}

		// One representative per (canonicalName) in this repo. Several
		// entities may describe the same model (a bare `Model` plus a
		// `SCOPE.Component`); prefer the one that yields the most fields,
		// breaking ties by entity-ID for determinism.
		repByName := map[string]modelEntity{}
		for _, e := range g.Entities {
			if !isDomainModelKind(e.Kind, e.Subtype) { // gate (a)
				continue
			}
			canon := canonicalModelName(e.Name)
			if canon == "" {
				continue
			}
			fields := fieldsForModel(g, e, childrenByParent)
			cand := modelEntity{repo: g.Repo, node: e, fields: fields}
			cur, ok := repByName[canon]
			if !ok ||
				len(cand.fields) > len(cur.fields) ||
				(len(cand.fields) == len(cur.fields) && cand.node.ID < cur.node.ID) {
				repByName[canon] = cand
			}
		}
		for canon, m := range repByName {
			byName[canon] = append(byName[canon], m)
		}
	}

	now := discoveredAt()
	var freshLinks, freshCands []Link

	// Stable iteration over canonical names.
	names := make([]string, 0, len(byName))
	for k := range byName {
		names = append(names, k)
	}
	sort.Strings(names)

	seenPair := map[string]bool{}

	for _, canon := range names {
		models := byName[canon]
		if len(models) < 2 {
			continue // need the same model in ≥2 shared-lib repos
		}
		// Stable order across repos.
		sort.Slice(models, func(i, j int) bool {
			if models[i].repo != models[j].repo {
				return models[i].repo < models[j].repo
			}
			return models[i].node.ID < models[j].node.ID
		})

		for i := 0; i < len(models); i++ {
			for j := i + 1; j < len(models); j++ {
				a, b := models[i], models[j]
				if a.repo == b.repo {
					continue // intra-repo same-name is not cross-language
				}

				// Gate (c): structural field overlap.
				overlap := jaccard(a.fields, b.fields)
				if overlap < sameAsCandidateFloor {
					continue // too weak — drop entirely (false-merge guard)
				}

				sa := entityKey(a.repo, a.node.ID)
				sb := entityKey(b.repo, b.node.ID)
				src, tgt := orderEndpoints(sa, sb)
				pairKey := src + "|" + tgt
				if seenPair[pairKey] {
					continue
				}
				seenPair[pairKey] = true

				ident := canon
				link := Link{
					ID:           MakeID(src, tgt, MethodSameAs),
					Source:       src,
					Target:       tgt,
					Relation:     RelationSameAs,
					Method:       MethodSameAs,
					Confidence:   ScoreSameAs(overlap, sameAsMinFieldOverlap),
					Channel:      nil,
					Identifier:   &ident,
					DiscoveredAt: now,
				}
				// #3628 — SAME_AS pairs two cross-language models by name +
				// Jaccard field-overlap. Even heavily gated, it is a fuzzy
				// identity guess, not a structural binding. heuristic.
				link.WithEdgeConfidence(ConfidenceHeuristic)
				if overlap >= sameAsMinFieldOverlap {
					freshLinks = append(freshLinks, link)
				} else {
					link.Reason = "same_as field overlap below link threshold"
					freshCands = append(freshCands, link)
				}
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodSameAs), freshLinks, rejects)
	if err != nil {
		return res, err
	}
	cAdded, cSkipped, err := replaceByMethod(paths.Candidates, newMethodSet(MethodSameAs), freshCands, rejects)
	if err != nil {
		return res, err
	}

	res.LinksAdded = added
	res.Candidates = cAdded
	res.Skipped = skipped + cSkipped
	return res, nil
}
