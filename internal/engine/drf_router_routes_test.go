package engine

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// sampleDRFRouterURLs exercises the DRF router.register pattern. It includes
// a SimpleRouter, a DefaultRouter, raw-string and plain-string prefixes,
// and trailing-comma kwargs to ensure every register call produces a
// ROUTES_TO edge.
const sampleDRFRouterURLs = `from rest_framework import routers
from rest_framework.routers import DefaultRouter, SimpleRouter
from .views import (
    UserViewSet, OrderViewSet, ProductViewSet,
    CategoryViewSet, ReviewViewSet,
)
from django.urls import path, include


router = DefaultRouter()
router.register(r'users', UserViewSet, basename='user')
router.register(r"orders", OrderViewSet)
router.register('products', ProductViewSet, basename='product')
router.register(r'categories', CategoryViewSet)

api_router = SimpleRouter()
api_router.register(r'reviews', ReviewViewSet, basename='review')


urlpatterns = [
    path('api/', include(router.urls)),
    path('api/v2/', include(api_router.urls)),
]
`

// TestDetect_DRFRouterRoutes verifies that every router.register(prefix, viewset, ...)
// call in a Django/DRF urls.py emits exactly one ROUTES_TO relationship from
// the prefix Route to the ViewSet target. Refs #43.
func TestDetect_DRFRouterRoutes(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "myapp/urls.py",
		Content:  []byte(sampleDRFRouterURLs),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	type rel struct{ from, to string }
	expected := map[rel]bool{
		{"Route:users", "View:UserViewSet"}:          false,
		{"Route:orders", "View:OrderViewSet"}:        false,
		{"Route:products", "View:ProductViewSet"}:    false,
		{"Route:categories", "View:CategoryViewSet"}: false,
		{"Route:reviews", "View:ReviewViewSet"}:      false,
	}

	var routesToCount int
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		// Only count edges produced by the DRF router rule (Route -> View).
		// The blueprint include(...) rule also emits Route -> Route ROUTES_TO
		// edges, which we ignore here.
		key := rel{r.FromID, r.ToID}
		if _, ok := expected[key]; ok {
			expected[key] = true
			routesToCount++
		}
	}

	if routesToCount != len(expected) {
		t.Errorf("DRF router.register ROUTES_TO count = %d, want %d", routesToCount, len(expected))
	}
	for k, seen := range expected {
		if !seen {
			t.Errorf("expected ROUTES_TO relationship %s -> %s, not found", k.from, k.to)
		}
	}

	// Sanity: yaml_driven property is set on at least one matching edge.
	var found bool
	for _, r := range result.Relationships {
		if r.Kind == "ROUTES_TO" && r.FromID == "Route:users" && r.Properties["pattern_type"] == "yaml_driven" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DRF router ROUTES_TO edge with pattern_type=yaml_driven")
	}
}

// sampleNonRouterRegisters exercises non-router `.register(...)` call sites
// that previously matched the unanchored DRF pattern and produced spurious
// Route entities. The tightened receiver anchor (router-like names only)
// must reject all of these. Refs #63.
const sampleNonRouterRegisters = `from django.dispatch import Signal
from myapp.queues import queue
from flask import Blueprint
from myapp.events import events

signal = Signal()
signal.register('user_created', some_handler)
queue.register('email_jobs', email_worker)
blueprint = Blueprint('admin', __name__)
blueprint.register('admin_panel', AdminView)
events.register('on_login', login_callback)
`

// TestDetect_NonRouterRegister_NoFalsePositives asserts that .register(...)
// calls on non-router receivers do NOT emit Route entities or ROUTES_TO
// edges from the DRF rule. Refs #63.
func TestDetect_NonRouterRegister_NoFalsePositives(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	det := New(rules)
	result, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     "myapp/handlers.py",
		Content:  []byte(sampleNonRouterRegisters),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	forbidden := map[string]bool{
		"user_created": true,
		"email_jobs":   true,
		"admin_panel":  true,
		"on_login":     true,
	}

	for _, e := range result.Entities {
		if e.Kind != "Route" {
			continue
		}
		if forbidden[e.Name] {
			t.Errorf("unexpected Route entity from non-router .register: %q", e.Name)
		}
	}

	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		// Strip the "Route:" prefix to compare with forbidden names.
		const p = "Route:"
		if len(r.FromID) > len(p) && r.FromID[:len(p)] == p {
			name := r.FromID[len(p):]
			if forbidden[name] {
				t.Errorf("unexpected ROUTES_TO edge from non-router .register: %s -> %s", r.FromID, r.ToID)
			}
		}
	}
}
