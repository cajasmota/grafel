package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6511 — every INJECTED_INTO edge emitted by python_di_graph used to
// carry RAW BARE IDENTIFIERS on both endpoints ("get_db" -> "list_things").
// A bare endpoint has no kind and no file anchor, so the resolver's byName
// tier can bind it to ANY same-named entity anywhere in the repo (or, when
// the name is ambiguous, to nothing at all — the state the issue observed:
// a dangling stub parked in DispositionDynamic and excluded from the
// resolver-bug rate).
//
// The repaired contract, per endpoint:
//
//   - CONSUMER (ToID) — the enclosing `def`. Its file is known EXACTLY (it is
//     the file being extracted), so it is addressed with the canonical
//     file-anchored operation structural-ref that every other
//     known-file operation endpoint in the tree uses,
//     extractor.BuildOperationStructuralRef:
//     scope:operation:method:python:<file>:<fn>
//     internal/resolve resolves this through lookupStructural →
//     lookupLocationKind(file, name, operationKindFamily) and, critically,
//     returns handled=true on a miss — so it can NEVER fall through to the
//     global bare-name tier. That is the whole point: the anchor is what
//     stops a same-named handler in another file from stealing the edge.
//
//   - PROVIDER (FromID) of the fastapi / litestar passes — a callable or class
//     that is very often IMPORTED from another module (the golden fixture's
//     get_db lives in app/deps.py while its consumer lives in
//     app/routers/things.py). Its file is NOT knowable here, so it carries the
//     kind prefix only — "SCOPE.Operation:<name>" — the same address #6444
//     settled on for the fastapi.yaml rule, which binds through BuildIndex's
//     dual-indexing of SCOPE.* kinds under their trimmed key.
//
//   - PROVIDER (FromID) of the dependency-injector @inject pass — a CONTAINER
//     ATTRIBUTE (a DI token), not a source-level operation. It is deliberately
//     left as the token: it is the same address the BINDS edge uses for its
//     token end, and stamping "SCOPE.Operation:" on it would assert a kind the
//     symbol does not have.

// diEdgesAt is diEdges with a caller-chosen file path, so the file anchor on
// the consumer endpoint is actually observable (diEdges hard-codes test.py).
func diEdgesAt(t *testing.T, path, src string) []types.RelationshipRecord {
	t.Helper()
	ext, ok := extractor.Get("python_di_graph")
	if !ok {
		t.Fatal("python_di_graph not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "python",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var rels []types.RelationshipRecord
	for _, e := range ents {
		rels = append(rels, e.Relationships...)
	}
	return rels
}

func injectedInto(rels []types.RelationshipRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindInjectedInto) {
			out = append(out, r)
		}
	}
	return out
}

// Site 1 — internal/custom/python/di_graph.go, the fastapi_depends pass.
func TestPyDIRefs6511_FastAPIDependsEndpointsAreResolvable(t *testing.T) {
	const path = "app/routers/things.py"
	src := `from fastapi import Depends
from app.deps import get_db

@router.get("/things")
def list_things(db = Depends(get_db)):
    return db
`
	got := injectedInto(diEdgesAt(t, path, src))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 INJECTED_INTO, got %d: %+v", len(got), got)
	}
	wantFrom := "SCOPE.Operation:get_db"
	wantTo := extractor.BuildOperationStructuralRef("python", path, "list_things")
	if got[0].FromID != wantFrom {
		t.Errorf("fastapi_depends FromID: want %q, got %q", wantFrom, got[0].FromID)
	}
	if got[0].ToID != wantTo {
		t.Errorf("fastapi_depends ToID: want %q, got %q", wantTo, got[0].ToID)
	}
}

// Site 2 — the dependency_injector_inject pass.
func TestPyDIRefs6511_InjectProvideEndpointsAreResolvable(t *testing.T) {
	const path = "app/services/main.py"
	src := `from dependency_injector.wiring import inject, Provide

@inject
def main(svc: Service = Provide[Container.service]):
    return svc
`
	got := injectedInto(diEdgesAt(t, path, src))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 INJECTED_INTO, got %d: %+v", len(got), got)
	}
	// The provider end stays the container attribute (a DI token, not an
	// operation) — same address the BINDS edge gives its token end.
	if got[0].FromID != "service" {
		t.Errorf("dependency_injector_inject FromID: want %q, got %q", "service", got[0].FromID)
	}
	wantTo := extractor.BuildOperationStructuralRef("python", path, "main")
	if got[0].ToID != wantTo {
		t.Errorf("dependency_injector_inject ToID: want %q, got %q", wantTo, got[0].ToID)
	}
}

// Site 3 — the litestar_provide injection pass.
func TestPyDIRefs6511_LitestarProvideEndpointsAreResolvable(t *testing.T) {
	const path = "app/controllers/items.py"
	src := `from litestar import Controller, get
from litestar.di import Provide

class ItemController(Controller):
    dependencies = {"db": Provide(get_db)}

    @get("/items")
    async def list_items(self, db: DB) -> list:
        return db.all()
`
	got := injectedInto(diEdgesAt(t, path, src))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 INJECTED_INTO, got %d: %+v", len(got), got)
	}
	wantFrom := "SCOPE.Operation:get_db"
	wantTo := extractor.BuildOperationStructuralRef("python", path, "list_items")
	if got[0].FromID != wantFrom {
		t.Errorf("litestar_provide FromID: want %q, got %q", wantFrom, got[0].FromID)
	}
	if got[0].ToID != wantTo {
		t.Errorf("litestar_provide ToID: want %q, got %q", wantTo, got[0].ToID)
	}
}

// Anti-permissive guard. The defect this issue is about is an endpoint that
// matches MORE BROADLY than intended, so the interesting failure direction is
// a consumer ref that has lost its file anchor — "list_things" or
// "SCOPE.Operation:list_things" both bind by bare name and would happily
// attach to a same-named handler in a different file. Every INJECTED_INTO
// consumer endpoint MUST embed the emitting file's path verbatim.
//
// The two same-named handlers below are extracted from two different files;
// the two edges must therefore be DISTINCT.
func TestPyDIRefs6511_ConsumerRefIsFileAnchoredNotBareName(t *testing.T) {
	src := `from fastapi import Depends

@router.get("/x")
def list_things(db = Depends(get_db)):
    return db
`
	a := injectedInto(diEdgesAt(t, "app/routers/things.py", src))
	b := injectedInto(diEdgesAt(t, "app/admin/things.py", src))
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("want 1 INJECTED_INTO per file, got %d and %d", len(a), len(b))
	}
	for _, tc := range []struct {
		path string
		rel  types.RelationshipRecord
	}{{"app/routers/things.py", a[0]}, {"app/admin/things.py", b[0]}} {
		if !strings.Contains(tc.rel.ToID, tc.path) {
			t.Errorf("consumer ref %q does not embed its file %q — an unanchored ref binds by bare name and can steal a same-named handler from another file", tc.rel.ToID, tc.path)
		}
		if tc.rel.ToID == "list_things" || tc.rel.ToID == "SCOPE.Operation:list_things" {
			t.Errorf("consumer ref %q is unanchored (bare-name-resolvable)", tc.rel.ToID)
		}
	}
	if a[0].ToID == b[0].ToID {
		t.Errorf("two same-named handlers in DIFFERENT files produced the SAME consumer ref %q — the file anchor is not doing any work", a[0].ToID)
	}
}
