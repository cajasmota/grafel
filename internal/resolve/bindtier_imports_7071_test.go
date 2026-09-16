package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// #7071 FINDING A — `ResolveImports` runs BEFORE every resolver the twelve
// original tiers live in, so every edge it binds arrives at them already
// hex and short-circuits. Two of its rungs are lexical guesses by this
// repo's own taxonomy and were previously unmarked, which made the absence
// of the marker on that population assert something false.
//
// The two rungs live in TWO functions — ResolveBareCallTarget (CALLS) and
// ResolveCrossFileReferenceTarget (REFERENCES) — reached from TWO separate
// branches of ResolveImports' switch. That is the exact shape that let the
// `References` funnel mutant survive while its embedded twin died, so each
// rung is graded at BOTH call sites rather than once at whichever is
// convenient.

// impRec builds an entity record with an optional embedded relationship.
func impRec(id, kind, name, file string, rels ...types.RelationshipRecord) types.EntityRecord {
	return types.EntityRecord{
		ID: id, Kind: kind, Name: name, SourceFile: file, Language: "python",
		Relationships: rels,
	}
}

// importsBind runs the real BuildImportTable + ResolveImports pass and
// returns the edge under test together with the pass's tier tally.
func importsBind(t *testing.T, recs []types.EntityRecord, at [2]int) (*types.RelationshipRecord, ImportResolveStats) {
	t.Helper()
	tbl := BuildImportTable(recs)
	st := ResolveImports(recs, tbl)
	return &recs[at[0]].Relationships[at[1]], st
}

func assertImportTier(t *testing.T, r *types.RelationshipRecord, st ImportResolveStats, wantToID string, wantTier BindTier) {
	t.Helper()
	if r.ToID != wantToID {
		t.Fatalf("edge bound to %q, want %q — a tier assertion on a differently-bound "+
			"edge grades nothing", r.ToID, wantToID)
	}
	got := BindTier(r.Properties.Get(types.PropBindTier))
	if got != wantTier {
		t.Fatalf("%s = %q, want %q", types.PropBindTier, got, wantTier)
	}
	if wantTier == "" {
		if len(st.BindTierCounts) != 0 {
			t.Fatalf("an evidence bind was counted as a guess: %v", st.BindTierCounts)
		}
		return
	}
	if st.BindTierCounts[wantTier] != 1 {
		t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)",
			wantTier, st.BindTierCounts[wantTier], st.BindTierCounts)
	}
}

// ---------------------------------------------------------------------------
// The CALLS call site (ResolveBareCallTarget), all three rungs.
//
// VARIED across the three rows: which rung answers — an explicit
// `from x import y` binding (evidence), a plain `import x` whose module
// uniquely contains the name (guess), and a `from x import *` wildcard
// (guess, and the only rung in the whole enumeration with no ambiguity
// sentinel).
//
// HELD CONSTANT: the target entity is always `handler` in module `svc`, the
// caller is always `app.py`, the edge is always a bare dot-free CALLS stub
// `handler`, and the assertion is always the triple (bound target, tier
// value, counter). Only the IMPORTS edge that introduces the name changes —
// which is what selects the rung.
//
// NOT VARIED here, and GRADED ELSEWHERE: the Java canonical tie-break
// inside rung 1. Round 2 of this change argued it was evidence at source
// rather than measuring it; round 3's review demonstrated that argument
// false, and it is now a tier with its own rows in
// TestBindTier_JavaCanonicalTiebreak_7071 below. This header used to say it
// was "the one judgement call in this pair" — it is not a judgement call
// any more, it is a measurement.
// ---------------------------------------------------------------------------

func TestBindTier_ImportPassCallsSite_7071(t *testing.T) {
	// The module's entity, identical in every row.
	// modulesForFile derives the dotted module from the path, so living in
	// svc.py IS what puts this entity in module "svc".
	target := types.EntityRecord{
		ID: "iiiimportcalls01", Kind: "Operation", Name: "handler",
		SourceFile: "svc.py", Language: "python",
	}

	cases := []struct {
		name       string
		importEdge types.RelationshipRecord
		wantTier   BindTier
	}{
		{
			// Rung 1. `from svc import handler` — the import statement names
			// the module AND the symbol. Nothing is inferred from the bare
			// call name, so no marker.
			name: "explicit from-import binding is evidence",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc.handler", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "handler"},
					{K: "local_name", V: "handler"},
					{K: "source_module", V: "svc"},
				},
			},
			wantTier: "",
		},
		{
			// Rung 2. `import svc` — the extractor saw `svc.handler()` and
			// STRIPPED the receiver, emitting the bare leaf. The only thing
			// that picks `svc` back out is that exactly one plain import in
			// this file answers.
			name: "plain-import module-attribute uniqueness is a guess",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "svc"},
					{K: "local_name", V: "svc"},
					{K: "source_module", V: "svc"},
				},
			},
			wantTier: BindTierImportPlainModuleAttr,
		},
		{
			// Rung 3. `from svc import *` — no sentinel anywhere; the first
			// wildcard module that answers wins.
			name: "wildcard first-hit is a guess",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "source_module", V: "svc"},
					{K: "wildcard", V: "1"},
				},
			},
			wantTier: BindTierImportWildcard,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs := []types.EntityRecord{
				target,
				impRec("iiiimportcalls02", "SCOPE.Component", "app", "app.py", c.importEdge),
				impRec("iiiimportcalls03", "Operation", "main", "app.py",
					types.RelationshipRecord{FromID: "iiiimportcalls03", ToID: "handler", Kind: "CALLS"}),
			}
			r, st := importsBind(t, recs, [2]int{2, 0})
			assertImportTier(t, r, st, "iiiimportcalls01", c.wantTier)
		})
	}
}

// ---------------------------------------------------------------------------
// The REFERENCES call site (ResolveCrossFileReferenceTarget).
//
// Same three rungs, a different function, a different branch of
// ResolveImports' switch, and a different stub shape — a Format A
// structural ref rather than a bare name.
//
// VARIED: which rung answers, exactly as above.
// HELD CONSTANT: the target entity, the caller file, the structural-ref
// stub, and the assertion triple.
//
// This exists because grading the CALLS site says nothing about this one.
// A mutant that drops the tier from ONLY this site compiles, passes the
// CALLS table, and would ship unmarked REFERENCES guesses.
// ---------------------------------------------------------------------------

func TestBindTier_ImportPassReferencesSite_7071(t *testing.T) {
	target := types.EntityRecord{
		ID: "iiiimportrefs001", Kind: "Component", Name: "Widget",
		SourceFile: "svc.py", Language: "python",
	}
	const stub = "scope:component:ref:python:app.py:Widget"

	cases := []struct {
		name       string
		importEdge types.RelationshipRecord
		wantTier   BindTier
	}{
		{
			name: "explicit from-import binding is evidence",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc.Widget", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "Widget"},
					{K: "local_name", V: "Widget"},
					{K: "source_module", V: "svc"},
				},
			},
			wantTier: "",
		},
		{
			name: "plain-import module-attribute uniqueness is a guess",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "svc"},
					{K: "local_name", V: "svc"},
					{K: "source_module", V: "svc"},
				},
			},
			wantTier: BindTierImportPlainModuleAttr,
		},
		{
			name: "wildcard first-hit is a guess",
			importEdge: types.RelationshipRecord{
				FromID: "app.py", ToID: "svc", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "source_module", V: "svc"},
					{K: "wildcard", V: "1"},
				},
			},
			wantTier: BindTierImportWildcard,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs := []types.EntityRecord{
				target,
				impRec("iiiimportrefs002", "SCOPE.Component", "app", "app.py", c.importEdge),
				impRec("iiiimportrefs003", "Operation", "use", "app.py",
					types.RelationshipRecord{FromID: "iiiimportrefs003", ToID: stub, Kind: "REFERENCES"}),
			}
			r, st := importsBind(t, recs, [2]int{2, 0})
			assertImportTier(t, r, st, "iiiimportrefs001", c.wantTier)
		})
	}
}

// refusalRecords builds the two-disagreeing-plain-imports graph shared by
// both refusal tests: `handler` exists in module a AND module b, the caller
// plain-imports both, so rung 2 must decline rather than take a first hit.
// The only thing that differs between the two callers is the edge Kind and
// stub shape, which is what routes them to the two different functions.
func refusalRecords(callerRel types.RelationshipRecord) []types.EntityRecord {
	return []types.EntityRecord{
		{ID: "iiiimportrefu001", Kind: "Operation", Name: "handler", SourceFile: "a.py", Language: "python"},
		{ID: "iiiimportrefu002", Kind: "Operation", Name: "handler", SourceFile: "b.py", Language: "python"},
		impRec("iiiimportrefu003", "SCOPE.Component", "app", "app.py",
			types.RelationshipRecord{
				FromID: "app.py", ToID: "a", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "a"}, {K: "local_name", V: "a"},
					{K: "source_module", V: "a"},
				},
			},
			types.RelationshipRecord{
				FromID: "app.py", ToID: "b", Kind: "IMPORTS",
				Properties: types.Props{
					{K: "imported_name", V: "b"}, {K: "local_name", V: "b"},
					{K: "source_module", V: "b"},
				},
			}),
		impRec("iiiimportrefu004", "Operation", "caller", "app.py", callerRel),
	}
}

// assertRefused checks that the stub was left verbatim, unmarked, uncounted.
func assertRefused(t *testing.T, r *types.RelationshipRecord, st ImportResolveStats, wantStub string) {
	t.Helper()
	if r.ToID != wantStub {
		t.Fatalf("two disagreeing plain imports must leave the stub verbatim, got %q — "+
			"if this bound, rung 2 has lost its ambiguity guard and is now rung 3, which is "+
			"the ONE property that justifies them being two tiers instead of one", r.ToID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != "" {
		t.Fatalf("a refused bind carried %s=%q", types.PropBindTier, got)
	}
	if len(st.BindTierCounts) != 0 {
		t.Fatalf("a refused bind was counted: %v", st.BindTierCounts)
	}
}

// TestBindTier_ImportPassRefusalIsNotMarked_7071 pins the other absence.
// Rung 2 REFUSES when two plain imports disagree, and a refusal leaves the
// stub verbatim — it must carry no marker and increment nothing, because
// absence of the key means "not a guess", which covers "not bound at all".
//
// It also grades the refusal itself, which is the one property rung 2 has
// that rung 3 does not. Without this row, a mutant that made rung 2 take
// the first hit — turning it into rung 3 — would be indistinguishable.
//
// THE REFUSAL IS A PER-FUNCTION PROPERTY, NOT A PER-TIER ONE. rung 2 is
// implemented TWICE — once in ResolveBareCallTarget and once in
// ResolveCrossFileReferenceTarget — and each copy has its own `plainHits ==
// 1` guard. Round 2 of this change graded the CALLS copy and claimed both
// were covered because the tier VALUES were graded at both sites; review
// mutated the REFERENCES copy and it survived against ./internal/resolve/,
// ./internal/quality/ AND the full ./cmd/grafel/ leg. The tier values and
// the refusal are different properties and need different rows. Both are
// here now.
func TestBindTier_ImportPassRefusalIsNotMarked_7071(t *testing.T) {
	t.Run("CALLS site (ResolveBareCallTarget)", func(t *testing.T) {
		recs := refusalRecords(types.RelationshipRecord{
			FromID: "iiiimportrefu004", ToID: "handler", Kind: "CALLS",
		})
		r, st := importsBind(t, recs, [2]int{3, 0})
		assertRefused(t, r, st, "handler")
	})

	t.Run("REFERENCES site (ResolveCrossFileReferenceTarget)", func(t *testing.T) {
		// The stub shape is what routes this to the other function: a
		// Format A structural ref rather than a bare name. Everything
		// else — the two disagreeing plain imports, the two candidate
		// entities, the caller file — is identical to the CALLS row above.
		const stub = "scope:operation:ref:python:app.py:handler"
		recs := refusalRecords(types.RelationshipRecord{
			FromID: "iiiimportrefu004", ToID: stub, Kind: "REFERENCES",
		})
		r, st := importsBind(t, recs, [2]int{3, 0})
		assertRefused(t, r, st, stub)
	})
}

// ---------------------------------------------------------------------------
// ResolveCrossModuleCallTarget — the fifteenth guess rung, found by review
// of round 2 THREE LINES ABOVE round 1's own finding, in the same funnel.
//
// ResolveImports' CALLS branch tries this probe FIRST and `continue`s on
// success, so an unmarked bind here is never reconsidered by the rungs
// round 2 marked. Its last rung looks `leaf` up at MODULE scope while the
// receiver is a CLASS, so it can bind a target that is not a member of the
// receiver at all — and before this it shipped with the key absent, which
// asserts "resolved on evidence".
//
// VARIED across the three rows: which rung answers — `import x` (the
// receiver IS the module, evidence), `from x import y` where y is a
// submodule (evidence), and `from x import y` where y is a CLASS (the
// guess).
// HELD CONSTANT: the caller file, the edge (a CALLS with import_alias +
// call_leaf properties), the leaf name `format`, and the assertion triple.
// Only the module layout and the IMPORTS binding change, which is what
// selects the rung.
// ---------------------------------------------------------------------------

func TestBindTier_ImportCrossModuleCallTarget_7071(t *testing.T) {
	// The edge is identical in every row: `<alias>.format()` with the
	// receiver stamped as an alias and the leaf split out.
	callEdge := func(alias string) types.RelationshipRecord {
		return types.RelationshipRecord{
			FromID: "iiiixmod00000003", ToID: "format", Kind: "CALLS",
			Properties: types.Props{
				{K: "call_leaf", V: "format"},
				{K: "import_alias", V: alias},
			},
		}
	}

	t.Run("plain `import x` — the receiver IS the module, evidence", func(t *testing.T) {
		recs := []types.EntityRecord{
			{ID: "iiiixmod00000001", Kind: "Operation", Name: "format", SourceFile: "utils.py", Language: "python"},
			impRec("iiiixmod00000002", "SCOPE.Component", "app", "app.py",
				types.RelationshipRecord{
					FromID: "app.py", ToID: "utils", Kind: "IMPORTS",
					Properties: types.Props{
						{K: "imported_name", V: "utils"}, {K: "local_name", V: "utils"},
						{K: "source_module", V: "utils"},
					},
				}),
			impRec("iiiixmod00000003", "Operation", "main", "app.py", callEdge("utils")),
		}
		r, st := importsBind(t, recs, [2]int{2, 0})
		assertImportTier(t, r, st, "iiiixmod00000001", "")
	})

	t.Run("`from x import y` where y is a SUBMODULE — evidence", func(t *testing.T) {
		recs := []types.EntityRecord{
			{ID: "iiiixmod00000011", Kind: "Operation", Name: "format", SourceFile: "pkg/text.py", Language: "python"},
			impRec("iiiixmod00000012", "SCOPE.Component", "app", "app.py",
				types.RelationshipRecord{
					FromID: "app.py", ToID: "pkg.text", Kind: "IMPORTS",
					Properties: types.Props{
						{K: "imported_name", V: "text"}, {K: "local_name", V: "text"},
						{K: "source_module", V: "pkg"},
					},
				}),
			impRec("iiiixmod00000013", "Operation", "main", "app.py", callEdge("text")),
		}
		// Reuse the shared edge id so callEdge's FromID still points at the
		// caller record.
		recs[2].Relationships[0].FromID = "iiiixmod00000013"
		r, st := importsBind(t, recs, [2]int{2, 0})
		assertImportTier(t, r, st, "iiiixmod00000011", "")
	})

	t.Run("`from x import y` where y is a CLASS — the same-class fallback is a guess", func(t *testing.T) {
		// `Helper` is a class in utils; `format` is a MODULE-LEVEL function
		// in utils and no member of Helper. `Helper.format()` binds to it
		// anyway — the guard only checks that Helper is in utils, which is
		// trivially true. That is the demonstration, not an argument: the
		// bound target is not a member of the named receiver.
		recs := []types.EntityRecord{
			{ID: "iiiixmod00000021", Kind: "Component", Name: "Helper", SourceFile: "utils.py", Language: "python"},
			{ID: "iiiixmod00000022", Kind: "Operation", Name: "format", SourceFile: "utils.py", Language: "python"},
			impRec("iiiixmod00000023", "SCOPE.Component", "app", "app.py",
				types.RelationshipRecord{
					FromID: "app.py", ToID: "utils.Helper", Kind: "IMPORTS",
					Properties: types.Props{
						{K: "imported_name", V: "Helper"}, {K: "local_name", V: "Helper"},
						{K: "source_module", V: "utils"},
					},
				}),
			impRec("iiiixmod00000024", "Operation", "main", "app.py", callEdge("Helper")),
		}
		recs[3].Relationships[0].FromID = "iiiixmod00000024"
		r, st := importsBind(t, recs, [2]int{3, 0})
		assertImportTier(t, r, st, "iiiixmod00000022", BindTierImportClassModuleAttr)
	})

	t.Run("the guard DOES decline when the receiver class is ambiguous", func(t *testing.T) {
		// The other half of the guard's behaviour, and the half a round-3
		// comment got wrong by calling the guard "trivially true whenever
		// the from-import above resolved". It is not: lookupModuleEntity
		// refuses on an ambiguous (module, name) tuple, so a class name that
		// collides inside its own module makes the fallback DECLINE.
		//
		// VARIED against the row above: `Helper` is ambiguous in `utils`
		// (two entities of that name in the module).
		// HELD CONSTANT: the caller, the edge, the leaf `format`, and the
		// fact that `format` itself is still unique — so the only thing
		// that can change the verdict is the receiver's ambiguity.
		recs := []types.EntityRecord{
			{ID: "iiiixmod00000031", Kind: "Component", Name: "Helper", SourceFile: "utils.py", Language: "python"},
			{ID: "iiiixmod00000032", Kind: "Component", Name: "Helper", SourceFile: "utils.py", Language: "python"},
			{ID: "iiiixmod00000033", Kind: "Operation", Name: "format", SourceFile: "utils.py", Language: "python"},
			impRec("iiiixmod00000034", "SCOPE.Component", "app", "app.py",
				types.RelationshipRecord{
					FromID: "app.py", ToID: "utils.Helper", Kind: "IMPORTS",
					Properties: types.Props{
						{K: "imported_name", V: "Helper"}, {K: "local_name", V: "Helper"},
						{K: "source_module", V: "utils"},
					},
				}),
			impRec("iiiixmod00000035", "Operation", "main", "app.py", callEdge("Helper")),
		}
		recs[4].Relationships[0].FromID = "iiiixmod00000035"
		r, st := importsBind(t, recs, [2]int{4, 0})
		if r.ToID != "format" {
			t.Fatalf("an ambiguous receiver class must make the same-class fallback decline, "+
				"leaving the stub verbatim; got %q. If this bound, the guard is genuinely "+
				"vacuous and the tier's doc comment should say so", r.ToID)
		}
		if got := r.Properties.Get(types.PropBindTier); got != "" {
			t.Fatalf("a declined bind carried %s=%q", types.PropBindTier, got)
		}
		if len(st.BindTierCounts) != 0 {
			t.Fatalf("a declined bind was counted: %v", st.BindTierCounts)
		}
	})
}

// ---------------------------------------------------------------------------
// The IMPORTS ladder — the three rungs round 3 covered with a blanket
// EVIDENCE sentence instead of checking.
//
// That sentence ("every other rung in this ladder resolves a qualifier the
// extractor minted") was the deleted universal's shape scoped to a file,
// and review refuted it by RUNNING two of the three. Each rung is now
// classified at its own site; these are the rows that observe it.
//
// VARIED across the three subtests: which rung answers — the PHP/C#
// namespace representative, the JS `<module>.default` basename
// substitution, and the Python re-export parent rebind.
// HELD CONSTANT: every case drives the real BuildImportTable +
// ResolveImports over one IMPORTS edge, and asserts the same triple —
// bound target, tier value, counter.
//
// Each subtest ALSO pins the paired evidence rung of the same function
// where one exists, because a tier value is only meaningful if its absence
// is meaningful: a mutant that stamps the whole wrapper would satisfy the
// guess row alone.
// ---------------------------------------------------------------------------

func TestBindTier_ImportsLadderRungs_7071(t *testing.T) {
	t.Run("PHP namespace prefix binds an arbitrary representative — a guess", func(t *testing.T) {
		// `use App\Sub;` names a NAMESPACE, not a class. Two classes live
		// in it; the rung sorts and takes the lowest id. Both candidates
		// are equally valid, and nothing declines.
		recs := []types.EntityRecord{
			{ID: "pppphpns0000001", Kind: "Component", Name: "Alpha",
				SourceFile: "App/Sub/Alpha.php", Language: "php"},
			{ID: "pppphpns0000002", Kind: "Component", Name: "Beta",
				SourceFile: "App/Sub/Beta.php", Language: "php"},
			{
				ID: "pppphpns0000003", Kind: "SCOPE.Component", Name: "Main",
				SourceFile: "App/Main.php", Language: "php",
				Relationships: []types.RelationshipRecord{{
					FromID: "App/Main.php", ToID: `App\Sub`, Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "php"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{2, 0})
		if r.ToID != "pppphpns0000001" && r.ToID != "pppphpns0000002" {
			t.Fatalf("namespace import bound %q, want one of the two namespace members — "+
				"if it bound nothing this row grades nothing", r.ToID)
		}
		assertImportTier(t, r, st, r.ToID, BindTierImportNamespaceRepresentative)
	})

	t.Run("PHP exact (module, leaf) is evidence", func(t *testing.T) {
		// Same wrapper, one rung earlier: `use App\Sub\Alpha;` names the
		// class, and the minted split matches it exactly. The single varied
		// axis against the row above is whether the import names a class.
		recs := []types.EntityRecord{
			{ID: "pppphpev0000001", Kind: "Component", Name: "Alpha",
				SourceFile: "App/Sub/Alpha.php", Language: "php"},
			{
				ID: "pppphpev0000002", Kind: "SCOPE.Component", Name: "Main",
				SourceFile: "App/Main.php", Language: "php",
				Relationships: []types.RelationshipRecord{{
					FromID: "App/Main.php", ToID: `App\Sub\Alpha`, Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "php"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "pppphpev0000001", "")
	})

	t.Run("C# namespace prefix — the namespace rung's SECOND call site", func(t *testing.T) {
		// resolveNamespaceTarget is reached from two places: the PHP
		// wrapper above, and this C#-gated arm of the else branch. Separate
		// lines in separate branches, so grading the PHP one says nothing
		// about this one — the lesson four earlier rounds of this change
		// have each been taught once.
		recs := []types.EntityRecord{
			// C# entities reach entitiesByModuleName ONLY through the
			// csharp_namespace property — modulesForFile derives nothing
			// from a .cs path.
			{ID: "cccsns000000001", Kind: "Component", Name: "Alpha",
				SourceFile: "App/Sub/Alpha.cs", Language: "csharp",
				Properties: map[string]string{"csharp_namespace": "App.Sub"}},
			{ID: "cccsns000000002", Kind: "Component", Name: "Beta",
				SourceFile: "App/Sub/Beta.cs", Language: "csharp",
				Properties: map[string]string{"csharp_namespace": "App.Sub"}},
			{
				ID: "cccsns000000003", Kind: "SCOPE.Component", Name: "Main",
				SourceFile: "App/Main.cs", Language: "csharp",
				Relationships: []types.RelationshipRecord{{
					FromID: "App/Main.cs", ToID: "App.Sub", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "csharp"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{2, 0})
		if r.ToID != "cccsns000000001" && r.ToID != "cccsns000000002" {
			t.Fatalf("C# namespace import bound %q, want one of the two namespace members", r.ToID)
		}
		assertImportTier(t, r, st, r.ToID, BindTierImportNamespaceRepresentative)
	})

	t.Run("JS `<module>.default` substitutes the file basename — a guess", func(t *testing.T) {
		// The extractor minted the leaf `default`. The rung throws it away
		// and matches the module's last path segment instead, case-folded.
		// `ThemedText` is never named by the edge.
		recs := []types.EntityRecord{
			{ID: "jjjjsdef0000001", Kind: "Component", Name: "ThemedText",
				SourceFile: "components/themed-text.tsx", Language: "typescript"},
			{
				ID: "jjjjsdef0000002", Kind: "SCOPE.Component", Name: "App",
				SourceFile: "app/index.tsx", Language: "typescript",
				Relationships: []types.RelationshipRecord{{
					FromID: "app/index.tsx", ToID: "components.themed-text.default", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "typescript"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "jjjjsdef0000001", BindTierImportJSDefaultBasename)
	})

	t.Run("JS `<dir>/index.default` barrel — the SECOND stamp in the same function", func(t *testing.T) {
		// BindTierImportJSDefaultBasename is stamped at three lines in
		// ResolveDottedImportTargetForJS. The row above reaches the first;
		// this one reaches the second, inside the `basename == "index"`
		// barrel branch, which substitutes the PARENT directory's segment
		// rather than the module's own. Two separate lines in two separate
		// branches — grading one says nothing about the other, which is the
		// lesson this change has now been taught at five different sites.
		//
		// The third stamp is unreachable as a success (it repeats the first
		// lookup with identical arguments after that lookup has missed) and
		// is labelled as such at the site; a mutant there is equivalent, and
		// no fixture is manufactured for it.
		//
		// VARIED against the row above: the module path ends in `index`.
		// HELD CONSTANT: the minted leaf is still `default`, the target is
		// still a single component entity, and the assertion is the same
		// triple.
		recs := []types.EntityRecord{
			{ID: "jjjjsbar0000001", Kind: "Component", Name: "new-note",
				SourceFile: "src/features/new-note/index.tsx", Language: "typescript"},
			{
				ID: "jjjjsbar0000002", Kind: "SCOPE.Component", Name: "App",
				SourceFile: "app/index2.tsx", Language: "typescript",
				Relationships: []types.RelationshipRecord{{
					FromID: "app/index2.tsx", ToID: "src.features.new-note.index.default", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "typescript"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "jjjjsbar0000001", BindTierImportJSDefaultBasename)
	})

	t.Run("JS named export is evidence", func(t *testing.T) {
		// Same wrapper, first rung: the edge names the symbol, so nothing
		// is substituted. Varies from the row above ONLY in the leaf.
		recs := []types.EntityRecord{
			{ID: "jjjjsevd0000001", Kind: "Component", Name: "ThemedText",
				SourceFile: "components/themed-text.tsx", Language: "typescript"},
			{
				ID: "jjjjsevd0000002", Kind: "SCOPE.Component", Name: "App",
				SourceFile: "app/index.tsx", Language: "typescript",
				Relationships: []types.RelationshipRecord{{
					FromID: "app/index.tsx", ToID: "components.themed-text.ThemedText", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "typescript"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "jjjjsevd0000001", "")
	})

	t.Run("Python re-export rebinds to the PARENT module — a guess", func(t *testing.T) {
		// `acme.celery.app` names a module-level binding no entity carries.
		// The rung strips `.app` and points the edge at the acme/celery.py
		// FILE entity instead — a different thing from the one it names.
		recs := []types.EntityRecord{
			{ID: "yyypyreex000001", Kind: "SCOPE.Component", Subtype: "file", Name: "acme/celery.py",
				SourceFile: "acme/celery.py", Language: "python"},
			{
				ID: "yyypyreex000002", Kind: "SCOPE.Component", Name: "init",
				SourceFile: "acme/__init__.py", Language: "python",
				Relationships: []types.RelationshipRecord{{
					FromID: "acme/__init__.py", ToID: "acme.celery.app", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "python"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "yyypyreex000001", BindTierImportPythonReexportParent)
	})

	t.Run("Python whole-path module import is evidence", func(t *testing.T) {
		// The #44 rung one step earlier: the WHOLE minted path names the
		// module, matched exactly. Varies from the row above only in
		// whether the path has a trailing symbol the graph cannot find.
		recs := []types.EntityRecord{
			{ID: "yyypyevd000001", Kind: "SCOPE.Component", Subtype: "file", Name: "acme/celery.py",
				SourceFile: "acme/celery.py", Language: "python"},
			{
				ID: "yyypyevd000002", Kind: "SCOPE.Component", Name: "init",
				SourceFile: "acme/__init__.py", Language: "python",
				Relationships: []types.RelationshipRecord{{
					FromID: "acme/__init__.py", ToID: "acme.celery", Kind: "IMPORTS",
					Properties: types.Props{{K: "language", V: "python"}},
				}},
			},
		}
		r, st := importsBind(t, recs, [2]int{1, 0})
		assertImportTier(t, r, st, "yyypyevd000001", "")
	})
}

// ---------------------------------------------------------------------------
// The Java canonical file tie-break — the sixteenth tier.
//
// Round 2 argued this was evidence because it "deduplicates two records of
// the same class". Review demonstrated the function enforces no such thing:
// modulesForJavaFile strips `*/src/main/java/` ANYWHERE in a path, so two
// Gradle modules collapse into one bucket and an app-module caller can bind
// the lib-module entity. That is E1/E3/E4's shape, and those are guesses.
//
// It has TWO production call sites in different branches of ResolveImports
// — the bare-CALLS rung 1 and the IMPORTS ladder — so both are graded.
// Grading one would say nothing about the other; that is the same lesson
// this change has now been taught four times.
//
// VARIED between the two rows: which call site is exercised (edge Kind
// CALLS vs IMPORTS, and the stub shape each takes).
// HELD CONSTANT: the two colliding Java entities, their Gradle modules,
// the ambiguity that forces the tie-break, and the assertion triple.
// ---------------------------------------------------------------------------

// javaCollidingRecords is the two-Gradle-module collision: `Bar` declared in
// BOTH lib/ and app/, which modulesForJavaFile folds into one `com.acme`
// bucket, flipping (com.acme, Bar) ambiguous. The canonical tie-break then
// prefers the file whose basename matches the class name — true of both, so
// the SCOPE-kind preference decides, and the winner is a cross-module pick.
func javaCollidingRecords(callerRel types.RelationshipRecord) []types.EntityRecord {
	return []types.EntityRecord{
		{ID: "jjjjjava00000001", Kind: "SCOPE.Component", Name: "Bar",
			SourceFile: "lib/src/main/java/com/acme/Bar.java", Language: "java"},
		{ID: "jjjjjava00000002", Kind: "Service", Name: "Bar",
			SourceFile: "app/src/main/java/com/acme/Bar.java", Language: "java"},
		{
			ID: "jjjjjava00000003", Kind: "Operation", Name: "App.run",
			SourceFile: "app/src/main/java/com/acme/App.java", Language: "java",
			Relationships: []types.RelationshipRecord{callerRel},
		},
	}
}

func TestBindTier_JavaCanonicalTiebreak_7071(t *testing.T) {
	t.Run("bare-CALLS rung 1 (ResolveBareCallTarget)", func(t *testing.T) {
		recs := javaCollidingRecords(types.RelationshipRecord{
			FromID: "jjjjjava00000003", ToID: "Bar", Kind: "CALLS",
		})
		// The import binding that makes `Bar` a local name in App.java.
		//
		// `language` is deliberately OMITTED here. The IMPORTS ladder gates
		// its own copy of the tie-break on `language == "java"`, so leaving
		// it off keeps that second site from firing and makes the count
		// below attributable to the CALLS site alone. With it present the
		// count is 2, which is correct behaviour and useless as a grade:
		// it would pass with the CALLS stamp deleted.
		recs[2].Relationships = append(recs[2].Relationships, types.RelationshipRecord{
			FromID: "app/src/main/java/com/acme/App.java", ToID: "com.acme.Bar", Kind: "IMPORTS",
			Properties: types.Props{
				{K: "imported_name", V: "Bar"},
				{K: "local_name", V: "Bar"}, {K: "source_module", V: "com.acme"},
			},
		})
		r, st := importsBind(t, recs, [2]int{2, 0})
		if got := BindTier(r.Properties.Get(types.PropBindTier)); got != BindTierJavaCanonicalFileTiebreak {
			t.Fatalf("%s = %q, want %q (bound to %q, counts %v)",
				types.PropBindTier, got, BindTierJavaCanonicalFileTiebreak, r.ToID, st.BindTierCounts)
		}
		if st.BindTierCounts[BindTierJavaCanonicalFileTiebreak] != 1 {
			t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)", BindTierJavaCanonicalFileTiebreak,
				st.BindTierCounts[BindTierJavaCanonicalFileTiebreak], st.BindTierCounts)
		}
	})

	t.Run("IMPORTS ladder (the second call site)", func(t *testing.T) {
		recs := javaCollidingRecords(types.RelationshipRecord{
			FromID: "app/src/main/java/com/acme/App.java", ToID: "com.acme.Bar", Kind: "IMPORTS",
			Properties: types.Props{
				{K: "imported_name", V: "Bar"}, {K: "language", V: "java"},
				{K: "local_name", V: "Bar"}, {K: "source_module", V: "com.acme"},
			},
		})
		r, st := importsBind(t, recs, [2]int{2, 0})
		if got := BindTier(r.Properties.Get(types.PropBindTier)); got != BindTierJavaCanonicalFileTiebreak {
			t.Fatalf("%s = %q, want %q (bound to %q, counts %v) — this is a DIFFERENT branch "+
				"of ResolveImports from the CALLS site; grading that one says nothing here",
				types.PropBindTier, got, BindTierJavaCanonicalFileTiebreak, r.ToID, st.BindTierCounts)
		}
		if st.BindTierCounts[BindTierJavaCanonicalFileTiebreak] != 1 {
			t.Fatalf("BindTierCounts[%s] = %d, want 1 (%v)", BindTierJavaCanonicalFileTiebreak,
				st.BindTierCounts[BindTierJavaCanonicalFileTiebreak], st.BindTierCounts)
		}
	})
}
