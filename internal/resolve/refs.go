// Package resolve rewrites stub-form RelationshipRecord endpoint references
// (e.g. "View:User", "Model:Article", or a bare "Hello") into deterministic
// 16-char graph entity IDs by looking them up in the merged entity set.
//
// This is the substance of PORT-2-FIX (issue #24). PORT-2 produced thousands
// of relationships but every cross-file ToID was left as a stub string, so
// graph traversal dead-ended at the first cross-file reference. The resolver
// closes that gap.
package resolve

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// Index is a kind-aware (kind, name) -> entity_id lookup. The inner map only
// retains a name when the (kind, name) tuple resolves to exactly one entity;
// ambiguous tuples are tracked separately in the embedded ambig set so the
// resolver can leave them as stubs rather than silently picking a wrong match.
type Index struct {
	// byKind[kind][name] = entity_id (only when unique within that kind).
	byKind map[string]map[string]string
	// ambigKind[kind][name] = true when a (kind, name) tuple is ambiguous.
	ambigKind map[string]map[string]bool

	// byName[name] = entity_id (only when unique across ALL kinds). Used
	// for the kind-agnostic fallback when a stub has no "Kind:" prefix or
	// when the kind-specific lookup misses.
	byName map[string]string
	// ambigName[name] = true when a name appears in two or more entities.
	ambigName map[string]bool
}

// Stats reports how many relationships the resolver rewrote and how many it
// left as stubs because of ambiguity / missing matches. Surfaced via the log
// line in cmd/archigraph/index.go for instrumentation.
type Stats struct {
	Rewritten int
	Ambiguous int
	Unmatched int
}

// BuildIndex constructs a (kind, name) -> entity_id lookup from a slice of
// EntityRecords. Records whose ID field is empty are skipped — the caller is
// expected to populate ID with graph.EntityID(...) before calling BuildIndex.
//
// The returned Index handles two kind forms emitted by upstream extractors:
//
//   - Plain kind, e.g. "Function", "Class", "Model".
//   - SCOPE-prefixed kind, e.g. "SCOPE.View", "SCOPE.Service" — emitted by
//     Pass 3 cross-language extractors. The lookup strips the "SCOPE." prefix
//     so a stub like "View:User" matches an entity of kind "SCOPE.View".
func BuildIndex(entities []types.EntityRecord) Index {
	idx := Index{
		byKind:    make(map[string]map[string]string),
		ambigKind: make(map[string]map[string]bool),
		byName:    make(map[string]string),
		ambigName: make(map[string]bool),
	}
	for k := range entities {
		e := &entities[k]
		if e.ID == "" || e.Name == "" {
			continue
		}
		// Index under both the plain kind and the trimmed kind ("SCOPE.View"
		// → "View"), so stubs can match either form.
		kinds := []string{e.Kind}
		if trimmed := strings.TrimPrefix(e.Kind, "SCOPE."); trimmed != e.Kind && trimmed != "" {
			kinds = append(kinds, trimmed)
		}
		for _, kind := range kinds {
			if kind == "" {
				continue
			}
			if idx.ambigKind[kind] != nil && idx.ambigKind[kind][e.Name] {
				continue
			}
			bucket := idx.byKind[kind]
			if bucket == nil {
				bucket = make(map[string]string)
				idx.byKind[kind] = bucket
			}
			if existing, ok := bucket[e.Name]; ok && existing != e.ID {
				delete(bucket, e.Name)
				if idx.ambigKind[kind] == nil {
					idx.ambigKind[kind] = make(map[string]bool)
				}
				idx.ambigKind[kind][e.Name] = true
				continue
			}
			bucket[e.Name] = e.ID
		}

		// Kind-agnostic name index. Two different entities sharing a name
		// (even across kinds) flips the name to ambiguous.
		if idx.ambigName[e.Name] {
			continue
		}
		if existing, ok := idx.byName[e.Name]; ok && existing != e.ID {
			delete(idx.byName, e.Name)
			idx.ambigName[e.Name] = true
			continue
		}
		idx.byName[e.Name] = e.ID
	}
	return idx
}

// Lookup resolves a stub string to an entity ID. The stub is split on the
// first ':' into (kind, name). If only the right-hand side is supplied (no
// ':' present) we fall back to the kind-agnostic name index.
//
// Returns (id, true) only when the lookup is unambiguous. Returns
// ("", false) when the stub has zero matches OR multiple matches — the
// caller leaves the original string in place in either case but tracks the
// outcome in Stats.
func (idx Index) Lookup(stub string) (string, bool) {
	if stub == "" {
		return "", false
	}
	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, true
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			// Ambiguous within this kind; fall through to kind-agnostic
			// only if the kind-agnostic name is itself unique.
		}
	}
	// Kind-agnostic fallback: bare name (no prefix) OR missed kind lookup.
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, true
	}
	return "", false
}

// LookupStatus reports whether a stub is unambiguous, ambiguous, or unmatched.
// Used by References to populate Stats counters without doing two passes.
func (idx Index) LookupStatus(stub string) (id string, status int) {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	if stub == "" {
		return "", statusUnmatched
	}
	kind, name := splitStub(stub)
	if kind != "" {
		if bucket, ok := idx.byKind[kind]; ok {
			if id, ok := bucket[name]; ok {
				return id, statusRewritten
			}
		}
		if idx.ambigKind[kind] != nil && idx.ambigKind[kind][name] {
			return "", statusAmbiguous
		}
	}
	lookupName := name
	if kind == "" {
		lookupName = stub
	}
	if id, ok := idx.byName[lookupName]; ok {
		return id, statusRewritten
	}
	if idx.ambigName[lookupName] {
		return "", statusAmbiguous
	}
	return "", statusUnmatched
}

// splitStub splits a stub string on the first ':' into (kind, name). If no
// ':' is present the full string is returned as the name and kind is empty.
func splitStub(s string) (kind, name string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// References rewrites ToID and FromID values in rels in place. It returns
// per-endpoint stats — one rel with both endpoints rewritten counts twice in
// Stats.Rewritten (once per endpoint). The 16-char hex IDs already present
// (matching the shape of graph.EntityID output) are left untouched.
func References(rels []types.RelationshipRecord, idx Index) Stats {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	var stats Stats
	for k := range rels {
		r := &rels[k]
		// FromID
		if r.FromID != "" && !isHexID(r.FromID) {
			id, st := idx.LookupStatus(r.FromID)
			switch st {
			case statusRewritten:
				r.FromID = id
				stats.Rewritten++
			case statusAmbiguous:
				stats.Ambiguous++
			case statusUnmatched:
				stats.Unmatched++
			}
		}
		// ToID
		if r.ToID != "" && !isHexID(r.ToID) {
			id, st := idx.LookupStatus(r.ToID)
			switch st {
			case statusRewritten:
				r.ToID = id
				stats.Rewritten++
			case statusAmbiguous:
				stats.Ambiguous++
			case statusUnmatched:
				stats.Unmatched++
			}
		}
	}
	return stats
}

// ReferencesEmbedded walks every EntityRecord's embedded Relationships slice
// and applies the same resolver. Pass 1 extractors emit cross-file CALLS
// edges as embedded relationships, so this is where most of the rewriting
// happens on real codebases.
//
// FromID is left alone here — embedded rels conventionally use the parent
// entity as the source, and the caller (buildDocument) substitutes the
// parent ID at edge-emission time when FromID is empty.
func ReferencesEmbedded(records []types.EntityRecord, idx Index) Stats {
	const (
		statusRewritten = 1
		statusAmbiguous = 2
		statusUnmatched = 3
	)
	var stats Stats
	for k := range records {
		rels := records[k].Relationships
		for j := range rels {
			r := &rels[j]
			if r.ToID == "" || isHexID(r.ToID) {
				continue
			}
			id, st := idx.LookupStatus(r.ToID)
			switch st {
			case statusRewritten:
				r.ToID = id
				stats.Rewritten++
			case statusAmbiguous:
				stats.Ambiguous++
			case statusUnmatched:
				stats.Unmatched++
			}
		}
	}
	return stats
}

// isHexID reports whether s is a 16-char lower-hex string — the shape of
// graph.EntityID() output. Anything matching this shape is assumed to be an
// already-resolved entity ID and is left untouched.
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
