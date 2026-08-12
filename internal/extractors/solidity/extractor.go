// Package solidity implements a regex-based extractor for Solidity smart-contract files.
//
// Extracted entities:
//   - `contract Foo {…}`  / `library Foo {…}` / `interface Foo {…}` → SCOPE.Component (subtype="contract"/"library"/"interface")
//   - `function name(…) …` → SCOPE.Operation (subtype="function")
//   - `event Name(…);`    → SCOPE.Operation (subtype="event")
//   - `modifier name(…){…}` → SCOPE.Operation (subtype="modifier")
//   - `uint256 public cap;` → SCOPE.Schema (subtype="field") for contract-level state variables
//   - `import "./Foo.sol"` / `import "…"` → IMPORTS relationship
//   - `contract Foo is Bar, Baz` → EXTENDS edges (on the contract component)
//   - Function-call expressions → CALLS edges
//   - CONTAINS edges (contract → its functions/events/modifiers/state variables)
//
// No tree-sitter grammar for Solidity is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions.
//
// Registers itself via init() and is imported by registry_gen.go.
package solidity

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("solidity", &Extractor{})
}

// Extractor implements extractor.Extractor for Solidity.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "solidity" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// importRE matches both styles:
	//   import "./Foo.sol";
	//   import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
	importRE = regexp.MustCompile(
		`(?m)^[ \t]*import\s+(?:[^"']*\s+)?["']([^"']+)["']`,
	)

	// contractRE matches contract/library/abstract contract/interface declarations.
	// Group 1: kind keyword (contract|library|interface|abstract)
	// Group 2: name
	// Group 3: inheritance list after "is" (may be empty string)
	contractRE = regexp.MustCompile(
		`(?m)^[ \t]*(abstract\s+contract|contract|library|interface)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:is\s+([A-Za-z_][A-Za-z0-9_,\s]*))?[{]`,
	)

	// functionRE matches function declarations inside contracts.
	// Group 1: function name (plain identifier; does NOT match receive/fallback specials)
	functionRE = regexp.MustCompile(
		`(?m)^[ \t]*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// eventRE matches event declarations.
	// Group 1: event name
	eventRE = regexp.MustCompile(
		`(?m)^[ \t]*event\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// modifierRE matches modifier declarations.
	// Group 1: modifier name
	modifierRE = regexp.MustCompile(
		`(?m)^[ \t]*modifier\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// callRE matches dotted or bare function-call patterns.
	// We look for: identifier( or Type.method( patterns in function bodies.
	// Group 1: the callee string (may be dotted).
	callDotRE = regexp.MustCompile(
		`\b([A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)
	callBareRE = regexp.MustCompile(
		`\b([a-z_][A-Za-z0-9_]+)\s*\(`,
	)
)

// solidityKeywords is the set of tokens to exclude from CALLS edges.
var solidityKeywords = map[string]bool{
	// Control flow / built-ins / types
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"return": true, "require": true, "revert": true, "assert": true,
	"emit": true, "delete": true, "new": true,
	// Type names
	"uint": true, "int": true, "bool": true, "address": true,
	"bytes": true, "string": true, "mapping": true,
	// Visibility / state-mutability keywords used as call-lookalike tokens
	"public": true, "private": true, "internal": true, "external": true,
	"pure": true, "view": true, "payable": true, "virtual": true, "override": true,
	// Constructor / fallback
	"constructor": true, "fallback": true, "receive": true,
	// Common builtins
	"keccak256": true, "sha256": true, "ripemd160": true, "ecrecover": true,
	"addmod": true, "mulmod": true, "gasleft": true, "blockhash": true,
	"selfdestruct": true,
}

// Extract processes the Solidity source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractSolidity(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "solidity")
	extractor.TagEntitiesLanguage(out, "solidity")
	return out, nil
}

// extractSolidity is the testable core.
func extractSolidity(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity (issue #577 pattern).
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "solidity",
	}))

	// ── 1. Import edges ──────────────────────────────────────────────────
	importEntities := buildImportEntities(filePath, src)
	entities = append(entities, importEntities...)

	// Framework/tool signals from import paths (OpenZeppelin/Foundry/Hardhat).
	signals := scanImportFrameworks(collectImportPaths(src))

	// ── 2. Contracts / libraries / interfaces ────────────────────────────
	scrubbed := stripCommentsAndStrings(src)
	contracts := findContracts(scrubbed, filePath, signals)
	entities = append(entities, contracts...)

	return entities
}

// collectImportPaths returns the raw import target paths in source order.
func collectImportPaths(src string) []string {
	var out []string
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) >= 2 {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// findContracts locates all contract/library/interface declarations, emits
// SCOPE.Component entities with EXTENDS and CONTAINS edges, and also emits
// SCOPE.Operation children (functions/events/modifiers). The framework signals
// (from import paths) let it stamp OpenZeppelin/Foundry/Hardhat attributes.
func findContracts(src, filePath string, signals frameworkSignals) []types.EntityRecord {
	var out []types.EntityRecord

	matches := contractRE.FindAllStringSubmatchIndex(src, -1)
	for idx, m := range matches {
		if len(m) < 6 {
			continue
		}
		kindRaw := src[m[2]:m[3]]
		name := src[m[4]:m[5]]

		subtype := strings.TrimSpace(kindRaw)
		if strings.HasPrefix(subtype, "abstract") {
			subtype = "contract"
		}

		// Inheritance list.
		var extends []string
		if m[6] >= 0 && m[7] > m[6] {
			rawList := strings.TrimSpace(src[m[6]:m[7]])
			for _, part := range strings.Split(rawList, ",") {
				parent := strings.TrimSpace(part)
				// Strip any generic arguments e.g. "ERC20("MyToken","MTK")" — take up to first non-ident char.
				if paren := strings.IndexAny(parent, "(<{"); paren >= 0 {
					parent = strings.TrimSpace(parent[:paren])
				}
				if parent != "" {
					extends = append(extends, parent)
				}
			}
		}

		startLine := lineOf(src, m[0])

		// Find the contract body boundary.
		bodyStart := m[1] // position just past the opening '{' marker position
		// The regex anchor ends at '{', so m[1] points one past the '{'.
		body, endLine := extractBracedBody(src, bodyStart-1)
		if endLine == 0 {
			endLine = startLine
		}

		// Determine preceding contract body's end to limit function scan scope.
		var prevBodyEnd int
		if idx > 0 {
			// Use m[0] of this match as the upper bound; the body search will be constrained.
			_ = prevBodyEnd
		}

		// Signature
		rawSig := strings.TrimSpace(src[m[0] : m[1]-1])
		rawSig = strings.Join(strings.Fields(rawSig), " ")

		rec := types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    subtype,
			SourceFile: filePath,
			Language:   "solidity",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  rawSig,
		}

		// ── Framework / tool stamping (issue #5371) ──────────────────────
		// OpenZeppelin: an import of @openzeppelin/contracts OR an EXTENDS
		// parent that names a canonical OZ base contract.
		usesOZ := signals.openzeppelin
		// Foundry: forge-std import OR a forge test/script contract.
		ftKind := foundryTestKind(extends)
		usesFoundry := signals.foundry || ftKind != ""

		if usesOZ {
			setProp(&rec.Properties, "framework", "openzeppelin")
		} else if usesFoundry {
			setProp(&rec.Properties, "framework", "foundry")
		} else if signals.hardhat {
			setProp(&rec.Properties, "framework", "hardhat")
		}
		if usesFoundry {
			setProp(&rec.Properties, "foundry", "true")
			if ftKind != "" {
				setProp(&rec.Properties, "foundry_kind", ftKind)
			}
		}
		if signals.hardhat {
			setProp(&rec.Properties, "hardhat", "true")
		}

		// EXTENDS edges. OZ-base parents carry framework="openzeppelin" so the
		// inheritance edge itself records the library binding.
		for _, parent := range extends {
			edge := types.RelationshipRecord{
				ToID: parent,
				Kind: "EXTENDS",
			}
			if isOpenZeppelinBase(parent) {
				usesOZ = true
				setProp(&rec.Properties, "framework", "openzeppelin")
				setProp(&rec.Properties, "openzeppelin", "true")
				edge.Properties = types.Props{{K: "framework", V: "openzeppelin"}}
			}
			rec.Relationships = append(rec.Relationships, edge)
		}
		if usesOZ {
			setProp(&rec.Properties, "openzeppelin", "true")
		}

		contractIdx := len(out)
		out = append(out, rec)

		// ── Children: functions, events, modifiers ───────────────────────
		if body == "" {
			continue
		}
		// body starts just past '{', so a child N newlines in sits N lines below it.
		braceLine := lineOf(src, bodyStart-1)

		// Functions.
		for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			qualName := name + "." + fnName
			fnStartLine := braceLine + strings.Count(body[:fm[0]], "\n")
			fnBody, fnEnd := declBody(body, fm[1])
			fnEndLine := fnStartLine
			if fnEnd >= 0 {
				fnEndLine = braceLine + strings.Count(body[:fnEnd], "\n")
			}
			rawFnSig := declSignature(body, fm[0], fm[1])

			callRels := collectCallsFromBody(fnBody, qualName)
			fnRec := types.EntityRecord{
				Name:          qualName,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      "solidity",
				StartLine:     fnStartLine,
				EndLine:       fnEndLine,
				Signature:     rawFnSig,
				Relationships: callRels,
			}
			fnIdx := len(out)
			out = append(out, fnRec)
			_ = fnIdx

			// CONTAINS edge from contract.
			toID := extractor.BuildOperationStructuralRef("solidity", filePath, qualName)
			out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// Events.
		for _, em := range eventRE.FindAllStringSubmatchIndex(body, -1) {
			if len(em) < 4 {
				continue
			}
			evName := body[em[2]:em[3]]
			qualName := name + "." + evName
			evStartLine := braceLine + strings.Count(body[:em[0]], "\n")
			rawEvSig := declSignature(body, em[0], em[1])
			// An event is always bodiless, so declBody yields the ';' offset.
			evEndLine := evStartLine
			if _, evEnd := declBody(body, em[1]); evEnd >= 0 {
				evEndLine = braceLine + strings.Count(body[:evEnd], "\n")
			}

			evRec := types.EntityRecord{
				Name:       qualName,
				Kind:       "SCOPE.Operation",
				Subtype:    "event",
				SourceFile: filePath,
				Language:   "solidity",
				StartLine:  evStartLine,
				EndLine:    evEndLine,
				Signature:  rawEvSig,
			}
			out = append(out, evRec)

			toID := extractor.BuildOperationStructuralRef("solidity", filePath, qualName)
			out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// Modifiers.
		for _, mm := range modifierRE.FindAllStringSubmatchIndex(body, -1) {
			if len(mm) < 4 {
				continue
			}
			modName := body[mm[2]:mm[3]]
			qualName := name + "." + modName
			modStartLine := braceLine + strings.Count(body[:mm[0]], "\n")
			rawModSig := declSignature(body, mm[0], mm[1])
			modBody, modEnd := declBody(body, mm[1])
			modEndLine := modStartLine
			if modEnd >= 0 {
				modEndLine = braceLine + strings.Count(body[:modEnd], "\n")
			}

			callRels := collectCallsFromBody(modBody, qualName)
			modRec := types.EntityRecord{
				Name:          qualName,
				Kind:          "SCOPE.Operation",
				Subtype:       "modifier",
				SourceFile:    filePath,
				Language:      "solidity",
				StartLine:     modStartLine,
				EndLine:       modEndLine,
				Signature:     rawModSig,
				Relationships: callRels,
			}
			out = append(out, modRec)

			toID := extractor.BuildOperationStructuralRef("solidity", filePath, qualName)
			out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}

		// State variables.
		for _, sv := range findStateVariables(body) {
			qualName := name + "." + sv.name

			out = append(out, types.EntityRecord{
				Name:       qualName,
				Kind:       "SCOPE.Schema",
				Subtype:    "field",
				SourceFile: filePath,
				Language:   "solidity",
				StartLine:  braceLine + strings.Count(body[:sv.start], "\n"),
				EndLine:    braceLine + strings.Count(body[:sv.end], "\n"),
				Signature:  declSignature(body, sv.start, sv.end),
			})

			toID := extractor.BuildSchemaFieldStructuralRef("solidity", filePath, qualName)
			out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
				ToID: toID,
				Kind: "CONTAINS",
			})
		}
	}

	return out
}

// buildImportEntities parses import statements and returns IMPORTS entities.
func buildImportEntities(filePath, src string) []types.EntityRecord {
	seen := make(map[string]bool)
	var out []types.EntityRecord

	for _, m := range importRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		importPath := strings.TrimSpace(src[m[2]:m[3]])
		if importPath == "" || seen[importPath] {
			continue
		}
		seen[importPath] = true
		startLine := lineOf(src, m[0])

		// Display name: last path segment without extension.
		displayName := importPath
		if slash := strings.LastIndexByte(importPath, '/'); slash >= 0 {
			displayName = importPath[slash+1:]
		}
		displayName = strings.TrimSuffix(displayName, ".sol")

		props := types.Props{
			{K: "imported_name", V: displayName},
			{K: "local_name", V: displayName},
			{K: "source_module", V: importPath},
		}

		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "solidity",
			StartLine:  startLine,
			EndLine:    startLine,
			Relationships: []types.RelationshipRecord{
				{
					FromID:     filePath,
					ToID:       importPath,
					Kind:       "IMPORTS",
					Properties: props,
				},
			},
		})
	}
	return out
}

// collectCallsFromBody extracts CALLS edges from a function/modifier body.
func collectCallsFromBody(body, callerName string) []types.RelationshipRecord {
	if body == "" || callerName == "" {
		return nil
	}
	scrubbed := stripCommentsAndStrings(body)
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	addCall := func(target string) {
		if target == "" || seen[target] {
			return
		}
		if solidityKeywords[target] {
			return
		}
		// Skip bare leaf that matches caller's own short name.
		leaf := target
		if dot := strings.LastIndexByte(target, '.'); dot >= 0 {
			leaf = target[dot+1:]
		}
		if solidityKeywords[leaf] {
			return
		}
		// Self-recursion check: skip bare-name targets that match the
		// caller's own leaf name (e.g. `transfer()` calling itself without
		// a receiver). Dotted targets (e.g. "token.transfer") are
		// cross-contract calls and MUST NOT be filtered even when the
		// leaf matches the caller's leaf name — "ERC20Vault.transfer"
		// calling "token.transfer" is a legitimate outbound call, not
		// recursion (#2114). Restrict the check to undotted targets only.
		if strings.IndexByte(target, '.') < 0 {
			callerLeaf := callerName
			if dot := strings.LastIndexByte(callerName, '.'); dot >= 0 {
				callerLeaf = callerName[dot+1:]
			}
			if leaf == callerLeaf {
				return
			}
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}

	for _, m := range callDotRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}
	for _, m := range callBareRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}
	return out
}

// extractBracedBody extracts the content between a matching pair of braces.
// openPos is the index of the '{' character in src.
// Returns (body content without braces, end line number relative to src start).
// If no matching '}' is found, returns ("", 0).
func extractBracedBody(src string, openPos int) (string, int) {
	// Find the '{'.
	start := openPos
	for start < len(src) && src[start] != '{' {
		start++
	}
	if start >= len(src) {
		return "", 0
	}
	depth := 0
	i := start
	for i < len(src) {
		ch := src[i]
		// Skip single-line comment.
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// Skip block comment.
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		// Skip string literals.
		if ch == '"' || ch == '\'' {
			q := ch
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				body := src[start+1 : i]
				endLine := lineOf(src, i)
				return body, endLine
			}
		}
		i++
	}
	return "", 0
}

// declBody returns the braced body of the member declaration whose regex match
// ended at matchEnd, together with the offset of the token that ends the
// declaration: the closing '}' of the body, or the ';' of a bodiless one. It
// returns "" and -1 when the declaration is unterminated.
//
// The ';' bound is the whole point. extractBracedBody on its own scans forward
// to the next '{' it can find anywhere, so on a bodiless declaration — an
// interface method, or `virtual` in an abstract contract — it runs past the
// ';' and returns the FOLLOWING member's body. That body then supplies the
// bodiless declaration's line span and, through collectCallsFromBody, its
// CALLS edges.
//
// src is scrubbed (see stripCommentsAndStrings), so a delimiter inside a
// comment or a string literal cannot end the declaration early. matchEnd sits
// just past the '(' opening the parameter list, so the paren scan starts one
// level deep.
func declBody(src string, matchEnd int) (string, int) {
	depth := 1
	for i := matchEnd; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				return "", i
			}
		case '{':
			if depth == 0 {
				body, endLine := extractBracedBody(src, i)
				if endLine == 0 {
					return "", -1
				}
				// body is src[i+1:close], so close sits len(body)+1 past i.
				return body, i + len(body) + 1
			}
		}
	}
	return "", -1
}

// declSignature returns the declaration starting at declStart up to the first
// '{' or ';' outside parentheses, with runs of whitespace collapsed. src is
// scrubbed (see stripCommentsAndStrings), so a delimiter inside a comment or a
// string literal cannot end the signature early. Falls back to the span up to
// fallbackEnd when the declaration has no terminator.
func declSignature(src string, declStart, fallbackEnd int) string {
	depth := 0
	for i := declStart; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '{', ';':
			if depth == 0 {
				return strings.Join(strings.Fields(src[declStart:i]), " ")
			}
		}
	}
	return strings.Join(strings.Fields(src[declStart:fallbackEnd]), " ")
}

// stateVarDecl locates one state variable inside a contract body.
type stateVarDecl struct {
	name  string
	start int // offset of the declaration's first token
	end   int // offset of the terminating ';'
}

// stateVarNonDeclKeywords holds the leading keywords of the other constructs
// that end with ';' at depth zero: bodiless members of an interface or abstract
// contract (`function f() external view returns (address);`, `modifier m()
// virtual;`), signature-only declarations (`event E(uint a);`, `error E();`),
// and directives (`using SafeERC20 for IERC20;`, `type Price is uint128;`).
var stateVarNonDeclKeywords = map[string]bool{
	"function": true, "modifier": true, "receive": true, "fallback": true,
	"event": true, "error": true,
	"using": true, "type": true,
}

// findStateVariables returns the state variables declared directly in a
// contract body: the statements terminated by ';' at bracket depth zero. body
// is the inside of the contract's own braces, so depth zero is the contract
// scope by construction and a function local or a struct or enum member, which
// sits inside further braces, can never qualify.
func findStateVariables(body string) []stateVarDecl {
	var out []stateVarDecl
	depth, stmt := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{', '(', '[':
			depth++
		case ')', ']':
			depth--
		case '}':
			// A '}' back at depth zero closed a block, which ends the statement
			// without a ';'. A ')' back at depth zero does not: it is still
			// inside the declaration that opened it.
			depth--
			if depth == 0 {
				stmt = i + 1
			}
		case ';':
			if depth == 0 {
				if d, ok := parseStateVar(body, stmt, i); ok {
					out = append(out, d)
				}
				stmt = i + 1
			}
		}
	}
	return out
}

// parseStateVar reads body[stmt:end] as a state variable declaration. The name
// is the last identifier before the initializer, which lands on the variable
// whatever order the type, visibility and mutability keywords come in.
func parseStateVar(body string, stmt, end int) (stateVarDecl, bool) {
	decl := strings.TrimLeft(body[stmt:end], " \t\r\n")
	if head := solIdentAt(decl, 0); head == "" || stateVarNonDeclKeywords[head] {
		return stateVarDecl{}, false
	}
	name := lastSolIdent(decl[:solInitializerPos(decl)])
	if name == "" {
		return stateVarDecl{}, false
	}
	return stateVarDecl{name: name, start: end - len(decl), end: end}, true
}

// solInitializerPos returns the offset of the initializer '=' in decl, or
// len(decl) when the declaration has none. A mapping arrow or a comparison
// operator is not an initializer, and neither is an '=' nested in a mapping's
// parentheses or in a struct-literal initializer.
func solInitializerPos(decl string) int {
	depth := 0
	for i := 0; i < len(decl); i++ {
		switch decl[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth != 0 || (i+1 < len(decl) && (decl[i+1] == '=' || decl[i+1] == '>')) {
				continue
			}
			if i > 0 && strings.IndexByte("=!<>", decl[i-1]) >= 0 {
				continue
			}
			return i
		}
	}
	return len(decl)
}

// solIdentAt returns the identifier starting at offset i in s, or "".
func solIdentAt(s string, i int) string {
	if i >= len(s) || !isSolIdentStart(s[i]) {
		return ""
	}
	j := i + 1
	for j < len(s) && isSolIdentPart(s[j]) {
		j++
	}
	return s[i:j]
}

// lastSolIdent returns the final identifier in s, or "".
func lastSolIdent(s string) string {
	var last string
	for i := 0; i < len(s); {
		ident := solIdentAt(s, i)
		if ident == "" {
			i++
			continue
		}
		last = ident
		i += len(ident)
	}
	return last
}

func isSolIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isSolIdentPart(c byte) bool {
	return isSolIdentStart(c) || (c >= '0' && c <= '9')
}

// lineOf returns the 1-based line number of the byte at offset in src.
func lineOf(src string, offset int) int {
	return strings.Count(src[:offset], "\n") + 1
}

// stripCommentsAndStrings replaces Solidity // and /* */ comments and string
// literals with spaces so regexes don't match inside them. The result has the
// same byte length AND the same newline positions as src, so an offset into it
// names the same byte, and the same line, as that offset into src.
func stripCommentsAndStrings(src string) string {
	out := make([]byte, len(src))
	copy(out, src)
	i := 0
	for i < len(src) {
		ch := src[i]
		// Single-line comment.
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}
		// Block comment.
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					break
				}
				out[i] = ' '
				i++
			}
			continue
		}
		// String literal (double-quote).
		if ch == '"' {
			out[i] = ' '
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
				out[i] = ' '
				i++
			}
			if i < len(src) {
				out[i] = ' '
				i++
			}
			continue
		}
		// String literal (single-quote).
		if ch == '\'' {
			out[i] = ' '
			i++
			for i < len(src) && src[i] != '\'' {
				if src[i] == '\\' && i+1 < len(src) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
				out[i] = ' '
				i++
			}
			if i < len(src) {
				out[i] = ' '
				i++
			}
			continue
		}
		i++
	}
	// Restore newlines blanked inside multi-line comments and strings. Their
	// contents are already spaces, so the reintroduced line starts match nothing.
	for i := range out {
		if src[i] == '\n' {
			out[i] = '\n'
		}
	}
	return string(out)
}
