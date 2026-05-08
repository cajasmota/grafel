// Package lua implements the tree-sitter–based extractor for Lua source files.
//
// Extracted entities:
//   - function_declaration (global, dot, colon) → Kind="SCOPE.Operation", Subtype="function"/"method"
//   - local function_declaration                → Kind="SCOPE.Operation", Subtype="function"
//
// Uses the lua grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
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

	imports := collectRequires(file.Tree.RootNode(), file.Content)
	var entities []types.EntityRecord
	walkLua(file.Tree.RootNode(), file, imports, &entities)
	return entities, nil
}

// walkLua performs a depth-first traversal collecting function nodes.
// The smacker/go-tree-sitter Lua grammar uses "function_statement" for both
// global and local functions (local functions have a "local" keyword child).
// "function_declaration" and "local_function" are used by tree-sitter-lua in
// Python — not present in this version of the grammar.
func walkLua(node *sitter.Node, file extractor.FileInput, imports []string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "function_statement":
		if rec, ok := buildFunctionStatement(node, file, imports); ok {
			*out = append(*out, rec)
		}
	// Legacy node type names (Python grammar compatibility).
	case "function_declaration":
		if rec, ok := buildFunctionDecl(node, file, imports); ok {
			*out = append(*out, rec)
		}
	case "local_function":
		if rec, ok := buildLocalFunction(node, file, imports); ok {
			*out = append(*out, rec)
		}
	}
	for i := range node.ChildCount() {
		walkLua(node.Child(int(i)), file, imports, out)
	}
}

// buildFunctionStatement handles function_statement nodes from the smacker/go-tree-sitter
// Lua grammar. Both global and local functions emit this node type.
// Global:  function M.name(params) ... end
// Local:   local function name(params) ... end
func buildFunctionStatement(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
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
	var nameNode *sitter.Node
	var rawName string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_name":
			nameNode = ch
			rawName = string(file.Content[ch.StartByte():ch.EndByte()])
		case "identifier":
			if isLocal && nameNode == nil {
				nameNode = ch
				rawName = string(file.Content[ch.StartByte():ch.EndByte()])
			}
		}
	}
	if rawName == "" {
		return types.EntityRecord{}, false
	}

	subtype := "function"
	if strings.Contains(rawName, ":") {
		subtype = "method"
	}
	// Use last segment as entity name.
	name := rawName
	if idx := strings.LastIndexAny(rawName, ":."); idx >= 0 {
		name = rawName[idx+1:]
	}
	if name == "" {
		return types.EntityRecord{}, false
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
	}, true
}

// buildFunctionDecl handles function_declaration nodes (global or dot/colon notation).
func buildFunctionDecl(node *sitter.Node, file extractor.FileInput, imports []string) (types.EntityRecord, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	fullName := string(file.Content[nameNode.StartByte():nameNode.EndByte()])
	subtype := "function"
	if strings.Contains(fullName, ":") {
		subtype = "method"
	}
	// Use last segment as entity name.
	name := fullName
	if idx := strings.LastIndexAny(fullName, ":."); idx >= 0 {
		name = fullName[idx+1:]
	}
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
	}, true
}

// buildLocalFunction handles local_function nodes.
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

// collectRequires gathers require("module") calls as import equivalents.
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
		if first != nil && string(src[first.StartByte():first.EndByte()]) == "require" {
			// smacker/go-tree-sitter Lua grammar uses function_arguments or string_argument.
			for i := range node.ChildCount() {
				ch := node.Child(int(i))
				if ch == nil {
					continue
				}
				switch ch.Type() {
				case "function_arguments":
					// Look for string child, then string_content inside it.
					for j := range ch.ChildCount() {
						arg := ch.Child(int(j))
						if arg != nil && arg.Type() == "string" {
							if raw := extractStringContent(arg, src); raw != "" {
								*out = append(*out, raw)
							}
						}
					}
				case "string_argument":
					if raw := extractStringContent(ch, src); raw != "" {
						*out = append(*out, raw)
					}
				case "string":
					if raw := extractStringContent(ch, src); raw != "" {
						*out = append(*out, raw)
					}
				}
			}
		}
	}
	for i := range node.ChildCount() {
		walkForRequires(node.Child(int(i)), src, out)
	}
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
