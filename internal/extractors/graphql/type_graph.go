// type_graph.go — SDL schema type→type graph (epic #3628, child of #3607 family).
//
// The base SDL extraction (graphql.go) emits one SCOPE.Schema node per object
// type / interface / input / enum / union and CONTAINS edges to each field. It
// does NOT model the *typed relationships between object types*: a field whose
// type is another object type (`orders: [Order!]!`) is a structural edge from
// the owning type to the referenced type — the schema's entity-relationship
// graph. That is a distinct gap from the resolver/endpoint work (#3607).
//
// This file adds GRAPH_RELATES edges (the same kind the ORM relational
// extractors use, #3611/#3747) between the *existing* type nodes graphql.go
// already emits — no parallel node is created. The edge is hung off the owning
// type node and points at the referenced type node, both addressed with the
// canonical structural ref BuildOperationStructuralRef("graphql", file, Name).
//
// Cardinality is captured from the field's GraphQL type expression:
//
//	[Order!]!   → list=true,  nullable=false (the list), item_nullable=false
//	[Order]     → list=true,  nullable=true,           item_nullable=true
//	Order!      → list=false, nullable=false
//	Order       → list=false, nullable=true
//
// Scalar fields (String/Int/Float/Boolean/ID and any non-object custom scalar)
// do NOT produce a type→type edge. enum/union/input target types: a field whose
// base type names a declared union is linked to the union's concrete members
// when those members are object types declared in the same file (honest-partial
// interface/union handling); an unresolved type name is skipped.
package graphql

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Built-in GraphQL scalars never make a type→type edge.
var builtinScalars = map[string]bool{
	"String": true, "Int": true, "Float": true, "Boolean": true, "ID": true,
}

// schemaTypes is the set of named definitions collected in a first pass over the
// source, used to resolve a field's base type to a real type node and to know
// its kind (object/interface/union/enum/input/scalar) so scalars and enums are
// correctly excluded from the type-graph.
type schemaTypes struct {
	kind          map[string]string   // type name → subtype (type/interface/union/enum/input/scalar)
	unionMembers  map[string][]string // union name → concrete member type names
	customScalars map[string]bool     // names declared via `scalar Foo`
}

// unionDefRE captures a union definition and its member list:
//
//	union SearchResult = User | Post | Comment
var unionDefRE = regexp.MustCompile(
	`(?m)^union\s+(\w+)\s*=\s*([^\n]+)`,
)

// collectSchemaTypes scans the whole source for every top-level named
// definition so the field-type resolver knows which names are object types
// (edge targets) vs scalars/enums (no edge). It is intentionally a cheap,
// single-pass regex scan mirroring typeDefRE in graphql.go.
func collectSchemaTypes(src string) schemaTypes {
	st := schemaTypes{
		kind:          make(map[string]string),
		unionMembers:  make(map[string][]string),
		customScalars: make(map[string]bool),
	}
	for _, m := range typeDefRE.FindAllStringSubmatchIndex(src, -1) {
		subtype := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		if _, ok := st.kind[name]; !ok {
			st.kind[name] = subtype
		}
		if subtype == "scalar" {
			st.customScalars[name] = true
		}
	}
	for _, m := range unionDefRE.FindAllStringSubmatch(src, -1) {
		name := m[1]
		var members []string
		for _, part := range strings.Split(m[2], "|") {
			mm := strings.TrimSpace(part)
			// stop a member list at a trailing directive / comment
			if i := strings.IndexAny(mm, " \t@#"); i >= 0 {
				mm = mm[:i]
			}
			if mm != "" {
				members = append(members, mm)
			}
		}
		st.unionMembers[name] = members
	}
	return st
}

// typeCardinality is the parsed shape of a GraphQL field type expression.
type typeCardinality struct {
	base         string // the named base type, e.g. "Order"
	list         bool   // wrapped in [...]
	nullable     bool   // the outermost value is nullable (no trailing !)
	itemNullable bool   // for lists, whether the inner item is nullable
}

// parseTypeExpr decomposes a GraphQL field type expression into its base type
// and list/nullable cardinality.
//
//	[Order!]!  → base=Order list=true  nullable=false itemNullable=false
//	[Order]    → base=Order list=true  nullable=true  itemNullable=true
//	Order!     → base=Order list=false nullable=false
//	Order      → base=Order list=false nullable=true
//
// Returns ok=false when no identifier base type can be recovered.
func parseTypeExpr(expr string) (typeCardinality, bool) {
	expr = strings.TrimSpace(expr)
	// Drop a trailing default-value / directive tail if any survived.
	if i := strings.IndexAny(expr, " \t="); i >= 0 {
		expr = expr[:i]
	}
	var tc typeCardinality
	if strings.HasPrefix(expr, "[") {
		tc.list = true
		// outer list nullability: trailing ! after the closing bracket
		closing := strings.LastIndex(expr, "]")
		if closing < 0 {
			return tc, false
		}
		outer := expr[closing+1:]
		tc.nullable = !strings.Contains(outer, "!")
		inner := strings.TrimSpace(expr[1:closing])
		tc.itemNullable = !strings.HasSuffix(inner, "!")
		inner = strings.TrimRight(inner, "!")
		tc.base = inner
	} else {
		tc.nullable = !strings.HasSuffix(expr, "!")
		tc.base = strings.TrimRight(expr, "!")
	}
	tc.base = strings.TrimSpace(tc.base)
	if !isTypeIdent(tc.base) {
		return tc, false
	}
	return tc, true
}

// isTypeIdent reports whether s is a bare GraphQL type identifier (letters /
// digits / underscore, leading non-digit). Rejects empty / wrapped remnants.
func isTypeIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' ||
			(c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9' && i > 0)
		if !ok {
			return false
		}
	}
	return true
}

// isObjectTarget reports whether a base type name is a valid type→type edge
// target: a declared object type or interface. Scalars (built-in or custom),
// enums and input objects are not edge targets in the entity-relationship
// graph. Unions are handled separately (expanded to members).
func (st schemaTypes) isObjectTarget(name string) bool {
	if builtinScalars[name] || st.customScalars[name] {
		return false
	}
	switch st.kind[name] {
	case "type", "interface":
		return true
	default:
		return false
	}
}

// typeGraphEdges returns the GRAPH_RELATES type→type edges for one object-type
// (or interface) definition named `owner`, given its parsed fields. The edges
// hang off the owner's type node (FromID = owner ref) and point at the
// referenced type node (ToID = target ref). Union-typed fields expand to one
// edge per concrete member resolvable in this file. Scalar / enum / input /
// unresolved targets are skipped.
func typeGraphEdges(owner, filePath string, fields []fieldHit, st schemaTypes) []types.RelationshipRecord {
	ownerRef := extractor.BuildOperationStructuralRef("graphql", filePath, owner)
	var out []types.RelationshipRecord
	seen := make(map[string]bool) // dedupe (field,target) pairs

	emit := func(target string, tc typeCardinality, fieldName string, viaUnion string) {
		key := fieldName + "→" + target
		if seen[key] {
			return
		}
		seen[key] = true
		props := map[string]string{
			"field_name":    fieldName,
			"list":          boolToString(tc.list),
			"nullable":      boolToString(tc.nullable),
			"cardinality":   cardinalityLabel(tc),
			"self_ref":      boolToString(target == owner),
			"graphql_field": owner + "." + fieldName,
		}
		if tc.list {
			props["item_nullable"] = boolToString(tc.itemNullable)
		}
		if viaUnion != "" {
			props["via_union"] = viaUnion
		}
		out = append(out, types.RelationshipRecord{
			FromID:     ownerRef,
			ToID:       extractor.BuildOperationStructuralRef("graphql", filePath, target),
			Kind:       string(types.RelationshipKindGraphRelates),
			Properties: props,
		})
	}

	for _, f := range fields {
		if f.typeExpr == "" {
			continue
		}
		tc, ok := parseTypeExpr(f.typeExpr)
		if !ok {
			continue
		}
		switch {
		case st.isObjectTarget(tc.base):
			emit(tc.base, tc, f.name, "")
		case st.kind[tc.base] == "union":
			// honest-partial: expand to concrete object members declared here.
			for _, member := range st.unionMembers[tc.base] {
				if st.isObjectTarget(member) {
					emit(member, tc, f.name, tc.base)
				}
			}
			// unresolved members are skipped (no edge).
		default:
			// scalar / enum / input / unresolved custom type → no edge.
		}
	}
	return out
}

// boolToString formats a bool as "true" / "false" for Properties storage,
// which is map[string]string.
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// cardinalityLabel maps a parsed cardinality to a short human label mirroring
// the ORM GRAPH_RELATES vocabulary so cross-language graph queries read
// uniformly. A list field is "to_many"; a singular object field is "to_one".
func cardinalityLabel(tc typeCardinality) string {
	if tc.list {
		return "to_many"
	}
	return "to_one"
}
