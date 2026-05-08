// Package swift implements the tree-sitter–based extractor for Swift source files.
//
// Extracted entities:
//   - function_declaration  → Kind="SCOPE.Operation", Subtype="function"
//   - class_declaration     → Kind="SCOPE.Component", Subtype="class"
//   - struct_declaration    → Kind="SCOPE.Component", Subtype="struct"
//   - protocol_declaration  → Kind="SCOPE.Component", Subtype="protocol"
//   - import_declaration    → IMPORTS relationship
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package swift

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("swift", &Extractor{})
}

// Extractor implements extractor.Extractor for Swift.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "swift" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.Tree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	walkNode(file.Tree.RootNode(), file, &entities)
	return entities, nil
}

// walkNode performs a depth-first traversal.
func walkNode(node *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration":
		// In smacker/go-tree-sitter/swift the node type "class_declaration"
		// is used for both class and struct declarations. Distinguish by the
		// first keyword child: "class" → class, "struct" → struct.
		subtype := "class"
		if node.ChildCount() > 0 {
			kw := node.Child(0)
			if kw != nil && string(file.Content[kw.StartByte():kw.EndByte()]) == "struct" {
				subtype = "struct"
			}
		}
		if rec, ok := buildComponent(node, file, subtype); ok {
			*out = append(*out, rec)
		}
	case "struct_declaration":
		if rec, ok := buildComponent(node, file, "struct"); ok {
			*out = append(*out, rec)
		}
	case "protocol_declaration":
		if rec, ok := buildComponent(node, file, "protocol"); ok {
			*out = append(*out, rec)
		}
	case "function_declaration":
		if rec, ok := buildOperation(node, file, "function"); ok {
			*out = append(*out, rec)
		}
	case "import_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkNode(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a SCOPE.Component entity.
func buildComponent(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	// Python signature format: "type Name" (keyword + name only)
	keyword := subtype
	if keyword == "struct" {
		keyword = "type"
	} else {
		keyword = "type"
	}
	sig := keyword + " " + name
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "swift",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates a SCOPE.Operation entity.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	// Python signature format: "func name() -> ReturnType" (no body)
	sig := methodSignature(file.Content, node)
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "swift",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// methodSignature extracts a clean method signature matching Python's format:
// "func name() -> ReturnType" — uses "()" placeholder and only includes return type.
func methodSignature(src []byte, node *sitter.Node) string {
	name := extractName(node, src)
	if name == "" {
		return ""
	}
	// Find return type by looking for "->" in the first line
	raw := firstLine(src, node)
	returnType := ""
	if idx := strings.Index(raw, "->"); idx >= 0 {
		rt := strings.TrimSpace(raw[idx+2:])
		// Remove trailing brace
		if braceIdx := strings.Index(rt, "{"); braceIdx >= 0 {
			rt = strings.TrimSpace(rt[:braceIdx])
		}
		returnType = " -> " + rt
	}
	return "func " + name + "()" + returnType
}

// buildImport creates a SCOPE.Component entity with an IMPORTS relationship.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := extractImportPath(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:       raw,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "swift",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   raw,
				Kind:   "IMPORTS",
			},
		},
	}, true
}

// extractImportPath extracts the module name from an import_declaration.
func extractImportPath(node *sitter.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		if t == "identifier" || t == "path_component" || t == "qualified_name" ||
			t == "simple_identifier" || t == "type_identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	// Fallback
	full := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	return strings.TrimPrefix(full, "import ")
}

// extractName finds the name of a declaration node.
func extractName(node *sitter.Node, src []byte) string {
	if child := node.ChildByFieldName("name"); child != nil {
		return string(src[child.StartByte():child.EndByte()])
	}
	// Fallback: first type_identifier or simple_identifier that isn't a keyword.
	keywords := map[string]bool{
		"class": true, "struct": true, "protocol": true, "func": true,
		"import": true, "open": true, "public": true, "internal": true,
		"private": true, "final": true, "override": true,
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		if t == "type_identifier" || t == "simple_identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
		if t == "identifier" {
			name := string(src[ch.StartByte():ch.EndByte()])
			if !keywords[name] {
				return name
			}
		}
	}
	return ""
}

// firstLine returns the first line of the node's source text.
func firstLine(src []byte, node *sitter.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}
