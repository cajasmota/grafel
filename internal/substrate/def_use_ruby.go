// Ruby def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `<name> = ...`, augmented `<name> += ...`, block params
//            `|<name>, ...|`, method parameters `def m(<name>, ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against Ruby keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `def` header (uses
// scanRubyFuncHeaders from effect_sinks_ruby.go).
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("ruby", sniffDefUseRuby) }

// rubyDefRe matches simple assignments, block params, and method params.
//   Group 1 (assign)     : `<name> = ` (not `==`, not `.attr =`).
//   Group 2 (block param): `|<name>, ...|` — first name only.
var rubyDefRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_][\w]*)\s*(?:\+|-|\*|/|%|\*\*|&|\||\^|<<|>>|\|\||&&)?=(?:[^=])` +
		`|\|\s*([A-Za-z_][\w]*)\s*[,|]`,
)

var rubyIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func rubyReservedDefUse(s string) bool {
	switch s {
	case "and", "or", "not", "if", "elsif", "else", "unless", "case", "when",
		"while", "until", "for", "in", "do", "begin", "rescue", "ensure",
		"raise", "return", "yield", "break", "next", "redo", "retry",
		"def", "end", "class", "module", "self", "super", "nil", "true",
		"false", "then", "require", "require_relative", "include", "extend",
		"attr_reader", "attr_writer", "attr_accessor", "private", "public",
		"protected", "puts", "print", "p", "lambda", "proc":
		return true
	}
	return false
}

func sniffDefUseRuby(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanRubyFuncHeaders(content)

	var defs []VarDef
	for _, m := range rubyDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || rubyReservedDefUse(name) {
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
	for _, m := range rubyIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if rubyReservedDefUse(name) {
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
