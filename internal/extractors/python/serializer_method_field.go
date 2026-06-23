// serializer_method_field.go — DRF SerializerMethodField → method link.
//
// Issue #2008 — When a DRF serializer declares
//
//	class FooSerializer(serializers.ModelSerializer):
//	    full_name = serializers.SerializerMethodField()
//	    def get_full_name(self, obj):
//	        return f"{obj.first} {obj.last}"
//
// the per-attribute SCOPE.Schema/field entity ("FooSerializer.full_name")
// has no typed edge to the implementing method ("FooSerializer.get_full_name").
// Without that link the docgen / NeighbourBrief pass can't enumerate the
// field's computation source, and graph queries against the field never
// reach the method that produces its value.
//
// DRF supports two forms:
//
//  1. Implicit naming: `<field> = SerializerMethodField()` resolves to
//     a sibling method `get_<field>(self, obj)`.
//  2. Explicit naming: `<field> = SerializerMethodField(method_name="custom")`
//     resolves to a sibling method `custom(self, obj)`.
//
// This pass walks every class body, looking for the SerializerMethodField
// shape at top-level (class-body) assignments, then emits a RESOLVED_BY
// edge from the field entity to the sibling method's structural-ref.
//
// "RESOLVED_BY" is consistent with edge nomenclature used by the
// engine layer for "this node's value is produced by that node"
// relationships. The edge carries `serializer_method_field=true` so
// graph audits can isolate this provenance without parsing source.
//
// Cap: at most one RESOLVED_BY edge per (field, method) pair. If the
// declared method does not exist as a sibling, the pass emits nothing
// (we don't fabricate the method; the absence is itself signal).

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitSerializerMethodFieldLinks walks every class_definition once and,
// for every `<field> = serializers.SerializerMethodField(...)` (or a
// suffix match — `*.SerializerMethodField`) declared in the class body,
// emits a RESOLVED_BY edge from the field's SCOPE.Schema entity to the
// matching method's structural-ref.
//
// Mutates *entities in place; safe with nil/empty inputs.
func emitSerializerMethodFieldLinks(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			nameNode := n.ChildByFieldName("name")
			cls := ""
			if nameNode != nil {
				cls = nodeText(nameNode, file.Content)
			}
			dotted := cls
			if parentClass != "" && cls != "" {
				dotted = parentClass + "." + cls
			}
			body := n.ChildByFieldName("body")
			if body != nil {
				scanSerializerBody(body, file, dotted, entities)
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), dotted)
				}
			}
			return
		case "decorated_definition":
			inner := n.ChildByFieldName("definition")
			if inner != nil {
				walk(inner, parentClass)
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")
}

// scanSerializerBody iterates immediate class-body statements, detects
// SerializerMethodField declarations, resolves the implementing method
// name (`method_name=` kwarg or default `get_<field>`), confirms the
// sibling method exists as a SCOPE.Operation entity, and stamps the
// RESOLVED_BY edge.
func scanSerializerBody(body ts.Node, file extractor.FileInput, parentClass string, entities *[]types.EntityRecord) {
	if body == nil || parentClass == "" {
		return
	}
	// Index this class's operation method names once; cheap linear pass.
	methodNames := make(map[string]bool)
	prefix := parentClass + "."
	for i := range *entities {
		e := &(*entities)[i]
		if e.SourceFile != file.Path {
			continue
		}
		if e.Kind != "SCOPE.Operation" {
			continue
		}
		if !strings.HasPrefix(e.Name, prefix) {
			continue
		}
		leaf := e.Name[len(prefix):]
		if leaf == "" || strings.Contains(leaf, ".") {
			// Skip methods of nested classes that share the prefix
			// (e.g. "FooSerializer.Meta.something").
			continue
		}
		methodNames[leaf] = true
	}

	for i := 0; i < int(body.ChildCount()); i++ {
		stmt := body.Child(i)
		if stmt == nil || stmt.Type() != "expression_statement" {
			continue
		}
		for j := 0; j < int(stmt.NamedChildCount()); j++ {
			expr := stmt.NamedChild(j)
			if expr == nil || expr.Type() != "assignment" {
				continue
			}
			lhs := expr.ChildByFieldName("left")
			rhs := expr.ChildByFieldName("right")
			if lhs == nil || rhs == nil || lhs.Type() != "identifier" || rhs.Type() != "call" {
				continue
			}
			attr := nodeText(lhs, file.Content)
			if attr == "" {
				continue
			}
			funcNode := rhs.ChildByFieldName("function")
			if funcNode == nil {
				continue
			}
			funcText := nodeText(funcNode, file.Content)
			leafType := funcText
			if dot := strings.LastIndexByte(leafType, '.'); dot >= 0 {
				leafType = leafType[dot+1:]
			}
			if leafType != "SerializerMethodField" {
				continue
			}
			methodName := lookupSerializerMethodName(rhs, file.Content)
			if methodName == "" {
				methodName = "get_" + attr
			}
			if !methodNames[methodName] {
				// The declared method doesn't exist in this class —
				// don't fabricate the edge. The pass stays a no-op
				// here; the absence remains signal.
				continue
			}
			// Locate the field entity ("<parentClass>.<attr>") to mutate.
			fieldName := parentClass + "." + attr
			fieldIdx := -1
			for k := range *entities {
				e := &(*entities)[k]
				if e.SourceFile == file.Path &&
					e.Kind == "SCOPE.Schema" &&
					e.Subtype == "field" &&
					e.Name == fieldName {
					fieldIdx = k
					break
				}
			}
			if fieldIdx < 0 {
				continue
			}
			methodEmitted := parentClass + "." + methodName
			toID := extractor.BuildOperationStructuralRef("python", file.Path, methodEmitted)
			// Dedup: only emit one RESOLVED_BY per (field, method) pair.
			already := false
			for _, r := range (*entities)[fieldIdx].Relationships {
				if r.Kind == "RESOLVED_BY" && r.ToID == toID {
					already = true
					break
				}
			}
			if already {
				continue
			}
			(*entities)[fieldIdx].Relationships = append((*entities)[fieldIdx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "RESOLVED_BY",
					Properties: map[string]string{
						"serializer_method_field": "true",
						"method_name":             methodName,
					},
				})
		}
	}
}

// lookupSerializerMethodName returns the value of the `method_name=`
// kwarg of a SerializerMethodField call, with surrounding quotes
// stripped. Returns "" when the kwarg is absent or its value isn't a
// string literal.
func lookupSerializerMethodName(callNode ts.Node, src []byte) string {
	argsNode := callNode.ChildByFieldName("arguments")
	if argsNode == nil {
		return ""
	}
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		arg := argsNode.Child(i)
		if arg == nil || arg.Type() != "keyword_argument" {
			continue
		}
		keyNode := arg.ChildByFieldName("name")
		valNode := arg.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		if nodeText(keyNode, src) != "method_name" {
			continue
		}
		if valNode.Type() != "string" {
			return ""
		}
		return stripQuotes(strings.TrimSpace(nodeText(valNode, src)))
	}
	return ""
}
