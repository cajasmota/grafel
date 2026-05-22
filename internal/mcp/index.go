package mcp

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
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

// BuildLabelIndex constructs a fresh LabelIndex from a graph document.
func BuildLabelIndex(doc *graph.Document) *LabelIndex {
	idx := &LabelIndex{
		ByID:    make(map[string]*graph.Entity, len(doc.Entities)),
		ByLabel: make(map[string][]*graph.Entity, len(doc.Entities)),
		ByQName: make(map[string]*graph.Entity, len(doc.Entities)),
	}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.ByID[e.ID] = e
		lbl := strings.ToLower(e.Name)
		idx.ByLabel[lbl] = append(idx.ByLabel[lbl], e)
		if e.QualifiedName != "" {
			idx.ByQName[strings.ToLower(e.QualifiedName)] = e
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
