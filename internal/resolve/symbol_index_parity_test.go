// Package resolve — M5 production-parity guard + harness (#4331).
//
// Issue #4331 wired BuildIndexFromModulesOrdered (M5, #2182/#2184) into the
// production index pipeline (cmd/archigraph/index.go) BEHIND A DEFAULT-OFF flag
// (ARCHIGRAPH_RESOLVE_MODULE_INDEX=1). This file is the parity contract for
// that wiring.
//
// # TWO M5 ENTRY POINTS, TWO BEHAVIOURS
//
//   - BuildIndexFromModules (legacy): sorts each module's entities by ID for
//     O(N) collision detection. The platform-variant merge (#1818) in
//     byPackageOperation/byPackageComponent is ORDER-SENSITIVE for 3+ mutually-
//     exclusive variants of one (pkgDir, name), so the sort produces a DIFFERENT
//     PlatformVariants topology than BuildIndex. TestM5_PlatformVariantParity_
//     LegacySortDiverges pins that (it is the original #4331 divergence) and
//     keeps the legacy path out of production.
//
//   - BuildIndexFromModulesOrdered (production-wired): preserves extraction
//     order (no ID sort) and partitions by package directory, so the order-
//     sensitive variant merge sees variants in the same order BuildIndex does.
//     It also populates byNamespaceMember / byKotlinPkgMember / byKotlinPkgFunc,
//     which the original insertModuleEntry omitted. The result is a FULL-Index
//     match with BuildIndex — asserted by indexParityDiff over every index
//     table, including the platform-variant fan-out.
package resolve

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// platformVariantTriple builds three mutually-exclusive GOOS variants of one
// top-level operation "Run" in package dir "svc/". Entity IDs are deliberately
// NOT in source-file order, so the legacy BuildModuleSymbols' sort-by-ID
// reorders them relative to the production extraction order used by `merged`.
func platformVariantTriple() (merged []types.EntityRecord, modules map[ModuleKey][]types.EntityRecord) {
	eDarwin := types.EntityRecord{ID: "cccccccccccccccc", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_darwin.go", Properties: map[string]string{"build_tag": "darwin"}}
	eLinux := types.EntityRecord{ID: "aaaaaaaaaaaaaaaa", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_linux.go", Properties: map[string]string{"build_tag": "linux"}}
	eWindows := types.EntityRecord{ID: "bbbbbbbbbbbbbbbb", Kind: "SCOPE.Operation", Name: "Run",
		SourceFile: "svc/run_windows.go", Properties: map[string]string{"build_tag": "windows"}}

	// Production extraction order (arbitrary, as a real extractor would emit).
	merged = []types.EntityRecord{eWindows, eDarwin, eLinux}
	// All three live in the SAME package dir -> SAME M5 module.
	modules = map[ModuleKey][]types.EntityRecord{"svc": {eWindows, eDarwin, eLinux}}
	return merged, modules
}

func normalizePV(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		cp := append([]string(nil), v...)
		sort.Strings(cp)
		out[k] = cp
	}
	return out
}

// TestM5_PlatformVariantParity_LegacySortDiverges pins the original #4331
// finding: the LEGACY sort-by-ID BuildIndexFromModules produces a DIFFERENT
// PlatformVariants topology than BuildIndex on a 3-way platform-variant
// collision. The legacy path therefore stays out of production; only the
// extraction-order-preserving BuildIndexFromModulesOrdered is wired.
func TestM5_PlatformVariantParity_LegacySortDiverges(t *testing.T) {
	merged, modules := platformVariantTriple()

	flat := BuildIndex(merged)
	mod := BuildIndexFromModules(modules, 0)

	// Canonical winner agrees (chosen by lexicographic SourceFile, order-
	// independent): both pick svc/run_darwin.go's entity.
	if flat.byPackageOperation["svc"]["Run"] != mod.byPackageOperation["svc"]["Run"] {
		t.Errorf("unexpected: byPackageOperation winner diverges (flat=%q mod=%q)",
			flat.byPackageOperation["svc"]["Run"], mod.byPackageOperation["svc"]["Run"])
	}

	flatPV := normalizePV(flat.PlatformVariants)
	modPV := normalizePV(mod.PlatformVariants)
	if reflect.DeepEqual(flatPV, modPV) {
		t.Fatalf("LEGACY divergence is gone — the sort-by-ID path now matches BuildIndex.\n"+
			"flat=%v mod=%v\nIf intended, retire BuildIndexFromModules in favour of the ordered path.",
			flatPV, modPV)
	}
	t.Logf("confirmed legacy sort-by-ID divergence (legacy path stays unwired):\n"+
		"  BuildIndex            PlatformVariants=%v\n  BuildIndexFromModules PlatformVariants=%v",
		flat.PlatformVariants, mod.PlatformVariants)
}

// TestM5_PlatformVariantParity_OrderedMatches is the inverse: the production-
// wired BuildIndexFromModulesOrdered reproduces BuildIndex's PlatformVariants
// topology EXACTLY on the same 3-way collision. If this ever fails, the wired
// M5 path has started cloning a different CALLS edge set — do NOT ship.
func TestM5_PlatformVariantParity_OrderedMatches(t *testing.T) {
	merged, _ := platformVariantTriple()

	flat := BuildIndex(merged)
	mod := BuildIndexFromModulesOrdered(merged, ModuleKeyByPkgDir)

	flatPV := normalizePV(flat.PlatformVariants)
	modPV := normalizePV(mod.PlatformVariants)
	if !reflect.DeepEqual(flatPV, modPV) {
		t.Fatalf("ordered M5 path diverges on PlatformVariants — NOT parity-safe.\n"+
			"BuildIndex=%v\nBuildIndexFromModulesOrdered=%v", flatPV, modPV)
	}
}

// indexParityDiff compares every index table of two Index values and returns a
// slice of human-readable mismatch descriptions (empty == identical). It is the
// core of the both-paths-identical harness: any (from,to,kind) edge difference
// downstream is ultimately rooted in one of these tables, so a full-table match
// guarantees an identical resolved edge set.
//
// PlatformVariants is order-normalised (the fan-out slice is a set, not a
// sequence). All other tables are compared verbatim via reflect.DeepEqual.
func indexParityDiff(a, b Index) []string {
	var diffs []string
	cmp := func(name string, x, y interface{}) {
		if !reflect.DeepEqual(x, y) {
			diffs = append(diffs, name)
		}
	}
	cmp("byKind", a.byKind, b.byKind)
	cmp("ambigKind", a.ambigKind, b.ambigKind)
	cmp("byName", a.byName, b.byName)
	cmp("ambigName", a.ambigName, b.ambigName)
	cmp("nameKinds", a.nameKinds, b.nameKinds)
	cmp("nameKindsReal", a.nameKindsReal, b.nameKindsReal)
	cmp("byLocation", a.byLocation, b.byLocation)
	cmp("ambigLocation", a.ambigLocation, b.ambigLocation)
	cmp("byLocationKind", a.byLocationKind, b.byLocationKind)
	cmp("byLocationKindReal", a.byLocationKindReal, b.byLocationKindReal)
	cmp("byQualifiedName", a.byQualifiedName, b.byQualifiedName)
	cmp("byMember", a.byMember, b.byMember)
	cmp("byPackageMember", a.byPackageMember, b.byPackageMember)
	cmp("byPackageOperation", a.byPackageOperation, b.byPackageOperation)
	cmp("byPackageComponent", a.byPackageComponent, b.byPackageComponent)
	cmp("byNamespaceMember", a.byNamespaceMember, b.byNamespaceMember)
	cmp("byKotlinPkgMember", a.byKotlinPkgMember, b.byKotlinPkgMember)
	cmp("byKotlinPkgFunc", a.byKotlinPkgFunc, b.byKotlinPkgFunc)
	cmp("PlatformVariants", normalizePV(a.PlatformVariants), normalizePV(b.PlatformVariants))
	return diffs
}

// assertFullIndexParity runs BuildIndex and BuildIndexFromModulesOrdered on the
// SAME entity slice and fails on ANY index-table difference. This is the
// both-paths-identical parity harness mandated by #4331.
func assertFullIndexParity(t *testing.T, entities []types.EntityRecord) {
	t.Helper()
	flat := BuildIndex(entities)
	mod := BuildIndexFromModulesOrdered(entities, ModuleKeyByPkgDir)
	if diffs := indexParityDiff(flat, mod); len(diffs) != 0 {
		t.Fatalf("FULL-INDEX PARITY FAILURE — BuildIndexFromModulesOrdered diverges from BuildIndex in tables: %v\n"+
			"flat=%+v\nmod=%+v", diffs, flat, mod)
	}
}

// representativeFixture exercises every index table BuildIndex populates across
// several languages and the order-sensitive edge cases #4331 cares about:
//   - 3-way platform-variant operations AND components in one pkgDir;
//   - dotted-name members (byMember / byPackageMember);
//   - C# namespace members (byNamespaceMember, #4374);
//   - Kotlin package members + top-level funcs (byKotlinPkgMember/Func, #4375);
//   - QualifiedName + Properties["ref"] qname indexing (endpoint/interface);
//   - cross-file same-package operations and name/kind collisions;
//   - entities with no SourceFile.
//
// Entity IDs are deliberately scrambled relative to source order so any
// surviving sort-by-ID bug in the wired path surfaces as a divergence.
func representativeFixture() []types.EntityRecord {
	return []types.EntityRecord{
		// 3-way platform-variant operation in svc/.
		{ID: "0000000000000003", Kind: "SCOPE.Operation", Name: "Run", SourceFile: "svc/run_windows.go",
			Properties: map[string]string{"build_tag": "windows"}},
		{ID: "0000000000000001", Kind: "SCOPE.Operation", Name: "Run", SourceFile: "svc/run_darwin.go",
			Properties: map[string]string{"build_tag": "darwin"}},
		{ID: "0000000000000002", Kind: "SCOPE.Operation", Name: "Run", SourceFile: "svc/run_linux.go",
			Properties: map[string]string{"build_tag": "linux"}},
		// 3-way platform-variant component in svc/.
		{ID: "00000000000000b3", Kind: "SCOPE.Component", Name: "Server", SourceFile: "svc/srv_windows.go",
			Properties: map[string]string{"build_tag": "windows"}},
		{ID: "00000000000000b1", Kind: "SCOPE.Component", Name: "Server", SourceFile: "svc/srv_darwin.go",
			Properties: map[string]string{"build_tag": "darwin"}},
		{ID: "00000000000000b2", Kind: "SCOPE.Component", Name: "Server", SourceFile: "svc/srv_linux.go",
			Properties: map[string]string{"build_tag": "linux"}},

		// Cross-file same-package Go operations (byPackageOperation).
		{ID: "00000000000000c2", Kind: "SCOPE.Operation", Name: "Greet", SourceFile: "pkg/b.go"},
		{ID: "00000000000000c1", Kind: "SCOPE.Operation", Name: "Hello", SourceFile: "pkg/a.go"},

		// Dotted-name member (byMember + byPackageMember).
		{ID: "00000000000000d1", Kind: "SCOPE.Operation", Name: "Mux.handle", SourceFile: "chi/mux.go"},
		{ID: "00000000000000d2", Kind: "SCOPE.Operation", Name: "Mux.serve", SourceFile: "chi/tree.go"},

		// C# namespace member (byNamespaceMember, #4374).
		{ID: "00000000000000e1", Kind: "SCOPE.Operation", Name: "Repo.Save", SourceFile: "cs/Repo.cs",
			Properties: map[string]string{"csharp_namespace": "App.Data"}},

		// Kotlin package member + top-level func (byKotlinPkgMember/Func, #4375).
		{ID: "00000000000000f1", Kind: "SCOPE.Operation", Name: "load", SourceFile: "kt/Loader.kt",
			Properties: map[string]string{"kotlin_package": "app.kt", "kotlin_enclosing_type": "Loader"}},
		{ID: "00000000000000f2", Kind: "SCOPE.Operation", Name: "main", SourceFile: "kt/Main.kt",
			Properties: map[string]string{"kotlin_package": "app.kt"}},

		// QualifiedName + endpoint/interface ref qname indexing.
		{ID: "0000000000000a01", Kind: "SCOPE.Endpoint", Name: "GetUser", SourceFile: "api/users.py",
			QualifiedName: "api.users.GetUser",
			Properties:    map[string]string{"ref": "scope:endpoint:api/users.py#GET:/users"}},
		{ID: "0000000000000a02", Kind: "SCOPE.Component", Name: "Handler", SourceFile: "rs/handler.rs",
			Properties: map[string]string{"ref": "scope:component:interface:rust:Handler"}},

		// Name/kind collision flips byName ambiguous but keeps byKind unique.
		{ID: "0000000000000b01", Kind: "SCOPE.Component", Name: "Config", SourceFile: "x/conf.go"},
		{ID: "0000000000000b02", Kind: "SCOPE.Operation", Name: "Config", SourceFile: "y/conf.go"},

		// Entity with no SourceFile (location indexes skipped).
		{ID: "0000000000000c99", Kind: "SCOPE.Operation", Name: "Floating", SourceFile: ""},
	}
}

// TestM5_OrderedFullIndexParity_Representative is the headline #4331 harness:
// it runs BOTH index-build paths on a representative multi-language fixture and
// asserts EVERY index table is byte-identical. A failure means the wired M5
// path would resolve a different edge set than production — block the merge.
func TestM5_OrderedFullIndexParity_Representative(t *testing.T) {
	assertFullIndexParity(t, representativeFixture())
}

// TestM5_OrderedFullIndexParity_Synthetic reuses the existing scale fixture
// (10 modules × 20 entities) and asserts full-Index parity there too, so the
// large-input shape is covered in addition to the hand-crafted edge cases.
func TestM5_OrderedFullIndexParity_Synthetic(t *testing.T) {
	modules, _ := syntheticModules(10, 20)
	assertFullIndexParity(t, flattenModules(modules))
}

// TestM5_OrderedResolutionParity_Representative is the edge-level companion to
// the table-level harness: it resolves a set of cross-cutting stubs under both
// indexes and fails on any resolved-ToID difference, proving the table parity
// translates to identical downstream edge resolution.
func TestM5_OrderedResolutionParity_Representative(t *testing.T) {
	entities := representativeFixture()
	flat := BuildIndex(entities)
	mod := BuildIndexFromModulesOrdered(entities, ModuleKeyByPkgDir)

	stubs := []string{
		"SCOPE.Operation:Hello",
		"SCOPE.Operation:Greet",
		"SCOPE.Component:Server",
		"SCOPE.Endpoint:GetUser",
		"scope:endpoint:api/users.py#GET:/users",
		"scope:component:interface:rust:Handler",
		"SCOPE.Operation:Config", // ambiguous → both leave unresolved
		"SCOPE.Operation:DoesNotExist",
	}
	for _, kind := range []string{"CALLS", "REFERENCES", "EXTENDS"} {
		mk := func(stub string) []types.RelationshipRecord {
			out := make([]types.RelationshipRecord, len(stubs))
			for i, s := range stubs {
				out[i] = types.RelationshipRecord{FromID: "0000000000000c99", ToID: s, Kind: kind}
			}
			_ = stub
			return out
		}
		rf := mk("")
		rm := mk("")
		References(rf, flat)
		References(rm, mod)
		for i := range rf {
			if rf[i].ToID != rm[i].ToID {
				t.Errorf("kind=%s stub=%q: flat resolved %q, ordered M5 resolved %q",
					kind, stubs[i], rf[i].ToID, rm[i].ToID)
			}
		}
	}
}

// ambiguousMultiCandidateFixture reproduces the #5206 cross-module multi-
// candidate divergence: a name/ref that maps to >1 candidate entity, where the
// candidates live in DIFFERENT modules (pkgDirs) AND the module that owns the
// globally-later candidate is FIRST-SEEN earlier (via an unrelated entity).
// That layout is exactly what makes BuildIndexFromModulesOrdered consume the
// colliding entities in a different order than flat BuildIndex — so the
// first-writer-wins winner (byQualifiedName refProp), and hence the resolved
// IMPLEMENTS / EXTENDS edge, diverges unless the global-position replay forces
// M5 to consume in flat's exact order.
//
// Shape (the OnApplicationShutdown / mysql-psycopg2 family, distilled):
//
//	module b/ is first-seen via `bFirst` (an unrelated entity), THEN two
//	interface-anchor entities in modules a/ and b/ SHARE one structural ref
//	`scope:component:interface:rust:Handler` (first-writer-wins, NO blanking),
//	and two implementer entities in modules c/ and d/ each EXTENDS that ref.
//	Flat keeps the a/ anchor as the ref winner (global order); pre-fix M5
//	grouped b/ first and kept the b/ anchor — so the two EXTENDS edges resolved
//	to a DIFFERENT target ID, diverging the (from,to,kind) multiset.
//
// Entity IDs are scrambled relative to source order so any residual order
// dependence surfaces.
func ambiguousMultiCandidateFixture() []types.EntityRecord {
	const sharedRef = "scope:component:interface:rust:Handler"
	return []types.EntityRecord{
		// module b/ is first-seen here via an unrelated entity — this is what
		// makes the pre-fix module grouping visit b/ before a/.
		{ID: "00000000000052b0", Kind: "SCOPE.Operation", Name: "bFirst",
			SourceFile: "b/early.rs"},

		// a/ anchor — GLOBAL first writer of the shared interface ref.
		{ID: "0000000000005201", Kind: "SCOPE.Component", Name: "Handler",
			SourceFile: "a/handler.rs", Properties: map[string]string{"ref": sharedRef}},
		// b/ anchor — later globally, but b/ is grouped first pre-fix, so it
		// would win first-writer-wins under module-order consumption.
		{ID: "0000000000005202", Kind: "SCOPE.Component", Name: "Handler",
			SourceFile: "b/handler.rs", Properties: map[string]string{"ref": sharedRef}},

		// Two implementers in further modules, each EXTENDS the shared ref.
		{ID: "00000000000052c0", Kind: "SCOPE.Component", Name: "ImplC",
			SourceFile: "c/impl.rs",
			Relationships: []types.RelationshipRecord{
				{FromID: "00000000000052c0", ToID: sharedRef, Kind: "EXTENDS"},
			}},
		{ID: "00000000000052d0", Kind: "SCOPE.Component", Name: "ImplD",
			SourceFile: "d/impl.rs",
			Relationships: []types.RelationshipRecord{
				{FromID: "00000000000052d0", ToID: sharedRef, Kind: "EXTENDS"},
			}},
	}
}

// resolvedEdgeMultiset runs ReferencesEmbedded over a COPY of the fixture and
// returns the sorted (from,to,kind) edge multiset after resolution. Working on
// a deep copy keeps the two index builds from sharing mutated relationship
// slices.
func resolvedEdgeMultiset(entities []types.EntityRecord, idx Index) []string {
	recs := deepCopyEntities(entities)
	ReferencesEmbedded(recs, idx)
	var edges []string
	for k := range recs {
		for _, r := range recs[k].Relationships {
			edges = append(edges, r.FromID+"|"+r.ToID+"|"+r.Kind)
		}
	}
	sort.Strings(edges)
	return edges
}

// deepCopyEntities clones the entity slice including each entity's embedded
// Relationships slice, so in-place rewrites under one index do not leak into
// the other index's resolution pass.
func deepCopyEntities(in []types.EntityRecord) []types.EntityRecord {
	out := make([]types.EntityRecord, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].Relationships != nil {
			out[i].Relationships = append([]types.RelationshipRecord(nil), in[i].Relationships...)
		}
	}
	return out
}

// TestM5_AmbiguousMultiCandidateResolutionParity is the #5206 regression guard
// and the end-to-end gap the unit table-parity harness missed: it resolves an
// ambiguous multi-candidate fixture (same-name IMPLEMENTS target across modules
// + a bare CALL matching two distinct candidates across modules) under BOTH the
// flat and the M5 ordered index, and asserts the resulting (from,to,kind) edge
// multiset is IDENTICAL. Before the global-position replay fix this FAILED
// (M5 picked a different first-writer-wins winner / collapsed the fan-out
// differently because it consumed the colliding entities in module order rather
// than global order); after the fix the two multisets match exactly.
func TestM5_AmbiguousMultiCandidateResolutionParity(t *testing.T) {
	entities := ambiguousMultiCandidateFixture()
	flat := BuildIndex(entities)
	mod := BuildIndexFromModulesOrdered(entities, ModuleKeyByPkgDir)

	// Index tables must be byte-identical first — this is the root-cause layer.
	if diffs := indexParityDiff(flat, mod); len(diffs) != 0 {
		t.Fatalf("#5206 index-table divergence on ambiguous fixture: %v\nflat=%+v\nmod=%+v",
			diffs, flat, mod)
	}

	flatEdges := resolvedEdgeMultiset(entities, flat)
	modEdges := resolvedEdgeMultiset(entities, mod)
	if !reflect.DeepEqual(flatEdges, modEdges) {
		t.Fatalf("#5206 ambiguous-resolution edge multiset diverges:\n flat (%d): %v\n  M5 (%d): %v",
			len(flatEdges), flatEdges, len(modEdges), modEdges)
	}
}
