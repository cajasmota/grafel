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
	name := ""
	switch {
	case decl.Type() == "declaration":
		// Legacy smacker: declaration[ identifier(enum), identifier(Name) ].
		name = enumHeaderName(decl, file.Content)
	case decl.Type() == "identifier" && nodeText(decl, file.Content) == "enum":
		// Current official grammar: bare `enum` identifier; the type name is
		// the next `identifier` sibling.
		name = nextIdentifierSibling(parent, declIdx, file.Content)
	}
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

// nextIdentifierSibling returns the text of the first `identifier` node that
// follows declIdx in parent's child list (the enum type name in the official
// grammar's bare-identifier header shape), or "" if a `closure` is reached
// first.
func nextIdentifierSibling(parent ts.Node, declIdx int, src []byte) string {
	for i := declIdx + 1; i < int(parent.ChildCount()); i++ {
		ch := parent.Child(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "identifier":
			return nodeText(ch, src)
		case "closure":
			return ""
		}
	}
	return ""
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

	// The regenerated (official) Groovy grammar has no enum rule, so a valued
	// enum body parses into a contorted token soup, e.g.
	//   `ACTIVE(1), INACTIVE(0)` →
	//     juxt_function_call(identifier ACTIVE,
	//        argument_list( (1) , identifier INACTIVE ))
	//     parenthesized_expression( 0 )
	// i.e. constant names and their parenthesised values are interleaved
	// across sibling nodes and out of their natural nesting. To recover the
	// members reliably we flatten the body into an ordered stream of
	// "name" / "value" events (by source position) and pair each constant
	// name with the value literal that immediately follows it, stopping at
	// the first field/constructor declaration. The legacy smacker shapes
	// (function_call / ERROR>parameter_list / parameter_list) still resolve
	// through the same event extraction.
	type ev struct {
		isName bool
		text   string
		line   int
	}
	var events []ev
	var collect func(n ts.Node, atBodyTop bool) bool // returns false → stop (hit a field/ctor)
	collect = func(n ts.Node, atBodyTop bool) bool {
		if n == nil {
			return true
		}
		switch n.Type() {
		case "declaration":
			// Enum field (`double mass`) — constants are done.
			return false
		case "string":
			events = append(events, ev{false, groovyStringLiteralContent(n, src), int(n.StartPoint().Row) + 1})
			return true
		case "number_literal", "true", "false":
			events = append(events, ev{false, nodeText(n, src), int(n.StartPoint().Row) + 1})
			return true
		case "function_call":
			// A function_call at body top with a closure arg is the enum
			// constructor (`Planet(double m){…}`) — stop. Otherwise it is a
			// legacy valued constant.
			if atBodyTop && childByType(n, "closure") != nil {
				return false
			}
			name, value := groovyEnumValuedConst(n, src)
			if name != "" {
				events = append(events, ev{true, name, int(n.StartPoint().Row) + 1})
				if value != "" {
					events = append(events, ev{false, value, int(n.StartPoint().Row) + 1})
				}
			}
			return true
		case "identifier":
			// A bare constant name. Builtin type keywords never reach here as
			// bare identifiers at enum-constant position.
			events = append(events, ev{true, nodeText(n, src), int(n.StartPoint().Row) + 1})
			return true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			if !collect(n.Child(i), false) {
				return false
			}
		}
		return true
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		if !collect(body.Child(i), true) {
			break
		}
	}

	// Pair each name with the value literal immediately following it.
	for i := 0; i < len(events); i++ {
		if !events[i].isName {
			continue
		}
		value := ""
		if i+1 < len(events) && !events[i+1].isName {
			value = events[i+1].text
		}
		add(events[i].text, value, events[i].line)
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
