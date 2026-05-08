// Package php implements the tree-sitter–based extractor for PHP source files.
//
// Extracted entities:
//   - class_declaration     → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration → Kind="SCOPE.Component", Subtype="interface"
//   - method_declaration    → Kind="SCOPE.Operation", Subtype="method"
//   - function_definition   → Kind="SCOPE.Operation", Subtype="function"
//   - namespace_definition  → IMPORTS relationship
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package php

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("php", &Extractor{})
}

// Extractor implements extractor.Extractor for PHP.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "php" }

// Extract walks the tree-sitter CST and returns entity records for the PHP file.
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

	case "function_definition":
		if rec, ok := buildOperation(node, file, "function"); ok {
			*out = append(*out, rec)
		}

	case "namespace_definition":
		if rec, ok := buildNamespace(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for class/interface declarations.
// Eloquent / Laravel framework labelling (MX-1106) is applied via tagEloquent:
// models, migrations and controllers get framework="laravel" plus a kind
// discriminator in Properties.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	rec := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "php",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}
	tagEloquent(&rec, node, file.Content, file.Path)
	return rec, true
}

// buildOperation creates an Operation entity for method/function declarations.
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
		Language:           "php",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildNamespace emits a Component representing a PHP namespace.
func buildNamespace(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		// Fallback: extract text after "namespace " keyword
		raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
		raw = strings.TrimPrefix(raw, "namespace ")
		if idx := strings.IndexAny(raw, " {;"); idx >= 0 {
			raw = raw[:idx]
		}
		name = strings.TrimSpace(raw)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	top := name
	if idx := strings.Index(name, "\\"); idx >= 0 {
		top = name[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "php",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   name,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// childFieldText extracts the text of a named child field.
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature.
// Python strips visibility modifiers and return types, keeping only:
//
//	function name(params)
func buildMethodSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)

	// Strip trailing { or body.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}

	// Strip return type annotation ": type" after closing paren.
	if parenIdx := strings.LastIndex(raw, ")"); parenIdx >= 0 {
		afterParen := raw[parenIdx+1:]
		if colonIdx := strings.Index(afterParen, ":"); colonIdx >= 0 {
			raw = raw[:parenIdx+1]
		}
	}

	// Strip visibility modifiers to match Python convention.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.TrimPrefix(raw, mod)
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the class body.
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}
