// Swift def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : `let x = ...`, `var x = ...` (local bindings inside functions),
//            `let x: T = ...`, bare `x = ...` reassignments.
//   - Uses : bare identifiers `\b<name>\b` filtered against Swift keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `func name(` header using the
// scanSwiftFuncHeaders helper from effect_sinks_swift.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("swift", sniffDefUseSwift) }

// swiftDefRe matches local variable / constant declarations and reassignments.
//
//	Group 1 (let/var form)   : `[let|var] name [: T] = ...`
//	Group 2 (bare assignment): `name = ...` (not `==`)
var swiftDefRe = regexp.MustCompile(
	`(?m)(?:\b(?:let|var)\s+([A-Za-z_][\w]*)\s*(?::\s*[A-Za-z_][\w<>\[\],?\s.!]*\s*)?=(?:[^=])` +
		`|^\s*([A-Za-z_][\w]*)\s*=(?:[^=]))`,
)

var swiftIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func swiftReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "while", "repeat", "do", "switch", "case", "default",
		"break", "continue", "return", "throw", "try", "catch", "guard", "defer",
		"let", "var", "class", "struct", "enum", "protocol", "extension", "func",
		"init", "deinit", "subscript", "typealias", "import", "associatedtype",
		"where", "in", "is", "as", "nil", "true", "false", "self", "Self", "super",
		"static", "override", "final", "lazy", "weak", "unowned", "private",
		"fileprivate", "internal", "public", "open", "mutating", "nonmutating",
		"required", "optional", "indirect", "inout", "operator", "precedencegroup",
		"async", "await", "actor", "nonisolated", "rethrows", "throws",
		"String", "Int", "Double", "Float", "Bool", "Array", "Dictionary", "Set",
		"Optional", "Any", "AnyObject", "Void", "Error", "print", "debugPrint":
		return true
	}
	return false
}

func sniffDefUseSwift(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanSwiftFuncHeaders(content)

	var defs []VarDef
	for _, m := range swiftDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		}
		if name == "" || swiftReservedDefUse(name) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		defs = append(defs, VarDef{Function: fn, Line: line, Var: name})
	}

	defOnLine := map[int]map[string]bool{}
	for _, d := range defs {
		set := defOnLine[d.Line]
		if set == nil {
			set = map[string]bool{}
			defOnLine[d.Line] = set
		}
		set[d.Var] = true
	}

	var uses []VarUse
	for _, m := range swiftIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if swiftReservedDefUse(name) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		if defOnLine[line] != nil && defOnLine[line][name] {
			continue
		}
		uses = append(uses, VarUse{Function: fn, Line: line, Var: name})
	}
	return defs, uses
}
