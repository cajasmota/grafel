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
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "rust")
	return entities, nil
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// PORT-2-FIX-2-ALL (#41): trait_item and impl_item attach a CONTAINS edge
// per function_item declared inside their declaration_list, every
// function_item body emits CALLS edges with stub to_id, and use_declaration
// nodes already emit IMPORTS (untouched).
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
		rec, ok := buildComponent(node, file, "trait")
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out)
			}
			return
		}
		traitIdx := len(*out)
		*out = append(*out, rec)
		body := findRustDeclList(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Issue #144 — structural-ref (Format A) keyed on file path
				// so trait→method CONTAINS edges disambiguate by location.
				toID := extractor.BuildOperationStructuralRef("rust", file.Path, child.Name)
				(*out)[traitIdx].Relationships = append((*out)[traitIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "impl_item":
		rec, ok := buildImpl(node, file)
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, out)
			}
			return
		}
		implIdx := len(*out)
		*out = append(*out, rec)
		body := findRustDeclList(node)
		if body != nil {
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				// Issue #144 — structural-ref (Format A) keyed on file path
				// so impl→method CONTAINS edges disambiguate when two files
				// each define an `impl Foo { fn new() }` shape.
				toID := extractor.BuildOperationStructuralRef("rust", file.Path, child.Name)
				(*out)[implIdx].Relationships = append((*out)[implIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		return

	case "function_item":
		if rec, ok := buildOperation(node, file); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(node.ChildByFieldName("body"), file.Content, rec.Name)...)
			*out = append(*out, rec)
		}
		return

	case "use_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// findRustDeclList returns the declaration_list child of a trait_item or
// impl_item, or nil when the body is missing.
func findRustDeclList(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "declaration_list" {
			return ch
		}
	}
	return nil
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression / macro_invocation descendant of body. Targets resolve to
// the rightmost identifier in the function expression; FromID is left empty
// so buildDocument substitutes the caller's entity ID at emit time.
func extractCallRelationships(body *sitter.Node, src []byte, callerName string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression", "macro_invocation")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := rustCallTarget(call, src)
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

// rustCallTarget resolves the callee identifier from a Rust call_expression
// or macro_invocation. For call_expression the function is the first child;
// for scoped_identifier / field_expression we use the rightmost identifier.
func rustCallTarget(call *sitter.Node, src []byte) string {
	switch call.Type() {
	case "call_expression":
		fn := call.ChildByFieldName("function")
		if fn == nil && call.ChildCount() > 0 {
			fn = call.Child(0)
		}
		if fn == nil {
			return ""
		}
		switch fn.Type() {
		case "identifier":
			return string(src[fn.StartByte():fn.EndByte()])
		case "scoped_identifier":
			if name := fn.ChildByFieldName("name"); name != nil {
				return string(src[name.StartByte():name.EndByte()])
			}
		case "field_expression":
			if name := fn.ChildByFieldName("field"); name != nil {
				return string(src[name.StartByte():name.EndByte()])
			}
		case "generic_function":
			if path := fn.ChildByFieldName("function"); path != nil {
				switch path.Type() {
				case "identifier":
					return string(src[path.StartByte():path.EndByte()])
				case "scoped_identifier":
					if name := path.ChildByFieldName("name"); name != nil {
						return string(src[name.StartByte():name.EndByte()])
					}
				case "field_expression":
					if name := path.ChildByFieldName("field"); name != nil {
						return string(src[name.StartByte():name.EndByte()])
					}
				}
			}
		}
	case "macro_invocation":
		if m := call.ChildByFieldName("macro"); m != nil {
			t := m.Type()
			if t == "identifier" {
				return string(src[m.StartByte():m.EndByte()])
			}
			if t == "scoped_identifier" {
				if name := m.ChildByFieldName("name"); name != nil {
					return string(src[name.StartByte():name.EndByte()])
				}
			}
		}
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
//
// Issue #101: pub-modifier and intra-crate prefixes are stripped here so
// the synthesised stub reaches the resolver in the canonical
// "<crate>::<path>" shape that synth.go's `::` branch matches against
// the external-crate allowlist. Without this:
//   - `pub use client::Foo` left the literal "pub" prefix on the stub
//     and never matched anything.
//   - `crate::module::Item` / `self::sibling` / `super::parent` are
//     intra-crate references; emitting them as IMPORTS guarantees a
//     bug-extractor since they cannot be on any external allowlist.
//     We drop them entirely.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := strings.TrimSpace(string(file.Content[node.StartByte():node.EndByte()]))
	// Visibility modifiers — `pub use ...`, `pub(crate) use ...`,
	// `pub(super) use ...`. Strip the modifier before the `use` token.
	raw = stripRustVisibility(raw)
	raw = strings.TrimPrefix(raw, "use ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	// Intra-crate paths are not external imports; emitting them as
	// IMPORTS would force a bug-extractor classification. The resolver
	// has no machinery to bind `crate::Foo` to a specific entity in
	// the same crate from this layer (Issue #101).
	if strings.HasPrefix(raw, "crate::") || raw == "crate" ||
		strings.HasPrefix(raw, "self::") || raw == "self" ||
		strings.HasPrefix(raw, "super::") || raw == "super" {
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

// stripRustVisibility removes a leading Rust visibility modifier from a
// declaration's source text. Handles `pub `, `pub(crate) `,
// `pub(super) `, `pub(in path::to::mod) `. Anything else is returned
// unchanged. Issue #101.
func stripRustVisibility(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "pub") {
		return s
	}
	rest := s[3:]
	if rest == "" {
		return s
	}
	// Plain `pub <decl>`.
	if rest[0] == ' ' || rest[0] == '\t' {
		return strings.TrimSpace(rest)
	}
	// Restricted vis: `pub(...) <decl>`.
	if rest[0] == '(' {
		if closeIdx := strings.IndexByte(rest, ')'); closeIdx >= 0 {
			return strings.TrimSpace(rest[closeIdx+1:])
		}
	}
	return s
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
