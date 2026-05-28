// Scala def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `val <name> = ...`, `var <name> = ...`, optional type
//            ascription `: T`, bare reassignments `<name> = ...`,
//            `for (<name> <- ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against Scala
//            keywords and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `def` header via the
// scanScalaFuncHeaders helper from effect_sinks_scala.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("scala", sniffDefUseScala) }

// scalaDefUseRe matches val/var-decls, bare assignments, and for-generators.
//   Group 1 (val/var) : `(val|var) <name>`.
//   Group 2 (bare)    : `<name> = ` (not `==`, not `.x =`).
//   Group 3 (for-gen) : `<name> <-`.
var scalaDefUseRe = regexp.MustCompile(
	`\b(?:val|var)\s+([A-Za-z_][\w]*)\b` +
		`|(?m)^\s*([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|\b([A-Za-z_][\w]*)\s*<-\b`,
)

var scalaIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func scalaReservedDefUse(s string) bool {
	switch s {
	case "abstract", "case", "catch", "class", "def", "do", "else", "extends",
		"false", "final", "finally", "for", "forSome", "if", "implicit",
		"import", "lazy", "match", "new", "null", "object", "override",
		"package", "private", "protected", "return", "sealed", "super",
		"this", "throw", "trait", "true", "try", "type", "val", "var",
		"while", "with", "yield", "given", "using", "then", "enum",
		"String", "Int", "Long", "Short", "Byte", "Char", "Boolean",
		"Float", "Double", "Unit", "Any", "AnyRef", "AnyVal", "Nothing",
		"List", "Map", "Set", "Seq", "Option", "Some", "None", "Either",
		"Left", "Right", "Future", "println", "print":
		return true
	}
	return false
}

func sniffDefUseScala(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanScalaFuncHeaders(content)

	var defs []VarDef
	for _, m := range scalaDefUseRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || scalaReservedDefUse(name) {
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
	for _, m := range scalaIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if scalaReservedDefUse(name) {
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
