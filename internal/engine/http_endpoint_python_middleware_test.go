package engine

import (
	"encoding/json"
	"testing"
)

// mwChain decodes the middleware_chain JSON stamped on an endpoint into the
// shared middlewareEntry slice for order/scope/name assertions.
func mwChain(t *testing.T, raw string) []middlewareEntry {
	t.Helper()
	if raw == "" {
		return nil
	}
	var chain []middlewareEntry
	if err := json.Unmarshal([]byte(raw), &chain); err != nil {
		t.Fatalf("middleware_chain not valid JSON: %v (%q)", err, raw)
	}
	return chain
}

// endpointMW runs the full synthesis pass and returns the decoded chain + props
// for the endpoint at "<VERB> <path>".
func endpointMW(t *testing.T, language, path, content, key string) (chain []middlewareEntry, count, names, scope string) {
	t.Helper()
	eps := authProps(t, language, path, content)
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got: %v)", key, keys)
	}
	return mwChain(t, e.Properties["middleware_chain"]),
		e.Properties["middleware_count"],
		e.Properties["middleware_names"],
		e.Properties["middleware_scope"]
}

// TestMiddleware_DjangoGlobal asserts the Django settings.MIDDLEWARE ordered
// list is bound to a same-file route op in declaration order, scope=global.
func TestMiddleware_DjangoGlobal(t *testing.T) {
	src := `
from django.urls import path, include
from rest_framework.routers import DefaultRouter
from myapp.views import DashboardViewSet

MIDDLEWARE = [
    'django.middleware.security.SecurityMiddleware',
    'django.contrib.sessions.middleware.SessionMiddleware',
    'django.contrib.auth.middleware.AuthenticationMiddleware',
]

router = DefaultRouter()
router.register(r'dashboard', DashboardViewSet)

urlpatterns = [
    path('api/', include(router.urls)),
]
`
	chain, count, names, scope := endpointMW(t, "python", "myapp/urls.py", src, "ANY /api/dashboard")
	if len(chain) != 3 {
		t.Fatalf("chain len=%d, want 3 (names=%q)", len(chain), names)
	}
	want := []string{"SecurityMiddleware", "SessionMiddleware", "AuthenticationMiddleware"}
	for i, w := range want {
		if chain[i].Name != w {
			t.Errorf("chain[%d].Name=%q, want %q", i, chain[i].Name, w)
		}
		if chain[i].Order != i {
			t.Errorf("chain[%d].Order=%d, want %d", i, chain[i].Order, i)
		}
		if chain[i].Scope != pythonMWScopeGlobal {
			t.Errorf("chain[%d].Scope=%q, want global", i, chain[i].Scope)
		}
	}
	// AuthenticationMiddleware carries the auth_kind tag (auth IN the chain).
	if chain[2].AuthKind != "auth" {
		t.Errorf("AuthenticationMiddleware auth_kind=%q, want auth", chain[2].AuthKind)
	}
	if count != "3" {
		t.Errorf("count=%q, want 3", count)
	}
	if scope != "global" {
		t.Errorf("scope=%q, want global", scope)
	}
}

// TestMiddleware_DRFView asserts a DRF view's permission_classes +
// throttle_classes bind to its endpoints (scope=view) with auth_kind on the
// permission.
func TestMiddleware_DRFView(t *testing.T) {
	src := `
from rest_framework import viewsets
from rest_framework.permissions import IsAuthenticated
from rest_framework.throttling import UserRateThrottle
from rest_framework.routers import DefaultRouter
from django.urls import path, include

class ReportViewSet(viewsets.ModelViewSet):
    permission_classes = [IsAuthenticated]
    throttle_classes = [UserRateThrottle]
    queryset = Report.objects.all()

router = DefaultRouter()
router.register(r'reports', ReportViewSet)
urlpatterns = [ path('api/', include(router.urls)) ]
`
	// The DRF ViewSet is registered same-file; its permission/throttle classes
	// bind to the composed /api/reports endpoints via the router prefix.
	eps := authProps(t, "python", "myapp/urls.py", src)
	var found bool
	for key, e := range eps {
		chain := mwChain(t, e.Properties["middleware_chain"])
		if len(chain) == 0 {
			continue
		}
		// Find the endpoint carrying the view chain.
		var hasPerm, hasThrottle bool
		for _, c := range chain {
			if c.Name == "IsAuthenticated" {
				hasPerm = true
				if c.AuthKind != "auth" {
					t.Errorf("%s: IsAuthenticated auth_kind=%q, want auth", key, c.AuthKind)
				}
				if c.Scope != pythonMWScopeView {
					t.Errorf("%s: IsAuthenticated scope=%q, want view", key, c.Scope)
				}
			}
			if c.Name == "UserRateThrottle" {
				hasThrottle = true
			}
		}
		if hasPerm && hasThrottle {
			found = true
		}
	}
	if !found {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("no endpoint carried the DRF view chain (IsAuthenticated + UserRateThrottle); got endpoints: %v", keys)
	}
}

// TestMiddleware_FastAPIGlobalAndRoute asserts app.add_middleware → global entry
// and a per-route dependencies=[Depends(...)] → route entry, both on a route.
func TestMiddleware_FastAPIGlobalAndRoute(t *testing.T) {
	src := `
from fastapi import FastAPI, Depends
from starlette.middleware.cors import CORSMiddleware

app = FastAPI()
app.add_middleware(CORSMiddleware, allow_origins=["*"])

def verify_token():
    ...

@app.get("/items/{item_id}", dependencies=[Depends(verify_token)])
async def read_item(item_id: int):
    return {}
`
	chain, count, names, scope := endpointMW(t, "python", "main.py", src, "GET /items/{item_id}")
	if len(chain) != 2 {
		t.Fatalf("chain len=%d, want 2 (names=%q count=%q)", len(chain), names, count)
	}
	// Outermost: global CORSMiddleware; innermost: route dependency.
	if chain[0].Name != "CORSMiddleware" || chain[0].Scope != pythonMWScopeGlobal {
		t.Errorf("chain[0]=%+v, want CORSMiddleware/global", chain[0])
	}
	if chain[1].Name != "verify_token" || chain[1].Scope != pythonMWScopeRoute {
		t.Errorf("chain[1]=%+v, want verify_token/route", chain[1])
	}
	if scope != "global+route" {
		t.Errorf("scope=%q, want global+route", scope)
	}
}

// TestMiddleware_DjangoDynamicSkipped asserts a MIDDLEWARE list assembled at
// runtime (concat) is NOT bound — honest-partial.
func TestMiddleware_DjangoDynamicSkipped(t *testing.T) {
	src := `
from django.urls import path, include
from rest_framework.routers import DefaultRouter
from myapp.views import XViewSet

BASE_MIDDLEWARE = ['a.B']
MIDDLEWARE = BASE_MIDDLEWARE + EXTRA_MIDDLEWARE

router = DefaultRouter()
router.register(r'x', XViewSet)
urlpatterns = [ path('api/', include(router.urls)) ]
`
	chain, count, _, _ := endpointMW(t, "python", "myapp/urls.py", src, "ANY /api/x")
	if len(chain) != 0 || (count != "" && count != "0") {
		t.Errorf("dynamic MIDDLEWARE bound a chain (len=%d count=%q), want none", len(chain), count)
	}
}
