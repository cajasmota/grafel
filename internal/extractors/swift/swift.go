// Package swift implements the tree-sitter–based extractor for Swift source files.
//
// Extracted entities:
//   - function_declaration  → Kind="SCOPE.Operation", Subtype="function"
//   - class_declaration     → Kind="SCOPE.Component", Subtype="class"|"struct"|"enum"|"actor"
//     (one shared grammar rule; "actor"/"distributed actor" → subtype="actor", #5417)
//   - struct_declaration    → Kind="SCOPE.Component", Subtype="struct"
//   - protocol_declaration  → Kind="SCOPE.Component", Subtype="protocol"
//   - import_declaration    → IMPORTS relationship
//
// Issue #381 (PORT-RELS-SWIFT) — emits the same three relationship kinds the
// other ported extractors emit:
//
//   - IMPORTS: every `import Module` carries Properties{local_name,
//     source_module, imported_name} matching the Java contract (#120) and
//     the Python schema (#93). For `import Module.Submodule.Symbol` the
//     leaf becomes local_name/imported_name and the prefix is source_module.
//   - CALLS: every `call_expression` inside a function body emits one CALLS
//     edge per unique target. Bare `foo()` → ToID="foo". Navigation
//     `obj.method()` → ToID="method"; when the receiver is a known
//     same-class field with a declared type T the edge carries
//     Properties["receiver_type"]=T (mirrors Java's "T.method" goal in
//     property form). Self-recursion is dropped, keywords (`self`, `super`,
//     `Self`) are filtered.
//   - CONTAINS: class/struct/enum declarations attach one CONTAINS edge per
//     function declared in the body, with the structural-ref shape
//     `scope:operation:method:swift:<file>:<name>` (Format A, #144) so the
//     resolver can disambiguate same-named methods declared in different
//     files.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package swift

import (
	"context"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	if file.TSTree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	// Issue #577 — emit file-level SCOPE.Component (subtype="file") so the
	// cross-repo import linker (#566) can map IMPORTS edges back to the
	// originating repo via the resolver's byName index. Generalises the
	// JS/TS fix from #570/#575.
	entities = append(entities, extractor.FileEntity(file))
	walkNode(file.TSTree.RootNode(), file, &entities)
	// Issue #4854 — in-file base-class EXTENDS for field-membership recursion.
	entities = attachSwiftExtends(entities)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "swift")
	extractor.TagEntitiesLanguage(entities, "swift")
	return entities, nil
}

// walkNode performs a depth-first traversal of the CST.
//
// Issue #381: class/struct/enum declarations attach a CONTAINS edge per
// function declared inside the body, and every function body is scanned for
// call_expression nodes that yield CALLS edges. import_declaration emits
// IMPORTS with the property contract.
func walkNode(node ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "class_declaration":
		// In tree-sitter-swift the node type "class_declaration" is used for
		// class, struct, enum AND actor declarations — they share one grammar
		// rule, distinguished by the leading declaration keyword.
		//
		// Issue #5417 (C3(b)) — `actor` / `distributed actor` are first-class
		// Swift concurrency components. The fresher grammar emits them as
		// `class_declaration` with an `"actor"` keyword child (plain actor) or
		// a leading `modifiers > "distributed"` then `"actor"` (distributed
		// actor). We surface them as SCOPE.Component subtype="actor",
		// mirroring class/struct/enum, so concurrency components stop
		// vanishing into the generic "class" bucket.
		subtype := swiftDeclSubtype(node, file.Content)
		rec, ok := buildComponent(node, file, subtype)
		if !ok {
			for i := range node.ChildCount() {
				walkNode(node.Child(int(i)), file, out)
			}
			return
		}
		classIdx := len(*out)
		*out = append(*out, rec)
		// Issue #4913 — Type System: for an `enum` declaration also emit a
		// SCOPE.Enum value-set node carrying its `case` members (parity with
		// the dart/python/ts/java enum value-sets) IN ADDITION to the
		// SCOPE.Component above. The Component models the nominal type; the
		// value-set answers "what cases can this take?".
		if subtype == "enum" {
			if ve, ok := buildEnumValueSet(node, file); ok {
				*out = append(*out, ve)
			}
		}
		// CONTAINS: every function declared in the class body.
		body := findClassBody(node)
		fieldTypes := collectFieldTypes(body, file.Content)
		if body != nil {
			before := len(*out)
			// Walk children of body collecting nested entities. Pass field
			// types through walkBody so call extraction inside same-class
			// functions can resolve receivers to declared types.
			walkBody(body, file, fieldTypes, rec.Name, out)
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				toID := extractor.BuildOperationStructuralRef("swift", file.Path, child.Name)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		// Issue #4854 — general field membership: one SCOPE.Schema/field per
		// stored property (let/var) + a type→field CONTAINS edge so a plain
		// Swift data struct/class has field children.
		fieldEnts, baseNames := emitSwiftFieldMembers(node, body, file.Content, rec.Name, file.Path)
		for _, fe := range fieldEnts {
			toID := extractor.BuildSchemaFieldStructuralRef("swift", file.Path, fe.Name)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
		*out = append(*out, fieldEnts...)
		if len(baseNames) > 0 {
			if (*out)[classIdx].Metadata == nil {
				(*out)[classIdx].Metadata = map[string]interface{}{}
			}
			(*out)[classIdx].Metadata["base_candidates"] = baseNames
		}
		return

	case "protocol_declaration":
		if rec, ok := buildComponent(node, file, "protocol"); ok {
			*out = append(*out, rec)
		}
		// Protocols may declare functions; recurse so they appear as
		// SCOPE.Operation entities (no CONTAINS by parity choice — they
		// are abstract requirements without bodies).
		for i := range node.ChildCount() {
			walkNode(node.Child(int(i)), file, out)
		}
		return

	case "function_declaration":
		// Top-level (file-scoped) function with no enclosing class context.
		if rec, ok := buildOperation(node, file, "function"); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name, nil)...)
			*out = append(*out, rec)
		}
		return

	case "import_declaration":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
		return

	case "typealias_declaration":
		// Issue #4913 — Type System: `typealias Name = <type>` →
		// SCOPE.Schema(subtype=type_alias), parity with the python/rust/go/
		// dart type_alias shape. Supersedes the loose vapor-only
		// reSwiftTypealias→Component v1 with a tree-sitter, language-wide,
		// value-set-grade Schema node carrying the aliased type_body.
		if rec, ok := buildTypeAlias(node, file); ok {
			*out = append(*out, rec)
		}
		return
	}

	for i := range node.ChildCount() {
		walkNode(node.Child(int(i)), file, out)
	}
}

// walkBody recurses into a class/struct/enum body, emitting Operation
// entities for each function and propagating field types so CALLS edges
// inside method bodies can carry a receiver_type property.
func walkBody(node ts.Node, file extractor.FileInput, fieldTypes map[string]string, _ string, out *[]types.EntityRecord) {
	if node == nil {
		return
	}
	if node.Type() == "function_declaration" {
		if rec, ok := buildOperation(node, file, "function"); ok {
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(findFunctionBody(node), file.Content, rec.Name, fieldTypes)...)
			*out = append(*out, rec)
		}
		return
	}
	for i := range node.ChildCount() {
		walkBody(node.Child(int(i)), file, fieldTypes, "", out)
	}
}

// swiftDeclSubtype classifies a `class_declaration` node (the shared rule for
// class / struct / enum / actor) by its leading declaration keyword.
//
// The keyword is an unnamed child of the declaration. It is usually a direct
// child, but a leading `modifiers` node (e.g. `final class`, `distributed
// actor`, `public struct`) may precede it. We scan the direct children for the
// first recognised keyword token; for the `distributed actor` shape the
// `distributed` modifier lives inside a `modifiers` node and the `actor`
// keyword follows it as a direct child, so a direct-child scan still finds it.
// Defaults to "class".
func swiftDeclSubtype(node ts.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch == nil || ch.IsNamed() {
			// Keywords (class/struct/enum/actor) are anonymous tokens; named
			// children (modifiers, type_identifier, bodies) are not keywords.
			continue
		}
		switch string(src[ch.StartByte():ch.EndByte()]) {
		case "struct":
			return "struct"
		case "enum":
			return "enum"
		case "actor":
			return "actor"
		case "class":
			return "class"
		}
	}
	return "class"
}

// findClassBody returns the body child of a class/struct/enum declaration.
func findClassBody(node ts.Node) ts.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		t := ch.Type()
		if t == "class_body" || t == "enum_class_body" || t == "protocol_body" {
			return ch
		}
	}
	return nil
}

// findFunctionBody returns the function_body child of a function_declaration.
func findFunctionBody(node ts.Node) ts.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "function_body" {
			return ch
		}
	}
	return nil
}

// collectFieldTypes scans a class body's property_declaration children and
// returns a map of property name → declared type name. Only single-pattern
// properties with an explicit type_annotation participate. Used to attach
// receiver_type to CALLS edges where the receiver is a known same-class
// field.
func collectFieldTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch.Type() != "property_declaration" {
			continue
		}
		var name, typ string
		for j := 0; j < int(ch.ChildCount()); j++ {
			c := ch.Child(j)
			switch c.Type() {
			case "pattern":
				// First simple_identifier under the pattern is the name.
				for k := 0; k < int(c.ChildCount()); k++ {
					if c.Child(k).Type() == "simple_identifier" {
						name = string(src[c.Child(k).StartByte():c.Child(k).EndByte()])
						break
					}
				}
			case "type_annotation":
				// Walk descendants for the first type_identifier.
				typ = firstDescendantText(c, src, "type_identifier")
			}
		}
		if name != "" && typ != "" {
			out[name] = typ
		}
	}
	return out
}

// firstDescendantText returns the source text of the first descendant of
// node whose type is `kind`, or "" when none exists.
func firstDescendantText(node ts.Node, src []byte, kind string) string {
	if node == nil {
		return ""
	}
	if node.Type() == kind {
		return string(src[node.StartByte():node.EndByte()])
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		if t := firstDescendantText(node.Child(i), src, kind); t != "" {
			return t
		}
	}
	return ""
}

// swiftCallStop lists Swift keywords / self-references that the parser
// surfaces as call_expression heads but are NOT real call targets. Mirrors
// the Kotlin extractor's keyword filter (#106).
var swiftCallStop = map[string]bool{
	"self":  true,
	"Self":  true,
	"super": true,
}

// swiftStatementKeywords are Swift keywords the tree-sitter grammar surfaces
// as `call_expression` heads when they take a trailing closure (e.g.
// `defer { ... }`, `repeat { ... }`). They are statements, not invocations,
// and emitting CALLS edges for them is pure noise (#499).
var swiftStatementKeywords = map[string]bool{
	"defer":  true,
	"repeat": true,
	"do":     true,
}

// swiftBareInitFiltered drops `init` when it appears as a bare (no receiver)
// call target. `Type.init(...)` is a legitimate explicit initializer call and
// must be preserved with receiver_type, but a bare `init()` token in a body
// can only originate from a non-call context the grammar mis-shapes — never
// from real user code (#499).
var swiftBareInitFiltered = map[string]bool{
	"init": true,
}

// extractCallRelationships returns one CALLS RelationshipRecord per unique
// call_expression descendant of body. The target name is the trailing
// simple_identifier of the call's expression. FromID is left empty so
// buildDocument substitutes the caller's entity ID at emit time.
//
// When the receiver is a known same-class field, the edge carries
// Properties["receiver_type"]=<DeclaredType>. Self-recursion is dropped to
// match Python/Go extractor dedup semantics.
func extractCallRelationships(body ts.Node, src []byte, callerName string, fieldTypes map[string]string) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	calls := findAllNodes(body, "call_expression")
	if len(calls) == 0 {
		return nil
	}
	type key struct {
		target, recv string
	}
	seen := make(map[key]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target, recvRoot := swiftCallTarget(call, src)
		if target == "" || target == callerName {
			continue
		}
		if swiftCallStop[target] {
			continue
		}
		// #499 — drop statement keywords the grammar shapes as
		// call_expression (e.g. `defer { ... }`). These are never real
		// invocations regardless of receiver.
		if swiftStatementKeywords[target] {
			continue
		}
		// #499 — drop bare `init` (no receiver). `Type.init(...)` is a
		// real explicit-initializer call and is preserved because
		// recvRoot != "" in that shape.
		if recvRoot == "" && swiftBareInitFiltered[target] {
			continue
		}
		recvType := ""
		if recvRoot != "" && fieldTypes != nil {
			recvType = fieldTypes[recvRoot]
		}
		k := key{target, recvType}
		if seen[k] {
			continue
		}
		seen[k] = true
		rel := types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(int(call.StartPoint().Row) + 1),
			},
		}
		if recvType != "" {
			rel.Properties["receiver_type"] = recvType
		}
		rels = append(rels, rel)
	}
	return rels
}

// swiftCallTarget resolves the callee name from a call_expression node and
// returns the receiver root identifier (or "" when the call is bare).
//
// Tree-sitter-swift shapes a call as `<expression> <call_suffix>` where
// `<call_suffix>` carries the parenthesized argument list / trailing
// closure that distinguishes a real method invocation from a plain
// property reference. We require a `call_suffix` sibling before emitting
// any CALLS edge.
//
// For a `simple_identifier` head (`foo()`) we return that identifier.
// For a `navigation_expression` head (`a.b.c()`) the callee is the
// rightmost `simple_identifier` of the trailing `navigation_suffix`, NOT
// the leftmost descendant. The receiver root is the leftmost
// simple_identifier of the chain — used to look up a declared field type.
func swiftCallTarget(call ts.Node, src []byte) (target, receiverRoot string) {
	if !hasCallSuffix(call) {
		return "", ""
	}
	if call.ChildCount() == 0 {
		return "", ""
	}
	first := call.Child(0)
	switch first.Type() {
	case "simple_identifier":
		return string(src[first.StartByte():first.EndByte()]), ""
	case "navigation_expression":
		// Trailing navigation_suffix gives the method name.
		var lastSuffix ts.Node
		for i := 0; i < int(first.ChildCount()); i++ {
			ch := first.Child(i)
			if ch.Type() == "navigation_suffix" {
				lastSuffix = ch
			}
		}
		if lastSuffix == nil {
			return "", ""
		}
		var method string
		for i := int(lastSuffix.ChildCount()) - 1; i >= 0; i-- {
			ch := lastSuffix.Child(i)
			if ch.Type() == "simple_identifier" {
				method = string(src[ch.StartByte():ch.EndByte()])
				break
			}
		}
		if method == "" {
			return "", ""
		}
		// Receiver root: descend through nested navigation_expression
		// children until we hit the leftmost simple_identifier.
		root := first
		for {
			if root.ChildCount() == 0 {
				break
			}
			c := root.Child(0)
			if c.Type() == "navigation_expression" {
				root = c
				continue
			}
			if c.Type() == "simple_identifier" {
				return method, string(src[c.StartByte():c.EndByte()])
			}
			break
		}
		return method, ""
	}
	return "", ""
}

// hasCallSuffix reports whether a call_expression node has a `call_suffix`
// child — its presence (parentheses or trailing closure) is what makes the
// node a real invocation rather than a bare property reference.
func hasCallSuffix(call ts.Node) bool {
	for i := 0; i < int(call.ChildCount()); i++ {
		if call.Child(i).Type() == "call_suffix" {
			return true
		}
	}
	return false
}

// findAllNodes returns every descendant of root whose Type() is in kinds.
func findAllNodes(root ts.Node, kinds ...string) []ts.Node {
	if root == nil {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []ts.Node
	stack := []ts.Node{root}
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

// buildComponent creates a SCOPE.Component entity.
func buildComponent(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	// Python signature format: "type Name" (keyword + name only)
	sig := "type " + name
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
func buildOperation(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := extractName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
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
func methodSignature(src []byte, node ts.Node) string {
	name := extractName(node, src)
	if name == "" {
		return ""
	}
	raw := firstLine(src, node)
	returnType := ""
	if idx := strings.Index(raw, "->"); idx >= 0 {
		rt := strings.TrimSpace(raw[idx+2:])
		if braceIdx := strings.Index(rt, "{"); braceIdx >= 0 {
			rt = strings.TrimSpace(rt[:braceIdx])
		}
		returnType = " -> " + rt
	}
	return "func " + name + "()" + returnType
}

// buildImport creates a SCOPE.Component entity carrying an IMPORTS
// relationship. Issue #381 — the IMPORTS edge follows the same Properties
// contract Java emits (#120) and the Python schema (#93):
//
//	Properties["local_name"]    — the leaf identifier introduced by the
//	                              import. For `import Foundation` this is
//	                              "Foundation"; for `import os.log.Logger`
//	                              this is "Logger".
//	Properties["source_module"] — the dotted prefix. For `import
//	                              Foundation` this is "Foundation"; for
//	                              `import os.log.Logger` this is "os.log".
//	Properties["imported_name"] — equal to local_name.
//
// Issue #492 — the import-carrier entity must NOT collide with real
// SwiftPM target/product names (e.g. an `import App` in a SwiftPM
// package whose product is also named `App` previously produced two
// entities both named `App` and tripped the bug-resolver on every
// reference). Two layered defenses:
//
//  1. Subtype="module" — mirrors the Python convention so the
//     cross-file resolver's pass-2 (module,name) reverse index skips
//     this entity entirely (see internal/resolve/imports.go).
//  2. Name is namespaced as `<file>::import::<module>` so that even if
//     a downstream consumer ignores Subtype the carrier name cannot
//     match a bare Swift type/target identifier.
func buildImport(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw := extractImportPath(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	leaf := raw
	mod := raw
	if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
		leaf = raw[dot+1:]
		mod = raw[:dot]
	}
	props := map[string]string{
		"local_name":    leaf,
		"source_module": mod,
		"imported_name": leaf,
	}

	return types.EntityRecord{
		Name:       file.Path + "::import::" + raw,
		Kind:       "SCOPE.Component",
		Subtype:    "module",
		SourceFile: file.Path,
		Language:   "swift",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     file.Path,
				ToID:       raw,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}, true
}

// extractImportPath extracts the module path from an import_declaration. The
// path may be a single identifier (`Foundation`) or a dotted chain
// (`os.log.Logger`). Submodule access is rare in Swift but supported.
//
// #499 — skip the `modifiers` / `attribute` subtrees. tree-sitter-swift
// shapes import attributes like `@_documentation(visibility: internal)`,
// `@_exported`, `@preconcurrency`, `@_implementationOnly`, `@testable` as
// children of a `modifiers` node that lives inside `import_declaration`.
// Each `attribute` carries `type_identifier` / `simple_identifier`
// descendants for the attribute name and its arguments, which previously
// got joined into synthetic dotted import paths
// (e.g. `_documentation.visibility.internal._exported.Foundation`).
// Detection is by tree-sitter node type — not by hardcoded attribute name —
// so any current or future Swift attribute spelling is skipped.
func extractImportPath(node ts.Node, src []byte) string {
	// Collect all identifier-like child segments and join with '.'.
	var parts []string
	var collect func(n ts.Node)
	collect = func(n ts.Node) {
		if n == nil {
			return
		}
		t := n.Type()
		// #499 — never descend into attribute / modifier subtrees.
		if t == "modifiers" || t == "attribute" || t == "attributes" {
			return
		}
		if t == "simple_identifier" || t == "type_identifier" {
			parts = append(parts, string(src[n.StartByte():n.EndByte()]))
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			collect(n.Child(i))
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		t := ch.Type()
		if t == "import" {
			continue
		}
		// #499 — top-level skip; the recursive guard above is a
		// belt-and-braces defense for any nested shape.
		if t == "modifiers" || t == "attribute" || t == "attributes" {
			continue
		}
		collect(ch)
	}
	if len(parts) > 0 {
		return strings.Join(parts, ".")
	}
	// Fallback — strip any leading attribute tokens before the literal
	// `import` keyword so the textual fallback also yields a clean module
	// path when the AST shape is unexpected.
	full := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	if idx := strings.Index(full, "import "); idx >= 0 {
		full = full[idx+len("import "):]
	}
	return strings.TrimSpace(full)
}

// extractName finds the name of a declaration node.
func extractName(node ts.Node, src []byte) string {
	if child := node.ChildByFieldName("name"); child != nil {
		return string(src[child.StartByte():child.EndByte()])
	}
	keywords := map[string]bool{
		"class": true, "struct": true, "protocol": true, "func": true,
		"import": true, "open": true, "public": true, "internal": true,
		"private": true, "final": true, "override": true, "enum": true,
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
func firstLine(src []byte, node ts.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}
