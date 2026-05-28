// Kotlin def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `val <name> = ...`, `var <name> = ...`, optional type
//            ascription `: T`, bare reassignments `<name> = ...`,
//            `for (<name> in ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against Kotlin
//            keywords and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `fun` header via the
// scanKotlinFuncHeaders helper from effect_sinks_kotlin.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("kotlin", sniffDefUseKotlin) }

// kotlinDefRe matches val/var-decls, bare assignments, and for-loops.
//   Group 1 (val/var)  : `(val|var) <name>[: T]`.
//   Group 2 (bare)     : `<name> =`.
//   Group 3 (for-in)   : `for (<name> in`.
var kotlinDefRe = regexp.MustCompile(
	`\b(?:val|var)\s+([A-Za-z_][\w]*)\b` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\bfor\s*\(\s*([A-Za-z_][\w]*)\s+in\b`,
)

var kotlinIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func kotlinReservedDefUse(s string) bool {
	switch s {
	case "as", "break", "class", "continue", "do", "else", "false", "for",
		"fun", "if", "in", "interface", "is", "null", "object", "package",
		"return", "super", "this", "throw", "true", "try", "typealias",
		"typeof", "val", "var", "when", "while", "by", "catch", "constructor",
		"delegate", "dynamic", "field", "file", "finally", "get", "import",
		"init", "param", "property", "receiver", "set", "setparam", "value",
		"where", "actual", "abstract", "annotation", "companion", "const",
		"crossinline", "data", "enum", "expect", "external", "final",
		"infix", "inline", "inner", "internal", "lateinit", "noinline",
		"open", "operator", "out", "override", "private", "protected",
		"public", "reified", "sealed", "suspend", "tailrec", "vararg",
		"String", "Int", "Long", "Short", "Byte", "Char", "Boolean",
		"Float", "Double", "List", "Map", "Set", "Array", "Unit", "Any",
		"Nothing", "println", "print":
		return true
	}
	return false
}

func sniffDefUseKotlin(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanKotlinFuncHeaders(content)

	var defs []VarDef
	for _, m := range kotlinDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case m[6] >= 0:
			name = content[m[6]:m[7]]
		}
		if name == "" || kotlinReservedDefUse(name) {
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
	for _, m := range kotlinIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if kotlinReservedDefUse(name) {
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
