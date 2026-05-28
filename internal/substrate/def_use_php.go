// PHP def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `$<name> = ...`, augmented `$<name> += ...`, `foreach (... as
//            $<name>)`, function parameters `function f($<name>, ...)`.
//   - Uses : `$<name>` reads (PHP variables always carry the sigil), not
//            on the LHS of a def already captured on the same line.
//
// Function attribution: nearest preceding `function` header via the
// scanPHPFuncHeaders helper from effect_sinks_php.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("php", sniffDefUsePHP) }

// phpDefRe matches assignment + foreach + function-param defs.
//   Group 1 (assign)   : `$<name> = ` (not `==`, not array-key `=>`).
//   Group 2 (foreach)  : `as $<name>` / `as $<key> => $<name>`.
var phpDefRe = regexp.MustCompile(
	`\$([A-Za-z_][\w]*)\s*(?:\+|-|\*|/|%|\.|\*\*|&|\||\^|<<|>>)?=(?:[^=])` +
		`|\bas\s+\$([A-Za-z_][\w]*)\b`,
)

// phpIdentUseRe matches a `$<name>` read.
var phpIdentUseRe = regexp.MustCompile(`\$([A-Za-z_][\w]*)\b`)

func phpReservedDefUse(s string) bool {
	switch s {
	case "this", "self", "static", "parent":
		return true
	}
	return false
}

func sniffDefUsePHP(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanPHPFuncHeaders(content)

	var defs []VarDef
	for _, m := range phpDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || phpReservedDefUse(name) {
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
	for _, m := range phpIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if phpReservedDefUse(name) {
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
