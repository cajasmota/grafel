package golang

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// httptest_e2e.go — link Go httptest route-by-string tests to the
// http_endpoint_definition they exercise (issue #4371).
//
// Background
// ----------
// Go HTTP integration tests drive the app through HTTP by passing the route as
// a STRING to the stdlib `net/http/httptest` machinery:
//
//	req := httptest.NewRequest(http.MethodPost, "/inspections/123/items", body)
//	rec := httptest.NewRecorder()
//	router.ServeHTTP(rec, req)
//
//	req, _ := http.NewRequest("GET", "/inspections/1", nil)
//	handler.ServeHTTP(w, req)
//
//	srv := httptest.NewServer(router)
//	resp, _ := http.Get(srv.URL + "/inspections/1")
//
// but no edge ever connected that route string to the http_endpoint_definition
// it exercises — so those endpoints looked untested. This generalizes the
// NestJS/supertest fix #4351, the Python fix #4369, and the Java/Spring fix
// #4370 to Go's stdlib httptest, complementing the name-affinity SUT-class
// TESTS edge the testify/ginkgo/gomega suites (#4358) already produce with a
// finer-grained endpoint-level edge.
//
// Extractor side: this extractor emits exactly ONE test_suite entity per
// `*_test.go` file that issues at least one httptest route-by-string call, and
// stamps the `VERB route` pairs onto its `e2e_route_calls` property — the same
// property the Jest (#4351), pytest (#4369), and JUnit (#4370) extractors
// stamp. It is decoupled from the testify suite (which gates on a testify
// marker that a pure-stdlib httptest file lacks); the two suites carry distinct
// names, and the shared resolve pass fires on whichever carries
// `e2e_route_calls`.
//
// Resolve side: the shared pass (engine.linkE2ERouteTestsToEndpoints, #4351) is
// REUSED UNCHANGED. It already gates on Kind||Subtype=="test_suite"
// (isTestSuiteEntity, #4369), wildcards {id}/:id/<int:id> definition segments,
// and tolerates the /api/vN mount prefix. No matcher fork, no new producer Kind.
//
// Conservatism: only literal `/...` routes are captured; a route built from a
// variable / fmt.Sprintf / non-literal expression (no static leading-slash
// path) is dropped, and the shared resolver emits an edge only on a UNIQUE
// verb+route match.

func init() {
	extractor.Register("custom_go_httptest_e2e", &goHTTPTestE2EExtractor{})
}

type goHTTPTestE2EExtractor struct{}

func (e *goHTTPTestE2EExtractor) Language() string { return "custom_go_httptest_e2e" }

var (
	// httptest.NewRequest(<verb>, "/route", body) — verb is the FIRST arg
	// (a "POST" string literal or an http.MethodX const), route the SECOND
	// (string literal). http.NewRequest has the same (verb, route, body) shape.
	// The verb arg is captured as the raw token; resolveHTTPVerbToken maps an
	// http.MethodX const or a quoted literal to the canonical upper verb.
	reGoHTTPTestNewRequest = regexp.MustCompile(
		`\b(?:httptest|http)\.NewRequest\s*\(\s*([^,]+?)\s*,\s*"([^"\n\r]+)"`)

	// httptest.NewRequestWithContext(ctx, <verb>, "/route", body) /
	// http.NewRequestWithContext(...) — same shape but a leading context arg.
	reGoHTTPTestNewRequestCtx = regexp.MustCompile(
		`\b(?:httptest|http)\.NewRequestWithContext\s*\(\s*[^,]+?\s*,\s*([^,]+?)\s*,\s*"([^"\n\r]+)"`)

	// http.Get(srv.URL + "/route") / http.Post(srv.URL+"/route", ...) etc. on
	// an httptest.NewServer — the verb is encoded in the stdlib helper NAME, the
	// route is the string literal concatenated onto the server URL. We match the
	// FIRST quoted "/..."-shaped literal in the call (the path suffix); a bare
	// srv.URL with no literal path suffix yields no route (dropped).
	reGoHTTPTestClientVerb = regexp.MustCompile(
		`\bhttp\.(Get|Post|Head|PostForm)\s*\([^)\n]*?"(/[^"\n\r]*)"`)

	// goHTTPMethodConst maps an http.MethodX constant to its verb.
	goHTTPMethodConst = map[string]string{
		"http.MethodGet":     "GET",
		"http.MethodPost":    "POST",
		"http.MethodPut":     "PUT",
		"http.MethodPatch":   "PATCH",
		"http.MethodDelete":  "DELETE",
		"http.MethodHead":    "HEAD",
		"http.MethodOptions": "OPTIONS",
	}

	// goHTTPClientVerb maps a stdlib http.<Helper> client call to its verb.
	goHTTPClientVerb = map[string]string{
		"Get": "GET", "Post": "POST", "Head": "HEAD", "PostForm": "POST",
	}

	// canonicalHTTPVerbs is the set of bare verb tokens we accept (case-insensitively)
	// from a quoted string-literal method argument.
	canonicalHTTPVerbs = map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
	}
)

func (e *goHTTPTestE2EExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/golang")
	_, span := tracer.Start(ctx, "indexer.go_httptest_e2e_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "go_httptest"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "go" {
		return nil, nil
	}
	src := string(file.Content)
	// Only fire on test files, and only when an httptest/http route-by-string
	// signal is present — keeps the extractor a strict no-op everywhere else.
	if !strings.HasSuffix(file.Path, "_test.go") {
		return nil, nil
	}
	if !strings.Contains(src, "httptest.") &&
		!strings.Contains(src, "http.NewRequest") {
		return nil, nil
	}

	routeCalls := collectGoHTTPTestRouteCalls(src)
	if len(routeCalls) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	ent := makeEntity("httptest_suite:"+goTestBaseName(file.Path), "SCOPE.Pattern", "test_suite",
		file.Path, file.Language, 1)
	setProps(&ent, "framework", "go_httptest", "provenance", "INFERRED_FROM_GO_HTTPTEST_ROUTE",
		"test_framework", "go_httptest",
		"e2e_route_calls", strings.Join(routeCalls, "\n"),
		"e2e_route_count", itoa(len(routeCalls)),
	)

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{ent}, nil
}

// collectGoHTTPTestRouteCalls extracts every Go stdlib httptest / http
// route-by-string call in a `*_test.go` file and returns de-duplicated
// `VERB route` lines — the exact shape the shared resolve pass consumes
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369/#4370). Three shapes are
// covered (#4371):
//
//	httptest.NewRequest(http.MethodPost, "/x/123/items", body)
//	http.NewRequest("GET", "/x/1", nil)               (served via ServeHTTP)
//	http.Get(srv.URL + "/x/1")  / http.Post(srv.URL+"/x", ...)
//
// The route is normalised to a path (scheme+authority and query/fragment
// stripped, repeated slashes collapsed). Concrete ids (`/x/123`) are preserved
// verbatim — the resolver wildcards `{id}`/`:id`/`<int:id>` definition
// segments. A call whose verb does not resolve to a known method, or whose
// route does not reduce to a leading-slash path, is dropped — conservative.
func collectGoHTTPTestRouteCalls(src string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		verb = strings.ToUpper(strings.TrimSpace(verb))
		if verb == "" {
			return
		}
		route := normaliseGoTestRoute(rawRoute)
		if route == "" || !strings.HasPrefix(route, "/") {
			return
		}
		line := verb + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}

	for _, m := range reGoHTTPTestNewRequest.FindAllStringSubmatch(src, -1) {
		if verb := resolveGoHTTPVerbToken(m[1]); verb != "" {
			add(verb, m[2])
		}
	}
	for _, m := range reGoHTTPTestNewRequestCtx.FindAllStringSubmatch(src, -1) {
		if verb := resolveGoHTTPVerbToken(m[1]); verb != "" {
			add(verb, m[2])
		}
	}
	for _, m := range reGoHTTPTestClientVerb.FindAllStringSubmatch(src, -1) {
		if verb, ok := goHTTPClientVerb[m[1]]; ok {
			add(verb, m[2])
		}
	}
	return out
}

// resolveGoHTTPVerbToken maps a raw method argument token to a canonical HTTP
// verb. It accepts an `http.MethodX` constant, a quoted string literal
// ("POST"), or a bare verb token; anything else (a variable, a function call)
// yields "" so the call is dropped — conservative.
func resolveGoHTTPVerbToken(token string) string {
	token = strings.TrimSpace(token)
	if v, ok := goHTTPMethodConst[token]; ok {
		return v
	}
	// Quoted string literal: "POST" / `PUT`.
	if len(token) >= 2 {
		q := token[0]
		if (q == '"' || q == '`') && token[len(token)-1] == q {
			inner := strings.ToUpper(strings.TrimSpace(token[1 : len(token)-1]))
			if canonicalHTTPVerbs[inner] {
				return inner
			}
			return ""
		}
	}
	// Bare verb token (rare, e.g. a local const already upper-cased).
	if up := strings.ToUpper(token); canonicalHTTPVerbs[up] {
		return up
	}
	return ""
}

// normaliseGoTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix (http://127.0.0.1:8080/x → /x), drops a query string
// / fragment, and collapses repeated slashes. Casing and path-param
// placeholders ({id}) are left untouched (the resolver compares literals
// case-insensitively and wildcards template segments). Returns "" when no path
// remains.
func normaliseGoTestRoute(raw string) string {
	p := strings.TrimSpace(raw)
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			p = rest[slash:]
		} else {
			return ""
		}
	}
	if q := strings.IndexAny(p, "?#"); q >= 0 {
		p = p[:q]
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}
