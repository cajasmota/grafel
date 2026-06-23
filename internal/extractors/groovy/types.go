// types.go — Groovy Type System extraction (#4914).
//
// The base groovy.go tree-sitter walk recognises class_declaration /
// class_definition (which also covers `interface`, since the smacker grammar
// parses `interface X {…}` as a class_definition) as SCOPE.Component nodes,
// `def`/typed methods as SCOPE.Operation, and `import` as IMPORTS — but it
// never modelled the Groovy *enum value set*. Every Groovy framework record
// (lang.groovy.framework.grails) marked enum_extraction `missing`, and there
// was no base language record at all.
//
// This pass — the highest-value LANGUAGE-CORE gap in #4914 — adds, fixture
// proven against the smacker/go-tree-sitter Groovy grammar (node shapes
// confirmed by CST probe), an enum value-set per `enum` declaration via the
// shared extractor.EnumEntity helper (kind_hint="groovy_enum"), matching the
// swift (#4913) / dart / python / java value-sets so a downstream cross-graph
// parity audit (#4420 / #3628) can diff the literal members without re-parsing
// source.
//
// Grammar shape (the smacker grammar does NOT have a dedicated enum node):
//
//	enum Color { RED, GREEN, BLUE }
//	  → declaration[ identifier("enum"), identifier("Color") ]   (the header)
//	    closure[ "{", ERROR[ parameter_list[ parameter[identifier]… ] ], "}" ]
//
//	enum Status { ACTIVE(1), INACTIVE(0) }
//	  → declaration[ identifier("enum"), identifier("Status") ]
//	    closure[ "{", function_call[ identifier("ACTIVE"), argument_list[…] ]… ]
//
// The header `declaration` and the body `closure` are SIBLINGS in the CST, so
// the walk pairs an `enum`-headed declaration with its immediately following
// `closure` sibling. Members are collected from the closure in two forms:
//
//   - parameter_list > parameter > identifier   (bare constants RED, GREEN)
//   - function_call  > identifier + argument_list  (valued constants ACTIVE(1),
//     HEARTS('red')) — the single literal argument (int/float/string/bool) is
//     lifted to the member value (StripLiteralQuotes).
//
// To separate enum CONSTANTS from enum BODY members (fields / constructors —
// e.g. `double mass; Planet(double m){…}`), member collection STOPS at the
// first `declaration` (field) child of the closure: in valid Groovy every
// constant precedes any field/method/constructor, so the trailing
// constructor `function_call` (`Planet(...)`) is never mis-counted as a value.
package groovy

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// buildGroovyEnumValueSet pairs an `enum`-headed declaration node with its
// following `closure` sibling and emits a SCOPE.Enum value-set entity. Returns
// ok=false when the node is not an enum header, has no following closure, or
// yields zero parseable constants.
//
// parent is the node whose child list contains both `decl` (at index declIdx)
// and the body closure — i.e. the enum's lexical container (source_file, a
// class closure for a nested enum, …).
func buildGroovyEnumValueSet(parent ts.Node, declIdx int, file extractor.FileInput) (types.EntityRecord, bool) {
	decl := parent.Child(declIdx)
	if decl == nil {
		return types.EntityRecord{}, false
	}
	name := enumHeaderName(decl, file.Content)
	if name == "" {
		return types.EntityRecord{}, false
	}
	body := nextClosureSibling(parent, declIdx)
	if body == nil {
		return types.EntityRecord{}, false
	}
	members := collectGroovyEnumMembers(body, file.Content)
	if len(members) == 0 {
		return types.EntityRecord{}, false
	}
	return extractor.EnumEntity(
		name, "groovy", "groovy_enum", file.Path,
		int(decl.StartPoint().Row)+1, int(body.EndPoint().Row)+1,
		members,
	)
}

// enumHeaderName returns the enum type name when decl is an `enum X` header
// (`declaration[ identifier("enum"), identifier("X") ]`), or "" otherwise.
func enumHeaderName(decl ts.Node, src []byte) string {
	if decl == nil || decl.Type() != "declaration" {
		return ""
	}
	var ids []string
	for i := 0; i < int(decl.ChildCount()); i++ {
		ch := decl.Child(i)
		if ch != nil && ch.Type() == "identifier" {
			ids = append(ids, nodeText(ch, src))
		}
	}
	if len(ids) < 2 || ids[0] != "enum" {
		return ""
	}
	return ids[1]
}

// nextClosureSibling returns the first `closure` node that follows declIdx in
// parent's child list, skipping anonymous separators, or nil.
func nextClosureSibling(parent ts.Node, declIdx int) ts.Node {
	for i := declIdx + 1; i < int(parent.ChildCount()); i++ {
		ch := parent.Child(i)
		if ch == nil {
			continue
		}
		if ch.Type() == "closure" {
			return ch
		}
		// A non-closure declaration/statement before any closure means this
		// enum header has no body block — bail.
		if ch.Type() == "declaration" || ch.Type() == "class_definition" {
			return nil
		}
	}
	return nil
}

// collectGroovyEnumMembers walks the enum body closure for constant members.
// Collection stops at the first `declaration` (an enum field like
// `double mass`), so the trailing constructor `function_call` is never counted.
func collectGroovyEnumMembers(body ts.Node, src []byte) []extractor.EnumMember {
	var members []extractor.EnumMember
	seen := map[string]bool{}
	add := func(name, value string, line int) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		members = append(members, extractor.EnumMember{Name: name, Value: value, Line: line})
	}

	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "declaration":
			// First enum-body field/constructor declaration → constants are done.
			return members
		case "function_call":
			// Valued constant: identifier(NAME) argument_list[ literal ].
			name, value := groovyEnumValuedConst(ch, src)
			if name != "" {
				add(name, value, int(ch.StartPoint().Row)+1)
			}
		case "ERROR":
			// Bare constants surface inside an ERROR node as a parameter_list.
			collectEnumBareConsts(ch, src, add)
		case "parameter_list":
			collectEnumBareConsts(ch, src, add)
		}
	}
	return members
}

// groovyEnumValuedConst extracts (name, value) from a `function_call` enum
// constant such as `ACTIVE(1)` or `HEARTS('red')`. The value is the single
// leading literal argument (number/string/bool); multi-arg or non-literal
// constructors keep the constant but drop the value.
func groovyEnumValuedConst(fc ts.Node, src []byte) (string, string) {
	nameNode := childByType(fc, "identifier")
	if nameNode == nil {
		return "", ""
	}
	name := nodeText(nameNode, src)
	if name == "" {
		return "", ""
	}
	argList := childByType(fc, "argument_list")
	if argList == nil {
		return name, ""
	}
	for i := 0; i < int(argList.ChildCount()); i++ {
		ch := argList.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "number_literal":
			return name, nodeText(ch, src)
		case "string":
			return name, groovyStringLiteralContent(ch, src)
		case "true", "false":
			return name, nodeText(ch, src)
		}
	}
	return name, ""
}

// collectEnumBareConsts walks a parameter_list (possibly the direct node or a
// descendant of an ERROR node) collecting bare constant identifiers.
func collectEnumBareConsts(node ts.Node, src []byte, add func(name, value string, line int)) {
	for _, pl := range findAllNodes(node, "parameter_list") {
		for i := 0; i < int(pl.ChildCount()); i++ {
			p := pl.Child(i)
			if p == nil || p.Type() != "parameter" {
				continue
			}
			id := childByType(p, "identifier")
			if id == nil {
				continue
			}
			add(nodeText(id, src), "", int(id.StartPoint().Row)+1)
		}
	}
}

// groovyStringLiteralContent returns the inner content of a `string` node,
// stripping the surrounding quote tokens.
func groovyStringLiteralContent(node ts.Node, src []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		ch := node.Child(i)
		if ch != nil && ch.Type() == "string_content" {
			return nodeText(ch, src)
		}
	}
	return strings.Trim(nodeText(node, src), `'"`)
}
