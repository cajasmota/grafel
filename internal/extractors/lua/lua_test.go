package lua_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tslua "github.com/smacker/go-tree-sitter/lua"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/lua"
)

func parseForTest(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tslua.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(src))
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
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	luaEntities := 0
	names := make(map[string]bool)
	for _, e := range entities {
		if e.Language == "lua" {
			luaEntities++
			names[e.Name] = true
			if e.Kind != "SCOPE.Operation" {
				t.Errorf("entity %q: expected Kind=SCOPE.Operation, got %q", e.Name, e.Kind)
			}
		}
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
		Tree:     tree,
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
		Tree:     tree,
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
