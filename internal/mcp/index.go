package mcp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	fb "github.com/cajasmota/grafel/internal/graph/fbgraph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
)

// LabelIndex is an inverted index for O(1) lookups by:
//
//   - exact entity ID
//   - exact label (lowercased)
//   - exact qualified-name (lowercased)
//
// ADR-0027 Cutover PR2: the index holds ENTITY INDICES (int32 positions in the
// graph.fb / Document entity vector) — NOT *graph.Entity pointers ALIASING
// Document.Entities. A lookup MATERIALIZES a fresh heap-copy graph.Entity on
// each hit, so the index no longer PINS live pointers into the Document backing
// array. Two lookups of the same id therefore return DIFFERENT pointers.
//
// POINTER INSTABILITY (audit-critical for PR7): consumers MUST NOT compare
// returned pointers by identity, use them as a map key, or cache one across
// calls expecting stability — dedup by entity ID instead. FuseRRF's
// pointer-identity was fixed in #5864; the PR2 audit swept the remaining
// consumers (get_source_resolve.appendUniqueCandidate + enrichment.applyResolutions).
//
// Value-materialization SOURCE — deliberately the live Document, not the mmap
// Reader (see the PR2 report): applyGroupAlgoOverlay stamps
// CommunityID/PageRank/Centrality/god/articulation onto Doc.Entities IN PLACE
// after load (the values live in the <group>-algo.json overlay, NOT in
// graph.fb). Materializing values from the Reader would silently drop those
// stamps — a behavior regression (TestApplyOverlay_ReStampsReparsedMobileRepo).
// So at() copies the LIVE Doc row, keeping PR2 byte-identical/behavior-neutral.
// The int32 index MAP is still built Reader-first (BuildLabelIndexFromReader),
// proving the index no longer needs Doc.Entities to be *constructed*; flipping
// the value source to the Reader is a PR7 concern gated on re-plumbing the
// overlay off the in-place Doc mutation.
type LabelIndex struct {
	// doc is retained as the JSON-only (nil-reader) value-materialization source
	// and the bounds oracle. When reader != nil, at() materializes the BASE
	// entity from the mmap Reader (graph.MaterializeEntity) instead and merges
	// the 5 group-algo overlay fields from the side-table below — so the index
	// path no longer depends on doc.Entities for VALUES (the PR7 prerequisite).
	doc *graph.Document

	// reader is the resident mmap Reader whose entity rows back this index
	// generation (set by BuildLabelIndexFromReader). Nil on the JSON-only /
	// no-graph.fb fallback, where at() reads doc.Entities directly. Reads happen
	// on the same s.mu-serialized read path that today reads lr.Doc; a reload
	// rebuilds the whole LabelIndex (fresh reader), so at() never dereferences a
	// reader across its own generation.
	reader *fbreader.Reader

	// overlay is the ADR-0027 overlay SIDE-TABLE: entity INDEX (int32 vector
	// position) -> the 5 group-algo values that are NOT authoritative in graph.fb
	// (per-repo Pass-4 was removed, so graph.fb carries permanent sentinels for
	// them). Populated by applyGroupAlgoOverlay from the SAME <group>-algo.json
	// data it stamps onto lr.Doc, keyed by resolving each overlay entity ID to
	// its vector index via byID. at() merges these onto the Reader-materialized
	// base so a lookup is byte-equal to today's overlaid Doc row. Only entities
	// WITH an overlay entry are present; a miss leaves the fb sentinel.
	//
	// Built ONLY when GRAFEL_SERVE_FROM_MMAP is ON — the flag-off default path
	// reads the overlaid Doc and never consults this table, so the ~single-digit
	// MB of resident memory is not paid until the flip is enabled. Rebuilt/
	// invalidated with the LabelIndex itself on every reload and reassigned on
	// every overlay re-stamp. nil on the flag-off path and until an overlay is
	// applied.
	overlay map[int32]entityOverlay

	byID    map[string]int32
	byLabel map[string][]int32
	byQName map[string]int32
}

// entityOverlay holds the 5 group-algo fields that live in the
// <group>-algo.json overlay rather than in graph.fb (CommunityID / PageRank /
// Centrality are pointers so nil distinguishes "no overlay value" from a real
// zero, matching graph.Entity's own representation; the two flags are plain
// bools). Independent heap copies (distinct from the Doc stamp's pointers) so
// the table remains a valid source after the PR7 Doc drop.
type entityOverlay struct {
	CommunityID      *int
	PageRank         *float64
	Centrality       *float64
	IsGodNode        bool
	IsArticulationPt bool
}

// keyInterner canonicalizes repeated map-key strings built during a single
// BuildLabelIndex call (Tier-2b index mop-up, mirrors the loader-side
// stringInterner pattern in internal/graph/load.go, #5847/Tier-1b) so that N
// entities sharing an equal lowercased Name/QualifiedName (extremely common —
// "Get", "String", "Equals", "New", accessor/overload names repeat across
// hundreds of classes on the real corpus) share ONE backing array for that
// key string instead of each byLabel/byQName insertion paying for its own
// independently-allocated strings.ToLower() copy.
//
// strings.ToLower already returns the ORIGINAL string (no allocation, shared
// with e.Name/e.QualifiedName) when the input has no uppercase runes, so the
// interner only ever pays for a map probe on that fast path; it earns its
// keep exactly on the case-folding path, where ToLower must allocate a fresh
// copy that would otherwise duplicate across every entity with the same
// label. One interner instance is shared across BOTH byLabel and byQName
// keys (mirroring Tier-1b's single from_id/to_id/id interner) so a label and
// a qualified name that happen to lowercase to the same text also share
// storage.
//
// Built and discarded within a single build call; not retained on LabelIndex,
// so it adds no resident cost of its own beyond its own build.
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

// newLabelIndex allocates an empty index sized for n entities plus its shared
// key interner. Both build paths funnel through add() so the keying
// (lowercased label with ambiguity list; last-write-wins lowercased qname
// skipped when empty) can never diverge between the Reader- and
// Document-sourced builds.
func newLabelIndex(n int) (*LabelIndex, *keyInterner) {
	if n < 0 {
		n = 0
	}
	return &LabelIndex{
		byID:    make(map[string]int32, n),
		byLabel: make(map[string][]int32, n),
		byQName: make(map[string]int32, n),
	}, &keyInterner{}
}

// add records the i-th entity's (id, name, qname) into the index maps. Keying
// mirrors the pre-PR2 pointer build exactly: byID by raw id, byLabel by
// lowercased name (appended → ambiguity list in vector order), byQName by
// lowercased qname (last-write-wins, skipped when empty).
func (l *LabelIndex) add(ki *keyInterner, i int32, id, name, qname string) {
	l.byID[id] = i
	lbl := ki.intern(strings.ToLower(name))
	l.byLabel[lbl] = append(l.byLabel[lbl], i)
	if qname != "" {
		l.byQName[ki.intern(strings.ToLower(qname))] = i
	}
}

// BuildLabelIndex constructs a fresh Document-sourced LabelIndex. Lookups
// materialize a heap copy out of doc.Entities. This is the nil-Reader fallback
// (JSON-only load / no graph.fb) and the parity baseline for the Reader-sourced
// build.
func BuildLabelIndex(doc *graph.Document) *LabelIndex {
	idx, ki := newLabelIndex(len(doc.Entities))
	idx.doc = doc
	for i := range doc.Entities {
		e := &doc.Entities[i]
		idx.add(ki, int32(i), e.ID, e.Name, e.QualifiedName)
	}
	return idx
}

// BuildLabelIndexFromReader constructs a fresh LabelIndex whose int32 index
// maps are built by iterating the resident mmap Reader (ADR-0027 Cutover PR2) —
// reading Id/Name/QualifiedName off each fb.Entity in vector order, the SAME
// order the Document loader produces. This proves the index can be CONSTRUCTED
// without touching Doc.Entities. The int32 maps are byte-identical to
// BuildLabelIndex(doc) over the same graph.fb (TestLabelIndex*ReaderParity_PR2).
//
// doc is retained as the value-materialization source (at() copies from it) so
// lookups stay behavior-neutral against in-place overlay stamps — see the
// LabelIndex type doc for why values are NOT read back from the Reader in PR2.
func BuildLabelIndexFromReader(r *fbreader.Reader, doc *graph.Document) *LabelIndex {
	idx, ki := newLabelIndex(r.EntityCount())
	idx.doc = doc
	idx.reader = r
	var i int32
	r.IterateEntities(func(e *fb.Entity) bool {
		idx.add(ki, i, string(e.Id()), string(e.Name()), string(e.QualifiedName()))
		i++
		return true
	})
	return idx
}

// at materializes a fresh heap-copy entity for the given vector index. Each call
// returns a DISTINCT pointer (see the LabelIndex pointer-instability contract).
// Only ever called with an index that came from one of the maps, so the
// bounds/nil guards are defensive.
//
// Value source is gated by GRAFEL_SERVE_FROM_MMAP (default OFF; read once at
// package load, per ADR-0027 §F3):
//
//   - OFF (default / production): copy the LIVE doc.Entities[idx] row, exactly as
//     PR2 does — GC-safe, byte-identical, and with NO handler-path mmap read. The
//     mmap Reader is NOT dereferenced here on the OFF path.
//   - ON (the PR7 flip, DARK until the F1/F2/F3 borrow protocol is wired into the
//     read path): decode the BASE entity from the resident graph.fb via
//     graph.MaterializeEntity (byte-identical to the Document row for the same
//     bytes) and merge the 5 group-algo overlay fields from the side-table — so
//     the value source is the Reader + side-table, NOT lr.Doc. Byte-equal to
//     today's overlaid Doc row (TestOverlaySideTable_ReaderMaterializeByteEqualsOverlaidDoc).
//
// The ON path reads the mmap on the handler path; it MUST NOT be enabled in
// production until a borrow is held across the read (else a concurrent reload's
// munmap is a read-after-unmap). That wiring + flipping the flag is a later step.
// The nil-Reader case (JSON-only / no graph.fb) always uses the Doc copy.
func (l *LabelIndex) at(idx int32) *graph.Entity {
	if l == nil || l.doc == nil || idx < 0 || int(idx) >= len(l.doc.Entities) {
		return nil
	}
	if l.reader != nil && serveFromMMap() {
		e := graph.MaterializeEntity(l.reader, int(idx))
		if ov, ok := l.overlay[idx]; ok {
			e.CommunityID = ov.CommunityID
			e.PageRank = ov.PageRank
			e.Centrality = ov.Centrality
			e.IsGodNode = ov.IsGodNode
			e.IsArticulationPt = ov.IsArticulationPt
		}
		return &e
	}
	e := l.doc.Entities[idx] // heap copy — a fresh pointer, not an alias into Doc
	return &e
}

// ByID returns a freshly materialized entity for the exact id, or nil when the
// id is unknown. Replaces the pre-PR2 `LabelIndex.ByID[id]` map access; the
// returned pointer is NOT stable across calls (see the pointer-instability
// contract). Use HasID for a presence check that avoids materializing.
func (l *LabelIndex) ByID(id string) *graph.Entity {
	if l == nil {
		return nil
	}
	idx, ok := l.byID[id]
	if !ok {
		return nil
	}
	return l.at(idx)
}

// HasID reports whether the exact id is present WITHOUT materializing the
// entity — the presence-check replacement for `_, ok := LabelIndex.ByID[id]`.
func (l *LabelIndex) HasID(id string) bool {
	if l == nil {
		return false
	}
	_, ok := l.byID[id]
	return ok
}

// ByQName returns a freshly materialized entity for the exact qualified name
// (case-insensitive), or nil. Replaces the pre-PR2
// `LabelIndex.ByQName[strings.ToLower(qn)]` map access — callers now pass the
// raw qname and the lowering happens here.
func (l *LabelIndex) ByQName(qname string) *graph.Entity {
	if l == nil {
		return nil
	}
	idx, ok := l.byQName[strings.ToLower(qname)]
	if !ok {
		return nil
	}
	return l.at(idx)
}

// Lookup finds an entity by ID, qualified name (case-insensitive), or label
// (case-insensitive). Label matches return the first hit if there are multiple.
// Returns nil if nothing matched. The returned entity is freshly materialized.
func (l *LabelIndex) Lookup(s string) *graph.Entity {
	if l == nil {
		return nil
	}
	if idx, ok := l.byID[s]; ok {
		return l.at(idx)
	}
	low := strings.ToLower(s)
	if idx, ok := l.byQName[low]; ok {
		return l.at(idx)
	}
	if idxs, ok := l.byLabel[low]; ok && len(idxs) > 0 {
		return l.at(idxs[0])
	}
	return nil
}

// LookupAll returns every entity matching s by ID, qualified name, or label.
// Used by get_source/inspect to surface a clarifier list when a label is
// ambiguous within a repo (#1650). Each returned entity is freshly materialized
// — dedup callers MUST key by entity ID, not pointer identity (PR2 audit).
func (l *LabelIndex) LookupAll(s string) []*graph.Entity {
	if l == nil {
		return nil
	}
	if idx, ok := l.byID[s]; ok {
		return []*graph.Entity{l.at(idx)}
	}
	low := strings.ToLower(s)
	if idx, ok := l.byQName[low]; ok {
		return []*graph.Entity{l.at(idx)}
	}
	if idxs, ok := l.byLabel[low]; ok && len(idxs) > 0 {
		out := make([]*graph.Entity, 0, len(idxs))
		for _, idx := range idxs {
			out = append(out, l.at(idx))
		}
		return out
	}
	return nil
}
