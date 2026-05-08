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
// error_pattern_count.
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
	walk(root, file, "", &entities)

	// Secondary pass: error-handling patterns.
	errorPatterns := extractErrorHandlingPatterns(root, file.Path)
	entities = append(entities, errorPatterns...)

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
		attribute.Int("error_pattern_count", len(errorPatterns)),
	)
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): class/interface declarations attach a CONTAINS
// edge per method/constructor declared inside the body, and every method
// or constructor body is scanned for method_invocation / object_creation
// nodes that yield CALLS edges with stub `to_id` (resolver rewrites
// cross-file refs in pass 5).
//
// Issue #65: methods/constructors declared inside a class, interface, or
// enum body are emitted with Name="<EnclosingType>.<member>" so that
// EntityRecord.ComputeID(SourceFile+Kind+Name) produces distinct IDs for
// same-named members on sibling types. Module-level constructs and
// methods inside anonymous classes (which lack a stable enclosing-type
// name) stay bare. Nested types carry only their immediate parent — the
// nested class/interface/enum itself stays bare, but its members are
// qualified by it (multi-dot fully-qualified IDs are out of scope here).
func walk(node *sitter.Node, file extractor.FileInput, parentType string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration", "interface_declaration", "enum_declaration":
		subtype := "class"
		switch node.Type() {
		case "interface_declaration":
			subtype = "interface"
		case "enum_declaration":
			subtype = "enum"
		}
		rec, ok := buildComponent(node, file, subtype)
		if !ok {
			// Still recurse so nested types/imports below this node are
			// captured even when the class itself is malformed.
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, parentType, out)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)
		body := node.ChildByFieldName("body")
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				// Members of this type are qualified by rec.Name (the
				// immediate enclosing type), regardless of any outer
				// type we may currently be nested under. Enum bodies wrap
				// methods/constructors in an extra `enum_body_declarations`
				// node — descend through it so those members still receive
				// the enclosing-enum qualification.
				child := body.Child(int(i))
				if child != nil && child.Type() == "enum_body_declarations" {
					for j := range child.ChildCount() {
						walk(child.Child(int(j)), file, rec.Name, out)
					}
					continue
				}
				walk(child, file, rec.Name, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						// ToID matches the dotted Name emitted by
						// buildOperation when parentType is non-empty.
						ToID: child.Name,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "method_declaration":
		if rec, ok := buildOperation(node, file, "method", parentType); ok {
			// Self-recursion is detected by the bare callee identifier;
			// extractCallRelationships compares against the caller name.
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, selfName)...)
			*out = append(*out, rec)
		}
		return

	case "constructor_declaration":
		if rec, ok := buildOperation(node, file, "constructor", parentType); ok {
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, selfName)...)
			*out = append(*out, rec)
		}
		return

	case "field_declaration":
		if rec, ok := buildField(node, file); ok {
			*out = append(*out, rec)
		}

	case "import_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	// Default recursion. parentType does NOT propagate through unrelated
	// nodes (e.g. method bodies, anonymous-class bodies) — methods nested
	// inside a method body or anonymous class are emitted bare because
	// they have no stable enclosing-type identifier.
	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, "", out)
	}
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// method_invocation / object_creation_expression descendant of body. Target
// resolves to the bare callee name (last identifier of a selector chain).
// FromID is left empty so buildDocument substitutes the caller's entity ID
// at emit time. Self-recursion is skipped.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "method_invocation", "object_creation_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := javaCallTarget(call, src)
		if target == "" || target == callerName {
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

// javaCallTarget resolves the callee name from a method_invocation or
// object_creation_expression node. For method_invocation it uses the "name"
// field; for object_creation_expression it uses the "type" field's trailing
// identifier (e.g. `new com.foo.Bar()` → "Bar").
func javaCallTarget(call *sitter.Node, src []byte) string {
	switch call.Type() {
	case "method_invocation":
		if name := call.ChildByFieldName("name"); name != nil {
			return string(src[name.StartByte():name.EndByte()])
		}
	case "object_creation_expression":
		typ := call.ChildByFieldName("type")
		if typ == nil {
			return ""
		}
		// Walk to the rightmost type_identifier.
		ids := findAllNodes(typ, "type_identifier")
		if len(ids) > 0 {
			n := ids[len(ids)-1]
			return string(src[n.StartByte():n.EndByte()])
		}
		return string(src[typ.StartByte():typ.EndByte()])
	}
	return ""
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
//
// Issue #65: when parentType is non-empty (member of a class/interface/enum),
// Name is emitted as "<parentType>.<member>" so two sibling types declaring
// same-named methods produce distinct ComputeID(SourceFile+Kind+Name) values.
// The dotted form is the encoding consumed by resolve.Index.byMember, which
// splits on the first '.'.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype, parentType string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	emittedName := name
	if parentType != "" {
		emittedName = parentType + "." + name
	}

	return types.EntityRecord{
		Name:               emittedName,
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

// nodeText returns the source text covered by node.
func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
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
