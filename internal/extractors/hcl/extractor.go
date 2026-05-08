// Package hcl implements the HCL/Terraform language extractor for archigraph.
//
// It extracts infrastructure entities from HCL files (Terraform .tf, .tfvars,
// and generic .hcl) using the smacker/go-tree-sitter HCL grammar.
//
// Entity mapping:
//
//	resource block  → Kind="SCOPE.Component", Subtype="resource"
//	data block      → Kind="SCOPE.Component", Subtype="data_source"
//	module block    → Kind="SCOPE.Component", Subtype="module"
//	provider block  → Kind="SCOPE.Component", Subtype="provider"
//	variable block  → Kind="SCOPE.Schema",    Subtype="variable"
//	output block    → Kind="SCOPE.Schema",     Subtype="output"
//	locals block    → Kind="SCOPE.Schema",     Subtype="local" (one per attribute key)
//
// Relationships:
//
//	depends_on = [...] on any block → DEPENDS_ON edges to each referenced resource
//
// OTel span: "indexer.extract.hcl" with attributes language, file_line_count, entity_count.
//
// Registered under both "hcl" and "terraform" language keys via init().
package hcl

import (
	"bytes"
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("hcl", &HCLExtractor{lang: "hcl"})
	extractor.Register("terraform", &HCLExtractor{lang: "terraform"})
}

// HCLExtractor extracts HCL/Terraform entities using tree-sitter.
type HCLExtractor struct {
	lang string
}

// Language implements extractor.Extractor.
func (e *HCLExtractor) Language() string { return e.lang }

// Extract implements extractor.Extractor.
// Returns partial results on node failures — never panics.
func (e *HCLExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor.hcl")
	ctx, span := tracer.Start(ctx, "indexer.extract.hcl")
	defer span.End()

	lang := file.Language
	if lang == "" {
		lang = e.lang
	}

	lineCount := 0
	if len(file.Content) > 0 {
		lineCount = bytes.Count(file.Content, []byte{'\n'}) + 1
	}

	// Fast-path: empty content.
	if len(file.Content) == 0 {
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", 0),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	tree := file.Tree
	if tree == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(hclGrammar())
		var err error
		tree, err = parser.ParseCtx(ctx, nil, file.Content)
		if err != nil {
			return nil, err
		}
	}

	root := tree.RootNode()
	if root == nil {
		span.SetAttributes(
			attribute.String("language", lang),
			attribute.Int("file_line_count", lineCount),
			attribute.Int("entity_count", 0),
		)
		return nil, nil
	}

	var records []types.EntityRecord
	walkBody(root, file.Content, file.Path, lang, &records)

	span.SetAttributes(
		attribute.String("language", lang),
		attribute.Int("file_line_count", lineCount),
		attribute.Int("entity_count", len(records)),
	)
	return records, nil
}

// ----------------------------------------------------------------
// AST walker
// ----------------------------------------------------------------

// walkBody walks a config_file or body node, dispatching top-level blocks.
// Only top-level blocks are dispatched; nested blocks (e.g., statement inside
// data) are not emitted as separate entities.
func walkBody(root *sitter.Node, src []byte, path, lang string, out *[]types.EntityRecord) {
	if root == nil {
		return
	}
	// root may be config_file → body, or body directly.
	var body *sitter.Node
	if root.Type() == "config_file" {
		body = firstChildByType(root, "body")
	} else if root.Type() == "body" {
		body = root
	}
	if body == nil {
		return
	}

	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		child := body.Child(i)
		if child == nil || child.Type() != "block" {
			continue
		}
		if rec, ok := extractBlock(child, src, path, lang); ok {
			*out = append(*out, rec...)
		}
	}
}

// extractBlock handles a single HCL block node and returns 0–N EntityRecords.
// Returns multiple records for locals blocks (one per attribute).
func extractBlock(n *sitter.Node, src []byte, path, lang string) ([]types.EntityRecord, bool) {
	// Block structure: identifier [string_lit ...] block_start body block_end
	blockType := blockTypeIdent(n, src)
	if blockType == "" {
		return nil, false
	}

	start, end := nodeLines(n)

	switch blockType {
	case "resource":
		return extractResourceBlock(n, src, path, lang, start, end)
	case "data":
		return extractDataBlock(n, src, path, lang, start, end)
	case "module":
		return extractModuleBlock(n, src, path, lang, start, end)
	case "variable":
		return extractVariableBlock(n, src, path, lang, start, end)
	case "output":
		return extractOutputBlock(n, src, path, lang, start, end)
	case "provider":
		return extractProviderBlock(n, src, path, lang, start, end)
	case "locals":
		return extractLocalsBlock(n, src, path, lang)
	case "terraform":
		// terraform block → file metadata, not emitted as entity per spec.
		return nil, false
	}
	return nil, false
}

// extractResourceBlock: resource "type" "name" → SCOPE.Component / resource
func extractResourceBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 2 {
		return nil, false
	}
	resourceType := labels[0]
	resourceName := labels[1]
	if resourceName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:          resourceName,
		Kind:          "SCOPE.Component",
		Subtype:       "resource",
		SourceFile:    path,
		StartLine:     start,
		EndLine:       end,
		Language:      lang,
		QualityScore:  0.9,
		QualifiedName: "resource." + resourceType + "." + resourceName,
		Metadata:      map[string]interface{}{"subtype": "resource", "resource_type": resourceType},
	}

	// Extract depends_on relationships.
	body := blockBody(n)
	if body != nil {
		deps := extractDependsOn(body, src, path)
		rec.Relationships = append(rec.Relationships, deps...)
	}

	return []types.EntityRecord{rec}, true
}

// extractDataBlock: data "type" "name" → SCOPE.Component / data_source
func extractDataBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 2 {
		return nil, false
	}
	dataType := labels[0]
	dataName := labels[1]
	if dataName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:          dataName,
		Kind:          "SCOPE.Component",
		Subtype:       "data_source",
		SourceFile:    path,
		StartLine:     start,
		EndLine:       end,
		Language:      lang,
		QualityScore:  0.9,
		QualifiedName: "data." + dataType + "." + dataName,
		Metadata:      map[string]interface{}{"subtype": "data_source", "data_type": dataType},
	}

	body := blockBody(n)
	if body != nil {
		deps := extractDependsOn(body, src, path)
		rec.Relationships = append(rec.Relationships, deps...)
	}

	return []types.EntityRecord{rec}, true
}

// extractModuleBlock: module "name" → SCOPE.Component / module
func extractModuleBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 1 {
		return nil, false
	}
	moduleName := labels[0]
	if moduleName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:         moduleName,
		Kind:         "SCOPE.Component",
		Subtype:      "module",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "module"},
	}

	// Extract source attribute into QualifiedName.
	body := blockBody(n)
	if body != nil {
		if src_ := attributeStringValue(body, "source", src); src_ != "" {
			rec.QualifiedName = src_
			rec.Metadata["source"] = src_
		}
		deps := extractDependsOn(body, src, path)
		rec.Relationships = append(rec.Relationships, deps...)
	}

	return []types.EntityRecord{rec}, true
}

// extractVariableBlock: variable "name" → SCOPE.Schema / variable
func extractVariableBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 1 {
		return nil, false
	}
	varName := labels[0]
	if varName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:         varName,
		Kind:         "SCOPE.Schema",
		Subtype:      "variable",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.8,
		Metadata:     map[string]interface{}{"subtype": "variable"},
	}
	return []types.EntityRecord{rec}, true
}

// extractOutputBlock: output "name" → SCOPE.Schema / output
func extractOutputBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 1 {
		return nil, false
	}
	outName := labels[0]
	if outName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:         outName,
		Kind:         "SCOPE.Schema",
		Subtype:      "output",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.8,
		Metadata:     map[string]interface{}{"subtype": "output"},
	}
	return []types.EntityRecord{rec}, true
}

// extractProviderBlock: provider "name" → SCOPE.Component / provider
func extractProviderBlock(n *sitter.Node, src []byte, path, lang string, start, end int) ([]types.EntityRecord, bool) {
	labels := blockLabels(n, src)
	if len(labels) < 1 {
		return nil, false
	}
	providerName := labels[0]
	if providerName == "" {
		return nil, false
	}

	rec := types.EntityRecord{
		Name:         providerName,
		Kind:         "SCOPE.Component",
		Subtype:      "provider",
		SourceFile:   path,
		StartLine:    start,
		EndLine:      end,
		Language:     lang,
		QualityScore: 0.9,
		Metadata:     map[string]interface{}{"subtype": "provider"},
	}
	return []types.EntityRecord{rec}, true
}

// extractLocalsBlock: locals { key = val ... } → one SCOPE.Schema / local per key
func extractLocalsBlock(n *sitter.Node, src []byte, path, lang string) ([]types.EntityRecord, bool) {
	body := blockBody(n)
	if body == nil {
		return nil, false
	}

	var records []types.EntityRecord
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil {
			continue
		}
		key := nodeText(keyNode, src)
		if key == "" {
			continue
		}
		attrStart, attrEnd := nodeLines(attr)
		records = append(records, types.EntityRecord{
			Name:         key,
			Kind:         "SCOPE.Schema",
			Subtype:      "local",
			SourceFile:   path,
			StartLine:    attrStart,
			EndLine:      attrEnd,
			Language:     lang,
			QualityScore: 0.8,
			Metadata:     map[string]interface{}{"subtype": "local"},
		})
	}

	if len(records) == 0 {
		return nil, false
	}
	return records, true
}

// ----------------------------------------------------------------
// Relationship extraction
// ----------------------------------------------------------------

// extractDependsOn finds a depends_on attribute in a body node and returns
// DEPENDS_ON relationships for each referenced identifier.
//
// depends_on = [resource.type.name, module.name]
// The AST: attribute → identifier("depends_on") / expression → collection_value → tuple → expression → ...
func extractDependsOn(body *sitter.Node, src []byte, fromPath string) []types.RelationshipRecord {
	if body == nil {
		return nil
	}
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil || nodeText(keyNode, src) != "depends_on" {
			continue
		}
		// Found depends_on attribute — extract tuple elements.
		return parseDependsOnTuple(attr, src, fromPath)
	}
	return nil
}

// parseDependsOnTuple walks the depends_on attribute expression tree collecting
// all traversal references like aws_iam_role.lambda_role.
//
// The AST path is: attribute → expression → collection_value → tuple →
// expression* (one per element). Each element expression contains variable_expr
// and get_attr siblings that form the dotted reference.
func parseDependsOnTuple(attr *sitter.Node, src []byte, fromPath string) []types.RelationshipRecord {
	if attr == nil {
		return nil
	}

	var rels []types.RelationshipRecord

	// Find all "expression" nodes that are direct children of a "tuple" node.
	// We walk the full subtree but only emit references for expressions whose
	// parent is a tuple (list element), not the top-level attribute expression.
	var collectTupleExprs func(n *sitter.Node)
	collectTupleExprs = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Type() == "tuple" {
			// Each direct expression child is a list element.
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child != nil && child.Type() == "expression" {
					ref := resolveReference(child, src)
					if ref != "" {
						rels = append(rels, types.RelationshipRecord{
							FromID: fromPath,
							ToID:   ref,
							Kind:   "DEPENDS_ON",
						})
					}
				}
			}
			return // done — no deeper tuples expected in depends_on
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			collectTupleExprs(n.Child(i))
		}
	}

	collectTupleExprs(attr)
	return rels
}

// resolveReference builds a dotted reference string from an expression node.
// Handles: variable_expr followed by get_attr children (e.g., aws_iam_role.lambda_role).
func resolveReference(expr *sitter.Node, src []byte) string {
	if expr == nil {
		return ""
	}
	var parts []string
	count := int(expr.ChildCount())
	for i := 0; i < count; i++ {
		child := expr.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "variable_expr":
			id := firstChildByType(child, "identifier")
			if id != nil {
				parts = append(parts, nodeText(id, src))
			}
		case "get_attr":
			id := firstChildByType(child, "identifier")
			if id != nil {
				parts = append(parts, nodeText(id, src))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ".")
}

// ----------------------------------------------------------------
// HCL AST helpers
// ----------------------------------------------------------------

// blockTypeIdent returns the block type identifier (first identifier child).
func blockTypeIdent(block *sitter.Node, src []byte) string {
	if block == nil {
		return ""
	}
	count := int(block.ChildCount())
	for i := 0; i < count; i++ {
		child := block.Child(i)
		if child != nil && child.Type() == "identifier" {
			return nodeText(child, src)
		}
	}
	return ""
}

// blockLabels returns all string_lit label values for a block (after the type identifier).
// For `resource "aws_lambda_function" "archigraph"` returns ["aws_lambda_function", "archigraph"].
func blockLabels(block *sitter.Node, src []byte) []string {
	if block == nil {
		return nil
	}
	var labels []string
	count := int(block.ChildCount())
	// Skip first identifier (block type), collect all string_lit nodes.
	seenIdent := false
	for i := 0; i < count; i++ {
		child := block.Child(i)
		if child == nil {
			continue
		}
		if child.Type() == "identifier" {
			if !seenIdent {
				seenIdent = true
				continue // skip block type identifier
			}
		}
		if child.Type() == "string_lit" {
			labels = append(labels, stringLitValue(child, src))
		}
	}
	return labels
}

// stringLitValue extracts the text content of a string_lit node (the template_literal child).
func stringLitValue(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.Type() == "template_literal" {
			return nodeText(child, src)
		}
	}
	// Fallback: strip surrounding quotes from raw text.
	raw := nodeText(n, src)
	return strings.Trim(raw, `"`)
}

// blockBody returns the body child of a block node.
func blockBody(block *sitter.Node) *sitter.Node {
	return firstChildByType(block, "body")
}

// attributeStringValue finds an attribute by key name inside a body node
// and returns its string value (template_literal).
func attributeStringValue(body *sitter.Node, key string, src []byte) string {
	if body == nil {
		return ""
	}
	count := int(body.ChildCount())
	for i := 0; i < count; i++ {
		attr := body.Child(i)
		if attr == nil || attr.Type() != "attribute" {
			continue
		}
		keyNode := firstChildByType(attr, "identifier")
		if keyNode == nil || nodeText(keyNode, src) != key {
			continue
		}
		// Found the attribute — extract string value from expression.
		return extractStringFromAttr(attr, src)
	}
	return ""
}

// extractStringFromAttr extracts the first template_literal string value
// from an attribute node's expression subtree.
func extractStringFromAttr(attr *sitter.Node, src []byte) string {
	if attr == nil {
		return ""
	}
	// Walk the expression subtree for template_literal.
	stack := []*sitter.Node{attr}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type() == "template_literal" {
			return nodeText(n, src)
		}
		for i := int(n.ChildCount()) - 1; i >= 0; i-- {
			if child := n.Child(i); child != nil {
				stack = append(stack, child)
			}
		}
	}
	return ""
}

// ----------------------------------------------------------------
// Node helpers
// ----------------------------------------------------------------

// firstChildByType returns the first child of n with the given type.
func firstChildByType(n *sitter.Node, typ string) *sitter.Node {
	if n == nil {
		return nil
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		child := n.Child(i)
		if child != nil && child.Type() == typ {
			return child
		}
	}
	return nil
}

// nodeText returns the source text for a node.
func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	s := n.StartByte()
	e := n.EndByte()
	if int(e) > len(src) {
		e = uint32(len(src))
	}
	return string(src[s:e])
}

// nodeLines returns (startLine, endLine) 1-indexed for a node.
func nodeLines(n *sitter.Node) (int, int) {
	return int(n.StartPoint().Row) + 1, int(n.EndPoint().Row) + 1
}
