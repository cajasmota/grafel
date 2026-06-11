package php

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
	extractor.Register("custom_php_phpunit_pest", &phpunitPestExtractor{})
}

// phpunitPestExtractor links PHPUnit / Pest feature-test route-by-string calls to
// the http_endpoint_definition they exercise.
//
// Issue #4686 (the PHP slice of epic #4615 / #4672). Laravel feature tests and
// Symfony functional tests drive the app through HTTP by passing the route as a
// STRING to a request method:
//
//	Laravel:  $this->get('/api/v1/x/get_counts')
//	          $this->getJson('/api/v1/x')         // + postJson/putJson/...
//	          $this->post('/api/v1/x', [...])
//	          $this->json('GET', '/api/v1/x')
//	          $this->call('GET', '/api/v1/x')
//	          get('/api/v1/x')                     // Pest global helpers
//	Symfony:  $client->request('GET', '/path')
//
// No edge ever connected that route string to the endpoint definition it
// exercises, so feature/functional tests never credited the controllers they
// cover. This generalises the NestJS/supertest (#4351), Python (#4369),
// Java/Spring (#4370), Ruby/Rails (#4371), Go (#4372/#4718) and C# (#4685) e2e
// route-hit fixes to PHP.
//
// The extractor mints ONE per-file test_suite (SCOPE.Pattern / subtype
// "test_suite") carrying the de-duplicated `VERB route` pairs on an
// `e2e_route_calls` property — the exact shape the shared resolve pass
// (engine.linkE2ERouteTestsToEndpoints) consumes to emit a TESTS edge to the
// specific http_endpoint_definition (synthesized from routes/web.php /
// routes/api.php / #[Route] attributes) the spec exercises. Resolution is
// deferred to resolve-time because only there is the cross-file endpoint index
// available. The node is minted ONLY when ≥1 route call is present, so non-route
// (shape-only) tests credit nothing — honest exclusion preserved.
//
// PHP has no one-suite-per-file collapse, so this is a NEW, additional node; it
// does not disturb the per-method CALLS edges from the tree-sitter extractor.
type phpunitPestExtractor struct{}

func (e *phpunitPestExtractor) Language() string { return "custom_php_phpunit_pest" }

var (
	// ── Laravel verb-suffixed helpers ────────────────────────────────────────
	// $this->get('/x') / ->getJson('/x') / ->postJson('/x', ...) / ->put(...) /
	// ->patchJson(...) / ->delete(...) / ->head(...) / ->options(...). Pest
	// rebinds these onto $this (TestCase), and Pest also exposes bare globals
	// (get('/x'), postJson('/x')) — both forms are captured. The verb-method
	// boundary is anchored so a chained `->getJson` on a non-`$this`/non-global
	// receiver (e.g. `$response->getContent()`) does NOT match: we require the
	// receiver to be `$this->`, `$client->`, or a line-leading bare call.
	rePhpLaravelVerbCall = regexp.MustCompile(
		`(?m)(?:\$this->|\bthis->|^[ \t]*)(get|post|put|patch|delete|head|options)(?:Json)?\s*\(\s*['"](/[^'"\n\r]*)['"]`,
	)

	// ── Laravel explicit-verb helpers: ->json('GET', '/x') / ->call('GET', '/x')
	rePhpLaravelExplicitVerb = regexp.MustCompile(
		`(?m)(?:\$this->|\bthis->)(?:json|call)\s*\(\s*['"]([A-Za-z]+)['"]\s*,\s*['"](/[^'"\n\r]*)['"]`,
	)

	// ── Symfony functional test: $client->request('GET', '/path') ────────────
	rePhpSymfonyRequest = regexp.MustCompile(
		`(?m)->request\s*\(\s*['"]([A-Za-z]+)['"]\s*,\s*['"](/[^'"\n\r]*)['"]`,
	)
)

func (e *phpunitPestExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/php")
	_, span := tracer.Start(ctx, "indexer.phpunit_pest_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "phpunit_pest"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "php" {
		return nil, nil
	}
	if !phpIsTestFile(file.Path) {
		return nil, nil
	}

	src := string(file.Content)
	routeCalls := collectPHPTestRouteCalls(src)
	if len(routeCalls) == 0 {
		span.SetAttributes(attribute.Int("entity_count", 0))
		return nil, nil
	}

	framework := "phpunit"
	if phpLooksLikePest(src) {
		framework = "pest"
	}

	suite := makeEntity("php_feature_suite:"+phpTestFileBaseName(file.Path),
		"SCOPE.Pattern", "test_suite", file.Path, file.Language, 1)
	setProps(&suite, "framework", framework,
		"provenance", "INFERRED_FROM_PHP_FEATURE_ROUTE",
		"test_framework", framework,
		"e2e_route_calls", strings.Join(routeCalls, "\n"),
		"e2e_route_count", fmt.Sprintf("%d", len(routeCalls)),
	)

	span.SetAttributes(attribute.Int("entity_count", 1))
	return []types.EntityRecord{suite}, nil
}

// collectPHPTestRouteCalls extracts every PHPUnit/Pest/Symfony feature-test
// route-by-string call in a PHP test file and returns de-duplicated `VERB route`
// lines — the exact shape the shared resolve pass consumes. Only literal `/...`
// routes are captured: a named-route helper (`route('x')` — an expression, not a
// `/path` literal) is conservatively skipped because the route name is not a
// path the endpoint index can match. Routes are normalised to a path.
func collectPHPTestRouteCalls(src string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(verb, rawRoute string) {
		route := normalisePHPTestRoute(rawRoute)
		if route == "" || !strings.HasPrefix(route, "/") {
			return
		}
		line := strings.ToUpper(strings.TrimSpace(verb)) + " " + route
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, line)
	}

	for _, m := range rePhpLaravelVerbCall.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2]) // verb suffix-helper (HEAD/OPTIONS included)
	}
	for _, m := range rePhpLaravelExplicitVerb.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2]) // json('GET', '/x') / call('GET', '/x')
	}
	for _, m := range rePhpSymfonyRequest.FindAllStringSubmatch(src, -1) {
		add(m[1], m[2]) // $client->request('GET', '/path')
	}
	return out
}

// normalisePHPTestRoute reduces a raw route literal to a path: strips a
// scheme+authority prefix (http://localhost/x → /x), drops a query string /
// fragment, and collapses repeated slashes. Casing and path-param placeholders
// ({id}) are left untouched (the resolver compares literals case-insensitively
// and wildcards template segments). Returns "" when no path remains.
func normalisePHPTestRoute(raw string) string {
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

// phpLooksLikePest reports whether a test file is a Pest spec (uses the bare
// it()/test()/describe() DSL or a `uses(...)` declaration) rather than a PHPUnit
// TestCase subclass. Best-effort label only — both produce the same e2e route
// suite; the label is informational.
func phpLooksLikePest(src string) bool {
	return rePestDSL.MatchString(src) || strings.Contains(src, "uses(")
}

var rePestDSL = regexp.MustCompile(`(?m)^\s*(?:it|test|describe)\s*\(\s*['"]`)

// phpIsTestFile reports whether path is a PHPUnit / Pest test file. Mirrors the
// tree-sitter extractor's isPHPTestFile and the coverage classifier convention:
// a `*Test.php` / `*_test.php` file, or any `.php` under a `/tests/` directory.
func phpIsTestFile(path string) bool {
	slashed := "/" + strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	if strings.Contains(slashed, "/tests/") || strings.Contains(slashed, "/test/") {
		return true
	}
	base := slashed
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return strings.HasSuffix(base, "test.php") || strings.HasSuffix(base, "_test.php")
}

// phpTestFileBaseName derives a human label from a PHP test file path, e.g.
// `tests/Feature/CountsTest.php` → `CountsTest`.
func phpTestFileBaseName(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".php")
}
