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
