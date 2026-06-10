package ruby

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_ruby_rspec", &rspecExtractor{})
}

type rspecExtractor struct{}

func (e *rspecExtractor) Language() string { return "custom_ruby_rspec" }

var (
	reRspecDescribe = regexp.MustCompile(
		`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+['"]([^'"]+)['"]`,
	)
	reRspecDescribeConst = regexp.MustCompile(
		`(?m)^\s*(?:RSpec\.)?(?:describe|context)\s+([A-Z][A-Za-z0-9_:]*)`,
	)
	reRspecExample = regexp.MustCompile(
		`(?m)^\s*(?:it|specify)\s+['"]([^'"]+)['"]`,
	)
	reRspecLet = regexp.MustCompile(
		`(?m)^\s*let!?\s*\(:([a-z_]+)\)`,
	)
	reRspecHook = regexp.MustCompile(
		`(?m)^\s*(before|after|around)\s*(?:\(:[a-z_]+\))?\s+do`,
	)
	reRspecShared = regexp.MustCompile(
		`(?m)^\s*shared_(?:examples|context)\s+['"]([^'"]+)['"]`,
	)
	reRspecSubject = regexp.MustCompile(
		`(?m)^\s*subject\s*(?::[a-z_]+|\{|\()`,
	)
	reRspecInclude = regexp.MustCompile(
		`(?m)^\s*(?:include_examples|it_behaves_like|include_context)\s+['"]([^'"]+)['"]`,
	)

	// ── Rails request-spec route-by-string capture (#4371) ───────────────────
	// Rails request / integration specs (RSpec) drive the app through HTTP by
	// passing the route as a STRING to a request method, but no edge ever
	// connected that route string to the http_endpoint_definition it exercises.
	// This generalizes the NestJS/supertest (#4351), Python (#4369), and
	// Java/Spring (#4370) e2e-route fixes to Ruby/Rails.
	//
	//	get  '/inspections/123'
	//	post '/inspections', params: { ... }
	//	patch "/inspections/#{id}", params: { ... }
	//	delete "/inspections/1"
	//
	// Group 1 = the RSpec request verb, group 2 = the route string literal. The
	// route MUST start with `/` so a named-route helper (inspections_path — an
	// identifier, not a `/path` literal) and a controller-spec symbol action
	// (get :show — no leading slash) are conservatively skipped. A `#{...}` Ruby
	// interpolation inside a route is left verbatim (the resolver treats a
	// definition `:id`/`{id}` segment as a wildcard; a concrete `/1` likewise
	// matches). The verb-method boundary is anchored to the line start (after
	// optional whitespace) so `.get` / `response.get` receivers and chained
	// matchers never match.
	reRspecRequestRoute = regexp.MustCompile(
		`(?m)^\s*(get|post|put|patch|delete)\s+(?:\(\s*)?['"](/[^'"\n\r]*)['"]`,
	)
)

func (e *rspecExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/ruby")
	_, span := tracer.Start(ctx, "indexer.rspec_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "rspec"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	if file.Language != "ruby" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. describe/context string labels -> SCOPE.Component
	for _, m := range reRspecDescribe.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		ent := makeEntity(label, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_GROUP",
			"group_label", label)
		add(ent)
	}

	// 1b. describe/context with constant names -> SCOPE.Component
	for _, m := range reRspecDescribeConst.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		ent := makeEntity(label, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_GROUP",
			"group_label", label, "is_constant", "true")
		add(ent)
	}

	// 2. it/specify examples -> SCOPE.Operation/function
	for i, m := range reRspecExample.FindAllStringSubmatchIndex(src, -1) {
		label := src[m[2]:m[3]]
		// Deduplicate by position-based name to allow same label in different groups
		name := fmt.Sprintf("%s#%d", label, i)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_EXAMPLE",
			"example_description", label)
		add(ent)
	}

	// 3. let/let! helpers -> SCOPE.Pattern
	for _, m := range reRspecLet.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_LET",
			"let_name", name)
		add(ent)
	}

	// 4. before/after hooks -> SCOPE.Operation/function
	hookCount := 0
	for _, m := range reRspecHook.FindAllStringSubmatchIndex(src, -1) {
		hookType := src[m[2]:m[3]]
		hookCount++
		name := fmt.Sprintf("%s_hook_%d", hookType, hookCount)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_HOOK",
			"hook_type", hookType)
		add(ent)
	}

	// 5. shared_examples/shared_context -> SCOPE.Component
	for _, m := range reRspecShared.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Component", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_SHARED")
		add(ent)
	}

	// 6. subject blocks -> SCOPE.Pattern
	subjectCount := 0
	for _, m := range reRspecSubject.FindAllStringIndex(src, -1) {
		subjectCount++
		name := fmt.Sprintf("subject_%d", subjectCount)
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_SUBJECT")
		add(ent)
	}

	// 7. include_examples / it_behaves_like -> SCOPE.Pattern (reference)
	for _, m := range reRspecInclude.FindAllStringSubmatchIndex(src, -1) {
		name := "include:" + src[m[2]:m[3]]
		ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "rspec", "provenance", "INFERRED_FROM_RSPEC_INCLUDE",
			"shared_group", src[m[2]:m[3]])
		add(ent)
	}

	// 8. Rails request-spec route-by-string calls (#4371). When this spec drives
	//    the app through HTTP by calling a route by string (get '/x/1', post
	//    '/x', ...), emit ONE one-per-file test_suite carrying the `VERB route`
	//    pairs on an `e2e_route_calls` property — the exact shape the shared
	//    resolve pass (engine.linkE2ERouteTestsToEndpoints, #4351/#4369/#4370)
	//    consumes to emit a finer-grained TESTS edge to the specific
	//    http_endpoint_definition (synthesized from routes.rb) the spec
	//    exercises. Resolution is deferred to resolve-time because only there is
	//    the cross-file endpoint index available (the route is defined in
	//    config/routes.rb, far from the spec) — merge-stable.
	//
	//    RSpec has no one-suite-per-file collapse like the Go/JS/Python/Java
	//    extractors (#4358/#4343), so this is a NEW, additional node minted only
	//    when route calls are present; it does not disturb the per-construct
	//    entities above. A full RSpec test-suite collapse à la #4358 is left as a
	//    follow-up (ref #4334).
	if routeCalls := collectRailsRequestSpecRouteCalls(src); len(routeCalls) > 0 {
		suite := makeEntity("rspec_request_suite:"+rspecFileBaseName(file.Path),
			"SCOPE.Pattern", "test_suite", file.Path, file.Language, 1)
		setProps(&suite, "framework", "rspec",
			"provenance", "INFERRED_FROM_RSPEC_REQUEST_ROUTE",
			"test_framework", "rspec",
			"e2e_route_calls", strings.Join(routeCalls, "\n"),
			"e2e_route_count", fmt.Sprintf("%d", len(routeCalls)),
		)
		add(suite)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}

// collectRailsRequestSpecRouteCalls extracts every Rails request/integration
// spec route-by-string call in an RSpec file and returns de-duplicated
// `VERB route` lines — the exact shape the shared resolve pass consumes
// (engine.linkE2ERouteTestsToEndpoints, #4351/#4369/#4370). Only literal
// `/...` routes are captured: named-route helpers (`inspections_path`) and
// controller-spec symbol actions (`get :show`) are conservatively skipped
// because they are not leading-slash string literals. The route is normalised
// to a path (scheme+authority and query/fragment stripped, repeated slashes
// collapsed); a `#{...}` Ruby interpolation and concrete ids are preserved
// verbatim (the resolver wildcards `:id`/`{id}` definition segments).
func collectRailsRequestSpecRouteCalls(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reRspecRequestRoute.FindAllStringSubmatch(src, -1) {
		verb := strings.ToUpper(strings.TrimSpace(m[1]))
		route := normaliseRubyTestRoute(m[2])
		if route == "" || !strings.HasPrefix(route, "/") {
			continue
		}
		line := verb + " " + route
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// normaliseRubyTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix (http://www.example.com/x → /x), drops a query string
// / fragment, and collapses repeated slashes. Casing and path-param
// placeholders / interpolations are left untouched (the resolver compares
// literals case-insensitively and wildcards `:id`/`{id}` segments). Returns ""
// when no path remains.
func normaliseRubyTestRoute(raw string) string {
	p := strings.TrimSpace(raw)
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			p = rest[slash:]
		} else {
			return ""
		}
	}
	// Drop a query string. A literal `#` fragment marker is rare in a route and
	// collides with Ruby's `#{...}` interpolation, so we only strip `#` when it
	// is NOT the start of an interpolation (`#{`).
	if q := strings.IndexByte(p, '?'); q >= 0 {
		p = p[:q]
	}
	if h := strings.IndexByte(p, '#'); h >= 0 && (h+1 >= len(p) || p[h+1] != '{') {
		p = p[:h]
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// rspecFileBaseName derives a human label from an RSpec file path, e.g.
// `spec/requests/inspections_spec.rb` → `inspections`.
func rspecFileBaseName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	p = strings.TrimSuffix(p, ".rb")
	p = strings.TrimSuffix(p, "_spec")
	return p
}
