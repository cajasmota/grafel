// Package parity provides a strict, order-independent structural-equivalence
// comparator for two graph Documents — the safety net for grafel's incremental
// reindex work (epic #5376, layer 2 #5309).
//
// Why this exists
// ───────────────
// #5309 will make grafel's graph-wide reindex phases (relationship resolution,
// community detection, link / process-flow / event-flow passes) *incremental*
// — scoped to the blast radius of a change instead of recomputed over the whole
// graph on every push. That is correctness-sensitive: a wrongly-skipped
// re-resolution silently yields a subtly incorrect graph (missing edges, stale
// flows, drifted communities) that no test would otherwise catch.
//
// The differential validator asserts the load-bearing invariant:
//
//	an incremental reindex must produce a graph STRUCTURALLY EQUIVALENT to a
//	full rebuild of the same end-state.
//
// Compare(full, incremental) returns a Report. When the graphs are structurally
// equivalent the report is Equivalent and empty; on any mismatch it carries a
// precise, human-readable diff (which entities / edges / community assignments
// differ, and per-field deltas) so a future incremental bug is debuggable, not
// just "not equal".
//
// Design
// ──────
//   - Order-independent: everything is set/map compared, never slice-index
//     compared. Two documents that list the same entities in a different order
//     are equivalent.
//   - Strict on graph STRUCTURE: entity identity + structural fields,
//     relationship (from, to, kind) + properties, and per-entity community
//     assignment.
//   - Tolerant of legitimately non-deterministic / cosmetic fields: see
//     toleratedEntityField / the document-level skips below. These never affect
//     query correctness, so requiring byte-equality on them would make the
//     validator flaky without buying any safety.
//
// Flows & links: grafel models process-flows, event-flows and links as ordinary
// entities (kinds such as `process_flow`) plus edges (`STEP_IN_PROCESS`,
// `ENTRY_POINT_OF`, link edges). They therefore fall out of the entity/edge
// comparison automatically — there is no separate flow collection on Document.
//
// This package is pure-Go, zero-dependency, and never mutates its inputs.
package parity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
)

// Options tunes the comparator's tolerance profile. The zero value is the
// STRICTEST setting: every entity, edge, property and community assignment must
// match exactly. Tolerances exist for the dimensions the incremental reindex
// path legitimately defers TODAY (before #5309 lands per-phase incrementalism)
// — each one is a known incremental-vs-full gap a later layer will close, not a
// licence to ignore real graph corruption. Keep this list small and explicit.
type Options struct {
	// IgnoreRelKinds suppresses edges of these kinds from the relationship
	// comparison (both presence and properties). Use for edge classes a pass
	// the incremental path does not yet run would produce (e.g. module-aggregation
	// CONTAINS / DEPENDS_ON edges, which #5309's link/flow layer will scope).
	IgnoreRelKinds map[string]bool

	// IgnoreEntityProps suppresses these entity Properties keys from the
	// structural comparison. Use for enrichment-pass outputs the incremental
	// path does not yet recompute (e.g. `module`, `test_reachable`).
	IgnoreEntityProps map[string]bool

	// IgnoreRelProps suppresses these relationship Properties keys.
	IgnoreRelProps map[string]bool

	// NormalizeStubEndpoints folds the not-yet-resolved "stub" edge endpoint
	// form the incremental scoped-resolver can leave on a freshly-extracted
	// cross-file edge onto the entity it names, so an edge that is structurally
	// the same in both graphs but carries a stub id on one side still matches.
	// This is the edge-FromID normalization gap #5309 will close in the scoped
	// resolver; until then the validator normalizes rather than false-alarms.
	NormalizeStubEndpoints bool
}

// Strict returns the zero-tolerance options (exact structural equality).
func Strict() Options { return Options{} }

// Report is the result of Compare. The zero value (Equivalent: true, empty
// slices) means the two graphs are structurally identical.
type Report struct {
	// Equivalent is true iff the two documents are structurally equivalent.
	Equivalent bool

	// EntitiesOnlyInA / EntitiesOnlyInB list entity identities (see entityKey)
	// present in exactly one document.
	EntitiesOnlyInA []string
	EntitiesOnlyInB []string

	// EntityFieldDiffs lists entities present in BOTH documents whose
	// structural fields differ, with a per-field description.
	EntityFieldDiffs []FieldDiff

	// RelsOnlyInA / RelsOnlyInB list relationship identities (from→to:kind)
	// present in exactly one document.
	RelsOnlyInA []string
	RelsOnlyInB []string

	// RelPropDiffs lists relationships present in both whose properties differ.
	RelPropDiffs []FieldDiff

	// CommunityAssignmentDiffs lists entities whose community_id differs between
	// the two documents (a community split / merge / relabel that drifted).
	CommunityAssignmentDiffs []FieldDiff

	// CommunitySetDiff is non-empty when the corpus-level community membership
	// (the set of {community → sorted member ids}) differs.
	CommunitySetDiff []string
}

// FieldDiff describes a single divergence on an entity/relationship/community
// that exists in both documents.
type FieldDiff struct {
	Key    string // the entity / relationship / entity identity this concerns
	Detail string // human-readable description of the divergence
}

// String renders a precise, multi-line diff suitable for a test failure
// message. It is empty when the report is Equivalent.
func (r Report) String() string {
	if r.Equivalent {
		return "graphs are structurally equivalent"
	}
	var b strings.Builder
	b.WriteString("graphs are NOT structurally equivalent:\n")

	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "  %s (%d):\n", title, len(items))
		shown := items
		const cap = 40
		truncated := false
		if len(shown) > cap {
			shown = shown[:cap]
			truncated = true
		}
		for _, it := range shown {
			fmt.Fprintf(&b, "    - %s\n", it)
		}
		if truncated {
			fmt.Fprintf(&b, "    … and %d more\n", len(items)-cap)
		}
	}
	writeFieldDiffs := func(title string, items []FieldDiff) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "  %s (%d):\n", title, len(items))
		shown := items
		const cap = 40
		truncated := false
		if len(shown) > cap {
			shown = shown[:cap]
			truncated = true
		}
		for _, it := range shown {
			fmt.Fprintf(&b, "    - %s: %s\n", it.Key, it.Detail)
		}
		if truncated {
			fmt.Fprintf(&b, "    … and %d more\n", len(items)-cap)
		}
	}

	writeList("entities only in A (full rebuild)", r.EntitiesOnlyInA)
	writeList("entities only in B (incremental)", r.EntitiesOnlyInB)
	writeFieldDiffs("entity field diffs", r.EntityFieldDiffs)
	writeList("relationships only in A (full rebuild)", r.RelsOnlyInA)
	writeList("relationships only in B (incremental)", r.RelsOnlyInB)
	writeFieldDiffs("relationship property diffs", r.RelPropDiffs)
	writeFieldDiffs("community assignment diffs", r.CommunityAssignmentDiffs)
	writeList("community membership diffs", r.CommunitySetDiff)
	return b.String()
}

// Compare returns a Report describing the strict structural equivalence of two
// documents (no tolerances). It is a thin wrapper over CompareWithOptions with
// Strict() options. By convention a is the reference (full rebuild) and b is
// the candidate (incremental result). Neither argument is mutated.
func Compare(a, b *graph.Document) Report {
	return CompareWithOptions(a, b, Strict())
}

// CompareWithOptions is Compare with an explicit tolerance profile (see Options).
//
// A nil document is treated as an empty document.
func CompareWithOptions(a, b *graph.Document, opts Options) Report {
	if a == nil {
		a = &graph.Document{}
	}
	if b == nil {
		b = &graph.Document{}
	}

	r := Report{Equivalent: true}

	compareEntities(a, b, &r, opts)
	compareRelationships(a, b, &r, opts)
	compareCommunities(a, b, &r)

	if len(r.EntitiesOnlyInA) > 0 || len(r.EntitiesOnlyInB) > 0 ||
		len(r.EntityFieldDiffs) > 0 ||
		len(r.RelsOnlyInA) > 0 || len(r.RelsOnlyInB) > 0 ||
		len(r.RelPropDiffs) > 0 ||
		len(r.CommunityAssignmentDiffs) > 0 || len(r.CommunitySetDiff) > 0 {
		r.Equivalent = false
	}
	return r
}

// ───────────────────────────── entities ─────────────────────────────

// entityKey is the stable identity of an entity for set comparison. The graph's
// own EntityID is a hash of (repo, kind, name, source_file); we key on the id
// when present, but fold in the human-readable identity so the diff output is
// legible and so two entities with a hash collision (astronomically unlikely)
// still compare on their real fields.
func entityKey(e graph.Entity) string {
	id := e.ID
	if id == "" {
		id = graph.EntityID("", e.Kind, e.Name, e.SourceFile)
	}
	// Name is folded into the key purely for legibility of the diff output —
	// the id already hashes (kind, name, source_file), so two entities that
	// share a key necessarily share these fields.
	return fmt.Sprintf("%s|%s|%s|%s|%s", id, e.Kind, e.Name, e.QualifiedName, e.SourceFile)
}

func compareEntities(a, b *graph.Document, r *Report, opts Options) {
	mapA := make(map[string]graph.Entity, len(a.Entities))
	for _, e := range a.Entities {
		mapA[entityKey(e)] = e
	}
	mapB := make(map[string]graph.Entity, len(b.Entities))
	for _, e := range b.Entities {
		mapB[entityKey(e)] = e
	}

	for k, ea := range mapA {
		eb, ok := mapB[k]
		if !ok {
			r.EntitiesOnlyInA = append(r.EntitiesOnlyInA, k)
			continue
		}
		if diff := entityStructuralDiff(ea, eb, opts); diff != "" {
			r.EntityFieldDiffs = append(r.EntityFieldDiffs, FieldDiff{Key: k, Detail: diff})
		}
	}
	for k := range mapB {
		if _, ok := mapA[k]; !ok {
			r.EntitiesOnlyInB = append(r.EntitiesOnlyInB, k)
		}
	}

	sort.Strings(r.EntitiesOnlyInA)
	sort.Strings(r.EntitiesOnlyInB)
	sortFieldDiffs(r.EntityFieldDiffs)
}

// entityStructuralDiff compares the structural (correctness-bearing) fields of
// two entities with the SAME identity key and returns a description of any
// divergence, or "" when equivalent. Tolerated / cosmetic fields (see below)
// are intentionally NOT compared.
//
// NB: community_id is compared separately in compareCommunities so that a
// community drift is reported in its own bucket rather than as a generic field
// diff — it is the single most likely thing the incremental community phase
// will get wrong.
func entityStructuralDiff(a, b graph.Entity, opts Options) string {
	var diffs []string
	cmpStr := func(name, va, vb string) {
		if va != vb {
			diffs = append(diffs, fmt.Sprintf("%s %q≠%q", name, va, vb))
		}
	}
	cmpInt := func(name string, va, vb int) {
		if va != vb {
			diffs = append(diffs, fmt.Sprintf("%s %d≠%d", name, va, vb))
		}
	}
	cmpStr("name", a.Name, b.Name)
	cmpStr("qualified_name", a.QualifiedName, b.QualifiedName)
	cmpStr("kind", a.Kind, b.Kind)
	cmpStr("subtype", a.Subtype, b.Subtype)
	cmpStr("source_file", a.SourceFile, b.SourceFile)
	cmpStr("language", a.Language, b.Language)
	cmpStr("signature", a.Signature, b.Signature)
	cmpInt("start_line", a.StartLine, b.StartLine)
	cmpInt("end_line", a.EndLine, b.EndLine)

	if d := stringSliceDiff("tags", a.Tags, b.Tags); d != "" {
		diffs = append(diffs, d)
	}
	if d := stringMapDiff("properties", filterMap(a.PropsSnapshot(), opts.IgnoreEntityProps), filterMap(b.PropsSnapshot(), opts.IgnoreEntityProps)); d != "" {
		diffs = append(diffs, d)
	}

	return strings.Join(diffs, "; ")
}

// ─────────────────────────── relationships ──────────────────────────

func relKey(fromID, toID, kind string) string {
	return fromID + "→" + toID + ":" + kind
}

func compareRelationships(a, b *graph.Document, r *Report, opts Options) {
	// Build a stub→entity-id resolver over the union of both entity sets so that
	// an un-resolved cross-file edge endpoint (the incremental scoped-resolver
	// gap) folds onto the entity it names. No-op unless NormalizeStubEndpoints.
	resolver := newEndpointResolver(a, b, opts)

	mapA := make(map[string]graph.Relationship, len(a.Relationships))
	for _, rel := range a.Relationships {
		if opts.IgnoreRelKinds[rel.Kind] {
			continue
		}
		mapA[relKey(resolver(rel.FromID), resolver(rel.ToID), rel.Kind)] = rel
	}
	mapB := make(map[string]graph.Relationship, len(b.Relationships))
	for _, rel := range b.Relationships {
		if opts.IgnoreRelKinds[rel.Kind] {
			continue
		}
		mapB[relKey(resolver(rel.FromID), resolver(rel.ToID), rel.Kind)] = rel
	}

	for k, ra := range mapA {
		rb, ok := mapB[k]
		if !ok {
			r.RelsOnlyInA = append(r.RelsOnlyInA, k)
			continue
		}
		if d := stringMapDiff("properties", filterMap(ra.PropsSnapshot(), opts.IgnoreRelProps), filterMap(rb.PropsSnapshot(), opts.IgnoreRelProps)); d != "" {
			r.RelPropDiffs = append(r.RelPropDiffs, FieldDiff{Key: k, Detail: d})
		}
	}
	for k := range mapB {
		if _, ok := mapA[k]; !ok {
			r.RelsOnlyInB = append(r.RelsOnlyInB, k)
		}
	}

	sort.Strings(r.RelsOnlyInA)
	sort.Strings(r.RelsOnlyInB)
	sortFieldDiffs(r.RelPropDiffs)
}

// ──────────────────────────── communities ───────────────────────────

// compareCommunities asserts two things:
//
//  1. Per-entity community assignment parity: every entity present in both
//     graphs has the same community_id. (Community IDs are integer labels; the
//     incremental community phase must not relabel an unchanged partition.)
//  2. Corpus membership parity: the set of {community_id → sorted member
//     entity-keys} matches. This catches a split/merge that happens to leave
//     most per-entity labels intact.
//
// Only entities present in BOTH graphs are considered for assignment parity —
// entities that are added/removed are already reported by compareEntities, and
// re-reporting their (necessarily different) community here would be noise.
func compareCommunities(a, b *graph.Document, r *Report) {
	keyA := make(map[string]graph.Entity, len(a.Entities))
	for _, e := range a.Entities {
		keyA[entityKey(e)] = e
	}

	for _, eb := range b.Entities {
		k := entityKey(eb)
		ea, ok := keyA[k]
		if !ok {
			continue // entity-set diff already reports this
		}
		ca, cb := communityLabel(ea), communityLabel(eb)
		if ca != cb {
			r.CommunityAssignmentDiffs = append(r.CommunityAssignmentDiffs,
				FieldDiff{Key: k, Detail: fmt.Sprintf("community_id %s≠%s", ca, cb)})
		}
	}
	sortFieldDiffs(r.CommunityAssignmentDiffs)

	// Corpus membership: community label → sorted member entity-keys, over the
	// shared entity set only (so add/remove churn doesn't masquerade as a
	// community-detection change).
	shared := func(d *graph.Document, other map[string]graph.Entity) map[string][]string {
		m := make(map[string][]string)
		for _, e := range d.Entities {
			k := entityKey(e)
			if _, ok := other[k]; !ok {
				continue
			}
			m[communityLabel(e)] = append(m[communityLabel(e)], k)
		}
		for lbl := range m {
			sort.Strings(m[lbl])
		}
		return m
	}
	keyB := make(map[string]graph.Entity, len(b.Entities))
	for _, e := range b.Entities {
		keyB[entityKey(e)] = e
	}
	memA := shared(a, keyB)
	memB := shared(b, keyA)

	labels := make(map[string]struct{})
	for l := range memA {
		labels[l] = struct{}{}
	}
	for l := range memB {
		labels[l] = struct{}{}
	}
	var lbls []string
	for l := range labels {
		lbls = append(lbls, l)
	}
	sort.Strings(lbls)
	for _, l := range lbls {
		ma, mb := memA[l], memB[l]
		if strings.Join(ma, ",") != strings.Join(mb, ",") {
			r.CommunitySetDiff = append(r.CommunitySetDiff,
				fmt.Sprintf("community %s: %d member(s) in A, %d in B", l, len(ma), len(mb)))
		}
	}
}

// communityLabel renders an entity's community_id as a stable string ("∅" when
// unset / nil), so the maps above never key on a *int.
func communityLabel(e graph.Entity) string {
	if e.CommunityID == nil {
		return "∅"
	}
	return fmt.Sprintf("%d", *e.CommunityID)
}

// ─────────────────────────── shared helpers ─────────────────────────

func stringSliceDiff(name string, a, b []string) string {
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	if strings.Join(sa, ",") == strings.Join(sb, ",") {
		return ""
	}
	return fmt.Sprintf("%s [%s]≠[%s]", name, strings.Join(sa, ","), strings.Join(sb, ","))
}

func stringMapDiff(name string, a, b map[string]string) string {
	if len(a) == 0 && len(b) == 0 {
		return ""
	}
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	var ks []string
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var parts []string
	for _, k := range ks {
		va, oka := a[k]
		vb, okb := b[k]
		switch {
		case oka && !okb:
			parts = append(parts, fmt.Sprintf("-%s=%q", k, va))
		case !oka && okb:
			parts = append(parts, fmt.Sprintf("+%s=%q", k, vb))
		case va != vb:
			parts = append(parts, fmt.Sprintf("%s %q≠%q", k, va, vb))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%s {%s}", name, strings.Join(parts, ", "))
}

func sortFieldDiffs(d []FieldDiff) {
	sort.Slice(d, func(i, j int) bool { return d[i].Key < d[j].Key })
}

// filterMap returns a copy of m with any keys present in drop removed. Returns
// m unchanged when drop is empty (the common strict path).
func filterMap(m map[string]string, drop map[string]bool) map[string]string {
	if len(drop) == 0 || len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if !drop[k] {
			out[k] = v
		}
	}
	return out
}

// newEndpointResolver returns a function that maps a relationship endpoint id to
// a canonical entity id. When NormalizeStubEndpoints is off it is the identity.
// When on, an endpoint that is NOT a known 16-hex entity id is treated as a
// "stub" of the form "...:<EntityName>" (the form the incremental scoped
// resolver can leave on a freshly-extracted cross-file edge); its final
// colon-segment is resolved via a name→id index built over BOTH entity sets, so
// the same logical edge matches regardless of which side resolved it.
func newEndpointResolver(a, b *graph.Document, opts Options) func(string) string {
	if !opts.NormalizeStubEndpoints {
		return func(s string) string { return s }
	}
	nameToID := make(map[string]string)
	add := func(e graph.Entity) {
		if e.ID == "" {
			return
		}
		if e.Name != "" {
			// First writer wins; collisions across files are rare for the tiny
			// fixtures this targets, and a collision only weakens normalization
			// (it can't manufacture a false match that strict structure misses).
			if _, ok := nameToID[e.Name]; !ok {
				nameToID[e.Name] = e.ID
			}
		}
		if e.QualifiedName != "" {
			if _, ok := nameToID[e.QualifiedName]; !ok {
				nameToID[e.QualifiedName] = e.ID
			}
		}
	}
	for _, e := range a.Entities {
		add(e)
	}
	for _, e := range b.Entities {
		add(e)
	}
	return func(s string) string {
		if isHexID(s) {
			return s
		}
		// Try the whole string, then its final colon-segment (the entity name
		// in a "scope:operation:...:Name" stub).
		if id, ok := nameToID[s]; ok {
			return id
		}
		if i := strings.LastIndex(s, ":"); i >= 0 && i+1 < len(s) {
			if id, ok := nameToID[s[i+1:]]; ok {
				return id
			}
		}
		return s
	}
}

// isHexID reports whether s is a 16-char lowercase-hex grafel entity id.
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
