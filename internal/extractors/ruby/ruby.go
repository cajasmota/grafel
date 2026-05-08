// Package ruby implements the tree-sitter–based extractor for Ruby source files.
//
// Extracted entities:
//   - class            → Kind="SCOPE.Component", Subtype="class"
//   - module           → Kind="SCOPE.Component", Subtype="module"
//   - method           → Kind="SCOPE.Operation", Subtype="method"
//   - singleton_method → Kind="SCOPE.Operation", Subtype="singleton_method"
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package ruby

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("ruby", &Extractor{})
}

// Extractor implements extractor.Extractor for Ruby.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "ruby" }

// Extract walks the tree-sitter CST and returns entity records for the Ruby file.
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
	case "class":
		if rec, ok := buildComponent(node, file, "class"); ok {
			*out = append(*out, rec)
		}

	case "module":
		if rec, ok := buildComponent(node, file, "module"); ok {
			*out = append(*out, rec)
		}

	case "method":
		if rec, ok := buildMethod(node, file, "function"); ok {
			*out = append(*out, rec)
		}

	case "singleton_method":
		if rec, ok := buildMethod(node, file, "function"); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for class/module definitions.
// Rails-specific framework labelling (MX-1106) is applied via tagRails:
// controllers, models, migrations and routes get framework="rails" plus
// a kind discriminator in Properties.
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
		Language:           "ruby",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}
	tagRails(&rec, node, file.Content, file.Path)
	return rec, true
}

// buildMethod creates an Operation entity for method definitions.
func buildMethod(node *sitter.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	sig := buildMethodSignature(node, file.Content)
	// Python adds "()" to Ruby method signatures for parity
	if !strings.Contains(sig, "(") {
		sig = sig + "()"
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "ruby",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
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

// buildMethodSignature builds a def signature (first line).
func buildMethodSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

// buildClassSignature constructs a readable signature for class/module.
func buildClassSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}
