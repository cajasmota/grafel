package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6894 — SCOPE.Route must reach the HTTP panes, and bare "Route" must stay.
//
// Two entity kinds differ only by a "SCOPE." prefix and — unlike the
// Endpoint pair of #6820 — BOTH of them mean the same thing: an HTTP route.
//
//   - bare "Route"       — emitted by the Java route extractors
//     (internal/custom/java/{javalin,vertx,play,struts,akka_http,
//     spring_webflux}_routes.go) and internal/engine/{spring,django}_routes.go.
//   - "SCOPE.Route"      — types.EntityKindRoute, emitted by the Lua routing
//     extractors (internal/custom/lua/{routing,kong}.go — OpenResty, Lapis,
//     Kong), Vaadin @Route pages (internal/custom/java/vaadin_gwt.go) and the
//     engine's utoipa / api-gateway / frontend-route synthesisers.
//
// Both spellings are members of types.AllEntityKinds() — EntityKindRoute
// ("SCOPE.Route") and EntityKindRouteBare ("Route"), added together by #6776
// arm B7 precisely because both are live.
//
// Nine dashboard sites compared the RAW kind against bare "Route" while
// sitting next to a `kind :=` that had already been stripped of the "SCOPE."
// prefix, so they accepted one spelling and silently rejected the other. The
// tenth site, graphstate.go's groupTopFrameworks, compared the STRIPPED kind
// and therefore accepted both — one site disagreeing with nine on the same
// predicate inside one feature, which is what marks this an accident.
//
// The fix compares the stripped kind everywhere, so both spellings are
// accepted at all ten sites. This is a WIDENING, not the repoint #6893 did
// for Endpoint: dropping bare "Route" would be a recall loss for every Java
// route extractor.
//
// Every assertion below reads the EMITTED ARTEFACT — the rendered pane
// payload, the exported OpenAPI document, the search response, the framework
// summary — never an internal counter.
//
// A NOTE ON THE POSITIVE CONTROLS, which is the trap #6893 fell into. Its
// detail-handler control was an http_endpoint_definition, a kind
// types.IsHTTPEndpointKind ALREADY matches, so the control and the arm being
// graded only ever fired together and the arm was ALIVE at three sites while
// every test passed. Here the two controls that grade the Route arm are the
// Route entities themselves:
//
//	kind             IsHTTPEndpointKind  ==httpEndpointKind  ==SCOPE.Endpoint
//	"Route"          no                  no                  no
//	"SCOPE.Route"    no                  no                  no
//
// so NO other clause in any of the ten predicates can match either of them.
// Each is therefore observed on its own. `realHTTPPath` is present too, but
// only as a vacuity guard (does this pane render anything at all?), never as
// the control for the Route arm.
// ---------------------------------------------------------------------------

// bareRoutePath is carried by a bare-"Route" entity, the spelling every Java
// route extractor emits. It grades the RECALL direction: a fix that repointed
// these sites at SCOPE.Route (the shape #6893 used for Endpoint) instead of
// widening to both would drop it.
const bareRoutePath = "/bare/route"

// scopeRoutePath is carried by a "SCOPE.Route" entity — the spelling the nine
// raw-comparison sites rejected outright. It is the entity #6894 is about.
const scopeRoutePath = "/scope/route"

// decoyNonRoutePath is carried by a kind that is NOT an HTTP route at all.
// It grades the FORBIDDEN direction: widening these predicates to accept both
// Route spellings must not widen them to accept anything else. Its path is
// deliberately "/"-prefixed so it survives isHTTPEndpointPath and reaches
// every one of the ten sites — a decoy the downstream filter rejects for us
// would grade nothing.
const decoyNonRoutePath = "/decoy/class"

// route6894Entities returns one repo's worth of entities: the two Route
// spellings, a non-route decoy, and a plain http_endpoint_definition used
// only as a vacuity guard.
func route6894Entities() []graph.Entity {
	return []graph.Entity{
		graph.Entity{
			ID:   "bare-route",
			Name: bareRoutePath,
			Kind: string(types.EntityKindRouteBare), // "Route" — Java route extractors
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      bareRoutePath,
			"framework": "javalin",
		}),
		graph.Entity{
			ID:   "scope-route",
			Name: scopeRoutePath,
			Kind: string(types.EntityKindRoute), // "SCOPE.Route" — Lua/Vaadin/gateway
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      scopeRoutePath,
			"framework": "openresty",
		}),
		graph.Entity{
			ID:   "decoy-class",
			Name: decoyNonRoutePath,
			Kind: string(types.EntityKindClass), // "SCOPE.Class" — not an HTTP route
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      decoyNonRoutePath,
			"framework": "decoyframework",
		}),
		graph.Entity{
			ID:   "real-http-6894",
			Name: "GET " + realHTTPPath,
			Kind: "http_endpoint_definition",
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      realHTTPPath,
			"framework": "gin",
		}),
	}
}

// route6894Server wires a Server holding route6894Entities under group "g".
// withIndex selects between the two independent search code paths
// (search_index.go's side-index and handlers_search.go's linear fallback).
func route6894Server(t *testing.T, withIndex bool) *Server {
	t.Helper()
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	grp := openAPITestGroup("g", route6894Entities())
	if withIndex {
		grp.Search = buildSearchIndex(grp)
		if grp.Search == nil {
			t.Fatal("buildSearchIndex returned nil; the indexed search path would not be exercised")
		}
	}
	srv.graphs.mu.Lock()
	srv.graphs.entries["g"] = &cacheEntry{group: grp, loadedAt: time.Now()}
	srv.graphs.mu.Unlock()
	return srv
}

// assertRoute6894Paths applies the four-way verdict to one pane's path list:
// a vacuity guard, the two Route spellings (each graded alone), and the
// forbidden direction.
func assertRoute6894Paths(t *testing.T, what string, paths []string) {
	t.Helper()
	if !ipc6820Contains(paths, realHTTPPath) {
		t.Fatalf("%s = %v, missing the plain http_endpoint_definition %q; every assertion "+
			"below would be vacuous", what, paths, realHTTPPath)
	}
	if !ipc6820Contains(paths, bareRoutePath) {
		t.Errorf("%s = %v, missing the bare-%q entity %q. Both Route spellings are live "+
			"kinds; every Java route extractor emits the bare one, so dropping it is a "+
			"recall loss.", what, paths, "Route", bareRoutePath)
	}
	if !ipc6820Contains(paths, scopeRoutePath) {
		t.Errorf("%s = %v, missing the %q entity %q (#6894). This site compares the RAW kind "+
			"against bare %q while `kind` beside it has already been stripped of the "+
			"\"SCOPE.\" prefix, so it rejects the SCOPE spelling outright.",
			what, paths, types.EntityKindRoute, scopeRoutePath, "Route")
	}
	if ipc6820Contains(paths, decoyNonRoutePath) {
		t.Errorf("%s = %v contains %q, a %q entity. Accepting both Route spellings must not "+
			"widen this predicate to non-route kinds.",
			what, paths, decoyNonRoutePath, types.EntityKindClass)
	}
}

// ---------------------------------------------------------------------------
// Site 8 — handlers_export_openapi.go: the exported document
// ---------------------------------------------------------------------------

func TestOpenAPIExport_AcceptsBothRouteSpellings(t *testing.T) {
	srv := route6894Server(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/export/g/openapi?format=json", nil)
	req.SetPathValue("group", "g")
	w := httptest.NewRecorder()
	srv.handleExportOpenAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	assertRoute6894Paths(t, "exported OpenAPI paths", keysOf(doc.Paths))
}

// ---------------------------------------------------------------------------
// Sites 1 and 3 — handlers_paths.go / v2_paths.go: the HTTP path panes
// ---------------------------------------------------------------------------

// TestPathsPanes_AcceptBothRouteSpellings covers the v1 and v2 Paths panes.
// Each is a separate predicate in a separate file, so each gets its own
// subtest rather than one loop a single-site fix would satisfy.
func TestPathsPanes_AcceptBothRouteSpellings(t *testing.T) {
	cases := []struct {
		name  string
		paths func(t *testing.T, s *Server) []string
	}{
		{
			name: "v1 /api/paths/{group}",
			paths: func(t *testing.T, s *Server) []string {
				body := serveJSON(t, s.handlePathsList, "/api/paths/g",
					map[string]string{"group": "g"})
				return collectStrings(asSlice(body["paths"]), "path")
			},
		},
		{
			name: "v2 /api/v2/paths/{id}",
			paths: func(t *testing.T, s *Server) []string {
				body := serveJSON(t, s.handleV2PathsList, "/api/v2/paths/g",
					map[string]string{"id": "g"})
				return collectPathsDeep(body["data"])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRoute6894Paths(t, "pane paths", tc.paths(t, route6894Server(t, false)))
		})
	}
}

// ---------------------------------------------------------------------------
// Sites 7 and 9 — search_index.go / handlers_search.go: the path search
// ---------------------------------------------------------------------------

// TestSearchPaths_AcceptBothRouteSpellings exercises BOTH search paths. The
// indexed one and the linear fallback are separate predicates in separate
// files; handleSearch picks between them on grp.Search, so a fix to one would
// leave the other unguarded if they shared a subtest.
func TestSearchPaths_AcceptBothRouteSpellings(t *testing.T) {
	for _, withIndex := range []bool{true, false} {
		name := "linear fallback (grp.Search == nil)"
		if withIndex {
			name = "prebuilt SearchIndex"
		}
		t.Run(name, func(t *testing.T) {
			srv := route6894Server(t, withIndex)
			// "/" matches every fixture path alike, so each one is a
			// candidate and the kind check is what separates them.
			body := serveJSON(t, srv.handleSearch, "/api/search/g?q=%2F",
				map[string]string{"group": "g"})
			assertRoute6894Paths(t, "search paths", collectStrings(asSlice(body["paths"]), "path"))
		})
	}
}

// ---------------------------------------------------------------------------
// Sites 2, 4, 5, 6 — the per-path detail endpoints
// ---------------------------------------------------------------------------

// TestPathDetail_ResolvesBothRouteSpellings covers the four detail handlers,
// each of which selects by hashStr(path) == pathHash. These are the sites
// where the compound-masking failure of #6893 bites: nothing else in the
// response observes the Route arm, so the controls have to be the Route
// entities themselves — and they are, since no other clause in these
// predicates matches either Route spelling.
func TestPathDetail_ResolvesBothRouteSpellings(t *testing.T) {
	handlers := []struct {
		name  string
		fn    func(s *Server) http.HandlerFunc
		url   string
		found func(map[string]any) bool
	}{
		{
			name:  "v1 path detail",
			fn:    func(s *Server) http.HandlerFunc { return s.handlePathDetail },
			url:   "/api/paths/g/",
			found: func(b map[string]any) bool { return b["path"] != nil && b["path"] != "" },
		},
		{
			name:  "v2 path detail",
			fn:    func(s *Server) http.HandlerFunc { return s.handleV2PathDetail },
			url:   "/api/v2/paths/g/",
			found: v2DataHasPath,
		},
		{
			name:  "v2 path posture",
			fn:    func(s *Server) http.HandlerFunc { return s.handleV2PathPosture },
			url:   "/api/v2/paths/g/posture/",
			found: v2DataHasPath,
		},
		{
			name:  "v2 downstream DAG",
			fn:    func(s *Server) http.HandlerFunc { return s.handleV2PathDownstreamDAG },
			url:   "/api/v2/paths/g/dag/",
			found: v2DataHasPath,
		},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			srv := route6894Server(t, false)

			resolves := func(path string) (bool, string) {
				t.Helper()
				hash := hashStr(path)
				req := httptest.NewRequest(http.MethodGet, h.url+hash, nil)
				setDetailPathValues(req, hash)
				w := httptest.NewRecorder()
				h.fn(srv)(w, req)
				if w.Code != http.StatusOK {
					return false, w.Body.String()
				}
				var body map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					return false, w.Body.String()
				}
				return h.found(body), w.Body.String()
			}

			// Vacuity guard: a plain http_endpoint_definition must resolve
			// through this handler at all. NOT the control for the Route arm —
			// IsHTTPEndpointKind already matches it, so it would stay green
			// with the Route arm deleted (the #6893 trap).
			if ok, body := resolves(realHTTPPath); !ok {
				t.Fatalf("the http_endpoint_definition %q did not resolve (body=%s); every "+
					"assertion below would be vacuous", realHTTPPath, body)
			}

			// Control 1 — bare "Route". Matched by no other clause here.
			if ok, body := resolves(bareRoutePath); !ok {
				t.Errorf("the bare-%q entity %q did not resolve (body=%s); every Java route "+
					"extractor emits this spelling", "Route", bareRoutePath, body)
			}

			// Control 2 — "SCOPE.Route". Matched by no other clause here, and
			// the reason #6894 exists.
			if ok, body := resolves(scopeRoutePath); !ok {
				t.Errorf("the %q entity %q did not resolve (body=%s); this handler compares "+
					"the RAW kind, so it rejects the SCOPE spelling outright",
					types.EntityKindRoute, scopeRoutePath, body)
			}

			// The forbidden direction: a non-route kind must NOT resolve.
			if ok, body := resolves(decoyNonRoutePath); ok {
				t.Errorf("the %q entity %q resolved as an HTTP path (body=%s); accepting both "+
					"Route spellings must not admit non-route kinds",
					types.EntityKindClass, decoyNonRoutePath, body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Site 10 — graphstate.go groupTopFrameworks: the group framework summary
// ---------------------------------------------------------------------------

// TestGroupTopFrameworks_AcceptsBothRouteSpellings pins the one site of the
// ten that ALREADY compared the stripped kind. It is the site a fix could most
// easily regress by "making it consistent" in the wrong direction, and the
// asymmetry it creates with its nine siblings is the evidence #6894 rests on.
func TestGroupTopFrameworks_AcceptsBothRouteSpellings(t *testing.T) {
	grp := openAPITestGroup("g", route6894Entities())
	got := groupTopFrameworks(grp, 8)

	if !ipc6820Contains(got, "gin") {
		t.Fatalf("frameworks = %v, missing %q from the http_endpoint_definition; every "+
			"assertion below would be vacuous", got, "gin")
	}
	if !ipc6820Contains(got, "javalin") {
		t.Errorf("frameworks = %v, missing %q — the bare-%q entity must stay counted",
			got, "javalin", "Route")
	}
	if !ipc6820Contains(got, "openresty") {
		t.Errorf("frameworks = %v, missing %q — this site reads the STRIPPED kind and so "+
			"already accepted %q; a fix must not lose that side",
			got, "openresty", types.EntityKindRoute)
	}
	if ipc6820Contains(got, "decoyframework") {
		t.Errorf("frameworks = %v include %q, which reaches the HTTP framework summary only "+
			"via a %q entity", got, "decoyframework", types.EntityKindClass)
	}
}
