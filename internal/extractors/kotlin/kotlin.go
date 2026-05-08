// Package kotlin implements the tree-sitter–based extractor for Kotlin source files.
//
// Extracted entities:
//   - class_declaration    → Kind="SCOPE.Component", Subtype="class"
//   - object_declaration   → Kind="SCOPE.Component", Subtype="object"
//   - function_declaration → Kind="SCOPE.Operation", Subtype="function"
//
// When a class carries a Spring stereotype annotation (@RestController,
// @Controller, @Service, @Component, @Repository) we additionally emit a
// Kind="SCOPE.Service" entity whose Name is the class name, matching the
// Python indexer's output.
//
// MX-1081: Import headers are intentionally NOT emitted as entities or
// IMPORTS relationships. The Python kotlin extractor does not emit them,
// and the Go extractor previously produced ghost "org" / "com" / "java"
// SCOPE.Component entities by splitting import paths on '.', which broke
// parity verdict classification.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package kotlin

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("kotlin", &Extractor{})
}

// Extractor implements extractor.Extractor for Kotlin.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "kotlin" }

// Extract walks the tree-sitter CST and returns entity records for the Kotlin file.
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
		subtype := "class"
		// Detect data class by checking modifiers or raw text.
		raw := string(file.Content[node.StartByte():node.EndByte()])
		if strings.Contains(raw, "data class ") {
			subtype = "data_class"
		}
		if rec, ok := buildComponent(node, file, subtype); ok {
			*out = append(*out, rec)
			// MX-1081: If the class is annotated with a Spring stereotype,
			// emit a parallel SCOPE.Service entity matching the Python
			// indexer's output shape. This lets the kotlin extractor own
			// service detection locally — the global service_detector
			// pattern is suppressed for kotlin to avoid double-emission.
			if svc, ok := buildSpringService(node, file, rec.Name); ok {
				*out = append(*out, svc)
			}
		}

	case "object_declaration":
		if rec, ok := buildComponent(node, file, "object"); ok {
			*out = append(*out, rec)
		}

	case "function_declaration":
		if rec, ok := buildOperation(node, file); ok {
			*out = append(*out, rec)
		}

		// MX-1081: import_header intentionally NOT handled — see package doc.
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for class/object declarations.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	// Kotlin grammar uses type_identifier for class/object names (no "name" field).
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		name = firstChildOfType(node, file.Content, "type_identifier")
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "kotlin",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates an Operation entity for function declarations.
func buildOperation(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// Kotlin grammar uses simple_identifier for function names (no "name" field).
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		name = firstChildOfType(node, file.Content, "simple_identifier")
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "kotlin",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildFunSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// springStereotypes is the set of Spring annotations that promote a Kotlin
// class to a SCOPE.Service entity. Matches the Python indexer.
var springStereotypes = map[string]bool{
	"RestController": true,
	"Controller":     true,
	"Service":        true,
	"Component":      true,
	"Repository":     true,
}

// buildSpringService emits a SCOPE.Service entity for a class declaration
// that carries a Spring stereotype annotation. Returns (_, false) when no
// stereotype is found so the caller can skip the append.
//
// The returned entity shape matches the Python golden:
//
//	name           = class name
//	kind           = "SCOPE.Service"
//	qualified_name = "<source_file>::<class_name>"
//	provenance     = "@<StereotypeName>" (e.g. "@RestController")
//	source_type    = "class"
func buildSpringService(node *sitter.Node, file extractor.FileInput, className string) (types.EntityRecord, bool) {
	if className == "" {
		return types.EntityRecord{}, false
	}
	// Inspect the class declaration's raw text. We scan for an @Stereotype
	// token in the modifiers/annotations that precede the class body.
	raw := string(file.Content[node.StartByte():node.EndByte()])
	classIdx := strings.Index(raw, "class ")
	if classIdx < 0 {
		classIdx = len(raw)
	}
	header := raw[:classIdx]
	stereotype := findSpringStereotype(header)
	if stereotype == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:          className,
		QualifiedName: file.Path + "::" + className,
		Kind:          "SCOPE.Service",
		SourceFile:    file.Path,
		Language:      "kotlin",
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		Properties: map[string]string{
			"provenance":  "@" + stereotype,
			"source_type": "class",
		},
		EnrichmentRequired: false,
	}, true
}

// findSpringStereotype scans a class header (everything before the `class`
// keyword) for the first recognised Spring stereotype annotation token.
// Returns the bare stereotype name (e.g. "RestController") or "".
func findSpringStereotype(header string) string {
	i := 0
	for i < len(header) {
		if header[i] != '@' {
			i++
			continue
		}
		i++
		start := i
		for i < len(header) && (header[i] == '_' || (header[i] >= 'A' && header[i] <= 'Z') || (header[i] >= 'a' && header[i] <= 'z') || (header[i] >= '0' && header[i] <= '9')) {
			i++
		}
		name := header[start:i]
		if springStereotypes[name] {
			return name
		}
	}
	return ""
}

// childFieldText extracts the text of a named child field.
func childFieldText(node *sitter.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// firstChildOfType returns the text of the first direct child with the given node type.
func firstChildOfType(node *sitter.Node, src []byte, nodeType string) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch.Type() == nodeType {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// buildFunSignature builds a function signature (up to body).
// Strips top-level annotations but keeps parameter annotations (e.g., @RequestBody).
// Python convention: "fun name(@ParamAnnotation param: Type): ReturnType".
func buildFunSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip body block
	if idx := strings.Index(raw, " {"); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip one-liner expression body
	if eqIdx := strings.Index(raw, " ="); eqIdx >= 0 {
		raw = raw[:eqIdx]
	}
	// Collapse newlines into spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip only top-level annotations (before "fun" keyword).
	// Keep parameter annotations intact.
	if funIdx := strings.Index(raw, "fun "); funIdx >= 0 {
		prefix := raw[:funIdx]
		suffix := raw[funIdx:]
		prefix = stripKotlinAnnotations(prefix)
		raw = strings.TrimSpace(prefix + suffix)
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature up to the class body.
// Strips annotations to match Python convention: "class Foo" or "data class Foo(...)".
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse to single line.
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip annotations.
	raw = stripKotlinAnnotations(raw)
	return strings.TrimSpace(raw)
}

// stripKotlinAnnotations removes @Annotation and @Annotation(...) tokens.
func stripKotlinAnnotations(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '@' {
			// Skip @Identifier
			i++
			for i < len(s) && (s[i] == '_' || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= '0' && s[i] <= '9')) {
				i++
			}
			// Skip optional (args)
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
