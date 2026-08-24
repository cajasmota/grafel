package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6511, provider end — the review defect.
//
// The first pass of this fix stamped "SCOPE.Operation:" onto the PROVIDER
// endpoint (FromID) of the fastapi_depends and litestar_provide sites. That is
// a FABRICATED kind: a FastAPI provider is very often a CLASS, not a function.
// Both of these are idiomatic and both land in the same code path:
//
//	def me(svc: AuthService = Depends()):      # bare Depends() → the annotation
//	def me(svc = Depends(SvcClass)):           # the class passed positionally
//
// Asserting Operation for a class is not merely cosmetic. internal/resolve
// probes byKind[kind][name] BEFORE the kind-agnostic byName tier
// (refs.go LookupStatusHint), so a wrong kind prefix actively PROMOTES a
// wrong-kind, same-named entity ahead of the entity actually being injected.
// Measured on a real index, PR binary vs its parent, with `class AuthService`
// in app/routers.py and an unrelated `def AuthService` in app/legacy.py:
//
//	prefixed : INJECTED_INTO SCOPE.Operation|AuthService (app/legacy.py)  -> ...
//	bare     : INJECTED_INTO SCOPE.Component|AuthService (app/routers.py) -> ...
//
// The prefix bound the edge to an unrelated function in another file.
//
// THE RULE THIS FILE PINS: the provider endpoint carries a kind prefix ONLY
// when the extractor has actually OBSERVED the provider to be a callable —
// i.e. a `def` of that name exists in the file being extracted. Anything the
// extractor has not seen defined (the overwhelmingly common case: an imported
// provider, or a class) stays BARE, which is kind-agnostic: it resolves on
// byName and, when that name is ambiguous, on hintKinds("INJECTED_INTO"),
// which the resolver already points at the component family.
//
// The PR's own reasoning for the dependency-injector site said prefixing
// "would assert a kind the symbol lacks". That applies verbatim here.

const opPrefix6511 = "SCOPE.Operation:"

// providerOf returns the single INJECTED_INTO provider endpoint for src.
func providerOf6511(t *testing.T, path, src string) string {
	t.Helper()
	got := injectedInto(diEdgesAt(t, path, src))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 INJECTED_INTO, got %d: %+v", len(got), got)
	}
	return got[0].FromID
}

// A class provider must NOT be addressed as an operation — neither the
// `Depends(SvcClass)` form nor the bare `Depends()` + annotation form.
func TestPyDIProvider6511_ClassProviderIsNotAddressedAsAnOperation(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "class passed positionally to Depends",
			src: `from fastapi import Depends

class SvcClass:
    pass

@router.get("/x")
def handler(svc = Depends(SvcClass)):
    return svc
`,
			want: "SvcClass",
		},
		{
			name: "bare Depends() resolved from the type annotation",
			src: `from fastapi import Depends
from app.services import AuthService

@router.get("/me")
def me(svc: AuthService = Depends()):
    return svc
`,
			want: "AuthService",
		},
		{
			name: "imported provider whose definition was never seen",
			src: `from fastapi import Depends
from app.deps import get_db

@router.get("/things")
def list_things(db = Depends(get_db)):
    return db
`,
			want: "get_db",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := providerOf6511(t, "app/routers.py", tc.src)
			if strings.HasPrefix(got, opPrefix6511) {
				t.Errorf("provider endpoint %q asserts kind %q for a symbol the extractor "+
					"never observed as a `def`. internal/resolve probes byKind before "+
					"byName, so this prefix PROMOTES a same-named function in an "+
					"unrelated file ahead of the entity actually injected (#6511 review)",
					got, strings.TrimSuffix(opPrefix6511, ":"))
			}
			if got != tc.want {
				t.Errorf("provider endpoint: want bare %q, got %q", tc.want, got)
			}
		})
	}
}

// Same rule on the litestar_provide site — it shares the defect and must share
// the fix.
func TestPyDIProvider6511_LitestarClassProviderIsNotAddressedAsAnOperation(t *testing.T) {
	src := `from litestar import Controller, get
from litestar.di import Provide
from app.services import AuthService

class ItemController(Controller):
    dependencies = {"svc": Provide(AuthService)}

    @get("/items")
    async def list_items(self, svc: AuthService) -> list:
        return []
`
	got := providerOf6511(t, "app/controllers/items.py", src)
	if strings.HasPrefix(got, opPrefix6511) {
		t.Errorf("litestar provider endpoint %q asserts Operation for a class provider", got)
	}
	if got != "AuthService" {
		t.Errorf("litestar provider endpoint: want bare %q, got %q", "AuthService", got)
	}
}

// The positive half of the rule, so the gate is not vacuously satisfied by
// "never prefix anything". When the provider IS observed as a callable — a
// `def` of that name in the very file being extracted — the Operation kind is
// a fact, not an assertion, and it is emitted.
func TestPyDIProvider6511_LocallyDefinedCallableProviderKeepsItsKind(t *testing.T) {
	src := `from fastapi import Depends

def get_db():
    yield None

@router.get("/things")
def list_things(db = Depends(get_db)):
    return db
`
	got := providerOf6511(t, "app/routers/things.py", src)
	want := opPrefix6511 + "get_db"
	if got != want {
		t.Errorf("provider endpoint for a provider DEFINED IN THIS FILE: want %q, got %q — "+
			"the kind is observed here, not fabricated, and dropping it would leave the "+
			"rule vacuous", want, got)
	}
}

// The dependency-injector @inject provider is a container attribute (a DI
// token). It was already bare and must stay bare — the local-`def` rule must
// not start prefixing it just because a same-named `def` happens to exist.
func TestPyDIProvider6511_InjectTokenStaysBareEvenBesideASameNamedDef(t *testing.T) {
	src := `from dependency_injector.wiring import inject, Provide

def service():
    return None

@inject
def main(svc: Service = Provide[Container.service]):
    return svc
`
	got := providerOf6511(t, "app/services/main.py", src)
	if got != "service" {
		t.Errorf("dependency_injector_inject provider endpoint: want the bare token %q, got %q", "service", got)
	}
}

// Node identity must NOT churn as a side effect of re-addressing edge
// endpoints (#6511 review).
//
// addEdge used to build the owner entity's Name out of rel.FromID/rel.ToID.
// Those happened to be the semantic names, so re-addressing the endpoints
// renamed every DI SCOPE.Pattern entity and changed its ComputeID —
// "di:INJECTED_INTO:get_db->list_things@9" would have become
// "di:INJECTED_INTO:SCOPE.Operation:get_db->scope:operation:method:python:
// app/routers/things.py:list_things@9", which additionally NESTS a structural
// ref inside an entity Name. The owner is now built from the semantic
// provider/consumer names, so identity is stable across any future change to
// how the endpoints are addressed.
func TestPyDIProvider6511_OwnerEntityNameIsStableUnderEndpointReaddressing(t *testing.T) {
	src := `from fastapi import Depends

def get_db():
    yield None

@router.get("/things")
def list_things(db = Depends(get_db)):
    return db
`
	ents := diEntities6511(t, "app/routers/things.py", src)
	var names []string
	for _, e := range ents {
		if e.Kind != "SCOPE.Pattern" || e.Properties["via"] != "fastapi_depends" {
			continue
		}
		names = append(names, e.Name)
		// The provider IS a local def here, so the endpoint carries the
		// Operation kind — and the owner name must still not mention it.
		if strings.Contains(e.Name, "SCOPE.") || strings.Contains(e.Name, "scope:") {
			t.Errorf("DI owner entity Name %q embeds an endpoint REF. Node identity "+
				"(and ComputeID) would then churn on every change to endpoint "+
				"addressing, and an entity Name would carry a nested structural ref",
				e.Name)
		}
	}
	if len(names) != 1 {
		t.Fatalf("want exactly 1 fastapi_depends owner entity, got %d: %v", len(names), names)
	}
	const want = "di:INJECTED_INTO:get_db->list_things@7"
	if names[0] != want {
		t.Errorf("DI owner entity Name = %q, want the semantic form %q", names[0], want)
	}
}

// diEntities6511 returns the raw entity records the DI extractor emits.
func diEntities6511(t *testing.T, path, src string) []types.EntityRecord {
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
	return ents
}

// The known-callable gate must count MODULE-LEVEL defs only (review follow-up
// on 9fe52ae0f).
//
// pyLocalDefNames was built on pyFunctions, whose rePyDef starts
// `^[ \t]*def` — the leading `[ \t]*` means an INDENTED def, i.e. a METHOD,
// satisfies the gate. A provider factory method on a container/registry class
// is a real DI idiom, so a class provider whose name collides with any method
// anywhere in the same file was stamped "SCOPE.Operation:" again and the
// byKind-before-byName promotion came straight back — the exact defect the
// gate exists to prevent.
//
// Module-level is strictly more conservative: fewer prefixes, more bare refs,
// and bare is kind-agnostic rather than kind-wrong, which is the whole basis
// of this fix.
func TestPyDIProvider6511_MethodDoesNotMakeAClassProviderCallable(t *testing.T) {
	src := `from fastapi import Depends


class Registry:
    def AuthService(self):
        return None


class AuthService:
    pass


@router.get("/me")
def me(svc: AuthService = Depends()):
    return svc
`
	got := providerOf6511(t, "app/routers.py", src)
	if got != "AuthService" {
		t.Errorf("provider endpoint = %q, want bare %q. `def AuthService` here is a "+
			"METHOD on Registry, not a module-level callable — it is not evidence "+
			"that the injected `class AuthService` is an Operation. Prefixing on it "+
			"reopens the byKind-before-byName promotion of a same-named function in "+
			"an unrelated file", got, "AuthService")
	}
}

// Same shape on the litestar site.
func TestPyDIProvider6511_LitestarMethodDoesNotMakeAClassProviderCallable(t *testing.T) {
	src := `from litestar import Controller, get
from litestar.di import Provide


class Registry:
    def AuthService(self):
        return None


class ItemController(Controller):
    dependencies = {"svc": Provide(AuthService)}

    @get("/items")
    async def list_items(self, svc) -> list:
        return []
`
	got := providerOf6511(t, "app/controllers/items.py", src)
	if got != "AuthService" {
		t.Errorf("litestar provider endpoint = %q, want bare %q — the only `def AuthService` "+
			"in this file is a method", got, "AuthService")
	}
}

// An `async def` at column 0 is still a module-level callable and must still
// qualify, so the tightening is a column rule and not an accidental ban on
// async providers.
func TestPyDIProvider6511_ModuleLevelAsyncDefStillCounts(t *testing.T) {
	src := `from fastapi import Depends


async def get_db():
    yield None


@router.get("/things")
def list_things(db = Depends(get_db)):
    return db
`
	got := providerOf6511(t, "app/routers/things.py", src)
	want := opPrefix6511 + "get_db"
	if got != want {
		t.Errorf("provider endpoint = %q, want %q — an `async def` at column 0 is a "+
			"module-level callable", got, want)
	}
}
