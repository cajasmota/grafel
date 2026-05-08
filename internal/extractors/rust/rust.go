// Package rust implements the tree-sitter–based extractor for Rust source files.
//
// Extracted entities:
//   - struct_item     → Kind="SCOPE.Component", Subtype="struct"
//   - enum_item       → Kind="SCOPE.Component", Subtype="enum"
//   - trait_item      → Kind="SCOPE.Component", Subtype="trait"
//   - impl_item       → Kind="SCOPE.Component", Subtype="impl"
//   - function_item   → Kind="SCOPE.Operation", Subtype="function"
//   - use_declaration → IMPORTS relationship
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package rust

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("rust", &Extractor{})
}

// Extractor implements extractor.Extractor for Rust.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "rust" }

// Extract walks the tree-sitter CST and returns entity records for the Rust file.
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
	case "struct_item":
		if rec, ok := buildComponent(node, file, "struct"); ok {
			*out = append(*out, rec)
		}

	case "enum_item":
		if rec, ok := buildComponent(node, file, "enum"); ok {
			*out = append(*out, rec)
		}

	case "trait_item":
		if rec, ok := buildComponent(node, file, "trait"); ok {
			*out = append(*out, rec)
		}

	case "impl_item":
		if rec, ok := buildImpl(node, file); ok {
			*out = append(*out, rec)
		}

	case "function_item":
		if rec, ok := buildOperation(node, file); ok {
			*out = append(*out, rec)
		}

	case "use_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for struct/enum/trait items.
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
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildTypeSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildImpl creates a Component entity for impl blocks.
// impl_item uses "type" field (impl Foo) or "trait" + "type" (impl Trait for Foo).
func buildImpl(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// "type" field holds the implementing type.
	name := childFieldText(node, "type", file.Content)
	if name == "" {
		// Fallback: scan for type_identifier or generic_type child.
		for i := range node.ChildCount() {
			ch := node.Child(int(i))
			t := ch.Type()
			if t == "type_identifier" || t == "generic_type" {
				name = string(file.Content[ch.StartByte():ch.EndByte()])
				break
			}
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            "impl",
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildTypeSignature(node, file.Content, name),
		EnrichmentRequired: false,
	}, true
}

// buildOperation creates an Operation entity for function items.
func buildOperation(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	sig := buildFnSignature(node, file.Content)
	// Strip "async " prefix to match Python parity
	sig = strings.TrimPrefix(sig, "async ")
	// Strip "pub " prefix for cleaner signatures
	sig = strings.TrimPrefix(sig, "pub ")
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "function",
		SourceFile:         file.Path,
		Language:           "rust",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// buildImport creates a Component entity for use declarations.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	raw = strings.TrimPrefix(raw, "use ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	top := raw
	if idx := strings.Index(raw, "::"); idx >= 0 {
		top = raw[:idx]
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "rust",
		Relationships: []types.RelationshipRecord{
			{
				FromID: file.Path,
				ToID:   raw,
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

// buildFnSignature builds the function signature (up to the body block).
func buildFnSignature(node *sitter.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, " {"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

// buildTypeSignature constructs a readable signature for struct/enum/trait/impl.
func buildTypeSignature(node *sitter.Node, src []byte, name string) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	if idx := strings.Index(raw, ";"); idx >= 0 {
		return strings.TrimSpace(raw[:idx+1])
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return name
}
