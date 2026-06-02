package links

// http_path_normalized_test.go — tests for the path_normalized cross-repo HTTP
// link strategy (#3752, roadmap oracle-priority #9).
//
// The strategy is the last STATIC orphan-retry stage: it fires only on consumer
// calls that every higher-precision stage (byPath, mount-prefix, case-style,
// url-pattern, param-normalized, literal-fill) left unmatched. It matches a
// client path to a server path when, after (a) stripping a leading version/api
// prefix, (b) collapsing every param token to `{}`, and (c) lower-casing, the
// two paths are byte-equal AND have the same segment count AND the same verb.
// The resulting link is tagged resolve_strategy=path_normalized,
// confidence=heuristic, and a client path that normalizes to MORE THAN ONE
// distinct server endpoint yields NO link (ambiguity guard).
//
// NOTE ON SUBSUMPTION: with the default prefix set (/api,/api/v1,/api/v2,/v1,/v2)
// the existing param_normalized (#2808) + byPath generic-strip (#1409) stages
// already resolve every prefix+param-name permutation BEFORE the orphan sweeps
// run, so path_normalized rarely needs to fire in practice — it is a guarded
// final consolidation net whose distinguishing contribution is the ambiguity
// suppression. The integration tests below install an extra, non-api mount
// prefix into the configurable pathNormPrefixSegments list so the byPath
// generic strip (which only knows the api/version family) misses and the
// path_normalized sweep is exercised end-to-end. The pure-function tests assert
// the full matching contract independently of that wiring.

import (
	"path/filepath"
	"testing"
)

// --- Pure-function contract tests -----------------------------------------

func TestPathNormalizeForMatch(t *testing.T) {
	cases := []struct{ in, want string }{
		// prefix stripping
		{"/api/v1/inspections/{pk}", "/inspections/{}"},
		{"/api/v2/inspections/{id}", "/inspections/{}"},
		{"/api/inspections/{id}", "/inspections/{}"},
		{"/v1/orders", "/orders"},
		{"/v2/orders", "/orders"},
		// param collapse, every style → {}
		{"/users/{id}", "/users/{}"},
		{"/users/:id", "/users/{}"},
		{"/users/<int:id>", "/users/{}"},
		{"/users/{pk}", "/users/{}"},
		// lower-casing
		{"/Users/{UserId}", "/users/{}"},
		// trailing slash stripped
		{"/orders/{id}/", "/orders/{}"},
		// multi-seg multi-param
		{"/api/v1/orders/{order_id}/lines/{line_id}", "/orders/{}/lines/{}"},
		// guard #3: stripping a bare prefix that would empty the path is NOT done
		{"/api", "/api"},
		{"/v1", "/v1"},
		{"/api/v1", "/api/v1"},
		// non-api leading segment is NOT stripped
		{"/orders/{id}", "/orders/{}"},
		{"/v3/orders", "/v3/orders"}, // v3 not in default set
	}
	for _, c := range cases {
		if got := pathNormalizeForMatch(c.in); got != c.want {
			t.Errorf("pathNormalizeForMatch(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestPathNormSegmentCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"/", 0},
		{"/users", 1},
		{"/users/{}", 2},
		{"/users/{}/orders", 3},
	}
	for _, c := range cases {
		if got := pathNormSegmentCount(c.in); got != c.want {
			t.Errorf("pathNormSegmentCount(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestPathNormResolve(t *testing.T) {
	cases := []struct {
		name       string
		cons, prod string
		wantKey    string
		wantOK     bool
	}{
		{"prefix+param-name diff", "/inspections/{id}", "/api/v1/inspections/{pk}", "/inspections/{}", true},
		{"same-seg param-name diff", "/users/{id}", "/users/{pk}", "/users/{}", true},
		{"prefix-only diff", "/v2/orders", "/orders", "/orders", true},
		{"api-prefix-only diff", "/api/v1/orders/{id}", "/orders/{pk}", "/orders/{}", true},
		// NEGATIVE: segment count differs (param vs nested resource)
		{"seg-count differ", "/users/{id}", "/users/{id}/orders", "", false},
		// NEGATIVE: distinct static segment
		{"static seg differ", "/users/{id}", "/clients/{id}", "", false},
		// NEGATIVE: empties
		{"empty consumer", "", "/orders", "", false},
		{"empty producer", "/orders", "", "", false},
	}
	for _, c := range cases {
		key, ok := pathNormResolve(c.cons, c.prod)
		if ok != c.wantOK || key != c.wantKey {
			t.Errorf("%s: pathNormResolve(%q,%q)=(%q,%v) want (%q,%v)",
				c.name, c.cons, c.prod, key, ok, c.wantKey, c.wantOK)
		}
	}
}

func TestDistinctEndpointCount(t *testing.T) {
	mk := func(verb, path string) *httpEndpointHit {
		return &httpEndpointHit{verb: verb, canonicalPath: path, name: "http:" + verb + ":" + path}
	}
	// Same logical endpoint emitted twice → 1 distinct.
	if n := distinctEndpointCount([]*httpEndpointHit{
		mk("GET", "/api/v1/users/{pk}"), mk("GET", "/api/v1/users/{pk}"),
	}); n != 1 {
		t.Errorf("dup endpoint count=%d want 1", n)
	}
	// Prefix-collision: /api/v1/users/{id} AND /users/{id} both normalize to
	// the client /users/{} but are DISTINCT server endpoints → 2 (ambiguous).
	if n := distinctEndpointCount([]*httpEndpointHit{
		mk("GET", "/api/v1/users/{id}"), mk("GET", "/users/{id}"),
	}); n != 2 {
		t.Errorf("prefix-collision distinct count=%d want 2", n)
	}
}

// --- Integration tests (force the orphan sweep via a custom mount prefix) ---

// withCustomPrefix installs an extra, non-api prefix into the configurable
// pathNormPrefixSegments so the byPath generic-strip (api-family only) misses
// and the path_normalized orphan sweep is exercised. Restores the default on
// cleanup. "/svc" is deliberately NOT recognised by apiPrefixRe / stripAPIPrefix.
func withCustomPrefix(t *testing.T) {
	t.Helper()
	orig := pathNormPrefixSegments
	pathNormPrefixSegments = append([]string{"/svc"}, orig...)
	t.Cleanup(func() { pathNormPrefixSegments = orig })
}

func httpEndpointEntity(id, verb, path, patternType, caller string) map[string]any {
	return httpEndpointEntityFile(id, id+".src", verb, path, patternType, caller)
}

// httpEndpointEntityFile is httpEndpointEntity with an explicit source file so a
// consumer synthetic can share the caller function's file (source_caller
// resolution requires same-file co-location).
func httpEndpointEntityFile(id, file, verb, path, patternType, caller string) map[string]any {
	props := map[string]any{
		"verb":         verb,
		"path":         path,
		"framework":    "test",
		"pattern_type": patternType,
	}
	if caller != "" {
		props["source_caller"] = caller
	}
	return map[string]any{
		"id": id, "name": "http:" + verb + ":" + path, "kind": "http_endpoint",
		"source_file": file, "properties": props,
	}
}

// TestPathNormalized_PrefixAndParamName: client GET /svc/inspections/{id},
// server GET /inspections/{pk}. The /svc prefix is not api-family so byPath /
// param_normalized miss; path_normalized strips /svc, collapses params, matches.
// Asserts the link, strategy=path_normalized, confidence=heuristic.
func TestPathNormalized_PrefixAndParamName(t *testing.T) {
	withCustomPrefix(t)
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "InspView", "kind": "Controller", "source_file": "v.py"},
			httpEndpointEntity("ep1", "GET", "/inspections/{pk}", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	// Rewire the IMPLEMENTS to point at ep1's real id (entity id is "ep1").
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getInsp", "kind": "Function", "source_file": "cep1.src"},
			httpEndpointEntity("cep1", "GET", "/svc/inspections/{id}", "http_endpoint_client_synthesis", "Function:getInsp"),
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-pathnorm-1", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(filepath.Join(home, "groups", "g-pathnorm-1-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	var link *Link
	for i := range doc.Links {
		l := &doc.Links[i]
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatalf("expected a path_normalized cross-repo link frontend::fn1 → backend; links=%+v", doc.Links)
	}
	if got := link.Properties["resolve_strategy"]; got != "path_normalized" {
		t.Errorf("resolve_strategy=%q want path_normalized", got)
	}
	if got := link.Properties[EdgeConfidenceKey]; got != ConfidenceHeuristic {
		t.Errorf("confidence marker=%q want %q", got, ConfidenceHeuristic)
	}
	if got := link.Properties["normalized_path"]; got != "/inspections/{}" {
		t.Errorf("normalized_path=%q want /inspections/{}", got)
	}
}

// TestPathNormalized_SameSegParamNameDiff: client /svc/users/{id} ↔ server
// /users/{pk} — pure param-name difference under a non-api prefix.
func TestPathNormalized_SameSegParamNameDiff(t *testing.T) {
	withCustomPrefix(t)
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "u.py"},
			httpEndpointEntity("ep1", "GET", "/users/{pk}", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "cep1.src"},
			httpEndpointEntity("cep1", "GET", "/svc/users/{id}", "http_endpoint_client_synthesis", "Function:getUser"),
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-pathnorm-2", root, home); err != nil {
		t.Fatal(err)
	}
	doc, _ := readDoc(filepath.Join(home, "groups", "g-pathnorm-2-links.json"))
	found := false
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Properties["resolve_strategy"] == "path_normalized" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected path_normalized link for same-seg param-name diff; links=%+v", doc.Links)
	}
}

// TestPathNormalized_AmbiguousNoLink: TWO servers — /svc-prefixed bare and an
// /api/v1-prefixed — both normalize to the client /users/{}. The client call is
// /svc/users/{id}. Two DISTINCT server endpoints share the normalized key →
// AMBIGUOUS → NO link, and misses[path_normalized_ambiguous] is recorded.
func TestPathNormalized_AmbiguousNoLink(t *testing.T) {
	withCustomPrefix(t)
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserViewA", "kind": "Controller", "source_file": "a.py"},
			httpEndpointEntity("ep1", "GET", "/users/{pk}", "http_endpoint_synthesis", ""),
			{"id": "h2", "name": "UserViewB", "kind": "Controller", "source_file": "b.py"},
			httpEndpointEntity("ep2", "GET", "/api/v1/users/{id}", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{
			{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"},
			{"from_id": "h2", "to_id": "ep2", "kind": "IMPLEMENTS"},
		},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "c.ts"},
			httpEndpointEntity("cep1", "GET", "/svc/users/{id}", "http_endpoint_client_synthesis", "Function:getUser"),
		},
	})
	home := filepath.Join(root, "ag-home")
	graphs, err := loadAllGraphs(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PathsFor(home, "g-pathnorm-amb")
	if err != nil {
		t.Fatal(err)
	}
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The client carries /svc, which byPath cannot strip, so byPath misses both
	// servers (/users/{pk} keys /users/{*}; /api/v1/users/{id} keys /users/{*}
	// via generic strip — neither equals the client's /svc/users/{*}). Both reach
	// the path_normalized sweep and normalize to /users/{}, but they are two
	// DISTINCT server endpoints → ambiguous → no link.
	doc, _ := readDoc(filepath.Join(home, "groups", "g-pathnorm-amb-links.json"))
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" && l.Properties["resolve_strategy"] == "path_normalized" {
			t.Errorf("ambiguous client must NOT be path_normalized-linked, got %+v", l)
		}
	}
	if res.CrossRepoResolveMissesByReason["path_normalized_ambiguous"] != 1 {
		t.Errorf("misses[path_normalized_ambiguous]=%d want 1; full=%v",
			res.CrossRepoResolveMissesByReason["path_normalized_ambiguous"],
			res.CrossRepoResolveMissesByReason)
	}
}

// TestPathNormalized_SegCountDiffNoLink: client /svc/users/{id} (2 segs) vs
// server /users/{pk}/orders (3 segs) — same normalized prefix-stripped stem but
// different segment count → NO path_normalized link.
func TestPathNormalized_SegCountDiffNoLink(t *testing.T) {
	withCustomPrefix(t)
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "OrdView", "kind": "Controller", "source_file": "o.py"},
			httpEndpointEntity("ep1", "GET", "/users/{pk}/orders", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "a.ts"},
			httpEndpointEntity("cep1", "GET", "/svc/users/{id}", "http_endpoint_client_synthesis", "Function:getUser"),
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-pathnorm-seg", root, home); err != nil {
		t.Fatal(err)
	}
	doc, _ := readDoc(filepath.Join(home, "groups", "g-pathnorm-seg-links.json"))
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			t.Errorf("seg-count differ must NOT link, got %+v", l)
		}
	}
}

// TestPathNormalized_VerbMismatchNoLink: client GET /svc/users/{id} vs server
// POST /users/{pk} — same normalized path but different verb → NO link.
func TestPathNormalized_VerbMismatchNoLink(t *testing.T) {
	withCustomPrefix(t)
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "UserView", "kind": "Controller", "source_file": "u.py"},
			httpEndpointEntity("ep1", "POST", "/users/{pk}", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getUser", "kind": "Function", "source_file": "a.ts"},
			httpEndpointEntity("cep1", "GET", "/svc/users/{id}", "http_endpoint_client_synthesis", "Function:getUser"),
		},
	})
	home := filepath.Join(root, "ag-home")
	if _, err := RunAllPasses("g-pathnorm-verb", root, home); err != nil {
		t.Fatal(err)
	}
	doc, _ := readDoc(filepath.Join(home, "groups", "g-pathnorm-verb-links.json"))
	for _, l := range doc.Links {
		if l.Method == MethodHTTP && l.Source == "frontend::fn1" {
			t.Errorf("verb mismatch must NOT link, got %+v", l)
		}
	}
}

// TestPathNormalized_DoesNotRelinkExactMatch: when an exact byPath match already
// resolved a consumer, the path_normalized sweep must not also emit a (duplicate
// or differently-tagged) link for it. Uses the canonical /api/v1 prefix so the
// existing stages resolve it; asserts the strategy is NOT path_normalized.
func TestPathNormalized_DoesNotRelinkExactMatch(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, fixtureGraph{
		Repo: "backend",
		Entities: []map[string]any{
			{"id": "h1", "name": "InspView", "kind": "Controller", "source_file": "v.py"},
			httpEndpointEntity("ep1", "GET", "/api/v1/inspections/{pk}", "http_endpoint_synthesis", ""),
		},
		Edges: []map[string]string{{"from_id": "h1", "to_id": "ep1", "kind": "IMPLEMENTS"}},
	})
	writeFixture(t, root, fixtureGraph{
		Repo: "frontend",
		Entities: []map[string]any{
			{"id": "fn1", "name": "getInsp", "kind": "Function", "source_file": "a.ts"},
			httpEndpointEntity("cep1", "GET", "/inspections/{id}", "http_endpoint_client_synthesis", "Function:getInsp"),
		},
	})
	home := filepath.Join(root, "ag-home")
	graphs, _ := loadAllGraphs(root)
	paths, _ := PathsFor(home, "g-pathnorm-exact")
	res, err := runHTTPPass(graphs, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CrossRepoResolveHitsByStrategy["path_normalized"] != 0 {
		t.Errorf("a pre-resolved consumer must not be re-linked by path_normalized; hits=%v",
			res.CrossRepoResolveHitsByStrategy)
	}
	if res.CrossRepoResolved != 1 {
		t.Errorf("CrossRepoResolved=%d want 1", res.CrossRepoResolved)
	}
}
