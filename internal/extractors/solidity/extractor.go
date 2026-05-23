// Package solidity implements a regex-based extractor for Solidity smart-contract files.
//
// Extracted entities:
//   - `contract Foo {…}`  / `library Foo {…}` / `interface Foo {…}` → SCOPE.Component (subtype="contract"/"library"/"interface")
//   - `function name(…) …` → SCOPE.Operation (subtype="function")
//   - `event Name(…);`    → SCOPE.Operation (subtype="event")
//   - `modifier name(…){…}` → SCOPE.Operation (subtype="modifier")
//   - `import "./Foo.sol"` / `import "…"` → IMPORTS relationship
//   - `contract Foo is Bar, Baz` → EXTENDS edges (on the contract component)
//   - Function-call expressions → CALLS edges
//   - CONTAINS edges (contract → its functions/events/modifiers)
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

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
		`(?m)^\s*import\s+(?:[^"']*\s+)?["']([^"']+)["']`,
	)

	// contractRE matches contract/library/abstract contract/interface declarations.
	// Group 1: kind keyword (contract|library|interface|abstract)
	// Group 2: name
	// Group 3: inheritance list after "is" (may be empty string)
	contractRE = regexp.MustCompile(
		`(?m)^\s*(abstract\s+contract|contract|library|interface)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:is\s+([A-Za-z_][A-Za-z0-9_,\s]*))?[{]`,
	)

	// functionRE matches function declarations inside contracts.
	// Group 1: function name (plain identifier; does NOT match receive/fallback specials)
	functionRE = regexp.MustCompile(
		`(?m)^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// eventRE matches event declarations.
	// Group 1: event name
	eventRE = regexp.MustCompile(
		`(?m)^\s*event\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)

	// modifierRE matches modifier declarations.
	// Group 1: modifier name
	modifierRE = regexp.MustCompile(
		`(?m)^\s*modifier\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
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

	// ── 2. Contracts / libraries / interfaces ────────────────────────────
	scrubbed := stripCommentsAndStrings(src)
	contracts := findContracts(scrubbed, filePath)
	entities = append(entities, contracts...)

	return entities
}

// findContracts locates all contract/library/interface declarations, emits
// SCOPE.Component entities with EXTENDS and CONTAINS edges, and also emits
// SCOPE.Operation children (functions/events/modifiers).
func findContracts(src, filePath string) []types.EntityRecord {
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

		startLine := strings.Count(src[:m[0]], "\n") + 1

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

		// EXTENDS edges.
		for _, parent := range extends {
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				FromID: filePath,
				ToID:   parent,
				Kind:   "EXTENDS",
			})
		}

		contractIdx := len(out)
		out = append(out, rec)

		// ── Children: functions, events, modifiers ───────────────────────
		if body == "" {
			continue
		}
		bodyLineOffset := startLine // functions' start-lines are relative offsets within body

		// Functions.
		for _, fm := range functionRE.FindAllStringSubmatchIndex(body, -1) {
			if len(fm) < 4 {
				continue
			}
			fnName := body[fm[2]:fm[3]]
			qualName := name + "." + fnName
			fnStartLine := bodyLineOffset + strings.Count(body[:fm[0]], "\n")
			fnBody, fnEndLine := extractBracedBody(body, fm[1])
			_ = fnBody
			if fnEndLine == 0 {
				fnEndLine = fnStartLine
			}
			sigEnd := fm[1]
			rawFnSig := strings.Join(strings.Fields(body[fm[0]:sigEnd]), " ")

			callRels := collectCallsFromBody(fnBody, qualName)
			fnRec := types.EntityRecord{
				Name:          qualName,
				Kind:          "SCOPE.Operation",
				Subtype:       "function",
				SourceFile:    filePath,
				Language:      "solidity",
				StartLine:     fnStartLine,
				EndLine:       fnStartLine + strings.Count(fnBody, "\n"),
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
			evStartLine := bodyLineOffset + strings.Count(body[:em[0]], "\n")
			rawEvSig := strings.Join(strings.Fields(body[em[0]:em[1]]), " ")

			evRec := types.EntityRecord{
				Name:       qualName,
				Kind:       "SCOPE.Operation",
				Subtype:    "event",
				SourceFile: filePath,
				Language:   "solidity",
				StartLine:  evStartLine,
				EndLine:    evStartLine,
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
			modStartLine := bodyLineOffset + strings.Count(body[:mm[0]], "\n")
			rawModSig := strings.Join(strings.Fields(body[mm[0]:mm[1]]), " ")
			modBody, _ := extractBracedBody(body, mm[1])

			callRels := collectCallsFromBody(modBody, qualName)
			modRec := types.EntityRecord{
				Name:          qualName,
				Kind:          "SCOPE.Operation",
				Subtype:       "modifier",
				SourceFile:    filePath,
				Language:      "solidity",
				StartLine:     modStartLine,
				EndLine:       modStartLine + strings.Count(modBody, "\n"),
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
	}

	return out
}

// buildImportEntities parses import statements and returns IMPORTS entities.
func buildImportEntities(filePath, src string) []types.EntityRecord {
	seen := make(map[string]bool)
	var out []types.EntityRecord

	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		importPath := strings.TrimSpace(m[1])
		if importPath == "" || seen[importPath] {
			continue
		}
		seen[importPath] = true

		// Display name: last path segment without extension.
		displayName := importPath
		if slash := strings.LastIndexByte(importPath, '/'); slash >= 0 {
			displayName = importPath[slash+1:]
		}
		displayName = strings.TrimSuffix(displayName, ".sol")

		props := map[string]string{
			"source_module": importPath,
			"imported_name": displayName,
			"local_name":    displayName,
		}

		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "solidity",
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
		// Avoid self-recursion on simple name match.
		callerLeaf := callerName
		if dot := strings.LastIndexByte(callerName, '.'); dot >= 0 {
			callerLeaf = callerName[dot+1:]
		}
		if leaf == callerLeaf {
			return
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
				endLine := strings.Count(src[:i], "\n") + 1
				return body, endLine
			}
		}
		i++
	}
	return "", 0
}

// stripCommentsAndStrings replaces Solidity // and /* */ comments and string
// literals with spaces so regexes don't match inside them.
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
	return string(out)
}
