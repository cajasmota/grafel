package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/engine/httproutes"
	"github.com/cajasmota/grafel/internal/types"
)

// TestGiraffe_BasicRoute covers the canonical Giraffe `choose [ ... ]` route
// list: `GET >=> route "/users" >=> handler`.
func TestGiraffe_BasicRoute(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> route "/users" >=> listUsers
        POST >=> route "/users" >=> createUser
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users",
	}, "giraffe-basic-route")
}

// TestGiraffe_RoutefTypedParam covers `routef "/users/%i"` printf-typed params,
// which canonicalise to the positional `{}` wildcard.
func TestGiraffe_RoutefTypedParam(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> routef "/users/%i" getUser
        DELETE >=> routef "/users/%i/posts/%s" deletePost
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users/{}",
		"http:DELETE:/users/{}/posts/{}",
	}, "giraffe-routef-typed-param")
}

// TestGiraffe_SaturnRouter covers the Saturn `router { ... }` computation-
// expression DSL with both plain and `:name` colon-param paths.
func TestGiraffe_SaturnRouter(t *testing.T) {
	src := `module App
open Saturn

let apiRouter = router {
    get "/users" listUsers
    get "/users/:id" getUser
    post "/users" createUser
}
`
	ids, _ := runDetect(t, "fsharp", "src/Router.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
	}, "saturn-router")
}

// TestGiraffe_InterpolatedRouteDropped is the honest-exclusion guard: an
// interpolated path must NOT forge an endpoint.
func TestGiraffe_InterpolatedRouteDropped(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> route $"/users/{prefix}" >=> handler
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/Dyn.fs", src)
	for _, id := range ids {
		if id == "http:GET:/" || id == "http:GET:/users" {
			t.Fatalf("interpolated route must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestGiraffe_NonWebFileIgnored is the negative guard: an F# file with no web
// marker that happens to call a `get`-like function must not be misread.
func TestGiraffe_NonWebFileIgnored(t *testing.T) {
	src := `module Cache

let lookup key =
    store |> Map.tryFind key
`
	ids, _ := runDetect(t, "fsharp", "src/Cache.fs", src)
	for _, id := range ids {
		if len(id) > 5 && id[:5] == "http:" {
			t.Fatalf("non-web F# file must not synthesize an endpoint; got %v", ids)
		}
	}
}

// TestGiraffe_CanonicalizeFormat unit-tests the routef `%fmt` → `{}` rewrite.
func TestGiraffe_CanonicalizeFormat(t *testing.T) {
	cases := map[string]string{
		"/users/%i":    "/users/{}",
		"/x/%s/%i":     "/x/{}/{}",
		"/g/%O":        "/g/{}",
		"/users":       "/users",
		"/users/:id":   "/users/{id}",
		"/pct/%%/done": "/pct/%/done",
	}
	for in, want := range cases {
		got := httproutes.Canonicalize(httproutes.FrameworkGiraffe, in)
		if got != want {
			t.Errorf("Canonicalize(giraffe, %q) = %q; want %q", in, got, want)
		}
	}
}

// TestGiraffe_SubRouteFolding (#4940) proves a `subRoute "/api" (...)` mount
// prefix is folded into the nested child routes, and that nesting composes
// left-to-right.
func TestGiraffe_SubRouteFolding(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    subRoute "/api" (
        choose [
            GET >=> route "/users" >=> listUsers
            subRoute "/v1" (
                choose [
                    GET >=> route "/health" >=> health
                ]
            )
        ]
    )
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/api/users",
		"http:GET:/api/v1/health",
	}, "giraffe-subroute-folding")
}

// TestGiraffe_ForwardFolding (#4940) proves a `forward "/admin" (...)` mount
// prefix folds exactly like subRoute.
func TestGiraffe_ForwardFolding(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    forward "/admin" (
        choose [
            POST >=> route "/users" >=> createUser
        ]
    )
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:POST:/admin/users",
	}, "giraffe-forward-folding")
}

// TestGiraffe_RouteStartsWithAndRoutex (#4940) proves the routeStartsWith prefix
// variant emits as a literal path and routex regex bodies canonicalise to `{}`.
func TestGiraffe_RouteStartsWithAndRoutex(t *testing.T) {
	src := `module App
open Giraffe

let webApp =
    choose [
        GET >=> routeStartsWith "/api" >=> apiHandler
        GET >=> routex "/users/(\d+)" idHandler
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/App.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/api",
		"http:GET:/users/{}",
	}, "giraffe-routestartswith-routex")
}

// TestGiraffe_NamedHandlerImplements (#4940) proves a same-file `let`-bound
// HttpHandler named as a route's handler yields an endpoint→handler IMPLEMENTS
// bridge edge (synthesis-time structural ref).
func TestGiraffe_NamedHandlerImplements(t *testing.T) {
	src := `module App
open Giraffe

let listUsers : HttpHandler = fun next ctx -> task { return! json [] next ctx }

let webApp =
    choose [
        GET >=> route "/users" >=> listUsers
    ]
`
	_, res := runDetect(t, "fsharp", "src/App.fs", src)
	found := false
	for _, r := range res.Relationships {
		if r.Kind == implementsEdgeKind &&
			r.Properties["pattern_type"] == "http_endpoint_synthesis_time_bridge" &&
			r.Properties["path"] == "/users" &&
			r.Properties["verb"] == "GET" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a synthesis-time IMPLEMENTS bridge for named handler listUsers -> GET /users; rels=%+v", res.Relationships)
	}
}

// TestGiraffe_SaturnNamedHandlerImplements (#4940) is the Saturn-router analogue
// of the named-handler bridge.
func TestGiraffe_SaturnNamedHandlerImplements(t *testing.T) {
	src := `module App
open Saturn

let listUsers : HttpHandler = fun next ctx -> task { return! json [] next ctx }

let apiRouter = router {
    get "/users" listUsers
}
`
	_, res := runDetect(t, "fsharp", "src/Router.fs", src)
	found := false
	for _, r := range res.Relationships {
		if r.Kind == implementsEdgeKind &&
			r.Properties["pattern_type"] == "http_endpoint_synthesis_time_bridge" &&
			r.Properties["path"] == "/users" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a synthesis-time IMPLEMENTS bridge for Saturn named handler listUsers -> GET /users; rels=%+v", res.Relationships)
	}
}

// TestGiraffe_E2ERouteTestLinkage is the end-to-end RED→GREEN proof (#4749
// validation). A Giraffe route GET /users is represented as an
// http_endpoint_definition; an fsharp-testserver test_suite carrying
// `e2e_route_calls = "GET /users"` must yield a TESTS edge from the suite to
// that endpoint via the shared linkE2ERouteTestsToEndpoints pass.
func TestGiraffe_E2ERouteTestLinkage(t *testing.T) {
	def := types.EntityRecord{
		Kind:       httpEndpointDefinitionKind,
		Name:       "http:GET:/users",
		SourceFile: "src/App.fs",
		Language:   "fsharp",
		Properties: map[string]string{
			"verb":         "GET",
			"path":         "/users",
			"framework":    "giraffe",
			"pattern_type": "http_endpoint_synthesis",
		},
	}
	suite := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		Name:       "UsersTests",
		SourceFile: "tests/UsersTests.fs",
		Language:   "fsharp",
		Properties: map[string]string{
			"framework":       "fsharp-testserver",
			"e2e_route_calls": "GET /users",
		},
	}

	merged := []types.EntityRecord{def, suite}
	resolved, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.E2ERouteTestEdges < 1 {
		t.Fatalf("expected >=1 e2e route-test edge for Giraffe GET /users; got %d", stats.E2ERouteTestEdges)
	}

	found := false
	for i := range resolved {
		if resolved[i].Name != "UsersTests" {
			continue
		}
		for _, r := range resolved[i].Relationships {
			if r.Kind == string(types.RelationshipKindTests) &&
				r.Properties["match_source"] == "e2e_supertest_route" &&
				r.Properties["route"] == "/users" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected TESTS edge from UsersTests suite to GET /users endpoint")
	}
}

// ---------------------------------------------------------------------------
// #5114 — the non-db tail of #4941: Falco / Suave / Oxpecker / ASP.NET
// minimal-API (F#) route extraction, alongside the existing Giraffe/Saturn
// coverage (#4906).
// ---------------------------------------------------------------------------

// TestFSharp5114_Oxpecker covers Oxpecker's Giraffe-compatible verb-combinator
// chains (`GET >=> route "/users"`, `routef "/users/%i"`), which ride the
// existing giraffeRouteRe recogniser.
func TestFSharp5114_Oxpecker(t *testing.T) {
	src := `module App
open Oxpecker

let endpoints =
    choose [
        GET  >=> route "/users"     >=> listUsers
        GET  >=> routef "/users/%i" getUser
        POST >=> route "/users"     >=> createUser
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/Oxpecker.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:GET:/users/{}",
		"http:POST:/users",
	}, "5114-oxpecker")
}

// TestFSharp5114_Falco covers the Falco endpoint DSL — both the bare verb form
// (`get "/users" handler`, riding saturnRouteRe) and the `mapGet` register
// helper (falcoMapRe).
func TestFSharp5114_Falco(t *testing.T) {
	src := `module App
open Falco
open Falco.Routing

let endpoints = [
    get  "/users"     listUsers
    post "/users"     createUser
    mapGet "/health"  healthCheck
]
`
	ids, _ := runDetect(t, "fsharp", "src/Falco.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/health",
	}, "5114-falco")
}

// TestFSharp5114_Suave covers Suave verb-combinator composition with the
// `path`/`pathScan` path combinator (`GET >=> path "/users"`, `POST >=>
// pathScan "/users/%d" handler`).
func TestFSharp5114_Suave(t *testing.T) {
	src := `module App
open Suave
open Suave.Filters
open Suave.Operators

let app =
    choose [
        GET  >=> path "/users"                       >=> listUsers
        POST >=> pathScan "/users/%d" (fun id -> ok)
    ]
`
	ids, _ := runDetect(t, "fsharp", "src/Suave.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users/{}",
	}, "5114-suave")
}

// TestFSharp5114_MinimalApi covers ASP.NET Core minimal-API route registration
// in F# (`app.MapGet("/users", handler)`).
func TestFSharp5114_MinimalApi(t *testing.T) {
	src := `module Program
open Microsoft.AspNetCore.Builder

let app = builder.Build()
app.MapGet("/users", listUsers) |> ignore
app.MapPost("/users", createUser) |> ignore
app.MapDelete("/users/{id}", deleteUser) |> ignore
`
	ids, _ := runDetect(t, "fsharp", "src/Program.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:DELETE:/users/{}",
	}, "5114-minimal-api")
}

// TestFSharp5114_NonWebFileIgnored is the no-match no-op: a plain F# file that
// references none of the web markers emits no endpoint.
func TestFSharp5114_NonWebFileIgnored(t *testing.T) {
	src := `module Math
let add a b = a + b
let path = "/not/a/route"
`
	ids, _ := runDetect(t, "fsharp", "src/Math.fs", src)
	requireNotContains(t, ids, []string{
		"http:GET:/not/a/route",
	}, "5114-non-web-ignored")
}

// TestFSharp5114_WrongLanguageNoOp is the wrong-language no-op: the F# route
// synthesiser must not fire on a non-F# file even with matching markers.
func TestFSharp5114_WrongLanguageNoOp(t *testing.T) {
	src := `app.MapGet("/users", listUsers);
`
	ids, _ := runDetect(t, "ruby", "src/app.rb", src)
	requireNotContains(t, ids, []string{
		"http:GET:/users",
	}, "5114-wrong-language")
}

// TestSaturn_ControllerForwarded (#5368) covers a Saturn `controller { }`
// resource block mounted under a prefix via `forward "/users" userController`
// inside a parent router. Each conventional action keyword present in the
// controller body must synthesise its REST verb+path at the mount prefix.
func TestSaturn_ControllerForwarded(t *testing.T) {
	src := `module App
open Saturn

let userController = controller {
    index (fun ctx -> listUsers ctx)
    show (fun ctx id -> getUser ctx id)
    create (fun ctx -> createUser ctx)
    update (fun ctx id -> updateUser ctx id)
    delete (fun ctx id -> deleteUser ctx id)
}

let apiRouter = router {
    forward "/users" userController
}
`
	ids, _ := runDetect(t, "fsharp", "src/Controller.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/users",
		"http:GET:/users/{}",
		"http:POST:/users",
		"http:PUT:/users/{}",
		"http:DELETE:/users/{}",
	}, "saturn-controller-forwarded")
}

// TestSaturn_ControllerActionsCoverage (#5368) proves the full Saturn action
// contract (index/show/add/edit/create/update/patch/delete/deleteAll) maps to
// the correct conventional REST verb+path shapes.
func TestSaturn_ControllerActionsCoverage(t *testing.T) {
	src := `module App
open Saturn

let postController = controller {
    index (fun ctx -> ())
    add (fun ctx -> ())
    edit (fun ctx id -> ())
    patch (fun ctx id -> ())
    deleteAll (fun ctx -> ())
}

let r = router {
    forward "/posts" postController
}
`
	ids, _ := runDetect(t, "fsharp", "src/Posts.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/posts",
		"http:GET:/posts/add",
		"http:GET:/posts/{}/edit",
		"http:PATCH:/posts/{}",
		"http:DELETE:/posts",
	}, "saturn-controller-actions")
}

// TestSaturn_ControllerUnmounted (#5368) honest fallback: a controller with no
// resolvable `forward` mount still emits its single-resource actions at the bare
// convention path (the action↔verb mapping is statically certain even without a
// prefix). The collection-root action (create → POST "/") canonicalises to "/"
// and is correctly dropped (root is not a distinct routable endpoint), so only
// the parametrised `show` action survives.
func TestSaturn_ControllerUnmounted(t *testing.T) {
	src := `module App
open Saturn

let widgetController = controller {
    show (fun ctx id -> ())
    create (fun ctx -> ())
}
`
	ids, _ := runDetect(t, "fsharp", "src/Widget.fs", src)
	requireContains(t, ids, []string{
		"http:GET:/{}",
	}, "saturn-controller-unmounted")
}

// TestSaturn_PipelineNotSynthesised (#5368) honest-exclusion guard: a Saturn
// `pipeline { }` middleware CE is NOT a route emitter, so it must not forge any
// endpoint even though it shares the CE shape with a controller.
func TestSaturn_PipelineNotSynthesised(t *testing.T) {
	src := `module App
open Saturn

let headerPipeline = pipeline {
    set_header "x-foo" "bar"
    plug acceptJson
}
`
	ids, _ := runDetect(t, "fsharp", "src/Pipe.fs", src)
	for _, id := range ids {
		if len(id) > 5 && id[:5] == "http:" {
			t.Fatalf("pipeline CE must not synthesise an endpoint; got %v", ids)
		}
	}
}
