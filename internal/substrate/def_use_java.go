// Java def-use sniffer (#2774 Phase 3C T1).
//
// Recognises:
//   - Defs : `<Type> <name> = ...`, simple `<name> = ...` reassignments,
//            `for (<Type> <name> :)` / `for (...; ...; ...)` index decls.
//   - Uses : bare identifiers `\b<name>\b` (filtered against keywords and
//            type names) not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding Java method header (uses the
// existing scanJavaFuncHeaders helper from effect_sinks_java.go).
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("java", sniffDefUseJava) }

// javaDefRe matches typed declarations and bare assignments.
//   Group 1 (typed)   : `<Type> <name> =`     where Type is one identifier.
//   Group 2 (bare)    : `<name> = ...`        not `==`, not `.x =`.
//   Group 3 (forEach) : `for (<Type> <name> :`.
var javaDefRe = regexp.MustCompile(
	`\b(?:[A-Z][\w<>,\s\[\]]*|int|long|short|byte|char|boolean|float|double|var)\s+([A-Za-z_][\w]*)\s*=` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\bfor\s*\(\s*(?:[A-Z][\w<>,\s\[\]]*|int|long|var)\s+([A-Za-z_][\w]*)\s*:`,
)

var javaIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func javaReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "while", "do", "switch", "case", "default",
		"break", "continue", "return", "throw", "throws", "try", "catch",
		"finally", "new", "instanceof", "true", "false", "null", "this",
		"super", "void", "public", "private", "protected", "static",
		"final", "abstract", "synchronized", "volatile", "transient",
		"native", "strictfp", "class", "interface", "enum", "extends",
		"implements", "package", "import", "int", "long", "short", "byte",
		"char", "boolean", "float", "double", "var", "String", "Integer",
		"Long", "Boolean", "Object", "List", "Map", "Set", "ArrayList",
		"HashMap", "HashSet", "Optional", "Override":
		return true
	}
	return false
}

func sniffDefUseJava(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanJavaFuncHeaders(content)

	var defs []VarDef
	for _, m := range javaDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || javaReservedDefUse(name) {
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
	for _, m := range javaIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if javaReservedDefUse(name) {
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
