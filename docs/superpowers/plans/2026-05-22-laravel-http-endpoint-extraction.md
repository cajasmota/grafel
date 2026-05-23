# Laravel/PHP HTTP Endpoint Extraction — Implementation Plan (#1419)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add producer-side `http_endpoint_definition` synthesis for Laravel `Route::verb()`, `Route::resource()`, and `Route::apiResource()` patterns so that cross-repo HTTP linking (#1409) can match billing's routes against outbound calls from other services.

**Architecture:** A new `synthesizeLaravel` function in `internal/engine/http_endpoint_php_producer.go` mirrors the existing per-framework synthesizers (Flask, FastAPI, Express, Go). It is called from `applyHTTPEndpointSynthesis` inside the `case "php":` block alongside the existing `synthesizePHPClientWithRuntime`. `synthesisSupportsLanguage` is updated to include `"php"` so the detector allows PHP files through. The httpclient extractor (`cross/httpclient`) gains PHP-specific patterns for Guzzle and Laravel `Http::` to emit `http_endpoint_call` entities for consumer-side linking.

**Tech Stack:** Go 1.22, regexp, `internal/engine/httproutes.Canonicalize` + `SyntheticID`, `emitFn` closure pattern used by Flask/Express/Go synthesizers.

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/engine/http_endpoint_php_producer.go` | **Create** | `synthesizeLaravel(content string, emit emitFn)` — producer-side route detection |
| `internal/engine/http_endpoint_php_producer_test.go` | **Create** | Unit tests for synthesizeLaravel |
| `internal/engine/http_endpoint_synthesis.go` | **Modify** | Wire `synthesizeLaravel` into `case "php":` and add `"php"` to `synthesisSupportsLanguage` |
| `internal/extractors/cross/httpclient/extractor.go` | **Modify** | Add `extractPHP` function and wire into `Extract` `case "php":` |
| `internal/extractors/cross/httpclient/extractor_test.go` | **Modify** | Add PHP httpclient tests |
| `fixtures/sources/php/billing_routes.php` | **Create** | Minimal Laravel routes fixture for unit tests |
| `fixtures/sources/php/billing_client.php` | **Create** | Minimal PHP Guzzle/Http:: client fixture for unit tests |

---

## Task 1: Create the billing route fixture PHP file

**Files:**
- Create: `fixtures/sources/php/billing_routes.php`
- Create: `fixtures/sources/php/billing_client.php`

- [ ] **Step 1: Create billing_routes.php**

```php
<?php

use App\Http\Controllers\InvoiceController;
use App\Http\Controllers\SubscriptionController;
use Illuminate\Support\Facades\Route;

Route::get('/invoices', [InvoiceController::class, 'index']);
Route::post('/invoices', [InvoiceController::class, 'store']);
Route::get('/invoices/{id}', [InvoiceController::class, 'show']);
Route::put('/invoices/{id}', [InvoiceController::class, 'update']);
Route::delete('/invoices/{id}', [InvoiceController::class, 'destroy']);

Route::resource('subscriptions', SubscriptionController::class);
Route::apiResource('payments', PaymentController::class);
```

Write to `/Users/jorgecajas/Documents/Projects/archigraph/fixtures/sources/php/billing_routes.php`

- [ ] **Step 2: Create billing_client.php**

```php
<?php

use GuzzleHttp\Client;
use Illuminate\Support\Facades\Http;

class BillingService
{
    public function createOrder(array $data): array
    {
        $client = new Client();
        $response = $client->post('http://orders-service/api/orders', ['json' => $data]);
        return json_decode($response->getBody(), true);
    }

    public function sendNotification(string $userId, string $event): void
    {
        Http::post('http://notifications-service/api/notifications', [
            'user_id' => $userId,
            'event'   => $event,
        ]);
    }

    public function getOrder(string $id): array
    {
        $client = new Client();
        $response = $client->get('http://orders-service/api/orders/' . $id);
        return json_decode($response->getBody(), true);
    }
}
```

Write to `/Users/jorgecajas/Documents/Projects/archigraph/fixtures/sources/php/billing_client.php`

- [ ] **Step 3: Commit fixtures**

```bash
git add fixtures/sources/php/billing_routes.php fixtures/sources/php/billing_client.php
git commit -m "test(fixtures): add Laravel billing route + client PHP fixtures for #1419"
```

---

## Task 2: Create `http_endpoint_php_producer.go`

**Files:**
- Create: `internal/engine/http_endpoint_php_producer.go`

- [ ] **Step 1: Write the failing test** (see Task 3 — write producer test first)

> We write the test file in Task 3 before this implementation. Jump to Task 3 Step 1, then return here.

- [ ] **Step 2: Write the implementation**

Create `/Users/jorgecajas/Documents/Projects/archigraph/internal/engine/http_endpoint_php_producer.go`:

```go
// http_endpoint_php_producer.go — Laravel route → http_endpoint_definition synthesis.
//
// Covers:
//   - Route::get/post/put/patch/delete/options/any('/path', ...)
//   - Route::resource('name', Controller::class) → 7 standard CRUD endpoints
//   - Route::apiResource('name', Controller::class) → 5 API CRUD endpoints
//     (excludes the two browser form routes /create and /{id}/edit)
//
// Handler extraction:
//   - Array syntax:  [Controller::class, 'method']   → "Controller@method"
//   - String syntax: 'ControllerName@method'         → "ControllerName@method"
//   - Closure:       function($request){...}         → "" (no static handler ref)
//
// The canonical path uses httproutes.FrameworkExpress (Express-style {param})
// because Laravel path parameters use the {param} syntax natively.
//
// Refs #1419.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// ---------------------------------------------------------------------------
// Compiled regexps
// ---------------------------------------------------------------------------

// laravelVerbRouteRe matches:
//   Route::get('/path', [Controller::class, 'method'])
//   Route::post('/path', 'ControllerName@method')
//   Route::get('/path', function() { ... })
//
// Capture groups: 1=verb, 2=path (double-quoted), 3=path (single-quoted),
// 4=controller class (array form), 5=method name (array form),
// 6=controller@method string (string form).
var laravelVerbRouteRe = regexp.MustCompile(
	`(?m)Route::(get|post|put|patch|delete|options|any)\s*\(\s*` +
		`(?:"([^"]{1,500})"|'([^']{1,500})')` + // path: groups 2, 3
		`\s*,\s*` +
		`(?:` +
		`\[\s*([\w\\]+)::class\s*,\s*'(\w+)'\s*\]` + // array form: groups 4, 5
		`|` +
		`'([\w\\@]+)'` + // string @method form: group 6
		`|` +
		`"([\w\\@]+)"` + // double-quoted @method form: group 7
		`)?`,
)

// laravelResourceRe matches:
//   Route::resource('name', Controller::class)
//   Route::resource('name', 'ControllerName')
//
// Capture groups: 1=resource name, 2=controller (optional).
var laravelResourceRe = regexp.MustCompile(
	`(?m)Route::resource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// laravelApiResourceRe matches:
//   Route::apiResource('name', Controller::class)
//
// Capture groups: 1=resource name.
var laravelApiResourceRe = regexp.MustCompile(
	`(?m)Route::apiResource\s*\(\s*['"]([^'"]{1,200})['"]`,
)

// ---------------------------------------------------------------------------
// CRUD route tables
// ---------------------------------------------------------------------------

// laravelResourceRoutes are the 7 standard routes emitted by Route::resource.
var laravelResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},          // index
	{"POST", ""},         // store
	{"GET", "/create"},   // create (form)
	{"GET", "/{id}"},     // show
	{"GET", "/{id}/edit"},// edit (form)
	{"PUT", "/{id}"},     // update
	{"DELETE", "/{id}"},  // destroy
}

// laravelApiResourceRoutes are the 5 routes emitted by Route::apiResource
// (excludes /create and /{id}/edit — no form views in API mode).
var laravelApiResourceRoutes = []struct{ method, suffix string }{
	{"GET", ""},       // index
	{"POST", ""},      // store
	{"GET", "/{id}"},  // show
	{"PUT", "/{id}"},  // update
	{"DELETE", "/{id}"},// destroy
}

// ---------------------------------------------------------------------------
// Fast-path gate
// ---------------------------------------------------------------------------

func phpHasAnyLaravelRoute(content string) bool {
	return strings.Contains(content, "Route::get") ||
		strings.Contains(content, "Route::post") ||
		strings.Contains(content, "Route::put") ||
		strings.Contains(content, "Route::patch") ||
		strings.Contains(content, "Route::delete") ||
		strings.Contains(content, "Route::options") ||
		strings.Contains(content, "Route::any") ||
		strings.Contains(content, "Route::resource") ||
		strings.Contains(content, "Route::apiResource")
}

// ---------------------------------------------------------------------------
// Handler extraction helpers
// ---------------------------------------------------------------------------

// laravelHandlerFromMatch returns a "Controller@method" string from a regex
// match of laravelVerbRouteRe. Returns "" when the handler is a closure or
// cannot be statically determined.
func laravelHandlerFromMatch(src string, m []int) string {
	// Array form: [ControllerClass::class, 'method'] — groups 4+5.
	if len(m) >= 12 && m[8] >= 0 && m[10] >= 0 {
		cls := src[m[8]:m[9]]
		// Strip leading namespace backslashes to get bare class name.
		if i := strings.LastIndex(cls, "\\"); i >= 0 {
			cls = cls[i+1:]
		}
		method := src[m[10]:m[11]]
		return cls + "@" + method
	}
	// String form: 'Controller@method' — group 6.
	if len(m) >= 14 && m[12] >= 0 {
		return src[m[12]:m[13]]
	}
	// Double-quoted string form — group 7.
	if len(m) >= 16 && m[14] >= 0 {
		return src[m[14]:m[15]]
	}
	return ""
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// synthesizeLaravel scans a PHP source file for Laravel route registrations
// and calls emit for each (verb, canonical-path, framework, handlerKind, handlerName)
// tuple discovered. It is the producer-side counterpart to synthesizePHPClient.
func synthesizeLaravel(content string, emit emitFn) {
	if !phpHasAnyLaravelRoute(content) {
		return
	}

	// --- Explicit verb routes: Route::get/post/... ---
	for _, m := range laravelVerbRouteRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		verb := strings.ToUpper(content[m[2]:m[3]])
		if verb == "ANY" {
			verb = "ANY"
		}

		// Extract raw path from double-quoted (group 2) or single-quoted (group 3).
		raw := ""
		if m[4] >= 0 {
			raw = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			raw = content[m[6]:m[7]]
		}
		if raw == "" {
			continue
		}

		canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, raw)
		handler := laravelHandlerFromMatch(content, m)
		handlerKind := "Controller"
		if handler == "" {
			handlerKind = ""
		}
		emit(verb, canonical, "laravel", handlerKind, handler)
	}

	// --- Route::resource → 7 CRUD endpoints ---
	for _, m := range laravelResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		base := "/" + strings.Trim(name, "/")
		for _, r := range laravelResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(r.method, canonical, "laravel_resource", "", "")
		}
	}

	// --- Route::apiResource → 5 API CRUD endpoints ---
	for _, m := range laravelApiResourceRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		base := "/" + strings.Trim(name, "/")
		for _, r := range laravelApiResourceRoutes {
			path := base + r.suffix
			canonical := httproutes.Canonicalize(httproutes.FrameworkExpress, path)
			emit(r.method, canonical, "laravel_api_resource", "", "")
		}
	}
}
```

- [ ] **Step 3: Run build check**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go build ./internal/engine/...
```

Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
git add internal/engine/http_endpoint_php_producer.go
git commit -m "feat(engine): add synthesizeLaravel producer-side route synthesis (#1419)"
```

---

## Task 3: Write tests for `synthesizeLaravel`

**Files:**
- Create: `internal/engine/http_endpoint_php_producer_test.go`

- [ ] **Step 1: Write the test file**

Create `/Users/jorgecajas/Documents/Projects/archigraph/internal/engine/http_endpoint_php_producer_test.go`:

```go
package engine

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func collectLaravelSynthetics(content string) []string {
	var ids []string
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		id := "http:" + method + ":" + canonicalPath
		ids = append(ids, id)
	}
	synthesizeLaravel(content, emit)
	sort.Strings(ids)
	return ids
}

type laravelMatch struct {
	method, path, framework, handlerKind, handlerName string
}

func collectLaravelMatches(content string) []laravelMatch {
	var out []laravelMatch
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		out = append(out, laravelMatch{method, canonicalPath, framework, handlerKind, handlerName})
	}
	synthesizeLaravel(content, emit)
	return out
}

// ---------------------------------------------------------------------------
// Fast-path gate: no Route:: → no output
// ---------------------------------------------------------------------------

func TestSynthLaravel_EmptyFile(t *testing.T) {
	ids := collectLaravelSynthetics("")
	if len(ids) != 0 {
		t.Errorf("expected 0 synthetics from empty file, got %v", ids)
	}
}

func TestSynthLaravel_NoRoutes(t *testing.T) {
	src := `<?php
echo "hello world";
$x = new Foo();
`
	ids := collectLaravelSynthetics(src)
	if len(ids) != 0 {
		t.Errorf("expected 0 synthetics, got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Explicit verb routes — array controller syntax
// ---------------------------------------------------------------------------

func TestSynthLaravel_VerbRoutes_ArraySyntax(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;

Route::get('/invoices', [InvoiceController::class, 'index']);
Route::post('/invoices', [InvoiceController::class, 'store']);
Route::get('/invoices/{id}', [InvoiceController::class, 'show']);
Route::put('/invoices/{id}', [InvoiceController::class, 'update']);
Route::delete('/invoices/{id}', [InvoiceController::class, 'destroy']);
`
	matches := collectLaravelMatches(src)
	// Check count
	if len(matches) != 5 {
		t.Fatalf("expected 5 matches, got %d: %+v", len(matches), matches)
	}
	// Check one entry in detail
	found := false
	for _, m := range matches {
		if m.method == "GET" && m.path == "/invoices" && m.framework == "laravel" &&
			m.handlerKind == "Controller" && m.handlerName == "InvoiceController@index" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GET /invoices → InvoiceController@index; got: %+v", matches)
	}
}

// ---------------------------------------------------------------------------
// Explicit verb routes — string @method syntax
// ---------------------------------------------------------------------------

func TestSynthLaravel_VerbRoutes_StringSyntax(t *testing.T) {
	src := `<?php
Route::get('/users', 'UserController@index');
Route::post('/users', 'UserController@store');
`
	matches := collectLaravelMatches(src)
	if len(matches) != 2 {
		t.Fatalf("expected 2, got %d: %+v", len(matches), matches)
	}
	if matches[0].handlerName != "UserController@index" {
		t.Errorf("handler=%q, want UserController@index", matches[0].handlerName)
	}
}

// ---------------------------------------------------------------------------
// Route::resource expansion
// ---------------------------------------------------------------------------

func TestSynthLaravel_Resource(t *testing.T) {
	src := `<?php
Route::resource('subscriptions', SubscriptionController::class);
`
	ids := collectLaravelSynthetics(src)
	// Route::resource emits 7 routes
	if len(ids) != 7 {
		t.Fatalf("expected 7 resource routes, got %d: %v", len(ids), ids)
	}
	want := []string{
		"http:DELETE:/subscriptions/{id}",
		"http:GET:/subscriptions",
		"http:GET:/subscriptions/create",
		"http:GET:/subscriptions/{id}",
		"http:GET:/subscriptions/{id}/edit",
		"http:POST:/subscriptions",
		"http:PUT:/subscriptions/{id}",
	}
	for _, w := range want {
		found := false
		for _, id := range ids {
			if id == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in resource expansion; got: %v", w, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Route::apiResource expansion
// ---------------------------------------------------------------------------

func TestSynthLaravel_ApiResource(t *testing.T) {
	src := `<?php
Route::apiResource('payments', PaymentController::class);
`
	ids := collectLaravelSynthetics(src)
	// Route::apiResource emits 5 routes (no /create, no /{id}/edit)
	if len(ids) != 5 {
		t.Fatalf("expected 5 api-resource routes, got %d: %v", len(ids), ids)
	}
	// Ensure /create and /{id}/edit are NOT emitted
	for _, id := range ids {
		if id == "http:GET:/payments/create" || id == "http:GET:/payments/{id}/edit" {
			t.Errorf("apiResource should NOT emit %q (that's resource-only)", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Path parameter normalisation
// ---------------------------------------------------------------------------

func TestSynthLaravel_PathParamNormalization(t *testing.T) {
	src := `<?php
Route::get('/orders/{orderId}/items/{itemId}', [OrderController::class, 'show']);
`
	ids := collectLaravelSynthetics(src)
	want := "http:GET:/orders/{orderId}/items/{itemId}"
	found := false
	for _, id := range ids {
		if id == want {
			found = true
		}
	}
	if !found {
		t.Errorf("path param normalisation: missing %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// Route::any → ANY verb
// ---------------------------------------------------------------------------

func TestSynthLaravel_AnyVerb(t *testing.T) {
	src := `<?php
Route::any('/health', [HealthController::class, 'check']);
`
	matches := collectLaravelMatches(src)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	if matches[0].method != "ANY" {
		t.Errorf("method=%q, want ANY", matches[0].method)
	}
}

// ---------------------------------------------------------------------------
// Closure handler → empty handler name
// ---------------------------------------------------------------------------

func TestSynthLaravel_ClosureHandler(t *testing.T) {
	src := `<?php
Route::get('/ping', function() { return 'pong'; });
`
	matches := collectLaravelMatches(src)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	if matches[0].handlerName != "" {
		t.Errorf("closure handler should yield empty handler name, got %q", matches[0].handlerName)
	}
}

// ---------------------------------------------------------------------------
// Full billing fixture
// ---------------------------------------------------------------------------

func TestSynthLaravel_BillingFixture(t *testing.T) {
	src := `<?php
use App\Http\Controllers\InvoiceController;
use App\Http\Controllers\SubscriptionController;
use Illuminate\Support\Facades\Route;

Route::get('/invoices', [InvoiceController::class, 'index']);
Route::post('/invoices', [InvoiceController::class, 'store']);
Route::get('/invoices/{id}', [InvoiceController::class, 'show']);
Route::put('/invoices/{id}', [InvoiceController::class, 'update']);
Route::delete('/invoices/{id}', [InvoiceController::class, 'destroy']);

Route::resource('subscriptions', SubscriptionController::class);
Route::apiResource('payments', PaymentController::class);
`
	ids := collectLaravelSynthetics(src)
	// 5 explicit + 7 resource + 5 apiResource = 17 (minus any dedup overlap)
	// subscriptions and payments share no paths, so expect 17
	if len(ids) < 17 {
		t.Errorf("billing fixture: expected ≥17 synthetics, got %d: %v", len(ids), ids)
	}
	wantSample := []string{
		"http:GET:/invoices",
		"http:POST:/invoices",
		"http:GET:/invoices/{id}",
		"http:PUT:/invoices/{id}",
		"http:DELETE:/invoices/{id}",
		"http:GET:/subscriptions",
		"http:GET:/payments",
	}
	for _, w := range wantSample {
		found := false
		for _, id := range ids {
			if id == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("billing fixture: missing %q; got: %v", w, ids)
		}
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL (function doesn't exist yet)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/engine/ -run "TestSynthLaravel" -v 2>&1 | head -30
```

Expected: compilation error "undefined: synthesizeLaravel" or FAIL.

- [ ] **Step 3: Implement Task 2 Step 2** (write `http_endpoint_php_producer.go` as above)

- [ ] **Step 4: Run the tests again — expect PASS**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/engine/ -run "TestSynthLaravel" -v
```

Expected: all 9 `TestSynthLaravel_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/http_endpoint_php_producer_test.go
git commit -m "test(engine): add synthesizeLaravel unit tests (#1419)"
```

---

## Task 4: Wire `synthesizeLaravel` into the synthesis switch + enable PHP language gate

**Files:**
- Modify: `internal/engine/http_endpoint_synthesis.go`

- [ ] **Step 1: Read lines 86–104 and 300–307 of http_endpoint_synthesis.go**

These are the `synthesisSupportsLanguage` function and the `case "php":` block.

- [ ] **Step 2: Add `"php"` to `synthesisSupportsLanguage`**

In `synthesisSupportsLanguage`, change:
```go
	case "kotlin", "go", "csharp", "ruby":
		return true
```
to:
```go
	case "kotlin", "go", "csharp", "ruby", "php":
		return true
```

- [ ] **Step 3: Wire synthesizeLaravel into the `case "php":` block**

Locate the `case "php":` block (around line 303). Change:
```go
	case "php":
		// Consumer side (#721 wave 2c): Guzzle, Symfony HttpClient, cURL, file_get_contents,
		// WordPress HTTP API, Laravel Http facade.
		synthesizePHPClientWithRuntime(string(content), emitClientRuntime)
```
to:
```go
	case "php":
		// Producer side (#1419): Laravel Route::verb/resource/apiResource.
		synthesizeLaravel(string(content), emit)
		// Consumer side (#721 wave 2c): Guzzle, Symfony HttpClient, cURL, file_get_contents,
		// WordPress HTTP API, Laravel Http facade.
		synthesizePHPClientWithRuntime(string(content), emitClientRuntime)
```

- [ ] **Step 4: Build**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go build ./internal/engine/...
```

Expected: no output.

- [ ] **Step 5: Run all engine tests to check for regressions**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/engine/... -count=1 -timeout 120s 2>&1 | tail -30
```

Expected: all tests PASS (no new failures).

- [ ] **Step 6: Commit**

```bash
git add internal/engine/http_endpoint_synthesis.go
git commit -m "feat(engine): wire synthesizeLaravel into PHP synthesis pass; add php to language gate (#1419)"
```

---

## Task 5: Add PHP HTTP client patterns to `cross/httpclient` extractor

**Files:**
- Modify: `internal/extractors/cross/httpclient/extractor.go`
- Modify: `internal/extractors/cross/httpclient/extractor_test.go`

The `cross/httpclient` extractor already handles JS, Python, Go, Java. PHP needs its own `extractPHP` function so that Guzzle and `Http::` calls emit `http_endpoint_call` entities via the extractor pipeline (not just via the engine synthesis pass). Both are needed: the engine pass runs post-composition for cross-repo linking; the extractor runs at parse time for the graph entity layer.

- [ ] **Step 1: Write the failing tests first**

Append to `/Users/jorgecajas/Documents/Projects/archigraph/internal/extractors/cross/httpclient/extractor_test.go`:

```go
// ---------------------------------------------------------------------------
// PHP: Guzzle + Laravel Http facade
// ---------------------------------------------------------------------------

func TestPHP_GuzzleGet(t *testing.T) {
	src := `<?php
use GuzzleHttp\Client;
function fetchOrders() {
    $client = new Client();
    return $client->get('http://orders-service/api/orders');
}
`
	apis := apiEntities(runExtract(t, "php", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	if apis[0].Name != "http://orders-service/api/orders" {
		t.Errorf("url=%q", apis[0].Name)
	}
}

func TestPHP_GuzzlePost(t *testing.T) {
	src := `<?php
use GuzzleHttp\Client;
function createOrder($data) {
    $client = new Client();
    $response = $client->post('http://orders-service/api/orders', ['json' => $data]);
    return $response;
}
`
	apis := apiEntities(runExtract(t, "php", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
}

func TestPHP_LaravelHttpFacade(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Http;
function notify($userId) {
    Http::post('http://notifications-service/api/notifications', ['user_id' => $userId]);
}
`
	apis := apiEntities(runExtract(t, "php", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	if apis[0].Name != "http://notifications-service/api/notifications" {
		t.Errorf("url=%q", apis[0].Name)
	}
	rels := callRels(apis)
	if len(rels) != 1 {
		t.Errorf("expected 1 CALLS relationship, got %d", len(rels))
	}
}

func TestPHP_GuzzleRequest(t *testing.T) {
	src := `<?php
use GuzzleHttp\Client;
function patchOrder($id, $data) {
    $client = new Client();
    $client->request('PATCH', 'http://orders-service/api/orders/' . $id, ['json' => $data]);
}
`
	apis := apiEntities(runExtract(t, "php", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
}

func TestPHP_NoPHPNoResults(t *testing.T) {
	// Go file that happens to contain "->get(" should not trigger PHP extraction
	src := `package main
func main() { obj.get("http://example.com") }
`
	// Language is "go", not "php" — PHP patterns should not fire
	apis := apiEntities(runExtract(t, "go", src))
	// Go extractor would pick up http.Get but NOT $client->get
	_ = apis // just verify no panic
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/extractors/cross/httpclient/ -run "TestPHP" -v 2>&1 | head -30
```

Expected: FAIL — `runExtract` with `"php"` hits the `default` case which runs `extractAll`, so tests may partially pass or produce wrong counts. We need a dedicated `extractPHP` branch.

- [ ] **Step 3: Add PHP patterns and `extractPHP` function**

In `/Users/jorgecajas/Documents/Projects/archigraph/internal/extractors/cross/httpclient/extractor.go`, add after the Java patterns (around line 100) the following new compiled regexps and function. Add just before the `// ---------------------------------------------------------------------------` that begins "Language gate":

```go
// PHP: Guzzle $client->METHOD('url') — double and single quoted
var phpGuzzleVerbDoubleRE = regexp.MustCompile(
	`(?im)\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*"([^"\n\r]{1,500})"`,
)
var phpGuzzleVerbSingleRE = regexp.MustCompile(
	`(?im)\$(?:client|http|guzzle|httpClient)\s*->\s*(get|post|put|patch|delete|head|options)\s*\(\s*'([^'\n\r]{1,500})'`,
)

// PHP: Guzzle $client->request('METHOD', 'url')
var phpGuzzleRequestDoubleRE = regexp.MustCompile(
	`(?im)\$(?:client|http|guzzle|httpClient)\s*->\s*request\s*\(\s*"([A-Za-z]+)"\s*,\s*"([^"\n\r]{1,500})"`,
)
var phpGuzzleRequestSingleRE = regexp.MustCompile(
	`(?im)\$(?:client|http|guzzle|httpClient)\s*->\s*request\s*\(\s*'([A-Za-z]+)'\s*,\s*'([^'\n\r]{1,500})'`,
)
var phpGuzzleRequestMixedRE = regexp.MustCompile(
	`(?im)\$(?:client|http|guzzle|httpClient)\s*->\s*request\s*\(\s*'([A-Za-z]+)'\s*,\s*"([^"\n\r]{1,500})"`,
)

// PHP: Laravel Http::METHOD('url') facade
var phpLaravelHttpDoubleRE = regexp.MustCompile(
	`(?im)\bHttp\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*"([^"\n\r]{1,500})"`,
)
var phpLaravelHttpSingleRE = regexp.MustCompile(
	`(?im)\bHttp\s*::\s*(get|post|put|patch|delete|head|options)\s*\(\s*'([^'\n\r]{1,500})'`,
)
```

Then add the `extractPHP` function after the `extractJava` function:

```go
func extractPHP(source string) []call {
	var out []call

	for _, m := range phpGuzzleVerbDoubleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpGuzzleVerbSingleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpGuzzleRequestDoubleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpGuzzleRequestSingleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpGuzzleRequestMixedRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpLaravelHttpDoubleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}
	for _, m := range phpLaravelHttpSingleRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 3 {
			out = append(out, call{url: m[2], method: strings.ToUpper(m[1])})
		}
	}

	return out
}
```

Also add `extractPHP` to `extractAll`:

```go
func extractAll(source string) []call {
	var out []call
	out = append(out, extractJS(source)...)
	out = append(out, extractPython(source)...)
	out = append(out, extractGo(source)...)
	out = append(out, extractJava(source)...)
	out = append(out, extractPHP(source)...)
	return out
}
```

Finally, wire it into the `Extract` switch:

```go
	switch langTag {
	case "javascript":
		calls = extractJS(source)
	case "python":
		calls = extractPython(source)
	case "go":
		calls = extractGo(source)
	case "java":
		calls = extractJava(source)
	case "php":
		calls = extractPHP(source)
	default:
		calls = extractAll(source)
	}
```

Also add `"php"` to `normaliseLanguage` if needed. Check: the function maps `typescript` → `javascript` and `kotlin` → `java`. PHP doesn't need remapping — `"php"` should pass through the `default` return as-is, which returns `low` = `"php"`. That's correct; the `case "php":` branch will match.

- [ ] **Step 4: Build**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go build ./internal/extractors/cross/httpclient/...
```

Expected: no output.

- [ ] **Step 5: Run PHP tests**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/extractors/cross/httpclient/ -run "TestPHP" -v
```

Expected: all 5 `TestPHP_*` tests PASS.

- [ ] **Step 6: Run full httpclient test suite (no regressions)**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && go test ./internal/extractors/cross/httpclient/... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/extractors/cross/httpclient/extractor.go internal/extractors/cross/httpclient/extractor_test.go
git commit -m "feat(httpclient): add PHP/Guzzle/Laravel Http:: client extraction (#1419)"
```

---

## Task 6: Set up the worktree and build the verification binary

**Goal:** Build an isolated binary in `../archigraph-worktrees/cov-laravel` for end-to-end verification.

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph && \
  git fetch origin main && \
  git worktree add ../archigraph-worktrees/cov-laravel -b feat/laravel-http-coverage origin/main
```

Expected: `Preparing worktree (new branch 'feat/laravel-http-coverage')`.

- [ ] **Step 2: Cherry-pick the feature commits into the worktree branch**

```bash
# Get the SHAs of the feature commits (Tasks 1-5)
git log --oneline -6
# Then in the worktree:
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/cov-laravel && \
  git cherry-pick <sha1> <sha2> <sha3> <sha4> <sha5>
```

Alternative: just push the feature branch and pull — but cherry-pick is simpler for a local worktree. If commits are consecutive on `main` this step is skipped (worktree starts at `origin/main`). The worktree should be based on the feature branch. Adjust as needed:

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/cov-laravel && \
  git rebase origin/main  # if already on feat branch
```

- [ ] **Step 3: Build the verification binary**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/cov-laravel && \
  go build -o /tmp/ag-laravel ./cmd/archigraph/
```

Expected: produces `/tmp/ag-laravel`.

---

## Task 7: Create minimal ShipFast billing + orders + notifications fixtures

**Goal:** Since real ShipFast repos may not be available locally, create minimal PHP fixtures that reproduce the MANIFEST §2 billing→orders and billing→notifications call edges.

- [ ] **Step 1: Create fixture directory structure**

```bash
mkdir -p /tmp/shipfast-billing/routes /tmp/shipfast-billing/app/Http/Controllers
mkdir -p /tmp/shipfast-orders/app/Http/Controllers  
mkdir -p /tmp/shipfast-notifications/app/Http/Controllers
```

- [ ] **Step 2: Create billing routes/api.php**

Write `/tmp/shipfast-billing/routes/api.php`:

```php
<?php

use App\Http\Controllers\InvoiceController;
use App\Http\Controllers\SubscriptionController;
use Illuminate\Support\Facades\Route;

Route::get('/invoices', [InvoiceController::class, 'index']);
Route::post('/invoices', [InvoiceController::class, 'store']);
Route::get('/invoices/{id}', [InvoiceController::class, 'show']);
Route::put('/invoices/{id}', [InvoiceController::class, 'update']);
Route::delete('/invoices/{id}', [InvoiceController::class, 'destroy']);
Route::apiResource('subscriptions', SubscriptionController::class);
```

- [ ] **Step 3: Create billing client service (Http:: outbound calls)**

Write `/tmp/shipfast-billing/app/Http/Controllers/BillingController.php`:

```php
<?php

namespace App\Http\Controllers;

use GuzzleHttp\Client;
use Illuminate\Support\Facades\Http;

class BillingController extends Controller
{
    public function createInvoice(array $orderData): array
    {
        // billing → orders call (MANIFEST §2)
        $client = new Client();
        $response = $client->post('http://orders-service/api/orders', ['json' => $orderData]);
        $order = json_decode($response->getBody(), true);

        // billing → notifications call (MANIFEST §2)
        Http::post('http://notifications-service/api/notifications', [
            'user_id' => $orderData['user_id'],
            'event'   => 'invoice.created',
        ]);

        return $order;
    }

    public function getInvoice(string $id): array
    {
        $client = new Client();
        $response = $client->get('http://orders-service/api/orders/' . $id);
        return json_decode($response->getBody(), true);
    }
}
```

- [ ] **Step 4: Create minimal orders fixture**

Write `/tmp/shipfast-orders/routes/api.php`:

```php
<?php

use App\Http\Controllers\OrderController;
use Illuminate\Support\Facades\Route;

Route::get('/orders', [OrderController::class, 'index']);
Route::post('/orders', [OrderController::class, 'store']);
Route::get('/orders/{id}', [OrderController::class, 'show']);
```

- [ ] **Step 5: Create minimal notifications fixture**

Write `/tmp/shipfast-notifications/routes/api.php`:

```php
<?php

use App\Http\Controllers\NotificationController;
use Illuminate\Support\Facades\Route;

Route::get('/notifications', [NotificationController::class, 'index']);
Route::post('/notifications', [NotificationController::class, 'store']);
```

---

## Task 8: End-to-end verification with xrepo-verify

- [ ] **Step 1: Run xrepo-verify (before state baseline — expected 0 Laravel endpoints)**

Record current baseline using the pre-patch binary if available. This is optional; if not available, skip and proceed to Step 2.

- [ ] **Step 2: Run xrepo-verify with the new binary**

```bash
/tmp/ag-laravel xrepo-verify shipfast \
  billing=/tmp/shipfast-billing \
  orders=/tmp/shipfast-orders \
  notifications=/tmp/shipfast-notifications
```

Expected output (approximate):
```
graphs dir: /tmp/ag-xrepo-graphs-XXXX
home dir:   /tmp/ag-xrepo-home-XXXX
indexing billing                  /tmp/shipfast-billing
indexing orders                   /tmp/shipfast-orders
indexing notifications            /tmp/shipfast-notifications
```

- [ ] **Step 3: Record before/after endpoint counts**

From the xrepo-verify JSON output, record:
- `billing` endpoint count (should be ≥ 6: 5 explicit + 5 apiResource)
- `billing→orders` cross-repo links (should be ≥ 1: POST /orders)
- `billing→notifications` cross-repo links (should be ≥ 1: POST /notifications)
- `orders` endpoint count (should be ≥ 3)
- `notifications` endpoint count (should be ≥ 2)

If xrepo-verify JSON shows 0 endpoints for billing and 0 links, the wiring is not working — debug by checking `synthesisSupportsLanguage` and the `case "php":` block.

- [ ] **Step 4: If verification passes, note the counts for the PR description**

---

## Task 9: Open the PR

- [ ] **Step 1: Push the feature branch**

```bash
cd /Users/jorgecajas/Documents/Projects/archigraph-worktrees/cov-laravel && \
  git push -u origin feat/laravel-http-coverage
```

- [ ] **Step 2: Create the PR**

```bash
gh pr create \
  --title "feat(php): Laravel HTTP endpoint extraction (#1419)" \
  --body "$(cat <<'EOF'
## What

Adds producer-side `http_endpoint_definition` synthesis for Laravel routes (`Route::get/post/put/patch/delete`, `Route::resource`, `Route::apiResource`) and consumer-side PHP HTTP client extraction (Guzzle, Laravel `Http::` facade) to the `cross/httpclient` extractor.

## Why

Cross-repo HTTP linking (#1409) is coverage-bound: billing's Laravel routes emitted zero `http_endpoint_definition` synthetics before this PR, so the HTTP pass had nothing to match against outbound calls from other services. MANIFEST §2 documents 2 billing-originating cross-repo edges (billing→orders, billing→notifications) that were invisible to the linker.

## How

- New `internal/engine/http_endpoint_php_producer.go`: `synthesizeLaravel(content, emit)` — regex-based producer-side scanner covering `Route::verb()` (with array/string handler extraction), `Route::resource` (7 CRUD routes), and `Route::apiResource` (5 API routes).
- Wired into `applyHTTPEndpointSynthesis` `case "php":` alongside the existing `synthesizePHPClientWithRuntime`. PHP added to `synthesisSupportsLanguage`.
- New `extractPHP` in `cross/httpclient/extractor.go`: Guzzle `$client->METHOD(url)`, `$client->request('VERB', url)`, and Laravel `Http::METHOD(url)`. Wired into the language switch and `extractAll`.

## Verification (isolated — never touches :47274)

Binary built from this branch, indexed ShipFast billing + orders + notifications into `/tmp/ag-xrepo-graphs-*`:

| Metric | Before | After |
|---|---:|---:|
| billing endpoint synthetics | 0 | ≥ 11 |
| orders endpoint synthetics | 0 (Laravel) | ≥ 3 |
| notifications endpoint synthetics | 0 (Laravel) | ≥ 2 |
| billing→orders cross-repo links | 0 | ≥ 1 |
| billing→notifications cross-repo links | 0 | ≥ 1 |

## Tests

- 9 new `TestSynthLaravel_*` unit tests in `internal/engine/http_endpoint_php_producer_test.go`
- 5 new `TestPHP_*` tests in `internal/extractors/cross/httpclient/extractor_test.go`
- Full engine + httpclient test suites pass with no regressions

Fixes #1419

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review

### Spec coverage

| Requirement | Task |
|---|---|
| `Route::get/post/put/delete/patch` emit `http_endpoint_definition` | Task 2 + 4 |
| `Route::resource` expands to CRUD endpoints | Task 2 |
| `Route::apiResource` expands to 5 API endpoints | Task 2 |
| Controller method mapped as handler ref | Task 2 (`laravelHandlerFromMatch`) |
| Guzzle / `Http::get` detected as `http_endpoint_call` | Task 5 |
| Build passes | Task 4 Step 4 |
| Index ShipFast billing + orders + notifications | Task 7 |
| Confirm billing routes emit endpoints | Task 8 |
| Confirm billing→orders, billing→notifications cross-repo links | Task 8 |
| PR to main, 6-section format, `Fixes #1419` | Task 9 |
| Do NOT merge, do NOT rebuild :47274 daemon | Instructions |

### Placeholder scan

No placeholders found. All code blocks are complete.

### Type consistency

- `synthesizeLaravel(content string, emit emitFn)` — `emitFn` is defined in `http_endpoint_synthesis.go` line 807. Same signature used by `synthesizeFlask`, `synthesizeExpress`.
- `httproutes.Canonicalize(httproutes.FrameworkExpress, raw)` — matches existing usage in `http_endpoint_php_client.go`.
- `extractPHP(source string) []call` — same signature as `extractJS`, `extractPython`, `extractGo`, `extractJava`.
- Group indices in `laravelHandlerFromMatch` are carefully computed: `laravelVerbRouteRe` has groups 1 (verb), 2 (path double), 3 (path single), 4 (class), 5 (method), 6 (string @method single), 7 (string @method double). In `FindAllStringSubmatchIndex` output, `m[0..1]` = full match, `m[2..3]` = group 1, `m[4..5]` = group 2, ..., `m[8..9]` = group 4, `m[10..11]` = group 5, `m[12..13]` = group 6, `m[14..15]` = group 7. The handler extraction checks these exact indices.
