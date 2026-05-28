// Rust def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `let <name> = ...`, `let mut <name> = ...`,
//            `let <name>: T = ...`, bare reassignments `<name> = ...`,
//            `for <name> in ...`.
//   - Uses : bare identifiers `\b<name>\b` filtered against Rust keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `fn` header via the
// scanRustFuncHeaders helper from effect_sinks_rust.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("rust", sniffDefUseRust) }

// rustDefRe matches let-bindings, bare assignments, and for-loop iter
// patterns.
//   Group 1 (let)     : `let [mut] <name>[: T] =`.
//   Group 2 (bare)    : `<name> = ` (not `==`, not `.field =`).
//   Group 3 (for-in)  : `for <name> in`.
var rustDefRe = regexp.MustCompile(
	`\blet\s+(?:mut\s+)?([A-Za-z_][\w]*)\b` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\bfor\s+([A-Za-z_][\w]*)\s+in\b`,
)

var rustIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func rustReservedDefUse(s string) bool {
	switch s {
	case "as", "break", "const", "continue", "crate", "else", "enum",
		"extern", "false", "fn", "for", "if", "impl", "in", "let", "loop",
		"match", "mod", "move", "mut", "pub", "ref", "return", "self",
		"Self", "static", "struct", "super", "trait", "true", "type",
		"unsafe", "use", "where", "while", "async", "await", "dyn",
		"box", "do", "final", "macro", "override", "priv", "typeof",
		"unsized", "virtual", "yield", "String", "Vec", "Option", "Result",
		"Box", "Some", "None", "Ok", "Err", "println", "print", "format",
		"panic", "vec":
		return true
	}
	return false
}

func sniffDefUseRust(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanRustFuncHeaders(content)

	var defs []VarDef
	for _, m := range rustDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || rustReservedDefUse(name) {
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
	for _, m := range rustIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if rustReservedDefUse(name) {
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
