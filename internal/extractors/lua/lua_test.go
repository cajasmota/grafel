package lua_test

import (
	"context"
	"testing"

	tslua "github.com/smacker/go-tree-sitter/lua"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/lua"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
)

func parseForTest(t *testing.T, src string) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tslua.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return tree
}

func TestLuaExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("lua")
	if !ok {
		t.Fatal("lua extractor not registered")
	}
}

func TestLuaExtractor_GlobalFunctions(t *testing.T) {
	src := `local M = {}

function M.find_all()
    return {}
end

function M.create(name, email)
    return {name=name, email=email}
end

function M.delete(id)
    return true
end

return M
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "module.lua",
		Content:  []byte(src),
		Language: "lua",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	luaEntities := 0
	names := make(map[string]bool)
	for _, e := range entities {
		if e.Language != "lua" {
			continue
		}
		// Issue #375 — the lua extractor now also emits SCOPE.Component
		// entities for module tables (`local M = {}`) and IMPORTS so the
		// resolver can wire CONTAINS/IMPORTS edges. Only count and shape-
		// check Operation entities here; the relationship-aware tests in
		// relationships_test.go cover the rest.
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		luaEntities++
		names[e.Name] = true
	}
	if luaEntities < 3 {
		t.Fatalf("expected at least 3 lua entities, got %d", luaEntities)
	}
	for _, want := range []string{"find_all", "create", "delete"} {
		if !names[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

func TestLuaExtractor_LocalFunction(t *testing.T) {
	src := `local function validate(x)
    return x ~= nil
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.lua",
		Content:  []byte(src),
		Language: "lua",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("expected at least 1 entity")
	}
	found := false
	for _, e := range entities {
		if e.Name == "validate" {
			found = true
			if e.Subtype != "function" {
				t.Errorf("expected Subtype=function, got %q", e.Subtype)
			}
		}
	}
	if !found {
		t.Error("expected function 'validate' to be extracted")
	}
}

func TestLuaExtractor_OOPClassInheritance(t *testing.T) {
	// #4911 — the dominant Lua OOP idiom: a base class table with a self
	// __index, and a child declared via setmetatable({}, {__index = Base}).
	src := `local Animal = {}
Animal.__index = Animal

function Animal.new(name)
    return setmetatable({name = name}, Animal)
end

function Animal:speak()
    return self.name
end

local Dog = setmetatable({}, { __index = Animal })
Dog.__index = Dog

function Dog:speak()
    return "woof"
end

return Dog
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "animal.lua",
		Content:  []byte(src),
		Language: "lua",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	classSubtype := map[string]string{}
	for _, e := range entities {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		classSubtype[e.Name] = e.Subtype
	}

	// Animal is `local Animal = {}` with a self __index → promoted to class.
	if got := classSubtype["Animal"]; got != "class" {
		t.Errorf("expected Animal subtype=class (self __index), got %q", got)
	}
	// Dog is declared inline via setmetatable (not `local Dog = {}`), so the
	// base walk emits no module-table Component for it — there is nothing to
	// promote, which is the precision-first behaviour (we never invent a class
	// entity). Inheritance against a table-declared child is covered by
	// TestLuaExtractor_OOPInheritanceTableForm.
}

func TestLuaExtractor_OOPInheritanceTableForm(t *testing.T) {
	// Child declared as an empty table first, then setmetatable applied — the
	// base walk DOES emit a module-table Component for Child, so the EXTENDS
	// edge attaches.
	src := `local Base = {}
Base.__index = Base

local Derived = {}
setmetatable(Derived, { __index = Base })
Derived.__index = Derived

function Derived:run()
    return "ok"
end

return Derived
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "derived.lua",
		Content:  []byte(src),
		Language: "lua",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var foundExtends bool
	for _, e := range entities {
		if e.Kind != "SCOPE.Component" || e.Name != "Derived" {
			continue
		}
		if e.Subtype != "class" {
			t.Errorf("expected Derived subtype=class, got %q", e.Subtype)
		}
		for _, r := range e.Relationships {
			if r.Kind == "EXTENDS" && r.Properties["base_name"] == "Base" {
				foundExtends = true
			}
		}
	}
	if !foundExtends {
		t.Error("expected EXTENDS edge Derived -> Base")
	}
}

func TestLuaExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.lua",
		Content:  []byte{},
		Language: "lua",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestLuaExtractor_LineNumbers(t *testing.T) {
	// Tree-sitter Lua grammar includes the leading newline in function_statement nodes,
	// so StartLine is the line containing the newline preceding the function keyword.
	src := `function greet(name)
    print("Hello " .. name)
end
`
	tree := parseForTest(t, src)
	ext, _ := extractor.Get("lua")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.lua",
		Content:  []byte(src),
		Language: "lua",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Name == "greet" {
			if e.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", e.StartLine)
			}
			if e.EndLine < e.StartLine {
				t.Errorf("expected EndLine >= StartLine, got start=%d end=%d", e.StartLine, e.EndLine)
			}
			return
		}
	}
	t.Error("entity 'greet' not found")
}
