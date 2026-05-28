// JS/TS def-use sniffer (#2774 Phase 3C T1).
//
// Recognises:
//   - Defs : `let x = ...`, `const x = ...`, `var x = ...`,
//            simple `x = ...` assignments (not `.member =`, not `==`).
//   - Uses : bare identifiers `\b<name>\b` that are not language keywords
//            and are not on the LHS of an assignment we already captured.
//
// Function attribution uses the same nearest-preceding-header heuristic
// as the JS/TS effect sniffer; nested closures lose some precision, but
// the common module-level / class-method case is correct.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("jsts", sniffDefUseJSTS) }

// jstsDefRe matches `let|const|var <name> =` and bare `<name> =`.
// Group 2 (let/const/var form) or group 4 (bare assignment) is the name.
var jstsDefRe = regexp.MustCompile(
	`(?m)(?:\b(let|const|var)\s+([A-Za-z_$][\w$]*)\s*=` +
		`|^\s*([A-Za-z_$][\w$]*)\s*=(?:[^=]))`,
)

// jstsIdentUseRe matches a bare identifier read. The pass filters out
// keywords and the names already captured as defs on the same line.
var jstsIdentUseRe = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\b`)

// jstsReservedDefUse rejects keywords and well-known globals so the
// use-set is dominated by real locals.
func jstsReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "while", "do", "switch", "case", "default",
		"break", "continue", "return", "throw", "try", "catch", "finally",
		"function", "class", "extends", "new", "delete", "typeof",
		"instanceof", "in", "of", "let", "const", "var", "true", "false",
		"null", "undefined", "this", "super", "import", "export", "from",
		"as", "async", "await", "yield", "void", "static", "public",
		"private", "protected", "readonly", "interface", "type", "enum",
		"namespace", "declare", "module", "package", "console", "window",
		"document", "globalThis", "process", "require", "Math", "JSON",
		"Object", "Array", "String", "Number", "Boolean", "Promise",
		"Date", "Error", "RegExp", "Map", "Set", "Symbol":
		return true
	}
	return false
}

func sniffDefUseJSTS(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanJSTSFuncHeaders(content)

	var defs []VarDef
	for _, m := range jstsDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var name string
		switch {
		case m[4] >= 0:
			name = content[m[4]:m[5]]
		case m[6] >= 0:
			name = content[m[6]:m[7]]
		}
		if name == "" || jstsReservedDefUse(name) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		defs = append(defs, VarDef{Function: fn, Line: line, Var: name})
	}

	// Build a per-line ban-set of names that appear as the LHS of a def
	// on that line so we don't double-count the LHS identifier as a use.
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
	for _, m := range jstsIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if jstsReservedDefUse(name) {
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
