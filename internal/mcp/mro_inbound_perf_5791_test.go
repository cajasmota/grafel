package mcp

// mro_inbound_perf_5791_test.go — regression coverage for #5791.
//
// buildMROInbound previously re-ran the full EXTENDS/IMPLEMENTS inheritance
// walk (resolveMember → extendsBases BFS → findClassEntity) PER MEMBER entity,
// scanning the whole graph on every callers-direction query and rebuilding on
// every reload epoch. On a ~296k-entity graph that was ~88s p50 for a 9-result
// query.
//
// The fix has two read-path parts, both asserted here by INVARIANT and
// CALL-COUNT (the wall-clock outlier is not locally reproducible):
//
//  1. Correctness golden: the memoized buildMROInbound produces EXACTLY the same
//     reverse-INHERITS edge set as an INDEPENDENT oracle (referenceMROInbound +
//     referenceResolveMember) that runs the pre-fix nested EXTENDS/IMPLEMENTS BFS
//     inline — not via the shared, now-memoized resolveMember. Guarded on
//     single-inheritance, diamond, cycle and multi-base (EXTENDS+IMPLEMENTS)
//     shapes so the flattening-equivalence claim is auto-verified.
//  2. No-rebuild-on-unchanged: two consecutive getMROInbound calls with an
//     unchanged contentHash do NOT rebuild; a genuine content change DOES.
//  3. Complexity witness: base-chain resolution is ~O(classes), not ~O(members) —
//     N members across K classes (N >> K) resolves the chain ~K times, not ~N.

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/frameworks/baseknowledge"
	"github.com/cajasmota/grafel/internal/graph"
)

// inheritanceHierarchyDoc builds a base class + several subclasses, each of
// which inherits (bodyless stubs) many members that resolve in-repo to the base
// method bodies. This exercises the reverse-INHERITS path with N members (>> K
// classes) so the complexity witness is meaningful.
//
// Base defines methods m0..m(memberCount-1) with real bodies. Each subclass
// EXTENDS Base and carries bodyless stubs for every method (which the child
// never redeclares) → every stub resolves provInheritedInRepo to the base body.
func inheritanceHierarchyDoc(subclassCount, memberCount int) *graph.Document {
	doc := &graph.Document{}
	// Base class.
	doc.Entities = append(doc.Entities, graph.Entity{
		ID: "base", Name: "BaseService", QualifiedName: "BaseService",
		Kind: "SCOPE.Component", Subtype: "class", SourceFile: "base.py",
		StartLine: 1, EndLine: 1, Language: "python",
	})
	methodName := func(i int) string { return "m" + itoa(i) }
	for i := 0; i < memberCount; i++ {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:   "base_" + methodName(i),
			Name: "BaseService." + methodName(i), QualifiedName: "BaseService." + methodName(i),
			Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.py",
			StartLine: 2 + i, EndLine: 2 + i, Language: "python",
		})
	}
	for c := 0; c < subclassCount; c++ {
		cls := "Child" + itoa(c)
		clsID := "child_" + itoa(c)
		doc.Entities = append(doc.Entities, graph.Entity{
			ID: clsID, Name: cls, QualifiedName: cls,
			Kind: "SCOPE.Component", Subtype: "class", SourceFile: cls + ".py",
			StartLine: 1, EndLine: 1, Language: "python",
		})
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID: "ext_" + clsID, FromID: clsID, ToID: "base", Kind: "EXTENDS",
		}.WithProperties(map[string]string{"language": "python", "base_name": "BaseService"}))
		for i := 0; i < memberCount; i++ {
			// Bodyless inherited stub: no source span, so resolveMember walks
			// EXTENDS to the base body.
			doc.Entities = append(doc.Entities, graph.Entity{
				ID:   clsID + "_" + methodName(i),
				Name: cls + "." + methodName(i), QualifiedName: cls + "." + methodName(i),
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: cls + ".py",
				StartLine: 0, EndLine: 0, Language: "python",
				Signature: "def " + methodName(i) + "(self)",
			})
		}
	}
	return doc
}

// referenceResolveMember is a VERBATIM copy of the PRE-FIX resolveMember: it
// runs the original nested EXTENDS/IMPLEMENTS BFS inline (visited set + frontier
// levels, first-match returns), WITHOUT the memoized baseChain. It is the
// independent oracle the memoized resolveMember must match — comparing against
// mroOutboundEdges/resolveMember would be circular (both now share baseChain).
// Kept byte-for-byte faithful to the loop this PR replaced so the golden truly
// guards the flattening-equivalence claim on diamond/cycle/multi-base shapes.
func referenceResolveMember(lr *LoadedRepo, e *graph.Entity) memberResolution {
	if lr == nil || lr.Doc == nil || e == nil {
		return memberResolution{Provenance: provUnresolved, Note: "no repo/entity"}
	}
	if res, ok := resolveInheritedEndpoint(lr, e); ok {
		return res
	}
	if isDefinitionEntity(e) {
		return memberResolution{Provenance: provExplicit}
	}
	member, owningName, isBodyless := classifyMember(e)
	if member == "" || owningName == "" {
		return memberResolution{Provenance: provExplicit}
	}
	if !isBodyless && hasRealBody(e) {
		return memberResolution{Provenance: provExplicit, Member: member, OwningClass: owningName}
	}
	owning := findClassEntity(lr, owningName)
	if owning == nil {
		return memberResolution{
			Provenance:  provUnresolved,
			Member:      member,
			OwningClass: owningName,
			Note:        "owning class entity not found in index",
		}
	}
	reg := baseknowledge.Default()
	// --- ORIGINAL nested BFS (the code this PR replaced with baseChain) ---
	visited := map[string]bool{owning.ID: true}
	frontier := extendsBases(lr, owning)
	var unknownBases []string
	for len(frontier) > 0 {
		var next []baseRef
		for _, b := range frontier {
			if b.entity != nil {
				if def := classDeclaredMember(lr, b.entity, member); def != nil {
					return memberResolution{
						Provenance:     provInheritedInRepo,
						Member:         member,
						OwningClass:    owningName,
						DefiningClass:  b.name,
						DefiningEntity: def,
					}
				}
			}
			if m, ok := reg.Member(b.name, member); ok {
				contract := m
				defining := contract.DefiningClass
				if defining == "" {
					defining = canonicalBaseFQN(reg, b.name)
				}
				return memberResolution{
					Provenance:    provInheritedExternal,
					Member:        member,
					OwningClass:   owningName,
					DefiningClass: defining,
					Contract:      &contract,
				}
			}
			if _, known := reg.Lookup(b.name); !known && b.entity == nil {
				unknownBases = append(unknownBases, b.name)
			}
			if b.entity != nil && !visited[b.entity.ID] {
				visited[b.entity.ID] = true
				next = append(next, extendsBases(lr, b.entity)...)
			}
		}
		frontier = next
	}
	note := "no defining class found via EXTENDS or knowledge pack"
	if len(unknownBases) > 0 {
		sort.Strings(unknownBases)
		note = "unrecognised base(s): " + strings.Join(dedupe(unknownBases), ", ")
	}
	return memberResolution{
		Provenance:  provUnresolved,
		Member:      member,
		OwningClass: owningName,
		Note:        note,
	}
}

// referenceMROInbound re-implements the PRE-FIX buildMROInbound exactly:
// iterate every member entity and resolve it via the NESTED-BFS
// referenceResolveMember (not the memoized path), applying the same
// mroOutboundEdges gating (skip entities that already carry a real INHERITS
// edge; skip external targets). This is the golden the memoized implementation
// must match byte-for-byte.
func referenceMROInbound(lr *LoadedRepo) map[string][]string {
	out := map[string][]string{}
	if lr == nil || lr.Doc == nil {
		return out
	}
	for i := range lr.Doc.Entities {
		e := &lr.Doc.Entities[i]
		if !isMemberEntity(e) {
			continue
		}
		// Mirror mroOutboundEdges: a stub that already carries a materialised
		// INHERITS edge is not double-projected.
		if adj := lr.getAdjacency(); adj != nil {
			skip := false
			for _, ed := range adj.Outgoing(e.ID) {
				if ed.kind == inheritsEdgeKind {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		res := referenceResolveMember(lr, e)
		if res.Provenance == provInheritedInRepo && res.DefiningEntity != nil && res.DefiningEntity.ID != e.ID {
			out[res.DefiningEntity.ID] = append(out[res.DefiningEntity.ID], e.ID)
		}
	}
	return out
}

func newLoadedRepo(doc *graph.Document) *LoadedRepo {
	return &LoadedRepo{Repo: "repo1", Doc: doc, LabelIndex: BuildLabelIndex(doc)}
}

func normalizeInbound(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		cp := append([]string(nil), v...)
		sort.Strings(cp)
		out[k] = cp
	}
	return out
}

// TestBuildMROInbound_MatchesReferenceGolden — correctness golden (#5791 part 1).
// The memoized buildMROInbound must produce EXACTLY the same reverse-INHERITS
// edge set as the pre-fix per-member resolver.
func TestBuildMROInbound_MatchesReferenceGolden(t *testing.T) {
	doc := inheritanceHierarchyDoc(4, 5)

	// Reference (golden) from a fresh repo so its resolver state is independent
	// of the memoized repo's caches.
	golden := referenceMROInbound(newLoadedRepo(doc))

	got := buildMROInbound(newLoadedRepo(doc))

	if len(golden) == 0 {
		t.Fatalf("golden reverse-INHERITS map is empty — fixture does not exercise the path")
	}
	if !reflect.DeepEqual(normalizeInbound(got), normalizeInbound(golden)) {
		t.Fatalf("memoized buildMROInbound differs from reference golden.\n got=%v\n want=%v", got, golden)
	}
	// Sanity: base methods (defining members) must key the map; each with one
	// stub per subclass.
	for i := 0; i < 5; i++ {
		id := "base_m" + itoa(i)
		if len(normalizeInbound(got)[id]) != 4 {
			t.Errorf("expected 4 inheriting stubs for %s, got %v", id, got[id])
		}
	}
}

// TestBuildMROInbound_MatchesReference_CrossFileVariants covers the existing
// in-repo fixture too, guarding equivalence on a differently-shaped graph.
func TestBuildMROInbound_MatchesReference_CrossFileVariants(t *testing.T) {
	doc := inRepoBaseCallDoc()
	golden := referenceMROInbound(newLoadedRepo(doc))
	got := buildMROInbound(newLoadedRepo(doc))
	if !reflect.DeepEqual(normalizeInbound(got), normalizeInbound(golden)) {
		t.Fatalf("inRepoBaseCallDoc: memoized differs from reference.\n got=%v\n want=%v", got, golden)
	}
}

// --- non-trivial hierarchy fixtures (reviewer follow-up) --------------------
//
// These exercise the graph shapes the flattening-equivalence was verified on by
// hand — diamond, cycle, and a superclass+interface multi-base — so the golden
// auto-guards them. Each defines base member bodies plus bodyless stubs that
// resolve back up the hierarchy, producing reverse-INHERITS edges.

// diamondDoc: A extends {B, C}; B extends D; C extends D. D defines foo()/bar()
// with bodies. A, B, C each carry bodyless foo/bar stubs. D is reached from A
// via TWO paths (A->B->D and A->C->D) — the visited guard must expand D once and
// the flattened chain must preserve the exact BFS order.
func diamondDoc() *graph.Document {
	cls := func(id, name, file string) graph.Entity {
		return graph.Entity{ID: id, Name: name, QualifiedName: name,
			Kind: "SCOPE.Component", Subtype: "class", SourceFile: file,
			StartLine: 1, EndLine: 1, Language: "python"}
	}
	body := func(id, qn, file string, line int) graph.Entity {
		return graph.Entity{ID: id, Name: qn, QualifiedName: qn,
			Kind: "SCOPE.Operation", Subtype: "method", SourceFile: file,
			StartLine: line, EndLine: line, Language: "python"}
	}
	stub := func(id, qn, file, method string) graph.Entity {
		return graph.Entity{ID: id, Name: qn, QualifiedName: qn,
			Kind: "SCOPE.Operation", Subtype: "method", SourceFile: file,
			StartLine: 0, EndLine: 0, Language: "python",
			Signature: "def " + method + "(self)"}
	}
	ext := func(from, to, base string) graph.Relationship {
		return graph.Relationship{ID: "ext_" + from + "_" + to, FromID: from, ToID: to, Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": base})
	}
	return &graph.Document{
		Entities: []graph.Entity{
			cls("d", "D", "d.py"),
			body("d_foo", "D.foo", "d.py", 2),
			body("d_bar", "D.bar", "d.py", 3),
			cls("b", "B", "b.py"),
			stub("b_foo", "B.foo", "b.py", "foo"),
			stub("b_bar", "B.bar", "b.py", "bar"),
			cls("c", "C", "c.py"),
			stub("c_foo", "C.foo", "c.py", "foo"),
			stub("c_bar", "C.bar", "c.py", "bar"),
			cls("a", "A", "a.py"),
			stub("a_foo", "A.foo", "a.py", "foo"),
			stub("a_bar", "A.bar", "a.py", "bar"),
		},
		Relationships: []graph.Relationship{
			ext("b", "d", "D"),
			ext("c", "d", "D"),
			ext("a", "b", "B"),
			ext("a", "c", "C"),
		},
	}
}

// cycleDoc: A extends B extends A (a pathological self-referential cycle). A
// defines foo() with a body; B carries a bodyless foo stub. The BFS must
// guard-terminate (visited seed) rather than loop forever, and B.foo must
// resolve to A.foo.
func cycleDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "a", Name: "A", QualifiedName: "A", Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "a.py", StartLine: 1, EndLine: 1, Language: "python"},
			{ID: "a_foo", Name: "A.foo", QualifiedName: "A.foo", Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "a.py", StartLine: 2, EndLine: 2, Language: "python"},
			{ID: "b", Name: "B", QualifiedName: "B", Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "b.py", StartLine: 1, EndLine: 1, Language: "python"},
			{ID: "b_foo", Name: "B.foo", QualifiedName: "B.foo", Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "b.py", StartLine: 0, EndLine: 0, Language: "python", Signature: "def foo(self)"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "ext_a_b", FromID: "a", ToID: "b", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "B"}),
			graph.Relationship{ID: "ext_b_a", FromID: "b", ToID: "a", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "A"}),
		},
	}
}

// multiBaseDoc: X EXTENDS Super and IMPLEMENTS Iface. Super defines foo() with a
// body; Iface declares bar() with a real (default-method) body. X carries
// bodyless foo/bar stubs. Exercises the mixed EXTENDS+IMPLEMENTS enumeration in
// declared edge order.
func multiBaseDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "super", Name: "Super", QualifiedName: "Super", Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "super.py", StartLine: 1, EndLine: 1, Language: "python"},
			{ID: "super_foo", Name: "Super.foo", QualifiedName: "Super.foo", Kind: "SCOPE.Operation",
				Subtype: "method", SourceFile: "super.py", StartLine: 2, EndLine: 2, Language: "python"},
			{ID: "iface", Name: "Iface", QualifiedName: "Iface", Kind: "SCOPE.Component", Subtype: "interface",
				SourceFile: "iface.py", StartLine: 1, EndLine: 1, Language: "python"},
			{ID: "iface_bar", Name: "Iface.bar", QualifiedName: "Iface.bar", Kind: "SCOPE.Operation",
				Subtype: "method", SourceFile: "iface.py", StartLine: 2, EndLine: 3, Language: "python"},
			{ID: "x", Name: "X", QualifiedName: "X", Kind: "SCOPE.Component", Subtype: "class",
				SourceFile: "x.py", StartLine: 1, EndLine: 1, Language: "python"},
			{ID: "x_foo", Name: "X.foo", QualifiedName: "X.foo", Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "x.py", StartLine: 0, EndLine: 0, Language: "python", Signature: "def foo(self)"},
			{ID: "x_bar", Name: "X.bar", QualifiedName: "X.bar", Kind: "SCOPE.Operation", Subtype: "method",
				SourceFile: "x.py", StartLine: 0, EndLine: 0, Language: "python", Signature: "def bar(self)"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "ext_x_super", FromID: "x", ToID: "super", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "Super"}),
			graph.Relationship{ID: "impl_x_iface", FromID: "x", ToID: "iface", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "python", "base_name": "Iface"}),
		},
	}
}

// TestBuildMROInbound_EquivalentOnNonTrivialHierarchies — reviewer follow-up.
// The flattening equivalence (memoized baseChain == original nested BFS) is
// auto-guarded on diamond / cycle / multi-base shapes by comparing the memoized
// buildMROInbound against the independent nested-BFS referenceMROInbound oracle.
func TestBuildMROInbound_EquivalentOnNonTrivialHierarchies(t *testing.T) {
	cases := []struct {
		name string
		doc  *graph.Document
	}{
		{"diamond", diamondDoc()},
		{"cycle", cycleDoc()},
		{"multi_base_extends_implements", multiBaseDoc()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := referenceMROInbound(newLoadedRepo(tc.doc))
			got := buildMROInbound(newLoadedRepo(tc.doc))
			if len(golden) == 0 {
				t.Fatalf("%s: golden reverse-INHERITS map is empty — fixture does not exercise the path", tc.name)
			}
			if !reflect.DeepEqual(normalizeInbound(got), normalizeInbound(golden)) {
				t.Fatalf("%s: memoized buildMROInbound differs from nested-BFS reference.\n got=%v\n want=%v",
					tc.name, normalizeInbound(got), normalizeInbound(golden))
			}
		})
	}
}

// TestBuildMROInbound_DiamondResolvesSharedBase pins the concrete diamond
// expectation: D's members are each inherited by A, B and C (D reached from A
// via both B and C, deduped to a single resolution).
func TestBuildMROInbound_DiamondResolvesSharedBase(t *testing.T) {
	got := normalizeInbound(buildMROInbound(newLoadedRepo(diamondDoc())))
	for _, base := range []string{"d_foo", "d_bar"} {
		if !reflect.DeepEqual(got[base], []string{"a_" + strings.TrimPrefix(base, "d_"), "b_" + strings.TrimPrefix(base, "d_"), "c_" + strings.TrimPrefix(base, "d_")}) {
			t.Errorf("expected %s inherited by A/B/C stubs, got %v", base, got[base])
		}
	}
}

// TestBuildMROInbound_CycleTerminates asserts the A<->B cycle does not hang and
// resolves B.foo -> A.foo.
func TestBuildMROInbound_CycleTerminates(t *testing.T) {
	got := normalizeInbound(buildMROInbound(newLoadedRepo(cycleDoc())))
	if !reflect.DeepEqual(got["a_foo"], []string{"b_foo"}) {
		t.Fatalf("expected cycle B.foo -> A.foo, got %v", got["a_foo"])
	}
}

// TestGetMROInbound_NoRebuildOnUnchangedContentHash — #5791 part 2.
// Two consecutive getMROInbound calls with an unchanged contentHash must reuse
// the cache (one build). A genuine content change must rebuild.
func TestGetMROInbound_NoRebuildOnUnchangedContentHash(t *testing.T) {
	doc := inheritanceHierarchyDoc(3, 3)
	lr := newLoadedRepo(doc)
	lr.contentHash = 0xAAAA

	start := mroBuildCount.Load()
	first := lr.getMROInbound()
	if got := mroBuildCount.Load() - start; got != 1 {
		t.Fatalf("first getMROInbound should build exactly once, built %d times", got)
	}
	second := lr.getMROInbound()
	if got := mroBuildCount.Load() - start; got != 1 {
		t.Fatalf("second getMROInbound with unchanged contentHash must NOT rebuild; total builds=%d", got)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached map differs across calls: %v vs %v", first, second)
	}

	// A genuine content change (new contentHash) MUST rebuild.
	lr.contentHash = 0xBBBB
	_ = lr.getMROInbound()
	if got := mroBuildCount.Load() - start; got != 2 {
		t.Fatalf("changed contentHash must trigger exactly one rebuild; total builds=%d (want 2)", got)
	}
}

// TestBaseChain_ResolvedPerClassNotPerMember — #5791 part 3 (complexity witness).
// With N members across K classes (N >> K), the base-chain resolver must run
// ~O(K) times, not ~O(N). Pre-fix the walk ran once per member.
func TestBaseChain_ResolvedPerClassNotPerMember(t *testing.T) {
	const subclasses, members = 5, 20 // 5 subclasses × 20 members = 100 stubs
	doc := inheritanceHierarchyDoc(subclasses, members)
	lr := newLoadedRepo(doc)
	lr.contentHash = 0xCAFE

	start := baseChainComputeCount.Load()
	_ = buildMROInbound(lr)
	chainComputes := baseChainComputeCount.Load() - start

	// Owning classes whose chain gets resolved: the base (its members are
	// explicit, no chain) is not walked; the 5 subclasses each resolve their
	// chain once → ~5, and must be far below the 100 member stubs.
	memberStubs := int64(subclasses * members)
	if chainComputes > int64(subclasses)+1 {
		t.Fatalf("base-chain resolved %d times for %d subclasses — expected ~O(classes) not O(members=%d)",
			chainComputes, subclasses, memberStubs)
	}
	if chainComputes >= memberStubs {
		t.Fatalf("base-chain resolved %d times (>= %d member stubs) — memoization not effective",
			chainComputes, memberStubs)
	}
}
