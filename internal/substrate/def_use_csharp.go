// C# def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `<Type> <name> = ...`, `var <name> = ...`, bare reassignments
//     `<name> = ...`, `foreach (<Type> <name> in ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against C# keywords
//     and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding C# method header via the
// scanCSharpFuncHeaders helper from effect_sinks_csharp.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("csharp", sniffDefUseCSharp) }

// csharpDefRe matches typed declarations, var-decls, bare assignments,
// and foreach.
//
//	Group 1 (typed/var) : `<Type|var> <name> =`.
//	Group 2 (bare)      : `<name> = ` (not `==`, not `.X =`).
//	Group 3 (foreach)   : `foreach (<Type|var> <name> in`.
var csharpDefRe = regexp.MustCompile(
	`\b(?:[A-Z][\w<>,\s\[\]?]*|int|long|short|byte|char|bool|float|double|decimal|string|object|var)\s+([A-Za-z_][\w]*)\s*=` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\bforeach\s*\(\s*(?:[A-Z][\w<>,\s\[\]?]*|var)\s+([A-Za-z_][\w]*)\s+in\b`,
)

var csharpIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func csharpReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "foreach", "while", "do", "switch", "case",
		"default", "break", "continue", "return", "throw", "try", "catch",
		"finally", "new", "is", "as", "in", "out", "ref", "true", "false",
		"null", "this", "base", "void", "public", "private", "protected",
		"internal", "static", "readonly", "const", "sealed", "abstract",
		"virtual", "override", "async", "await", "yield", "using", "namespace",
		"class", "struct", "interface", "enum", "record", "var", "int",
		"long", "short", "byte", "char", "bool", "float", "double", "decimal",
		"string", "object", "String", "Int32", "Int64", "Boolean", "Object",
		"List", "Dictionary", "Task", "IEnumerable", "Console":
		return true
	}
	return false
}

func sniffDefUseCSharp(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanCSharpFuncHeaders(content)

	var defs []VarDef
	for _, m := range csharpDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || csharpReservedDefUse(name) {
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
	for _, m := range csharpIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if csharpReservedDefUse(name) {
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
