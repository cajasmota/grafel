package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// readBackendFixture loads a #2851 routing fixture from
// testdata/fixtures/typescript/. Fixtures are hand-written and
// dependency-manifest-free.
func readBackendFixture(t *testing.T, name string) string {
	t.Helper()
	// internal/engine → repo root is three levels up.
	p := filepath.Join("..", "..", "testdata", "fixtures", "typescript", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// TestSynth_Adonis covers Route.<verb>('/path', 'Ctrl.method') and the
// Route.resource(...) RESTful expansion.
func TestSynth_Adonis(t *testing.T) {
	src := readBackendFixture(t, "adonisjs_routes.ts")
	got, _ := runDetect(t, "typescript", "start/routes.ts", src)
	want := []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/users/{id}",
		"http:PUT:/users/{id}",
		"http:DELETE:/users/{id}",
		// Route.resource('posts', 'PostsController') expansion.
		"http:GET:/posts",
		"http:POST:/posts",
		"http:GET:/posts/{id}",
		"http:PUT:/posts/{id}",
		"http:DELETE:/posts/{id}",
	}
	requireContains(t, got, want, "adonis")
}

// TestSynth_Hapi covers server.route({ method, path, handler }) including the
// array-method and {id?} optional-param forms.
func TestSynth_Hapi(t *testing.T) {
	src := readBackendFixture(t, "hapi_routes.ts")
	got, _ := runDetect(t, "typescript", "server.ts", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users/{id}",
		"http:PUT:/users/{id}",
	}
	requireContains(t, got, want, "hapi")
}

// TestSynth_Feathers covers service registration via app.use('/svc', service)
// and the standard REST verb expansion.
func TestSynth_Feathers(t *testing.T) {
	src := readBackendFixture(t, "feathers_routes.ts")
	got, _ := runDetect(t, "typescript", "app.ts", src)
	want := []string{
		"http:GET:/messages",
		"http:POST:/messages",
		"http:GET:/messages/{id}",
		"http:PUT:/messages/{id}",
		"http:PATCH:/messages/{id}",
		"http:DELETE:/messages/{id}",
		"http:GET:/users",
		"http:GET:/users/{id}",
	}
	requireContains(t, got, want, "feathers")
}

// TestSynth_Marble covers r.pipe(r.matchPath(...), r.matchType(...)) effects.
func TestSynth_Marble(t *testing.T) {
	src := readBackendFixture(t, "marblejs_routes.ts")
	got, _ := runDetect(t, "typescript", "user.effects.ts", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
	}
	requireContains(t, got, want, "marble")
}

// TestSynth_Polka covers Express-shaped app.<verb>('/path', handler) for Polka.
func TestSynth_Polka(t *testing.T) {
	src := readBackendFixture(t, "polka_routes.ts")
	got, _ := runDetect(t, "typescript", "server.ts", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
	}
	requireContains(t, got, want, "polka")
}

// TestSynth_Restify covers server.<verb>('/path', handler) including the
// short verb aliases (del → DELETE).
func TestSynth_Restify(t *testing.T) {
	src := readBackendFixture(t, "restify_routes.ts")
	got, _ := runDetect(t, "typescript", "server.ts", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
		"http:DELETE:/users/{id}",
	}
	requireContains(t, got, want, "restify")
}

// TestSynth_Sails covers the declarative config/routes.js map. The
// synthesizer is path-gated on the routes-config filename.
func TestSynth_Sails(t *testing.T) {
	src := readBackendFixture(t, "sails_routes.ts")
	got, _ := runDetect(t, "javascript", "config/routes.js", src)
	want := []string{
		"http:GET:/users",
		"http:GET:/users/{id}",
		"http:POST:/users",
		"http:PUT:/users/{id}",
		"http:DELETE:/users/{id}",
		"http:ANY:/health",
	}
	requireContains(t, got, want, "sails")
}

// TestSynth_SailsPathGated asserts the Sails synthesizer does NOT fire on a
// non-routes-config file even with a matching map literal — the path gate
// prevents false positives on arbitrary object literals.
func TestSynth_SailsPathGated(t *testing.T) {
	src := readBackendFixture(t, "sails_routes.ts")
	got, _ := runDetect(t, "javascript", "src/some_config.js", src)
	for _, id := range got {
		if id == "http:GET:/users/{id}" {
			t.Errorf("sails synthesizer fired off a non-routes-config file: got %v", got)
		}
	}
}
