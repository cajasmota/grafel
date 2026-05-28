// Nim def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : `var x = ...`, `let x = ...`, `const x = ...` (local bindings),
//            `var x: T = ...`, bare `x = ...` reassignments.
//   - Uses : bare identifiers `\b<name>\b` filtered against Nim keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `proc/func/method/template/macro`
// header using the scanNimFuncHeaders helper from effect_sinks_nim.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("nim", sniffDefUseNim) }

// nimDefRe matches local variable / constant declarations and reassignments.
//
//	Group 1 (var/let/const form): `(var|let|const) name [: T] = ...`
//	Group 2 (bare assignment)   : `name = ...` (not `==`)
var nimDefRe = regexp.MustCompile(
	`(?m)(?:\b(?:var|let|const)\s+([A-Za-z_][\w`+"`"+`]*)\s*(?::\s*[A-Za-z_][\w\[\], ]*\s*)?=(?:[^=])` +
		`|^\s*([A-Za-z_][\w`+"`"+`]*)\s*=(?:[^=]))`,
)

var nimIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w` + "`" + `]*)\b`)

func nimReservedDefUse(s string) bool {
	switch s {
	case "if", "elif", "else", "when", "case", "of", "for", "while", "do",
		"break", "continue", "return", "raise", "try", "except", "finally",
		"let", "var", "const", "proc", "func", "method", "template", "macro",
		"iterator", "type", "import", "from", "export", "include", "discard",
		"in", "notin", "is", "isnot", "not", "and", "or", "xor", "shl", "shr",
		"div", "mod", "true", "false", "nil", "result", "self", "this",
		"string", "int", "float", "bool", "char", "seq", "array", "tuple",
		"object", "ref", "ptr", "addr", "cast", "sizeof", "echo", "write":
		return true
	}
	return false
}

func sniffDefUseNim(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanNimFuncHeaders(content)

	var defs []VarDef
	for _, m := range nimDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || nimReservedDefUse(name) {
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
	for _, m := range nimIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if nimReservedDefUse(name) {
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
