// types.go — Swift Type System extraction (#4913).
//
// The base swift.go tree-sitter walk recognises class / struct / enum /
// protocol declarations as SCOPE.Component nodes and `import` as IMPORTS,
// but it never modelled the Swift *type system* values: every SwiftUI /
// vapor framework record marks enum_extraction / type_alias_extraction as
// `missing`, and the only typealias support was the loose vapor-scoped
// reSwiftTypealias regex (vapor_extended.go) that emitted a bare
// SCOPE.Component(subtype="typealias") with no aliased-type body.
//
// This pass — the highest-value LANGUAGE-CORE gap in #4913 — adds, fixture
// proven, against the smacker/go-tree-sitter Swift grammar (node shapes
// confirmed by CST probe):
//
//   - enum value-set: an `enum` declaration is parsed (in addition to the
//     existing SCOPE.Component) into a SCOPE.Enum value-set via the shared
//     extractor.EnumEntity helper (kind_hint="swift_enum"). Each `enum_entry`
//     yields one member per `simple_identifier` (so `case south, east` →
//     two members); a `case x = <literal>` raw value is lifted to the
//     member value (integer/string/bool literals). Answers "what cases can
//     this enum take?" for cross-graph enum parity (#3628 / #4420),
//     matching the dart/python/ts/java value-sets.
//
//   - typealias → SCOPE.Schema(subtype="type_alias") with a type_body
//     property (the aliased type text after `=`), parity with the
//     python (internal/extractors/python/types.go) / rust / go / dart
//     type_alias shape. Tree-sitter `typealias_declaration` is used so the
//     full RHS — including function types `(Int) -> Void` and generic
//     `Result<T, E>` — is captured, superseding the vapor-only regex v1.
//
// Both run inside the existing walkNode dispatch (swift.go), so there is no
// double-walk and no double-emit: the enum value-set is emitted alongside
// (not instead of) the nominal Component, and typealias is a node type the
// base walk previously fell through.
package swift

import (
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildEnumValueSet parses an `enum` class_declaration into a SCOPE.Enum
// value-set carrying its case members. Returns false when the enum has no
// extractable cases (e.g. a generic-parameter-only forward decl).
func buildEnumValueSet(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	name := enumName(node, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := findClassBody(node)
	if body == nil {
		return types.EntityRecord{}, false
	}
	members := collectEnumCases(body, file.Content)
	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1
	return extractor.EnumEntity(name, "swift", "swift_enum", file.Path,
		startLine, endLine, members)
}

// enumName returns the type_identifier name child of an enum declaration.
func enumName(node ts.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch.Type() == "type_identifier" {
			return string(src[ch.StartByte():ch.EndByte()])
		}
	}
	return ""
}

// collectEnumCases walks the enum_class_body for enum_entry nodes and emits
// one EnumMember per declared case identifier. A `case a, b` group yields two
// members; a `case x = <literal>` raw value is lifted to the member value.
func collectEnumCases(body ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	for i := 0; i < int(body.ChildCount()); i++ {
		entry := body.Child(i)
		if entry.Type() != "enum_entry" {
			continue
		}
		line := int(entry.StartPoint().Row) + 1
		// Within an enum_entry the case identifiers are simple_identifier
		// children; an `=` introduces the raw value for the *single* preceding
		// identifier (Swift forbids raw values on comma-grouped cases, so a
		// grouped `case a, b` carries no `=`).
		var pending []string
		var rawValue string
		sawEq := false
		for j := 0; j < int(entry.ChildCount()); j++ {
			c := entry.Child(j)
			switch c.Type() {
			case "simple_identifier":
				pending = append(pending, string(src[c.StartByte():c.EndByte()]))
			case "=":
				sawEq = true
			default:
				if sawEq && rawValue == "" {
					rawValue = extractor.StripLiteralQuotes(
						strings.TrimSpace(string(src[c.StartByte():c.EndByte()])))
				}
			}
		}
		for _, id := range pending {
			v := ""
			// Raw value applies only when there is exactly one case in the
			// entry (Swift grammar guarantee).
			if sawEq && len(pending) == 1 {
				v = rawValue
			}
			members = append(members, extractor.EnumMember{Name: id, Value: v, Line: line})
		}
	}
	return members
}

// buildTypeAlias parses a `typealias_declaration` into a SCOPE.Schema
// type_alias record carrying the aliased type body.
func buildTypeAlias(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	var name, body string
	sawEq := false
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		switch ch.Type() {
		case "type_identifier":
			if name == "" {
				name = string(file.Content[ch.StartByte():ch.EndByte()])
			}
		case "=":
			sawEq = true
		default:
			if sawEq && body == "" && ch.Type() != "typealias" {
				body = strings.TrimSpace(string(file.Content[ch.StartByte():ch.EndByte()]))
			}
		}
	}
	if name == "" {
		return types.EntityRecord{}, false
	}
	line := int(node.StartPoint().Row) + 1
	props := map[string]string{"line": strconv.Itoa(line)}
	if body != "" && len(body) <= 512 {
		props["type_body"] = body
	}
	sig := "typealias " + name
	if body != "" {
		sig += " = " + body
	}
	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "type_alias",
		Language:           "swift",
		SourceFile:         file.Path,
		StartLine:          line,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		Properties:         props,
		EnrichmentRequired: false,
	}, true
}
