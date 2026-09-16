// Package csharp implements the tree-sitter–based extractor for C# source files.
//
// Extracted entities:
//   - method_declaration      → Kind="SCOPE.Operation", Subtype="method"
//   - constructor_declaration → Kind="SCOPE.Operation", Subtype="constructor"
//   - class_declaration       → Kind="SCOPE.Component", Subtype="class"
//   - using_directive         → Kind="SCOPE.Component", Subtype="import"
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
	"unicode"

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
	walk(root, file, "", fileScopedNamespace(root, file.Content), nil, imports, cross, &entities)
	// Issue #3641 (epic #3625) — config-key consumption edges
	// (Configuration["X"] / GetValue / GetConnectionString /
	// Environment.GetEnvironmentVariable) → shared SCOPE.Config config_key nodes.
	emitConfigConsumerEdges(root, file.Content, &entities)
	// Epic #3628 — error-flow topology: typed `throw new` / `catch` shapes →
	// THROWS / CATCHES edges to a shared SCOPE.ExceptionType node, matching the
	// Java / Python / Go / JS flagship error_flow model.
	emitExceptionFlowEdges(root, file.Content, &entities)
	// Issue #6742 — class-hierarchy edges: EXTENDS for a base class, IMPLEMENTS
	// for an implemented interface, for EVERY supertype rather than only the
	// ones declared in this same file (which is all #4854 ever emitted, and
	// always as EXTENDS).
	entities = attachCsharpHierarchy(entities)
	// Issue #6912 — field→declared-type REFERENCES edges, for the types
	// DECLARED IN THIS FILE only. Runs after the walk because the set of
	// in-file declarations is only complete once every declaration is on the
	// table; see field_type_refs.go for why the target is addressed
	// structurally rather than by bare name.
	entities = attachCsharpFieldTypeRefs(entities, file.Path)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(entities, "csharp")
	extractor.TagEntitiesLanguage(entities, "csharp")
	return entities, nil
}

func fileScopedNamespace(root ts.Node, src []byte) string {
	if root == nil {
		return ""
	}
	for i := range root.ChildCount() {
		ch := root.Child(int(i))
		if ch == nil || ch.Type() != "file_scoped_namespace_declaration" {
			continue
		}
		if nf := ch.ChildByFieldName("name"); nf != nil {
			return strings.TrimSpace(nodeText(nf, src))
		}
	}
	return ""
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
		// (folds with the endpoint-bound DTO members in #4715 when the Kind
		// matches too — MergeWithCustom keys on (SourceFile, Kind, Name)
		// since #6104, not on Name alone).
		fieldEnts := emitFieldMembers(node, body, file.Content, rec.Name, file.Path)
		for _, fe := range fieldEnts {
			toID := extractor.BuildSchemaFieldStructuralRef("csharp", file.Path, fe.Name)
			(*out)[classIdx].Relationships = append((*out)[classIdx].Relationships,
				types.RelationshipRecord{ToID: toID, Kind: "CONTAINS"})
		}
		*out = append(*out, fieldEnts...)
		// Issue #6742 — stash the ordered base list and the declaration
		// keyword for the hierarchy post-pass, which needs every type in the
		// file on the table before it can tell a base class from an interface.
		if baseNames := csBaseTypeNames(node, file.Content); len(baseNames) > 0 {
			if (*out)[classIdx].Metadata == nil {
				(*out)[classIdx].Metadata = map[string]interface{}{}
			}
			(*out)[classIdx].Metadata["hierarchy_bases"] = baseNames
			(*out)[classIdx].Metadata["hierarchy_decl"] = csDeclKeyword(node.Type())
		}
		return

	case "enum_declaration":
		if rec, ok := buildEnumEntity(node, file); ok {
			stampNamespace(&rec, ns)
			// Issue #6742 — an enum IS routed through the hierarchy pass,
			// deliberately. `enum E : byte` really does parse as a base_list,
			// and the ONLY thing that stops `byte` becoming a supertype is
			// csBaseTypeNames' node-type allow-list. Skipping enums here
			// instead would suppress the edge one level earlier and leave that
			// allow-list ungraded for the case its own doc names — which is
			// exactly the state #6742's first cut shipped in.
			if baseNames := csBaseTypeNames(node, file.Content); len(baseNames) > 0 {
				if rec.Metadata == nil {
					rec.Metadata = map[string]interface{}{}
				}
				rec.Metadata["hierarchy_bases"] = baseNames
				rec.Metadata["hierarchy_decl"] = csDeclKeyword(node.Type())
			}
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

	props := types.Props{}
	leaf := raw
	mod := raw
	if dot := strings.LastIndexByte(raw, '.'); dot > 0 {
		leaf = raw[dot+1:]
		mod = raw[:dot]
	}
	props.Set("language", "csharp")
	props.Set("source_module", mod)
	props.Set("imported_name", leaf)
	if alias != "" {
		props.Set("local_name", alias)
	} else {
		props.Set("local_name", leaf)
	}

	return types.EntityRecord{
		Name: top,
		Kind: "SCOPE.Component",
		// #6601 — `Subtype:"import"` is load-bearing, not decoration. Every
		// prune predicate in internal/resolve/imports.go selects carriers with
		// `Kind == "SCOPE.Component" && Subtype == "import"`. With the subtype
		// unset the kind matched and the subtype never did, so the prune pass
		// did not merely fail to remove these carriers — it never looked at
		// them (`considered=0`), and they shipped in the flatbuffer as
		// orphans. Do not drop this literal without changing those predicates.
		Subtype:    "import",
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
		props := types.Props{
			{K: "line", V: strconv.Itoa(int(call.StartPoint().Row) + 1)},
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
					props.Set("csharp_call_ns", strings.Join(b.nsCandidates, ";"))
					props.Set("csharp_call_type", b.typ)
					props.Set("call_leaf", b.leaf)
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
	//
	// #7068: this loop used to match `for_each_statement`, a node type
	// tree-sitter-c-sharp does not have (the grammar spells it
	// `foreach_statement`), and the literal was UNPAIRED — no arm anywhere
	// matched the real name — so the whole loop had NEVER EXECUTED. Code that
	// has never run is not known to be correct, and an audit of the body
	// against real CST dumps found three separate defects beyond the label.
	//
	//   1. FABRICATED PSEUDO-TYPES. `implicit_type` reaches leafTypeName's
	//      last-resort branch, which returns the raw text "var" because `var`
	//      IS a well-formed C# identifier. Correcting the label alone would
	//      have emitted `var.Process` — a fabricated DOTTED receiver, which is
	//      worse than the honest bare name it replaces because a dotted target
	//      escapes the resolver's bare-name class entirely (#7056).
	//      `dynamic` is the same category wearing a different node: it is a
	//      contextual keyword meaning "no static type", but the grammar spells
	//      it `identifier`, so leafTypeName returns "dynamic" and the arm would
	//      emit `dynamic.Process`. csNonBindableTypeKeyword refuses BOTH.
	//
	//      UNVERIFIED, and repeated here because this is where the decision
	//      reads as settled: the two tokens may not be symmetric. A user type
	//      named `var` is prohibited by the language; one named `dynamic` is
	//      believed to be PERMITTED, and if so, `class dynamic {}` beside
	//      `foreach (dynamic d in xs)` is a legal program whose correct edge the
	//      guard silently drops. NO C# COMPILER EXISTS IN THIS ENVIRONMENT, so
	//      neither the reviewer who raised it nor the author could demonstrate
	//      the rule. See csNonBindableTypeKeyword for what settles it.
	//
	//      A `foreach`'s `right` field is the COLLECTION, not the element, so
	//      inferImplicitLocalType's RHS trick cannot be reused here — it would
	//      need the collection's generic argument. `foreach (var o in …)`
	//      therefore stays untyped and its calls keep their bare leaf. That is
	//      a known limit, not a bug, and it is where the recall actually is:
	//      96.6% of the `foreach` statements in the measured corpus are `var`.
	//
	//      STILL FABRICATED, pinned as known-wrong rather than fixed: an open
	//      GENERIC TYPE PARAMETER. `foreach (T o in xs)` inside `class C<T>`
	//      parses as `identifier` and emits `T.Process`. Refusing it needs
	//      type-parameter SCOPE, which this extractor does not have anywhere —
	//      classCtx carries only `fields`, and the neighbouring #6912 field-type
	//      pass records the identical hole as its own known limitation with its
	//      own pinned over-fire tests (field_type_refs.go, "an open type
	//      parameter `T` is dropped only because nothing in the file happens to
	//      be named `T`"). Building that scope is a separate arm; a name
	//      blocklist guessing at `T`/`TKey` would be worse than the honest gap.
	//      TestCSharp_Foreach7068_TypeParameterIsFabricated_KnownWrong pins it,
	//      and a fix is EXPECTED to break that test.
	//
	//   2. A WRONG NAME FALLBACK. The arm fell back to "first identifier child"
	//      when the `left` field was not an identifier. The only form that
	//      reaches it is `foreach (var (a, b) in pairs)` — `type` is present
	//      (`implicit_type`, so the empty-type guard does not fire) and `left`
	//      is a `tuple_pattern`. The fallback then scanned direct children and
	//      found `pairs`, the COLLECTION, binding it to "var" so a later
	//      `pairs.Clear()` emitted `var.Clear`. There are 7 such statements in
	//      the measured corpus. It is deleted: the keyword guard above now
	//      refuses that input before a name is looked at, and no OTHER form is
	//      known that reaches the fallback. Note the precise claim — it is NOT
	//      "every form with a `type` field has an identifier `left`", which the
	//      `var (a, b)` case above disproves; it is that every form REACHING
	//      this point does, over the enumerated space in
	//      foreach_localvar_7068_test.go. That space is not proven exhaustive.
	//
	//   3. TAKING A NAME SOMETHING ELSE HAD ALREADY BOUND. This pass runs
	//      AFTER the local_declaration_statement pass over the same `out` map,
	//      so a plain `out[name] = typ` let a `foreach` binding CLOBBER a
	//      local's — regardless of source order, because the two passes are
	//      separate walks. A `foreach` variable's scope is its own statement,
	//      so a sibling block may reuse the name, and clobbering turned a
	//      previously-CORRECT `Order.Ship` into `Line.Ship`. That would have
	//      been a regression minted by this change.
	//
	//      The FIRST fix for this consulted `out` — "skip a name the locals
	//      pass already bound" — and the invariant claimed for it ("no receiver
	//      that resolved correctly without this arm can change") was FALSE, for
	//      a reason worth recording: `local_declaration_statement` is the only
	//      binding form this extractor collects, so consulting `out` asks
	//      "did WE type this name?", not "is this name taken?". A name bound by
	//      a `using`, a `catch`, a lambda parameter, a local-function parameter
	//      or an `out` declaration is absent from `out` entirely, so the
	//      `foreach` pass took the key unopposed and converted a BARE leaf —
	//      which the resolver's same-file tier often launders to the RIGHT
	//      target (#7071) — into a WRONG dotted one. Measured end to end:
	//      `using (Conn c = Open()) { c.Close(); }` beside `foreach (Order c
	//      in orders)` emitted `Order.Close`, on an `Order` that has no
	//      `Close`, where the arm-off tree emitted the correct `Conn.Close`.
	//
	//      So the guard now asks the right question, of the SOURCE rather than
	//      of our own map: csNamesBoundOutsideForeach collects every name bound
	//      by any declaration construct in the body and the `foreach` pass
	//      refuses those names outright. That check SUBSUMES the old `out`
	//      lookup (a local declaration is a `variable_declarator`, which the
	//      ledger collects), so the `out` lookup is gone rather than left in
	//      front of it — a redundant guard fires only where the real one
	//      already fires and leaves both ungraded.
	//
	//      WHAT IS AND IS NOT GUARANTEED. `out` is a superset of what it held
	//      before, agreeing on every shared key, and a `foreach` name that
	//      collides with any binding form in csNamesBoundOutsideForeach's list
	//      is refused. That list is an ENUMERATION, so the guarantee is only as
	//      wide as the list: a binding construct not on it would still be taken
	//      unopposed. "0 changed-target rows" is therefore a CORPUS
	//      OBSERVATION, not a structural proof — it was stated as the latter in
	//      an earlier revision of this comment and that was wrong.
	//      The flat map also still cannot model block scope, so a
	//      same-name/different-type collision degrades to a bare leaf rather
	//      than to a wrong dotted target.
	//
	// Forms that DO bind — the enumerated space, NOT proven exhaustive:
	// identifier (`Order o`), predefined_type (`string s`), generic_name
	// (`List<Order> g`), array_type (`Order[] a`), nullable_type (`Order? o`),
	// qualified_name (`Ns.Order o`) and alias_qualified_name — the last only in
	// its SINGLE-segment form (`global::Order o`), since `global::A.B.Foo`
	// parses as a qualified_name whose qualifier merely contains the alias.
	// Plus `await foreach`, which is the same node. findAllNodes is a full
	// descendant walk, so nested loops bind.
	//
	// pointer_type (`Node* p`) is a deliberate half-entry. It shares
	// leafTypeName's `array_type, pointer_type` case and WOULD enter the map —
	// stated in the conditional because no valid C# lets a test observe it, and
	// an unobservable claim is unfalsifiable rather than verified. What IS
	// observed is that no compilable C# turns it into a dotted CALLS target: a
	// pointer has no
	// members, so `p.Touch()` does not compile, and the `p->Touch()` that does
	// is not a `member_access_expression` and never reaches receiver typing.
	// Listing it as a binding form would assert something no valid program can
	// observe; the observed bare result is pinned instead.
	//
	// Forms that bind NOTHING, each pinned by a test: an explicitly typed
	// deconstruction `foreach ((Order a, Order b) in xs)` (no `type` field at
	// all), `foreach (var o in xs)`, `foreach (dynamic d in xs)` and
	// `foreach (var (a, b) in xs)` (keyword guard), and `foreach (ref Order o in
	// span)` / `ref readonly` — `ref_type` has no leafTypeName case, so its raw
	// text "ref Order" fails the identifier allow-list and yields "". The ref
	// form is an unfixed gap, not a decision; it is out of scope here.
	feTypes := map[string]string{}
	feAmbiguous := map[string]bool{}
	for _, fr := range findAllNodes(body, "foreach_statement") {
		typ := leafTypeName(fr.ChildByFieldName("type"), src)
		// An empty `typ` means "we could not determine a type". Such a
		// statement must not reach the ledger below at all: writing "" would
		// both poison the ambiguity check against a later real binding of the
		// same name and, before the two-stage rewrite, have overwritten a
		// correct local binding with a value receiverTypeName reads as absent.
		if typ == "" || csNonBindableTypeKeyword(typ) {
			continue
		}
		l := fr.ChildByFieldName("left")
		if l == nil || l.Type() != "identifier" {
			continue
		}
		name := string(src[l.StartByte():l.EndByte()])
		if prev, seen := feTypes[name]; seen && prev != typ {
			feAmbiguous[name] = true
			continue
		}
		feTypes[name] = typ
	}
	claimed := csNamesBoundOutsideForeach(body, src)
	for name, typ := range feTypes {
		if feAmbiguous[name] || claimed[name] {
			continue
		}
		out[name] = typ
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

// csNonBindableTypeKeyword reports whether a declared-type leaf is a C# keyword
// that occupies a type position without NAMING a type, so binding a receiver to
// it would fabricate a dotted target (#7068).
//
// Two tokens qualify, and they reach here through DIFFERENT node types, which
// is why one string check covers both and neither is redundant:
//
//	var      → implicit_type, whose raw text leafTypeName returns verbatim
//	dynamic  → identifier, which leafTypeName returns by its first case
//
// Both mean "no static type is known here". Emitting `var.Process` or
// `dynamic.Process` is strictly worse than the bare `Process` that not binding
// produces, because a dotted target leaves the resolver's bare-name class and
// is scored as a confident bind (#7071).
//
// This is deliberately an exact-token list, not a heuristic. It does NOT catch
// an open generic type parameter (`foreach (T o in xs)`) — see the type
// parameter note in collectLocalVarTypes; that needs scope, not a word list.
//
// UNVERIFIED, recorded rather than asserted: the two tokens may not be
// symmetric on the over-refusal axis. A user type named `var` is prohibited by
// the language, but a user type named `dynamic` is believed to be PERMITTED and
// to shadow the keyword in type position — if that is so, `class dynamic {}`
// beside `foreach (dynamic d in xs)` is a legal program whose correct edge this
// guard silently drops. NO C# COMPILER EXISTS IN THIS ENVIRONMENT, so neither
// the reviewer who raised it nor the author could demonstrate the rule either
// way, and asserting a language rule nobody ran is the exact defect class this
// change was opened to fix. What settles it: compile `class dynamic {}` with
// `csc`. Until then the direction is the conservative one — the guard drops an
// edge rather than fabricating a dotted target — and its blast radius is this
// arm only.
//
// SCOPE: this predicate is used by the `foreach` arm ONLY. The
// local_declaration_statement path above keeps isImplicitVarType, so
// `dynamic d = x; d.P();` still fabricates `dynamic.P` exactly as it does on
// main. Widening the locals path is a behaviour change to code that has always
// run and belongs to its own graded change, not to this node-type fix.
func csNonBindableTypeKeyword(declType string) bool {
	return declType == "var" || declType == "dynamic"
}

// csNamesBoundOutsideForeach returns every identifier bound by a declaration
// construct inside body, EXCLUDING `foreach` loop variables themselves.
//
// It exists because `collectLocalVarTypes`' `foreach` arm must not take a name
// something else has already bound, and "already bound" cannot be answered from
// the `out` map: `local_declaration_statement` is the only binding form this
// extractor collects, so `out` answers "did we TYPE this name?" — a strictly
// narrower question than "is this name TAKEN?". A `using`, a `catch`, a lambda
// parameter, a local-function parameter or an `out` declaration binds a name
// that never reaches `out` at all. Consulting `out` therefore let the `foreach`
// pass retarget a receiver from a bare leaf (which the resolver's same-file tier
// often launders to the RIGHT target, #7071) to a WRONG dotted one.
//
// A `var` local whose initialiser defeats inferImplicitLocalType is the same
// hazard from the other direction: the name IS a local declaration but never
// enters `out`, so the ledger must record names REGARDLESS of whether a type
// was derived for them. That is why this walks the source rather than reading
// `out`.
//
// Every node type below was confirmed by CST dump against the grammar these
// tests parse with; each carries the bound name in its `name` field except
// `implicit_parameter`, whose own text is the name:
//
//	variable_declarator    local decl, `using (T c = …)`, `using T c = …`,
//	                       `for (int c = …)`, `fixed (…)`
//	catch_declaration      `catch (Boom c)`
//	parameter              lambda `(T c) => …`, local function `void F(T c)`,
//	                       and ordinary method parameters
//	implicit_parameter     lambda `c => …`
//	declaration_expression `out var c`, `out T c`
//	declaration_pattern    `x is T c`, `case T c:`
//	from_clause/let_clause/join_clause   LINQ range variables
//	join_into_clause       `join … into c`
//	(positional)           `group … into c` / `select … into c`, which have NO
//	                       node of their own — see the scan at the end
//
// THE GUARANTEE IS AS WIDE AS THIS LIST AND NO WIDER. It is an enumeration, not
// a derivation from the grammar, so a binding construct missing from it would
// still be taken unopposed by the `foreach` arm. That is not hypothetical: the
// LINQ `into` continuation variable WAS missing from the first version of this
// list and produced the fabricated `Order.Count` on a corpus-shaped query, in
// the same shape as the `using` case one level up. The list should be read as
// "everything found so far", not as a closed set.
//
// GRADED IN BOTH DIRECTIONS, which are different questions:
//
//	REFUSAL (a colliding name must be refused) — graded for
//	variable_declarator (typed and untyped), catch_declaration, from_clause,
//	let_clause, join_clause, join_into_clause and the positional `into` scan.
//
//	FOUR ENTRIES ARE COVERED BUT UNGRADED IN THE REFUSAL DIRECTION, and they
//	are named rather than left to be discovered: `parameter` (lambda and
//	local-function parameters), `implicit_parameter`, `declaration_expression`
//	(`out var c`) and `declaration_pattern` (`x is T c`). Mutants deleting any
//	of them survive the suite. The reason is not laziness: a refusal fixture
//	needs a same-name collision, `out` declarations and `is` patterns are
//	scoped to the ENCLOSING BLOCK (so colliding with a nested `foreach` is very
//	likely CS0136 and no such program exists), and whether a lambda or
//	local-function parameter may reuse a sibling `foreach` variable's name is a
//	shadowing rule that CANNOT BE CHECKED WITHOUT A C# COMPILER — and none
//	exists in this environment. Writing those rows on an unchecked premise is
//	the defect class this change exists to fix; a named gap costs less.
//
//	OVER-REFUSAL (a NON-colliding name must be left alone) — covered in
//	AGGREGATE by the CONTROL row of essentially every test in
//	foreach_localvar_7068_test.go, and per-binding-form by
//	TestCSharp_Foreach7068_NonCollidingBindingIsKept.
//
//	BE PRECISE ABOUT WHAT THAT TABLE ADDED, because an earlier revision of this
//	comment was not. It claimed the over-refusal direction had been "graded by
//	nothing" before it existed. That is FALSE and scoring says so: a mutant
//	refusing every `foreach` name unconditionally, and one claiming every
//	identifier in the body, were BOTH already DEAD against the previous
//	revision's tests — 21 failing lines across 11 distinct tests, killed by
//	their CONTROL rows. Those controls are `t.Fatalf`, so the subtests abort
//	before any refusal assertion runs; even the narrow reading ("they passed
//	every refusal row") is not observed. What the table adds is per-form
//	resolution: which binding form over-refuses, rather than that something
//	does.
//
//	AND IT DOES NOT GRADE ANY INDIVIDUAL ENTRY. Its rows hold constant that the
//	bound name differs from the loop variable, and the loop variable is bound
//	ONLY by the `foreach_statement` — which is deliberately not on this list —
//	so no widening of any entry here can put that name in the ledger. Adding
//	`foreach_statement` to the list, the textbook over-refusal, is DEAD but
//	PASSES that table. Read it as grading the aggregate direction with
//	per-form diagnostics, not as a per-entry gate.
//
//	Over-refusal is in any case the safe direction — it drops a receiver type
//	rather than fabricating one — and since this arm never ran before #7068 it
//	can only add. The reason to keep watching it is that this ledger is a list
//	that has grown under review twice, and a growing list eventually swallows
//	names it was never meant to touch.
//
// `foreach_statement` is deliberately absent: its own loop variables are the
// names being offered, and collecting them would make every `foreach` refuse
// itself. Collisions between two `foreach` loops are handled by the ambiguity
// ledger in collectLocalVarTypes instead.
func csNamesBoundOutsideForeach(body ts.Node, src []byte) map[string]bool {
	if body == nil {
		return nil
	}
	out := map[string]bool{}
	named := findAllNodes(body,
		"variable_declarator",
		"catch_declaration",
		"parameter",
		"declaration_expression",
		"declaration_pattern",
		"from_clause",
		"let_clause",
		"join_clause",
		"join_into_clause",
	)
	for _, n := range named {
		if nm := n.ChildByFieldName("name"); nm != nil {
			if t := string(src[nm.StartByte():nm.EndByte()]); t != "" {
				out[t] = true
			}
			continue
		}
		// variable_declarator exposes `name`, but fall back to the first
		// identifier child for any shape that does not.
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(i)
			if c != nil && c.Type() == "identifier" {
				out[string(src[c.StartByte():c.EndByte()])] = true
				break
			}
		}
	}
	// A lambda's implicit parameter carries no `name` field; the node IS the
	// identifier.
	for _, n := range findAllNodes(body, "implicit_parameter") {
		if t := string(src[n.StartByte():n.EndByte()]); t != "" {
			out[t] = true
		}
	}
	// The LINQ CONTINUATION variable of `group … into c` and `select … into c`
	// has NO node of its own: the grammar emits the anonymous `into` token and a
	// bare `identifier` as DIRECT CHILDREN of the query_expression, so there is
	// nothing for findAllNodes to match and the loop above cannot see it. It is
	// found positionally instead — the identifier following the `into` token.
	//
	// `join … into c` is the sibling case and is NOT handled here: it wraps the
	// pair in a `join_into_clause`, which the list above collects. Both were
	// missed by the first version of this ledger, and the `join` one was missed
	// in a way worth recording: `join_clause` was already on the list and has no
	// `name` field, so the first-identifier fallback returned the join RANGE
	// variable and stopped — correct as far as it went, which is exactly what
	// made the missing continuation variable invisible.
	for _, q := range findAllNodes(body, "query_expression") {
		for i := 0; i < int(q.ChildCount())-1; i++ {
			c := q.Child(i)
			if c == nil || c.Type() != "into" {
				continue
			}
			if nx := q.Child(i + 1); nx != nil && nx.Type() == "identifier" {
				if t := string(src[nx.StartByte():nx.EndByte()]); t != "" {
					out[t] = true
				}
			}
		}
	}
	return out
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
	case "array_type", "pointer_type":
		// `T[]` and `T*` both wrap the element type in a `type` field, and
		// pointer_type NESTS (`int**` is pointer_type(pointer_type(int))),
		// which the recursion unwinds.
		//
		// pointer_type was added by #6771 review. It is a BEHAVIOUR CHANGE
		// from main, not a no-op: `Ns.Widget* p` used to reach the old
		// blocklist last resort, whose character set `" <>[]?,"` contains
		// neither `*` nor `:`, so `Ns.Widget*` was returned VERBATIM as a
		// type name and produced the unresolvable CALLS target
		// `Ns.Widget*.Delta`. Dropping it to a bare `Delta` would be worse
		// than either — a bare name falls into the resolver's bare-name
		// class and can be rewritten to any unrelated `Delta` — so the leaf
		// is resolved properly instead.
		if elem := typ.ChildByFieldName("type"); elem != nil {
			return leafTypeName(elem, src)
		}
		if first := typ.NamedChild(0); first != nil {
			return leafTypeName(first, src)
		}
	case "qualified_name", "alias_qualified_name":
		// `System.Text.StringBuilder` — the leaf is the rightmost SEGMENT,
		// which the grammar hands over directly as the `name` field of
		// qualified_name{qualifier, ".", name}. Taking it from the node
		// STRUCTURE is the whole point (#6771): the previous implementation
		// flattened the node with findAllNodes and indexed the last element,
		// but findAllNodes is a LIFO stack walk that visits the rightmost
		// child FIRST, so on the grammar's left-nested `A.B.C` it accumulated
		// [C, B, A] and returned `A` — the NAMESPACE ROOT — under a comment
		// claiming the opposite. Never re-derive this from a flattened
		// descendant list; that list's order is not source order.
		//
		// `name` is a _simple_name, so it can be a generic_name
		// (`A.B.Handler<int>`); recursing strips the type-argument list.
		//
		// alias_qualified_name shares this branch: `global::Foo` is
		// alias{global} "::" name{Foo}, the same `name` field. Only the
		// SINGLE-segment form is an alias_qualified_name — `global::A.B.Foo`
		// parses as a qualified_name whose qualifier happens to contain one,
		// so it was already handled.
		//
		// alias_qualified_name was added by #6771 review, and it is a
		// BEHAVIOUR CHANGE from main, not a no-op. csQualifiedLeaf's
		// blocklist contained ":" so csBaseTypeNames dropped `global::Foo`,
		// but csBaseTypeNames was its ONLY caller; the other 14 sites reached
		// leafTypeName's old blocklist, whose set `" <>[]?,"` has no ":", so
		// they returned `global::Foo` VERBATIM (`global::Foo.Alpha`). Leaving
		// it to the new identifier guard would drop it to a bare `Alpha`,
		// which is worse than either: a bare name joins the resolver's
		// bare-name class and can be rewritten to any unrelated `Alpha`.
		//
		// A `name`-less fallback was written and DELETED: the field is
		// mandatory in both grammars and no parse omitting it could be
		// constructed (`using A.;` yields a zero-width MISSING identifier and
		// exits via the `identifier` case, never reaching here), so the
		// branch was unreachable and survived every mutant.
		if leaf := typ.ChildByFieldName("name"); leaf != nil {
			return leafTypeName(leaf, src)
		}
	}
	// Last resort — use the raw text, but only when it is actually shaped
	// like a C# identifier. This is an ALLOW-list on purpose: the previous
	// guard rejected a BLOCKLIST of characters (`" <>[]?,"`), which admitted
	// every punctuation token nobody thought to enumerate — `:` contains none
	// of them and was returned as a type name.
	//
	// This is HARDENING, not the fix for an observed bug. No path from the 15
	// call sites is known to hand a bare punctuation node to this branch:
	// csBaseTypeNames walks NamedChild with `default: continue`, so the
	// anonymous ":" never reaches it, and every other site passes a
	// declaration's `type` field. What the allow-list buys is that the guard
	// no longer depends on that being true of every future caller.
	raw := strings.TrimSpace(string(src[typ.StartByte():typ.EndByte()]))
	if !isCsIdentifierText(raw) {
		return ""
	}
	return raw
}

// isCsIdentifierText reports whether s is a single C# identifier: an optional
// `@` verbatim prefix, then a letter or `_`, then letters, digits or `_`.
// Unicode letters are accepted — C# identifiers are not restricted to ASCII.
//
// Everything else is rejected, including the shapes a blocklist guard used to
// let through: punctuation (`:`, `::`, `=>`), operators, dotted qualified
// names, leading digits, and anything containing whitespace.
//
// leaf_type_name_6771_test.go enumerates the printable-ASCII token space
// against an independent oracle in BOTH directions, but that enumeration is
// BOUNDED AT TWO CHARACTERS: a loosening that only bites at index >= 2 (say,
// accepting `.` or `*` there) is invisible to it and is caught instead by the
// longhand negatives beside it, which include `"System.Text.StringBuilder"`.
func isCsIdentifierText(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '@' {
		s = s[1:]
	}
	for i, r := range s {
		switch {
		case r == '_' || unicode.IsLetter(r):
		case i > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return s != ""
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
