// Zig def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : `const x = ...;`, `var x = ...;`, `var x: T = ...;`,
//            `const x: T = ...;`, bare `x = ...;` reassignments.
//   - Uses : bare identifiers `\b<name>\b` filtered against Zig keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `fn name(` header using the
// scanZigFuncHeaders helper from effect_sinks_zig.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("zig", sniffDefUseZig) }

// zigDefRe matches Zig variable and constant declarations and reassignments.
//
//	Group 1 (const/var form): `[pub] (const|var) name [: T] = ...`
//	Group 2 (bare assign)   : `name = ...` (not `==`)
var zigDefRe = regexp.MustCompile(
	`(?m)(?:\b(?:const|var)\s+([A-Za-z_][\w]*)\s*(?::\s*[\w\[\]\s\*?!@.]*\s*)?=(?:[^=])` +
		`|^\s*([A-Za-z_][\w]*)\s*=(?:[^=]))`,
)

var zigIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func zigReservedDefUse(s string) bool {
	switch s {
	case "const", "var", "fn", "pub", "return", "if", "else", "while", "for",
		"switch", "break", "continue", "defer", "errdefer", "try", "catch",
		"unreachable", "undefined", "null", "true", "false", "void", "bool",
		"u8", "u16", "u32", "u64", "u128", "usize", "i8", "i16", "i32", "i64",
		"i128", "isize", "f16", "f32", "f64", "f128", "c_int", "c_uint",
		"c_long", "c_ulong", "c_char", "anytype", "noreturn", "comptime",
		"inline", "noinline", "extern", "export", "packed", "align", "callconv",
		"struct", "union", "enum", "error", "type", "anyerror", "and", "or",
		"orelse", "async", "await", "suspend", "resume", "nosuspend",
		"std", "self", "allocator":
		return true
	}
	return false
}

func sniffDefUseZig(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanZigFuncHeaders(content)

	var defs []VarDef
	for _, m := range zigDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || zigReservedDefUse(name) {
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
	for _, m := range zigIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if zigReservedDefUse(name) {
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
