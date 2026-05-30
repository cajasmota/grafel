// effect_sinks_lua_test.go — tests for the Lua effect-sink sniffer.
//
// Fixtures use realistic OpenResty (resty.mysql, resty.redis, resty.http,
// ngx.location.capture) and Lapis (lapis.db) patterns to verify that the
// sniffer detects the correct effect kinds.
package substrate

import "testing"

// luaEffectsByFn collapses sniffer output into fn -> set-of-effects for
// concise assertions.
func luaEffectsByFn(content string) map[string]map[Effect]bool {
	out := map[string]map[Effect]bool{}
	for _, m := range sniffEffectsLua(content) {
		if out[m.Function] == nil {
			out[m.Function] = map[Effect]bool{}
		}
		out[m.Function][m.Effect] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// DB effects — resty.mysql
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_RestyMysqlRead(t *testing.T) {
	src := `
local mysql = require "resty.mysql"

function get_user(id)
    local db, err = mysql:new()
    local res, err = db:query("SELECT * FROM users WHERE id = " .. id)
    return res
end
`
	got := luaEffectsByFn(src)
	if !got["get_user"][EffectDBRead] {
		t.Errorf("get_user: expected db_read from mysql:query(SELECT), got %v", got["get_user"])
	}
}

func TestSniffEffectsLua_RestyMysqlWrite(t *testing.T) {
	src := `
local mysql = require "resty.mysql"

function create_user(name, email)
    local db = mysql:new()
    local res, err = db:query("INSERT INTO users(name,email) VALUES('" .. name .. "','" .. email .. "')")
    return res
end

function delete_user(id)
    local db = mysql:new()
    db:query("DELETE FROM users WHERE id = " .. id)
end
`
	got := luaEffectsByFn(src)
	if !got["create_user"][EffectDBWrite] {
		t.Errorf("create_user: expected db_write from mysql:query(INSERT), got %v", got["create_user"])
	}
	if !got["delete_user"][EffectDBWrite] {
		t.Errorf("delete_user: expected db_write from mysql:query(DELETE), got %v", got["delete_user"])
	}
}

// ---------------------------------------------------------------------------
// DB effects — resty.redis
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_RestyRedisRead(t *testing.T) {
	src := `
local redis = require "resty.redis"

function get_session(token)
    local red = redis:new()
    red:connect("127.0.0.1", 6379)
    local val, err = red:get("session:" .. token)
    return val
end

function list_items(key)
    local red = redis:new()
    red:connect("127.0.0.1", 6379)
    return red:lrange(key, 0, -1)
end
`
	got := luaEffectsByFn(src)
	if !got["get_session"][EffectDBRead] {
		t.Errorf("get_session: expected db_read from red:get, got %v", got["get_session"])
	}
	if !got["list_items"][EffectDBRead] {
		t.Errorf("list_items: expected db_read from red:lrange, got %v", got["list_items"])
	}
}

func TestSniffEffectsLua_RestyRedisWrite(t *testing.T) {
	src := `
local redis = require "resty.redis"

function store_session(token, data, ttl)
    local red = redis:new()
    red:connect("127.0.0.1", 6379)
    red:set("session:" .. token, data)
    red:expire("session:" .. token, ttl)
end

function enqueue_job(queue, payload)
    local red = redis:new()
    red:connect("127.0.0.1", 6379)
    red:lpush(queue, payload)
end
`
	got := luaEffectsByFn(src)
	if !got["store_session"][EffectDBWrite] {
		t.Errorf("store_session: expected db_write from red:set/expire, got %v", got["store_session"])
	}
	if !got["enqueue_job"][EffectDBWrite] {
		t.Errorf("enqueue_job: expected db_write from red:lpush, got %v", got["enqueue_job"])
	}
}

// ---------------------------------------------------------------------------
// DB effects — Lapis db
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_LapisDBRead(t *testing.T) {
	src := `
local db = require "lapis.db"

function fetch_posts(user_id)
    return db.select("* from posts where user_id = ?", user_id)
end
`
	got := luaEffectsByFn(src)
	if !got["fetch_posts"][EffectDBRead] {
		t.Errorf("fetch_posts: expected db_read from db.select, got %v", got["fetch_posts"])
	}
}

func TestSniffEffectsLua_LapisDBWrite(t *testing.T) {
	src := `
local db = require "lapis.db"

function save_post(title, body)
    db.insert("posts", { title = title, body = body })
end

function remove_post(id)
    db.delete("posts", { id = id })
end
`
	got := luaEffectsByFn(src)
	if !got["save_post"][EffectDBWrite] {
		t.Errorf("save_post: expected db_write from db.insert, got %v", got["save_post"])
	}
	if !got["remove_post"][EffectDBWrite] {
		t.Errorf("remove_post: expected db_write from db.delete, got %v", got["remove_post"])
	}
}

// ---------------------------------------------------------------------------
// HTTP effects — resty.http
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_RestyHTTP(t *testing.T) {
	src := `
local http = require "resty.http"

function call_api(url)
    local httpc = http.new()
    local res, err = httpc:request_uri(url, {
        method = "GET",
    })
    return res.body
end

function proxy_request(path)
    local httpc = http.new()
    httpc:connect("backend", 8080)
    local res, err = httpc:request({
        path = path,
        method = "POST",
    })
    return res
end
`
	got := luaEffectsByFn(src)
	if !got["call_api"][EffectHTTPOut] {
		t.Errorf("call_api: expected http_out from httpc:request_uri, got %v", got["call_api"])
	}
	if !got["proxy_request"][EffectHTTPOut] {
		t.Errorf("proxy_request: expected http_out from httpc:request, got %v", got["proxy_request"])
	}
}

func TestSniffEffectsLua_NgxCapture(t *testing.T) {
	src := `
function subrequest_handler()
    local res = ngx.location.capture("/backend/api")
    return res.body
end

function multi_subrequest()
    local r1, r2 = ngx.location.capture_multi({
        { "/api/a" },
        { "/api/b" },
    })
    return r1, r2
end
`
	got := luaEffectsByFn(src)
	if !got["subrequest_handler"][EffectHTTPOut] {
		t.Errorf("subrequest_handler: expected http_out from ngx.location.capture, got %v", got["subrequest_handler"])
	}
	if !got["multi_subrequest"][EffectHTTPOut] {
		t.Errorf("multi_subrequest: expected http_out from ngx.location.capture_multi, got %v", got["multi_subrequest"])
	}
}

// ---------------------------------------------------------------------------
// FS effects
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_FSRead(t *testing.T) {
	src := `
function read_config(path)
    local f = io.open(path, "r")
    if not f then return nil end
    local content = f:read("*a")
    f:close()
    return content
end

function stream_lines(path)
    local lines = {}
    for line in io.lines(path) do
        lines[#lines + 1] = line
    end
    return lines
end
`
	got := luaEffectsByFn(src)
	if !got["read_config"][EffectFSRead] {
		t.Errorf("read_config: expected fs_read from io.open/file:read, got %v", got["read_config"])
	}
	if !got["stream_lines"][EffectFSRead] {
		t.Errorf("stream_lines: expected fs_read from io.lines, got %v", got["stream_lines"])
	}
}

func TestSniffEffectsLua_FSWrite(t *testing.T) {
	src := `
function write_log(path, msg)
    local f = io.open(path, "a")
    f:write(msg .. "\n")
    f:close()
end

function rotate_file(old_path, new_path)
    os.rename(old_path, new_path)
end

function delete_tmp(path)
    os.remove(path)
end
`
	got := luaEffectsByFn(src)
	if !got["write_log"][EffectFSWrite] {
		t.Errorf("write_log: expected fs_write from io.open(a)/file:write, got %v", got["write_log"])
	}
	if !got["rotate_file"][EffectFSWrite] {
		t.Errorf("rotate_file: expected fs_write from os.rename, got %v", got["rotate_file"])
	}
	if !got["delete_tmp"][EffectFSWrite] {
		t.Errorf("delete_tmp: expected fs_write from os.remove, got %v", got["delete_tmp"])
	}
}

// ---------------------------------------------------------------------------
// Mutation effects — table field assignment
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_TableMutation(t *testing.T) {
	src := `
function update_state(state, key, val)
    state.value = val
    state.updated_at = os.time()
end

function set_header(ctx, name, value)
    ctx.headers[name] = value
end
`
	got := luaEffectsByFn(src)
	if !got["update_state"][EffectMutation] {
		t.Errorf("update_state: expected mutation from state.value=, got %v", got["update_state"])
	}
	if !got["set_header"][EffectMutation] {
		t.Errorf("set_header: expected mutation from ctx.headers[name]=, got %v", got["set_header"])
	}
}

// ---------------------------------------------------------------------------
// OpenResty realistic handler fixture (integration-style)
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_OpenRestyHandler(t *testing.T) {
	src := `
local mysql  = require "resty.mysql"
local redis  = require "resty.redis"
local http   = require "resty.http"
local cjson  = require "cjson"

function handle_request()
    -- read session from redis
    local red = redis:new()
    red:connect("127.0.0.1", 6379)
    local session = red:get("sess:" .. ngx.var.cookie_session)

    -- fetch user from mysql
    local db = mysql:new()
    db:connect({ host = "127.0.0.1", port = 3306, database = "app" })
    local rows = db:query("SELECT id, name FROM users WHERE token = '" .. session .. "'")

    -- call downstream service
    local httpc = http.new()
    local res = httpc:request_uri("http://service/api/v1/data", { method = "GET" })

    -- write result back to cache
    red:set("cache:" .. session, res.body)
    red:expire("cache:" .. session, 300)

    -- write audit log
    local f = io.open("/var/log/app/audit.log", "a")
    f:write(ngx.now() .. " " .. session .. "\n")
    f:close()

    -- mutate response table
    local resp = {}
    resp.data = cjson.decode(res.body)
    resp.user = rows[1]

    ngx.say(cjson.encode(resp))
end
`
	got := luaEffectsByFn(src)
	fn := "handle_request"

	if !got[fn][EffectDBRead] {
		t.Errorf("%s: expected db_read (red:get + db:query SELECT), got %v", fn, got[fn])
	}
	if !got[fn][EffectDBWrite] {
		t.Errorf("%s: expected db_write (red:set/expire), got %v", fn, got[fn])
	}
	if !got[fn][EffectHTTPOut] {
		t.Errorf("%s: expected http_out (httpc:request_uri), got %v", fn, got[fn])
	}
	if !got[fn][EffectFSWrite] {
		t.Errorf("%s: expected fs_write (io.open+append), got %v", fn, got[fn])
	}
	if !got[fn][EffectMutation] {
		t.Errorf("%s: expected mutation (resp.data=…), got %v", fn, got[fn])
	}
}

// ---------------------------------------------------------------------------
// Lapis realistic handler fixture (integration-style)
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_LapisHandler(t *testing.T) {
	src := `
local lapis  = require "lapis"
local db     = require "lapis.db"
local http   = require "resty.http"

local app = lapis.Application()

function app:index()
    -- read posts
    local posts = db.select("* from posts order by created_at desc limit 10")

    -- write a view log
    db.insert("view_logs", { path = self.req.parsed_url.path })

    -- subrequest to renderer
    local res = ngx.location.capture("/render/posts")

    -- write rendered output to cache file
    local f = io.open("/tmp/cache/index.html", "w")
    f:write(res.body)
    f:close()

    -- mutate self.params for next middleware
    self.params.rendered = true

    return { render = "posts/index", posts = posts }
end
`
	got := luaEffectsByFn(src)
	fn := "index"

	if !got[fn][EffectDBRead] {
		t.Errorf("%s: expected db_read (db.select), got %v", fn, got[fn])
	}
	if !got[fn][EffectDBWrite] {
		t.Errorf("%s: expected db_write (db.insert), got %v", fn, got[fn])
	}
	if !got[fn][EffectHTTPOut] {
		t.Errorf("%s: expected http_out (ngx.location.capture), got %v", fn, got[fn])
	}
	if !got[fn][EffectFSWrite] {
		t.Errorf("%s: expected fs_write (io.open write + file:write), got %v", fn, got[fn])
	}
	if !got[fn][EffectMutation] {
		t.Errorf("%s: expected mutation (self.params.rendered=true), got %v", fn, got[fn])
	}
}

// ---------------------------------------------------------------------------
// Registration smoke-tests
// ---------------------------------------------------------------------------

func TestSniffEffectsLua_Registered(t *testing.T) {
	if EffectSnifferFor("lua") == nil {
		t.Fatal("lua effect sniffer not registered")
	}
}

func TestSniffEffectsLua_Empty(t *testing.T) {
	if got := sniffEffectsLua(""); got != nil {
		t.Errorf("empty content must yield nil, got %v", got)
	}
}
