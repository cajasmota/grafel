package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Issue #7071 — twelve resolver tiers bind a bare name from lexical
// proximity alone, and until this change the edge they produced was
// byte-identical to one bound on a qualified name the extractor actually
// resolved. Nothing on the edge, and no counter anywhere, said which.
//
// Every test below asserts the MARKER AT THE DECIDING TIER: the edge
// carries types.PropBindTier with the specific tier's own value, and
// Stats.BindTierCounts agrees. Asserting merely "some marker is present"
// would pass for all twelve with a single stamp at the funnel, which is
// precisely the wrong fix — it would mark exact and structural binds too,
// and it would miss the four per-shape tiers that never reach a funnel.
//
// The negative controls are as load-bearing as the positives: an evidence
// tier that starts carrying the marker is the same defect in the other
// direction, and only a forbidden assertion can catch a too-broad
// predicate.

// tierFixture is one end-to-end case: a set of entity records carrying an
// embedded relationship, the (record, rel) coordinates of the edge under
// test, and what the resolver should say about how it bound.
type tierFixture struct {
	name string
	// records is the whole graph. BuildIndex sees all of it.
	records []types.EntityRecord
	// relAt locates the edge under test as records[relAt[0]].Relationships[relAt[1]].
	relAt [2]int
	// wantTier is the tier value expected on the edge. "" means the edge
	// must carry NO marker — the negative controls.
	wantTier BindTier
	// wantToID is the entity ID the edge must bind to. A tier assertion on
	// an edge that bound to the wrong thing (or to nothing) grades nothing,
	// so every case pins the target by content too.
	wantToID string
}

// runTierFixture resolves f through the real embedded-reference pass and
// returns the edge under test.
func runTierFixture(t *testing.T, f tierFixture) (*types.RelationshipRecord, Stats) {
	t.Helper()
	recs := make([]types.EntityRecord, len(f.records))
	copy(recs, f.records)
	idx := BuildIndex(recs)
	stats := ReferencesEmbeddedWithAllowlist(recs, idx, nil)
	return &recs[f.relAt[0]].Relationships[f.relAt[1]], stats
}

func checkTierFixture(t *testing.T, f tierFixture) {
	t.Helper()
	r, stats := runTierFixture(t, f)
	if r.ToID != f.wantToID {
		t.Fatalf("%s: edge bound to %q, want %q — the tier assertion below "+
			"would grade nothing on a differently-bound edge", f.name, r.ToID, f.wantToID)
	}
	got := BindTier(r.Properties.Get(types.PropBindTier))
	if got != f.wantTier {
		t.Fatalf("%s: %s = %q, want %q", f.name, types.PropBindTier, got, f.wantTier)
	}
	if f.wantTier == "" {
		if n := len(stats.BindTierCounts); n != 0 {
			t.Fatalf("%s: expected no guess tier to fire, got counts %v", f.name, stats.BindTierCounts)
		}
		return
	}
	if stats.BindTierCounts[f.wantTier] != 1 {
		t.Fatalf("%s: Stats.BindTierCounts[%s] = %d, want 1 (counts: %v)",
			f.name, f.wantTier, stats.BindTierCounts[f.wantTier], stats.BindTierCounts)
	}
}

func op(id, name, file string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: "Operation", Name: name, SourceFile: file, Language: "go"}
}

func comp(id, name, file string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: "Component", Name: name, SourceFile: file, Language: "go"}
}

// callerRec is an Operation carrying one embedded edge of the given kind
// whose ToID is the raw stub under test.
func callerRec(id, name, file, kind, stub string, props types.Props) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: "Operation", Name: name, SourceFile: file, Language: "go",
		Relationships: []types.RelationshipRecord{{
			FromID: id, ToID: stub, Kind: kind, Properties: props,
		}},
	}
}

// ---------------------------------------------------------------------------
// The twelve guess tiers.
//
// VARIED across the table: the tier each row is designed to reach, and
// exactly the graph shape that makes the earlier tiers decline — how many
// same-named entities exist, which files and directories they live in,
// whether their Name is bare or dotted, their Kind, the edge Kind, and the
// edge properties.
//
// HELD CONSTANT across the table: the caller is always an Operation
// carrying exactly ONE embedded edge whose ToID is a raw bare stub;
// Language is "go" on every record (varied only where a tier is
// language-gated, and then stated in the row); the edge is resolved through
// ReferencesEmbeddedWithAllowlist with a nil allowlist; the assertion is
// always (bound target, tier value, counter) as a triple.
//
// NOT VARIED, and therefore NOT GRADED by this table: the FROM endpoint is
// always an already-resolved hex-shaped ID, so no row here exercises a FROM
// rewrite. That is deliberate and matches the scope of the marker, which is
// named to_bind_tier and describes the TO endpoint only. See
// TestBindTier_FromEndpointGuessIsNotMarked_7071 below, which pins the gap
// rather than leaving it silent.
// ---------------------------------------------------------------------------

func TestBindTier_EveryGuessTierIsMarked_7071(t *testing.T) {
	cases := []tierFixture{
		{
			// G1: "Target" is unique graph-wide, so byName hits before any
			// locality tier is consulted. Caller deliberately lives in a
			// THIRD directory so no locality tier could have produced it.
			name:     "global-name: unique bare name anywhere in the graph",
			records:  []types.EntityRecord{op("aaaaaaaaaaaaaaa1", "Target", "pkg/a/target.go"), callerRec("aaaaaaaaaaaaaaa2", "Caller", "pkg/z/caller.go", "CALLS", "Target", nil)},
			relAt:    [2]int{1, 0},
			wantTier: BindTierGlobalName,
			wantToID: "aaaaaaaaaaaaaaa1",
		},
		{
			// G2: two entities named "Target" make byName ambiguous; only
			// one is in the CALLS (operation) family, so the kind hint
			// narrows to it. VARIES from G1 only in the presence of the
			// second, differently-kinded "Target".
			name: "global-kind-family: ambiguous bare name, unique within the relKind's family",
			records: []types.EntityRecord{
				op("bbbbbbbbbbbbbbb1", "Target", "pkg/a/target.go"),
				comp("bbbbbbbbbbbbbbb2", "Target", "pkg/b/target.go"),
				callerRec("bbbbbbbbbbbbbbb3", "Caller", "pkg/z/caller.go", "CALLS", "Target", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierGlobalKindFamily,
			wantToID: "bbbbbbbbbbbbbbb1",
		},
		{
			// L1: two OPERATIONS named "Target" — same kind family, so the
			// G2 hint cannot narrow either. One of them shares the caller's
			// FILE. VARIES from G2 in the second Target's kind and file.
			name: "file-kind: same-file real entity, kind-filtered",
			records: []types.EntityRecord{
				op("ccccccccccccccc1", "Target", "pkg/a/caller.go"),
				op("ccccccccccccccc2", "Target", "pkg/b/other.go"),
				callerRec("ccccccccccccccc3", "Caller", "pkg/a/caller.go", "CALLS", "Target", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierFileKind,
			wantToID: "ccccccccccccccc1",
		},
		{
			// L2: same as L1 except the local Target moved to a SIBLING
			// FILE in the caller's directory, so the same-file tier declines
			// and byPackageOperation answers. The single varied axis
			// against L1 is the target's file.
			name: "package-operation: same directory, different file",
			records: []types.EntityRecord{
				op("ddddddddddddddd1", "Target", "pkg/a/target.go"),
				op("ddddddddddddddd2", "Target", "pkg/b/other.go"),
				callerRec("ddddddddddddddd3", "Caller", "pkg/a/caller.go", "CALLS", "Target", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierPackageOperation,
			wantToID: "ddddddddddddddd1",
		},
		{
			// L3: the target's Name is DOTTED ("Svc.merge"), so nothing
			// indexes it under the bare stub "merge" — byName misses
			// entirely (statusUnmatched, the #778 path) and the same-file
			// leaf-name suffix scan answers. This is the tier #7071's
			// end-to-end C# reproduction runs through.
			name: "file-leaf-name: same-file dotted member matched by its leaf",
			records: []types.EntityRecord{
				op("eeeeeeeeeeeeeee1", "Svc.merge", "pkg/a/caller.go"),
				callerRec("eeeeeeeeeeeeeee2", "Caller.run", "pkg/a/caller.go", "CALLS", "merge", nil),
			},
			relAt:    [2]int{1, 0},
			wantTier: BindTierFileLeafName,
			wantToID: "eeeeeeeeeeeeeee1",
		},
		{
			// L4: L3 with the dotted member moved to a sibling file in the
			// same directory. Single varied axis against L3: the file.
			name: "package-leaf-name: same-directory dotted member matched by its leaf",
			records: []types.EntityRecord{
				op("fffffffffffffff1", "Svc.merge", "pkg/a/svc.go"),
				callerRec("fffffffffffffff2", "Caller.run", "pkg/a/caller.go", "CALLS", "merge", nil),
			},
			relAt:    [2]int{1, 0},
			wantTier: BindTierPackageLeafName,
			wantToID: "fffffffffffffff1",
		},
		{
			// L5: EXTENDS, not CALLS — the byPackageComponent arm of
			// lookupBareWithLocality. Two components named "Base" make
			// byName ambiguous; the one in the caller's directory (but not
			// its file) wins. VARIES from L2 in the edge kind and the
			// target's entity kind together, which is what selects this
			// switch arm — AND in Language, which is java rather than go.
			//
			// The language is load-bearing and was found by measurement,
			// not by reading: with Language "go" this exact fixture binds
			// through E3 (go-package-component) instead, because the Go
			// component tier is tried BEFORE the funnel that reaches the
			// locality tiers. The two tiers consult different indexes
			// (byPackageComponent via lookupPackageComponent vs. via the
			// locality switch) and are genuinely distinct sites; the
			// language is what tells them apart.
			name: "package-component: same directory component on EXTENDS (non-go)",
			records: []types.EntityRecord{
				{ID: "ggggggggggggggg1", Kind: "Component", Name: "Base", SourceFile: "pkg/a/Base.java", Language: "java"},
				{ID: "ggggggggggggggg2", Kind: "Component", Name: "Base", SourceFile: "pkg/b/Base.java", Language: "java"},
				{ID: "ggggggggggggggg3", Kind: "Operation", Name: "Child", SourceFile: "pkg/a/Child.java", Language: "java",
					Relationships: []types.RelationshipRecord{{FromID: "ggggggggggggggg3", ToID: "Base", Kind: "EXTENDS"}}},
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierPackageComponent,
			wantToID: "ggggggggggggggg1",
		},
		{
			// L6: the only same-file thing wearing the name is a SCOPE.*
			// PLACEHOLDER — not a declaration at all. Two of them make the
			// global name ambiguous; the real-entity tier skips
			// scope-prefixed kinds, so the last-resort placeholder probe
			// answers. VARIES from L1 in the target entities' Kind only.
			name: "file-scope-placeholder: same-file SCOPE.Component placeholder",
			records: []types.EntityRecord{
				{ID: "hhhhhhhhhhhhhhh1", Kind: "SCOPE.Component", Name: "navigate", SourceFile: "pkg/a/caller.go", Language: "ts"},
				{ID: "hhhhhhhhhhhhhhh2", Kind: "SCOPE.Component", Name: "navigate", SourceFile: "pkg/b/other.go", Language: "ts"},
				callerRec("hhhhhhhhhhhhhhh3", "Caller", "pkg/a/caller.go", "CALLS", "navigate", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierFileScopePlaceholder,
			wantToID: "hhhhhhhhhhhhhhh1",
		},
		{
			// L6, the OTHER probe. The tier makes two probes in a fixed
			// order — SCOPE.Operation first, then SCOPE.Component — and
			// they are separate lines in the source. Scoring only one of
			// them would grade one line and leave its neighbour open, so
			// this row varies the placeholder KIND and nothing else.
			//
			// They are NOT observationally identical: they bind different
			// entities, so a mutant on either is distinguishable. They do
			// share one tier VALUE, deliberately — see the note on
			// BindTierFileScopePlaceholder.
			name: "file-scope-placeholder: same-file SCOPE.Operation placeholder",
			records: []types.EntityRecord{
				{ID: "hhhhhhhhhhhhhhh4", Kind: "SCOPE.Operation", Name: "navigate", SourceFile: "pkg/a/caller.go", Language: "ts"},
				{ID: "hhhhhhhhhhhhhhh5", Kind: "SCOPE.Operation", Name: "navigate", SourceFile: "pkg/a/other.go", Language: "ts"},
				callerRec("hhhhhhhhhhhhhhh6", "Caller", "pkg/a/caller.go", "CALLS", "navigate", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierFileScopePlaceholder,
			wantToID: "hhhhhhhhhhhhhhh4",
		},
		{
			// E3: Go DEPENDS_ON to a bare type name unique in the parent's
			// package directory. Two "Server" components make byName
			// ambiguous so the tier is reached, and the caller's own package
			// picks one. Language IS load-bearing here (the tier is gated to
			// go) and is stated rather than inherited silently.
			name: "go-package-component: bare receiver type unique in the package dir",
			records: []types.EntityRecord{
				comp("iiiiiiiiiiiiiii1", "Server", "pkg/a/server.go"),
				comp("iiiiiiiiiiiiiii2", "Server", "pkg/b/server.go"),
				callerRec("iiiiiiiiiiiiiii3", "Handler", "pkg/a/handler.go", "DEPENDS_ON", "Server", nil),
			},
			relAt:    [2]int{2, 0},
			wantTier: BindTierGoPackageComponent,
			wantToID: "iiiiiiiiiiiiiii1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkTierFixture(t, c) })
	}
}

// ---------------------------------------------------------------------------
// The three per-shape tiers that need more than one entity + one caller to
// reach. Kept out of the table above because each needs a different graph
// shape (an IMPLEMENTS harvest, a package-level collision sentinel, a
// separate resolver pass), and folding them in would have meant a table
// whose rows share no held-constant axes at all.
//
// VARIED across these three: which per-shape tier fires.
// HELD CONSTANT: each still asserts the same triple — bound target, tier
// value, counter — through checkTierFixture or its explicit equivalent.
// ---------------------------------------------------------------------------

// E1. `interface_dispatch_type` names an interface with exactly ONE known
// implementer that has a member by this name. The property is real
// evidence about the CALL; "exactly one implementer in the graph" is an
// inference about the CORPUS, and that is the half that makes it a guess.
func TestBindTier_GoInterfaceDispatchIsMarked_7071(t *testing.T) {
	recs := []types.EntityRecord{
		comp("nnnnnnnnnnnnnnn1", "Store", "pkg/s/store.go"),
		{ID: "nnnnnnnnnnnnnnn2", Kind: "Component", Name: "MemStore", SourceFile: "pkg/m/mem.go", Language: "go",
			Relationships: []types.RelationshipRecord{{FromID: "nnnnnnnnnnnnnnn2", ToID: "Store", Kind: "IMPLEMENTS"}}},
		op("nnnnnnnnnnnnnnn3", "MemStore.Get", "pkg/m/mem.go"),
		callerRec("nnnnnnnnnnnnnnn4", "Handler.serve", "pkg/c/handler.go", "CALLS", "Get",
			types.Props{{K: "interface_dispatch_type", V: "Store"}}),
	}
	idx := BuildIndex(recs)
	stats := ReferencesEmbeddedWithAllowlist(recs, idx, nil)
	r := recs[3].Relationships[0]
	if r.ToID != "nnnnnnnnnnnnnnn3" {
		t.Fatalf("interface-dispatch edge bound to %q, want the sole implementer's member", r.ToID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != string(BindTierGoInterfaceDispatch) {
		t.Fatalf("%s = %q, want %q", types.PropBindTier, got, BindTierGoInterfaceDispatch)
	}
	if stats.BindTierCounts[BindTierGoInterfaceDispatch] != 1 {
		t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)",
			BindTierGoInterfaceDispatch, stats.BindTierCounts[BindTierGoInterfaceDispatch], stats.BindTierCounts)
	}
}

// E2. The receiver type WAS stamped and IS ambiguous within the package —
// two `Svc.merge` in the same directory put a collision sentinel in
// byPackageMember. The #6125 hoist then discards the type evidence and
// scans the caller's FILE by leaf name, where the only `*.merge` is
// `Helper.merge`. The edge therefore binds to a method on a DIFFERENT type
// than the one the extractor named, which is the sharpest illustration in
// the whole change of why the marker is needed: nothing else about this
// edge records that a receiver type was available and thrown away.
func TestBindTier_GoAmbiguousReceiverLeafIsMarked_7071(t *testing.T) {
	recs := []types.EntityRecord{
		op("ooooooooooooooo1", "Svc.merge", "pkg/a/svc1.go"),
		op("ooooooooooooooo2", "Svc.merge", "pkg/a/svc2.go"),
		op("ooooooooooooooo3", "Helper.merge", "pkg/a/caller.go"),
		callerRec("ooooooooooooooo4", "Caller.run", "pkg/a/caller.go", "CALLS", "merge",
			types.Props{{K: "receiver_type", V: "Svc"}}),
	}
	idx := BuildIndex(recs)
	stats := ReferencesEmbeddedWithAllowlist(recs, idx, nil)
	r := recs[3].Relationships[0]
	if r.ToID != "ooooooooooooooo3" {
		t.Fatalf("edge bound to %q, want Helper.merge (ooooooooooooooo3) — the #6125 hoist's "+
			"same-file leaf scan; a different target means this test is not on that tier", r.ToID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != string(BindTierGoAmbiguousReceiverLeaf) {
		t.Fatalf("%s = %q, want %q", types.PropBindTier, got, BindTierGoAmbiguousReceiverLeaf)
	}
	if stats.BindTierCounts[BindTierGoAmbiguousReceiverLeaf] != 1 {
		t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)", BindTierGoAmbiguousReceiverLeaf,
			stats.BindTierCounts[BindTierGoAmbiguousReceiverLeaf], stats.BindTierCounts)
	}
}

// E4. The Rust pass, which is a different function entirely and never
// reaches either funnel.
//
// VARIED between the two subtests: whether the candidate directory the
// import resolver offered actually contains the member.
// HELD CONSTANT: the same records, the same edge properties except
// rust_call_pkg_dirs, and the same target entity. That is what makes the
// pair a control rather than two unrelated cases — both edges bind to
// ooooo…, and only the tier differs.
func TestBindTier_RustCrateUniqueMember_7071(t *testing.T) {
	build := func(dirs string) ([]types.EntityRecord, Stats, int) {
		recs := []types.EntityRecord{
			{ID: "ppppppppppppppp1", Kind: "Operation", Name: "OrderService.new", SourceFile: "src/svc/order.rs", Language: "rust"},
			{ID: "ppppppppppppppp2", Kind: "Operation", Name: "main", SourceFile: "src/main.rs", Language: "rust",
				Relationships: []types.RelationshipRecord{{
					FromID: "ppppppppppppppp2", ToID: "OrderService::new", Kind: "CALLS",
					Properties: types.Props{
						{K: "call_leaf", V: "new"},
						{K: "rust_call_pkg_dirs", V: dirs},
						{K: "rust_call_scope", V: "OrderService"},
					},
				}}},
		}
		idx := BuildIndex(recs)
		var stats Stats
		n := idx.ResolveRustCrossModuleCalls(recs, &stats)
		return recs, stats, n
	}

	t.Run("crate-wide fallback is a guess and is marked", func(t *testing.T) {
		// The offered directory holds nothing; only crate-wide uniqueness
		// answers.
		recs, stats, n := build("src/nowhere")
		if n != 1 {
			t.Fatalf("rewrites = %d, want 1", n)
		}
		r := recs[1].Relationships[0]
		if r.ToID != "ppppppppppppppp1" {
			t.Fatalf("bound to %q, want ppppppppppppppp1", r.ToID)
		}
		if got := r.Properties.Get(types.PropBindTier); got != string(BindTierRustCrateUniqueMember) {
			t.Fatalf("%s = %q, want %q", types.PropBindTier, got, BindTierRustCrateUniqueMember)
		}
		if stats.BindTierCounts[BindTierRustCrateUniqueMember] != 1 {
			t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)", BindTierRustCrateUniqueMember,
				stats.BindTierCounts[BindTierRustCrateUniqueMember], stats.BindTierCounts)
		}
	})

	t.Run("candidate-directory bind is evidence and is not marked", func(t *testing.T) {
		// The directory came from a resolved `use`, and it holds the
		// member. Same target, same edge, no marker.
		recs, stats, n := build("src/svc")
		if n != 1 {
			t.Fatalf("rewrites = %d, want 1", n)
		}
		r := recs[1].Relationships[0]
		if r.ToID != "ppppppppppppppp1" {
			t.Fatalf("bound to %q, want ppppppppppppppp1", r.ToID)
		}
		if got := r.Properties.Get(types.PropBindTier); got != "" {
			t.Fatalf("candidate-directory bind carried %s=%q; it resolved through an offered "+
				"`use` directory, which is evidence", types.PropBindTier, got)
		}
		if len(stats.BindTierCounts) != 0 {
			t.Fatalf("candidate-directory bind was counted as a guess: %v", stats.BindTierCounts)
		}
	})
}

// ---------------------------------------------------------------------------
// Negative controls — the evidence tiers.
//
// VARIED: the evidence the extractor supplied — an exact QualifiedName, a
// structural address, a resolved receiver type.
// HELD CONSTANT: the stub is still a non-hex string the resolver must
// rewrite, the edge still binds, and the assertion is still the same
// triple. If the marker were written at a funnel rather than at the
// deciding tier, every row here would carry it.
// ---------------------------------------------------------------------------

func TestBindTier_EvidenceTiersAreNotMarked_7071(t *testing.T) {
	cases := []tierFixture{
		{
			// The extractor minted the QualifiedName and the resolver
			// matched it exactly. Note "Target" is ALSO unique by bare name
			// here, so the global-name tier would have bound the same edge
			// to the same entity — the row grades that the marker follows
			// the tier that actually decided, not the outcome.
			name: "exact qualified name is evidence, not a guess",
			records: []types.EntityRecord{
				{ID: "jjjjjjjjjjjjjjj1", Kind: "Operation", Name: "Target", QualifiedName: "pkg/a.Target", SourceFile: "pkg/a/target.go", Language: "go"},
				callerRec("jjjjjjjjjjjjjjj2", "Caller", "pkg/z/caller.go", "CALLS", "pkg/a.Target", nil),
			},
			relAt:    [2]int{1, 0},
			wantTier: "",
			wantToID: "jjjjjjjjjjjjjjj1",
		},
		{
			// A receiver type the Go extractor stamped and that resolves
			// unambiguously within the package. The member Name is dotted,
			// so the FILE-LEAF tier could have reached the very same entity
			// — that is what makes this a control and not a tautology.
			name: "stamped unambiguous receiver type is evidence, not a guess",
			records: []types.EntityRecord{
				op("kkkkkkkkkkkkkkk1", "Svc.merge", "pkg/a/svc.go"),
				callerRec("kkkkkkkkkkkkkkk2", "Caller.run", "pkg/a/caller.go", "CALLS", "merge",
					types.Props{{K: "receiver_type", V: "Svc"}}),
			},
			relAt:    [2]int{1, 0},
			wantTier: "",
			wantToID: "kkkkkkkkkkkkkkk1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { checkTierFixture(t, c) })
	}
}

// TestBindTier_UnboundEdgeIsNotMarked_7071 pins the other absence: an edge
// the resolver REFUSED to bind must not carry a marker either. Absence of
// the key therefore means "not a guess", which covers both "bound on
// evidence" and "not bound at all" — a consumer that needs to tell those
// apart reads ToID, which is documented on types.PropBindTier.
func TestBindTier_UnboundEdgeIsNotMarked_7071(t *testing.T) {
	checkTierFixture(t, tierFixture{
		name: "two same-kind candidates in different files, no caller locality",
		records: []types.EntityRecord{
			op("lllllllllllllll1", "Target", "pkg/a/target.go"),
			op("lllllllllllllll2", "Target", "pkg/b/target.go"),
			callerRec("lllllllllllllll3", "Caller", "pkg/z/caller.go", "CALLS", "Target", nil),
		},
		relAt:    [2]int{2, 0},
		wantTier: "",
		wantToID: "Target",
	})
}

// TestBindTier_FromEndpointGuessIsNotMarked_7071 states the scope gap out
// loud instead of leaving it to be discovered. The SAME guess tiers can
// fire when the resolver rewrites a FROM endpoint, and those binds are not
// marked, because one key per edge cannot carry two answers and the key is
// named to_bind_tier. If a from-side marker is ever added this test is the
// thing that must change, which is the point of writing it down.
func TestBindTier_FromEndpointGuessIsNotMarked_7071(t *testing.T) {
	recs := []types.EntityRecord{
		op("mmmmmmmmmmmmmmm1", "Origin", "pkg/a/origin.go"),
		{
			ID: "mmmmmmmmmmmmmmm2", Kind: "Operation", Name: "Carrier", SourceFile: "pkg/z/carrier.go", Language: "go",
			Relationships: []types.RelationshipRecord{{
				FromID: "Origin", ToID: "mmmmmmmmmmmmmmm1", Kind: "CALLS",
			}},
		},
	}
	idx := BuildIndex(recs)
	stats := ReferencesEmbeddedWithAllowlist(recs, idx, nil)
	r := recs[1].Relationships[0]
	if r.FromID != "mmmmmmmmmmmmmmm1" {
		t.Fatalf("from endpoint did not bind through the global-name tier: %q", r.FromID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != "" {
		t.Fatalf("from-side guess carried %s=%q; the key describes the TO endpoint only",
			types.PropBindTier, got)
	}
	if n := len(stats.BindTierCounts); n != 0 {
		t.Fatalf("from-side guess was counted: %v", stats.BindTierCounts)
	}
}

// TestBindTier_GlobalNameConflatesBareAndQualifiedStubs_7071 pins the known
// coarseness of the global-name tier as a MEASURED FACT rather than a
// sentence in a doc comment, because it is the one place the marker
// currently overstates itself.
//
// byName is keyed on the entity's Name. The stub matched against it may be a
// bare leaf, or a DOTTED name the extractor built from a receiver type it
// successfully resolved. Both land on global-name, and this test is the
// evidence: the row below carries a competitor, so a locality tier could not
// have produced the answer — the only thing that did is the qualified string
// the extractor minted, which is type evidence, and it is nonetheless marked
// as a guess.
//
// VARIED against the table's global-name row: the stub is dotted and a
// same-leaf competitor exists.
// HELD CONSTANT: the edge kind, the caller's file, and the assertion shape.
//
// This test is written to DESCRIBE today's behaviour, not to bless it. If
// global-name is ever split on the separator, this test must change, and
// that is the point of it existing.
func TestBindTier_GlobalNameConflatesBareAndQualifiedStubs_7071(t *testing.T) {
	recs := []types.EntityRecord{
		op("qqqqqqqqqqqqqqq1", "AuditEntry.Touch", "Services/UserService.cs"),
		op("qqqqqqqqqqqqqqq2", "AuditNote.Touch", "Services/UserService.cs"),
		callerRec("qqqqqqqqqqqqqqq3", "AuditLog.Replay", "Services/UserService.cs", "CALLS", "AuditEntry.Touch", nil),
	}
	idx := BuildIndex(recs)
	ReferencesEmbeddedWithAllowlist(recs, idx, nil)
	r := recs[2].Relationships[0]
	if r.ToID != "qqqqqqqqqqqqqqq1" {
		t.Fatalf("dotted stub bound to %q, want the entity of that exact Name", r.ToID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != string(BindTierGlobalName) {
		t.Fatalf("%s = %q, want %q. If this now reads as an evidence tier (empty) or a "+
			"new qualified-name tier, global-name has been split — update the doc comment "+
			"on BindTierGlobalName and the #7071 PR's caveat along with this test",
			types.PropBindTier, got, BindTierGlobalName)
	}
}

// TestBindTier_StandaloneReferencesPath_7071 covers the OTHER funnel.
//
// References / ReferencesWithAllowlist resolve standalone relationship
// records with NO caller context, so the only tiers they can reach are the
// two global ones. Everything else in this file exercises the embedded path,
// and the two funnels are separate lines in separate loops: a mutant that
// stamps unconditionally at this one survived while its embedded twin died,
// which is exactly the "graded at one anchor and zero of the other" shape.
//
// VARIED across the two rows: whether the stub is an exact QualifiedName
// (evidence) or a graph-unique bare name (the global-name guess).
// HELD CONSTANT: the entity set, the edge kind, and the absence of caller
// context — which is what makes this the References path rather than the
// embedded one.
func TestBindTier_StandaloneReferencesPath_7071(t *testing.T) {
	ents := []types.EntityRecord{
		{ID: "rrrrrrrrrrrrrrr1", Kind: "Operation", Name: "Target", QualifiedName: "pkg/a.Target", SourceFile: "pkg/a/target.go", Language: "go"},
		{ID: "rrrrrrrrrrrrrrr2", Kind: "Operation", Name: "Caller", SourceFile: "pkg/z/caller.go", Language: "go"},
	}
	cases := []struct {
		name     string
		stub     string
		wantTier BindTier
	}{
		{"exact qualified name is evidence", "pkg/a.Target", ""},
		{"graph-unique bare name is the global-name guess", "Target", BindTierGlobalName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx := BuildIndex(ents)
			rels := []types.RelationshipRecord{{FromID: "rrrrrrrrrrrrrrr2", ToID: c.stub, Kind: "CALLS"}}
			stats := ReferencesWithAllowlist(rels, idx, nil)
			if rels[0].ToID != "rrrrrrrrrrrrrrr1" {
				t.Fatalf("edge bound to %q, want rrrrrrrrrrrrrrr1", rels[0].ToID)
			}
			if got := BindTier(rels[0].Properties.Get(types.PropBindTier)); got != c.wantTier {
				t.Fatalf("%s = %q, want %q", types.PropBindTier, got, c.wantTier)
			}
			want := 0
			if c.wantTier != "" {
				want = 1
			}
			if got := stats.BindTierCounts[c.wantTier]; c.wantTier != "" && got != want {
				t.Fatalf("BindTierCounts[%s] = %d, want %d", c.wantTier, got, want)
			}
			if c.wantTier == "" && len(stats.BindTierCounts) != 0 {
				t.Fatalf("evidence bind on the References path was counted: %v", stats.BindTierCounts)
			}
		})
	}
}

// TestFormatBindTiers_7071 grades the report line itself. It is the only
// thing a corpus run prints, so a silent defect here is a silent loss of the
// measurement the whole change exists to produce.
//
// VARIED: empty, all-zero, one tier, several tiers.
// HELD CONSTANT: the assertion is on the exact rendered string, so both the
// per-tier figures and the total are pinned. A mutant that drops the total
// survived an earlier, looser version of this test.
func TestFormatBindTiers_7071(t *testing.T) {
	cases := []struct {
		name string
		in   map[BindTier]int
		want string
	}{
		{"nil", nil, ""},
		{"all zero", map[BindTier]int{BindTierGlobalName: 0}, ""},
		{"one tier", map[BindTier]int{BindTierFileLeafName: 3}, "file-leaf-name=3 total=3"},
		{
			// Rendered in AllBindTiers order, not map order, and the total
			// is the sum — not the number of tiers.
			"several tiers, AllBindTiers order",
			map[BindTier]int{BindTierFileLeafName: 3, BindTierGlobalName: 41, BindTierRustCrateUniqueMember: 1},
			"global-name=41 file-leaf-name=3 rust-crate-unique-member=1 total=45",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatBindTiers(c.in); got != c.want {
				t.Fatalf("FormatBindTiers = %q, want %q", got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FINDING B — the REPORT PIPELINE.
//
// Three times in this change the same defect appeared, and every one was on
// the reporting path rather than the marking path: the `References` funnel
// graded while its embedded twin was not, `FormatBindTiers` dropping the
// total with no test on the only line a corpus run prints, and the merge
// step below with no test at all. A marker whose output silently vanishes
// is a marker nobody grades on.
//
// The failure mode is specifically silent: FormatBindTiers returns "" for an
// empty tally and cmd/grafel's `if line != ""` guard then prints NOTHING.
// There is no zero row to notice.
// ---------------------------------------------------------------------------

// TestMergeBindTiers_7071 grades the Stats→Stats assembly.
//
// VARIED: nil dst, nil src, empty src, a dst that already holds counts for
// the same tier and for a different one.
// HELD CONSTANT: the tier values used, so every row is comparable, and the
// assertion is always on dst's resulting map.
func TestMergeBindTiers_7071(t *testing.T) {
	t.Run("accumulates across passes rather than overwriting", func(t *testing.T) {
		dst := &Stats{BindTierCounts: map[BindTier]int{
			BindTierGlobalName:   2,
			BindTierFileLeafName: 1,
		}}
		src := &Stats{BindTierCounts: map[BindTier]int{
			BindTierGlobalName:      5,
			BindTierPackageLeafName: 3,
		}}
		MergeBindTiers(dst, src)
		want := map[BindTier]int{
			BindTierGlobalName:      7,
			BindTierFileLeafName:    1,
			BindTierPackageLeafName: 3,
		}
		for tier, n := range want {
			if dst.BindTierCounts[tier] != n {
				t.Fatalf("after merge, [%s] = %d, want %d (%v)",
					tier, dst.BindTierCounts[tier], n, dst.BindTierCounts)
			}
		}
		if len(dst.BindTierCounts) != len(want) {
			t.Fatalf("merge invented or dropped tiers: %v", dst.BindTierCounts)
		}
	})

	t.Run("allocates dst's map when it has none", func(t *testing.T) {
		// cmd/grafel's totalStats is a bare struct literal, so this is the
		// PRODUCTION shape, not a corner case.
		dst := &Stats{}
		MergeBindTiers(dst, &Stats{BindTierCounts: map[BindTier]int{BindTierImportWildcard: 4}})
		if dst.BindTierCounts[BindTierImportWildcard] != 4 {
			t.Fatalf("merge into a zero-value Stats lost the counts: %v", dst.BindTierCounts)
		}
	})

	t.Run("nil src and empty src are no-ops, not panics", func(t *testing.T) {
		dst := &Stats{BindTierCounts: map[BindTier]int{BindTierGlobalName: 1}}
		MergeBindTiers(dst, nil)
		MergeBindTiers(dst, &Stats{})
		MergeBindTiers(nil, &Stats{BindTierCounts: map[BindTier]int{BindTierGlobalName: 9}})
		if dst.BindTierCounts[BindTierGlobalName] != 1 {
			t.Fatalf("a no-op merge changed dst: %v", dst.BindTierCounts)
		}
	})
}

// TestMergeBindTierMap_7071 grades the map→Stats assembly, which is the
// SOLE route by which the two import-pass tiers reach the report —
// ResolveImports is not a Reference pass and returns an ImportResolveStats.
// A pass whose counts have exactly one route to the report is a pass whose
// counts disappear the moment that route breaks.
func TestMergeBindTierMap_7071(t *testing.T) {
	dst := &Stats{}
	MergeBindTierMap(dst, map[BindTier]int{
		BindTierImportPlainModuleAttr: 6,
		BindTierImportWildcard:        2,
	})
	MergeBindTierMap(dst, map[BindTier]int{BindTierImportWildcard: 1})
	MergeBindTierMap(dst, nil)
	MergeBindTierMap(nil, map[BindTier]int{BindTierGlobalName: 1})
	if dst.BindTierCounts[BindTierImportPlainModuleAttr] != 6 ||
		dst.BindTierCounts[BindTierImportWildcard] != 3 {
		t.Fatalf("map merge did not accumulate: %v", dst.BindTierCounts)
	}
}

// TestBindTierReportPipeline_EndToEnd_7071 is the one that would have caught
// MR10 without anybody thinking to mutate the merge.
//
// It walks the whole reporting path a corpus run walks — per-pass Stats →
// merge → FormatBindTiers → the string cmd/grafel prints — and asserts the
// FINAL STRING. Grading the renderer alone (TestFormatBindTiers_7071) and
// the merge alone (above) still leaves the composition ungraded; this row
// is the composition.
//
// VARIED: nothing. It is a single end-to-end trace, deliberately, because
// what it grades is that the stages are WIRED, not how any stage behaves.
// HELD CONSTANT: everything; the value is the exact expected output.
func TestBindTierReportPipeline_EndToEnd_7071(t *testing.T) {
	// Three producers, exactly as cmd/grafel has them: two Reference
	// passes, the Rust pass, and the import pass's bare map.
	emb := Stats{BindTierCounts: map[BindTier]int{BindTierGlobalName: 10, BindTierFileLeafName: 2}}
	stand := Stats{BindTierCounts: map[BindTier]int{BindTierGlobalName: 1}}
	rust := Stats{BindTierCounts: map[BindTier]int{BindTierRustCrateUniqueMember: 4}}
	imports := map[BindTier]int{BindTierImportWildcard: 3}

	var total Stats
	MergeBindTiers(&total, &emb)
	MergeBindTiers(&total, &stand)
	MergeBindTiers(&total, &rust)
	MergeBindTierMap(&total, imports)

	got := FormatBindTiers(total.BindTierCounts)
	want := "global-name=11 file-leaf-name=2 import-wildcard=3 rust-crate-unique-member=4 total=20"
	if got != want {
		t.Fatalf("report line = %q, want %q.\n"+
			"An empty or short line here means a producer's counts never reach the report, and "+
			"because cmd/grafel guards the print on a non-empty string, that failure is SILENT "+
			"on a real corpus run.", got, want)
	}
}

// TestBindTier_PlatformVariantCloneInheritsTheMarker_7071 — FINDING D.
//
// The #1818 fan-out clones an already-resolved CALLS edge once per platform
// variant. Its source comment claims the clone inherits the marker "by
// construction" and is deliberately neither re-stamped nor re-counted. That
// was prose no test observed, and the parity gates structurally cannot see
// it: they compare full vs incremental, and a defect present on both sides
// produces no divergence.
//
// Both halves of the claim are asserted here, because they are separable —
// a clone could carry the marker and still be double-counted.
func TestBindTier_PlatformVariantCloneInheritsTheMarker_7071(t *testing.T) {
	recs := []types.EntityRecord{
		op("ssssssssssssss01", "Target", "pkg/a/target.go"),
		op("ssssssssssssss02", "TargetVariant", "pkg/a/target_windows.go"),
		callerRec("ssssssssssssss03", "Caller", "pkg/z/caller.go", "CALLS", "Target", nil),
	}
	idx := BuildIndex(recs)
	// The fan-out is driven by this map alone; it is populated by the
	// build-tag variant pass in production.
	idx.PlatformVariants = map[string][]string{"ssssssssssssss01": {"ssssssssssssss02"}}

	stats := ReferencesEmbeddedWithAllowlist(recs, idx, nil)

	rels := recs[2].Relationships
	if len(rels) != 2 {
		t.Fatalf("expected the original edge plus one platform-variant clone, got %d", len(rels))
	}
	parent, clone := rels[0], rels[1]
	if parent.ToID != "ssssssssssssss01" || clone.ToID != "ssssssssssssss02" {
		t.Fatalf("fan-out did not produce (parent=%q, clone=%q) as expected", parent.ToID, clone.ToID)
	}
	if got := parent.Properties.Get(types.PropBindTier); got != string(BindTierGlobalName) {
		t.Fatalf("parent %s = %q, want %q", types.PropBindTier, got, BindTierGlobalName)
	}
	if got := clone.Properties.Get(types.PropBindTier); got != string(BindTierGlobalName) {
		t.Fatalf("clone %s = %q, want %q — a clone that drops the marker ships a guess "+
			"wearing 'not a guess', and no parity gate can see it because the defect would be "+
			"present on both sides of the comparison", types.PropBindTier, got, BindTierGlobalName)
	}
	// The second half of the claim, separably. One decision, one count.
	if n := stats.BindTierCounts[BindTierGlobalName]; n != 1 {
		t.Fatalf("BindTierCounts[%s] = %d, want 1 — the clone is the SAME bind reaching a "+
			"second platform variant, so counting it again inflates the per-tier incidence "+
			"this change exists to measure", BindTierGlobalName, n)
	}
}

// TestBindTier_AllTiersEnumerated_7071 pins the enumeration itself. A tier
// constant that exists but is missing from AllBindTiers is invisible to
// every report built over it, which is the unmeasured state this change
// exists to end.
func TestBindTier_AllTiersEnumerated_7071(t *testing.T) {
	seen := map[BindTier]bool{}
	for _, tr := range AllBindTiers {
		if tr == "" {
			t.Fatal("AllBindTiers contains a blank tier; blank means 'no guess' and must never be a member")
		}
		if seen[tr] {
			t.Fatalf("AllBindTiers lists %q twice", tr)
		}
		seen[tr] = true
	}
	if len(AllBindTiers) != 16 {
		t.Fatalf("AllBindTiers has %d members, want 16 — adding or removing one is a scope "+
			"change that must be argued, not a silent edit.\n"+
			"The enumeration is of GUESS TIERS ACROSS THE WHOLE RESOLVER, not of one file: "+
			"ELEVEN in internal/resolve/refs.go (G1, G2, L1-L6, E1-E3) and FIVE in "+
			"internal/resolve/imports.go (the Rust crate-wide rung; the plain-import and "+
			"wildcard rungs shared by ResolveBareCallTarget and "+
			"ResolveCrossFileReferenceTarget; ResolveCrossModuleCallTarget's same-class "+
			"fallback; and the Java canonical file tie-break).\n"+
			"Every one of the last four was found by REVIEW, not by the grounding, and each "+
			"round of review found one more. If you are adding a seventeenth because you "+
			"found another unclassified site, that is the expected outcome, not a surprise: "+
			"say where it is and why it is a guess rather than evidence.",
			len(AllBindTiers))
	}
}
