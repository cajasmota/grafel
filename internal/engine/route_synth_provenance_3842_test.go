package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// route_synth_provenance_3842_test.go — T10 #3842 end-to-end VERIFY: the
// convention-framework resourceful-route synthesizers (Rails `resources`,
// Laravel `Route::resource`/`apiResource`) now stamp `provenance=
// framework_synthesized` + the per-verb effective CONTRACT (effective_status,
// effective_error_statuses, effective_action, defining_class) on the canonical
// http_endpoint synthetic — fanning the DRF effective-contract shape (#3835)
// across the OO/convention frameworks.

// findSynth returns the http_endpoint synthetic for (verb, path) from a pass
// result, or fails the test.
func findSynth(t *testing.T, ents []types.EntityRecord, verb, path string) types.EntityRecord {
	t.Helper()
	id := "http:" + verb + ":" + path
	for _, e := range ents {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no synthesized route %s; got %d entities", id, len(ents))
	return types.EntityRecord{}
}

func assertSynthProp(t *testing.T, e types.EntityRecord, key, want string) {
	t.Helper()
	got := ""
	if e.Properties != nil {
		got = e.Properties[key]
	}
	if got != want {
		t.Errorf("%s: %s=%q want %q", e.ID, key, got, want)
	}
}

func assertSynthPropAbsent(t *testing.T, e types.EntityRecord, key string) {
	t.Helper()
	if e.Properties != nil {
		if v, ok := e.Properties[key]; ok {
			t.Errorf("%s: %s should be absent, got %q", e.ID, key, v)
		}
	}
}

// TestRouteSynth_Rails_Resources_Provenance asserts the SPECIFIC verb+path+
// provenance+status for every distinctive route of `resources :widgets`.
func TestRouteSynth_Rails_Resources_Provenance(t *testing.T) {
	src := `Rails.application.routes.draw do
  resources :widgets
end`
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Lang: "ruby", Path: "config/routes.rb", Content: []byte(src),
	})

	// index — GET /widgets → 200
	idx := findSynth(t, res.Entities, "GET", "/widgets")
	assertSynthProp(t, idx, "framework", "rails_resources")
	assertSynthProp(t, idx, "provenance", "framework_synthesized")
	assertSynthProp(t, idx, "effective_kind", "synthesized")
	assertSynthProp(t, idx, "effective_action", "index")
	assertSynthProp(t, idx, "effective_status", "200")
	assertSynthProp(t, idx, "defining_class", "ActionDispatch::Routing::Mapper::Resources")

	// create — POST /widgets → 201 + 422
	create := findSynth(t, res.Entities, "POST", "/widgets")
	assertSynthProp(t, create, "provenance", "framework_synthesized")
	assertSynthProp(t, create, "effective_action", "create")
	assertSynthProp(t, create, "effective_status", "201")
	assertSynthProp(t, create, "effective_error_statuses", "422")

	// show — GET /widgets/{id} → 200 + 404
	show := findSynth(t, res.Entities, "GET", "/widgets/{id}")
	assertSynthProp(t, show, "effective_action", "show")
	assertSynthProp(t, show, "effective_status", "200")
	assertSynthProp(t, show, "effective_error_statuses", "404")

	// update — PATCH /widgets/{id} → 200
	patch := findSynth(t, res.Entities, "PATCH", "/widgets/{id}")
	assertSynthProp(t, patch, "effective_action", "update")
	assertSynthProp(t, patch, "effective_status", "200")

	// destroy — DELETE /widgets/{id} → 204
	del := findSynth(t, res.Entities, "DELETE", "/widgets/{id}")
	assertSynthProp(t, del, "effective_action", "destroy")
	assertSynthProp(t, del, "effective_status", "204")
}

// TestRouteSynth_Laravel_Resource_Provenance asserts the Laravel
// `Route::resource('posts', PostController::class)` synthesized routes.
func TestRouteSynth_Laravel_Resource_Provenance(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::resource('posts', PostController::class);
`
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Lang: "php", Path: "routes/web.php", Content: []byte(src),
	})

	// store — POST /posts → 201
	store := findSynth(t, res.Entities, "POST", "/posts")
	assertSynthProp(t, store, "framework", "laravel_resource")
	assertSynthProp(t, store, "provenance", "framework_synthesized")
	assertSynthProp(t, store, "effective_action", "store")
	assertSynthProp(t, store, "effective_status", "201")

	// destroy — DELETE /posts/{id} → 204
	del := findSynth(t, res.Entities, "DELETE", "/posts/{id}")
	assertSynthProp(t, del, "effective_action", "destroy")
	assertSynthProp(t, del, "effective_status", "204")

	// update — PUT /posts/{id} → 200 (Laravel resource uses PUT)
	upd := findSynth(t, res.Entities, "PUT", "/posts/{id}")
	assertSynthProp(t, upd, "effective_action", "update")
	assertSynthProp(t, upd, "effective_status", "200")

	// index — GET /posts → 200
	idx := findSynth(t, res.Entities, "GET", "/posts")
	assertSynthProp(t, idx, "effective_action", "index")
	assertSynthProp(t, idx, "effective_status", "200")
}

// TestRouteSynth_Laravel_ApiResource_Provenance asserts apiResource emits the
// 5-route API subset with provenance + status (no /create, no /{id}/edit).
func TestRouteSynth_Laravel_ApiResource_Provenance(t *testing.T) {
	src := `<?php
use Illuminate\Support\Facades\Route;
Route::apiResource('articles', ArticleController::class);
`
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Lang: "php", Path: "routes/api.php", Content: []byte(src),
	})

	store := findSynth(t, res.Entities, "POST", "/articles")
	assertSynthProp(t, store, "framework", "laravel_api_resource")
	assertSynthProp(t, store, "provenance", "framework_synthesized")
	assertSynthProp(t, store, "effective_status", "201")

	// API resources have NO form-view routes — /articles/create must not exist.
	for _, e := range res.Entities {
		if e.ID == "http:GET:/articles/create" {
			t.Errorf("apiResource must not synthesize the create form-view route")
		}
	}
}

// TestRouteSynth_Negative_PlainRoute asserts a plain (non-resource) Rails verb
// route is NOT given the framework_synthesized provenance — only the resourceful
// macro synthesis is contracted.
func TestRouteSynth_Negative_PlainRoute(t *testing.T) {
	src := `Rails.application.routes.draw do
  get '/health', to: 'health#index'
end`
	res := applyHTTPEndpointSynthesis(DetectorPassArgs{
		Lang: "ruby", Path: "config/routes.rb", Content: []byte(src),
	})
	health := findSynth(t, res.Entities, "GET", "/health")
	// A plain explicit verb route is framework=rails, NOT a synthesized-resource
	// route — it must carry neither the synthesized provenance nor a fabricated
	// effective_status.
	assertSynthProp(t, health, "framework", "rails")
	assertSynthPropAbsent(t, health, "provenance")
	assertSynthPropAbsent(t, health, "effective_status")
}
