// Package csharp implements the tree-sitter–based extractor for C# source files.
//
// Extracted entities:
//   - method_declaration      → Kind="SCOPE.Operation", Subtype="method"
//   - constructor_declaration → Kind="SCOPE.Operation", Subtype="constructor"
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - interface_declaration   → Kind="SCOPE.Component", Subtype="interface"
//   - struct_declaration      → Kind="SCOPE.Component", Subtype="struct"
//   - record_declaration      → Kind="SCOPE.Component", Subtype="type"
//   - enum_declaration        → Kind="SCOPE.Schema",    Subtype="enum"
//   - using_directive         → IMPORTS relationship
//
// Issue #368 — relationship parity. The extractor emits:
//
//   - IMPORTS edges with the property contract Python/Java emit
//     (local_name, source_module, imported_name, wildcard) so the
//     cross-file resolver can build a per-file binding table for C#.
//
//   - CALLS edges for invocation_expression / object_creation_expression
//     descendants of every method/constructor body. When the receiver of
//     a member-access invocation is a known field, parameter, or local
//     declared with a typed leaf, the edge target is the dotted form
//     "<ReceiverType>.<method>" (mirroring Java #120). PascalCase bare
//     receivers (`Math.Max`, `String.Format`) are kept dotted as a
//     static-call shape so the resolver's byKind/byName can rebind
//     cross-file. Bare-name calls fall back to the leaf method name.
//
//   - CONTAINS edges from a class/interface/struct to each of its
//     methods/constructors via BuildOperationStructuralRef (Format A).
//
// Issue #65 parity: methods declared inside a class/interface/struct body
// are emitted with Name="<EnclosingType>.<member>" so two sibling types
// declaring same-named methods produce distinct ComputeID values.
//
// The extractor registers itself via init() and is auto-imported by the
// generated registry_gen.go.
package csharp

import (
	"context"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("csharp", &Extractor{})
}

// Extractor implements extractor.Extractor for C#.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "csharp" }

// Extract walks the tree-sitter CST and returns entity records for the C# file.
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
	root := file.TSTree.RootNode()
	imports := collectImportNames(root, file.Content)
	// Issue #4374 — per-file cross-namespace context (namespaces, usings,
	// aliases, `using static` types) used to bind qualified cross-namespace
	// calls to a concrete (namespace, type, leaf) for the resolver.
	cross := buildCrossCtx(root, file.Content)
	walk(root, file, "", "", nil, imports, cross, &entities)
	// Issue #3641 (epic #3625) — config-key consumption edges
	// (Configuration["X"] / GetValue / GetConnectionString /
	// Environment.GetEnvironmentVariable) → shared SCOPE.Config config_key nodes.
	emitConfigConsumerEdges(root, file.Content, &entities)
	// Epic #3628 — error-flow topology: typed `throw new` / `catch` shapes →
	// THROWS / CATCHES edges to a shared SCOPE.ExceptionType node, matching the
	// Java / Python / Go / JS flagship error_flow model.
	emitExceptionFlowEdges(root, file.Content, &entities)
	// Issue #4854 — in-file base-class EXTENDS for field-membership recursion.
	entities = attachCsharpExtends(entities)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "csharp")
	extractor.TagEntitiesLanguage(entities, "csharp")
	return entities, nil
}

// classCtx carries the resolution context for cross-file receiver
// binding. fields maps a declared field/property name to its declared
// leaf-type identifier. Per-class only; nested classes rebuild the map.
type classCtx struct {
	fields map[string]string
}

// walk performs a depth-first traversal of the CST, collecting entities.
//
// ns carries the enclosing C# namespace (issue #4374). It is captured on
// namespace_declaration / file_scoped_namespace_declaration and stamped onto
// every Component/Operation/Schema entity so the resolver can build a
// namespace-keyed member index for cross-namespace CALLS binding.
func walk(
	node ts.Node,
	file extractor.FileInput,
	parentType string,
	ns string,
	cc *classCtx,
	imports map[string]bool,
	cross *csharpCrossCtx,
	out *[]types.EntityRecord,
) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "namespace_declaration", "file_scoped_namespace_declaration":
		childNS := ns
		if nf := node.ChildByFieldName("name"); nf != nil {
			if v := strings.TrimSpace(nodeText(nf, file.Content)); v != "" {
				childNS = v
			}
		}
		for i := range node.ChildCount() {
			walk(node.Child(int(i)), file, parentType, childNS, cc, imports, cross, out)
		}
		return
	}

	switch node.Type() {
	case "class_declaration", "interface_declaration", "struct_declaration", "record_declaration":
		subtype := "class"
		switch node.Type() {
		case "interface_declaration":
			subtype = "interface"
		case "struct_declaration":
			subtype = "struct"
		case "record_declaration":
			subtype = "type"
		}
		rec, ok := buildComponent(node, file, subtype)
		if !ok {
			for i := range node.ChildCount() {
				walk(node.Child(int(i)), file, parentType, ns, cc, imports, cross, out)
			}
			return
		}
		stampNamespace(&rec, ns)
		classIdx := len(*out)
		*out = append(*out, rec)
		body := findTypeBody(node)
		if body != nil {
			// #4428: emit value-set nodes for class-level constant COLLECTIONS
			// (static-readonly Dictionary const maps + grouped string consts).
			// Append-only: never replaces the field entities the walk emits.
			emitConstCollectionsForClass(body, file, rec.Name, out)
			localCtx := &classCtx{fields: collectFieldTypes(body, file.Content)}
			before := len(*out)
			for i := range body.ChildCount() {
				walk(body.Child(int(i)), file, rec.Name, ns, localCtx, imports, cross, out)
			}
			after := len(*out)
			for k := before; k < after; k++ {
				child := &(*out)[k]
				if child.Kind != "SCOPE.Operation" {
					continue
				}
				toID := extractor.BuildOperationStructuralRef("csharp", file.Path, child.Name)
				(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
					types.RelationshipRecord{
						ToID: toID,
						Kind: "CONTAINS",
					})
			}
		}
		// Issue #4854 — general field membership: one SCOPE.Schema/field per
		// property / public field / record positional parameter, plus a
		// class→field CONTAINS edge so a plain data class has field children
		// (dedups by Name with the endpoint-bound DTO members in #4715).
		fieldEnts, baseNames := emitFieldMembers(node, body, file.Content, rec.Name, file.Path)
		for _, fe := range fieldEnts {
			toID := extractor.BuildSchemaFieldStructuralRef("csharp", file.Path, fe.Name)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
		*out = append(*out, fieldEnts...)
		// Stash base-type candidates for the in-file EXTENDS post-pass.
		if len(baseNames) > 0 {
			if (*out)[classIdx].Metadata == nil {
				(*out)[classIdx].Metadata = map[string]interface{}{}
			}
			(*out)[classIdx].Metadata["base_candidates"] = baseNames
		}
		return

	case "enum_declaration":
		if rec, ok := buildEnumEntity(node, file); ok {
			stampNamespace(&rec, ns)
			*out = append(*out, rec)
		}
		// Value-carrying SCOPE.Enum value-set node (data-model, epic #3628).
		if vs, ok := buildEnumValueSet(node, file); ok {
			*out = append(*out, vs)
		}
		return

	case "method_declaration":
		if rec, ok := buildOperation(node, file, "method", parentType); ok {
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			stampNamespace(&rec, ns)
			paramTypes := collectParamTypes(node, file.Content)
			body := node.ChildByFieldName("body")
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, selfName, cc, paramTypes, imports, cross)...)
			*out = append(*out, rec)
		}
		return

	case "constructor_declaration":
		if rec, ok := buildOperation(node, file, "constructor", parentType); ok {
			selfName := rec.Name
			if nameNode := node.ChildByFieldName("name"); nameNode != nil {
				selfName = nodeText(nameNode, file.Content)
			}
			stampNamespace(&rec, ns)
			paramTypes := collectParamTypes(node, file.Content)
			body := node.ChildByFieldName("body")
			rec.Relationships = append(rec.Relationships,
				extractCallRelationships(body, file.Content, selfName, cc, paramTypes, imports, cross)...)
			*out = append(*out, rec)
		}
		return

	case "using_directive":
		if rec, ok := buildImport(node, file); ok {
			*out = append(*out, rec)
		}
		return
	}

	// Default recursion. parentType / cc do NOT propagate through
	// unrelated nodes (e.g. method bodies) — methods nested inside a
	// method body are emitted bare. ns (the enclosing namespace) DOES
	// propagate so nested types still record their namespace (#4374).
	for i := range node.ChildCount() {
		walk(node.Child(int(i)), file, "", ns, nil, imports, cross, out)
	}
}

// stampNamespace records the enclosing C# namespace on an entity so the
// resolver can build a namespace-keyed member index for cross-namespace CALLS
// binding (#4374). No-op for the global (file-root) namespace.
func stampNamespace(rec *types.EntityRecord, ns string) {
	if ns == "" {
		return
	}
	if rec.Properties == nil {
		rec.Properties = map[string]string{}
	}
	rec.Properties["csharp_namespace"] = ns
}

// findTypeBody returns the declaration_list child of a class/interface/
// struct declaration, or nil when the type has no body.
func findTypeBody(node ts.Node) ts.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == "declaration_list" {
			return ch
		}
	}
	return nil
}

// buildComponent creates a SCOPE.Component entity for class/interface/struct declarations.
func buildComponent(node ts.Node, file extractor.FileInput, subtype string) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Component",
		Subtype:            subtype,
		SourceFile:         file.Path,
		Language:           "csharp",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildClassSignature(node, file.Content),
		EnrichmentRequired: false,
	}, true
}

// buildEnumEntity creates a SCOPE.Schema entity (subtype="enum") for enum declarations.
// Enum member names are collected and stored in the Signature field as a comma-separated
// list so the graph carries the full member set without additional enrichment.
func buildEnumEntity(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}

	// Collect member names from enum_member_declaration_list children.
	var members []string
	for _, memberList := range findAllNodes(node, "enum_member_declaration_list") {
		for _, member := range findAllNodes(memberList, "enum_member_declaration") {
			// The first identifier child is the member name.
			for i := 0; i < int(member.ChildCount()); i++ {
				ch := member.Child(i)
				if ch != nil && ch.Type() == "identifier" {
					members = append(members, string(file.Content[ch.StartByte():ch.EndByte()]))
					break
				}
			}
		}
	}

	sig := name
	if len(members) > 0 {
		sig = name + " { " + strings.Join(members, ", ") + " }"
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum",
		SourceFile:         file.Path,
		Language:           "csharp",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
	}, true
}

// buildEnumValueSet builds the value-carrying SCOPE.Enum node for a C# enum,
// capturing each member's explicit literal value (`Active = 1`) when present.
// Members with no explicit value (`Active,` — C# auto-assigns the position) are
// recorded value-less; honest-partial avoids fabricating the implicit ordinal.
func buildEnumValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := childFieldText(node, "name", file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	var members []extractor.EnumMember
	// Walk the member-declaration list in source order (findAllNodes is a
	// stack DFS and reverses sibling order, which would corrupt the value-set).
	list := findChildByType(node, "enum_member_declaration_list")
	if list != nil {
		for i := 0; i < int(list.ChildCount()); i++ {
			member := list.Child(i)
			if member == nil || member.Type() != "enum_member_declaration" {
				continue
			}
			var mname, mval string
			for j := 0; j < int(member.ChildCount()); j++ {
				ch := member.Child(j)
				if ch == nil {
					continue
				}
				if ch.Type() == "identifier" && mname == "" {
					mname = string(file.Content[ch.StartByte():ch.EndByte()])
					continue
				}
				// The value follows the `=` token: the first named, non-name
				// child is the literal/expression initialiser.
				if mname != "" && ch.IsNamed() && ch.Type() != "identifier" {
					mval = extractor.StripLiteralQuotes(
						string(file.Content[ch.StartByte():ch.EndByte()]))
				}
			}
			if mname != "" {
				members = append(members, extractor.EnumMember{Name: mname, Value: mval})
			}
		}
	}
	return extractor.EnumEntity(
		name, "csharp", "csharp_enum", file.Path,
		int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1, members,
	)
}

// buildOperation creates a SCOPE.Operation entity for method/constructor declarations.
//
// Issue #65 parity: when parentType is non-empty, Name is emitted as
// "<parentType>.<member>".
func buildOperation(node ts.Node, file extractor.FileInput, subtype, parentType string) (types.EntityRecord, bool) {
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
		Language:           "csharp",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          buildMethodSignature(file.Content, node),
		EnrichmentRequired: false,
	}, true
}

// buildImport creates a SCOPE.Component entity with an IMPORTS relationship.
//
// Issue #368 — IMPORTS edges carry the same Properties contract Python/Java
// emit (local_name / source_module / imported_name / wildcard).
//
// `using System.Collections.Generic;` →
//
//	local_name="Generic", source_module="System.Collections", imported_name="Generic"
//
// `using static System.Math;` →
//
//	local_name="Math", source_module="System", imported_name="Math"
//
// `using A = System.Console;` (alias) →
//
//	local_name="A", source_module="System", imported_name="Console"
func buildImport(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	raw, alias := extractUsingTargetWithAlias(node, file.Content)
	if raw == "" {
		return types.EntityRecord{}, false
	}

	// Top-level namespace is the first segment.
	top := raw
	if idx := strings.Index(raw, "."); idx >= 0 {
		top = raw[:idx]
	}

	props := map[string]string{}
	leaf := raw
	mod := raw
	if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
		leaf = raw[dot+1:]
		mod = raw[:dot]
	}
	props["source_module"] = mod
	props["imported_name"] = leaf
	if alias != "" {
		props["local_name"] = alias
	} else {
		props["local_name"] = leaf
	}

	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: file.Path,
		Language:   "csharp",
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

// extractUsingTargetWithAlias returns (target, alias) from a using_directive.
// `using X.Y;` → ("X.Y", ""). `using A = X.Y;` → ("X.Y", "A").
// `using static X.Y;` → ("X.Y", "").
//
// tree-sitter-c-sharp shape (observed): the directive has an optional
// "name" field carrying the alias identifier in the aliased form
// `using A = X.Y;`. The path itself appears as a sibling
// qualified_name / identifier / member_access_expression child without
// a field name.
func extractUsingTargetWithAlias(node ts.Node, src []byte) (string, string) {
	var alias string
	// Detect alias via the "name" field (only present in `using A = X.Y;`).
	if n := node.ChildByFieldName("name"); n != nil && n.Type() == "identifier" {
		alias = string(src[n.StartByte():n.EndByte()])
	}

	// Pick the path: the first qualified_name / member_access_expression
	// child, falling back to the first identifier that isn't the alias.
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		t := ch.Type()
		if t == "qualified_name" || t == "member_access_expression" {
			return string(src[ch.StartByte():ch.EndByte()]), alias
		}
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil || ch.Type() != "identifier" {
			continue
		}
		text := string(src[ch.StartByte():ch.EndByte()])
		// Skip the alias-side identifier (carried by field "name").
		if text == alias {
			continue
		}
		return text, alias
	}
	// Fallback: strip "using ", "static ", "<alias> = ", ";"
	full := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
	full = strings.TrimSuffix(full, ";")
	full = strings.TrimPrefix(full, "using ")
	full = strings.TrimPrefix(full, "static ")
	if eq := strings.Index(full, "="); eq >= 0 {
		full = strings.TrimSpace(full[eq+1:])
	}
	return strings.TrimSpace(full), alias
}

// collectImportNames scans the file for top-level using_directive nodes
// and returns a set of locally-bound simple names. Used by the receiver
// binder as a confirming signal for PascalCase static-call shapes.
func collectImportNames(root ts.Node, src []byte) map[string]bool {
	if root == nil {
		return nil
	}
	out := make(map[string]bool)
	for _, n := range findAllNodes(root, "using_directive") {
		raw, alias := extractUsingTargetWithAlias(n, src)
		if raw == "" {
			continue
		}
		leaf := raw
		if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
			leaf = raw[dot+1:]
		}
		if alias != "" {
			out[alias] = true
		} else {
			out[leaf] = true
		}
	}
	return out
}

// extractCallRelationships emits CALLS edges for invocation_expression and
// object_creation_expression descendants of body. Receiver-aware target
// resolution mirrors Java #120: receivers typed via fields, parameters,
// or locals produce dotted "<Type>.<method>" targets; PascalCase bare
// receivers stay dotted; everything else falls back to the bare leaf.
func extractCallRelationships(
	body ts.Node,
	src []byte,
	callerName string,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
	cross *csharpCrossCtx,
) []types.RelationshipRecord {
	if body == nil || callerName == "" {
		return nil
	}
	locals := collectLocalVarTypes(body, src)
	merged := paramTypes
	if len(locals) > 0 {
		merged = make(map[string]string, len(paramTypes)+len(locals))
		for k, v := range locals {
			merged[k] = v
		}
		for k, v := range paramTypes {
			merged[k] = v // params win over locals
		}
	}
	calls := findAllNodes(body, "invocation_expression", "object_creation_expression")
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	rels := make([]types.RelationshipRecord, 0, len(calls))
	for _, call := range calls {
		target := csharpCallTarget(call, src, cc, merged, imports)
		if target == "" {
			continue
		}
		// Self-recursion check: skip bare-name targets that match the
		// caller's own leaf name (e.g. `Process()` calling itself without
		// a receiver). Dotted targets (e.g. "OrderService.Process") are
		// cross-type calls and MUST NOT be filtered even when the leaf
		// matches the caller's name — "OrderController.Process" calling
		// "OrderService.Process" is a legitimate outbound call, not
		// recursion (#2114). The previous check applied the leaf match
		// to all dotted targets, which incorrectly dropped every CALLS
		// edge where the callee method shared its name with the enclosing
		// method.
		if strings.IndexByte(target, '.') < 0 && target == callerName {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		props := map[string]string{
			"line": strconv.Itoa(int(call.StartPoint().Row) + 1),
		}
		// Issue #4374 — stamp the resolved (namespace candidates, type, leaf)
		// for a qualified cross-namespace call so the resolver can bind it via
		// the namespace-keyed member index instead of the ambiguous global
		// byName index. Only fires for statically type-qualified member-access
		// invocations; bare / instance-receiver / object-creation calls are
		// left untouched (no false stamps).
		if cross != nil && call.Type() == "invocation_expression" {
			if fn := call.ChildByFieldName("function"); fn != nil {
				if b := cross.resolveQualifiedCall(fn, src, cc, merged); b != nil {
					props["csharp_call_ns"] = strings.Join(b.nsCandidates, ";")
					props["csharp_call_type"] = b.typ
					props["call_leaf"] = b.leaf
				}
			}
		}
		rels = append(rels, types.RelationshipRecord{
			ToID:       target,
			Kind:       "CALLS",
			Properties: props,
		})
	}
	return rels
}

// csharpCallTarget resolves the callee target from an invocation_expression
// or object_creation_expression node.
func csharpCallTarget(
	call ts.Node,
	src []byte,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) string {
	switch call.Type() {
	case "invocation_expression":
		fn := call.ChildByFieldName("function")
		if fn == nil {
			return ""
		}
		switch fn.Type() {
		case "identifier":
			return string(src[fn.StartByte():fn.EndByte()])
		case "member_access_expression":
			nameNode := fn.ChildByFieldName("name")
			if nameNode == nil {
				return ""
			}
			method := string(src[nameNode.StartByte():nameNode.EndByte()])
			obj := fn.ChildByFieldName("expression")
			if obj == nil {
				return method
			}
			recv := receiverTypeName(obj, src, cc, paramTypes, imports)
			if recv == "" {
				return method
			}
			return recv + "." + method
		case "generic_name":
			// `Method<T>(...)` — leaf is the leading identifier.
			for i := 0; i < int(fn.ChildCount()); i++ {
				ch := fn.Child(i)
				if ch != nil && ch.Type() == "identifier" {
					return string(src[ch.StartByte():ch.EndByte()])
				}
			}
		}
	case "object_creation_expression":
		// `new Foo(...)` — type field carries the type expression.
		typ := call.ChildByFieldName("type")
		if typ == nil {
			return ""
		}
		return leafTypeName(typ, src)
	}
	return ""
}

// receiverTypeName returns the declared type of a member-access receiver
// when statically determinable, or "" otherwise.
func receiverTypeName(
	obj ts.Node,
	src []byte,
	cc *classCtx,
	paramTypes map[string]string,
	imports map[string]bool,
) string {
	if obj == nil {
		return ""
	}
	switch obj.Type() {
	case "identifier":
		ident := string(src[obj.StartByte():obj.EndByte()])
		if cc != nil {
			if t, ok := cc.fields[ident]; ok && t != "" {
				return t
			}
		}
		if t, ok := paramTypes[ident]; ok && t != "" {
			return t
		}
		// PascalCase static-call shape.
		if isPascalCase(ident) {
			return ident
		}
		_ = imports
		return ""
	case "member_access_expression":
		// `this.<field>` and the implicit-this shape `<field>` (where
		// tree-sitter-c-sharp elides the `this` so the inner
		// member_access_expression has no expression field, only a
		// name field). Both bind to the enclosing class's fields.
		nameChild := obj.ChildByFieldName("name")
		if nameChild == nil {
			return ""
		}
		exprChild := obj.ChildByFieldName("expression")
		if exprChild != nil && exprChild.Type() != "this_expression" && exprChild.Type() != "this" {
			// Other shapes (`a.b.c.method`) — deeper chains we don't
			// currently type. Drop through to "".
			return ""
		}
		ident := string(src[nameChild.StartByte():nameChild.EndByte()])
		if cc != nil {
			if t, ok := cc.fields[ident]; ok && t != "" {
				return t
			}
		}
		return ""
	}
	return ""
}

// isPascalCase reports whether s starts with an uppercase ASCII letter
// followed by at least one more character. C# identifiers, like Java,
// are overwhelmingly ASCII PascalCase for types.
func isPascalCase(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// collectFieldTypes walks the immediate children of a declaration_list
// and returns a map of field/property-name → declared-leaf-type for
// every field_declaration and property_declaration.
//
// Multi-declarator fields (`int x, y;`) bind every variable to the
// declared type. Fields without a parseable type are dropped.
func collectFieldTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := make(map[string]string)
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "field_declaration":
			// field_declaration wraps a variable_declaration{type, declarator+}.
			vd := findChildByType(ch, "variable_declaration")
			if vd == nil {
				continue
			}
			typ := leafTypeName(vd.ChildByFieldName("type"), src)
			if typ == "" {
				continue
			}
			for j := 0; j < int(vd.ChildCount()); j++ {
				d := vd.Child(j)
				if d == nil || d.Type() != "variable_declarator" {
					continue
				}
				name := childFieldText(d, "name", src)
				if name == "" {
					// fall back to first identifier child.
					for k := 0; k < int(d.ChildCount()); k++ {
						cc := d.Child(k)
						if cc != nil && cc.Type() == "identifier" {
							name = string(src[cc.StartByte():cc.EndByte()])
							break
						}
					}
				}
				if name == "" {
					continue
				}
				if _, ok := out[name]; !ok {
					out[name] = typ
				}
			}
		case "property_declaration":
			typ := leafTypeName(ch.ChildByFieldName("type"), src)
			if typ == "" {
				continue
			}
			name := childFieldText(ch, "name", src)
			if name == "" {
				continue
			}
			if _, ok := out[name]; !ok {
				out[name] = typ
			}
		}
	}
	return out
}

// collectParamTypes returns parameter-name → leaf-type for every formal
// parameter on a method/constructor declaration.
func collectParamTypes(node ts.Node, src []byte) map[string]string {
	if node == nil {
		return nil
	}
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	out := make(map[string]string)
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		if p == nil || p.Type() != "parameter" {
			continue
		}
		typ := leafTypeName(p.ChildByFieldName("type"), src)
		if typ == "" {
			continue
		}
		name := childFieldText(p, "name", src)
		if name == "" {
			continue
		}
		out[name] = typ
	}
	return out
}

// collectLocalVarTypes walks descendants of body and returns
// local-name → declared leaf type for local_declaration_statement
// nodes. Implicitly-typed `var` declarations are not bound.
func collectLocalVarTypes(body ts.Node, src []byte) map[string]string {
	if body == nil {
		return nil
	}
	out := map[string]string{}
	for _, decl := range findAllNodes(body, "local_declaration_statement") {
		vd := findChildByType(decl, "variable_declaration")
		if vd == nil {
			continue
		}
		// The declared type. For an implicitly-typed `var` declaration the
		// leaf is "var" (or ""), which carries no usable type; in that case we
		// infer the type CONSERVATIVELY from the right-hand-side initialiser
		// per declarator (see inferImplicitLocalType). #4685 (mirrors Java
		// #4717 `newExprClassName`): only `new ClassName(...)` and
		// target-typed `new(...)` (paired with an explicit declared type, which
		// the non-var path already handles) bind; a factory / method-call /
		// DI-returning-interface RHS (`var s = factory.Create();`) stays
		// UNTYPED so the call resolves to its bare leaf rather than a fabricated
		// receiver type.
		declType := leafTypeName(vd.ChildByFieldName("type"), src)
		implicit := isImplicitVarType(declType)
		if declType == "" && !implicit {
			continue
		}
		for i := 0; i < int(vd.ChildCount()); i++ {
			ch := vd.Child(i)
			if ch == nil || ch.Type() != "variable_declarator" {
				continue
			}
			name := childFieldText(ch, "name", src)
			if name == "" {
				for k := 0; k < int(ch.ChildCount()); k++ {
					cc := ch.Child(k)
					if cc != nil && cc.Type() == "identifier" {
						name = string(src[cc.StartByte():cc.EndByte()])
						break
					}
				}
			}
			if name == "" {
				continue
			}
			typ := declType
			if implicit {
				typ = inferImplicitLocalType(ch, src)
				if typ == "" {
					continue
				}
			}
			out[name] = typ
		}
	}
	// `foreach (T x in xs)` — bind loop variable.
	for _, fr := range findAllNodes(body, "for_each_statement") {
		typ := leafTypeName(fr.ChildByFieldName("type"), src)
		if typ == "" {
			continue
		}
		name := ""
		// tree-sitter-c-sharp uses the field "left" or a direct identifier.
		if l := fr.ChildByFieldName("left"); l != nil && l.Type() == "identifier" {
			name = string(src[l.StartByte():l.EndByte()])
		}
		if name == "" {
			// Fallback: first identifier child after the type.
			for j := 0; j < int(fr.ChildCount()); j++ {
				ch := fr.Child(j)
				if ch != nil && ch.Type() == "identifier" {
					name = string(src[ch.StartByte():ch.EndByte()])
					break
				}
			}
		}
		if name != "" {
			out[name] = typ
		}
	}
	return out
}

// isImplicitVarType reports whether a declared-type leaf is the implicitly
// typed `var` keyword. tree-sitter-c-sharp models `var x = …` with an
// `implicit_type` node whose text is "var"; leafTypeName's last-resort branch
// returns the raw "var" token for it. We treat that as "no static declared
// type" and fall back to RHS inference (#4685).
func isImplicitVarType(declType string) bool {
	return declType == "var"
}

// inferImplicitLocalType returns the leaf class name a `var` local is bound to
// when, and ONLY when, its initialiser is an object-creation expression —
// `var c = new XController(svc)` → "XController". This mirrors the conservatism
// of Java #4717 `newExprClassName`: a factory/method-call/DI RHS that returns
// an interface (`var s = factory.Create();`, `var svc = sp.GetRequiredService<…>()`)
// yields "" so the receiver stays bare and no fabricated dotted target is
// emitted. Target-typed `new(...)` is handled on the explicit-declared-type
// path (the declared type is concrete there), so it is intentionally NOT
// inferred here (an implicit `var x = new();` does not type-check in C#).
func inferImplicitLocalType(declarator ts.Node, src []byte) string {
	if declarator == nil {
		return ""
	}
	// The declarator's value child is the initialiser expression. tree-sitter
	// exposes it as the named child following the `=`; scan named children for
	// the (single) object_creation_expression or a DI service-resolution call.
	for i := 0; i < int(declarator.NamedChildCount()); i++ {
		ch := declarator.NamedChild(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "object_creation_expression":
			typ := ch.ChildByFieldName("type")
			if typ == nil {
				// Fall back to the first identifier/generic_name child.
				for j := 0; j < int(ch.NamedChildCount()); j++ {
					c := ch.NamedChild(j)
					if c != nil && (c.Type() == "identifier" || c.Type() == "generic_name" || c.Type() == "qualified_name") {
						typ = c
						break
					}
				}
			}
			if leaf := leafTypeName(typ, src); leaf != "" && leaf != "var" {
				return leaf
			}
		case "invocation_expression":
			if leaf := diServiceTypeArg(ch, src); leaf != "" {
				return leaf
			}
		}
	}
	return ""
}

// diServiceTypeArgMethods is the set of .NET dependency-injection resolution
// methods whose single generic type argument IS the resolved service type.
// `sp.GetRequiredService<XController>()` / `_factory.Services.GetService<T>()`
// — the WebApplicationFactory + IServiceProvider idiom (#4685 gap 2). We bind
// the local to the type argument so a follow-up `c.Method()` resolves to that
// class. This is fully static (the type argument is a compile-time token), so
// it is sound; non-DI generic calls don't match the method-name allow-list and
// stay bare.
var diServiceTypeArgMethods = map[string]bool{
	"GetRequiredService":      true,
	"GetService":              true,
	"GetServices":             true,
	"GetKeyedService":         true,
	"GetRequiredKeyedService": true,
}

// diServiceTypeArg returns the leaf type argument of a DI service-resolution
// invocation (`...GetRequiredService<XController>()`), or "" when the call is
// not a recognised single-type-argument resolution method. The method name is
// taken from the invocation's function node: either a bare `generic_name`
// (`GetRequiredService<T>()`) or a `member_access_expression` whose `name` is a
// `generic_name` (`sp.GetRequiredService<T>()`).
func diServiceTypeArg(call ts.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	var gen ts.Node
	switch fn.Type() {
	case "generic_name":
		gen = fn
	case "member_access_expression":
		if name := fn.ChildByFieldName("name"); name != nil && name.Type() == "generic_name" {
			gen = name
		}
	}
	if gen == nil {
		return ""
	}
	// Method name is the leading identifier of the generic_name.
	var methodName string
	for i := 0; i < int(gen.NamedChildCount()); i++ {
		c := gen.NamedChild(i)
		if c != nil && c.Type() == "identifier" {
			methodName = string(src[c.StartByte():c.EndByte()])
			break
		}
	}
	if !diServiceTypeArgMethods[methodName] {
		return ""
	}
	// Single type argument → its leaf is the service type.
	tal := findChildByType(gen, "type_argument_list")
	if tal == nil {
		return ""
	}
	var args []ts.Node
	for i := 0; i < int(tal.NamedChildCount()); i++ {
		if c := tal.NamedChild(i); c != nil {
			args = append(args, c)
		}
	}
	if len(args) != 1 {
		return "" // multiple/zero type args — ambiguous, stay bare
	}
	if leaf := leafTypeName(args[0], src); leaf != "" && leaf != "var" {
		return leaf
	}
	return ""
}

// leafTypeName returns the leaf type identifier of a C# type node,
// stripping generic parameters, nullable markers, and array suffixes.
// Returns "" for type nodes the function can't characterise.
func leafTypeName(typ ts.Node, src []byte) string {
	if typ == nil {
		return ""
	}
	switch typ.Type() {
	case "identifier", "predefined_type":
		return strings.TrimSpace(string(src[typ.StartByte():typ.EndByte()]))
	case "generic_name":
		// First child is the underlying identifier.
		for i := 0; i < int(typ.ChildCount()); i++ {
			ch := typ.Child(i)
			if ch != nil && ch.Type() == "identifier" {
				return string(src[ch.StartByte():ch.EndByte()])
			}
		}
	case "nullable_type":
		if first := typ.NamedChild(0); first != nil {
			return leafTypeName(first, src)
		}
	case "array_type":
		if elem := typ.ChildByFieldName("type"); elem != nil {
			return leafTypeName(elem, src)
		}
		if first := typ.NamedChild(0); first != nil {
			return leafTypeName(first, src)
		}
	case "qualified_name":
		// `System.String` — leaf is the rightmost identifier.
		ids := findAllNodes(typ, "identifier")
		if len(ids) > 0 {
			n := ids[len(ids)-1]
			return strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
		}
	}
	// Last resort — use the raw text if it looks like a single identifier.
	raw := strings.TrimSpace(string(src[typ.StartByte():typ.EndByte()]))
	if raw == "" || strings.ContainsAny(raw, " <>[]?,") {
		return ""
	}
	return raw
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
		if n == nil {
			continue
		}
		if set[n.Type()] {
			out = append(out, n)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return out
}

// findChildByType returns the first direct child of node with type t.
func findChildByType(node ts.Node, t string) ts.Node {
	if node == nil {
		return nil
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == t {
			return ch
		}
	}
	return nil
}

// nodeText returns the source text covered by node.
func nodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

// childFieldText extracts the text of a named child field (e.g. "name").
func childFieldText(node ts.Node, field string, src []byte) string {
	child := node.ChildByFieldName(field)
	if child == nil {
		return ""
	}
	return string(src[child.StartByte():child.EndByte()])
}

// buildMethodSignature builds a Python-parity method signature for C#.
// Collapses multi-line declarations, strips attribute args, keeps visibility.
func buildMethodSignature(src []byte, node ts.Node) string {
	raw := string(src[node.StartByte():node.EndByte()])
	// Strip attribute arguments FIRST to remove braces inside attribute args
	// like [HttpGet("{id}")] → [HttpGet], before body-brace search.
	raw = stripCSharpAttributeArgs(raw)
	// Trim at body start.
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	// Remove lambda-style body.
	if idx := strings.Index(raw, "=>"); idx >= 0 {
		raw = raw[:idx]
	}
	// Collapse newlines + whitespace into single spaces.
	raw = strings.Join(strings.Fields(raw), " ")
	return strings.TrimSpace(raw)
}

// buildClassSignature returns a short signature for class/interface declarations.
// Strips attributes and inheritance to match Python convention: "public class Name".
func buildClassSignature(node ts.Node, src []byte) string {
	raw := string(src[node.StartByte():node.EndByte()])
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.Join(strings.Fields(raw), " ")
	// Strip attributes entirely (Python doesn't include them for C# classes).
	raw = stripCSharpAttributes(raw)
	// Strip inheritance (: BaseClass).
	if idx := strings.Index(raw, " :"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// stripCSharpAttributeArgs strips arguments from C# attributes: [Foo("bar")] -> [Foo].
func stripCSharpAttributeArgs(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '[' {
			// Copy [
			result.WriteByte('[')
			i++
			// Copy attribute name.
			for i < len(s) && s[i] != '(' && s[i] != ']' {
				result.WriteByte(s[i])
				i++
			}
			// Skip (args).
			if i < len(s) && s[i] == '(' {
				depth := 1
				i++
				for i < len(s) && depth > 0 {
					switch s[i] {
					case '(':
						depth++
					case ')':
						depth--
					}
					i++
				}
			}
			// Copy ].
			if i < len(s) && s[i] == ']' {
				result.WriteByte(']')
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// stripCSharpAttributes removes all [Attribute] and [Attribute(...)] tokens entirely.
func stripCSharpAttributes(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '[' {
			// Skip until matching ].
			depth := 1
			i++
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '[':
					depth++
				case ']':
					depth--
				}
				i++
			}
			// Skip trailing space.
			for i < len(s) && s[i] == ' ' {
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}
