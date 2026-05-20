// Package crystal implements a regex-based extractor for Crystal source files.
//
// Extracted entities:
//   - class / abstract class / struct   → Kind="SCOPE.Component", Subtype="class"
//   - module                            → Kind="SCOPE.Component", Subtype="module"
//   - lib (C binding block)             → Kind="SCOPE.Component", Subtype="lib"
//   - def / abstract def                → Kind="SCOPE.Operation",  Subtype="method"
//   - macro                             → Kind="SCOPE.Operation",  Subtype="macro"
//
// Relationships emitted:
//   - IMPORTS   — `require "..."` / `require_relative "..."` → SCOPE.Component placeholder
//   - CALLS     — every `name(` invocation inside a def/macro body
//   - CONTAINS  — each class/module/struct links to its defs/macros (Format A ref)
//   - EXTENDS   — `class Foo < Bar` inheritance edge
//
// No tree-sitter grammar for Crystal is available in smacker/go-tree-sitter
// (the project tracks an open upstream gap; see #44). This extractor therefore
// uses line-oriented regex parsing, matching the Dart extractor precedent.
//
// Registers itself via init() and is imported by registry_gen.go.
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("crystal", &Extractor{})
}

// Extractor implements extractor.Extractor for Crystal.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "crystal" }

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

var (
	// classRE matches class / abstract class / struct declarations.
	// Group 1: name; Group 2: optional superclass name (after `< ClassName`).
	classRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?(?:class|struct)\s+(\w+)(?:\s*<\s*(\w[\w:]*))?(?:\s|$)`,
	)

	// moduleRE matches module declarations.
	// Group 1: module name.
	moduleRE = regexp.MustCompile(
		`(?m)^[ \t]*module\s+([\w:]+)(?:\s|$)`,
	)

	// libRE matches lib (C binding) declarations.
	// Group 1: lib name.
	libRE = regexp.MustCompile(
		`(?m)^[ \t]*lib\s+(\w+)(?:\s|$)`,
	)

	// requireRE matches `require "path"` and `require_relative "path"`.
	// Group 1: path string content.
	requireRE = regexp.MustCompile(
		`(?m)^[ \t]*require(?:_relative)?\s+"([^"]+)"`,
	)

	// defRE matches `def name`, `abstract def name`, and `def self.name`.
	// Group 1: full def name (may contain `self.`).
	defRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?def\s+((?:self\.)?[\w?!]+)(?:\s*[\(\n]|$)`,
	)

	// macroRE matches `macro name`.
	// Group 1: macro name.
	macroRE = regexp.MustCompile(
		`(?m)^[ \t]*macro\s+(\w+)(?:\s*[\(\n]|$)`,
	)

	// callRE matches `[receiver.]name(` invocations.
	// Group 1: optional receiver chain ending with `.`.
	// Group 2: callee identifier.
	callRE = regexp.MustCompile(
		`((?:[A-Za-z_][\w]*\.)+)?([A-Za-z_][\w?!]*)\s*\(`,
	)
)

// skipKeywords are Crystal keywords / control-flow constructs that match
// the call pattern but are not actual method invocations.
var skipKeywords = map[string]bool{
	"if": true, "unless": true, "while": true, "until": true,
	"for": true, "do": true, "case": true, "when": true,
	"rescue": true, "ensure": true, "begin": true,
	"return": true, "yield": true, "raise": true,
	"require": true, "require_relative": true,
	"def": true, "class": true, "module": true, "struct": true,
	"macro": true, "lib": true, "enum": true, "union": true,
	"abstract": true, "private": true, "protected": true,
	"new": true, "super": true, "self": true,
	"true": true, "false": true, "nil": true,
	"typeof": true, "sizeof": true, "instance_sizeof": true,
	"offsetof": true, "pointerof": true, "out": true,
	"include": true, "extend": true, "prepend": true,
}

// callKeywordReceivers are tokens that should be stripped when they appear
// as the receiver root (`self.foo()` → target="foo", recv="").
var callKeywordReceivers = map[string]bool{
	"self":  true,
	"super": true,
}

// Extract processes a Crystal source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractCrystal(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "crystal")
	return out, nil
}

// ---------------------------------------------------------------------------
// Core extraction
// ---------------------------------------------------------------------------

// scopeSpan tracks a class/module/struct's byte range so we can:
//  1. Attach CONTAINS edges from the scope to its defs.
//  2. Assign a def to the innermost enclosing scope.
type scopeSpan struct {
	name      string
	startByte int
	endByte   int // byte offset of the closing `end`
	startLine int
	endLine   int
	idx       int // index in the output slice
}

func extractCrystal(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// ── 1. Require / import edges ─────────────────────────────────────────
	for _, m := range requireRE.FindAllStringSubmatchIndex(src, -1) {
		path := src[m[2]:m[3]]
		if path == "" {
			continue
		}
		entities = append(entities, types.EntityRecord{
			Name:       path,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "crystal",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   path,
					Kind:   "IMPORTS",
				},
			},
		})
	}

	// ── 2. Scope declarations (class / struct / module / lib) ────────────
	var scopes []scopeSpan

	// classes and structs
	for _, m := range classRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endByte := findEndKeyword(src, m[1])
		endLine := strings.Count(src[:endByte], "\n") + 1
		superclass := ""
		if m[4] >= 0 {
			superclass = src[m[4]:m[5]]
		}
		rec := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "class",
			SourceFile:         filePath,
			Language:           "crystal",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          buildScopeSignature(src, m[0]),
			EnrichmentRequired: false,
		}
		// EXTENDS edge when a superclass is declared.
		if superclass != "" {
			rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
				ToID: superclass,
				Kind: "EXTENDS",
			})
		}
		idx := len(entities)
		entities = append(entities, rec)
		scopes = append(scopes, scopeSpan{
			name:      name,
			startByte: m[0],
			endByte:   endByte,
			startLine: startLine,
			endLine:   endLine,
			idx:       idx,
		})
	}

	// modules
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endByte := findEndKeyword(src, m[1])
		endLine := strings.Count(src[:endByte], "\n") + 1
		idx := len(entities)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "module",
			SourceFile:         filePath,
			Language:           "crystal",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          buildScopeSignature(src, m[0]),
			EnrichmentRequired: false,
		})
		scopes = append(scopes, scopeSpan{
			name:      name,
			startByte: m[0],
			endByte:   endByte,
			startLine: startLine,
			endLine:   endLine,
			idx:       idx,
		})
	}

	// lib blocks (C bindings)
	for _, m := range libRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endByte := findEndKeyword(src, m[1])
		endLine := strings.Count(src[:endByte], "\n") + 1
		idx := len(entities)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "lib",
			SourceFile:         filePath,
			Language:           "crystal",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          buildScopeSignature(src, m[0]),
			EnrichmentRequired: false,
		})
		scopes = append(scopes, scopeSpan{
			name:      name,
			startByte: m[0],
			endByte:   endByte,
			startLine: startLine,
			endLine:   endLine,
			idx:       idx,
		})
	}

	// ── 3. Method / macro declarations ───────────────────────────────────
	emitOperation := func(name, subtype string, matchStart, afterKeyword int) {
		startLine := strings.Count(src[:matchStart], "\n") + 1
		bodyEnd := findEndKeyword(src, afterKeyword)
		endLine := strings.Count(src[:bodyEnd], "\n") + 1

		// Skip if this byte is a scope declaration start (class/module head).
		if isScopeDeclaration(scopes, matchStart) {
			return
		}

		bodyStart := afterKeyword
		if bodyStart > bodyEnd {
			bodyStart = bodyEnd
		}
		body := src[bodyStart:bodyEnd]

		rec := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "crystal",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          name + "()",
			EnrichmentRequired: false,
		}
		rec.Relationships = append(rec.Relationships,
			extractCallRelationships(body, name)...)
		opIdx := len(entities)
		entities = append(entities, rec)

		// CONTAINS — attach to innermost enclosing scope.
		if cls := enclosingScope(scopes, matchStart, bodyEnd); cls != nil {
			toID := extractor.BuildOperationStructuralRef("crystal", filePath, name)
			entities[cls.idx].Relationships = append(entities[cls.idx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
		_ = opIdx
	}

	for _, m := range defRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		emitOperation(name, "method", m[0], m[1])
	}

	for _, m := range macroRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		emitOperation(name, "macro", m[0], m[1])
	}

	return entities
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildScopeSignature returns the first line of a declaration as its signature.
func buildScopeSignature(src string, startByte int) string {
	line := src[startByte:]
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	return strings.TrimSpace(line)
}

// tokenRE matches Crystal keywords that open or close an `end`-terminated block.
// We scan for these in order and track depth.
var (
	endOpenerRE = regexp.MustCompile(`\b(class|module|struct|def|macro|lib|begin|do|if|unless|case|while|until|for)\b`)
	endCloserRE = regexp.MustCompile(`\bend\b`)
	// Combined scanner — matches either an opener or `end`.
	endScanRE = regexp.MustCompile(`\b(class|module|struct|def|macro|lib|begin|do|if|unless|case|while|until|for|end)\b`)
)

// findEndKeyword scans forward from fromByte tracking nested block openers and
// returns the byte offset just after the matching `end` keyword.
// Falls back to len(src)-1 if the source is malformed.
func findEndKeyword(src string, fromByte int) int {
	sub := src[fromByte:]
	depth := 1
	pos := 0
	for pos < len(sub) {
		loc := endScanRE.FindStringIndex(sub[pos:])
		if loc == nil {
			break
		}
		tok := sub[pos+loc[0] : pos+loc[1]]
		if tok == "end" {
			depth--
			if depth == 0 {
				return fromByte + pos + loc[1]
			}
		} else {
			depth++
		}
		pos += loc[1]
	}
	return len(src) - 1
}

// Suppress unused-variable warnings for the named regexp vars that are
// only used as documentation anchors.
var (
	_ = endOpenerRE
	_ = endCloserRE
)

// isScopeDeclaration reports whether matchStart coincides with the start of
// a recorded scope declaration (preventing defs from double-consuming a
// scope's declaration line).
func isScopeDeclaration(scopes []scopeSpan, matchStart int) bool {
	for _, s := range scopes {
		if matchStart == s.startByte {
			return true
		}
	}
	return false
}

// enclosingScope returns the innermost scopeSpan whose byte range contains
// [methodStart, methodEnd]. Returns nil for top-level defs.
func enclosingScope(scopes []scopeSpan, methodStart, methodEnd int) *scopeSpan {
	var best *scopeSpan
	for i := range scopes {
		s := &scopes[i]
		if methodStart > s.startByte && methodEnd <= s.endByte {
			if best == nil || s.startByte > best.startByte {
				best = s
			}
		}
	}
	return best
}

// extractCallRelationships scans a method/macro body for invocation heads and
// returns one CALLS edge per unique callee.
func extractCallRelationships(body, callerName string) []types.RelationshipRecord {
	if body == "" || callerName == "" {
		return nil
	}
	type key struct{ target, recv string }
	seen := make(map[key]bool)
	var rels []types.RelationshipRecord

	for _, m := range callRE.FindAllStringSubmatchIndex(body, -1) {
		recvChain := ""
		if m[2] >= 0 {
			recvChain = body[m[2]:m[3]]
		}
		callee := body[m[4]:m[5]]
		if skipKeywords[callee] || callee == callerName {
			continue
		}
		// Reject if preceded by an identifier char (likely a return type).
		if m[0] > 0 {
			prev := body[m[0]-1]
			if (prev >= 'A' && prev <= 'Z') || (prev >= 'a' && prev <= 'z') ||
				(prev >= '0' && prev <= '9') || prev == '_' {
				continue
			}
		}
		recvRoot := ""
		if recvChain != "" {
			chain := strings.TrimSuffix(recvChain, ".")
			if dot := strings.Index(chain, "."); dot >= 0 {
				recvRoot = chain[:dot]
			} else {
				recvRoot = chain
			}
			if callKeywordReceivers[recvRoot] {
				recvRoot = ""
			}
		}
		k := key{target: callee, recv: recvRoot}
		if seen[k] {
			continue
		}
		seen[k] = true
		rel := types.RelationshipRecord{
			ToID: callee,
			Kind: "CALLS",
		}
		if recvRoot != "" {
			rel.Properties = map[string]string{"receiver_root": recvRoot}
		}
		rels = append(rels, rel)
	}
	return rels
}
