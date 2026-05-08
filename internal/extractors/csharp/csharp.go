// Package csharp implements the tree-sitter–based extractor for C# source files.
//
// Extracted entities:
//   - method_declaration      → Kind="SCOPE.Operation", Subtype="method"
//   - constructor_declaration → Kind="SCOPE.Operation", Subtype="constructor"
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration   → Kind="SCOPE.Component", Subtype="interface"
//   - using_directive         → IMPORTS relationship
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package csharp

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("csharp", &Extractor{})
}

// Extractor implements extractor.Extractor for C#.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "csharp" }

// Extract walks the tree-sitter CST and returns entity records for the C# file.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	walk(file.Tree.RootNode(), file, &entities)
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
func walk(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration":
		if rec, ok := buildComponent(node, file, "class"); ok {
			*out = append(*out, rec)
		}

	case "interface_declaration":
		if rec, ok := buildComponent(node, file, "interface"); ok {
			*out = append(*out, rec)
		}

	case "method_declaration":
		if rec, ok := buildOperation(node, file, "method"); ok {
			*out = append(*out, rec)
		}

	case "constructor_declaration":
		if rec, ok := buildOperation(node, file, "constructor"); ok {
			*out = append(*out, rec)
		}

	case "using_directive":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a SCOPE.Component entity for class/interface declarations.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "csharp",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates a SCOPE.Operation entity for method/constructor declarations.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "csharp",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildImport creates a SCOPE.Component entity with an IMPORTS relationship.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := extractUsingTarget(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	// Top-level namespace is the first segment.
	top := raw
	if idx := strings.Index(raw, "."); idx >= 0 {
		top = raw[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "csharp",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   raw,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// extractUsingTarget returns the namespace path from a using_directive node.
func extractUsingTarget(node *sitter.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		if t == "qualified_name" || t == "member_access_expression" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
		if t == "identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	// Fallback: strip "using " and ";"
	full := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	full = strings.TrimSuffix(full, ";")
	full = strings.TrimPrefix(full, "using ")
	full = strings.TrimPrefix(full, "static ")
	return strings.TrimSpace(full)
}

// childFieldText extracts the text of a named child field (e.g. "name").
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature for C#.
// Collapses multi-line declarations, strips attribute args, keeps visibility.
func buildMethodSignature(src []byte, node *sitter.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip attribute arguments FIRST to remove braces inside attribute args
	// like [HttpGet("{id}")] → [HttpGet], before body-brace search.
	raw = stripCSharpAttributeArgs(raw)
	// Trim at body start.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Remove lambda-style body.
	if idx := strings.Index(raw, "=>"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	return strings.TrimSpace(raw)
}

// buildClassSignature returns a short signature for class/interface declarations.
// Strips attributes and inheritance to match Python convention: "public class Name".
func buildClassSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip attributes entirely (Python doesn't include them for C# classes).
	raw = stripCSharpAttributes(raw)
	// Strip inheritance (: BaseClass).
	if idx := strings.Index(raw, " :"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// stripCSharpAttributeArgs strips arguments from C# attributes: [Foo("bar")] -> [Foo].
func stripCSharpAttributeArgs(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '[' {
			// Copy [
			result.WriteByte('[')
			i++
			// Copy attribute name.
			for i < len(s) && s[i] != '(' && s[i] != ']' {
				result.WriteByte(s[i])
				i++
			}
			// Skip (args).
			if i < len(s) && s[i] == '(' {
				depth := 1
				i++
				for i < len(s) && depth > 0 {
					switch s[i] {
					case '(':
						depth++
					case ')':
						depth--
					}
					i++
				}
			}
			// Copy ].
			if i < len(s) && s[i] == ']' {
				result.WriteByte(']')
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// stripCSharpAttributes removes all [Attribute] and [Attribute(...)] tokens entirely.
func stripCSharpAttributes(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '[' {
			// Skip until matching ].
			depth := 1
			i++
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '[':
					depth++
				case ']':
					depth--
				}
				i++
			}
			// Skip trailing space.
			for i < len(s) && s[i] == ' ' {
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}
