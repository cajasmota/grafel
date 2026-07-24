package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// TestDetect_DjangoRouteComposition_Issue64 is the regression test for
// issue #64: a urls.py with `path("api/", include(router.urls))` and
// `router.register(r"users", UserViewSet)` / `router.register(r"orders",
// OrderViewSet)` must produce composed Route entities (`/api/users`,
// `/api/orders`) — not orphan Route:users + Route:orders + Route:api/.
func TestDetect_DjangoRouteComposition_Issue64(t *testing.T) {
	fixturePath := filepath.Join("testdata", "issue64_drf_urls.py.fixture")
	src, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "myapp/urls.py",
		Content:  src,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	wantComposed := []string{"/api/users", "/api/orders"}
	forbidden := []string{"users", "orders", "api/"}

	got := make(map[string]bool)
	for _, e := range result.Entities {
		if e.Kind == "Route" {
			got[e.Name] = true
		}
	}

	for _, p := range wantComposed {
		if !got[p] {
			t.Errorf("missing composed Route %q (got %v)", p, got)
		}
	}
	for _, p := range forbidden {
		if got[p] {
			t.Errorf("orphan Route %q must not be present after AST composition", p)
		}
	}

	// admin/ Route stays — it isn't bound to a router via include().
	if !got["admin/"] {
		t.Errorf("expected unrelated Route:admin/ to be preserved (got %v)", got)
	}

	// ROUTES_TO edges: composed targets only. We must see
	// Route:/api/users -> View:UserViewSet and Route:/api/orders ->
	// View:OrderViewSet, all marked ast_driven. The bare-name YAML edges
	// (Route:users -> View:UserViewSet) must be gone.
	type rel struct{ from, to string }
	wantRels := map[rel]bool{
		{"Route:/api/users", "View:UserViewSet"}:   false,
		{"Route:/api/orders", "View:OrderViewSet"}: false,
	}
	forbiddenFrom := map[string]bool{
		"Route:users":  true,
		"Route:orders": true,
	}
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		if forbiddenFrom[r.FromID] {
			t.Errorf("orphan ROUTES_TO from bare-name Route survived: %s -> %s",
				r.FromID, r.ToID)
		}
		k := rel{r.FromID, r.ToID}
		if _, ok := wantRels[k]; ok {
			wantRels[k] = true
			if r.Properties.Get("pattern_type") != "ast_driven" {
				t.Errorf("ROUTES_TO %s->%s pattern_type = %q, want ast_driven",
					r.FromID, r.ToID, r.Properties.Get("pattern_type"))
			}
		}
	}
	for k, seen := range wantRels {
		if !seen {
			t.Errorf("missing composed ROUTES_TO %s -> %s", k.from, k.to)
		}
	}
}

// TestDetect_DjangoRoute_NoIncludeBinding verifies that a urls.py whose
// router is NOT bound via `path(..., include(router.urls))` (i.e. the
// router.urls are spliced directly into urlpatterns) is left to the YAML
// rules. The AST pass must not invent a prefix.
func TestDetect_DjangoRoute_NoIncludeBinding(t *testing.T) {
	src := `from django.urls import path
from rest_framework.routers import DefaultRouter
from myapp.views import UserViewSet

router = DefaultRouter()
router.register(r'users', UserViewSet)

urlpatterns = router.urls
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "flat_urls.py",
		Content:  []byte(src),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	// With no include() binding the AST pass is a no-op; the YAML rules
	// continue to emit Route:users.
	var seen bool
	for _, e := range result.Entities {
		if e.Kind == "Route" && e.Name == "users" {
			seen = true
		}
	}
	if !seen {
		t.Error("expected Route:users from YAML rules when no include()-binding is present")
	}
}

// TestDetect_DjangoRoute_NonPythonNoOp verifies that the Django pass is a
// no-op for non-Python files (the engine still runs all language rules,
// and the AST pass shouldn't accidentally fire on, say, a Java file that
// happens to mention `path(` and `.register(`).
func TestDetect_DjangoRoute_NonPythonNoOp(t *testing.T) {
	src := `// java code that mentions path( include( and .register( in comments
public class Foo {}
`
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	_, err = det.Detect(context.Background(), extractor.FileInput{
		Path:     "Foo.java",
		Content:  []byte(src),
		Language: "java",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
}
