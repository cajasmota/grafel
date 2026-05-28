// Crystal def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : `x = ...` local assignments, typed `x : T = ...`,
//            `@x = ...` instance variable assignments.
//   - Uses : bare identifiers `\b<name>\b` filtered against Crystal keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `def name` header using the
// scanCrystalFuncHeaders helper from effect_sinks_crystal.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("crystal", sniffDefUseCrystal) }

// crystalDefRe matches local variable assignments.
//
//	Group 1 (typed/bare): `name = ...` at start of line (not `==`)
//	Group 2 (ivar):       `@name = ...`
var crystalDefRe = regexp.MustCompile(
	`(?m)^\s*([a-z_][\w]*)\s*=(?:[^=])` +
		`|^\s*(@[a-z_][\w]*)\s*=(?:[^=])`,
)

var crystalIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func crystalReservedDefUse(s string) bool {
	switch s {
	case "if", "elsif", "else", "unless", "case", "when", "while", "until",
		"for", "in", "do", "begin", "rescue", "ensure", "raise", "return",
		"yield", "break", "next", "redo", "retry", "def", "end", "class",
		"module", "struct", "lib", "fun", "macro", "self", "nil", "true",
		"false", "abstract", "require", "include", "extend", "protected",
		"private", "puts", "print", "p", "pp", "loop", "spawn", "typeof",
		"sizeof", "pointerof", "offsetof", "instance_sizeof", "as", "is_a?",
		"responds_to?", "select", "then":
		return true
	}
	return false
}

func sniffDefUseCrystal(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanCrystalFuncHeaders(content)

	var defs []VarDef
	for _, m := range crystalDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		var name string
		switch {
		case m[2] >= 0:
			name = content[m[2]:m[3]]
		case m[4] >= 0:
			// ivar — strip leading @
			raw := content[m[4]:m[5]]
			if len(raw) > 1 {
				name = raw[1:]
			}
		}
		if name == "" || crystalReservedDefUse(name) {
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
	for _, m := range crystalIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if crystalReservedDefUse(name) {
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
