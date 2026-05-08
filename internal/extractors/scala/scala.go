// Package scala implements the tree-sitter–based extractor for Scala source files.
//
// Extracted entities:
//   - function_definition    → Kind="SCOPE.Operation", Subtype="function"
//   - class_definition       → Kind="SCOPE.Component", Subtype="class"
//   - trait_definition       → Kind="SCOPE.Component", Subtype="trait"
//   - object_definition      → Kind="SCOPE.Component", Subtype="object"
//   - case_class_definition  → Kind="SCOPE.Component", Subtype="case_class"
//   - import_declaration     → IMPORTS relationships
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package scala

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("scala", &Extractor{})
}

// Extractor implements extractor.Extractor for Scala.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "scala" }

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
	case "class_definition":
		subtype := "class"
		// Detect case class by checking raw source text.
		raw := string(file.Content[node.StartByte():node.EndByte()])
		if strings.HasPrefix(strings.TrimSpace(raw), "case class ") {
			subtype = "case_class"
		}
		if rec, ok := buildComponent(node, file, subtype); ok {
			*out = append(*out, rec)
		}
	case "trait_definition":
		if rec, ok := buildComponent(node, file, "trait"); ok {
			*out = append(*out, rec)
		}
	case "object_definition":
		if rec, ok := buildComponent(node, file, "object"); ok {
			*out = append(*out, rec)
		}
	case "case_class_definition":
		if rec, ok := buildComponent(node, file, "case_class"); ok {
			*out = append(*out, rec)
		}
	case "function_definition":
		if rec, ok := buildOperation(node, file, "function"); ok {
			*out = append(*out, rec)
		}
	case "function_declaration":
		if rec, ok := buildOperation(node, file, "function"); ok {
			*out = append(*out, rec)
		}
	case "import_declaration":
		recs := buildImports(node, file)
		*out = append(*out, recs...)
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
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "scala",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          classSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates a SCOPE.Operation entity for function definitions.
func buildOperation(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "scala",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          methodSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildImports creates SCOPE.Component entities with IMPORTS relationships.
//
// In smacker/go-tree-sitter/scala, import_declaration children are:
// "import" identifier "." identifier ... [namespace_selectors | identifier]
// We reconstruct the full dotted path from the direct children.
func buildImports(node *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	// Collect the base path and optional selectors.
	var pathParts []string
	var selectors []string

	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		switch t {
		case "import", ".":
			// skip keyword and dots
		case "identifier":
			text := string(file.Content[ch.StartByte():ch.EndByte()])
			pathParts = append(pathParts, text)
		case "namespace_selectors":
			// children: "{" identifier "," identifier ... "}"
			for j := range ch.ChildCount() {
				sel := ch.Child(int(j))
				if sel.Type() == "identifier" {
					selectors = append(selectors, string(file.Content[sel.StartByte():sel.EndByte()]))
				}
			}
		case "stable_identifier", "import_expression", "import_selectors":
			// fallback for other grammar versions
			pathParts = append(pathParts, string(file.Content[ch.StartByte():ch.EndByte()]))
		}
	}

	base := strings.Join(pathParts, ".")

	var targets []string
	if len(selectors) > 0 {
		for _, sel := range selectors {
			if sel != "_" {
				targets = append(targets, base+"."+sel)
			}
		}
	} else if base != "" {
		targets = []string{base}
	}

	if len(targets) == 0 {
		return nil
	}

	var out []types.EntityRecord
	for _, target := range targets {
		top := target
		if idx := strings.Index(target, "."); idx >= 0 {
			top = target[:idx]
		}
		out = append(out, types.EntityRecord{
			Name:       top,
			Kind:       "SCOPE.Component",
			SourceFile: file.Path,
			Language:   "scala",
			Relationships: []types.RelationshipRecord{
				{
					FromID: file.Path,
					ToID:   target,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

// extractName finds the name of a declaration node.
func extractName(node *sitter.Node, src []byte) string {
	if child := node.ChildByFieldName("name"); child != nil {
		return string(src[child.StartByte():child.EndByte()])
	}
	keywords := map[string]bool{
		"class": true, "trait": true, "object": true, "case": true,
		"def": true, "val": true, "var": true, "extends": true,
		"abstract": true, "sealed": true, "final": true, "override": true,
		"private": true, "protected": true, "implicit": true,
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		t := ch.Type()
		if t == "identifier" || t == "type_identifier" {
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
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// methodSignature extracts a clean method signature, truncating at body.
// Matches Python's behavior: "def name(params): ReturnType" without body.
func methodSignature(src []byte, node *sitter.Node) string {
	raw := firstLine(src, node)
	// Remove "override " prefix for cleaner parity
	raw = strings.TrimPrefix(raw, "override ")
	// Truncate at " = " or " = {" or " {"
	for _, sep := range []string{" = {", " = ", " {"} {
		if idx := strings.Index(raw, sep); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

// classSignature extracts a clean class/trait signature without body.
// Strips extends/with clauses and type parameters to match Python convention.
func classSignature(src []byte, node *sitter.Node) string {
	raw := firstLine(src, node)
	// Truncate at opening brace or opening paren for case classes with params.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip extends clause.
	if idx := strings.Index(raw, " extends "); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip type parameters: Name[T, ID] -> Name
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}
