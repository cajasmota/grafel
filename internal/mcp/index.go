package mcp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// LabelIndex is an inverted index for O(1) lookups by:
//
//   - exact entity ID
//   - exact label (lowercased)
//   - exact qualified-name (lowercased)
//
// All maps point into the underlying Document.Entities slice — never copy.
type LabelIndex struct {
	ByID    map[string]*graph.Entity
	ByLabel map[string][]*graph.Entity
	ByQName map[string]*graph.Entity
}

// keyInterner canonicalizes repeated map-key strings built during a single
// BuildLabelIndex call (Tier-2b index mop-up, mirrors the loader-side
// stringInterner pattern in internal/graph/load.go, #5847/Tier-1b) so that N
// entities sharing an equal lowercased Name/QualifiedName (extremely common —
// "Get", "String", "Equals", "New", accessor/overload names repeat across
// hundreds of classes on the real corpus) share ONE backing array for that
// key string instead of each ByLabel/ByQName insertion paying for its own
// independently-allocated strings.ToLower() copy.
//
// strings.ToLower already returns the ORIGINAL string (no allocation, shared
// with e.Name/e.QualifiedName) when the input has no uppercase runes, so the
// interner only ever pays for a map probe on that fast path; it earns its
// keep exactly on the case-folding path, where ToLower must allocate a fresh
// copy that would otherwise duplicate across every entity with the same
// label. One interner instance is shared across BOTH ByLabel and ByQName
// keys (mirroring Tier-1b's single from_id/to_id/id interner) so a label and
// a qualified name that happen to lowercase to the same text also share
// storage.
//
// Built and discarded within a single BuildLabelIndex call; not retained on
// LabelIndex, so it adds no resident cost of its own beyond its own build.
type keyInterner struct {
	m map[string]string
}

// intern returns the canonical copy of s, sharing backing storage with any
// prior call that saw an equal string.
func (ki *keyInterner) intern(s string) string {
	if v, ok := ki.m[s]; ok {
		return v
	}
	if ki.m == nil {
		ki.m = make(map[string]string)
	}
	ki.m[s] = s
	return s
}

// BuildLabelIndex constructs a fresh LabelIndex from a graph document.
func BuildLabelIndex(doc *graph.Document) *LabelIndex {
	idx := &LabelIndex{
		ByID:    make(map[string]*graph.Entity, len(doc.Entities)),
		ByLabel: make(map[string][]*graph.Entity, len(doc.Entities)),
		ByQName: make(map[string]*graph.Entity, len(doc.Entities)),
	}
	var ki keyInterner
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.ByID[e.ID] = e
		lbl := ki.intern(strings.ToLower(e.Name))
		idx.ByLabel[lbl] = append(idx.ByLabel[lbl], e)
		if e.QualifiedName != "" {
			idx.ByQName[ki.intern(strings.ToLower(e.QualifiedName))] = e
		}
	}
	return idx
}

// Lookup finds an entity by ID, qualified name (case-insensitive), or label
// (case-insensitive). Label matches return the first hit if there are multiple.
// Returns nil if nothing matched.
func (l *LabelIndex) Lookup(s string) *graph.Entity {
	if e, ok := l.ByID[s]; ok {
		return e
	}
	low := strings.ToLower(s)
	if e, ok := l.ByQName[low]; ok {
		return e
	}
	if es, ok := l.ByLabel[low]; ok && len(es) > 0 {
		return es[0]
	}
	return nil
}

// LookupAll returns every entity matching s by ID, qualified name, or label.
// Used by get_source/inspect to surface a clarifier list when a label is
// ambiguous within a repo (#1650).
func (l *LabelIndex) LookupAll(s string) []*graph.Entity {
	if e, ok := l.ByID[s]; ok {
		return []*graph.Entity{e}
	}
	low := strings.ToLower(s)
	if e, ok := l.ByQName[low]; ok {
		return []*graph.Entity{e}
	}
	if es, ok := l.ByLabel[low]; ok && len(es) > 0 {
		return es
	}
	return nil
}
