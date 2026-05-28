// Solidity taint-sites sniffer (#2778 Phase 2B T3).
//
// Recognises Solidity source / sink / sanitizer primitives for
// EVM smart-contract security analysis.
//
// Sources:
//   - msg.data — raw calldata from the external caller (highest-risk input)
//   - External function parameters — any `external` or `public` function's
//     parameters are untrusted by definition. Approximated by matching
//     `external` / `public` function signatures with non-literal param types.
//   - msg.sender / tx.origin — caller address (tainted for auth checks)
//
// Sinks:
//   - External CALL with user data: `<address>.call{...}(msg.data)` or
//     `abi.encodeWithSelector(..., userVar)` — passing tainted data to
//     a low-level call
//   - DELEGATECALL with user selector: `<address>.delegatecall(msg.data)` —
//     extremely dangerous; any tainted data in the selector is an RCE
//   - Storage writes with untrusted input: `mapping[userInput] = value` or
//     `array[userInput] = value` — access control bypass / griefing vectors
//
// Sanitizers:
//   - OpenZeppelin ReentrancyGuard: `nonReentrant` modifier present on a
//     function — mitigates reentrancy from external calls
//   - Checks-effects-interactions: presence of `require(` / `revert(` before
//     the external call in the same function — approximated by require/revert
//     appearing before `.call{` in the same function body (file-level heuristic)
//   - Access control: `onlyOwner` / `onlyRole` / `onlyAdmin` modifiers from
//     OpenZeppelin access control
package substrate

import "regexp"

func init() { RegisterTaintSniffer("solidity", sniffTaintSolidity) }

// solSourceMsgDataRe matches msg.data (raw calldata) and tx.origin.
var solSourceMsgDataRe = regexp.MustCompile(
	`\bmsg\s*\.\s*(?:data|sender|value)\b` +
		`|\btx\s*\.\s*origin\b`,
)

// solSourceExternalParamRe matches `external` or `public` function
// declarations with at least one parameter. The parameters are tainted
// by definition (they come from external callers).
var solSourceExternalParamRe = regexp.MustCompile(
	`(?m)function\s+[A-Za-z_][\w]*\s*\([^)]+\)\s*(?:external|public)\b`,
)

// solSinkLowLevelCallRe matches low-level `.call{...}(data)` /
// `.delegatecall(data)` / `.staticcall(data)` — any of these with
// a non-literal data argument is a high-risk sink.
var solSinkLowLevelCallRe = regexp.MustCompile(
	`\.\s*call\s*\{[^}]*\}\s*\(\s*(?:msg\.data|abi\.|[A-Za-z_][\w]*)` +
		`|\.\s*delegatecall\s*\(\s*(?:msg\.data|[A-Za-z_][\w]*)` +
		`|\.\s*staticcall\s*\(\s*(?:msg\.data|[A-Za-z_][\w]*)`,
)

// solSinkABIEncodeUserRe matches abi.encodeWithSelector / abi.encodeCall
// with user-controlled variable arguments — passing tainted data into an
// encoded call payload.
var solSinkABIEncodeUserRe = regexp.MustCompile(
	`\babi\s*\.\s*(?:encodeWithSelector|encodeWithSignature|encodeCall)\s*\([^)]*[A-Za-z_][\w]*\s*[,)]`,
)

// solSinkStorageUserKeyRe matches mapping / array writes where the key/index
// is msg.sender-derived or a user-controlled variable (potential griefing).
var solSinkStorageUserKeyRe = regexp.MustCompile(
	`\b[A-Za-z_][\w]*\s*\[\s*(?:msg\s*\.\s*sender|tx\s*\.\s*origin|[A-Za-z_][\w]*)\s*\]\s*=(?:[^=])`,
)

// solSanitizerReentrancyGuardRe matches the OpenZeppelin nonReentrant modifier.
// HARD RULE per #2778: the modifier must be declared on the function that
// contains the external call — approximated by file-level presence of
// `nonReentrant` modifier usage.
var solSanitizerReentrancyGuardRe = regexp.MustCompile(
	`\bnonReentrant\b` +
		`|\bReentrancyGuard\b`,
)

// solSanitizerChecksEffectsRe matches require / revert before an external
// call — the checks-effects-interactions pattern.
var solSanitizerChecksEffectsRe = regexp.MustCompile(
	`\brequire\s*\(` +
		`|\brevert\s*(?:\(|\w)`,
)

// solSanitizerAccessControlRe matches OpenZeppelin access control modifiers.
var solSanitizerAccessControlRe = regexp.MustCompile(
	`\bonlyOwner\b|\bonlyRole\b|\bonlyAdmin\b|\bonlyMinter\b` +
		`|\bhasRole\s*\(` +
		`|\brequire\s*\(\s*(?:msg\s*\.\s*sender\s*==\s*owner|hasRole\b)`,
)

func sniffTaintSolidity(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanSolidityFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, solSourceMsgDataRe, TaintKindSource, TaintCategoryGeneric, "msg.data/msg.sender/tx.origin", 1.0)
	out = appendTaintMatches(out, content, headers, solSourceExternalParamRe, TaintKindSource, TaintCategoryGeneric, "external/public function params", 0.8)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, solSanitizerReentrancyGuardRe, TaintKindSanitizer, TaintCategoryGeneric, "nonReentrant/ReentrancyGuard", 1.0)
	out = appendTaintMatches(out, content, headers, solSanitizerChecksEffectsRe, TaintKindSanitizer, TaintCategoryGeneric, "require/revert (checks-effects-interactions)", 0.8)
	out = appendTaintMatches(out, content, headers, solSanitizerAccessControlRe, TaintKindSanitizer, TaintCategoryGeneric, "onlyOwner/onlyRole/hasRole", 0.9)
	// Sinks.
	out = appendTaintMatches(out, content, headers, solSinkLowLevelCallRe, TaintKindSink, TaintCategoryCommand, ".call/.delegatecall(user-data)", 1.0)
	out = appendTaintMatches(out, content, headers, solSinkABIEncodeUserRe, TaintKindSink, TaintCategoryGeneric, "abi.encodeWithSelector(user-arg)", 0.85)
	out = appendTaintMatches(out, content, headers, solSinkStorageUserKeyRe, TaintKindSink, TaintCategoryGeneric, "mapping[msg.sender/userVar]=", 0.75)
	return out
}
