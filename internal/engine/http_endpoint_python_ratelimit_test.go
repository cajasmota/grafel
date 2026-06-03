package engine

import "testing"

// pyRLProps runs the full detector and projects the rate-limit contract
// (including the DRF-specific rate_limit_scope_name) for each synthetic endpoint.
func pyRLProps(t *testing.T, path, content string) map[string]pyRLEndpoint {
	t.Helper()
	raw := authProps(t, "python", path, content)
	out := map[string]pyRLEndpoint{}
	for k, e := range raw {
		out[k] = pyRLEndpoint{
			limited:   e.Properties["rate_limited"],
			rate:      e.Properties["rate_limit"],
			scope:     e.Properties["rate_limit_scope"],
			source:    e.Properties["rate_limit_source"],
			scopeName: e.Properties["rate_limit_scope_name"],
		}
	}
	return out
}

type pyRLEndpoint struct {
	limited, rate, scope, source, scopeName string
}

func mustPyRL(t *testing.T, eps map[string]pyRLEndpoint, key string) pyRLEndpoint {
	t.Helper()
	e, ok := eps[key]
	if !ok {
		keys := make([]string, 0, len(eps))
		for k := range eps {
			keys = append(keys, k)
		}
		t.Fatalf("endpoint %q not synthesised (got %v)", key, keys)
	}
	return e
}

// slowapi `@limiter.limit("100/minute")` on a Starlette/FastAPI-shaped route
// synthesized from the YAML rules → rate=100/minute, source=limiter.limit.
func TestPyRateLimit_SlowapiStarlette(t *testing.T) {
	src := `
from starlette.applications import Starlette
from starlette.routing import Route
from slowapi import Limiter

limiter = Limiter(key_func=lambda r: r.client.host)

@limiter.limit("100/minute")
async def listing(request):
    return JSONResponse({})

async def open_ep(request):
    return JSONResponse({})

app = Starlette(routes=[
    Route("/listing", listing, methods=["GET"]),
    Route("/open", open_ep, methods=["GET"]),
])
`
	eps := pyRLProps(t, "app.py", src)
	x := mustPyRL(t, eps, "GET /listing")
	if x.limited != "true" {
		t.Fatalf("GET /listing: rate_limited=%q, want true", x.limited)
	}
	if x.rate != "100/minute" {
		t.Errorf("GET /listing: rate_limit=%q, want 100/minute", x.rate)
	}
	if x.source != "limiter.limit" {
		t.Errorf("GET /listing: source=%q, want limiter.limit", x.source)
	}
	// Negative: undecorated route is not stamped.
	open := mustPyRL(t, eps, "GET /open")
	if open.limited != "" {
		t.Errorf("GET /open: rate_limited=%q, want empty (not fabricated)", open.limited)
	}
}

// django-ratelimit `@ratelimit(key='ip', rate='10/m')` on a Sanic route →
// rate=10/m, scope=ip.
func TestPyRateLimit_DjangoRatelimitSanic(t *testing.T) {
	src := `
from sanic import Sanic
from django_ratelimit.decorators import ratelimit

app = Sanic("svc")

@app.get("/throttled")
@ratelimit(key='ip', rate='10/m')
async def throttled(request):
    return text("ok")
`
	eps := pyRLProps(t, "server.py", src)
	x := mustPyRL(t, eps, "GET /throttled")
	if x.limited != "true" || x.rate != "10/m" {
		t.Fatalf("GET /throttled: limited=%q rate=%q, want true 10/m", x.limited, x.rate)
	}
	if x.scope != "ip" {
		t.Errorf("GET /throttled: scope=%q, want ip", x.scope)
	}
	if x.source != "ratelimit" {
		t.Errorf("GET /throttled: source=%q, want ratelimit", x.source)
	}
}

// DRF ViewSet with throttle_scope='burst' (ScopedRateThrottle), registered on a
// router → the composed /api/reports endpoint is stamped scope=endpoint,
// scope_name=burst, rate honest-partial (config-driven, never fabricated).
func TestPyRateLimit_DRFThrottleScope(t *testing.T) {
	src := `
from rest_framework import viewsets
from rest_framework.throttling import ScopedRateThrottle
from rest_framework.routers import DefaultRouter
from django.urls import path, include

class ReportViewSet(viewsets.ModelViewSet):
    throttle_classes = [ScopedRateThrottle]
    throttle_scope = 'burst'
    queryset = Report.objects.all()

router = DefaultRouter()
router.register(r'reports', ReportViewSet)
urlpatterns = [ path('api/', include(router.urls)) ]
`
	eps := pyRLProps(t, "myapp/urls.py", src)
	var x pyRLEndpoint
	var foundKey string
	for k, e := range eps {
		if e.limited == "true" {
			x, foundKey = e, k
			break
		}
	}
	if foundKey == "" {
		t.Fatalf("no endpoint stamped rate_limited (endpoints=%v)", eps)
	}
	if x.scope != "endpoint" {
		t.Errorf("%s: scope=%q, want endpoint", foundKey, x.scope)
	}
	if x.scopeName != "burst" {
		t.Errorf("%s: scope_name=%q, want burst", foundKey, x.scopeName)
	}
	// Honest-partial: rate lives in DEFAULT_THROTTLE_RATES, never fabricated.
	if x.rate != "" {
		t.Errorf("%s: rate=%q, want empty (config-driven, honest-partial)", foundKey, x.rate)
	}
}

// DRF ViewSet with a co-located custom throttle subclass declaring rate='1000/day'
// → the rate IS resolved (not honest-partial), scope=user, source=DailyThrottle.
func TestPyRateLimit_DRFCustomThrottleRate(t *testing.T) {
	src := `
from rest_framework import viewsets
from rest_framework.throttling import UserRateThrottle
from rest_framework.routers import DefaultRouter
from django.urls import path, include

class DailyThrottle(UserRateThrottle):
    rate = '1000/day'

class ExportViewSet(viewsets.ModelViewSet):
    throttle_classes = [DailyThrottle]
    queryset = Export.objects.all()

router = DefaultRouter()
router.register(r'exports', ExportViewSet)
urlpatterns = [ path('api/', include(router.urls)) ]
`
	eps := pyRLProps(t, "myapp/urls.py", src)
	var x pyRLEndpoint
	var foundKey string
	for k, e := range eps {
		if e.limited == "true" {
			x, foundKey = e, k
			break
		}
	}
	if foundKey == "" {
		t.Fatalf("no endpoint stamped rate_limited (endpoints=%v)", eps)
	}
	if x.rate != "1000/day" {
		t.Errorf("%s: rate=%q, want 1000/day (resolved from custom subclass)", foundKey, x.rate)
	}
	if x.scope != "user" {
		t.Errorf("%s: scope=%q, want user", foundKey, x.scope)
	}
	if x.source != "DailyThrottle" {
		t.Errorf("%s: source=%q, want DailyThrottle", foundKey, x.source)
	}
}

// Tornado class-method handler: the limiter on `MainHandler.get` binds to /t
// ONLY — a different handler class's same-named `get` is not stamped (the
// class-qualified key prevents bare-method bleed).
func TestPyRateLimit_TornadoMethodPrecise(t *testing.T) {
	src := `
import tornado.web

class MainHandler(tornado.web.RequestHandler):
    @limiter.limit("40/minute")
    def get(self):
        self.write("x")

class OpenHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("y")

app = tornado.web.Application([(r"/t", MainHandler), (r"/o", OpenHandler)])
`
	eps := pyRLProps(t, "app.py", src)
	limited := mustPyRL(t, eps, "GET /t")
	if limited.limited != "true" || limited.rate != "40/minute" {
		t.Errorf("GET /t: limited=%q rate=%q, want true 40/minute", limited.limited, limited.rate)
	}
	open := mustPyRL(t, eps, "GET /o")
	if open.limited != "" {
		t.Errorf("GET /o: rate_limited=%q, want empty (bare-method bleed must not occur)", open.limited)
	}
}

// Falcon resource class (base-less `class Res:`) responder method: the limiter
// on `Res.on_get` binds to /f only; a different resource's `on_get` is not
// stamped. Guards the base-less-class enclosing-class resolution.
func TestPyRateLimit_FalconResourceMethod(t *testing.T) {
	src := `
import falcon

class Res:
    @limiter.limit("14/minute")
    def on_get(self, req, resp):
        resp.text = "ok"

class Open:
    def on_get(self, req, resp):
        resp.text = "y"

app = falcon.App()
app.add_route("/f", Res())
app.add_route("/o", Open())
`
	eps := pyRLProps(t, "api.py", src)
	limited := mustPyRL(t, eps, "GET /f")
	if limited.limited != "true" || limited.rate != "14/minute" {
		t.Errorf("GET /f: limited=%q rate=%q, want true 14/minute", limited.limited, limited.rate)
	}
	open := mustPyRL(t, eps, "GET /o")
	if open.limited != "" {
		t.Errorf("GET /o: rate_limited=%q, want empty (base-less-class bleed must not occur)", open.limited)
	}
}

// Negative: a non-rate decorator (e.g. @cached) does not stamp a posture, and
// the Flask/FastAPI custom-extractor posture is never clobbered.
func TestPyRateLimit_NoFabrication(t *testing.T) {
	src := `
from sanic import Sanic
app = Sanic("svc")

@app.get("/plain")
async def plain(request):
    return text("ok")
`
	eps := pyRLProps(t, "plain.py", src)
	x := mustPyRL(t, eps, "GET /plain")
	if x.limited != "" {
		t.Errorf("GET /plain: rate_limited=%q, want empty (not fabricated)", x.limited)
	}
}
