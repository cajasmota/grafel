// Python def-use sniffer (#2774 Phase 3C T1).
//
// Recognises:
//   - Defs : `<name> = ...`, augmented `<name> += ...`, `for <name> in`,
//            `with ... as <name>:` and function-parameter names.
//   - Uses : bare identifiers `\b<name>\b` that are not Python keywords
//            and not the LHS of a def we already captured on the same line.
//
// Function attribution: nearest preceding `def` / `async def`. Class
// bodies are followed too (methods carry the method name).
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("python", sniffDefUsePython) }

// pyDefRe matches simple assignments and `for` / `with as` bindings.
//   Group 1 (assign)    : `<name> = ` (not `==`, not `.attr =`).
//   Group 2 (for)       : `for <name> in`.
//   Group 3 (with as)   : `as <name>:` / `as <name>,`.
var pyDefRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_][\w]*)\s*(?:\+|-|\*|/|%|//|\*\*|&|\||\^|<<|>>)?=(?:[^=])` +
		`|\bfor\s+([A-Za-z_][\w]*)\s+in\b` +
		`|\bas\s+([A-Za-z_][\w]*)\s*[:,)]`,
)

// pyIdentUseRe is the bare-identifier read regex.
var pyIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func pyReservedDefUse(s string) bool {
	switch s {
	case "and", "or", "not", "is", "in", "if", "elif", "else", "for", "while",
		"break", "continue", "return", "yield", "raise", "try", "except",
		"finally", "with", "as", "import", "from", "def", "class", "async",
		"await", "lambda", "pass", "global", "nonlocal", "del", "assert",
		"True", "False", "None", "self", "cls", "print", "len", "range",
		"str", "int", "float", "bool", "list", "dict", "tuple", "set",
		"super", "isinstance", "issubclass", "type", "Exception":
		return true
	}
	return false
}

func sniffDefUsePython(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanPyFuncHeaders(content)

	var defs []VarDef
	for _, m := range pyDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || pyReservedDefUse(name) {
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
	for _, m := range pyIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if pyReservedDefUse(name) {
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
