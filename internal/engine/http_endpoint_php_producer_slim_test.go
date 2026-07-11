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
