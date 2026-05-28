// Solidity effect-sink sniffer (#2776 Phase 1A T3).
//
// Recognises Solidity sink primitives. Solidity executes on the EVM; the
// effect lattice maps as follows:
//
//   - mutation  : state-variable write (any assignment to a contract storage
//                 variable — equivalent to SSTORE). This is the primary
//                 observable side-effect in Solidity; contracts have no
//                 OS filesystem or outbound HTTP.
//   - http_out  : external contract call via .call{...}() / .send() /
//                 .transfer() / interface method on external address — these
//                 are cross-contract invocations that can trigger re-entrancy,
//                 semantically analogous to an outbound network call.
//   - db_read   : not_applicable — no SQL in Solidity; mapping/array reads
//                 are pure EVM reads without a network boundary.
//   - db_write  : not_applicable — mapping/array writes are captured as
//                 mutation above.
//   - fs_read / fs_write : not_applicable — EVM has no filesystem.
//
// Function attribution uses `function name(` / `modifier name(` headers.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("solidity", sniffEffectsSolidity) }

// solidityFuncHeaderRe matches Solidity function / modifier / constructor
// declarations. Capture group 1 is the name ("constructor" for constructors).
var solidityFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:function\s+([A-Za-z_][\w]*)|modifier\s+([A-Za-z_][\w]*)|constructor)\s*\(`,
)

// solidityMutationRe matches storage-variable writes:
//   - Simple `varName = ...` (top-level state variable assign in a function)
//   - Mapping writes: `mapping[key] = ...`
//   - Struct/array field writes: `storage.field = ...`
//
// We exclude local-variable assignments by requiring:
//   - LHS must be an identifier (no `memory` / `uint`-typed local decls)
//   - Assignment must NOT be a comparison (`!=`, `==`, `<=`, `>=`)
var solidityMutationRe = regexp.MustCompile(
	`(?m)^\s*(?:[A-Za-z_][\w]*\s*(?:\[(?:[^\[\]])*\])?\s*(?:\.\s*[A-Za-z_][\w]*)?\s*)=(?:[^=])`,
)

// solidityExternalCallRe matches cross-contract invocations:
//   - address.call{value:...}(abi.encodeWithSelector(...))
//   - address.send(amount) / address.transfer(amount)
//   - contractVar.methodName(...) where contractVar is an interface/contract type
var solidityExternalCallRe = regexp.MustCompile(
	`\.\s*call\s*\{` +
		`|\.\s*(?:send|transfer)\s*\(` +
		`|\babi\s*\.\s*(?:encodeWithSelector|encodeWithSignature|encodeCall)\s*\(` +
		`|\b(?:IERC20|IERC721|IERC1155|IUniswap|I[A-Z][A-Za-z]+)\s*\(\s*[^)]+\s*\)\s*\.\s*[A-Za-z_][\w]*\s*\(`,
)

func sniffEffectsSolidity(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanSolidityFuncHeaders(content)
	var out []EffectMatch
	out = appendSolidityMatches(out, content, headers, solidityMutationRe, EffectMutation, "storage-write", 0.7)
	out = appendSolidityMatches(out, content, headers, solidityExternalCallRe, EffectHTTPOut, "external-call/transfer", 1.0)
	return out
}

func scanSolidityFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range solidityFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		var name string
		switch {
		case len(m) >= 4 && m[2] >= 0:
			name = content[m[2]:m[3]]
		case len(m) >= 6 && m[4] >= 0:
			name = content[m[4]:m[5]]
		default:
			name = "constructor"
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: name})
	}
	return hs
}

func appendSolidityMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: conf,
		})
	}
	return out
}
