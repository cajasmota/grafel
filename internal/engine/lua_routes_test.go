package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Lapis — app:<verb>("/path", fn) / app:match / respond_to  (#3484)
// ---------------------------------------------------------------------------

// TestLapis_VerbRoutes asserts a canonical endpoint per Lapis verb route,
// including `:id`→{id} normalisation and app-receiver handler attribution.
func TestLapis_VerbRoutes(t *testing.T) {
	src := `
local lapis = require("lapis")
local app = lapis.Application()

app:get("/users", function(self)
  return { json = { users = {} } }
end)

app:post("/users", function(self)
  return { json = { created = true } }
end)

app:get("/users/:id", function(self)
  return { json = { id = self.params.id } }
end)

app:delete("/users/:id", function(self)
  return { status = 204 }
end)
`
	ids, res := runDetect(t, "lua", "app.lua", src)
	want := []string{
		"http:GET:/users",
		"http:POST:/users",
		"http:GET:/users/{id}",
		"http:DELETE:/users/{id}",
	}
	requireContains(t, ids, want, "lapis-verb-routes")

	var found bool
	for _, e := range res.Entities {
		if e.ID != "http:GET:/users/{id}" {
			continue
		}
		found = true
		if e.Properties["framework"] != "lapis" {
			t.Errorf("lapis: framework=%q, want lapis", e.Properties["framework"])
		}
		if e.Properties["source_handler"] != "SCOPE.Component:app" {
			t.Errorf("lapis: source_handler=%q, want SCOPE.Component:app", e.Properties["source_handler"])
		}
		if e.Properties["verb"] != "GET" {
			t.Errorf("lapis: verb=%q, want GET", e.Properties["verb"])
		}
	}
	if !found {
		t.Errorf("lapis: missing http:GET:/users/{id}")
	}
}

// TestLapis_NamedMatch asserts the named `app:match("name", "/path", fn)` form
// emits an ANY endpoint attributed to the route name, with :id normalisation.
func TestLapis_NamedMatch(t *testing.T) {
	src := `
local app = lapis.Application()
app:match("user_show", "/users/:id", function(self)
  return { render = "users/show" }
end)
`
	ids, res := runDetect(t, "lua", "app.lua", src)
	requireContains(t, ids, []string{"http:ANY:/users/{id}"}, "lapis-named-match")

	for _, e := range res.Entities {
		if e.ID == "http:ANY:/users/{id}" {
			if e.Properties["source_handler"] != "SCOPE.Operation:user_show" {
				t.Errorf("lapis-named: source_handler=%q, want SCOPE.Operation:user_show", e.Properties["source_handler"])
			}
		}
	}
}

// TestLapis_AnonMatch asserts the unnamed `app:match("/path", fn)` form.
func TestLapis_AnonMatch(t *testing.T) {
	src := `
local app = lapis.Application()
app:match("/about", function(self)
  return { render = "about" }
end)
`
	ids, _ := runDetect(t, "lua", "app.lua", src)
	requireContains(t, ids, []string{"http:ANY:/about"}, "lapis-anon-match")
}

// TestLapis_RespondTo asserts each verb of a respond_to({...}) table is mapped
// to its own endpoint on the enclosing route path.
func TestLapis_RespondTo(t *testing.T) {
	src := `
local respond_to = require("lapis.application").respond_to
local app = lapis.Application()

app:match("/resource", respond_to({
  GET = function(self)
    return { json = {} }
  end,
  POST = function(self)
    return { json = { ok = true } }
  end,
  DELETE = function(self)
    return { status = 204 }
  end,
}))
`
	ids, _ := runDetect(t, "lua", "app.lua", src)
	requireContains(t, ids, []string{
		"http:GET:/resource",
		"http:POST:/resource",
		"http:DELETE:/resource",
	}, "lapis-respond-to")
}

// TestLapis_NoSignalNoEmit asserts a plain Lua file without a lapis signal
// emits no Lapis endpoints (the `x:get(...)` shape alone is not enough).
func TestLapis_NoSignalNoEmit(t *testing.T) {
	src := `
local cache = some_lib.new()
cache:get("/not/a/route")
`
	ids, _ := runDetect(t, "lua", "cache.lua", src)
	for _, id := range ids {
		if id == "http:GET:/not/a/route" {
			t.Errorf("lapis: emitted endpoint for non-lapis file: %q", id)
		}
	}
}

// ---------------------------------------------------------------------------
// OpenResty — nginx location + lua-resty-router  (#3484)
// ---------------------------------------------------------------------------

// TestOpenResty_Location asserts nginx `location` stanzas with content_by_lua
// emit ANY endpoints attributed to the lua content handler.
func TestOpenResty_Location(t *testing.T) {
	src := `
http {
  server {
    location /api/users {
      content_by_lua_block {
        ngx.say("users")
      }
    }
    location = /health {
      content_by_lua_block {
        ngx.say("ok")
      }
    }
  }
}
`
	ids, res := runDetect(t, "lua", "routes.lua", src)
	requireContains(t, ids, []string{
		"http:ANY:/api/users",
		"http:ANY:/health",
	}, "openresty-location")

	for _, e := range res.Entities {
		if e.ID == "http:ANY:/api/users" {
			if e.Properties["framework"] != "openresty" {
				t.Errorf("openresty: framework=%q, want openresty", e.Properties["framework"])
			}
		}
	}
}

// TestOpenResty_LocationNoLuaNoEmit asserts a static-file location block (no
// content_by_lua) does NOT emit an app endpoint.
func TestOpenResty_LocationNoLuaNoEmit(t *testing.T) {
	src := `
http {
  server {
    location /static {
      root /var/www;
    }
  }
}
`
	ids, _ := runDetect(t, "lua", "static.lua", src)
	for _, id := range ids {
		if id == "http:ANY:/static" {
			t.Errorf("openresty: emitted endpoint for static-only location: %q", id)
		}
	}
}

// TestOpenResty_RestyRouter asserts lua-resty-router DSL verb routes emit
// canonical endpoints with :id normalisation.
func TestOpenResty_RestyRouter(t *testing.T) {
	src := `
local router = require("resty.router")
local r = router:new()

r:get("/users/:id", function(params)
  ngx.say(params.id)
end)

r:post("/users", function()
  ngx.status = 201
end)
`
	ids, res := runDetect(t, "lua", "router.lua", src)
	requireContains(t, ids, []string{
		"http:GET:/users/{id}",
		"http:POST:/users",
	}, "openresty-resty-router")

	for _, e := range res.Entities {
		if e.ID == "http:GET:/users/{id}" {
			if e.Properties["framework"] != "lua-resty-router" {
				t.Errorf("resty-router: framework=%q, want lua-resty-router", e.Properties["framework"])
			}
		}
	}
}
