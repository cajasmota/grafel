// Package python_test — the reviewer's end-to-end demonstration of the #6511
// provider-end defect, reduced to a resolver-level gate.
//
// The reviewer measured this with two real indexer binaries. Fixture:
// app/routers.py declares `class AuthService` and `def me(svc: AuthService =
// Depends())`; app/legacy.py declares an UNRELATED `def AuthService`.
//
//	provider addressed "SCOPE.Operation:AuthService"
//	  -> SCOPE.Operation|AuthService in app/legacy.py    ← WRONG, another file
//	provider addressed bare "AuthService"
//	  -> SCOPE.Component|AuthService in app/routers.py   ← the class injected
//
// The mechanism is internal/resolve's tier order: LookupStatusHint probes
// byKind[kind][name] BEFORE the kind-agnostic byName tier, so a fabricated
// kind prefix does not merely fail to help — it actively promotes a
// wrong-kind, same-named entity ahead of the right one. Left bare, the name is
// ambiguous, hintKinds("INJECTED_INTO") narrows to the component family, and
// the class wins.
//
// This file is `package python_test` so it may import internal/resolve: the
// symptom being pinned is WHICH ENTITY THE EDGE LANDS ON, which is the
// resolver's answer, not the extractor's.
package python_test

import (
	"context"
	"testing"

	// Registers the python_di_graph extractor.
	_ "github.com/cajasmota/grafel/internal/custom/python"
	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

const (
	routersFile6511 = "app/routers.py"
	legacyFile6511  = "app/legacy.py"

	// Stand-ins for the production sha256 ids, distinct by construction so
	// the gate cannot pass by ID collision.
	authClassID6511  = "cccc000000000001" // class AuthService, app/routers.py
	legacyFuncID6511 = "dddd000000000001" // def AuthService,   app/legacy.py — UNRELATED
	meFuncID6511     = "cccc000000000002" // def me,            app/routers.py
)

// routersSrc6511 is the reviewer's consumer file: an idiomatic FastAPI
// class-provider injection via bare `Depends()` plus a type annotation.
const routersSrc6511 = `from fastapi import Depends


class AuthService:
    pass


@router.get("/me")
def me(svc: AuthService = Depends()):
    return svc
`

// diRecords6511 runs the REAL python_di_graph extractor over routers.py and
// returns the entity records it emits (each carrying its DI relationship).
func diRecords6511(t *testing.T) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("python_di_graph")
	if !ok {
		t.Fatal("python_di_graph not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: routersFile6511, Content: []byte(routersSrc6511), Language: "python",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// TestPyDIProviderDoesNotBindToUnrelatedSameNamedFunction_6511 is the gate for
// the reviewed regression. The provider end of the INJECTED_INTO edge must
// never land on `def AuthService` in app/legacy.py, a function that has
// nothing to do with the injection.
func TestPyDIProviderDoesNotBindToUnrelatedSameNamedFunction_6511(t *testing.T) {
	recs := []types.EntityRecord{
		// ── app/routers.py — what is actually injected ──────────────────
		{
			ID: authClassID6511, Kind: "SCOPE.Component", Name: "AuthService",
			Subtype: "class", SourceFile: routersFile6511, Language: "python",
		},
		{
			ID: meFuncID6511, Kind: "SCOPE.Operation", Name: "me",
			Subtype: "function", SourceFile: routersFile6511, Language: "python",
		},
		// ── app/legacy.py — an UNRELATED function of the same name ──────
		{
			ID: legacyFuncID6511, Kind: "SCOPE.Operation", Name: "AuthService",
			Subtype: "function", SourceFile: legacyFile6511, Language: "python",
		},
	}
	recs = append(recs, diRecords6511(t)...)

	idx := resolve.BuildIndex(recs)
	resolve.ReferencesEmbedded(recs, idx)

	var got *types.RelationshipRecord
	for i := range recs {
		for j := range recs[i].Relationships {
			if recs[i].Relationships[j].Kind == string(types.RelationshipKindInjectedInto) {
				got = &recs[i].Relationships[j]
			}
		}
	}
	if got == nil {
		t.Fatal("fixture is vacuous: the extractor emitted no INJECTED_INTO edge at all")
	}

	if got.FromID == legacyFuncID6511 {
		t.Fatalf("INJECTED_INTO provider bound to `def AuthService` in %s — an UNRELATED "+
			"function in another file. The edge should reach `class AuthService` in %s. "+
			"Addressing a class provider as SCOPE.Operation makes internal/resolve's "+
			"byKind tier (probed BEFORE byName) hand the edge to the wrong-kind "+
			"same-named entity (#6511 review)", legacyFile6511, routersFile6511)
	}
	if got.FromID != authClassID6511 {
		t.Errorf("INJECTED_INTO provider = %q, want the injected class %q (%s)",
			got.FromID, authClassID6511, routersFile6511)
	}
	if got.ToID != meFuncID6511 {
		t.Errorf("INJECTED_INTO consumer = %q, want `def me` %q (%s) — the consumer end "+
			"is the file-anchored half of #6511 and must stay resolved",
			got.ToID, meFuncID6511, routersFile6511)
	}
}
