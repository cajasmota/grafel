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
// NOT VARIED, therefore NOT GRADED here: the Java canonical tie-break inside
// rung 1, which is argued as evidence at source rather than measured. It is
// the one judgement call in this pair and is called out in the PR body.
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

// TestBindTier_ImportPassRefusalIsNotMarked_7071 pins the other absence.
// Rung 2 REFUSES when two plain imports disagree, and a refusal leaves the
// stub verbatim — it must carry no marker and increment nothing, because
// absence of the key means "not a guess", which covers "not bound at all".
//
// It also grades the refusal itself, which is the one property rung 2 has
// that rung 3 does not. Without this row, a mutant that made rung 2 take
// the first hit — turning it into rung 3 — would be indistinguishable.
func TestBindTier_ImportPassRefusalIsNotMarked_7071(t *testing.T) {
	recs := []types.EntityRecord{
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
		impRec("iiiimportrefu004", "Operation", "main", "app.py",
			types.RelationshipRecord{FromID: "iiiimportrefu004", ToID: "handler", Kind: "CALLS"}),
	}
	r, st := importsBind(t, recs, [2]int{3, 0})
	if r.ToID != "handler" {
		t.Fatalf("two disagreeing plain imports must leave the stub verbatim, got %q — "+
			"if this bound, rung 2 has lost its ambiguity guard and is now rung 3", r.ToID)
	}
	if got := r.Properties.Get(types.PropBindTier); got != "" {
		t.Fatalf("a refused bind carried %s=%q", types.PropBindTier, got)
	}
	if len(st.BindTierCounts) != 0 {
		t.Fatalf("a refused bind was counted: %v", st.BindTierCounts)
	}
}
