// Package lua implements the tree-sitter–based extractor for Lua source files.
//
// Extracted entities:
//   - function_statement (global, dot, colon) → Kind="SCOPE.Operation", Subtype="function"/"method"
//   - local function_statement                → Kind="SCOPE.Operation", Subtype="function"
//   - local M = {} module tables               → Kind="SCOPE.Component", Subtype="module_table"
//
// Issue #375 (PORT-RELS-LUA) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `require("foo.bar")` and `require "foo"` carries
//     Properties{local_name, source_module, imported_name, import_kind}
//     matching the Java contract (#120) and the Python schema (#93). The
//     full required path becomes ToID and source_module; if the require is
//     bound to a local (`local foo = require(...)`), local_name is the LHS
//     identifier; otherwise it falls back to the trailing path segment.
//
//   - CALLS: every `function_call` descendant of a function body emits one
//     CALLS edge per unique callee. Bare `helper()` → ToID="helper". Dotted
//     `foo.run(x)` and method `obj:bar()` → trailing identifier ("run",
//     "bar"). Self-recursion is dropped, `require` is filtered (it produces
//     IMPORTS, not CALLS).
//
//   - CONTAINS: the canonical lua module-table pattern
//
//     local M = {}
//     function M.foo() end
//     function M:bar() end
//
//     attaches one CONTAINS edge per `M.<name>` / `M:<name>` function
//     declared in the file, with structural-ref shape
//     `scope:operation:method:lua:<file>:<name>` (BuildOperationStructuralRef,
//     Format A, #144).
//
// Uses the lua grammar from smacker/go-tree-sitter. Registers itself via
// init() and is auto-imported by the generated registry_gen.go.
package lua

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("lua", &Extractor{})
}

// Extractor implements extractor.Extractor for Lua.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "lua" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	root := file.Tree.RootNode()
	imports := collectRequires(root, file.Content)

	var entities []types.EntityRecord

	// Pass 1: emit IMPORTS entities for every require call.
	emitImportRecords(root, file, &entities)

	// Pass 2: collect module-table names (`local M = {}`) so we can wire
	// CONTAINS edges from M to its `function M.x` / `function M:x` methods.
	moduleTables := collectModuleTables(root, file)
	moduleTableIdx := make(map[string]int, len(moduleTables))
	for name, rec := range moduleTables {
		moduleTableIdx[name] = len(entities)
		entities = append(entities, rec)
	}

	// Pass 3: walk the tree, emitting Operation entities for each
	// function_statement/function_declaration/local_function and attaching
	// CALLS edges from each function body. CONTAINS edges are appended to
	// the matching module-table component.
	walkLua(root, file, imports, moduleTableIdx, &entities)

	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "lua")
	return entities, nil
}

// walkLua performs a depth-first traversal collecting function nodes.
// The smacker/go-tree-sitter Lua grammar uses "function_statement" for both
// global and local functions (local functions have a "local" keyword child).
// "function_declaration" and "local_function" are kept for compatibility with
// older grammar versions.
func walkLua(node *sitter.Node, file extractor.FileInput, imports []string, moduleIdx map[string]int, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "function_statement":
		if rec, receiver, ok := buildFunctionStatement(node, file, imports); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
			idx := len(*out)
			*out = append(*out, rec)
			if receiver != "" {
				if mIdx, has := moduleIdx[receiver]; has {
					toID := extractor.BuildOperationStructuralRef("lua", file.Path, rec.Name)
					(*out)[mIdx].Relationships = append((*out)[mIdx].Relationships,
						types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
				}
			}
			_ = idx
		}
	// Legacy node type names (older grammars).
	case "function_declaration":
		if rec, receiver, ok := buildFunctionDecl(node, file, imports); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
			*out = append(*out, rec)
			if receiver != "" {
				if mIdx, has := moduleIdx[receiver]; has {
					toID := extractor.BuildOperationStructuralRef("lua", file.Path, rec.Name)
					(*out)[mIdx].Relationships = append((*out)[mIdx].Relationships,
						types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
				}
			}
		}
	case "local_function":
		if rec, ok := buildLocalFunction(node, file, imports); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name)...)
			*out = append(*out, rec)
		}
	}
	for i := range node.ChildCount() {
		walkLua(node.Child(int(i)), file, imports, moduleIdx, out)
	}
}

// findFunctionBody returns the `function_body` child of a function node, or
// nil. Used for both function_statement and the older function_declaration
// shapes.
func findFunctionBody(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "function_body" || ch.Type() == "block" {
			return ch
		}
	}
	return nil
}

// buildFunctionStatement handles function_statement nodes from the smacker/go-tree-sitter
// Lua grammar. Both global and local functions emit this node type.
// Global:  function M.name(params) ... end
// Local:   local function name(params) ... end
//
// receiver is the leading identifier of a dotted/colon function name (e.g.
// "M" for `function M.foo`). It's "" for plain `function foo` and local
// functions. The walker uses receiver to attach CONTAINS edges from
// matching `local M = {}` module-table components (#375).
func buildFunctionStatement(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, string, bool) {
	// Determine if this is a local function (has "local" keyword child).
	isLocal := false
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "local" {
			isLocal = true
			break
		}
	}

	// Extract name from function_name or identifier child.
	var rawName string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_name":
			rawName = string(file.Content[ch.StartByte():ch.EndByte()])
		case "identifier":
			if isLocal && rawName == "" {
				rawName = string(file.Content[ch.StartByte():ch.EndByte()])
			}
		}
	}
	if rawName == "" {
		return types.EntityRecord{}, "", false
	}

	subtype := "function"
	if strings.Contains(rawName, ":") {
		subtype = "method"
	}
	// Use last segment as entity name; receiver is everything before it.
	name := rawName
	receiver := ""
	if idx := strings.LastIndexAny(rawName, ":."); idx >= 0 {
		receiver = rawName[:idx]
		name = rawName[idx+1:]
	}
	if name == "" {
		return types.EntityRecord{}, "", false
	}

	// Extract parameters from parameter_list child.
	params := "()"
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "parameter_list" {
			params = "(" + string(file.Content[ch.StartByte():ch.EndByte()]) + ")"
			break
		}
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "lua",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "function " + name + params,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, receiver, true
}

// buildFunctionDecl handles function_declaration nodes (global or dot/colon notation)
// from older tree-sitter-lua grammar versions.
func buildFunctionDecl(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, string, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}, "", false
	}
	fullName := string(file.Content[nameNode.StartByte():nameNode.EndByte()])
	subtype := "function"
	if strings.Contains(fullName, ":") {
		subtype = "method"
	}
	// Use last segment as entity name.
	name := fullName
	receiver := ""
	if idx := strings.LastIndexAny(fullName, ":."); idx >= 0 {
		receiver = fullName[:idx]
		name = fullName[idx+1:]
	}
	if name == "" {
		return types.EntityRecord{}, "", false
	}

	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = string(file.Content[paramsNode.StartByte():paramsNode.EndByte()])
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "lua",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "function " + name + params,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, receiver, true
}

// buildLocalFunction handles local_function nodes (older grammars).
func buildLocalFunction(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	// local_function: local function <identifier> <parameters> <block> end
	var nameNode *sitter.Node
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "identifier" {
			nameNode = ch
			break
		}
	}
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := string(file.Content[nameNode.StartByte():nameNode.EndByte()])
	if name == "" {
		return types.EntityRecord{}, false
	}

	paramsNode := node.ChildByFieldName("parameters")
	params := "()"
	if paramsNode != nil {
		params = string(file.Content[paramsNode.StartByte():paramsNode.EndByte()])
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "lua",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "function " + name + params,
		EnrichmentRequired: false,
		Properties: map[string]string{
			"imports": strings.Join(imports, ","),
		},
	}, true
}

// collectRequires gathers require("module") calls as the legacy
// Properties["imports"] string list (preserved for backwards compatibility
// with the existing Operation-entity contract).
func collectRequires(root *sitter.Node, src []byte) []string {
	var imports []string
	walkForRequires(root, src, &imports)
	return imports
}

func walkForRequires(node *sitter.Node, src []byte, out *[]string) {
	if node == nil {
		return
	}
	if node.Type() == "function_call" && node.ChildCount() > 0 {
		first := node.Child(0)
		if first != nil && strings.TrimSpace(string(src[first.StartByte():first.EndByte()])) == "require" {
			if path := requireArgPath(node, src); path != "" {
				*out = append(*out, path)
			}
		}
	}
	for i := range node.ChildCount() {
		walkForRequires(node.Child(int(i)), src, out)
	}
}

// requireArgPath extracts the string path from a `require(...)` /
// `require "..."` function_call node. Returns "" if the head isn't require
// or no string argument is present.
func requireArgPath(call *sitter.Node, src []byte) string {
	if call == nil || call.ChildCount() == 0 {
		return ""
	}
	for i := range call.ChildCount() {
		ch := call.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_arguments":
			for j := range ch.ChildCount() {
				arg := ch.Child(int(j))
				if arg != nil && arg.Type() == "string" {
					if raw := extractStringContent(arg, src); raw != "" {
						return raw
					}
				}
			}
		case "string_argument":
			if raw := extractStringContent(ch, src); raw != "" {
				return raw
			}
		case "string":
			if raw := extractStringContent(ch, src); raw != "" {
				return raw
			}
		}
	}
	return ""
}

// extractStringContent extracts the string value from a string node,
// handling both bare text and nested string_content child.
func extractStringContent(node *sitter.Node, src []byte) string {
	// Try string_content child first (smacker grammar).
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "string_content" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	// Fall back to raw text with quotes stripped.
	raw := string(src[node.StartByte():node.EndByte()])
	return strings.Trim(raw, `'"`)
}

// emitImportRecords walks the tree and emits one SCOPE.Component entity per
// require call, carrying an IMPORTS relationship. Mirrors the elixir/dart
// pattern (#370/#369): each import is its own entity record so the resolver
// can dispatch on Properties["language"].
func emitImportRecords(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	walkForImportEdges(root, file, out)
}

func walkForImportEdges(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	if node.Type() == "variable_declaration" {
		// `local foo = require("foo.bar")` — capture LHS identifier as
		// local_name, then descend looking for the require call.
		if path, lhs, ok := analyzeRequireDecl(node, file.Content); ok {
			*out = append(*out, makeImportRecord(file, path, lhs))
			return
		}
	}
	if node.Type() == "function_call" && node.ChildCount() > 0 {
		first := node.Child(0)
		if first != nil && strings.TrimSpace(string(file.Content[first.StartByte():first.EndByte()])) == "require" {
			if path := requireArgPath(node, file.Content); path != "" {
				*out = append(*out, makeImportRecord(file, path, ""))
			}
			return
		}
	}
	for i := range node.ChildCount() {
		walkForImportEdges(node.Child(int(i)), file, out)
	}
}

// analyzeRequireDecl checks whether a variable_declaration node has the shape
// `local <ident> = require("path")`. Returns the required path and the LHS
// identifier on success.
func analyzeRequireDecl(decl *sitter.Node, src []byte) (string, string, bool) {
	var lhs string
	var requirePath string
	for i := 0; i < int(decl.ChildCount()); i++ {
		ch := decl.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "variable_declarator":
			// The first identifier child is the bound name.
			for j := 0; j < int(ch.ChildCount()); j++ {
				gc := ch.Child(j)
				if gc != nil && gc.Type() == "identifier" {
					lhs = string(src[gc.StartByte():gc.EndByte()])
					break
				}
			}
		case "function_call":
			// require(...) or require "..."
			if ch.ChildCount() > 0 {
				head := ch.Child(0)
				if head != nil && strings.TrimSpace(string(src[head.StartByte():head.EndByte()])) == "require" {
					requirePath = requireArgPath(ch, src)
				}
			}
		}
	}
	if requirePath == "" {
		return "", "", false
	}
	return requirePath, lhs, true
}

// makeImportRecord builds the SCOPE.Component entity carrying a single
// IMPORTS edge for a require call.
//
//	Properties["local_name"]    — LHS identifier if `local x = require(...)`,
//	                              otherwise the trailing path segment.
//	Properties["source_module"] — the full required path (matches ToID).
//	Properties["imported_name"] — equal to local_name.
//	Properties["import_kind"]   — always "require" for lua.
func makeImportRecord(file extractor.FileInput, requirePath, lhs string) types.EntityRecord {
	leaf := requirePath
	if dot := strings.LastIndexByte(requirePath, '.'); dot >= 0 {
		leaf = requirePath[dot+1:]
	}
	local := lhs
	if local == "" {
		local = leaf
	}
	return types.EntityRecord{
		Name:       requirePath,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "lua",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   requirePath,
				Kind:   "IMPORTS",
				Properties: map[string]string{
					"local_name":    local,
					"source_module": requirePath,
					"imported_name": local,
					"import_kind":   "require",
				},
			},
		},
	}
}

// collectModuleTables finds every top-level `local <Name> = {}` declaration
// and returns it as a SCOPE.Component entity. CONTAINS edges are filled in
// later by walkLua as it encounters `function <Name>.x` / `function <Name>:x`
// declarations (#375).
func collectModuleTables(root *sitter.Node, file extractor.FileInput) map[string]types.EntityRecord {
	src := file.Content
	out := make(map[string]types.EntityRecord)
	for i := 0; i < int(root.ChildCount()); i++ {
		ch := root.Child(i)
		if ch == nil || ch.Type() != "variable_declaration" {
			continue
		}
		// Must be `local`.
		hasLocal := false
		var lhs string
		hasEmptyTable := false
		for j := 0; j < int(ch.ChildCount()); j++ {
			c2 := ch.Child(j)
			if c2 == nil {
				continue
			}
			switch c2.Type() {
			case "local":
				hasLocal = true
			case "variable_declarator":
				for k := 0; k < int(c2.ChildCount()); k++ {
					gc := c2.Child(k)
					if gc != nil && gc.Type() == "identifier" {
						lhs = string(src[gc.StartByte():gc.EndByte()])
						break
					}
				}
			case "tableconstructor":
				// Empty table: only `{` and `}` children (no field children).
				empty := true
				for k := 0; k < int(c2.ChildCount()); k++ {
					gc := c2.Child(k)
					if gc == nil {
						continue
					}
					t := gc.Type()
					if t != "{" && t != "}" {
						empty = false
						break
					}
				}
				hasEmptyTable = empty
			}
		}
		if hasLocal && lhs != "" && hasEmptyTable {
			out[lhs] = types.EntityRecord{
				Name:       lhs,
				Kind:       "SCOPE.Component",
				Subtype:    "module_table",
				SourceFile: file.Path,
				Language:   "lua",
				StartLine:  int(ch.StartPoint().Row) + 1,
				EndLine:    int(ch.EndPoint().Row) + 1,
			}
		}
	}
	return out
}

// luaCallStop lists call heads we never emit as CALLS targets.
//
// `require` is filtered because it produces IMPORTS, not CALLS. Standard lua
// keywords (`if`, `while`, `for`, etc.) never appear as `function_call` heads
// in this grammar (they have their own statement node types), so they don't
// need stop-listing — but we still drop a few common pseudo-callees that
// some grammars surface (e.g. control-flow remnants) for defensive parity
// with the elixir filter.
var luaCallStop = map[string]bool{
	"require": true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// function_call descendant of body. Targets:
//
//   - Bare `helper()`           — first child is `identifier` "helper"
//   - Dotted `foo.run(args)`    — children are identifier ".", identifier
//     ("foo", ".", "run") followed by paren/args.
//     Callee is the LAST identifier before the
//     paren/argument node ("run").
//   - Method `obj:bar(args)`    — same, with `self_call_colon` instead of "."
//
// Self-recursion is dropped. `require` is filtered (handled by IMPORTS).
func extractCallRelationships(body *sitter.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "function_call")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := luaCallTarget(call, src)
		if target == "" || target == callerName {
			continue
		}
		if luaCallStop[target] {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		rels = append(rels, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return rels
}

// luaCallTarget resolves the callee name from a function_call node.
//
// Shape per the smacker/go-tree-sitter Lua grammar:
//
//	function_call
//	  identifier  "foo"             ← receiver, ignored for dotted calls
//	  "."          (or self_call_colon ":")
//	  identifier  "run"             ← callee, this is what we return
//	  function_call_paren "(" / function_arguments / string_argument
//
// For bare `helper()` the first child is the only identifier and IS the
// callee. The rule: take the LAST identifier child that appears before the
// first paren/arguments/string-argument child.
func luaCallTarget(call *sitter.Node, src []byte) string {
	if call == nil || call.ChildCount() == 0 {
		return ""
	}
	last := ""
	for i := 0; i < int(call.ChildCount()); i++ {
		ch := call.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_call_paren", "function_arguments", "string_argument":
			// We've reached the argument list — return the last identifier
			// we saw (the callee).
			return last
		case "identifier":
			last = string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return last
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root *sitter.Node, kinds ...string) []*sitter.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []*sitter.Node
	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}
