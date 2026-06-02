package engine

import "testing"

// rlProps reuses the auth pass test harness (full detector run) and projects
// the rate-limit property contract for each synthetic endpoint.
func rlProps(t *testing.T, language, path, content string) map[string]rateLimitEndpoint {
	t.Helper()
	raw := authProps(t, language, path, content)
	out := map[string]rateLimitEndpoint{}
	for k, e := range raw {
		out[k] = rateLimitEndpoint{
			limited: e.Properties["rate_limited"],
			rate:    e.Properties["rate_limit"],
			scope:   e.Properties["rate_limit_scope"],
			source:  e.Properties["rate_limit_source"],
		}
	}
	return out
}

type rateLimitEndpoint struct {
	limited, rate, scope, source string
}

func mustEndpoint(t *testing.T, eps map[string]rateLimitEndpoint, key string) rateLimitEndpoint {
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

// Route-level limiter resolves windowMs+max → human rate, stamped on exactly
// the endpoint it is applied to.
func TestRateLimit_ExpressRouteLevel(t *testing.T) {
	src := `
const express = require('express');
const rateLimit = require('express-rate-limit');
const app = express();
const limiter = rateLimit({ windowMs: 60000, max: 100 });
app.get('/api/x', limiter, (req, res) => res.send('x'));
app.get('/api/open', (req, res) => res.send('ok'));
`
	eps := rlProps(t, "typescript", "app.ts", src)
	x := mustEndpoint(t, eps, "GET /api/x")
	if x.limited != "true" {
		t.Fatalf("GET /api/x: rate_limited=%q, want true", x.limited)
	}
	if x.rate != "100/60s" {
		t.Errorf("GET /api/x: rate_limit=%q, want 100/60s", x.rate)
	}
	if x.scope != "route" {
		t.Errorf("GET /api/x: scope=%q, want route", x.scope)
	}
	// Negative: an endpoint with no limiter is not stamped.
	open := mustEndpoint(t, eps, "GET /api/open")
	if open.limited != "" {
		t.Errorf("GET /api/open: rate_limited=%q, want empty (not fabricated)", open.limited)
	}
}

// Inline factory call applied directly to a route, with a `15 * 60 * 1000`
// windowMs arithmetic product → 900s.
func TestRateLimit_ExpressInlineProductWindow(t *testing.T) {
	src := `
const express = require('express');
const rateLimit = require('express-rate-limit');
const app = express();
app.post('/login', rateLimit({ windowMs: 15 * 60 * 1000, max: 5 }), (req, res) => res.end());
`
	eps := rlProps(t, "typescript", "app.ts", src)
	login := mustEndpoint(t, eps, "POST /login")
	if login.limited != "true" || login.rate != "5/900s" {
		t.Errorf("POST /login: limited=%q rate=%q, want true 5/900s", login.limited, login.rate)
	}
}

// App-level `app.use(limiter)` applies to every endpoint in file scope.
func TestRateLimit_ExpressAppLevel(t *testing.T) {
	src := `
const express = require('express');
const rateLimit = require('express-rate-limit');
const app = express();
const apiLimiter = rateLimit({ windowMs: 3600000, max: 1000 });
app.use(apiLimiter);
app.get('/a', (req, res) => res.end());
app.get('/b', (req, res) => res.end());
`
	eps := rlProps(t, "typescript", "app.ts", src)
	for _, key := range []string{"GET /a", "GET /b"} {
		e := mustEndpoint(t, eps, key)
		if e.limited != "true" {
			t.Errorf("%s: rate_limited=%q, want true", key, e.limited)
		}
		if e.rate != "1000/3600s" {
			t.Errorf("%s: rate_limit=%q, want 1000/3600s", key, e.rate)
		}
		if e.scope != "app" {
			t.Errorf("%s: scope=%q, want app", key, e.scope)
		}
	}
}

// Negative: a limiter defined but never applied to any route → no stamp.
func TestRateLimit_ExpressDefinedButNotApplied(t *testing.T) {
	src := `
const express = require('express');
const rateLimit = require('express-rate-limit');
const app = express();
const limiter = rateLimit({ windowMs: 60000, max: 100 });
app.get('/free', (req, res) => res.end());
`
	eps := rlProps(t, "typescript", "app.ts", src)
	free := mustEndpoint(t, eps, "GET /free")
	if free.limited != "" {
		t.Errorf("GET /free: rate_limited=%q, want empty (limiter never applied)", free.limited)
	}
}

// Honest-partial: an imported limiter binding (factory call not in this file)
// → rate_limited=true with rate omitted, never fabricated.
func TestRateLimit_ExpressImportedLimiterHonestPartial(t *testing.T) {
	src := `
const express = require('express');
const { rateLimiter } = require('./middleware');
const app = express();
app.get('/imported', rateLimiter, (req, res) => res.end());
`
	eps := rlProps(t, "typescript", "app.ts", src)
	e := mustEndpoint(t, eps, "GET /imported")
	if e.limited != "true" {
		t.Errorf("GET /imported: rate_limited=%q, want true", e.limited)
	}
	if e.rate != "" {
		t.Errorf("GET /imported: rate_limit=%q, want empty (unresolved, honest-partial)", e.rate)
	}
}

// Unit-level rate resolution.
func TestResolveJSRate(t *testing.T) {
	cases := []struct{ body, want string }{
		{"windowMs: 60000, max: 100", "100/60s"},
		{"windowMs: 1000, max: 5", "5/1s"},
		{"windowMs: 3600000, max: 1000", "1000/3600s"},
		{"windowMs: 15 * 60 * 1000, max: 5", "5/900s"},
		{"max: 100", ""},        // no window → unresolved
		{"windowMs: 60000", ""}, // no max → unresolved
		{"windowMs: 500, max: 2", "2/500ms"},
	}
	for _, c := range cases {
		if got := resolveJSRate(c.body); got != c.want {
			t.Errorf("resolveJSRate(%q)=%q, want %q", c.body, got, c.want)
		}
	}
}
