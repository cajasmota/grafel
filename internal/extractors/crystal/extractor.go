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
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	extractor.TagEntitiesLanguage(out, "crystal")
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
	knownTypes := knownTypeNames(scopes)
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
		// #4937 — Type.method receiver resolution: stamp the owning type so a
		// downstream consumer can resolve a bare `def foo` to `Owner.foo`.
		if recvType := receiverTypeForMethod(scopes, matchStart, bodyEnd); recvType != "" {
			rec.Properties = map[string]string{"receiver_type": recvType}
		}
		// #4937 — macro-generated method visibility: count def/define_method
		// heads (incl. interpolated `def {{name}}`) the plain def scanner skips
		// inside the macro body, and flag `{% for %}`-driven code-gen.
		if subtype == "macro" {
			if cnt, iterates := macroGenStats(body); cnt > 0 {
				if rec.Properties == nil {
					rec.Properties = map[string]string{}
				}
				rec.Properties["macro_generated"] = "true"
				rec.Properties["generated_method_count"] = strconv.Itoa(cnt)
				if iterates {
					rec.Properties["generated_via"] = "macro_for_iteration"
				}
			}
		}
		rec.Relationships = append(rec.Relationships,
			extractCallRelationships(body, name, knownTypes)...)
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

	// ── 4. #4937 depth: enum / alias / Spectator spec suite ──────────────
	entities = append(entities, extractEnums(src, filePath)...)
	entities = append(entities, extractAliases(src, filePath)...)
	entities = append(entities, extractSpecSuite(src, filePath)...)

	// ── 5. #6852 — file carrier for the path-anchored IMPORTS above ──────
	//
	// Section 1 stamps `FromID: filePath` on every require's IMPORTS edge, and
	// internal/resolve/refs.go has no path→entity index: a path-valued FromID
	// resolves iff some emitted node carries that exact string as its Name.
	// Nothing here does as a rule (the seven Name sites are enumerated in
	// file_carrier_6852_test.go's header), so without this the raw path reached
	// the graph as the edge's FROM end — #6847's measurement, #6815's fix.
	//
	// CONDITIONAL, via extractor.PrependFileCarrier. An unconditional carrier
	// mints one bare orphan node per .cr file across a whole repo, which no
	// recall-shaped assertion can see (#6518, #6815).
	//
	// PLACEMENT: last, for TWO independent reasons, each SCORED by its own
	// mutant — because a placement comment that lists reasons without a mutant
	// per reason is prose (the clojure/cobol lesson, #6897). Note what the
	// scoring showed about the two tests, since "one mutant each" would
	// otherwise read as "two independently-necessary cells": the depth-pass
	// cell (reason 2) kills BOTH moves and strictly dominates for placement,
	// so the CONTAINS test is never the SOLE killer of a placement move. It is
	// not redundant — it pins CONTAINS ownership in the carrier's presence,
	// which is a separate regression — but only reason 2's cell is load-bearing
	// for the placement itself.
	//
	//  1. THE INDEX HAZARD. scopeSpan.idx is an INDEX INTO `entities`,
	//     dereferenced at :325 to hang each def/macro's CONTAINS edge on its
	//     enclosing scope; a head-insertion before that step shifts every
	//     stored index by one and the edges land on the wrong records. Unlike
	//     clojure (#6897) this is NOT entailed by the carrier's own condition:
	//     section 1 runs FIRST, so the anchor already exists before `scopes` is
	//     built and a conditional carrier placed earlier really is emitted.
	//     Graded by TestCrystal_CarrierPlacementDoesNotShiftTheContainsPass_6852
	//     — which kills the move with wrong-owner CONTAINS rows, alongside
	//     reason 2's cell.
	//  2. CLAUSE-3 VISIBILITY OF THE DEPTH PASSES. FileCarrierFor can only
	//     decline for a record it is handed, so the call must run after the
	//     extractEnums / extractAliases / extractSpecSuite appends above or a
	//     record one of them names could take a second node under one
	//     graph.EntityID (#6369/#6480). Graded by
	//     TestCrystal_DepthPassRecordNamedLikeThePathGetsNoSecondCarrier_6852,
	//     which is the ONLY thing that kills that move — reason 1's test does
	//     not, since every scopeSpan.idx has been dereferenced by then.
	//
	// The tagging call in Extract is NOT a third reason: it is a grading route,
	// not a correctness constraint (see below).
	//
	// The "crystal" token is stamped explicitly rather than left to Extract's
	// TagEntitiesLanguage, which runs after this and would fill an empty one —
	// so the token is graded through the Properties["language"] provenance
	// route, not through the Language field alone. See
	// TestCrystal_CarrierShape_6852.
	return extractor.PrependFileCarrier(filePath, "crystal", entities)
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
func extractCallRelationships(body, callerName string, knownTypes map[string]bool) []types.RelationshipRecord {
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
		// Compute line number by counting newlines up to match position
		lineNum := 1 + strings.Count(body[:m[0]], "\n")
		// #4937 — Type.method receiver resolution: when the receiver root names
		// a known in-file type (PascalCase class/struct/module), the call is a
		// class-method invocation; resolve the target to the dotted
		// `Type.method` form so it binds cross-file to the type's method.
		toID := callee
		if recvRoot != "" && knownTypes[recvRoot] {
			toID = recvRoot + "." + callee
		}
		rel := types.RelationshipRecord{
			ToID: toID,
			Kind: "CALLS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(lineNum)},
			},
		}
		if recvRoot != "" {
			rel.Properties.Set("receiver_root", recvRoot)
			if knownTypes[recvRoot] {
				rel.Properties.Set("receiver_type", recvRoot)
			}
		}
		rels = append(rels, rel)
	}
	return rels
}
