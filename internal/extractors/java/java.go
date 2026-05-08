// Package java implements the tree-sitter–based extractor for Java source files.
//
// Extracted entities:
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration   → Kind="SCOPE.Component", Subtype="interface"
//   - method_declaration      → Kind="SCOPE.Operation", Subtype="method"
//   - constructor_declaration → Kind="SCOPE.Operation", Subtype="constructor"
//   - import_declaration      → IMPORTS relationship
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package java

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("java", &Extractor{})
}

// Extractor implements extractor.Extractor for Java.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "java" }

// Extract walks the tree-sitter CST and returns entity records for the Java file.
//
// OTel span "extractor.java" carries attributes: file, entity_count,
// error_pattern_count (MX-1047).
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.java")
	_, span := tracer.Start(ctx, "extractor.java")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if file.Tree == nil || len(file.Content) == 0 {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("error_pattern_count", 0),
		)
		return nil, nil
	}

	var entities []types.EntityRecord
	root := file.Tree.RootNode()
	walk(root, file, &entities)

	// Secondary pass: error-handling patterns (MX-1047).
	errorPatterns := extractErrorHandlingPatterns(root, file.Path)
	entities = append(entities, errorPatterns...)

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
		attribute.Int("error_pattern_count", len(errorPatterns)),
	)
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

	case "field_declaration":
		if rec, ok := buildField(node, file); ok {
			*out = append(*out, rec)
		}

	case "import_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for class/interface declarations.
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
		Language:           "java",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates an Operation entity for method/constructor declarations.
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
		Language:           "java",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildField creates a Schema entity for field declarations.
func buildField(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// Field declarations have a "declarator" child containing the variable name.
	name := ""
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch.Type() == "variable_declarator" {
			name = childFieldText(ch, "name", file.Content)
			break
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	// Build field signature: "Type name" (strip visibility).
	fieldSig := buildFieldSignature(node, file.Content, name)

	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    "field",
		SourceFile: file.Path,
		Language:   "java",
		StartLine:  int(node.StartPoint().Row) + 1,
		EndLine:    int(node.EndPoint().Row) + 1,
		Signature:  fieldSig,
	}, true
}

// buildFieldSignature produces "Type name" for a Java field, stripping visibility.
func buildFieldSignature(node *sitter.Node, src []byte, name string) string {
	raw := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	// Remove everything after '=' (initializer).
	if idx := strings.Index(raw, "="); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	// Remove trailing ';'.
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	// Strip visibility modifiers.
	for _, mod := range []string{"public ", "private ", "protected ", "static ", "final "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	return strings.TrimSpace(raw)
}

// buildImport creates a Component entity representing an imported package.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	raw = strings.TrimPrefix(raw, "import ")
	raw = strings.TrimPrefix(raw, "static ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	// Top-level package is the first segment.
	top := raw
	if idx := strings.Index(raw, "."); idx >= 0 {
		top = raw[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "java",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   raw,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// childFieldText extracts the text of a named child field (e.g. "name").
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature.
// Captures annotations + return type + name + parameters, collapsing
// multi-line declarations into a single line (up to the opening brace).
// Strips visibility modifiers and annotation arguments.
func buildMethodSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip annotation arguments FIRST to remove braces inside annotation args
	// like @DeleteMapping("/{id}") → @DeleteMapping, so the body-brace search
	// doesn't get confused by braces in annotation strings.
	raw = stripAnnotationArgs(raw)
	// Trim at opening brace (body start).
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip visibility modifiers to match Python convention.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the opening brace.
// Strips visibility modifiers and annotation arguments to match Python convention.
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip visibility modifiers.
	for _, mod := range []string{"public ", "private ", "protected ", "static "} {
		raw = strings.ReplaceAll(raw, mod, "")
	}
	// Strip annotation arguments: @Foo("bar") -> @Foo
	raw = stripAnnotationArgs(raw)
	return strings.TrimSpace(raw)
}

// stripAnnotationArgs removes parenthesised arguments from Java annotations.
// @RequestMapping("/api/users") -> @RequestMapping
// Only strips args immediately following an @Identifier — does not affect
// method parameter parens.
func stripAnnotationArgs(s string) string {
	var result strings.Builder
	depth := 0
	// expectAnnotationParen: true right after @AnnotationName, before a space or (.
	expectAnnotationParen := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '@':
			expectAnnotationParen = true
			result.WriteByte(ch)
		case expectAnnotationParen && (ch == '_' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')):
			// Still in annotation identifier name.
			result.WriteByte(ch)
		case expectAnnotationParen && ch == '(':
			// Annotation args start — eat until matching ')'.
			depth = 1
			expectAnnotationParen = false
			for i++; i < len(s) && depth > 0; i++ {
				switch s[i] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			i-- // outer loop will i++
		case expectAnnotationParen:
			// Non-identifier char after @Name — annotation has no args.
			expectAnnotationParen = false
			result.WriteByte(ch)
		default:
			result.WriteByte(ch)
		}
	}
	return result.String()
}
