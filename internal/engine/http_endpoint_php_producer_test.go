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
	// Check one entry in detail — handler is intentionally empty so the
	// resolve pass keeps the synthetic (NoHandlerProp path) even when the
	// controller class lives in a different file (normal PSR-4 layout).
	found := false
	for _, m := range matches {
		if m.method == "GET" && m.path == "/invoices" && m.framework == "laravel" &&
			m.handlerKind == "" && m.handlerName == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GET /invoices with empty handler; got: %+v", matches)
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
	// Handler is intentionally empty — controllers live in separate files.
	if matches[0].handlerName != "" {
		t.Errorf("handler=%q, want empty (PSR-4 cross-file convention)", matches[0].handlerName)
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
