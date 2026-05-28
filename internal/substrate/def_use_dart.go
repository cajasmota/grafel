// Dart def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : `var x = ...`, `final x = ...`, `const x = ...`,
//            typed `String x = ...`, bare `x = ...` reassignments.
//   - Uses : bare identifiers `\b<name>\b` filtered against Dart keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `void|type name(` header using
// the scanDartFuncHeaders helper from effect_sinks_dart.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("dart", sniffDefUseDart) }

// dartDefRe matches local variable declarations and bare assignments.
//
//	Group 1 (var/final/const/typed): `(var|final|const|<type>) <name> = ...`
//	Group 2 (bare assignment)       : `<name> = ...` (not `==`)
var dartDefRe = regexp.MustCompile(
	`(?m)(?:(?:var|final|const|late)\s+(?:[A-Za-z_][\w<>?,\s]*\s+)?([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|^\s*([A-Za-z_][\w]*)\s*=(?:[^=]))`,
)

var dartIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func dartReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "while", "do", "switch", "case", "default",
		"break", "continue", "return", "throw", "try", "catch", "finally",
		"new", "true", "false", "null", "this", "super", "in", "is", "as",
		"var", "final", "const", "late", "class", "extends", "implements",
		"mixin", "with", "abstract", "static", "void", "dynamic", "async",
		"await", "yield", "import", "export", "library", "part", "show",
		"hide", "typedef", "enum", "covariant", "external", "factory",
		"get", "set", "operator", "required", "String", "int", "double",
		"bool", "num", "List", "Map", "Set", "Future", "Stream", "Object",
		"Function", "Null", "Never", "Type", "Iterable", "print":
		return true
	}
	return false
}

func sniffDefUseDart(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanDartFuncHeaders(content)

	var defs []VarDef
	for _, m := range dartDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || dartReservedDefUse(name) {
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
	for _, m := range dartIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if dartReservedDefUse(name) {
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
