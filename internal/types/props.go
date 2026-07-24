package types

import (
	"encoding/json"
	"sort"
)

// ---------------------------------------------------------------------------
// Compact property storage for the extraction record types.
//
// EntityRecord/RelationshipRecord properties are overwhelmingly small — one
// or two entries ("language", "line") — yet a map[string]string pays ~300 B
// of Go hmap/bucket overhead per instance regardless. At corpus scale that
// dominates resident memory: internal/extractor.TagRelationshipsLanguage
// alone builds ~1.34M single-entry maps (~147 MB) on a full index run.
//
// Props is the sorted key/value slice that replaces it. It is a direct port
// of the propKV representation introduced for graph.Entity in Phase B
// (#5850) — see internal/graph/graph.go — deliberately mirroring its shape,
// method set and semantics so the two do not diverge. It is replicated here
// rather than reused because graph's propKV is unexported and internal/graph
// sits above internal/types in the dependency order; importing graph from
// types would invert that layering.
//
// Semantics are identical to the map it replaces: a miss reads as "",
// assignment is last-write-wins, and the zero value is safe to read, range
// and delete from. Iteration is key-sorted and therefore deterministic,
// which also makes the JSON wire shape byte-identical to encoding/json's
// map output (which sorts keys too).
// ---------------------------------------------------------------------------

// PropKV is one key/value pair in a Props slice.
type PropKV struct {
	K string
	V string
}

// Props is a property set stored as a slice sorted by key. The zero value
// is an empty, usable set.
type Props []PropKV

// find returns the insertion/match index of key via binary search, and
// whether it was found. O(log n).
func (p Props) find(key string) (int, bool) {
	i := sort.Search(len(p), func(i int) bool { return p[i].K >= key })
	if i < len(p) && p[i].K == key {
		return i, true
	}
	return i, false
}

// PropsFromMap builds a sorted Props from a map[string]string, or nil if m
// is empty. Used at call boundaries that still hand over a plain map.
func PropsFromMap(m map[string]string) Props {
	if len(m) == 0 {
		return nil
	}
	out := make(Props, 0, len(m))
	for k, v := range m {
		out = append(out, PropKV{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// Get returns the value for key, or "" if key is absent.
func (p Props) Get(key string) string {
	if i, ok := p.find(key); ok {
		return p[i].V
	}
	return ""
}

// Lookup returns the value for key and whether it was present.
func (p Props) Lookup(key string) (string, bool) {
	if i, ok := p.find(key); ok {
		return p[i].V, true
	}
	return "", false
}

// Set sets key to val (last write wins), lazily allocating the backing
// slice. The receiver is a pointer because insertion may reallocate.
func (p *Props) Set(key, val string) {
	i, ok := p.find(key)
	if ok {
		(*p)[i].V = val
		return
	}
	*p = append(*p, PropKV{})
	copy((*p)[i+1:], (*p)[i:])
	(*p)[i] = PropKV{K: key, V: val}
}

// SetDefault sets key to val only if key is absent, reporting whether it
// wrote. Mirrors the `if _, ok := m[k]; !ok { m[k] = v }` idiom that the
// property producers use, without a second lookup.
func (p *Props) SetDefault(key, val string) bool {
	i, ok := p.find(key)
	if ok {
		return false
	}
	*p = append(*p, PropKV{})
	copy((*p)[i+1:], (*p)[i:])
	(*p)[i] = PropKV{K: key, V: val}
	return true
}

// Delete removes key, if present. No-op if absent or unset.
func (p *Props) Delete(key string) {
	i, ok := p.find(key)
	if !ok {
		return
	}
	*p = append((*p)[:i], (*p)[i+1:]...)
}

// Range calls f for every key/value pair in key-sorted order. Iteration
// stops early if f returns false. Safe to call on a nil Props (no-op).
func (p Props) Range(f func(k, v string) bool) {
	for _, kv := range p {
		if !f(kv.K, kv.V) {
			return
		}
	}
}

// Len returns the number of properties.
func (p Props) Len() int { return len(p) }

// Snapshot returns an independent copy of the properties as a map, or nil
// if there are none. Callers must not assume the result aliases internal
// storage.
func (p Props) Snapshot() map[string]string {
	if len(p) == 0 {
		return nil
	}
	out := make(map[string]string, len(p))
	for _, kv := range p {
		out[kv.K] = kv.V
	}
	return out
}

// Clone returns an independent copy of p, or nil if p is empty.
func (p Props) Clone() Props {
	if len(p) == 0 {
		return nil
	}
	out := make(Props, len(p))
	copy(out, p)
	return out
}

// MarshalJSON emits the same object encoding a map[string]string would.
// encoding/json sorts map keys, and Props is already key-sorted, so the
// bytes are identical — the on-disk graph and golden fixtures are unaffected.
func (p Props) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	buf := []byte{'{'}
	for i, kv := range p {
		if i > 0 {
			buf = append(buf, ',')
		}
		k, err := json.Marshal(kv.K)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(kv.V)
		if err != nil {
			return nil, err
		}
		buf = append(buf, k...)
		buf = append(buf, ':')
		buf = append(buf, v...)
	}
	return append(buf, '}'), nil
}

// UnmarshalJSON decodes a JSON object into sorted Props. A JSON null
// decodes to nil, matching map[string]string.
func (p *Props) UnmarshalJSON(b []byte) error {
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*p = PropsFromMap(m)
	return nil
}
