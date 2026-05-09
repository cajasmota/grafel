// Package php implements the tree-sitter–based extractor for PHP source files.
//
// Extracted entities:
//   - class_declaration     → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration → Kind="SCOPE.Component", Subtype="interface"
//   - method_declaration    → Kind="SCOPE.Operation", Subtype="method"
//   - function_definition   → Kind="SCOPE.Operation", Subtype="function"
//   - namespace_definition       → IMPORTS relationship (file → own namespace)
//   - namespace_use_declaration  → IMPORTS relationship (file → imported FQN)
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
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "php")
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

	case "namespace_use_declaration":
		// Issue #102: emit one IMPORTS edge per `use` statement so the
		// synth allowlist (Symfony\, Doctrine\, Twig\, Psr\, ...) can
		// classify the FQN as ExternalKnown via the `\`-separator
		// branch in classifyExternal. Without this every `use Foo\Bar;`
		// would be invisible to the resolver and the bug-rate stays
		// pinned to whatever extractor emitted before #102.
		for _, rec := range buildUseImports(node, file) {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, out)
	}
}

// buildComponent creates a Component entity for class/interface declarations.
// Eloquent / Laravel framework labelling is applied via tagEloquent:
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

// buildUseImports emits one IMPORTS edge per imported symbol on a
// `namespace_use_declaration` node. Issue #102.
//
// PHP `use` shapes handled:
//   - simple:    use Foo\Bar;                 → IMPORTS Foo\Bar
//   - aliased:   use Foo\Bar as B;            → IMPORTS Foo\Bar (alias dropped)
//   - function:  use function Foo\helper;     → IMPORTS Foo\helper
//   - const:     use const Foo\PI;            → IMPORTS Foo\PI
//   - grouped:   use Foo\Bar\{A, B as C};     → IMPORTS Foo\Bar\A, Foo\Bar\B
//
// Aliases are intentionally stripped: the synth allowlist matches on the
// root namespace segment (Symfony, Doctrine, Twig, ...), so emitting the
// canonical FQN gives the synth `\`-branch a clean root to classify.
func buildUseImports(node *sitter.Node, file extractor.FileInput) []types.EntityRecord {
	if node == nil {
		return nil
	}

	// Detect grouped use: a child of type "namespace_use_group" preceded
	// by a `namespace_name` prefix. Tree-sitter PHP exposes the prefix
	// directly as a sibling child of the declaration node.
	var prefix string
	for i := range int(node.ChildCount()) {
		ch := node.Child(i)
		switch ch.Type() {
		case "namespace_name":
			prefix = strings.TrimSpace(string(file.Content[ch.StartByte():ch.EndByte()]))
		case "namespace_use_group":
			return buildUseGroup(ch, file, prefix)
		}
	}

	// Simple/aliased/function/const forms — one or more
	// `namespace_use_clause` children. (PHP allows comma-separated
	// clauses like `use Foo, Bar;` though it's rare.)
	var out []types.EntityRecord
	for i := range int(node.ChildCount()) {
		ch := node.Child(i)
		if ch.Type() != "namespace_use_clause" {
			continue
		}
		fqn := useClauseFQN(ch, file.Content)
		if fqn == "" {
			continue
		}
		out = append(out, useImportRecord(fqn, file.Path))
	}
	return out
}

// buildUseGroup expands a `namespace_use_group` node by joining each
// clause's name onto the shared prefix. Issue #102.
func buildUseGroup(group *sitter.Node, file extractor.FileInput, prefix string) []types.EntityRecord {
	if group == nil || prefix == "" {
		return nil
	}
	var out []types.EntityRecord
	for i := range int(group.ChildCount()) {
		ch := group.Child(i)
		// Tree-sitter PHP uses `namespace_use_group_clause` for grouped
		// imports and `namespace_use_clause` for non-grouped — accept
		// both so the code is robust to grammar revisions.
		if ch.Type() != "namespace_use_group_clause" && ch.Type() != "namespace_use_clause" {
			continue
		}
		tail := useClauseFQN(ch, file.Content)
		if tail == "" {
			continue
		}
		fqn := prefix + "\\" + strings.TrimPrefix(tail, "\\")
		out = append(out, useImportRecord(fqn, file.Path))
	}
	return out
}

// useClauseFQN returns the qualified-name text of a namespace_use_clause,
// stripping any trailing `as Alias` segment. Returns "" when the clause
// has no qualified_name child (defensive — malformed input).
func useClauseFQN(clause *sitter.Node, src []byte) string {
	for i := range int(clause.ChildCount()) {
		ch := clause.Child(i)
		// `qualified_name` / `name` cover plain `use` clauses;
		// `namespace_name` covers `namespace_use_group_clause` children
		// (group imports), which wrap the trailing segment in a
		// namespace_name even when it's a single name.
		switch ch.Type() {
		case "qualified_name", "name", "namespace_name":
			return strings.TrimSpace(string(src[ch.StartByte():ch.EndByte()]))
		}
	}
	// Fallback: take clause text up to " as ".
	raw := strings.TrimSpace(string(src[clause.StartByte():clause.EndByte()]))
	if idx := strings.Index(raw, " as "); idx > 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// useImportRecord builds a SCOPE.Component placeholder + IMPORTS edge
// for a single PHP use-statement target. The component Name is the top
// namespace segment (Symfony, Doctrine, App, ...) — same convention as
// buildNamespace — so emitting the same `use` from multiple files
// idempotently merges to one Component per top-level namespace.
func useImportRecord(fqn, srcPath string) types.EntityRecord {
	// Strip leading '\' (PHP allows fully-qualified `use \Foo\Bar`).
	fqn = strings.TrimPrefix(fqn, "\\")
	top := fqn
	if idx := strings.Index(fqn, "\\"); idx >= 0 {
		top = fqn[:idx]
	}
	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: srcPath,
		Language:   "php",
		Relationships: []types.RelationshipRecord{
			{
				FromID: srcPath,
				ToID:   fqn,
				Kind:   "IMPORTS",
			},
		},
	}
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
