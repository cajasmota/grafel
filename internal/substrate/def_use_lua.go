// Lua def-use sniffer (Phase 3C).
//
// Recognises:
//   - Defs : `local <name> = ...` local variable declarations
//     `<name> = ...` module-level or upvalue assignments
//     function parameter names from `function f(<name>, ...)`
//   - Uses : bare identifiers `\b<name>\b` filtered against Lua keywords
//     and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `function` header.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("lua", sniffDefUseLua) }

// luaDefRe matches local variable declarations and bare assignments.
//
//	Group 1: local <name> = ... (local declaration)
//	Group 2: bare <name> = ... (assignment, not ==)
var luaDefRe = regexp.MustCompile(
	`(?m)^\s*local\s+([A-Za-z_][\w]*)\s*(?:,\s*[A-Za-z_][\w]*)*\s*=` +
		`|^\s*([A-Za-z_][\w]*)\s*(?:\+|-|\*|/|%|\.\.)?\s*=(?:[^=])`,
)

// luaFuncHeaderRe matches function declarations for attribution.
// Captures the function name (last segment of dotted/colon names).
//
//	function foo(...)   — plain global function
//	function M.foo(...) — module method
//	function M:bar(...) — method with self
//	local function baz(...) — local function
var luaFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:local\s+)?function\s+(?:[A-Za-z_][\w]*[.:])*([A-Za-z_][\w]*)\s*\(`,
)

// luaIdentUseRe matches bare identifiers.
var luaIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

// luaReservedDefUse reports whether s is a Lua keyword or common builtin
// that should not be tracked as a user variable.
func luaReservedDefUse(s string) bool {
	switch s {
	case "and", "break", "do", "else", "elseif", "end", "false", "for",
		"function", "goto", "if", "in", "local", "nil", "not", "or",
		"repeat", "return", "then", "true", "until", "while",
		// common builtins
		"print", "type", "error", "assert", "require", "pcall", "xpcall",
		"pairs", "ipairs", "next", "select", "tostring", "tonumber",
		"rawget", "rawset", "rawequal", "rawlen", "setmetatable", "getmetatable",
		"unpack", "table", "string", "math", "io", "os", "coroutine",
		"ngx", "cjson", "json":
		return true
	}
	return false
}

func sniffDefUseLua(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}

	// Build function header list for attribution.
	type luaFuncHeader struct {
		Line int
		Name string
	}
	var headers []luaFuncHeader
	for _, m := range luaFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		headers = append(headers, luaFuncHeader{
			Line: lineOfOffset(content, m[0]),
			Name: content[m[2]:m[3]],
		})
	}

	nearestLuaHeader := func(line int) string {
		best := ""
		for _, h := range headers {
			if h.Line <= line {
				best = h.Name
			}
		}
		return best
	}

	var defs []VarDef
	for _, m := range luaDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || luaReservedDefUse(name) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestLuaHeader(line)
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
	for _, m := range luaIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if luaReservedDefUse(name) {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestLuaHeader(line)
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
