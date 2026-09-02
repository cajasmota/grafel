package csharp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/types"
)

// hierarchy.go — C# class-hierarchy edges (issue #6742).
//
// THE GAP. Before this file the C# extractor emitted a hierarchy edge only
// when the base type was declared in the SAME FILE, and always as EXTENDS.
// `class ReportJob : IJob`, `class UsersController : ControllerBase` and
// `class UserService : IUserService` — the three shapes the golden C#
// fixtures actually contain — produced nothing at all, while Java's
// structurally identical `class SendEmailJob implements Job` produced an
// IMPLEMENTS edge (internal/extractors/java/java.go). "What implements this
// interface?" therefore returned empty for C#, indistinguishable from "no
// implementations exist".
//
// THE VOCABULARY. Exactly the two kinds Java already emits — EXTENDS and
// IMPLEMENTS (types.RelationshipKindExtends / RelationshipKindImplements). No
// C#-specific kind: internal/extractors/hierarchy_parity_6742_test.go runs
// both extractors over the same structural shape and fails if the two ever
// disagree on the kind.
//
// A COLON IN C# MEANS SEVERAL DIFFERENT THINGS. Only ONE of them is a base
// list, and the grammar — not a text scan — is what tells them apart:
//
//	class C : B              base_list                          → edge
//	class C<T> where T : IF  type_parameter_constraints_clause  → NO edge
//	enum E : byte            base_list holding a predefined_type → NO edge
//	x = c ? A : B            conditional_expression             → NO edge
//	case 1:  /  label:       switch_section / labeled_statement → NO edge
//
// The negative cases are pinned in hierarchy_6742_test.go and, because recall
// is structurally blind to over-firing, again as forbidden_relationships rows
// on the golden fixtures.
//
// WHICH KIND. C# writes a base class and its interfaces in one undifferentiated
// base_list, so the kind has to be decided. In order of authority:
//
//  1. A struct can never have a base class → every entry is IMPLEMENTS.
//  2. An interface can only inherit interfaces → every entry is EXTENDS
//     (interface inheritance is `extends` in Java too).
//  3. C# requires the base class, if present, to be the FIRST entry → every
//     entry after the first is IMPLEMENTS, by language rule.
//  4. For the first entry of a class/record: if the type is declared in this
//     same file, its own declaration keyword decides.
//  5. Otherwise the .NET `I`+PascalCase interface naming convention decides.
//     This is the only guess, and it is confined to first-position,
//     out-of-file base types.

// csBaseTypeNames returns the ordered base-type leaf names of a class /
// record / struct / interface declaration's base_list.
//
// It walks NAMED children only and accepts only type-shaped node kinds, which
// is what keeps punctuation, base-constructor arguments and an enum's
// underlying storage type out of the result:
//
//	class P(int x) : B(x)   base_list = [identifier B, argument_list (x)]
//	record R(int X) : Rb(X) base_list = [primary_constructor_base_type Rb(X)]
//	enum E : byte           base_list = [predefined_type byte]
//
// leafTypeName's last-resort branch returns raw text whenever it contains none
// of " <>[]?," — which the bare ":" token passes — so the type allow-list here
// is load-bearing, not defensive.
func csBaseTypeNames(node ts.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	bl := findChildByType(node, "base_list")
	if bl == nil {
		return nil
	}
	var out []string
	for i := 0; i < int(bl.NamedChildCount()); i++ {
		ch := bl.NamedChild(i)
		if ch == nil {
			continue
		}
		var name string
		switch ch.Type() {
		case "qualified_name", "alias_qualified_name":
			// `Microsoft.AspNetCore.Mvc.ControllerBase` — the supertype is
			// the RIGHTMOST segment. leafTypeName's qualified_name branch
			// walks with findAllNodes, whose stack traversal does not return
			// identifiers in source order, so it is not usable here.
			name = csQualifiedLeaf(nodeText(ch, src))
		case "identifier", "generic_name", "nullable_type":
			name = leafTypeName(ch, src)
		case "primary_constructor_base_type":
			// `record R(int X) : Rb(X)` — the supertype is the node's own
			// type child; the argument_list beside it is call arguments.
			for j := 0; j < int(ch.NamedChildCount()); j++ {
				sub := ch.NamedChild(j)
				if sub == nil || sub.Type() == "argument_list" {
					continue
				}
				if n := leafTypeName(sub, src); n != "" {
					name = n
					break
				}
			}
		default:
			// argument_list (primary-constructor base args), predefined_type
			// (an enum's underlying storage type), and anything else the
			// grammar may park here are not supertypes.
			continue
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// csQualifiedLeaf reduces a dotted type reference to its rightmost segment and
// strips any generic type-argument list: `A.B.Base<T>` → "Base". An empty
// string is returned when the result is not a plain identifier.
func csQualifiedLeaf(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "<"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " <>[]?,.:()") {
		return ""
	}
	return s
}

// csLooksLikeInterfaceName reports whether name follows the .NET interface
// naming convention: a capital `I` followed by another upper-case letter
// (IJob, IUserService, IComparable). `Item` and `Invoice` are not matched.
//
// This is the LAST resort in the kind decision — reached only for the first
// entry of a class or record base list whose type is not declared in this
// file. Every other position is settled by a C# language rule or by the base
// type's own in-file declaration keyword.
func csLooksLikeInterfaceName(name string) bool {
	if len(name) < 2 || name[0] != 'I' {
		return false
	}
	c := name[1]
	return c >= 'A' && c <= 'Z'
}

// attachCsharpHierarchy emits one EXTENDS or IMPLEMENTS edge per base-list
// entry of every type declared in the file, using the ordered base names and
// declaration keyword each owner stashed on its Metadata during the walk.
//
// The edge target is the bare leaf type name, the same resolvable form every
// other C# edge target uses: it binds to the in-file declaration when there is
// one and to the cross-file class entity through the resolver's byName index
// otherwise, and stays a legible bare name when the supertype is external
// (IJob, ControllerBase) and no entity exists to bind to at all.
//
// FromID is left empty so the edge is anchored to the record that carries it,
// as the walk's own CONTAINS edges are. The pre-#6742 pass stamped the owner's
// bare NAME as FromID, which is ambiguous the moment two files declare the
// same type name.
func attachCsharpHierarchy(records []types.EntityRecord) []types.EntityRecord {
	// Declaration keyword of every type declared in this file, so an in-file
	// base type decides its own edge kind.
	declKind := make(map[string]string)
	for i := range records {
		if records[i].Kind != "SCOPE.Component" {
			continue
		}
		switch records[i].Subtype {
		case "class", "interface", "struct", "type":
			declKind[records[i].Name] = records[i].Subtype
		}
	}

	for i := range records {
		if records[i].Kind != "SCOPE.Component" || records[i].Metadata == nil {
			continue
		}
		bases, _ := records[i].Metadata["hierarchy_bases"].([]string)
		ownerDecl, _ := records[i].Metadata["hierarchy_decl"].(string)
		delete(records[i].Metadata, "hierarchy_bases")
		delete(records[i].Metadata, "hierarchy_decl")
		if len(bases) == 0 {
			continue
		}
		owner := records[i].Name
		for pos, base := range bases {
			if base == "" || base == owner {
				continue
			}
			kind := csHierarchyKind(ownerDecl, pos, base, declKind)
			dup := false
			for _, ex := range records[i].Relationships {
				if ex.Kind == kind && ex.ToID == base {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			records[i].Relationships = append(records[i].Relationships,
				types.RelationshipRecord{
					ToID: base,
					Kind: kind,
				})
		}
	}
	return records
}

// csHierarchyKind decides EXTENDS vs IMPLEMENTS for the base-list entry at
// index pos of a declaration of kind ownerDecl. See the rule ladder at the top
// of this file; declKind carries the declaration keyword of every type
// declared in the same file.
func csHierarchyKind(ownerDecl string, pos int, base string, declKind map[string]string) string {
	switch ownerDecl {
	case "interface":
		// An interface can only inherit interfaces, and that relation is
		// `extends` — the same word Java uses for it.
		return string(types.RelationshipKindExtends)
	case "struct":
		// A struct has no base class; every entry is an interface.
		return string(types.RelationshipKindImplements)
	}
	if pos > 0 {
		// C# requires the base class, when present, to be written first.
		return string(types.RelationshipKindImplements)
	}
	switch declKind[base] {
	case "interface":
		return string(types.RelationshipKindImplements)
	case "class", "struct", "type":
		return string(types.RelationshipKindExtends)
	}
	if csLooksLikeInterfaceName(base) {
		return string(types.RelationshipKindImplements)
	}
	return string(types.RelationshipKindExtends)
}

// csDeclKeyword maps a tree-sitter declaration node type to the Subtype the
// walk stamps on the emitted SCOPE.Component, so attachCsharpHierarchy can
// apply the per-declaration rules above.
func csDeclKeyword(nodeType string) string {
	switch nodeType {
	case "interface_declaration":
		return "interface"
	case "struct_declaration":
		return "struct"
	case "record_declaration":
		return "record"
	case "class_declaration":
		return "class"
	}
	return strings.TrimSuffix(nodeType, "_declaration")
}
