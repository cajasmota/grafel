package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// #6820 — Electron IPC channels must not be presented as HTTP endpoints.
//
// Two entity kinds differ only by a "SCOPE." prefix and mean different things:
//
//   - SCOPE.Endpoint  — the HTTP entrypoint kind (types.EntityKindEndpoint).
//   - bare "Endpoint" — declared ONLY by the Electron rule pack
//     (internal/engine/rules/javascript_typescript/frameworks/electron.yaml
//     lines 41/46/52 — ipcMain / ipcRenderer / contextBridge), where it names
//     an Electron IPC channel. It is never an HTTP route and never had a URL.
//
// Ten dashboard sites keyed on the BARE spelling, so IPC channels rendered in
// the HTTP panes and were written into the exported OpenAPI document — a
// published artefact people read to enumerate HTTP attack surface.
//
// Note the pre-fix predicate `e.Kind == "Endpoint"` compared the RAW kind, so
// it admitted *only* the Electron kind and never matched SCOPE.Endpoint at
// all: the panes accepted exactly the wrong one of the pair.
//
// The tests below assert the EMITTED ARTEFACT (rendered pane payload, exported
// OpenAPI document, search response, framework summary) rather than any
// internal counter, and every absence assertion is paired with a positive
// control in the same payload so the loop can never pass vacuously.
// ---------------------------------------------------------------------------

// ipcChannelPathLike is the Electron IPC channel whose *name* is shaped like an
// HTTP path. This is the load-bearing fixture for the OpenAPI export and the
// paths list: both apply isHTTPEndpointPath() downstream, which rejects a
// channel name such as "app:quit" on its own. Only a channel named like a path
// survives that filter, so only this shape proves the kind check is what
// excludes it. `ipcMain.handle('/api/config', ...)` is legal Electron.
const ipcChannelPathLike = "/api/config"

// ipcChannelPlain is a conventionally-named channel. It reaches the panes that
// do NOT filter by path shape (the search side-index and the linear search
// fallback), where the kind check is the only thing standing in the way.
const ipcChannelPlain = "app:quit"

// realHTTPPath is the positive control: a genuine backend handler that must
// remain in every pane after the fix.
const realHTTPPath = "/api/users"

// scopeEndpointPath is carried by a SCOPE.Endpoint entity — the kind the panes
// are now supposed to key on. It grades the POSITIVE arm of the replacement:
// without it, swapping `e.Kind == "Endpoint"` for a constant that nothing in
// the fixture matches would be indistinguishable from deleting the clause.
const scopeEndpointPath = "/api/scoped"

// ipc6820Entities returns one repo's worth of entities: two Electron IPC
// channels exactly as the electron.yaml source_patterns emit them (bare kind,
// no "path" property — the channel name IS the entity name, and the engine
// stamps framework=<language> and pattern_type=yaml_driven on every
// source-pattern entity, see internal/engine/detector.go:450), one real
// http_endpoint_definition, and one SCOPE.Endpoint.
func ipc6820Entities() []graph.Entity {
	return []graph.Entity{
		graph.Entity{
			ID:   "ipc-pathlike",
			Name: ipcChannelPathLike,
			Kind: "Endpoint", // bare — Electron IPC, NOT HTTP
		}.WithProperties(map[string]string{
			"framework":    "javascript_typescript",
			"pattern_type": "yaml_driven",
		}),
		graph.Entity{
			ID:   "ipc-plain",
			Name: ipcChannelPlain,
			Kind: "Endpoint", // bare — Electron IPC, NOT HTTP
		}.WithProperties(map[string]string{
			"framework":    "javascript_typescript",
			"pattern_type": "yaml_driven",
		}),
		graph.Entity{
			ID:   "real-http",
			Name: "GET " + realHTTPPath,
			Kind: "http_endpoint_definition",
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      realHTTPPath,
			"framework": "gin",
		}),
		graph.Entity{
			ID:   "scope-ep",
			Name: "GET " + scopeEndpointPath,
			Kind: string(types.EntityKindEndpoint), // "SCOPE.Endpoint" — HTTP
		}.WithProperties(map[string]string{
			"method":    "GET",
			"verb":      "GET",
			"path":      scopeEndpointPath,
			"framework": "scopeframework",
		}),
	}
}

// ipc6820Server wires a Server holding ipc6820Entities under group "g".
// withIndex controls whether the pre-built SearchIndex is attached, which
// selects between the two independent search code paths (search_index.go's
// side-index and handlers_search.go's linear fallback).
func ipc6820Server(t *testing.T, withIndex bool) *Server {
	t.Helper()
	srv, err := NewServer(DefaultConfig(), newFakeStore())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	grp := openAPITestGroup("g", ipc6820Entities())
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

// serveJSON runs h against GET url with the given path values and returns the
// decoded body, failing the test on a non-200.
func serveJSON(t *testing.T, h http.HandlerFunc, url string, pathValues map[string]string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200; body=%s", url, w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: bad JSON: %v; body=%s", url, err, w.Body.String())
	}
	return body
}

// ---------------------------------------------------------------------------
// Site 8 (priority) — handlers_export_openapi.go: the exported document
// ---------------------------------------------------------------------------

// TestOpenAPIExport_ExcludesElectronIPCChannels is the priority half of #6820.
// The OpenAPI document is a published artefact; an IPC channel in it is a
// false claim about the HTTP surface a service exposes.
func TestOpenAPIExport_ExcludesElectronIPCChannels(t *testing.T) {
	srv := ipc6820Server(t, false)
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

	// Positive control FIRST. Without it the absence check below could pass on
	// an empty document — a vacuous guard that reads like a real one.
	if _, ok := doc.Paths[realHTTPPath]; !ok {
		t.Fatalf("exported paths %v are missing the real endpoint %q; the absence assertion "+
			"below would be vacuous", keysOf(doc.Paths), realHTTPPath)
	}
	if _, ok := doc.Paths[scopeEndpointPath]; !ok {
		t.Errorf("exported paths %v are missing the SCOPE.Endpoint entry %q; the export must "+
			"key on SCOPE.Endpoint, not merely drop the bare kind", keysOf(doc.Paths), scopeEndpointPath)
	}

	// The forbidden direction.
	if _, ok := doc.Paths[ipcChannelPathLike]; ok {
		t.Errorf("exported OpenAPI paths contain %q, an Electron IPC channel (bare %q kind) — "+
			"it is not an HTTP endpoint and never had a URL; paths=%v",
			ipcChannelPathLike, "Endpoint", keysOf(doc.Paths))
	}
	if _, ok := doc.Paths[ipcChannelPlain]; ok {
		t.Errorf("exported OpenAPI paths contain the IPC channel %q; paths=%v",
			ipcChannelPlain, keysOf(doc.Paths))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Sites 1 and 3 — handlers_paths.go / v2_paths.go: the HTTP path panes
// ---------------------------------------------------------------------------

// TestPathsPanes_ExcludeElectronIPCChannels covers both the v1 Paths pane and
// the v2 Paths pane. Each is a separate predicate in a separate file, so each
// gets its own subtest rather than one loop that a single fix would satisfy.
func TestPathsPanes_ExcludeElectronIPCChannels(t *testing.T) {
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
				// The v2 payload nests routes inside a per-backend tree rather
				// than a flat array, so gather every "path" field at any depth.
				// Keyed on the field name, NOT a whole-body substring match:
				// a substring assertion over the whole response would survive
				// deleting the thing it checks.
				return collectPathsDeep(body["data"])
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := ipc6820Server(t, false)
			paths := tc.paths(t, srv)

			if !ipc6820Contains(paths, realHTTPPath) {
				t.Fatalf("pane paths %v are missing the real endpoint %q; the absence "+
					"assertion below would be vacuous", paths, realHTTPPath)
			}
			if !ipc6820Contains(paths, scopeEndpointPath) {
				t.Errorf("pane paths %v are missing the SCOPE.Endpoint entry %q", paths, scopeEndpointPath)
			}
			if ipc6820Contains(paths, ipcChannelPathLike) {
				t.Errorf("pane paths %v contain %q, an Electron IPC channel, not an HTTP route",
					paths, ipcChannelPathLike)
			}
			if ipc6820Contains(paths, ipcChannelPlain) {
				t.Errorf("pane paths %v contain the IPC channel %q", paths, ipcChannelPlain)
			}
		})
	}
}

// collectPathsDeep walks a decoded JSON value and returns every string sitting
// under a "path" key, at any nesting depth.
func collectPathsDeep(v any) []string {
	var out []string
	var walk func(any)
	walk = func(n any) {
		switch t := n.(type) {
		case map[string]any:
			if s, ok := t["path"].(string); ok && s != "" {
				out = append(out, s)
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

// ---------------------------------------------------------------------------
// Sites 7 and 9 — search_index.go / handlers_search.go: the path search
// ---------------------------------------------------------------------------

// TestSearchPaths_ExcludeElectronIPCChannels exercises BOTH search paths. The
// indexed one and the linear fallback are separate predicates in separate
// files; handleSearch picks between them on grp.Search, so a fix to one would
// leave the other unguarded if they shared a subtest.
//
// Neither of these two sites applies isHTTPEndpointPath downstream, so the
// plainly-named channel "app:quit" reaches them: here the kind check is the
// only thing that can exclude it, and this is the pane where a user first
// SEES an IPC channel offered as an HTTP path.
func TestSearchPaths_ExcludeElectronIPCChannels(t *testing.T) {
	for _, withIndex := range []bool{true, false} {
		name := "linear fallback (grp.Search == nil)"
		if withIndex {
			name = "prebuilt SearchIndex"
		}
		t.Run(name, func(t *testing.T) {
			srv := ipc6820Server(t, withIndex)
			// "a" matches "/api/users", "/api/config", "/api/scoped" and
			// "app:quit" alike, so every fixture is a candidate and the
			// kind check is what separates them.
			body := serveJSON(t, srv.handleSearch, "/api/search/g?q=a", map[string]string{"group": "g"})
			paths := collectStrings(asSlice(body["paths"]), "path")

			if !ipc6820Contains(paths, realHTTPPath) {
				t.Fatalf("search paths %v are missing the real endpoint %q; the absence "+
					"assertion below would be vacuous", paths, realHTTPPath)
			}
			if !ipc6820Contains(paths, scopeEndpointPath) {
				t.Errorf("search paths %v are missing the SCOPE.Endpoint entry %q", paths, scopeEndpointPath)
			}
			if ipc6820Contains(paths, ipcChannelPathLike) {
				t.Errorf("search paths %v contain the IPC channel %q", paths, ipcChannelPathLike)
			}
			if ipc6820Contains(paths, ipcChannelPlain) {
				t.Errorf("search paths %v contain the IPC channel %q — this pane applies no "+
					"path-shape filter, so the entity kind is the only guard", paths, ipcChannelPlain)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Sites 2, 4, 5, 6 — the per-path detail endpoints
// ---------------------------------------------------------------------------

// TestPathDetail_ElectronIPCChannelIsNotResolvable covers the four detail
// handlers, each of which selects by hashStr(path) == pathHash rather than by
// path shape. They are reachable directly by URL: before the fix, a user who
// found an IPC channel in the search pane could click through to a full HTTP
// path detail page, a posture report and a downstream DAG for a channel that
// has no URL.
//
// Each handler is asserted separately because each carries its own copy of the
// kind predicate.
func TestPathDetail_ElectronIPCChannelIsNotResolvable(t *testing.T) {
	ipcHash := hashStr(ipcChannelPathLike)
	realHash := hashStr(realHTTPPath)
	scopeHash := hashStr(scopeEndpointPath)

	handlers := []struct {
		name string
		fn   func(s *Server) http.HandlerFunc
		url  string
		// found reports whether the decoded body describes a resolved path.
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
			srv := ipc6820Server(t, false)

			// Positive control: the SAME handler, driven by the SAME
			// mechanism, must resolve a real HTTP path. Without this the
			// "not resolvable" assertion below passes for a handler that
			// resolves nothing at all.
			okReq := httptest.NewRequest(http.MethodGet, h.url+realHash, nil)
			setDetailPathValues(okReq, realHash)
			okW := httptest.NewRecorder()
			h.fn(srv)(okW, okReq)
			if okW.Code != http.StatusOK {
				t.Fatalf("real path %q: status = %d, want 200; the IPC assertion would be "+
					"vacuous; body=%s", realHTTPPath, okW.Code, okW.Body.String())
			}
			var okBody map[string]any
			if err := json.Unmarshal(okW.Body.Bytes(), &okBody); err != nil {
				t.Fatalf("real path: bad JSON: %v", err)
			}
			if !h.found(okBody) {
				t.Fatalf("real path %q did not resolve (body=%s); the IPC assertion below "+
					"would be vacuous", realHTTPPath, okW.Body.String())
			}

			// SECOND positive control, and it is not redundant with the first.
			//
			// realHTTPPath is an http_endpoint_definition, which
			// types.IsHTTPEndpointKind already matches. So the control above
			// stays green even if the SCOPE.Endpoint arm of this site's
			// predicate is deleted outright: the two conditions only ever fire
			// together, and a compound condition graded as a whole grades
			// neither half. These four detail sites are exactly where that
			// masking bites — unlike the list, search and export sites,
			// nothing else here observes the arm.
			//
			// scopeEndpointPath is carried by a SCOPE.Endpoint entity, which NO
			// other clause in the predicate matches. It resolves only if this
			// site keys on SCOPE.Endpoint, so deleting that arm fails here.
			scopeReq := httptest.NewRequest(http.MethodGet, h.url+scopeHash, nil)
			setDetailPathValues(scopeReq, scopeHash)
			scopeW := httptest.NewRecorder()
			h.fn(srv)(scopeW, scopeReq)
			if scopeW.Code != http.StatusOK {
				t.Fatalf("SCOPE.Endpoint path %q: status = %d, want 200; this handler must "+
					"key on SCOPE.Endpoint, not merely reject the bare Electron kind; body=%s",
					scopeEndpointPath, scopeW.Code, scopeW.Body.String())
			}
			var scopeBody map[string]any
			if err := json.Unmarshal(scopeW.Body.Bytes(), &scopeBody); err != nil {
				t.Fatalf("SCOPE.Endpoint path: bad JSON: %v", err)
			}
			if !h.found(scopeBody) {
				t.Errorf("the SCOPE.Endpoint entity at %q did not resolve (body=%s); without "+
					"this the SCOPE.Endpoint arm of this site is ungraded",
					scopeEndpointPath, scopeW.Body.String())
			}

			// The forbidden direction: the IPC channel must not resolve.
			req := httptest.NewRequest(http.MethodGet, h.url+ipcHash, nil)
			setDetailPathValues(req, ipcHash)
			w := httptest.NewRecorder()
			h.fn(srv)(w, req)
			if w.Code != http.StatusOK {
				return // a 404/400 for the channel is a correct outcome
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				return
			}
			if h.found(body) {
				t.Errorf("the Electron IPC channel %q resolved as an HTTP path; body=%s",
					ipcChannelPathLike, w.Body.String())
			}
			if strings.Contains(w.Body.String(), ipcChannelPathLike) {
				t.Errorf("response for the IPC channel hash mentions %q; body=%s",
					ipcChannelPathLike, w.Body.String())
			}
		})
	}
}

// setDetailPathValues stamps both route-parameter spellings used by the path
// detail handlers: the v1 handler reads {group,pathHash}, the three v2 handlers
// read {id,hash}. Setting both lets one table drive all four.
func setDetailPathValues(r *http.Request, hash string) {
	r.SetPathValue("group", "g")
	r.SetPathValue("pathHash", hash)
	r.SetPathValue("id", "g")
	r.SetPathValue("hash", hash)
}

// v2DataHasPath reports whether a v2 envelope carries a non-empty resolved path.
func v2DataHasPath(b map[string]any) bool {
	if ok, _ := b["ok"].(bool); !ok {
		return false
	}
	data, _ := b["data"].(map[string]any)
	if data == nil {
		return false
	}
	p, _ := data["path"].(string)
	return p != ""
}

// ---------------------------------------------------------------------------
// Site 10 — graphstate.go groupTopFrameworks: the group framework summary
// ---------------------------------------------------------------------------

// TestGroupTopFrameworks_ExcludesElectronIPCChannels pins the group listing's
// "frameworks" summary. Every rule-pack source-pattern entity is stamped with
// framework=<language> by internal/engine/detector.go, so before the fix an
// Electron app reported "javascript_typescript" as one of its top HTTP
// frameworks purely on the strength of its IPC channels.
//
// This site read the SCOPE-STRIPPED kind, so unlike the other nine it already
// accepted SCOPE.Endpoint; the bug here is only that it ALSO accepted the bare
// kind. That makes it the site where a fix could most easily lose the
// SCOPE.Endpoint side by accident, which the positive control below catches.
func TestGroupTopFrameworks_ExcludesElectronIPCChannels(t *testing.T) {
	grp := openAPITestGroup("g", ipc6820Entities())
	got := groupTopFrameworks(grp, 8)

	if !ipc6820Contains(got, "gin") {
		t.Fatalf("frameworks = %v, missing %q from the real http_endpoint_definition; the "+
			"absence assertion below would be vacuous", got, "gin")
	}
	if !ipc6820Contains(got, "scopeframework") {
		t.Errorf("frameworks = %v, missing %q — the SCOPE.Endpoint entity must still be "+
			"counted; this site accepted it before the fix and must keep doing so",
			got, "scopeframework")
	}
	if ipc6820Contains(got, "javascript_typescript") {
		t.Errorf("frameworks = %v include %q, which reached the HTTP framework summary only "+
			"via the two Electron IPC channels", got, "javascript_typescript")
	}
}

// ---------------------------------------------------------------------------
// The destination — where the IPC channels go instead
// ---------------------------------------------------------------------------

// TestElectronIPCChannels_RemainOnTheArchitectureTopology is the recall half of
// #6820, and it is the assertion the HTTP panes cannot make about themselves.
//
// Electron IPC channels are real entities. Removing them from the HTTP panes
// without a destination would be a recall loss invisible to every test above —
// each of those passes just as happily if the entities stop existing. Their
// destination is the compound architecture topology, whose renderableKind /
// nodeTier switches read the SCOPE-STRIPPED kind and therefore accept both
// spellings. Those two sites are deliberately NOT changed by #6820: the IPC
// boundary is architecturally significant and belongs on the edge tier.
//
// If someone later "finishes the job" by pointing renderableKind at
// SCOPE.Endpoint too, this test fails — which is the point.
func TestElectronIPCChannels_RemainOnTheArchitectureTopology(t *testing.T) {
	srv := ipc6820Server(t, false)
	body := serveJSON(t, srv.handleV2TopologyCompound,
		"/api/v2/topology/g/compound", map[string]string{"group": "g"})
	data, _ := body["data"].(map[string]any)
	if data == nil {
		t.Fatalf("no data in topology response: %v", body)
	}
	nodes := asSlice(data["nodes"])
	labels := collectStrings(nodes, "label")

	for _, want := range []string{ipcChannelPathLike, ipcChannelPlain} {
		if !ipc6820Contains(labels, want) {
			t.Errorf("the Electron IPC channel %q is absent from the architecture topology "+
				"(labels=%v). #6820 removes IPC channels from the HTTP panes; it does NOT "+
				"authorise dropping them from the UI, and this is where they land.",
				want, labels)
		}
	}

	// And they land on the edge tier, not silently on a default lane.
	for _, n := range nodes {
		m, _ := n.(map[string]any)
		if m == nil {
			continue
		}
		lbl, _ := m["label"].(string)
		if lbl != ipcChannelPathLike && lbl != ipcChannelPlain {
			continue
		}
		if tier, _ := m["tier"].(string); tier != string(tierEdge) {
			t.Errorf("IPC channel %q is on tier %q, want %q", lbl, tier, tierEdge)
		}
	}
}

// TestElectronIPCChannels_RemainInEntitySearch is the second destination. The
// generic entity index is kind-agnostic — search_index.go appends every entity
// before it builds the HTTP side-index — so an IPC channel stays findable by
// name even though it left the *path* results.
//
// Asserted through the search response for both the indexed and the linear
// path, because those are two different code paths and only one of them
// consults the index.
func TestElectronIPCChannels_RemainInEntitySearch(t *testing.T) {
	for _, withIndex := range []bool{true, false} {
		name := "linear fallback (grp.Search == nil)"
		if withIndex {
			name = "prebuilt SearchIndex"
		}
		t.Run(name, func(t *testing.T) {
			srv := ipc6820Server(t, withIndex)
			body := serveJSON(t, srv.handleSearch, "/api/search/g?q=app",
				map[string]string{"group": "g"})
			labels := collectStrings(asSlice(body["entities"]), "label")
			if !ipc6820Contains(labels, ipcChannelPlain) {
				t.Errorf("the Electron IPC channel %q is not findable in entity search "+
					"(labels=%v); dropping it from the HTTP panes must not remove it "+
					"from the graph's search surface", ipcChannelPlain, labels)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// collectStrings pulls field `key` out of every map in rows.
func collectStrings(rows []any, key string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		m, _ := r.(map[string]any)
		if m == nil {
			continue
		}
		if s, ok := m[key].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ipc6820Contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
