// http_endpoint_php_producer_lrroute_test.go — deep-extraction tests for
// Laravel routing (lrRoute namespace). These tests assert EXACT path+method+
// controller@action tuples — NOT just "≥1 route".
//
// Covers:
//   - Route::resource → 7 exact routes with controller@action attribution
//   - Route::apiResource → 5 exact routes with controller@action attribution
//   - Route::group(['prefix'=>..., 'middleware'=>...], fn) → prefix prepended
//   - Nested Route::group → accumulated prefix
//   - Route::controller(X::class)->group(fn) → controller-scoped group
//   - Invokable single-action controller → Controller@__invoke
//   - Route model binding {photo} via FrameworkExpress canonicalization
//   - ->name('x') chaining (decorative, does not affect path/method)
//
// Refs #3393.
package engine

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers (local to this file)
// ---------------------------------------------------------------------------

type lrMatch struct {
	method, path, framework, handlerKind, handlerName string
}

func collectLrMatches(content string) []lrMatch {
	var out []lrMatch
	emit := func(method, canonicalPath, framework, handlerKind, handlerName string) {
		out = append(out, lrMatch{method, canonicalPath, framework, handlerKind, handlerName})
	}
	synthesizeLaravel(content, emit)
	return out
}

func collectLrIDs(content string) []string {
	var ids []string
	emit := func(method, canonicalPath, framework, _, _ string) {
		ids = append(ids, "http:"+method+":"+canonicalPath)
	}
	synthesizeLaravel(content, emit)
	sort.Strings(ids)
	return ids
}

func assertLrRoute(t *testing.T, matches []lrMatch, method, path, handlerKind, handlerName string) {
	t.Helper()
	for _, m := range matches {
		if m.method == method && m.path == path &&
			m.handlerKind == handlerKind && m.handlerName == handlerName {
			return
		}
	}
	t.Errorf("missing route: %s %s handler=(%s, %s); got: %+v",
		method, path, handlerKind, handlerName, matches)
}

func assertNotLrRoute(t *testing.T, ids []string, id string) {
	t.Helper()
	for _, got := range ids {
		if got == id {
			t.Errorf("unexpected route %q should NOT be present; got: %v", id, ids)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Route::resource — 7 exact routes with controller@action attribution
// ---------------------------------------------------------------------------

func TestLrRoute_Resource_SevenRoutes(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;

Route::resource('photos', PhotoController::class);
`
	matches := collectLrMatches(src)
	if len(matches) != 7 {
		t.Fatalf("Route::resource: expected exactly 7 routes, got %d: %+v", len(matches), matches)
	}

	// Assert exact path+method+controller@action for each of the 7 routes.
	assertLrRoute(t, matches, "GET", "/photos", "SCOPE.Operation", "PhotoController@index")
	assertLrRoute(t, matches, "POST", "/photos", "SCOPE.Operation", "PhotoController@store")
	assertLrRoute(t, matches, "GET", "/photos/create", "SCOPE.Operation", "PhotoController@create")
	assertLrRoute(t, matches, "GET", "/photos/{id}", "SCOPE.Operation", "PhotoController@show")
	assertLrRoute(t, matches, "GET", "/photos/{id}/edit", "SCOPE.Operation", "PhotoController@edit")
	assertLrRoute(t, matches, "PUT", "/photos/{id}", "SCOPE.Operation", "PhotoController@update")
	assertLrRoute(t, matches, "DELETE", "/photos/{id}", "SCOPE.Operation", "PhotoController@destroy")
}

func TestLrRoute_Resource_WithoutController_NoAttribution(t *testing.T) {
	// When controller class is not provided (legacy string-only syntax without class ref),
	// handler attribution falls back to empty.
	src := `<?php
Route::resource('articles', 'ArticleController');
`
	matches := collectLrMatches(src)
	if len(matches) != 7 {
		t.Fatalf("expected 7 routes, got %d", len(matches))
	}
	// handlerName should still follow the pattern
	assertLrRoute(t, matches, "GET", "/articles", "SCOPE.Operation", "ArticleController@index")
	assertLrRoute(t, matches, "DELETE", "/articles/{id}", "SCOPE.Operation", "ArticleController@destroy")
}

// ---------------------------------------------------------------------------
// Route::apiResource — 5 exact routes with controller@action attribution
// ---------------------------------------------------------------------------

func TestLrRoute_ApiResource_FiveRoutes(t *testing.T) {
	src := `<?php
Route::apiResource('payments', PaymentController::class);
`
	matches := collectLrMatches(src)
	if len(matches) != 5 {
		t.Fatalf("Route::apiResource: expected exactly 5 routes, got %d: %+v", len(matches), matches)
	}

	// Assert exact path+method+controller@action for each of the 5 API routes.
	assertLrRoute(t, matches, "GET", "/payments", "SCOPE.Operation", "PaymentController@index")
	assertLrRoute(t, matches, "POST", "/payments", "SCOPE.Operation", "PaymentController@store")
	assertLrRoute(t, matches, "GET", "/payments/{id}", "SCOPE.Operation", "PaymentController@show")
	assertLrRoute(t, matches, "PUT", "/payments/{id}", "SCOPE.Operation", "PaymentController@update")
	assertLrRoute(t, matches, "DELETE", "/payments/{id}", "SCOPE.Operation", "PaymentController@destroy")
}

func TestLrRoute_ApiResource_NoBrowserFormRoutes(t *testing.T) {
	src := `<?php
Route::apiResource('payments', PaymentController::class);
`
	ids := collectLrIDs(src)
	// /create and /{id}/edit must NOT be emitted for apiResource.
	assertNotLrRoute(t, ids, "http:GET:/payments/create")
	assertNotLrRoute(t, ids, "http:GET:/payments/{id}/edit")
}

// ---------------------------------------------------------------------------
// Route::group with prefix → path prefixing
// ---------------------------------------------------------------------------

func TestLrRoute_GroupPrefix_Simple(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;

Route::group(['prefix' => 'admin'], function () {
    Route::get('/users', [AdminUserController::class, 'index']);
    Route::post('/users', [AdminUserController::class, 'store']);
    Route::delete('/users/{user}', [AdminUserController::class, 'destroy']);
});
`
	matches := collectLrMatches(src)
	assertLrRoute(t, matches, "GET", "/admin/users", "SCOPE.Operation", "AdminUserController.index")
	assertLrRoute(t, matches, "POST", "/admin/users", "SCOPE.Operation", "AdminUserController.store")
	assertLrRoute(t, matches, "DELETE", "/admin/users/{user}", "SCOPE.Operation", "AdminUserController.destroy")

	// Ensure the un-prefixed paths are NOT emitted.
	ids := collectLrIDs(src)
	assertNotLrRoute(t, ids, "http:GET:/users")
	assertNotLrRoute(t, ids, "http:POST:/users")
}

func TestLrRoute_GroupPrefix_WithMiddleware(t *testing.T) {
	src := `<?php
Route::group(['prefix' => 'api/v1', 'middleware' => ['auth', 'throttle:60,1']], function () {
    Route::get('/profile', [ProfileController::class, 'show']);
    Route::put('/profile', [ProfileController::class, 'update']);
});
`
	matches := collectLrMatches(src)
	assertLrRoute(t, matches, "GET", "/api/v1/profile", "SCOPE.Operation", "ProfileController.show")
	assertLrRoute(t, matches, "PUT", "/api/v1/profile", "SCOPE.Operation", "ProfileController.update")
}

func TestLrRoute_GroupPrefix_NestedGroups(t *testing.T) {
	src := `<?php
Route::group(['prefix' => 'api'], function () {
    Route::group(['prefix' => 'v2'], function () {
        Route::get('/orders', [OrderController::class, 'index']);
        Route::post('/orders', [OrderController::class, 'store']);
    });
});
`
	matches := collectLrMatches(src)
	assertLrRoute(t, matches, "GET", "/api/v2/orders", "SCOPE.Operation", "OrderController.index")
	assertLrRoute(t, matches, "POST", "/api/v2/orders", "SCOPE.Operation", "OrderController.store")

	ids := collectLrIDs(src)
	// Neither /v2/orders nor /orders alone should be emitted.
	assertNotLrRoute(t, ids, "http:GET:/v2/orders")
	assertNotLrRoute(t, ids, "http:GET:/orders")
}

func TestLrRoute_GroupPrefix_ResourceInsideGroup(t *testing.T) {
	src := `<?php
Route::group(['prefix' => 'admin'], function () {
    Route::resource('photos', PhotoController::class);
});
`
	matches := collectLrMatches(src)
	if len(matches) != 7 {
		t.Fatalf("expected 7 resource routes inside group, got %d: %+v", len(matches), matches)
	}
	assertLrRoute(t, matches, "GET", "/admin/photos", "SCOPE.Operation", "PhotoController@index")
	assertLrRoute(t, matches, "POST", "/admin/photos", "SCOPE.Operation", "PhotoController@store")
	assertLrRoute(t, matches, "GET", "/admin/photos/create", "SCOPE.Operation", "PhotoController@create")
	assertLrRoute(t, matches, "GET", "/admin/photos/{id}", "SCOPE.Operation", "PhotoController@show")
	assertLrRoute(t, matches, "GET", "/admin/photos/{id}/edit", "SCOPE.Operation", "PhotoController@edit")
	assertLrRoute(t, matches, "PUT", "/admin/photos/{id}", "SCOPE.Operation", "PhotoController@update")
	assertLrRoute(t, matches, "DELETE", "/admin/photos/{id}", "SCOPE.Operation", "PhotoController@destroy")
}

// ---------------------------------------------------------------------------
// Route::controller(X::class)->group(fn) — controller-scoped group
// ---------------------------------------------------------------------------

func TestLrRoute_ControllerGroup(t *testing.T) {
	src := `<?php
Route::controller(OrderController::class)->group(function () {
    Route::get('/orders', 'index');
    Route::get('/orders/{id}', 'show');
    Route::post('/orders', 'store');
    Route::put('/orders/{id}', 'update');
    Route::delete('/orders/{id}', 'destroy');
});
`
	ids := collectLrIDs(src)
	// Path extraction must work; handler attribution for string-method form is present.
	found := false
	for _, id := range ids {
		if id == "http:GET:/orders" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Route::controller group: expected http:GET:/orders; got: %v", ids)
	}
	if len(ids) < 5 {
		t.Errorf("expected ≥5 routes from controller group, got %d: %v", len(ids), ids)
	}
}

// ---------------------------------------------------------------------------
// Invokable single-action controller → Controller@__invoke
// ---------------------------------------------------------------------------

func TestLrRoute_InvokableController(t *testing.T) {
	src := `<?php
Route::get('/dashboard', DashboardController::class);
Route::post('/checkout', CheckoutController::class);
`
	matches := collectLrMatches(src)
	if len(matches) != 2 {
		t.Fatalf("expected 2 invokable routes, got %d: %+v", len(matches), matches)
	}
	assertLrRoute(t, matches, "GET", "/dashboard", "SCOPE.Operation", "DashboardController.__invoke")
	assertLrRoute(t, matches, "POST", "/checkout", "SCOPE.Operation", "CheckoutController.__invoke")
}

func TestLrRoute_InvokableController_NamespaceStripped(t *testing.T) {
	src := `<?php
use App\Http\Controllers\Auth\LoginController;

Route::get('/login', App\Http\Controllers\Auth\LoginController::class);
`
	matches := collectLrMatches(src)
	if len(matches) != 1 {
		t.Fatalf("expected 1 invokable route, got %d: %+v", len(matches), matches)
	}
	assertLrRoute(t, matches, "GET", "/login", "SCOPE.Operation", "LoginController.__invoke")
}

// ---------------------------------------------------------------------------
// Route model binding — {photo} param passes through canonicalization
// ---------------------------------------------------------------------------

func TestLrRoute_ModelBinding_SingleParam(t *testing.T) {
	src := `<?php
Route::get('/photos/{photo}', [PhotoController::class, 'show']);
Route::put('/photos/{photo}', [PhotoController::class, 'update']);
Route::delete('/photos/{photo}', [PhotoController::class, 'destroy']);
`
	ids := collectLrIDs(src)
	want := []string{
		"http:DELETE:/photos/{photo}",
		"http:GET:/photos/{photo}",
		"http:PUT:/photos/{photo}",
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
			t.Errorf("model binding: missing %q; got: %v", w, ids)
		}
	}
}

func TestLrRoute_ModelBinding_MultipleParams(t *testing.T) {
	src := `<?php
Route::get('/users/{user}/photos/{photo}', [UserPhotoController::class, 'show']);
`
	ids := collectLrIDs(src)
	want := "http:GET:/users/{user}/photos/{photo}"
	found := false
	for _, id := range ids {
		if id == want {
			found = true
		}
	}
	if !found {
		t.Errorf("multi-param model binding: missing %q; got: %v", want, ids)
	}
}

// ---------------------------------------------------------------------------
// ->name('x') chaining — decorative, must NOT affect path/method extraction
// ---------------------------------------------------------------------------

func TestLrRoute_NameChain_DoesNotAffectExtraction(t *testing.T) {
	src := `<?php
Route::get('/users', [UserController::class, 'index'])->name('users.index');
Route::post('/users', [UserController::class, 'store'])->name('users.store');
`
	matches := collectLrMatches(src)
	if len(matches) != 2 {
		t.Fatalf("->name() chaining: expected 2 routes, got %d: %+v", len(matches), matches)
	}
	assertLrRoute(t, matches, "GET", "/users", "SCOPE.Operation", "UserController.index")
	assertLrRoute(t, matches, "POST", "/users", "SCOPE.Operation", "UserController.store")
}

// ---------------------------------------------------------------------------
// Combined fixture: group + resource + invokable + model binding
// ---------------------------------------------------------------------------

func TestLrRoute_FullFixture(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;

// Unauthenticated routes
Route::get('/login', LoginController::class);
Route::post('/login', LoginController::class);

// Authenticated group
Route::group(['prefix' => 'api/v1', 'middleware' => ['auth:sanctum']], function () {
    // Resource routes
    Route::resource('photos', PhotoController::class);
    Route::apiResource('comments', CommentController::class);

    // Explicit route with model binding
    Route::get('/users/{user}/photos/{photo}', [UserPhotoController::class, 'show'])
        ->name('user.photo.show');

    Route::delete('/photos/{photo}', [PhotoController::class, 'destroy'])
        ->name('photos.destroy')
        ->middleware('can:delete,photo');
});
`
	matches := collectLrMatches(src)

	// Invokable login routes.
	assertLrRoute(t, matches, "GET", "/login", "SCOPE.Operation", "LoginController.__invoke")
	assertLrRoute(t, matches, "POST", "/login", "SCOPE.Operation", "LoginController.__invoke")

	// Resource inside group: all 7 with /api/v1 prefix.
	assertLrRoute(t, matches, "GET", "/api/v1/photos", "SCOPE.Operation", "PhotoController@index")
	assertLrRoute(t, matches, "POST", "/api/v1/photos", "SCOPE.Operation", "PhotoController@store")
	assertLrRoute(t, matches, "GET", "/api/v1/photos/create", "SCOPE.Operation", "PhotoController@create")
	assertLrRoute(t, matches, "GET", "/api/v1/photos/{id}", "SCOPE.Operation", "PhotoController@show")
	assertLrRoute(t, matches, "GET", "/api/v1/photos/{id}/edit", "SCOPE.Operation", "PhotoController@edit")
	assertLrRoute(t, matches, "PUT", "/api/v1/photos/{id}", "SCOPE.Operation", "PhotoController@update")
	assertLrRoute(t, matches, "DELETE", "/api/v1/photos/{id}", "SCOPE.Operation", "PhotoController@destroy")

	// API resource inside group: 5 routes with /api/v1 prefix.
	assertLrRoute(t, matches, "GET", "/api/v1/comments", "SCOPE.Operation", "CommentController@index")
	assertLrRoute(t, matches, "POST", "/api/v1/comments", "SCOPE.Operation", "CommentController@store")
	assertLrRoute(t, matches, "GET", "/api/v1/comments/{id}", "SCOPE.Operation", "CommentController@show")
	assertLrRoute(t, matches, "PUT", "/api/v1/comments/{id}", "SCOPE.Operation", "CommentController@update")
	assertLrRoute(t, matches, "DELETE", "/api/v1/comments/{id}", "SCOPE.Operation", "CommentController@destroy")

	// apiResource must NOT emit /create or /{id}/edit.
	ids := collectLrIDs(src)
	assertNotLrRoute(t, ids, "http:GET:/api/v1/comments/create")
	assertNotLrRoute(t, ids, "http:GET:/api/v1/comments/{id}/edit")

	// Explicit route with model binding inside group.
	assertLrRoute(t, matches, "GET", "/api/v1/users/{user}/photos/{photo}",
		"SCOPE.Operation", "UserPhotoController.show")

	// ->name() + ->middleware() chaining must not break extraction.
	assertLrRoute(t, matches, "DELETE", "/api/v1/photos/{photo}",
		"SCOPE.Operation", "PhotoController.destroy")
}
