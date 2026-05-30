// Package lua — smoke tests for Lua custom extractors.
package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

func makeFile(path, content string) extractor.FileInput {
	return extractor.FileInput{
		Path:    path,
		Content: []byte(content),
	}
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func TestLuaRoutingOpenResty(t *testing.T) {
	src := `
nginx.conf content:
location /api/users {
    content_by_lua_block {
        local user = ngx.var.uri
    }
}
location = /health {
    content_by_lua_file /path/to/health.lua;
}
`
	e := &luaRoutingExtractor{}
	got, err := e.Extract(context.Background(), makeFile("nginx.conf", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected location entities, got none")
	}
	var paths []string
	for _, r := range got {
		if p, ok := r.Properties["path"]; ok {
			paths = append(paths, p)
		}
	}
	found := false
	for _, p := range paths {
		if strings.Contains(p, "/api/users") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /api/users route, got %v", paths)
	}
}

func TestLuaRoutingLapis(t *testing.T) {
	src := `
local lapis = require("lapis")
local app = lapis.Application()

app:get("/users", function(self)
    return { json = { users = {} } }
end)

app:post("/users", function(self)
    return { json = { created = true } }
end)

app:match("user_show", "/users/:id", function(self)
    return { render = "users/show" }
end)
`
	e := &luaRoutingExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected route entities, got none")
	}
	var methods []string
	for _, r := range got {
		if m, ok := r.Properties["method"]; ok {
			methods = append(methods, m)
		}
	}
	hasGET, hasPOST := false, false
	for _, m := range methods {
		if m == "GET" {
			hasGET = true
		}
		if m == "POST" {
			hasPOST = true
		}
	}
	if !hasGET || !hasPOST {
		t.Errorf("expected GET and POST routes, got methods %v", methods)
	}
}

// TestLuaRoutingLapisCanonical asserts `:id`→{id} normalisation and the
// unnamed app:match("/path", fn) form are captured.
func TestLuaRoutingLapisCanonical(t *testing.T) {
	src := `
local app = lapis.Application()
app:get("/users/:id", function(self) end)
app:match("/about", function(self) end)
app:match("named", "/things/:thing_id", function(self) end)
`
	e := &luaRoutingExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	canon := map[string]string{} // path -> canonical_path
	kinds := map[string]bool{}
	for _, r := range got {
		if p, ok := r.Properties["path"]; ok {
			canon[p] = r.Properties["canonical_path"]
		}
		kinds[r.Properties["kind"]] = true
	}
	if canon["/users/:id"] != "/users/{id}" {
		t.Errorf("verb route canonical_path=%q, want /users/{id}", canon["/users/:id"])
	}
	if canon["/things/:thing_id"] != "/things/{thing_id}" {
		t.Errorf("named route canonical_path=%q, want /things/{thing_id}", canon["/things/:thing_id"])
	}
	if !kinds["anon_route"] {
		t.Errorf("expected anon_route kind for app:match(\"/about\", ...); kinds=%v", kinds)
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func TestLuaAuthJWT(t *testing.T) {
	src := `
local jwt = require("resty.jwt")

local function verify_token(token)
    local jwt_obj = jwt:verify("my_secret", token)
    if not jwt_obj.verified then
        ngx.exit(401)
    end
    return jwt_obj.payload
end
`
	e := &luaAuthExtractor{}
	got, err := e.Extract(context.Background(), makeFile("auth.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected JWT auth entities, got none")
	}
	hasJWT := false
	for _, r := range got {
		if r.Properties["library"] == "resty.jwt" {
			hasJWT = true
		}
	}
	if !hasJWT {
		t.Error("expected resty.jwt library signal")
	}
}

// TestLuaAuthOIDC asserts lua-resty-openidc detection with auth_method=oidc.
func TestLuaAuthOIDC(t *testing.T) {
	src := `
local openidc = require("resty.openidc")
local res, err = openidc.authenticate(opts)
if not res then
  ngx.exit(401)
end
`
	e := &luaAuthExtractor{}
	got, err := e.Extract(context.Background(), makeFile("auth.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	hasAuthenticate := false
	for _, r := range got {
		if r.Properties["kind"] == "oidc_authenticate" {
			hasAuthenticate = true
			if r.Properties["auth_method"] != "oidc" {
				t.Errorf("oidc: auth_method=%q, want oidc", r.Properties["auth_method"])
			}
		}
	}
	if !hasAuthenticate {
		t.Error("expected openidc.authenticate guard entity")
	}
}

// TestLuaAuthRequireLogin asserts Lapis @require_login is captured as a
// session-method auth guard.
func TestLuaAuthRequireLogin(t *testing.T) {
	src := `
local app = lapis.Application()
app:before_filter(require_login)
app:get("/dashboard", function(self) end)
`
	e := &luaAuthExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range got {
		if r.Properties["kind"] == "require_login" {
			found = true
			if r.Properties["auth_method"] != "session" {
				t.Errorf("require_login: auth_method=%q, want session", r.Properties["auth_method"])
			}
		}
	}
	if !found {
		t.Error("expected require_login auth guard entity")
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func TestLuaMiddlewareOpenResty(t *testing.T) {
	src := `
access_by_lua_block {
    -- check auth
}
rewrite_by_lua_block {
    -- rewrite URL
}
header_filter_by_lua_block {
    ngx.header["X-Custom"] = "value"
}
`
	e := &luaMiddlewareExtractor{}
	got, err := e.Extract(context.Background(), makeFile("nginx.conf", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected middleware entities, got none")
	}
	phases := map[string]bool{}
	for _, r := range got {
		if p, ok := r.Properties["phase"]; ok {
			phases[p] = true
		}
	}
	if !phases["access_by_lua_block"] {
		t.Errorf("expected access_by_lua_block phase, got %v", phases)
	}
}

// TestLuaMiddlewareOrdering asserts the OpenResty phase chain carries both a
// textual chain_index and a canonical lifecycle phase_order, so the middleware
// chain is reconstructable. The fixture lists phases OUT of lifecycle order to
// prove phase_order reflects the request lifecycle, not file position.
func TestLuaMiddlewareOrdering(t *testing.T) {
	src := `
log_by_lua_block { ngx.log(ngx.INFO, "done") }
access_by_lua_block { check_auth() }
rewrite_by_lua_block { ngx.req.set_uri("/v2") }
`
	e := &luaMiddlewareExtractor{}
	got, err := e.Extract(context.Background(), makeFile("nginx.conf", src))
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]string{}    // phase -> phase_order
	chainIdx := map[string]string{} // phase -> chain_index
	for _, r := range got {
		if p, ok := r.Properties["phase"]; ok && r.Properties["kind"] == "nginx_phase" {
			order[p] = r.Properties["phase_order"]
			chainIdx[p] = r.Properties["chain_index"]
		}
	}
	// Lifecycle: rewrite(2) < access(3) < log(7).
	if order["rewrite_by_lua_block"] != "2" {
		t.Errorf("rewrite phase_order=%q, want 2", order["rewrite_by_lua_block"])
	}
	if order["access_by_lua_block"] != "3" {
		t.Errorf("access phase_order=%q, want 3", order["access_by_lua_block"])
	}
	if order["log_by_lua_block"] != "7" {
		t.Errorf("log phase_order=%q, want 7", order["log_by_lua_block"])
	}
	// Textual order in the fixture: log(0) < access(1) < rewrite(2).
	if chainIdx["log_by_lua_block"] != "0" || chainIdx["access_by_lua_block"] != "1" || chainIdx["rewrite_by_lua_block"] != "2" {
		t.Errorf("chain_index mismatch: %v", chainIdx)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestLuaValidationOpenResty(t *testing.T) {
	src := `
ngx.req.read_body()
local args = ngx.req.get_post_args()
local data = cjson.decode(ngx.req.get_body_data())
if not args.name then
    ngx.exit(400)
end
`
	e := &luaValidationExtractor{}
	got, err := e.Extract(context.Background(), makeFile("handler.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected validation entities, got none")
	}
}

func TestLuaValidationLapis(t *testing.T) {
	src := `
local capture_errors = require("lapis.application").capture_errors
local validate = require("lapis.validate")

app:post("/users", capture_errors(function(self)
    validate.assert_valid(self.params, {
        { "username", exists = true, min_length = 3 }
    })
    return { json = { ok = true } }
end))
`
	e := &luaValidationExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected validation entities for Lapis, got none")
	}
}

func TestLuaValidationLapisFieldRules(t *testing.T) {
	src := `
local validate = require("lapis.validate")
app:post("/users", capture_errors(function(self)
    validate.assert_valid(self.params, {
        { "username", exists = true, min_length = 3 },
        { "email", exists = true, matches_pattern = "^.+@.+$" }
    })
    return { json = { ok = true } }
end))
`
	e := &luaValidationExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	// Value assertion: the specific (field, rule) pairs must be captured.
	want := map[string]bool{
		"username.exists":       false,
		"username.min_length":   false,
		"email.exists":          false,
		"email.matches_pattern": false,
	}
	for _, r := range got {
		if r.Properties["kind"] != "field_rule" {
			continue
		}
		key := r.Properties["field"] + "." + r.Properties["rule"]
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing field+rule entity %q", k)
		}
	}
}

func TestLuaValidationLapisCSRF(t *testing.T) {
	src := `
local csrf = require("lapis.csrf")
app:post("/form", capture_errors(function(self)
    csrf.assert_token(self)
    return { redirect_to = "/" }
end))
`
	e := &luaValidationExtractor{}
	got, err := e.Extract(context.Background(), makeFile("app.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	hasCSRF := false
	for _, r := range got {
		if r.Properties["kind"] == "csrf_token" {
			hasCSRF = true
		}
	}
	if !hasCSRF {
		t.Error("expected csrf_token validation entity")
	}
}

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

func TestLuaObservabilityLog(t *testing.T) {
	src := `
local function handle(self)
    ngx.log(ngx.INFO, "request received")
    ngx.log(ngx.ERR, "error occurred: " .. tostring(err))
end
`
	e := &luaObservabilityExtractor{}
	got, err := e.Extract(context.Background(), makeFile("handler.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected observability entities, got none")
	}
	hasLog := false
	for _, r := range got {
		if r.Properties["kind"] == "log" {
			hasLog = true
		}
	}
	if !hasLog {
		t.Error("expected log kind entity")
	}
}

func TestLuaObservabilityMetrics(t *testing.T) {
	src := `
local prometheus = require("resty.prometheus")
local metric_requests = prometheus:counter("requests_total", "Total requests")
local metric_latency = prometheus:histogram("request_duration_ms", "Request latency")
`
	e := &luaObservabilityExtractor{}
	got, err := e.Extract(context.Background(), makeFile("metrics.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	hasMetric := false
	for _, r := range got {
		if r.Properties["library"] == "resty.prometheus" {
			hasMetric = true
		}
	}
	if !hasMetric {
		t.Error("expected prometheus metric entity")
	}

	// Value-asserting: the specific metric names must be resolved from the literals.
	names := map[string]string{}
	for _, r := range got {
		if n := r.Properties["metric_name"]; n != "" {
			names[n] = r.Properties["kind"]
		}
	}
	if names["requests_total"] != "counter" {
		t.Errorf("expected counter metric_name=requests_total, got names=%v", names)
	}
	if names["request_duration_ms"] != "histogram" {
		t.Errorf("expected histogram metric_name=request_duration_ms, got names=%v", names)
	}
}

func TestLuaObservabilityMetricNameUnresolved(t *testing.T) {
	// Non-literal name (variable) must NOT be falsely resolved.
	src := `
local prometheus = require("resty.prometheus")
local name = "dynamic_metric"
local m = prometheus:gauge(name)
`
	e := &luaObservabilityExtractor{}
	got, err := e.Extract(context.Background(), makeFile("metrics.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Properties["kind"] == "gauge" {
			if r.Properties["metric_name"] != "<unresolved>" {
				t.Errorf("expected unresolved gauge name, got %q", r.Properties["metric_name"])
			}
		}
	}
}

func TestLuaObservabilityTraceSpanName(t *testing.T) {
	src := `
local span = tracer:start_span("handle_request")
kong.tracing.start_span("db.query")
span:set_attribute("http.status", 200)
`
	e := &luaObservabilityExtractor{}
	got, err := e.Extract(context.Background(), makeFile("trace.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	spanNames := map[string]bool{}
	for _, r := range got {
		if n := r.Properties["span_name"]; n != "" {
			spanNames[n] = true
		}
	}
	if !spanNames["handle_request"] {
		t.Errorf("expected otel span_name=handle_request, got %v", spanNames)
	}
	if !spanNames["db.query"] {
		t.Errorf("expected kong span_name=db.query, got %v", spanNames)
	}
}

// ---------------------------------------------------------------------------
// Testing
// ---------------------------------------------------------------------------

func TestLuaTestingBusted(t *testing.T) {
	src := `
describe("User API", function()
    local app

    setup(function()
        app = require("app")
    end)

    it("returns 200 on GET /users", function()
        local status, body = app:handle(request)
        assert.equal(200, status)
    end)

    it("validates required fields", function()
        assert.has_error(function()
            app:create_user({})
        end)
    end)
end)
`
	e := &luaTestingExtractor{}
	got, err := e.Extract(context.Background(), makeFile("spec/user_spec.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected testing entities, got none")
	}
	hasSuite := false
	for _, r := range got {
		if r.Subtype == "test_suite" {
			hasSuite = true
		}
	}
	if !hasSuite {
		t.Error("expected test_suite entity for describe block")
	}
}
