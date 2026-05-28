// Go def-use sniffer (#2774 Phase 3C T1).
//
// Recognises:
//   - Defs : `<name> :=`, `var <name> ...`, `<name> = ...` reassignments,
//            `for <name> :=`, `for <name>, <name2> := range`,
//            short-declarations with multiple LHS names.
//   - Uses : bare identifiers `\b<name>\b` filtered against the Go
//            keyword set and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding Go function header (uses the
// existing scanGoFuncHeaders helper from effect_sinks_golang.go).
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("go", sniffDefUseGo) }

// goShortDeclRe matches a short-declaration `<name>, <name2>, ... := ...`.
// Group 1 = the entire comma-separated LHS list.
var goShortDeclRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z_][\w]*(?:\s*,\s*[A-Za-z_][\w]*)*)\s*:=`,
)

// goVarDeclRe matches `var <name>` (single, no parens).
var goVarDeclRe = regexp.MustCompile(`\bvar\s+([A-Za-z_][\w]*)`)

// goAssignRe matches a bare assignment `<name> = ...` (not `==`, not `:=`).
var goAssignRe = regexp.MustCompile(
	`(?m)^[ \t]*([A-Za-z_][\w]*)\s*=(?:[^=])`,
)

// goForRangeRe matches `for <name>, _ := range` etc — same identifier
// shape as the short-decl form; we already cover it via goShortDeclRe
// because of the `:=` operator. Kept here as documentation only.

var goIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

// goSplitCSV splits the LHS list captured by goShortDeclRe into per-name
// entries, trimming whitespace. Avoids strings.Split's allocation for the
// common single-name case.
func goSplitCSV(lhs string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(lhs); i++ {
		if i == len(lhs) || lhs[i] == ',' {
			seg := lhs[start:i]
			for len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') {
				seg = seg[1:]
			}
			for len(seg) > 0 && (seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '\t') {
				seg = seg[:len(seg)-1]
			}
			if seg != "" {
				out = append(out, seg)
			}
			start = i + 1
		}
	}
	return out
}

func goReservedDefUse(s string) bool {
	switch s {
	case "break", "case", "chan", "const", "continue", "default", "defer",
		"else", "fallthrough", "for", "func", "go", "goto", "if", "import",
		"interface", "map", "package", "range", "return", "select", "struct",
		"switch", "type", "var", "true", "false", "nil", "iota", "_",
		"make", "new", "len", "cap", "append", "copy", "delete", "panic",
		"recover", "print", "println", "string", "int", "int32", "int64",
		"uint", "uint32", "uint64", "byte", "rune", "float32", "float64",
		"bool", "error", "any":
		return true
	}
	return false
}

func sniffDefUseGo(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanGoFuncHeaders(content)

	var defs []VarDef
	addDef := func(name string, off int) {
		if name == "" || goReservedDefUse(name) {
			return
		}
		line := lineOfOffset(content, off)
		fn := nearestHeader(headers, line)
		if fn == "" {
			return
		}
		defs = append(defs, VarDef{Function: fn, Line: line, Var: name})
	}
	for _, m := range goShortDeclRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		for _, nm := range goSplitCSV(content[m[2]:m[3]]) {
			addDef(nm, m[0])
		}
	}
	for _, m := range goVarDeclRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		addDef(content[m[2]:m[3]], m[0])
	}
	for _, m := range goAssignRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		addDef(content[m[2]:m[3]], m[0])
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
	for _, m := range goIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if goReservedDefUse(name) {
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
