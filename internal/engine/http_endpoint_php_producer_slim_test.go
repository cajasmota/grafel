package engine

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers (mirrors collectLaravelSynthetics/collectLaravelMatches in
// http_endpoint_php_producer_test.go for the Slim synthesizer).
// ---------------------------------------------------------------------------

type slimMatch struct {
	method, path, framework, handlerKind, handlerName string
}

func collectSlimMatches(content string) []slimMatch {
	var out []slimMatch
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		out = append(out, slimMatch{method, canonicalPath, framework, handlerKind, handlerName})
	}
	synthesizeSlim(content, emit)
	return out
}

func collectSlimSynthetics(content string) []string {
	var ids []string
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		ids = append(ids, "http:"+method+":"+canonicalPath)
	}
	synthesizeSlim(content, emit)
	sort.Strings(ids)
	return ids
}

// ---------------------------------------------------------------------------
// Fast-path gate: no $app->verb(...) → no output
// ---------------------------------------------------------------------------

func TestSynthSlim_EmptyFile(t *testing.T) {
	ids := collectSlimSynthetics("")
	if len(ids) != 0 {
		t.Errorf("expected 0 synthetics from empty file, got %v", ids)
	}
}

func TestSynthSlim_NoRoutes(t *testing.T) {
	src := `<?php
echo "hello world";
$x = new Foo();
`
	ids := collectSlimSynthetics(src)
	if len(ids) != 0 {
		t.Errorf("expected 0 synthetics, got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Explicit verb routes
// ---------------------------------------------------------------------------

func TestSynthSlim_VerbRoutes(t *testing.T) {
	src := `<?php
require __DIR__ . '/../vendor/autoload.php';

use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Slim\Factory\AppFactory;

$app = AppFactory::create();

$app->get('/users', function (Request $request, Response $response) {
    return $response;
});
$app->post('/users', function (Request $request, Response $response) {
    return $response;
});
$app->get('/users/:id', function (Request $request, Response $response, $args) {
    return $response;
});
$app->put('/users/:id', function (Request $request, Response $response, $args) {
    return $response;
});
$app->delete('/users/:id', function (Request $request, Response $response, $args) {
    return $response;
});

$app->run();
`
	matches := collectSlimMatches(src)
	if len(matches) != 5 {
		t.Fatalf("expected 5 matches, got %d: %+v", len(matches), matches)
	}

	want := map[string]bool{
		"GET /users":         false,
		"POST /users":        false,
		"GET /users/{id}":    false,
		"PUT /users/{id}":    false,
		"DELETE /users/{id}": false,
	}
	for _, m := range matches {
		if m.framework != "slim" {
			t.Errorf("expected framework=slim, got %q for %+v", m.framework, m)
		}
		key := m.method + " " + m.path
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected match %+v", m)
			continue
		}
		want[key] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing expected route %q; got: %+v", k, matches)
		}
	}
}

func TestSynthSlim_ColonParamCanonicalization(t *testing.T) {
	src := `<?php
$app->get('/invoices/:invoiceId/items/:itemId', function () {});
`
	matches := collectSlimMatches(src)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	if matches[0].path != "/invoices/{invoiceId}/items/{itemId}" {
		t.Errorf("path = %q, want /invoices/{invoiceId}/items/{itemId}", matches[0].path)
	}
	if matches[0].method != "GET" {
		t.Errorf("method = %q, want GET", matches[0].method)
	}
}

func TestSynthSlim_DoubleQuotedPath(t *testing.T) {
	src := `<?php
$app->get("/health", function () {});
`
	matches := collectSlimMatches(src)
	if len(matches) != 1 || matches[0].path != "/health" {
		t.Fatalf("expected 1 match for /health, got %+v", matches)
	}
}

func TestSynthSlim_AnyVerb(t *testing.T) {
	src := `<?php
$app->any('/webhook', function () {});
`
	matches := collectSlimMatches(src)
	if len(matches) != 1 || matches[0].method != "ANY" {
		t.Fatalf("expected 1 ANY match, got %+v", matches)
	}
}

// ---------------------------------------------------------------------------
// group()/map() exclusion (#5739 item c, refs #5738 review): slimRouteVerbRe
// intentionally excludes group/map — they register a route SET or dispatch by
// method-list, they are not themselves an HTTP-verb route — but that was only
// ever asserted implicitly via the compiled verb alternation. Pin it down
// explicitly so a future edit to the verb list can't silently reintroduce
// group/map as if they were routes.
// ---------------------------------------------------------------------------

func TestSynthSlim_GroupNotEmitted(t *testing.T) {
	// A genuine verb route ($app->get) sits alongside the $app->group call so
	// phpHasAnySlimRoute trips and synthesizeSlim actually reaches
	// slimRouteVerbRe — otherwise the fast-path gate would short-circuit and
	// this would be a trivially-passing duplicate of the no-routes test.
	src := `<?php
$app->get('/real', function () {});
$app->group('/admin', function () {
    $this->get('/users', function () {});
});
`
	ids := collectSlimSynthetics(src)
	want := []string{"http:GET:/real"}
	if !equalStringSlices(ids, want) {
		t.Errorf("expected exactly the /real verb route emitted and NO synthetic for the group path, got %v", ids)
	}
}

func TestSynthSlim_MapNotEmitted(t *testing.T) {
	// See TestSynthSlim_GroupNotEmitted: the $app->get('/real', ...) is what
	// gets synthesizeSlim past the fast-path gate so slimRouteVerbRe runs and
	// the $app->map exclusion is genuinely exercised.
	src := `<?php
$app->get('/real', function () {});
$app->map(['GET', 'POST'], '/both', function () {});
`
	ids := collectSlimSynthetics(src)
	want := []string{"http:GET:/real"}
	if !equalStringSlices(ids, want) {
		t.Errorf("expected exactly the /real verb route emitted and NO synthetic for the map path, got %v", ids)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Lumen vs Slim framework attribution (#5739 item b, refs #5738 review):
// Lumen's $app->verb(...) idiom is identical to Slim's, so synthesizeSlim
// used to stamp every Lumen endpoint framework="slim". A Lumen marker in the
// source now relabels those endpoints framework="lumen"; a genuine Slim
// source is unaffected.
// ---------------------------------------------------------------------------

func TestSynthSlim_LumenSourceLabeledLumen(t *testing.T) {
	src := `<?php
// bootstrap/app.php
$app = new Laravel\Lumen\Application(
    dirname(__DIR__)
);

$app->get('/users', function () {});
$app->post('/users', 'UserController@store');
`
	matches := collectSlimMatches(src)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.framework != "lumen" {
			t.Errorf("expected framework=lumen for Lumen-marked source, got %q for %+v", m.framework, m)
		}
	}
}

func TestSynthSlim_SlimSourceStillLabeledSlim(t *testing.T) {
	src := `<?php
use Slim\Factory\AppFactory;

$app = AppFactory::create();
$app->get('/users', function () {});
`
	matches := collectSlimMatches(src)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	if matches[0].framework != "slim" {
		t.Errorf("expected framework=slim, got %q", matches[0].framework)
	}
}
