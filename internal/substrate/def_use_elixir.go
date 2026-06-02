// Elixir def-use sniffer (#2775 Phase 3C T2).
//
// Recognises:
//   - Defs : `<name> = ...` pattern-match binding (Elixir's primary form),
//     function-head params `def f(<name>, ...)`.
//   - Uses : bare identifiers `\b<name>\b` filtered against Elixir
//     keywords / atoms and not on the LHS of a def we already
//     captured on the same line.
//
// Function attribution: nearest preceding `def` / `defp` / `defmacro`
// header via the scanElixirFuncHeaders helper from effect_sinks_elixir.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("elixir", sniffDefUseElixir) }

// elixirDefUseRe matches simple bindings.
//
//	Group 1 (assign)  : `<name> = ` (not `==`, not match-op `<-`).
var elixirDefUseRe = regexp.MustCompile(
	`(?m)^\s*([a-z_][\w]*[?!]?)\s*=(?:[^=])`,
)

var elixirIdentUseRe = regexp.MustCompile(`\b([a-z_][\w]*[?!]?)\b`)

func elixirReservedDefUse(s string) bool {
	switch s {
	case "def", "defp", "defmacro", "defmacrop", "defmodule", "defstruct",
		"defprotocol", "defimpl", "defexception", "do", "end", "if",
		"unless", "else", "case", "cond", "when", "with", "for", "fn",
		"true", "false", "nil", "and", "or", "not", "in", "import",
		"alias", "require", "use", "raise", "rescue", "catch", "throw",
		"after", "receive", "try", "quote", "unquote", "super", "__MODULE__",
		"__DIR__", "__ENV__", "__CALLER__":
		return true
	}
	return false
}

func sniffDefUseElixir(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanElixirFuncHeaders(content)

	var defs []VarDef
	for _, m := range elixirDefUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if name == "" || elixirReservedDefUse(name) {
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
	for _, m := range elixirIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if elixirReservedDefUse(name) {
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
