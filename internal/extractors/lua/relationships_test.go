package lua_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/lua"
	"github.com/cajasmota/archigraph/internal/types"
)

// runLua parses src with the real lua grammar and returns extracted entities.
func runLua(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.lua",
		Content:  []byte(src),
		Language: "lua",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func luaFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func luaHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	e := luaFind(ents, name, kind)
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == edgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestLua_ImportsRequire (#375): every `require(...)` and `require "..."` form
// emits an IMPORTS relationship from the file to the required module path.
// Properties carry local_name/source_module/imported_name/import_kind matching
// the contract used by elixir/dart/ruby.
func TestLua_ImportsRequire(t *testing.T) {
	src := `local foo = require("foo.bar")
local baz = require "baz"
require("side.effect")
`
	ents := runLua(t, src)

	type want struct {
		local, mod, kind string
	}
	expect := map[string]want{
		"foo.bar":     {"foo", "foo.bar", "require"}, // dotted path; LHS local var wins for local_name
		"baz":         {"baz", "baz", "require"},
		"side.effect": {"effect", "side.effect", "require"}, // no LHS — fall back to leaf segment
	}
	seen := map[string]bool{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			w, ok := expect[r.ToID]
			if !ok {
				continue
			}
			seen[r.ToID] = true
			if r.FromID != "test.lua" {
				t.Errorf("IMPORTS %s: FromID=%q want test.lua", r.ToID, r.FromID)
			}
			if r.Properties["local_name"] != w.local {
				t.Errorf("IMPORTS %s: local_name=%q want %q", r.ToID, r.Properties["local_name"], w.local)
			}
			if r.Properties["source_module"] != w.mod {
				t.Errorf("IMPORTS %s: source_module=%q want %q", r.ToID, r.Properties["source_module"], w.mod)
			}
			if r.Properties["import_kind"] != w.kind {
				t.Errorf("IMPORTS %s: import_kind=%q want %q", r.ToID, r.Properties["import_kind"], w.kind)
			}
		}
	}
	for k := range expect {
		if !seen[k] {
			t.Errorf("expected IMPORTS edge for %s", k)
		}
	}
}

// TestLua_CallsBareName: bare function calls inside a function body produce
// CALLS edges with the simple identifier as ToID, deduped.
func TestLua_CallsBareName(t *testing.T) {
	src := `function caller()
    helper()
    helper()
    other_thing()
end

function helper()
end

function other_thing()
end
`
	ents := runLua(t, src)
	if !luaHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "helper") {
		t.Errorf("expected CALLS caller→helper")
	}
	if !luaHasRel(ents, "caller", "SCOPE.Operation", "CALLS", "other_thing") {
		t.Errorf("expected CALLS caller→other_thing")
	}
	caller := luaFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	n := 0
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "helper" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected dedup CALLS caller→helper to 1, got %d", n)
	}
}

// TestLua_CallsDotted: dotted calls like `foo.run(x)` must emit CALLS to the
// trailing identifier ("run"), not the receiver ("foo"). Same for the method
// colon `obj:bar()` → "bar".
func TestLua_CallsDotted(t *testing.T) {
	src := `function caller()
    foo.run(1)
    self:bye()
    obj:method(2)
end
`
	ents := runLua(t, src)
	caller := luaFind(ents, "caller", "SCOPE.Operation")
	if caller == nil {
		t.Fatal("expected caller op")
	}
	want := map[string]bool{"run": false, "bye": false, "method": false}
	forbidden := map[string]bool{"foo": true, "self": true, "obj": true}
	for _, r := range caller.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if forbidden[r.ToID] {
			t.Errorf("receiver %q must not be emitted as CALLS target", r.ToID)
		}
		if _, ok := want[r.ToID]; ok {
			want[r.ToID] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("expected CALLS caller→%s", k)
		}
	}
}

// TestLua_CallsKeywordsFiltered: lua built-ins (require) used as call heads
// for imports are not real call targets in our model. require() inside a body
// must not produce a CALLS edge.
func TestLua_CallsKeywordsFiltered(t *testing.T) {
	src := `function loader()
    local x = require("mod")
    print(x)
end
`
	ents := runLua(t, src)
	loader := luaFind(ents, "loader", "SCOPE.Operation")
	if loader == nil {
		t.Fatal("expected loader op")
	}
	for _, r := range loader.Relationships {
		if r.Kind == "CALLS" && r.ToID == "require" {
			t.Errorf("require must not be emitted as CALLS target")
		}
	}
}

// TestLua_CallsDropSelfRecursion: a function calling itself does not emit a
// CALLS edge to its own name.
func TestLua_CallsDropSelfRecursion(t *testing.T) {
	src := `function loop(x)
    loop(x - 1)
end
`
	ents := runLua(t, src)
	loop := luaFind(ents, "loop", "SCOPE.Operation")
	if loop == nil {
		t.Fatal("expected loop op")
	}
	for _, r := range loop.Relationships {
		if r.Kind == "CALLS" && r.ToID == "loop" {
			t.Errorf("self-recursion must not produce CALLS edge")
		}
	}
}

// TestLua_ContainsModuleTable (#375): the canonical lua module-table pattern
//
//	local M = {}
//	function M.foo() end
//	function M:bar() end
//
// emits a SCOPE.Component for `M` with one CONTAINS edge per dotted/colon
// function declared on it. ToID is the canonical structural-ref shape
// `scope:operation:method:lua:<file>:<name>`.
func TestLua_ContainsModuleTable(t *testing.T) {
	src := `local M = {}

function M.foo()
end

function M:bar()
end

function M.baz(x)
    return x
end

return M
`
	ents := runLua(t, src)
	m := luaFind(ents, "M", "SCOPE.Component")
	if m == nil {
		t.Fatal("expected SCOPE.Component for module table M")
	}
	contains := 0
	for _, r := range m.Relationships {
		if r.Kind == "CONTAINS" {
			contains++
		}
	}
	if contains != 3 {
		t.Errorf("expected 3 CONTAINS edges from M, got %d (rels=%+v)", contains, m.Relationships)
	}
	for _, fn := range []string{"foo", "bar", "baz"} {
		want := "scope:operation:method:lua:test.lua:" + fn
		if !luaHasRel(ents, "M", "SCOPE.Component", "CONTAINS", want) {
			t.Errorf("expected CONTAINS M→%s", want)
		}
	}
}

// TestLua_LanguageTagging: every emitted relationship carries
// Properties["language"]="lua" so the resolver picks the right
// dynamic-pattern catalog (issue #90).
func TestLua_LanguageTagging(t *testing.T) {
	src := `local M = {}
local foo = require("foo")

function M.caller()
    helper()
end

function M.helper()
end
`
	ents := runLua(t, src)
	any := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			any++
			if r.Properties["language"] != "lua" {
				t.Errorf("entity %q rel %s→%s missing language tag (props=%+v)",
					e.Name, r.Kind, r.ToID, r.Properties)
			}
		}
	}
	if any == 0 {
		t.Fatal("expected at least one relationship to verify tagging")
	}
}
