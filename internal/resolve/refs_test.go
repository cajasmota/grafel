package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func ent(id, kind, name string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: kind, Name: name, SourceFile: "x.go"}
}

func entAt(id, kind, name, file string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: kind, Name: name, SourceFile: file}
}

func TestReferences_Unambiguous(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("unambiguous: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_Ambiguous(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Foo"),
		ent("bbbbbbbbbbbbbbbb", "Function", "Foo"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Foo", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Foo" {
		t.Fatalf("ambiguous: ToID was rewritten to %s, expected stub preserved", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %d", stats.Ambiguous)
	}
}

func TestReferences_Unmatched(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Missing", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Missing" {
		t.Fatalf("unmatched: ToID was rewritten: %s", rels[0].ToID)
	}
	if stats.Unmatched != 1 {
		t.Fatalf("expected 1 unmatched, got %d", stats.Unmatched)
	}
}

func TestReferences_KindAware(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "User"),
		ent("bbbbbbbbbbbbbbbb", "View", "User"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:User", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("kind-aware: ToID resolved to wrong entity: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_StubMissingPrefix(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("bare-name fallback: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_ScopePrefixedKind(t *testing.T) {
	// Pass 3 cross-language extractors emit kinds like "SCOPE.View". A stub
	// "View:User" must still resolve to that entity.
	entities := []types.EntityRecord{ent("cccccccccccccccc", "SCOPE.View", "Dashboard")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:Dashboard", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "cccccccccccccccc" {
		t.Fatalf("scope-prefixed kind: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_SkipsHexIDs(t *testing.T) {
	// Already-resolved IDs (16-char lower hex) must be left untouched.
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ffffffffffffffff", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "ffffffffffffffff" {
		t.Fatalf("hex-ID was modified: %s", rels[0].ToID)
	}
	if stats.Rewritten != 0 || stats.Ambiguous != 0 || stats.Unmatched != 0 {
		t.Fatalf("hex-ID counted in stats: %+v", stats)
	}
}

func TestReferencesEmbedded(t *testing.T) {
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{ToID: "Hello", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	if records[1].Relationships[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("embedded: ToID not rewritten: %s", records[1].Relationships[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

// PORT-2-FIX-3 (issue #31) — structural reference resolution + kind hint.

func TestReferences_StructuralFormatA(t *testing.T) {
	// scope:component:class:python:core/views/orders.py:OrderViewSet
	// resolves to the entity at that file+name.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("dddddddddddddddd", "SCOPE.Component", "Other", "core/views/other.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:component:class:python:core/views/orders.py:OrderViewSet",
		Kind:   "USES",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("format A: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_StructuralFormatB(t *testing.T) {
	// scope:operation:method:python:core/views/orders.py:OrderViewSet#create
	// resolves to the method entity recorded as "OrderViewSet.create".
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "OrderViewSet.create", "core/views/orders.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:operation:method:python:core/views/orders.py:OrderViewSet#create",
		Kind:   "CALLS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("format B: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_StructuralLocationAmbiguous(t *testing.T) {
	// Same (file, name) recorded twice → ambiguous, stub preserved.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "OrderViewSet", "core/views/orders.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.View", "OrderViewSet", "core/views/orders.py"),
	}
	stub := "scope:component:class:python:core/views/orders.py:OrderViewSet"
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: stub, Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != stub {
		t.Fatalf("location-ambig: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %+v", stats)
	}
}

func TestReferences_KindHintCallsPrefersOperation(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Operation", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("kind-hint CALLS: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_KindHintExtendsPrefersComponent(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "EXTENDS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("kind-hint EXTENDS: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %+v", stats)
	}
}

func TestReferences_KindHintMissingStillAmbiguous(t *testing.T) {
	// Same setup as the disambiguation test, but the relationship's Kind
	// doesn't bias toward any family → still ambiguous, stub preserved.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Operation", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Foo" {
		t.Fatalf("hint-missing: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %+v", stats)
	}
}

// PORT-2-FIX-4 (issue #48) — FromID rewrite + kind-aware location index.

func TestReferencesEmbedded_FromIDRewritten(t *testing.T) {
	// Embedded relationship has a stub FromID. After ReferencesEmbedded,
	// FromID must be rewritten to the resolved entity ID.
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "Function:Hello", ToID: "ffffffffffffffff", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	if got := records[1].Relationships[0].FromID; got != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("FromID not rewritten: %s", got)
	}
	if stats.FromRewritten != 1 {
		t.Fatalf("expected FromRewritten=1, got %+v", stats)
	}
}

func TestReferencesEmbedded_BothEndpointsRewritten(t *testing.T) {
	// Both FromID and ToID stubs; both should be rewritten and counted.
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{ID: "cccccccccccccccc", Kind: "Function", Name: "World", SourceFile: "c.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{FromID: "Function:Hello", ToID: "Function:World", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	rel := records[2].Relationships[0]
	if rel.FromID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("FromID not rewritten: %s", rel.FromID)
	}
	if rel.ToID != "cccccccccccccccc" {
		t.Fatalf("ToID not rewritten: %s", rel.ToID)
	}
	if stats.FromRewritten != 1 || stats.ToRewritten != 1 || stats.Rewritten != 2 {
		t.Fatalf("expected from=1 to=1 total=2, got %+v", stats)
	}
}

func TestReferences_SameFileKindDisambiguation(t *testing.T) {
	// Two entities at (a.py, "Foo") with different kinds. EXTENDS hint
	// biases toward Component family → resolves to Component entity.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "Foo", "a.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "Foo",
		Kind:   "EXTENDS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("kind-disambig EXTENDS: ToID=%s", rels[0].ToID)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("expected ToRewritten=1, got %+v", stats)
	}
}

func TestReferences_SameFileKindNoHintStillAmbiguous(t *testing.T) {
	// Same setup as above, but the relationship Kind doesn't bias toward
	// any family → stub preserved (ambiguous), preserving existing
	// behavior when kind hint is absent.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "Foo", "a.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "Foo",
		Kind:   "USES",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Foo" {
		t.Fatalf("no-hint: ToID was rewritten to %s", rels[0].ToID)
	}
	if stats.ToAmbiguous != 1 {
		t.Fatalf("expected ToAmbiguous=1, got %+v", stats)
	}
}

func TestReferences_StatsTrackingPerEndpoint(t *testing.T) {
	// Mix of outcomes across both endpoints — verify the per-endpoint
	// counters tally correctly.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Hello", "a.go"),
	}
	rels := []types.RelationshipRecord{
		// from rewritten, to unmatched
		{FromID: "Function:Hello", ToID: "Function:Missing", Kind: "CALLS"},
		// from unmatched, to rewritten
		{FromID: "Function:Nope", ToID: "Function:Hello", Kind: "CALLS"},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if stats.FromRewritten != 1 {
		t.Fatalf("FromRewritten: %+v", stats)
	}
	if stats.FromUnmatched != 1 {
		t.Fatalf("FromUnmatched: %+v", stats)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("ToRewritten: %+v", stats)
	}
	if stats.ToUnmatched != 1 {
		t.Fatalf("ToUnmatched: %+v", stats)
	}
	if stats.Rewritten != 2 || stats.Unmatched != 2 {
		t.Fatalf("aggregates: %+v", stats)
	}
}

func TestReferences_StructuralFormatA_KindDisambiguation(t *testing.T) {
	// PORT-2-FIX-2 emits two entities at the same (file, name) with
	// different kinds (Component class + Operation method named the same
	// as the class). Format-A structural ref with scope-kind "component"
	// should pick the Component entity.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "SCOPE.Component", "CoreConfig", "core/apps.py"),
		entAt("bbbbbbbbbbbbbbbb", "SCOPE.Operation", "CoreConfig", "core/apps.py"),
	}
	rels := []types.RelationshipRecord{{
		FromID: "0000000000000000",
		ToID:   "scope:component:class:python:core/apps.py:CoreConfig",
		Kind:   "EXTENDS",
	}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("structural-A kind-aware: ToID=%s", rels[0].ToID)
	}
	if stats.ToRewritten != 1 {
		t.Fatalf("expected ToRewritten=1, got %+v", stats)
	}
}

// VERIFY-2-PREP / issue #56 — disposition tagging.

// allowDjango is a tiny test-only ExternalAllowlist that recognises
// just "django" as a known external package. Keeps the tests free of an
// import on internal/external.
func allowDjango(pkg string) bool { return pkg == "django" }

func TestDisposition_Resolved(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionResolved]; got != 2 {
		t.Fatalf("expected 2 resolved (from + to), got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_ExternalKnown(t *testing.T) {
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ext:django", Kind: "CALLS"}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionExternalKnown]; got != 1 {
		t.Fatalf("expected 1 external-known, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_ExternalUnknown(t *testing.T) {
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ext:somerandompackage", Kind: "CALLS"}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionExternalUnknown]; got != 1 {
		t.Fatalf("expected 1 external-unknown, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_Dynamic(t *testing.T) {
	rels := []types.RelationshipRecord{{
		FromID:     "0000000000000000",
		ToID:       "getattr(self, name)",
		Kind:       "CALLS",
		Properties: map[string]string{"language": "python"},
	}}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
		t.Fatalf("expected 1 dynamic, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_BugExtractor(t *testing.T) {
	// Graph has 0 entities named "NonexistentClass" → bug-extractor.
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "View", "RealView")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:NonexistentClass", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 1 {
		t.Fatalf("expected 1 bug-extractor, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_BugResolver(t *testing.T) {
	// Three entities all named "Foo" under different kinds → bare-name
	// "Foo" with USES (no kind hint) is ambiguous, but the name DOES
	// exist in the graph → bug-resolver, not bug-extractor.
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Function", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Foo", "b.py"),
		entAt("cccccccccccccccc", "View", "Foo", "c.py"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugResolver]; got != 1 {
		t.Fatalf("expected 1 bug-resolver, got %+v", stats.DispositionCounts)
	}
}

func TestDisposition_SampleCap(t *testing.T) {
	// 10 distinct unmatched stubs in the bug-extractor bucket — only 5
	// should be retained as samples.
	rels := make([]types.RelationshipRecord, 0, 10)
	for i := 0; i < 10; i++ {
		rels = append(rels, types.RelationshipRecord{
			FromID: "0000000000000000",
			ToID:   "View:Missing" + string(rune('A'+i)),
			Kind:   "USES",
		})
	}
	idx := BuildIndex(nil)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)
	if got := stats.DispositionCounts[DispositionBugExtractor]; got != 10 {
		t.Fatalf("expected 10 bug-extractor counts, got %d", got)
	}
	if got := len(stats.DispositionSamples[DispositionBugExtractor]); got != 5 {
		t.Fatalf("expected 5 retained samples, got %d", got)
	}
}

func TestDisposition_BugRate(t *testing.T) {
	// 1 resolved (to) + 1 resolved (from hex) → 2 endpoints resolved.
	// 1 bug-extractor (View:Missing) + 1 bug-resolver (bare "Foo" with
	// 2 entities under different kinds + USES kind which doesn't bias).
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Hello"),
		entAt("bbbbbbbbbbbbbbbb", "Function", "Foo", "a.py"),
		entAt("cccccccccccccccc", "Component", "Foo", "b.py"),
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"},
		{FromID: "0000000000000000", ToID: "View:Missing", Kind: "USES"},
		{FromID: "0000000000000000", ToID: "Foo", Kind: "USES"},
	}
	idx := BuildIndex(entities)
	stats := ReferencesWithAllowlist(rels, idx, allowDjango)

	bugs := stats.DispositionCounts[DispositionBugExtractor] +
		stats.DispositionCounts[DispositionBugResolver]
	if bugs != 2 {
		t.Fatalf("expected 2 bug endpoints, got %+v", stats.DispositionCounts)
	}
	// 6 endpoints total (3 rels x 2 endpoints).
	expected := 2.0 / 6.0
	if stats.BugRate < expected-1e-9 || stats.BugRate > expected+1e-9 {
		t.Fatalf("bug rate: expected %.4f got %.4f counts=%+v", expected, stats.BugRate, stats.DispositionCounts)
	}
}

// TestDisposition_Unclassified covers the catch-all Disposition bucket
// (issue #81). DispositionUnclassified fires when an endpoint slips past
// every other check in classifyDispositionLang: it's not a hex ID, not an
// "ext:" placeholder, doesn't match a dynamic-dispatch pattern, and the
// name extracted from the stub is empty — leaving nothing for the bug-*
// classifier to look up. Real production stubs that hit this path include
// trailing-colon ("Kind:") forms emitted when an extractor failed to
// capture the target name, and malformed structural-refs whose tail
// segment is blank.
func TestDisposition_Unclassified(t *testing.T) {
	cases := []struct {
		name string
		stub string
	}{
		{
			// Bare "Kind:" with no name half — splitStub yields
			// ("Kind", "") → name == "" → Unclassified.
			name: "trailing colon empty name",
			stub: "View:",
		},
		{
			// Just the delimiter — both halves empty, kind-agnostic
			// path also has nothing to look up.
			name: "delimiter only",
			stub: ":",
		},
		{
			// Structural-ref with all 6 segments but a blank tail.
			// classifyDispositionLang's scope-aware extraction pulls
			// tail → name == "" → Unclassified. Note: the resolver's
			// lookupStructural separately rejects this with
			// statusUnmatched, but the disposition classifier still
			// has to bucket the endpoint.
			name: "scope ref with empty tail",
			stub: "scope:component:class:python:pkg/file.py:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rels := []types.RelationshipRecord{{
				FromID: "0000000000000000",
				ToID:   tc.stub,
				Kind:   "USES",
			}}
			idx := BuildIndex(nil)
			stats := ReferencesWithAllowlist(rels, idx, allowDjango)
			if got := stats.DispositionCounts[DispositionUnclassified]; got != 1 {
				t.Fatalf("stub %q: expected 1 unclassified, got counts=%+v",
					tc.stub, stats.DispositionCounts)
			}
			// Sanity: the catch-all must NOT leak into the bug-*
			// buckets — those fire only when a name was extracted.
			if got := stats.DispositionCounts[DispositionBugExtractor]; got != 0 {
				t.Fatalf("stub %q: unexpected bug-extractor count %d", tc.stub, got)
			}
			if got := stats.DispositionCounts[DispositionBugResolver]; got != 0 {
				t.Fatalf("stub %q: unexpected bug-resolver count %d", tc.stub, got)
			}
		})
	}
}

// TestDisposition_ReflectionBuiltinsBeatExternal is the regression test for
// issue #95. The external synthesiser used to add Python reflection
// builtins (getattr / setattr / hasattr / delattr / eval / exec / compile /
// __import__) and the JS Function constructor to its stdlib stop-list,
// which caused unresolved CALLS pointing at them to be rewritten to
// "ext:builtins" / "ext:globalThis" and then classified as
// ExternalUnknown — burying the dynamic-dispatch signal under
// external-import noise. After the fix, classifyDispositionLang runs the
// per-language dynamic-pattern catalog BEFORE the "ext:" prefix check, so
// the original reflection-builtin stub wins regardless of how synthesis
// rewrote the resolved ID.
func TestDisposition_ReflectionBuiltinsBeatExternal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		lang         string
		originalStub string
		// resolvedID simulates the post-synthesis state where the
		// synthesiser would (incorrectly) have stamped these builtins
		// with an "ext:<pkg>" prefix.
		resolvedID string
	}{
		{"py_getattr", "python", "getattr", "ext:builtins"},
		{"py_setattr", "python", "setattr", "ext:builtins"},
		{"py_hasattr", "python", "hasattr", "ext:builtins"},
		{"py_delattr", "python", "delattr", "ext:builtins"},
		{"py_eval", "python", "eval", "ext:builtins"},
		{"py_exec", "python", "exec", "ext:builtins"},
		{"py_compile", "python", "compile", "ext:builtins"},
		{"py_dunder_import", "python", "__import__", "ext:builtins"},
		{"py_getattr_call", "python", "getattr(self, name)", "ext:builtins"},
		{"py_eval_call", "python", "eval(src)", "ext:builtins"},
		{"js_eval", "javascript", "eval", "ext:globalThis"},
		{"js_function_ctor", "javascript", "Function", "ext:globalThis"},
		{"js_new_function", "javascript", "new Function(src)", "ext:globalThis"},
	}
	idx := BuildIndex(nil)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			endpoints := []EndpointPair{{
				FromID:     "0000000000000000",
				ToID:       tc.resolvedID,
				ToOriginal: tc.originalStub,
				Language:   tc.lang,
			}}
			stats := idx.ClassifyEndpoints(endpoints, allowDjango)
			if got := stats.DispositionCounts[DispositionDynamic]; got != 1 {
				t.Fatalf("%s: expected 1 dynamic, got counts=%+v",
					tc.originalStub, stats.DispositionCounts)
			}
			if got := stats.DispositionCounts[DispositionExternalUnknown]; got != 0 {
				t.Fatalf("%s: leaked into external-unknown (counts=%+v)",
					tc.originalStub, stats.DispositionCounts)
			}
		})
	}
}

// TestReflectionBuiltins_RecognisedAsDynamic asserts the resolver-side
// invariant for issue #95: every reflection builtin we care about is
// recognised as a dynamic pattern by isDynamicPatternLang for its
// language. The companion synthesiser-side guard
// (TestSynthesize_ReflectionBuiltinsLeftAlone in internal/external)
// covers the stdlibBareNames stop-list directly; this test does not
// inspect that map (it lives in another package and is unexported) —
// it only checks the per-language dynamic catalog (Refs #95).
func TestReflectionBuiltins_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	// We import nothing from the external package here — that's a
	// separate test in internal/external. This test verifies the
	// resolver-side invariant that the per-language catalog catches
	// every reflection builtin on its own.
	reflectionBuiltins := []struct {
		stub string
		lang string
	}{
		{"getattr", "python"},
		{"setattr", "python"},
		{"hasattr", "python"},
		{"delattr", "python"},
		{"eval", "python"},
		{"exec", "python"},
		{"compile", "python"},
		{"__import__", "python"},
		{"eval", "javascript"},
		{"Function", "javascript"},
	}
	for _, b := range reflectionBuiltins {
		if !isDynamicPatternLang(b.stub, b.lang) {
			t.Fatalf("reflection builtin %q (%s) not recognised as dynamic", b.stub, b.lang)
		}
	}
}

// TestReferences_QualifiedNameMatch covers issue #100: a stub equal to an
// entity's QualifiedName (verbatim) must resolve directly. The markdown
// extractor emits Document --CONTAINS--> "<file>::<heading-slug>" edges
// where the ToID is exactly the heading entity's QualifiedName; before the
// fix splitStub split on the first ':' and the lookup landed in the
// bug-extractor bucket.
func TestReferences_QualifiedNameMatch(t *testing.T) {
	heading := types.EntityRecord{
		ID:            "abcdef0123456789",
		Kind:          "SCOPE.Heading",
		Name:          "Installation",
		QualifiedName: "README.md::installation",
		SourceFile:    "README.md",
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "README.md::installation", Kind: "CONTAINS"},
	}
	idx := BuildIndex([]types.EntityRecord{heading})
	stats := References(rels, idx)
	if rels[0].ToID != "abcdef0123456789" {
		t.Fatalf("QName resolution: ToID=%q, want abcdef0123456789", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

// TestReferences_QualifiedNameAmbiguous: two entities sharing a
// QualifiedName should leave the stub unresolved (ambiguous), never silently
// pick one.
func TestReferences_QualifiedNameAmbiguous(t *testing.T) {
	entities := []types.EntityRecord{
		{ID: "1111111111111111", Kind: "SCOPE.Heading", Name: "Foo",
			QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
		{ID: "2222222222222222", Kind: "SCOPE.Heading", Name: "Foo",
			QualifiedName: "doc.md::foo", SourceFile: "doc.md"},
	}
	rels := []types.RelationshipRecord{
		{FromID: "0000000000000000", ToID: "doc.md::foo", Kind: "CONTAINS"},
	}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "doc.md::foo" {
		t.Fatalf("ambiguous QName should preserve stub, got %q", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %d", stats.Ambiguous)
	}
}

func TestBuildLocationIndex(t *testing.T) {
	entities := []types.EntityRecord{
		entAt("aaaaaaaaaaaaaaaa", "Component", "Foo", "a.py"),
		entAt("bbbbbbbbbbbbbbbb", "Component", "Bar", "a.py"),
		// Duplicate (file, name) → dropped.
		entAt("cccccccccccccccc", "Component", "Foo", "a.py"),
		entAt("dddddddddddddddd", "Component", "Foo", "b.py"),
	}
	loc := BuildLocationIndex(entities)
	if loc["a.py"]["Bar"] != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("a.py/Bar: %s", loc["a.py"]["Bar"])
	}
	if _, dup := loc["a.py"]["Foo"]; dup {
		t.Fatalf("a.py/Foo should be dropped as ambiguous; got %s", loc["a.py"]["Foo"])
	}
	if loc["b.py"]["Foo"] != "dddddddddddddddd" {
		t.Fatalf("b.py/Foo: %s", loc["b.py"]["Foo"])
	}
}

// TestRubyRailsDSL_RecognisedAsDynamic locks in issue #107: Rails
// ActionController DSL methods, ActiveRecord query builders, and
// ActiveRecord dynamic finders must be recognised as Dynamic by
// isDynamicPatternLang for lang="ruby". Pre-fix these landed in
// bug-extractor and drove rails-realworld bug-rate to 38.93% and
// sidekiq to 29.83%.
func TestRubyRailsDSL_RecognisedAsDynamic(t *testing.T) {
	t.Parallel()
	// Dynamic catalog (Rails DSL + ActiveRecord query builders +
	// dynamic finders). These all classify Dynamic for ruby source.
	dynamic := []string{
		// ActionController DSL
		"render", "permit", "require", "redirect_to", "respond_to",
		"before_action", "skip_before_action", "after_action",
		"around_action", "helper_method", "params", "session", "flash",
		"cookies", "request", "response",
		// Dynamic finders
		"find_by_email", "find_by_id", "find_by_name!",
		"find_or_create_by_slug", "find_or_create_by_email!",
		// ActiveRecord query DSL
		"order", "where", "joins", "includes", "eager_load", "preload",
		"pluck", "distinct", "group", "having", "limit", "offset",
		"scope", "belongs_to", "has_many", "has_one",
		"has_and_belongs_to_many", "validates", "validate",
		"before_save", "after_save", "before_create", "after_create",
		"before_destroy", "after_destroy",
	}
	for _, stub := range dynamic {
		stub := stub
		t.Run("ruby/"+stub, func(t *testing.T) {
			t.Parallel()
			if !isDynamicPatternLang(stub, "ruby") {
				t.Fatalf("Rails DSL stub %q not recognised as Dynamic for lang=ruby", stub)
			}
		})
	}
}

// TestRubyRailsDSL_GatedToRuby confirms the Rails DSL bare-name patterns
// only match when lang="ruby" — otherwise generic names like `where`,
// `order`, `params`, `request` would shadow user methods in JS / Go /
// Python codebases (lesson from #94 / #105).
func TestRubyRailsDSL_GatedToRuby(t *testing.T) {
	t.Parallel()
	stubs := []string{
		"where", "order", "params", "request", "response", "limit",
		"group", "render", "validates",
	}
	otherLangs := []string{"go", "python", "javascript", "rust", "java", "kotlin"}
	for _, stub := range stubs {
		for _, lang := range otherLangs {
			stub, lang := stub, lang
			t.Run(stub+"/"+lang, func(t *testing.T) {
				t.Parallel()
				if isDynamicPatternLang(stub, lang) {
					t.Fatalf("stub %q matched dynamic for lang=%q; "+
						"must be gated to ruby only", stub, lang)
				}
			})
		}
	}
}

// TestRubyRailsDSL_RejectedGenericCollectionOps locks in the explicit
// rejection list from issue #107: generic collection ops (each / map /
// select / find / count / length / size) MUST NOT be classified
// Dynamic — they're plain Enumerable methods, used everywhere, and
// hiding them would mask real missing-resolution bugs.
func TestRubyRailsDSL_RejectedGenericCollectionOps(t *testing.T) {
	t.Parallel()
	rejected := []string{"each", "map", "select", "find", "count", "length", "size"}
	for _, stub := range rejected {
		stub := stub
		t.Run("ruby/"+stub, func(t *testing.T) {
			t.Parallel()
			if isDynamicPatternLang(stub, "ruby") {
				t.Fatalf("generic collection op %q matched dynamic for ruby; "+
					"must NOT be in catalog (issue #107 rejection list)", stub)
			}
		})
	}
}
