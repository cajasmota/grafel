// Package solidity implements a regex-based extractor for Solidity smart-contract files.
//
// Extracted entities:
//   - `contract Foo {…}`  / `library Foo {…}` / `interface Foo {…}` → SCOPE.Component (subtype="contract"/"library"/"interface")
//   - `function name(…) …` → SCOPE.Operation (subtype="function"), inside a
//     contract or at file level (a free function)
//   - `constructor(…){…}`  → SCOPE.Operation (subtype="constructor")
//   - `receive(){…}` / `fallback(){…}` → SCOPE.Operation (subtype="receive"/"fallback")
//   - `event Name(…);`    → SCOPE.Operation (subtype="event")
//   - `error Name(…);`    → SCOPE.Operation (subtype="error")
//   - `modifier name(…){…}` → SCOPE.Operation (subtype="modifier")
//   - `struct S {…}` / `enum E {…}` → SCOPE.Schema (subtype="struct"/"enum")
//   - `type T is uint128;` → SCOPE.Schema (subtype="type")
//   - `uint256 public cap;` → SCOPE.Schema (subtype="field") for contract-level state variables
//   - `import "./Foo.sol"` / `import "…"` → IMPORTS relationship
//   - `contract Foo is Bar, Baz` → EXTENDS edges (on the contract component),
//     including the base-constructor form `is ERC20("n","s")`
//   - Function-call expressions → CALLS edges
//   - Modifier usage in a declaration's attribute section → CALLS edges
//   - CONTAINS edges (contract → its members)
//
// Every declaration form except `function`/`event`/`modifier`/state variables
// was added by #6423; before it, a contract with base-constructor arguments
// matched nothing and took its whole file's contents with it.
//
// No tree-sitter grammar for Solidity is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions.
//
// Registers itself via init() and is imported by registry_gen.go.
package solidity

import (
	"context"
	"regexp"
	"strconv"
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
	//
	// The inheritance group is `[^{};]*`, not an identifier class, because a
	// base-constructor call belongs there: `contract MyToken is ERC20("My
	// Token", "MTK")` is ordinary Solidity and the canonical OpenZeppelin
	// usage. The old class `[A-Za-z_][A-Za-z0-9_,\s]*` could not hold `(`, so
	// the whole declaration failed to match — and since findContracts only
	// walks matched bodies, the contract and every member inside it vanished
	// from the graph rather than degrading (#6423). Excluding `{` is what
	// bounds the group at the contract's own opening brace; excluding `;`
	// keeps a malformed, brace-less declaration from swallowing the rest of
	// the file. The list is split by parseInheritanceList, which is
	// paren-aware — splitting the raw text on every comma yields the
	// "parents" `ERC20(` and `)`.
	contractRE = regexp.MustCompile(
		`(?m)^[ \t]*(abstract\s+contract|contract|library|interface)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:is\s+([^{};]*))?[{]`,
	)

	// functionRE matches function declarations. Inside a contract body it
	// finds members; run against a source whose contract bodies have been
	// masked out (see maskRanges) it finds file-level free functions.
	// Group 1: function name (plain identifier; does NOT match receive/fallback specials)
	functionRE = regexp.MustCompile(
		`(?m)^[ \t]*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// constructorRE matches a constructor. The keyword IS the declaration —
	// there is no name to capture — which is why functionRE cannot find one
	// and why nothing was emitted for the member where OpenZeppelin-style
	// contracts do their wiring (#6423).
	constructorRE = regexp.MustCompile(
		`(?m)^[ \t]*constructor\s*\(`,
	)

	// specialFunctionRE matches the two nameless entry points, `receive()`
	// and `fallback()`. Group 1 is the keyword, which is also the member name.
	specialFunctionRE = regexp.MustCompile(
		`(?m)^[ \t]*(receive|fallback)\s*\(`,
	)

	// errorRE matches custom error declarations (Solidity >=0.8.4). Like an
	// event, an error is a bodiless signature terminated by ';'.
	// Group 1: error name
	errorRE = regexp.MustCompile(
		`(?m)^[ \t]*error\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// structRE matches struct declarations. Group 1: struct name.
	structRE = regexp.MustCompile(
		`(?m)^[ \t]*struct\s+([A-Za-z_][A-Za-z0-9_]*)\s*[{]`,
	)

	// enumRE matches enum declarations. Group 1: enum name.
	enumRE = regexp.MustCompile(
		`(?m)^[ \t]*enum\s+([A-Za-z_][A-Za-z0-9_]*)\s*[{]`,
	)

	// userTypeRE matches a user-defined value type, `type Price is uint128;`.
	// Group 1: the type name. Requiring whitespace after the keyword is what
	// separates the declaration from the `type(uint256).max` builtin, whose
	// keyword is immediately followed by '('.
	userTypeRE = regexp.MustCompile(
		`(?m)^[ \t]*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+is\s`,
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
	// Hung on the file entity (entities[0]) rather than on a per-import
	// SCOPE.Component placeholder — see buildImportRelationships for why
	// (issue #6368; the #742 / #681 / #693 pattern).
	entities[0].Relationships = append(entities[0].Relationships,
		buildImportRelationships(filePath, src)...)

	// Framework/tool signals from import paths (OpenZeppelin/Foundry/Hardhat).
	signals := scanImportFrameworks(collectImportPaths(src))

	// ── 2. Contracts / libraries / interfaces ────────────────────────────
	scrubbed := stripCommentsAndStrings(src)
	contracts, spans := findContracts(scrubbed, filePath, signals)
	entities = append(entities, contracts...)

	// ── 3. File-level declarations ───────────────────────────────────────
	// Free functions, errors, structs, enums and user-defined value types can
	// all be declared outside any contract, and none of them had a code path
	// before #6423. The scan runs against a copy of the source with every
	// contract's declaration and body blanked out, so a member cannot be
	// emitted twice — once qualified by its contract and once bare — and the
	// masking preserves byte offsets and newline positions, so an offset into
	// the masked text still names the same line.
	entities = append(entities, findFileLevelDecls(maskRanges(scrubbed, spans), filePath)...)

	return entities
}

// span is a half-open byte range [start, end) of the scrubbed source.
type span struct{ start, end int }

// maskRanges returns src with every byte inside one of the ranges replaced by
// a space, leaving newlines in place so offsets and line numbers are unchanged.
func maskRanges(src string, ranges []span) string {
	if len(ranges) == 0 {
		return src
	}
	out := []byte(src)
	for _, r := range ranges {
		if r.start < 0 {
			r.start = 0
		}
		if r.end > len(out) {
			r.end = len(out)
		}
		for i := r.start; i < r.end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

// findFileLevelDecls emits the declarations that live outside any contract.
// masked is the scrubbed source with contract declarations blanked out, so a
// match here is a file-level declaration by construction.
func findFileLevelDecls(masked, filePath string) []types.EntityRecord {
	var out []types.EntityRecord

	// Free functions carry a body and therefore CALLS edges, exactly as a
	// contract member does. Extracting them also un-dangles the edges that
	// already pointed at them: before #6423 a call to a free function was
	// emitted as an unresolved bare string because the callee was never an
	// entity.
	for _, fm := range functionRE.FindAllStringSubmatchIndex(masked, -1) {
		if len(fm) < 4 {
			continue
		}
		name := masked[fm[2]:fm[3]]
		fnBody, fnEnd := declBody(masked, fm[1])
		rec := types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Operation",
			Subtype:       "function",
			SourceFile:    filePath,
			Language:      "solidity",
			StartLine:     lineOf(masked, fm[0]),
			EndLine:       lineOf(masked, fm[0]),
			Signature:     declSignature(masked, fm[0], fm[1]),
			Relationships: declCalls(masked, fm[1], fnBody, name),
		}
		if fnEnd >= 0 {
			rec.EndLine = lineOf(masked, fnEnd)
		}
		out = append(out, rec)
	}

	for _, spec := range memberDeclSpecs {
		for _, m := range spec.re.FindAllStringSubmatchIndex(masked, -1) {
			if len(m) < 4 {
				continue
			}
			name := masked[m[2]:m[3]]
			out = append(out, types.EntityRecord{
				Name:       name,
				Kind:       spec.kind,
				Subtype:    spec.subtype,
				SourceFile: filePath,
				Language:   "solidity",
				StartLine:  lineOf(masked, m[0]),
				EndLine:    lineOf(masked, declEndOffset(masked, m[1], spec.bodied, m[0])),
				Signature:  declSignature(masked, m[0], m[1]),
			})
		}
	}
	return out
}

// memberDeclSpecs are the type-ish declarations that may appear at file level
// or directly inside a contract body, and that had no code path before #6423.
// Errors are SCOPE.Operation for the same reason events are: they are
// bodiless, invocable signatures, and `revert E(...)` reads as a call site.
var memberDeclSpecs = []struct {
	re      *regexp.Regexp
	kind    string
	subtype string
	bodied  bool // true when the regex match ends at '{' rather than at '('
}{
	{errorRE, "SCOPE.Operation", "error", false},
	{structRE, "SCOPE.Schema", "struct", true},
	{enumRE, "SCOPE.Schema", "enum", true},
	{userTypeRE, "SCOPE.Schema", "type", false},
}

// declEndOffset returns the offset of the byte that ends a declaration whose
// regex match ended at matchEnd. A bodied declaration (struct/enum) ends at
// the '}' closing the brace the match consumed; a bodiless one ends at its
// ';'. fallback is returned when the declaration is unterminated.
func declEndOffset(src string, matchEnd int, bodied bool, fallback int) int {
	if bodied {
		body, endLine := extractBracedBody(src, matchEnd-1)
		if endLine == 0 {
			return fallback
		}
		return matchEnd + len(body)
	}
	// `type X is Y;` never opens a paren, so declBody's paren depth of 1 would
	// never return to zero. Scan for the terminator directly instead.
	for i := matchEnd; i < len(src); i++ {
		if src[i] == ';' {
			return i
		}
	}
	return fallback
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
// It also returns the byte span of every contract declaration (keyword through
// closing brace) so the caller can mask them out before scanning for
// file-level declarations.
func findContracts(src, filePath string, signals frameworkSignals) ([]types.EntityRecord, []span) {
	var out []types.EntityRecord
	var spans []span

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
			extends = parseInheritanceList(src[m[6]:m[7]])
		}

		startLine := lineOf(src, m[0])

		// Find the contract body boundary.
		bodyStart := m[1] // position just past the opening '{' marker position
		// The regex anchor ends at '{', so m[1] points one past the '{'.
		body, endLine := extractBracedBody(src, bodyStart-1)
		unterminated := endLine == 0
		if unterminated {
			endLine = startLine
		}
		// The declaration span, for the file-level mask. When the body is
		// unterminated extractBracedBody returns an empty body, so
		// `bodyStart + len(body) + 1` degenerates to the header — and masking
		// LESS is the dangerous direction here, not the safe one: whatever the
		// mask leaves visible, findFileLevelDecls emits. An unterminated
		// contract left its entire body unmasked and every member came back out
		// as a bare, top-level entity with no CONTAINS owner — a phantom that
		// did not exist before #6423, because the file-level scan did not
		// exist. An unterminated contract runs to the end of the file by
		// definition, so the span does too; the members are lost with it, which
		// is the pre-#6423 behaviour and the honest one, since a member of an
		// unterminated contract has no measurable span (#6423 review).
		declEnd := bodyStart + len(body) + 1
		if unterminated {
			declEnd = len(src)
		}
		spans = append(spans, span{start: m[0], end: declEnd})

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

		// Every member regex below is line-anchored (`(?m)^[ \t]*…`), not
		// scope-aware, so it matches just as happily inside a nested function
		// body as at contract level. `fallback();` — an ordinary statement
		// dispatching to this contract's own fallback — sits at the start of a
		// line and matched specialFunctionRE, minting a phantom member plus a
		// CONTAINS edge from the contract (#6423 review). findStateVariables
		// has tracked brace depth from the start for exactly this reason; the
		// member scans now do too.
		//
		// Only the function/constructor/receive/fallback scan is REACHABLE at
		// depth > 0 today: `fallback();` and `receive();` are ordinary
		// statements, and a Yul `function helper(x) -> y {` inside `assembly`
		// matches functionRE two braces deep (the #6425 phantom, which this
		// guard also removes). No valid Solidity puts an `event`, `modifier`,
		// `error`, `struct`, `enum` or `type X is Y` declaration inside a
		// function body, so the guard on those three loops cannot fire on
		// well-formed input and no test pins it. It is applied anyway, and
		// said so here rather than left implicit, because that reachability
		// argument rests on the LANGUAGE and not on the scanner: the scanner
		// is line-anchored either way, and a member kind added later would
		// otherwise inherit the bug silently.
		depths := braceDepths(body)
		isMemberPos := func(off int) bool {
			return off >= 0 && off < len(depths) && depths[off] == 0
		}

		// Functions, constructors, and the receive/fallback entry points.
		// The last three emitted nothing at all before #6423: functionRE
		// requires the literal `function` keyword, which none of them has.
		for _, spec := range []struct {
			re *regexp.Regexp
			// fixedName is the member name when the keyword is the whole
			// declaration (a constructor); empty when group 1 holds the name.
			fixedName string
			subtype   string
		}{
			{functionRE, "", "function"},
			{constructorRE, "constructor", "constructor"},
			{specialFunctionRE, "", ""},
		} {
			for _, fm := range spec.re.FindAllStringSubmatchIndex(body, -1) {
				if !isMemberPos(fm[0]) {
					continue // a statement inside another member's body
				}
				fnName, subtype := spec.fixedName, spec.subtype
				if fnName == "" {
					if len(fm) < 4 {
						continue
					}
					fnName = body[fm[2]:fm[3]]
				}
				if subtype == "" {
					subtype = fnName
				}
				qualName := name + "." + fnName
				fnStartLine := braceLine + strings.Count(body[:fm[0]], "\n")
				fnBody, fnEnd := declBody(body, fm[1])
				fnEndLine := fnStartLine
				if fnEnd >= 0 {
					fnEndLine = braceLine + strings.Count(body[:fnEnd], "\n")
				}

				fnRec := types.EntityRecord{
					Name:          qualName,
					Kind:          "SCOPE.Operation",
					Subtype:       subtype,
					SourceFile:    filePath,
					Language:      "solidity",
					StartLine:     fnStartLine,
					EndLine:       fnEndLine,
					Signature:     declSignature(body, fm[0], fm[1]),
					Relationships: declCalls(body, fm[1], fnBody, qualName),
				}
				out = append(out, fnRec)

				// CONTAINS edge from contract.
				toID := extractor.BuildOperationStructuralRef("solidity", filePath, qualName)
				out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
			}
		}

		// Errors, structs, enums and user-defined value types. All four are
		// legal directly inside a contract body as well as at file level, and
		// asserting only one position lets a half-fix look complete (#6423).
		for _, spec := range memberDeclSpecs {
			for _, dm := range spec.re.FindAllStringSubmatchIndex(body, -1) {
				if len(dm) < 4 || !isMemberPos(dm[0]) {
					continue
				}
				qualName := name + "." + body[dm[2]:dm[3]]
				out = append(out, types.EntityRecord{
					Name:       qualName,
					Kind:       spec.kind,
					Subtype:    spec.subtype,
					SourceFile: filePath,
					Language:   "solidity",
					StartLine:  braceLine + strings.Count(body[:dm[0]], "\n"),
					EndLine:    braceLine + strings.Count(body[:declEndOffset(body, dm[1], spec.bodied, dm[0])], "\n"),
					Signature:  declSignature(body, dm[0], dm[1]),
				})

				toID := extractor.BuildOperationStructuralRef("solidity", filePath, qualName)
				if spec.kind == "SCOPE.Schema" {
					toID = extractor.BuildSchemaFieldStructuralRef("solidity", filePath, qualName)
				}
				out[contractIdx].Relationships = append(out[contractIdx].Relationships, types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
			}
		}

		// Events.
		for _, em := range eventRE.FindAllStringSubmatchIndex(body, -1) {
			if len(em) < 4 || !isMemberPos(em[0]) {
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
			if len(mm) < 4 || !isMemberPos(mm[0]) {
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

	return out, spans
}

// parseInheritanceList splits the text of an `is` clause into parent names.
// It splits on commas at paren depth zero and drops any argument list, so
// `ERC20("My Token", "MTK"), Ownable` yields [ERC20 Ownable]. Splitting on
// every comma instead yields `ERC20(` and `)` — the second of which is not
// even an identifier (#6423). The comma inside the base-constructor call is
// the reason the depth tracking is needed rather than a plain suffix trim.
func parseInheritanceList(raw string) []string {
	var out []string
	depth, start := 0, 0
	flush := func(part string) {
		if cut := strings.IndexAny(part, "(<{"); cut >= 0 {
			part = part[:cut]
		}
		if parent := strings.TrimSpace(part); parent != "" {
			out = append(out, parent)
		}
	}
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				flush(raw[start:i])
				start = i + 1
			}
		}
	}
	flush(raw[start:])
	return out
}

// solDeclAttrKeywords are the tokens that may appear in a declaration's
// attribute section without being a modifier invocation.
var solDeclAttrKeywords = map[string]bool{
	"public": true, "private": true, "internal": true, "external": true,
	"pure": true, "view": true, "payable": true, "nonpayable": true,
	"virtual": true, "override": true, "returns": true, "constant": true,
	"immutable": true, "anonymous": true, "memory": true, "calldata": true,
	"storage": true, "indexed": true,
}

// declAttributes returns the attribute section of a member declaration whose
// regex match ended at matchEnd: the text between the parameter list's closing
// ')' and the body's '{' (or the ';' of a bodiless declaration).
//
// This is the region declBody deliberately skips over, and skipping it is why
// modifier *usage* produced no edge at all before #6423 — modifiers were
// declared and contained, never connected to the functions applying them.
// matchEnd sits just past the '(' opening the parameter list, so the scan
// starts one level deep, exactly as declBody's does.
func declAttributes(src string, matchEnd int) string {
	depth, tailStart := 1, -1
	for i := matchEnd; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && tailStart < 0 {
				tailStart = i + 1
			}
		case '{', ';':
			if depth == 0 && tailStart >= 0 && tailStart <= i {
				return src[tailStart:i]
			}
		}
	}
	return ""
}

// modifierUsages returns the modifier invocations named in an attribute
// section, in source order and deduplicated. A base-constructor call —
// `constructor(...) Ownable(initialOwner)` — sits in the same position and is
// returned too: it is a genuine invocation of the base contract.
//
// Every identifier that is not a known attribute keyword counts, and any
// parenthesised argument list is skipped whole. That skip is what keeps the
// scan from minting `uint256` out of `returns (uint256)` and `A`/`B` out of
// `override(A, B)`: a recall fix that took every identifier here would raise
// the entity count and quietly fill the graph with keyword call targets.
func modifierUsages(attrs string) []string {
	var out []string
	seen := make(map[string]bool)
	for i := 0; i < len(attrs); {
		ident := solIdentAt(attrs, i)
		if ident == "" {
			i++
			continue
		}
		i += len(ident)
		for i < len(attrs) && (attrs[i] == ' ' || attrs[i] == '\t' || attrs[i] == '\n' || attrs[i] == '\r') {
			i++
		}
		if i < len(attrs) && attrs[i] == '(' {
			depth := 0
			for ; i < len(attrs); i++ {
				if attrs[i] == '(' {
					depth++
				} else if attrs[i] == ')' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
			}
		}
		if solDeclAttrKeywords[ident] || solidityKeywords[ident] || seen[ident] {
			continue
		}
		seen[ident] = true
		out = append(out, ident)
	}
	return out
}

// declCalls returns the CALLS edges for one member declaration: the modifier
// invocations from its attribute section first, then the calls in its body.
// Body targets that repeat a modifier name are dropped, so applying a modifier
// and calling a same-named function yields one edge rather than two.
func declCalls(src string, matchEnd int, declaredBody, qualName string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	seen := make(map[string]bool)
	for _, mod := range modifierUsages(declAttributes(src, matchEnd)) {
		seen[mod] = true
		out = append(out, types.RelationshipRecord{ToID: mod, Kind: "CALLS"})
	}
	for _, rel := range collectCallsFromBody(declaredBody, qualName) {
		if seen[rel.ToID] {
			continue
		}
		seen[rel.ToID] = true
		out = append(out, rel)
	}
	return out
}

// buildImportRelationships parses import statements and returns the IMPORTS
// relationships, one per distinct import path, for the caller to hang on the
// per-file SCOPE.Component (subtype="file") carrier.
//
// # Why there is no entity here (issue #6368)
//
// This used to be buildImportEntities, emitting one SCOPE.Component per
// distinct import path named after the path basename minus ".sol". That
// entity was a pure carrier — nothing referenced it — and it was actively
// harmful, because the loop dedupes by PATH while naming by BASENAME and
// graph.EntityID hashes repo|kind|name|sourceFile with neither Subtype nor the
// line span in it. Two records could therefore share one id:
//
//   - a file declaring `interface IERC20` that also imports another
//     IERC20.sol: the placeholder (the import line) and the interface;
//   - `./a/Token.sol` and `./b/Token.sol` imported by one file: two
//     placeholders, two paths, one id.
//
// Both were silent, and both resolved differently on the two indexing paths.
// On the CLI full rebuild (Path B) resolve.PruneImportPlaceholders would drop
// a placeholder marked Subtype:"import"; on the daemon's incremental reindex
// (internal/extractors.TryIncremental, Path A — the DEFAULT, #5231) nothing
// prunes, and convertExtractedRecords folds the colliding records with
// foldDuplicateEntity's gap-fill-never-override rule. Since extractSolidity
// appends imports before contracts, the placeholder always won the survivor
// slot: an unmarked placeholder took the declaration's Subtype but kept the
// import statement's one-line span, and a marked one made a real
// `interface IERC20` read as subtype="import".
//
// Not emitting the record is the fix that holds on both paths, because the
// colliding row is never created. It is also the established in-repo answer
// for this exact shape — JS/TS #742 (see
// internal/extractors/javascript/prune_import_placeholders.go), Java #681/#694
// and Python #693/#715 all moved their IMPORTS edges onto the file carrier.
// #742's correctness invariants were re-verified for solidity:
//
//  1. The file entity exists and is entities[0] — extractSolidity appends
//     extractor.FileEntity unconditionally before anything else.
//  2. The IMPORTS FromID is still the file path — unchanged below.
//  3. resolve.BuildImportTable walks EVERY record's Relationships and keys on
//     rel.FromID falling back to r.SourceFile (imports.go:216-226); it never
//     requires an import-placeholder host. Both values are the file path here.
//  4. The cross-repo linker (#566/#570) matches file-level SCOPE.Component
//     entities, which is now the host rather than a sibling of the host.
//  5. Real contract/interface/library components and the file entity are
//     untouched.
//  6. Solidity has no extractor-side import binding table to invalidate (the
//     substrate's is built from source text in internal/substrate/solidity.go,
//     not from these records).
//
// # The edge is carried over UNCHANGED, including its ToID
//
// ToID stays the raw import specifier, exactly as the placeholder's edge
// always carried it. #742 is explicit that the IMPORTS edge and its
// Properties are preserved and only the wrapper entity is dropped, and that
// restraint is load-bearing here.
//
// Resolving the specifier to a repo-relative path (`./a/Token.sol` in
// `src/Main.sol` -> `src/a/Token.sol`, which is the Name of that file's own
// carrier) was tried and MEASURED, because it would have let the byName tier
// bind these edges and so kept the import-hygiene and cycle-detection numbers
// the marker approach had produced. It binds two of the fixtures correctly and
// MIS-BINDS a third: on a two-file import cycle, `./B.sol` from `src/A.sol`
// resolved to `src/B.sol` and ReferencesEmbedded bound it to
// `SCOPE.Operation/B.pong` — a function, not the file or the contract. A wrong
// bind is worse than an unresolved endpoint, and it is the same failure class
// (#6296) this issue exists to remove, so the resolution was dropped. Making
// those name tiers handle solidity import paths is resolver work and belongs
// to #6369.
//
// Consequence, stated plainly: the #642 pre-prune ToID rewrite
// (imports.go:2887) and the #6156 `ext:` restore (imports.go:3384) are BOTH
// gated on a live `Kind=="SCOPE.Component" && Subtype=="import"` record, so
// neither fires for solidity once the placeholder is gone. Solidity IMPORTS
// ToIDs remain raw path strings — unchanged from before this issue, not a
// regression, but not an improvement either. What this change fixes is the
// EntityID collision, on both indexing paths.
func buildImportRelationships(filePath, src string) []types.RelationshipRecord {
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

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

		// Local name: last path segment without extension. Unchanged — it is
		// what the import table binds on, and internal/substrate/solidity.go
		// computes the identical value independently at :61-66.
		displayName := importPath
		if slash := strings.LastIndexByte(importPath, '/'); slash >= 0 {
			displayName = importPath[slash+1:]
		}
		displayName = strings.TrimSuffix(displayName, ".sol")

		out = append(out, types.RelationshipRecord{
			FromID: filePath,
			ToID:   importPath,
			Kind:   "IMPORTS",
			// types.Props is a SORTED slice and Props.Get binary-searches it
			// (types/props.go:67 -> find); a literal in the wrong key order
			// silently reads back as absent. Keep these ascending by key.
			Properties: types.Props{
				{K: "imported_name", V: displayName},
				// The import statement's line. It used to live on the
				// placeholder's StartLine — TestSolidity_LineAttribution was
				// written for the defect where "imports were emitted with no
				// line at all" — so dropping the entity must not drop the
				// line with it. `line` is the repo-wide relationship
				// convention (ocaml:509, zig:376, nim:450, svelte:708, …).
				{K: "line", V: strconv.Itoa(startLine)},
				{K: "local_name", V: displayName},
				{K: "source_module", V: importPath},
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

// braceDepths returns, for each byte of a contract body, the brace nesting
// depth that byte sits at: zero for a byte directly in the contract scope,
// one or more for a byte inside a function body, a struct, an enum or an
// assembly block. An opening '{' is reported at the OUTER depth and its
// matching '}' likewise, so "the match starts at depth zero" is exactly "the
// declaration is a direct member of this contract".
//
// The member scans need this for the same reason findStateVariables tracks
// depth inline: the declaration regexes are line-anchored, not scope-aware.
// body is the scrubbed source (stripCommentsAndStrings ran before
// findContracts), so a brace inside a comment or a string literal cannot skew
// the count. Refs #6423 review.
func braceDepths(body string) []int {
	depths := make([]int, len(body))
	depth := 0
	for i := 0; i < len(body); i++ {
		if body[i] == '}' && depth > 0 {
			depth--
		}
		depths[i] = depth
		if body[i] == '{' {
			depth++
		}
	}
	return depths
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
