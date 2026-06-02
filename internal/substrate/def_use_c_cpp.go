// C / C++ def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `<Type> <name> = ...`, `auto <name> = ...`, bare
//     reassignments `<name> = ...`, `for (<Type> <name> = ...; ...)`
//     and range-for `for (<Type> <name> : ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against C/C++
//     keywords and standard-library type names and not on the LHS
//     of a def we already captured.
//
// Function attribution: nearest preceding C/C++ function header via the
// scanCCPPFuncHeaders helper from effect_sinks_c_cpp.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("c-cpp", sniffDefUseCCPP) }

// cppDefRe matches typed declarations, auto-decls, bare assignments,
// and range-for.
//
//	Group 1 (typed)    : `<Type|auto> <name> =`.
//	Group 2 (bare)     : `<name> = ` (not `==`, not `.x =`, not `->x =`).
//	Group 3 (range-for): `for (<Type|auto> <name> :`.
var cppDefRe = regexp.MustCompile(
	`\b(?:auto|int|long|short|char|float|double|bool|void|size_t|std::[A-Za-z_][\w:<>,\s]*|[A-Z][\w<>,:\s\*\&]*)\s+\*?\s*([A-Za-z_][\w]*)\s*=` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\bfor\s*\(\s*(?:auto|const\s+auto|[A-Z][\w<>:\s\*\&]*)\s+\*?\s*([A-Za-z_][\w]*)\s*:`,
)

var cppIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func cppReservedDefUse(s string) bool {
	switch s {
	case "auto", "break", "case", "char", "const", "continue", "default",
		"do", "double", "else", "enum", "extern", "float", "for", "goto",
		"if", "inline", "int", "long", "register", "restrict", "return",
		"short", "signed", "sizeof", "static", "struct", "switch",
		"typedef", "union", "unsigned", "void", "volatile", "while",
		"bool", "true", "false", "nullptr", "class", "namespace", "using",
		"public", "private", "protected", "virtual", "override", "final",
		"new", "delete", "this", "throw", "try", "catch", "template",
		"typename", "operator", "friend", "explicit", "mutable", "constexpr",
		"noexcept", "decltype", "static_cast", "dynamic_cast", "reinterpret_cast",
		"const_cast", "size_t", "ptrdiff_t", "nullptr_t", "std", "string",
		"vector", "map", "set", "list", "array", "unordered_map", "unordered_set",
		"pair", "tuple", "shared_ptr", "unique_ptr", "weak_ptr", "make_shared",
		"make_unique", "cout", "cerr", "cin", "endl", "printf", "scanf",
		"malloc", "free", "memcpy", "memset", "strlen", "strcpy", "NULL":
		return true
	}
	return false
}

func sniffDefUseCCPP(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanCCPPFuncHeaders(content)

	var defs []VarDef
	for _, m := range cppDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || cppReservedDefUse(name) {
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
	for _, m := range cppIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if cppReservedDefUse(name) {
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
