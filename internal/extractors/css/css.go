// Package css implements the tree-sitter–based extractor for CSS/SCSS/Less source files.
//
// Extracted entities (plain CSS via tree-sitter):
//   - rule_set (selectors)     → Kind="SCOPE.Stylesheet", Subtype="selector"
//   - keyframes_statement      → Kind="SCOPE.Stylesheet", Subtype="keyframe"
//   - at_rule (@mixin/@function) → Kind="SCOPE.Stylesheet", Subtype="mixin"
//   - import_statement (@import) → Kind="SCOPE.Component",  Subtype="import"
//   - CSS custom properties (--var) → Kind="SCOPE.Stylesheet", Subtype="variable"
//
// Extracted entities (SCSS/Less via regex —):
//   - SCSS $variable: value    → Kind="SCOPE.Component", Subtype="variable"
//   - SCSS @mixin name(params) → Kind="SCOPE.Component", Subtype="mixin"
//   - SCSS @function name      → Kind="SCOPE.Component", Subtype="function"
//   - SCSS/Less @import "x"    → Kind="SCOPE.Component", Subtype="import"
//   - Less @variable: value    → Kind="SCOPE.Component", Subtype="variable"
//   - Less .mixin(params) {    → Kind="SCOPE.Component", Subtype="mixin"
//
// Emitted relationships (per issue #383 PORT-RELS-CSS):
//
//	IMPORTS  — one edge per @import directive (plain CSS, SCSS, Less),
//	           attached to the @import entity. CALLS and CONTAINS are not
//	           applicable to the CSS family and are pinned out by tests.
//
// Uses the css grammar from smacker/go-tree-sitter for plain CSS.
// SCSS/Less use regex because go-tree-sitter has no dedicated SCSS/Less grammar.
// Registers itself via init() and is imported by registry_gen.go.
package css

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("css", &Extractor{})
}

// Extractor implements extractor.Extractor for CSS/SCSS.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "css" }

// mixinKeywords are at-rule keywords that map to mixin-like constructs.
var mixinKeywords = map[string]bool{
	"@mixin":    true,
	"@function": true,
	"@include":  true,
}

// Extract dispatches to CSS (tree-sitter), SCSS, or Less extraction based on
// the file extension embedded in file.Path.
//
// For.scss and.sass files: regex-based SCSS extraction.
// For.less files: regex-based Less extraction.
// For all other .css files: tree-sitter CSS extraction (unchanged).
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}

	ext := strings.ToLower(filepath.Ext(file.Path))

	switch ext {
	case ".scss", ".sass":
		var entities []types.EntityRecord
		ExtractSCSS(ctx, file, &entities)
		lang := "scss"
		if ext == ".sass" {
			lang = "sass"
		}
		extractor.TagRelationshipsLanguage(entities, lang)
		extractor.TagEntitiesLanguage(entities, lang)
		return entities, nil
	case ".less":
		var entities []types.EntityRecord
		ExtractLess(ctx, file, &entities)
		extractor.TagRelationshipsLanguage(entities, "less")
		extractor.TagEntitiesLanguage(entities, "less")
		return entities, nil
	default:
		// Plain CSS: tree-sitter parse required.
		if file.Tree == nil {
			return nil, nil
		}
		var entities []types.EntityRecord
		root := file.Tree.RootNode()
		extractCSS(root, file, &entities)
		extractor.TagRelationshipsLanguage(entities, "css")
		extractor.TagEntitiesLanguage(entities, "css")
		return entities, nil
	}
}

// extractCSS traverses the CSS tree collecting all entity types.
func extractCSS(root *sitter.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	// Collect selectors, keyframes, at_rules in one pass.
	seenVars := make(map[string]bool)
	walkCSS(root, file, seenVars, out)
}

func walkCSS(node *sitter.Node, file extractor.FileInput, seenVars map[string]bool, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "rule_set":
		if rec, ok := buildSelector(node, file); ok {
			*out = append(*out, rec)
		}
	case "keyframes_statement":
		if rec, ok := buildKeyframe(node, file); ok {
			*out = append(*out, rec)
		}
	case "at_rule":
		if rec, ok := buildAtRule(node, file); ok {
			*out = append(*out, rec)
		}
	case "declaration":
		if rec, ok := buildVariable(node, file, seenVars); ok {
			*out = append(*out, rec)
		}
	case "import_statement":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
	}

	for i := range node.ChildCount() {
		walkCSS(node.Child(int(i)), file, seenVars, out)
	}
}

func nodeText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func childByType(node *sitter.Node, types ...string) *sitter.Node {
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && typeSet[ch.Type()] {
			return ch
		}
	}
	return nil
}

func buildSelector(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	selectorsNode := childByType(node, "selectors")
	if selectorsNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(selectorsNode, file.Content))
	if len(name) > 80 {
		name = name[:77] + "..."
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Stylesheet",
		Subtype:            "selector",
		SourceFile:         file.Path,
		Language:           "css",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          name,
		EnrichmentRequired: false,
	}, true
}

func buildKeyframe(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	nameNode := childByType(node, "keyframes_name")
	name := "?"
	if nameNode != nil {
		name = strings.TrimSpace(nodeText(nameNode, file.Content))
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Stylesheet",
		Subtype:            "keyframe",
		SourceFile:         file.Path,
		Language:           "css",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "@keyframes " + name,
		EnrichmentRequired: false,
	}, true
}

func buildAtRule(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	kwNode := childByType(node, "at_keyword")
	if kwNode == nil {
		return types.EntityRecord{}, false
	}
	kw := strings.ToLower(strings.TrimSpace(nodeText(kwNode, file.Content)))
	if !mixinKeywords[kw] {
		return types.EntityRecord{}, false
	}
	// Name is in keyword_query child.
	nameNode := childByType(node, "keyword_query")
	name := "?"
	if nameNode != nil {
		name = strings.TrimSpace(nodeText(nameNode, file.Content))
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Stylesheet",
		Subtype:            "mixin",
		SourceFile:         file.Path,
		Language:           "css",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          kw + " " + name,
		EnrichmentRequired: false,
	}, true
}

// buildImport handles tree-sitter import_statement nodes for plain CSS.
// Both forms are supported:
//
//	@import "foo.css";        — module ref is the string_value child
//	@import url("foo.css");   — module ref is the string_value inside the
//	                            call_expression's arguments
//
// Media queries trailing the directive (e.g. `screen`, `print`) are
// ignored — they don't change the import target.
func buildImport(node *sitter.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	module := importModuleRef(node, file.Content)
	if module == "" {
		return types.EntityRecord{}, false
	}
	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1
	return types.EntityRecord{
		Name:       module,
		Kind:       "SCOPE.Component",
		Subtype:    "import",
		SourceFile: file.Path,
		Language:   "css",
		StartLine:  startLine,
		EndLine:    endLine,
		Signature:  "@import " + module,
		Relationships: []types.RelationshipRecord{
			buildImportRel(file.Path, module),
		},
		EnrichmentRequired: false,
	}, true
}

// importModuleRef extracts the imported module path from an import_statement
// node, handling both bare-string and url(...) forms. Returns "" if no
// module ref can be located.
func importModuleRef(node *sitter.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "string_value":
			return unquoteStringValue(ch, src)
		case "call_expression":
			args := childByType(ch, "arguments")
			if args == nil {
				continue
			}
			if str := childByType(args, "string_value"); str != nil {
				return unquoteStringValue(str, src)
			}
		}
	}
	return ""
}

// unquoteStringValue strips the surrounding "" or ” quote tokens from a
// tree-sitter string_value node and returns the inner text.
func unquoteStringValue(node *sitter.Node, src []byte) string {
	raw := nodeText(node, src)
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		first, last := raw[0], raw[len(raw)-1]
		if (first == '"' || first == '\'') && first == last {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func buildVariable(node *sitter.Node, file extractor.FileInput, seen map[string]bool) (types.EntityRecord, bool) {
	propNode := childByType(node, "property_name")
	if propNode == nil {
		return types.EntityRecord{}, false
	}
	prop := nodeText(propNode, file.Content)
	if !strings.HasPrefix(prop, "--") {
		return types.EntityRecord{}, false
	}
	if seen[prop] {
		return types.EntityRecord{}, false
	}
	seen[prop] = true
	return types.EntityRecord{
		Name:               prop,
		Kind:               "SCOPE.Stylesheet",
		Subtype:            "variable",
		SourceFile:         file.Path,
		Language:           "css",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          prop,
		EnrichmentRequired: false,
	}, true
}
