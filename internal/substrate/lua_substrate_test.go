// lua_substrate_test.go — smoke tests for Lua substrate sniffers.
package substrate

import (
	"testing"
)

// ---------------------------------------------------------------------------
// def_use_lua
// ---------------------------------------------------------------------------

func TestSniffDefUseLua_Basic(t *testing.T) {
	src := `
function process(req)
    local name = req.name
    local count = 0
    count = count + 1
    return name .. tostring(count)
end
`
	defs, uses := sniffDefUseLua(src)
	if len(defs) == 0 {
		t.Error("expected defs, got none")
	}
	if len(uses) == 0 {
		t.Error("expected uses, got none")
	}
	defNames := map[string]bool{}
	for _, d := range defs {
		defNames[d.Var] = true
	}
	if !defNames["name"] {
		t.Errorf("expected def for 'name', defs=%v", defs)
	}
	if !defNames["count"] {
		t.Errorf("expected def for 'count', defs=%v", defs)
	}
}

func TestSniffDefUseLua_Empty(t *testing.T) {
	defs, uses := sniffDefUseLua("")
	if defs != nil || uses != nil {
		t.Error("empty content should return nil")
	}
}

func TestSniffDefUseLua_FunctionAttr(t *testing.T) {
	src := `
function foo()
    local x = 1
    local y = x + 2
    return y
end

function bar()
    local z = "hello"
    return z
end
`
	defs, _ := sniffDefUseLua(src)
	fooVars := map[string]bool{}
	barVars := map[string]bool{}
	for _, d := range defs {
		if d.Function == "foo" {
			fooVars[d.Var] = true
		}
		if d.Function == "bar" {
			barVars[d.Var] = true
		}
	}
	if !fooVars["x"] || !fooVars["y"] {
		t.Errorf("expected x,y defs in foo, got %v", fooVars)
	}
	if !barVars["z"] {
		t.Errorf("expected z def in bar, got %v", barVars)
	}
}

// ---------------------------------------------------------------------------
// entry_points_lua
// ---------------------------------------------------------------------------

func TestSniffLuaEntryPoints_Main(t *testing.T) {
	src := `#!/usr/bin/env lua
function main()
    print("hello")
end
main()
`
	eps := sniffLuaEntryPoints(src)
	if len(eps) == 0 {
		t.Fatal("expected entry points, got none")
	}
	var kinds []EntryKind
	for _, ep := range eps {
		kinds = append(kinds, ep.Kind)
	}
	hasCLI := false
	for _, k := range kinds {
		if k == EntryKindCLIMain {
			hasCLI = true
		}
	}
	if !hasCLI {
		t.Errorf("expected EntryKindCLIMain, got %v", kinds)
	}
}

func TestSniffLuaEntryPoints_Busted(t *testing.T) {
	src := `
describe("MyApp", function()
    it("does something", function()
        assert.equal(1, 1)
    end)
end)
`
	eps := sniffLuaEntryPoints(src)
	hasTest := false
	for _, ep := range eps {
		if ep.Kind == EntryKindTestEntry {
			hasTest = true
		}
	}
	if !hasTest {
		t.Error("expected EntryKindTestEntry for busted describe block")
	}
}

func TestSniffLuaEntryPoints_Love(t *testing.T) {
	src := `
function love.load()
    -- init game
end

function love.update(dt)
    -- update
end

function love.draw()
    -- draw
end
`
	eps := sniffLuaEntryPoints(src)
	hasLifecycle := false
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle {
			hasLifecycle = true
		}
	}
	if !hasLifecycle {
		t.Error("expected EntryKindFrameworkLifecycle for love.load/update/draw")
	}
}

func TestSniffLuaEntryPoints_Empty(t *testing.T) {
	eps := sniffLuaEntryPoints("")
	if len(eps) != 0 {
		t.Error("empty content should return nil/empty")
	}
}

// ---------------------------------------------------------------------------
// taint_sites_lua
// ---------------------------------------------------------------------------

func TestSniffTaintLua_Sources(t *testing.T) {
	src := `
function handler()
    ngx.req.read_body()
    local args = ngx.req.get_post_args()
    local headers = ngx.req.get_headers()
    local data = cjson.decode(ngx.req.get_body_data())
    return args.name
end
`
	matches := sniffTaintLua(src)
	if len(matches) == 0 {
		t.Fatal("expected taint matches, got none")
	}
	sourcePrimitives := map[string]bool{}
	for _, m := range matches {
		if m.Kind == TaintKindSource {
			sourcePrimitives[m.Primitive] = true
		}
	}
	if !sourcePrimitives["ngx.req.get_args"] {
		t.Errorf("expected ngx.req.get_args source, got %v", sourcePrimitives)
	}
}

func TestSniffTaintLua_Sinks(t *testing.T) {
	src := `
function unsafe_handler()
    local cmd = ngx.req.get_uri_args().cmd
    os.execute(cmd)
    io.open(cmd, "r")
    load(cmd)()
end
`
	matches := sniffTaintLua(src)
	sinkPrims := map[string]bool{}
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			sinkPrims[m.Primitive] = true
		}
	}
	if !sinkPrims["os.execute/io.popen"] {
		t.Errorf("expected os.execute sink, got %v", sinkPrims)
	}
}

func TestSniffTaintLua_Sanitizers(t *testing.T) {
	src := `
function safe_handler()
    local input = ngx.req.get_uri_args().q
    local escaped = ngx.quote_sql_str(input)
    local uri_safe = ngx.escape_uri(input)
    return escaped
end
`
	matches := sniffTaintLua(src)
	sanPrims := map[string]bool{}
	for _, m := range matches {
		if m.Kind == TaintKindSanitizer {
			sanPrims[m.Primitive] = true
		}
	}
	if !sanPrims["ngx.quote_sql_str"] {
		t.Errorf("expected ngx.quote_sql_str sanitizer, got %v", sanPrims)
	}
}

func TestSniffTaintLua_Empty(t *testing.T) {
	matches := sniffTaintLua("")
	if len(matches) != 0 {
		t.Error("empty content should return nil")
	}
}

// ---------------------------------------------------------------------------
// template_pattern_lua
// ---------------------------------------------------------------------------

func TestSniffTemplatePatternsLua_Log(t *testing.T) {
	src := `
function handler()
    ngx.log(ngx.INFO, "request received at endpoint")
    ngx.log(ngx.ERR, "failed to parse body")
    print("debug output here")
end
`
	patterns := sniffTemplatePatternsLua(src)
	if len(patterns) == 0 {
		t.Fatal("expected template patterns, got none")
	}
	hasLog := false
	for _, p := range patterns {
		if p.Kind == TemplateKindLog {
			hasLog = true
		}
	}
	if !hasLog {
		t.Error("expected TemplateKindLog patterns")
	}
}

func TestSniffTemplatePatternsLua_SQL(t *testing.T) {
	src := `
function get_users()
    local query = "SELECT * FROM users WHERE active = 1"
    return db:query(query)
end
`
	patterns := sniffTemplatePatternsLua(src)
	hasSQL := false
	for _, p := range patterns {
		if p.Kind == TemplateKindSQL {
			hasSQL = true
		}
	}
	if !hasSQL {
		t.Error("expected TemplateKindSQL pattern")
	}
}

func TestSniffTemplatePatternsLua_Empty(t *testing.T) {
	patterns := sniffTemplatePatternsLua("")
	if len(patterns) != 0 {
		t.Error("empty content should return nil")
	}
}
