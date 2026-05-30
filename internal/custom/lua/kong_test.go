// Package lua — value-asserting tests for the Kong + APISIX gateway extractor.
package lua

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// recView is a thin read-only view over an emitted EntityRecord used by the
// assertions below to look up a property without repeated nil-checks.
type recView struct{ rec types.EntityRecord }

func newRecView(r types.EntityRecord) recView { return recView{rec: r} }

func (v recView) prop(k string) string     { return v.rec.Properties[k] }
func (v recView) props() map[string]string { return v.rec.Properties }

// ---------------------------------------------------------------------------
// Kong
// ---------------------------------------------------------------------------

func TestLuaKongHandlerPlugin(t *testing.T) {
	src := `
local MyAuthPlugin = {
  PRIORITY = 1000,
  VERSION = "2.1.0",
}

function MyAuthPlugin:init_worker()
end

function MyAuthPlugin:access(conf)
  local token = kong.request.get_header("authorization")
  if not token then
    return kong.response.exit(401)
  end
end

function MyAuthPlugin:log(conf)
  kong.log("done")
end

return MyAuthPlugin
`
	e := &luaKongExtractor{}
	got, err := e.Extract(context.Background(), makeFile("kong/plugins/my-auth/handler.lua", src))
	if err != nil {
		t.Fatal(err)
	}

	// Plugin entity must exist with name from the path, priority, version, and
	// the auth signal (name contains "auth").
	var plugin *recView
	var phases = map[string]*recView{}
	for i := range got {
		r := newRecView(got[i])
		switch r.prop("kind") {
		case "plugin":
			plugin = &r
		case "plugin_phase":
			ph := r.prop("phase")
			rc := r
			phases[ph] = &rc
		}
	}

	if plugin == nil {
		t.Fatal("expected a kong plugin entity")
	}
	if plugin.prop("plugin_name") != "my-auth" {
		t.Errorf("plugin_name = %q, want my-auth", plugin.prop("plugin_name"))
	}
	if plugin.prop("priority") != "1000" {
		t.Errorf("priority = %q, want 1000", plugin.prop("priority"))
	}
	if plugin.prop("version") != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", plugin.prop("version"))
	}
	if plugin.prop("framework") != "kong" {
		t.Errorf("framework = %q, want kong", plugin.prop("framework"))
	}
	if plugin.prop("auth_plugin") != "true" {
		t.Errorf("expected auth_plugin=true for my-auth plugin, props=%v", plugin.props())
	}
	// phases list must be present and ordered (init_worker before access before log)
	if plugin.prop("phases") != "init_worker,access,log" {
		t.Errorf("phases = %q, want init_worker,access,log", plugin.prop("phases"))
	}

	// Specific phase entities: access and log must be emitted.
	if phases["access"] == nil {
		t.Fatal("expected an access phase entity")
	}
	if phases["log"] == nil {
		t.Fatal("expected a log phase entity")
	}
	// access phase carries priority + correct phase_order (access=6 in our rank).
	if phases["access"].prop("phase_order") != "6" {
		t.Errorf("access phase_order = %q, want 6", phases["access"].prop("phase_order"))
	}
	if phases["log"].prop("phase_order") != "10" {
		t.Errorf("log phase_order = %q, want 10", phases["log"].prop("phase_order"))
	}
	if phases["access"].prop("priority") != "1000" {
		t.Errorf("access phase priority = %q, want 1000", phases["access"].prop("priority"))
	}
	// access phase of an auth plugin is itself an auth signal.
	if phases["access"].prop("signal") != "auth" {
		t.Errorf("access signal = %q, want auth", phases["access"].prop("signal"))
	}
}

func TestLuaKongSchema(t *testing.T) {
	src := `
return {
  name = "my-auth",
  fields = {
    { config = {
        type = "record",
        fields = {
          { secret = { type = "string", required = true } },
          { ttl = { type = "number", default = 3600 } },
          { header_name = { type = "string", default = "authorization" } },
        },
    } },
  },
}
`
	e := &luaKongExtractor{}
	got, err := e.Extract(context.Background(), makeFile("kong/plugins/my-auth/schema.lua", src))
	if err != nil {
		t.Fatal(err)
	}
	var schema *recView
	for i := range got {
		r := newRecView(got[i])
		if r.prop("kind") == "plugin_config_schema" {
			rc := r
			schema = &rc
		}
	}
	if schema == nil {
		t.Fatal("expected a config_schema entity")
	}
	if schema.prop("plugin_name") != "my-auth" {
		t.Errorf("schema plugin_name = %q, want my-auth", schema.prop("plugin_name"))
	}
	fields := schema.prop("fields")
	for _, want := range []string{"secret", "ttl", "header_name"} {
		if !contains(fields, want) {
			t.Errorf("schema fields %q missing %q", fields, want)
		}
	}
	if schema.prop("field_count") != "3" {
		t.Errorf("field_count = %q, want 3", schema.prop("field_count"))
	}
}

func TestLuaKongAdminAPIAndDao(t *testing.T) {
	api := `
return {
  ["/my-entities"] = {
    schema = my_schema,
    methods = { GET = function() end },
  },
  ["/my-entities/:id"] = {
    methods = { DELETE = function() end },
  },
}
`
	e := &luaKongExtractor{}
	got, err := e.Extract(context.Background(), makeFile("kong/plugins/my-auth/api.lua", api))
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	for i := range got {
		r := newRecView(got[i])
		if r.prop("kind") == "admin_api_route" {
			routes = append(routes, r.prop("path"))
		}
	}
	if !sliceHas(routes, "/my-entities") {
		t.Errorf("expected /my-entities admin route, got %v", routes)
	}

	dao := `
return {
  my_entities = {
    name = "my_entities",
    primary_key = { "id" },
    fields = {
      { id = { type = "string" } },
    },
  },
}
`
	got2, err := e.Extract(context.Background(), makeFile("kong/plugins/my-auth/daos.lua", dao))
	if err != nil {
		t.Fatal(err)
	}
	var foundDao bool
	for i := range got2 {
		r := newRecView(got2[i])
		if r.prop("kind") == "custom_dao" {
			foundDao = true
			if r.prop("entity_name") != "my_entities" {
				t.Errorf("dao entity_name = %q, want my_entities", r.prop("entity_name"))
			}
		}
	}
	if !foundDao {
		t.Error("expected a custom_dao entity from daos.lua")
	}
}

// ---------------------------------------------------------------------------
// APISIX
// ---------------------------------------------------------------------------

func TestLuaApisixPlugin(t *testing.T) {
	src := `
local schema = {
  type = "object",
  properties = {
    header = { type = "string" },
    value = { type = "string" },
  },
  required = { "header" },
}

local _M = {
  version = 0.1,
  priority = 2500,
  name = "jwt-auth",
  schema = schema,
}

function _M.rewrite(conf, ctx)
  core.log.info("rewrite")
end

function _M.access(conf, ctx)
  local ok = verify(conf)
  if not ok then
    return 401
  end
end

function _M.log(conf, ctx)
end

return _M
`
	e := &luaKongExtractor{}
	got, err := e.Extract(context.Background(), makeFile("apisix/plugins/jwt-auth.lua", src))
	if err != nil {
		t.Fatal(err)
	}

	var plugin, schema *recView
	phases := map[string]*recView{}
	for i := range got {
		r := newRecView(got[i])
		switch r.prop("kind") {
		case "plugin":
			rc := r
			plugin = &rc
		case "plugin_config_schema":
			rc := r
			schema = &rc
		case "plugin_phase":
			rc := r
			phases[r.prop("phase")] = &rc
		}
	}

	if plugin == nil {
		t.Fatal("expected an apisix plugin entity")
	}
	if plugin.prop("framework") != "apisix" {
		t.Errorf("framework = %q, want apisix", plugin.prop("framework"))
	}
	if plugin.prop("plugin_name") != "jwt-auth" {
		t.Errorf("plugin_name = %q, want jwt-auth", plugin.prop("plugin_name"))
	}
	if plugin.prop("priority") != "2500" {
		t.Errorf("priority = %q, want 2500", plugin.prop("priority"))
	}
	if plugin.prop("version") != "0.1" {
		t.Errorf("version = %q, want 0.1", plugin.prop("version"))
	}
	if plugin.prop("auth_plugin") != "true" {
		t.Errorf("expected auth_plugin=true for jwt-auth, props=%v", plugin.props())
	}
	if plugin.prop("phases") != "rewrite,access,log" {
		t.Errorf("phases = %q, want rewrite,access,log", plugin.prop("phases"))
	}

	// phase entities
	if phases["rewrite"] == nil || phases["access"] == nil || phases["log"] == nil {
		t.Fatalf("expected rewrite/access/log phase entities, got %v", keys(phases))
	}
	if phases["rewrite"].prop("phase_order") != "4" {
		t.Errorf("rewrite phase_order = %q, want 4", phases["rewrite"].prop("phase_order"))
	}
	if phases["access"].prop("signal") != "auth" {
		t.Errorf("access signal = %q, want auth (jwt-auth plugin)", phases["access"].prop("signal"))
	}
	if phases["access"].prop("priority") != "2500" {
		t.Errorf("access priority = %q, want 2500", phases["access"].prop("priority"))
	}

	// schema
	if schema == nil {
		t.Fatal("expected an apisix config_schema entity")
	}
	if schema.prop("plugin_name") != "jwt-auth" {
		t.Errorf("schema plugin_name = %q, want jwt-auth", schema.prop("plugin_name"))
	}
	f := schema.prop("fields")
	if !contains(f, "header") || !contains(f, "value") {
		t.Errorf("schema fields = %q, want header,value", f)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(haystack, needle string) bool {
	for _, p := range splitComma(haystack) {
		if p == needle {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func sliceHas(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func keys(m map[string]*recView) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
