// Solidity def-use sniffer (#2779 Phase 3C T3).
//
// Recognises:
//   - Defs : typed local declarations `uint256 x = ...;`, `address x = ...;`,
//            `bool x = ...;`, bare `x = ...;` reassignments (not `==`).
//   - Uses : bare identifiers `\b<name>\b` filtered against Solidity keywords
//            and not on the LHS of a def we already captured.
//
// Function attribution: nearest preceding `function name(` / `modifier name(`
// header using the scanSolidityFuncHeaders helper from effect_sinks_solidity.go.
package substrate

import "regexp"

func init() { RegisterDefUseSniffer("solidity", sniffDefUseSolidity) }

// solidityLocalDefRe matches typed local variable declarations.
// Solidity locals require an explicit type keyword before the identifier.
//
//	Group 1 (typed form) : `<type> [memory|storage|calldata] name = ...`
//	Group 2 (bare assign): `name = ...` (not `==`)
var solidityLocalDefRe = regexp.MustCompile(
	`(?m)(?:\b(?:uint(?:\d+)?|int(?:\d+)?|address(?:\s+payable)?|bool|bytes(?:\d+)?|string|mapping)\s+` +
		`(?:(?:memory|storage|calldata)\s+)?([A-Za-z_][\w]*)\s*=(?:[^=])` +
		`|^\s*([A-Za-z_][\w]*)\s*=(?:[^=]))`,
)

var solidityIdentUseRe = regexp.MustCompile(`\b([A-Za-z_][\w]*)\b`)

func solidityReservedDefUse(s string) bool {
	switch s {
	case "if", "else", "for", "while", "do", "break", "continue", "return",
		"throw", "revert", "require", "assert", "emit", "delete", "new",
		"true", "false", "this", "super", "selfdestruct", "suicide",
		"function", "modifier", "constructor", "fallback", "receive",
		"event", "error", "struct", "enum", "mapping", "contract",
		"library", "interface", "abstract", "is", "using", "pragma",
		"import", "returns", "memory", "storage", "calldata", "payable",
		"external", "internal", "public", "private", "view", "pure",
		"virtual", "override", "indexed", "anonymous", "constant",
		"immutable", "uint", "int", "address", "bool", "bytes", "string",
		"msg", "tx", "block", "abi", "type", "gasleft", "now",
		"uint8", "uint16", "uint32", "uint64", "uint128", "uint256",
		"int8", "int16", "int32", "int64", "int128", "int256",
		"bytes1", "bytes2", "bytes4", "bytes8", "bytes16", "bytes32":
		return true
	}
	return false
}

func sniffDefUseSolidity(content string) ([]VarDef, []VarUse) {
	if content == "" {
		return nil, nil
	}
	headers := scanSolidityFuncHeaders(content)

	var defs []VarDef
	for _, m := range solidityLocalDefRe.FindAllStringSubmatchIndex(content, -1) {
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
		if name == "" || solidityReservedDefUse(name) {
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
	for _, m := range solidityIdentUseRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if solidityReservedDefUse(name) {
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
