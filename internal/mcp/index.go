package mcp

import (
	"sort"
	"strings"
	"sync"

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
	//
	// #5912 (h): typed fbreader.GraphView, not the concrete *Reader, so a
	// segment-set repo backs this index with a *MultiReader (N segment mmaps,
	// global-index at()/EntityAt resolution) transparently — every at()/count
	// call below is a GraphView method already implemented identically on both.
	reader fbreader.GraphView

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

	// descOverlay is the DESCRIPTION side-table (#5904 PR-a): entity INDEX (int32
	// vector position) -> the agent-written "description" property that lives in
	// <stateDir>/descriptions.json rather than in graph.fb (the enrichment
	// write-back no longer rewrites the graph — the #5915 P1 collapse hazard).
	// materializeFromReader ADDITIVELY PropSet-s it onto the Reader-materialized
	// base so a lookup on the mmap path surfaces the description exactly as the
	// overlaid Doc row does. A miss leaves whatever description graph.fb carried
	// (extractor-native / baked-in) intact — never cleared. Populated by
	// applyDescriptionOverlay from the SAME descriptions.json it PropSet-s onto
	// lr.Doc, keyed by resolving each entity ID to its vector index. Built ONLY
	// when GRAFEL_SERVE_FROM_MMAP is ON (the flag-off Doc path never consults it);
	// nil otherwise and until a description sidecar is applied.
	descOverlay map[int32]string

	// readerMu points at the owning LoadedRepo.readerMu — the strictly-innermost
	// ADR-0027 SIGBUS-safety mutex (memory epic #5850, "Option B"). Wired by
	// reloadLocked; nil for the Doc-only build and directly-constructed indexes
	// (tests), where at() takes the legacy unlocked reader/Doc path. When non-nil,
	// at() dereferences the mmap ONLY under this mutex and ONLY after checking
	// handle.readRetired, so a lookup on a *LabelIndex captured before a reload —
	// whose mapping the reload retired+munmapped — falls back to the Doc instead of
	// dereferencing the freed region.
	readerMu *sync.Mutex
	// handle is this index generation's MapHandle (its reader == reader above).
	// at() checks handle.readRetired under readerMu. Set together with readerMu.
	handle *MapHandle

	byID    map[string]int32
	byLabel map[string][]int32
	byQName map[string]int32

	// byKind maps an entity's RAW Kind string -> its vector indices, in the SAME
	// index space as byID (the IterateEntities / range counter both build paths
	// share). Each list is index-sorted (appended in vector order). Keys are the
	// RAW kind (NOT lowered/scope-stripped) because the hot Kind-predicate
	// scanners classify raw kinds themselves (isDefinitionKind / classifyEndpoint-
	// Kind lower+strip internally; isTopic lowers; the process/pattern scanners
	// compare exact raw kinds). memory epic #5850 / mmap-flip #5870: it lets a
	// Kind-filtered scanner (forEachEntityOfKinds) materialize ONLY the entities
	// whose Kind matches its predicate — dozens of kinds vs 427k entities —
	// instead of forEach-materializing the whole set per query. Populated by the
	// SAME add() both build paths funnel through, so the Doc-built and Reader-
	// built maps are DeepEqual, exactly like byID/byLabel/byQName.
	byKind map[string][]int32
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
		byKind:  make(map[string][]int32),
	}, &keyInterner{}
}

// add records the i-th entity's (id, name, qname, kind) into the index maps.
// Keying mirrors the pre-PR2 pointer build exactly: byID by raw id, byLabel by
// lowercased name (appended → ambiguity list in vector order), byQName by
// lowercased qname (last-write-wins, skipped when empty). byKind appends the
// index under the RAW kind (interned — kinds repeat heavily across entities, so
// one backing string per distinct kind), preserving vector order so each list
// stays index-sorted.
func (l *LabelIndex) add(ki *keyInterner, i int32, id, name, qname, kind string) {
	l.byID[id] = i
	lbl := ki.intern(strings.ToLower(name))
	l.byLabel[lbl] = append(l.byLabel[lbl], i)
	if qname != "" {
		l.byQName[ki.intern(strings.ToLower(qname))] = i
	}
	k := ki.intern(kind)
	l.byKind[k] = append(l.byKind[k], i)
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
		idx.add(ki, int32(i), e.ID, e.Name, e.QualifiedName, e.Kind)
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
func BuildLabelIndexFromReader(r fbreader.GraphView, doc *graph.Document) *LabelIndex {
	idx, ki := newLabelIndex(r.EntityCount())
	idx.doc = doc
	idx.reader = r
	var i int32
	r.IterateEntities(func(e *fb.Entity) bool {
		idx.add(ki, i, string(e.Id()), string(e.Name()), string(e.QualifiedName()), string(e.Kind()))
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
//
// Bound source (memory epic #5850 Path P PR6): the OFF/Doc-fallback paths
// bound idx against len(l.doc.Entities); the ON/resident-reader path bounds
// idx against l.reader.EntityCount() instead, so a PR7 Doc-emptying (which
// drops doc.Entities to length 0 while the reader still holds every row)
// cannot turn every valid index into a false "not found".
func (l *LabelIndex) at(idx int32) *graph.Entity {
	if l == nil || l.doc == nil || idx < 0 {
		return nil
	}
	if l.reader != nil && serveFromMMap() {
		// ADR-0027 SIGBUS-safety (memory epic #5850): when wired (production), take
		// the strictly-innermost readerMu around the mmap dereference. If this
		// generation's mapping was retired+munmapped by a concurrent reload (a stale
		// captured *LabelIndex), readRetired is set true under this same readerMu, so
		// fall back to the GC-safe Doc copy instead of dereferencing the freed
		// region. Unwired indexes (readerMu==nil: tests / direct construction) keep
		// the legacy unlocked reader path — no reload retires them.
		if l.readerMu != nil {
			l.readerMu.Lock()
			if l.handle != nil && l.handle.readRetired {
				l.readerMu.Unlock()
				// Retired generation → return nil, do NOT fall back to
				// l.doc.Entities. This branch fires only for a STALE captured
				// *LabelIndex (e.g. the lock-free BM25 resolver closure) whose
				// generation was retired+munmapped by a concurrent reload. Two
				// reasons the Doc row is no longer a valid source here:
				//   1. Post-deretain-flip (#5870 PR7bc), reloadLocked skeletonizes
				//      lr.Doc for reader-present repos — doc.Entities is length 0,
				//      so indexing it would be an out-of-range panic (and even the
				//      len-guarded fallback would report every index "not found").
				//   2. A retired generation's index is stale against the reindexed
				//      successor generation anyway — a Doc-sourced row from the
				//      superseded graph is not a meaningful answer.
				// Callers that resolve indices lock-free (BM25 Search via
				// idx.resolve → at()) already tolerate a nil entity by skipping
				// that result row (see scoring.go Search: `if ent == nil { continue }`).
				return nil
			}
			// PR6: the bound is the RESIDENT READER's row count, not
			// len(l.doc.Entities) — a PR7 Doc-emptying leaves doc.Entities at 0
			// while the reader still holds every row, so a Doc-sourced bound would
			// wrongly report every index out of range.
			if int(idx) >= l.reader.EntityCount() {
				l.readerMu.Unlock()
				return nil
			}
			e := l.materializeFromReader(idx)
			l.readerMu.Unlock()
			return e
		}
		if int(idx) >= l.reader.EntityCount() {
			return nil
		}
		return l.materializeFromReader(idx)
	}
	if int(idx) >= len(l.doc.Entities) {
		return nil
	}
	e := l.doc.Entities[idx] // heap copy — a fresh pointer, not an alias into Doc
	return &e
}

// atMaterializeHook, when non-nil, is invoked once per Reader entity
// materialization. Test-only observability for single-materialization perf
// assertions (memory epic #5850 Path P: getByIDOne must materialize exactly one
// entity, not the whole set). Nil in production — one predictable nil-check.
var atMaterializeHook func()

// materializeFromReader decodes the base entity from the mmap Reader and merges
// the group-algo overlay side-table. Callers on the wired path MUST hold
// readerMu (the mmap dereference happens here); MaterializeEntity copies every
// string out to the heap, so the returned entity is safe to use past release.
func (l *LabelIndex) materializeFromReader(idx int32) *graph.Entity {
	if atMaterializeHook != nil {
		atMaterializeHook()
	}
	e := graph.MaterializeEntity(l.reader, int(idx))
	if ov, ok := l.overlay[idx]; ok {
		e.CommunityID = ov.CommunityID
		e.PageRank = ov.PageRank
		e.Centrality = ov.Centrality
		e.IsGodNode = ov.IsGodNode
		e.IsArticulationPt = ov.IsArticulationPt
	}
	// Description side-table (#5904 PR-a): ADDITIVE merge — a present entry sets
	// (or overrides) the "description" property; a miss leaves the base entity's
	// own description (extractor-native / baked-in) untouched.
	if d, ok := l.descOverlay[idx]; ok {
		e.PropSet("description", d)
	}
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

// indicesForKinds returns the union of vector indices for every byKind KEY that
// satisfies pred, in ASCENDING INDEX ORDER — i.e. the SAME order the entities
// occupy in the entity vector, so a scanner that iterates the result visits them
// in exactly the order a forEachEntity full-scan + Kind filter would (memory epic
// #5850 / mmap-flip #5870, order-preservation is load-bearing for the ordered
// scanners).
//
// It iterates byKind's KEYS (dozens of kinds, not 427k entities) and applies pred
// to each raw kind string; only the matching kinds' index lists are gathered. An
// entity has exactly ONE kind, so the gathered lists are disjoint — no dedup is
// needed. Each per-kind list is already index-sorted, but map iteration order over
// the keys is nondeterministic, so the merged union is sorted ascending before
// return; dropping that sort is what the order-mutation test detects.
func (l *LabelIndex) indicesForKinds(pred func(kind string) bool) []int32 {
	if l == nil || l.byKind == nil || pred == nil {
		return nil
	}
	var out []int32
	for k, idxs := range l.byKind {
		if pred(k) {
			out = append(out, idxs...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
