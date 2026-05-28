// Solidity template-pattern sniffer (#2779 Phase 3D T3).
//
// Template-pattern scope for Solidity:
//
//   - i18n  : not_applicable — Solidity runs on the EVM; there is no user
//             locale or i18n framework at the contract layer.
//   - sql   : not_applicable — Solidity has no SQL; storage access is via
//             mapping / array reads / SSTORE, not SQL statements.
//   - log   : Solidity emits events and uses revert/require strings for
//             on-chain "log messages". We capture:
//             * `emit EventName("literal message")` strings
//             * `require(..., "error msg")` / `revert("error msg")` literals
//             These are the nearest analogue to log-format strings in EVM code.
//
// Only the log_format kind is applicable for Solidity.
package substrate

import "regexp"

func init() {
	RegisterTemplatePatternSniffer("solidity", sniffTemplatePatternsSolidity)
}

// solidityEmitRe matches `emit EventName("literal")` string arguments.
// Group 1 = the literal payload.
var solidityEmitRe = regexp.MustCompile(
	`\bemit\s+[A-Za-z_][\w]*\s*\([^)]*"([^"]+)"`,
)

// solidityRequireRe matches `require(..., "message")` and `revert("message")`.
// Group 1 = the literal message.
var solidityRequireRe = regexp.MustCompile(
	`\b(?:require|revert)\s*\([^)]*"([^"]+)"`,
)

func sniffTemplatePatternsSolidity(content string) []TemplatePattern {
	if content == "" {
		return nil
	}
	headers := scanSolidityFuncHeaders(content)
	var out []TemplatePattern

	for _, m := range solidityEmitRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "emit.literal",
		})
	}
	for _, m := range solidityRequireRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, TemplatePattern{
			Function: nearestHeader(headers, lineOfOffset(content, m[0])),
			Line:     lineOfOffset(content, m[0]),
			Kind:     TemplateKindLog,
			Literal:  TruncateLiteral(content[m[2]:m[3]]),
			Tag:      "require/revert.msg",
		})
	}
	return out
}
