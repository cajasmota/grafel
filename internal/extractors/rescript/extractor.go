// Package rescript implements a regex-based extractor for ReScript source files.
//
// ReScript is an OCaml-derived language with TypeScript-like syntax that compiles
// to JavaScript. It uses pipe-first (`->`) instead of pipe-last (`|>`), has
// JSX support for React components, and organises code into modules.
//
// Extracted entities:
//   - module declarations (`module Foo = { ... }` or `module Foo`)    → SCOPE.Component, Subtype="module"
//   - `let` function bindings (`let foo = (x) => x + 1`)              → SCOPE.Operation, Subtype="let"
//   - `type` declarations                                               → SCOPE.Component, Subtype="type"
//   - `open Foo` statements                                             → IMPORTS edges
//   - pipe-first `->` chains                                            → CALLS edges
//   - JSX-style React component usage (`<MyComp />`)                    → RENDERS edges
//
// File extensions: .res (implementation), .resi (interface/signature)
//
// No tree-sitter grammar for ReScript is available in smacker/go-tree-sitter,
// so this extractor parses ReScript with regular expressions similar to the
// F# and Haskell extractors.
//
// Registers itself via init() and is imported by registry_gen.go.
package rescript

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("rescript", &Extractor{})
}

// Extractor implements extractor.Extractor for ReScript.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "rescript" }

// ---------------------------------------------------------------------------
// Compiled regular expressions
// ---------------------------------------------------------------------------

var (
	// moduleRE matches:
	//   module Foo = { ...
	//   module Foo = module_type { ...
	//   module Foo
	// Group 1: module name (PascalCase)
	moduleRE = regexp.MustCompile(
		`(?m)^[ \t]*module\s+([A-Z][a-zA-Z0-9_]*)\s*(?:=\s*\{|=\s*[A-Z][a-zA-Z0-9_.]*\s*\{|$|\s*\{)`,
	)

	// letRE matches let bindings at any indentation level.
	// Captures: group 1 = indentation, group 2 = name.
	// Matches:
	//   let foo = ...
	//   let foo = (x, y) => ...
	//   let foo: int = ...
	//   let rec foo = ...
	// Excludes bare `let _ = ` (anonymous) — name must start with letter/underscore.
	letRE = regexp.MustCompile(
		`(?m)^([ \t]*)let(?:\s+rec)?\s+([a-z_][a-zA-Z0-9_']*)\s*(?::[^=\n]*)?\s*=`,
	)

	// typeRE matches type declarations:
	//   type t = ...
	//   type user = { name: string }
	//   type status = Active | Inactive
	//   type t                          (opaque type in .resi interface files)
	// Group 1: indentation, Group 2: type name
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?(?:\s*=|[ \t]*$)`,
	)

	// openRE matches open statements:
	//   open Belt
	//   open React
	//   open Js.Promise
	// Group 1: module path
	openRE = regexp.MustCompile(
		`(?m)^[ \t]*open\s+([A-Z][a-zA-Z0-9_.]*)\s*$`,
	)

	// callRE matches direct function calls: name( or Module.name(
	callRE = regexp.MustCompile(
		`(?:^|[^\w.'"])([A-Za-z_][A-Za-z0-9_.]*)(?:\s*<[^>]*)?\s*\(`,
	)

	// pipeFirstRE matches pipe-first chains: expr->name or expr->Module.name
	// The `->` operator in ReScript pipes the left side as the first argument.
	pipeFirstRE = regexp.MustCompile(
		`->\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// jsxTagRE matches JSX-style component tags:
	//   <MyComponent  or  <MyComponent.Sub
	// Only uppercase-starting names (component names) generate RENDERS edges.
	// Group 1: component name
	jsxTagRE = regexp.MustCompile(
		`<([A-Z][A-Za-z0-9_]*(?:\.[A-Z][A-Za-z0-9_]*)*)[\s/>]`,
	)
)

// rescriptKeywords is the set of tokens to exclude from CALLS edges.
var rescriptKeywords = map[string]bool{
	"if": true, "else": true, "switch": true, "when": true,
	"while": true, "for": true, "do": true,
	"let": true, "rec": true, "in": true, "and": true,
	"type": true, "module": true, "open": true, "include": true,
	"try": true, "catch": true, "raise": true, "exception": true,
	"external": true, "export": true, "import": true,
	"true": true, "false": true, "null": true,
	"async": true, "await": true,
	"Belt": true, "React": true, "Js": true,
}

// Extract processes ReScript source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractReScript(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "rescript")
	extractor.TagEntitiesLanguage(out, "rescript")
	return out, nil
}

func extractReScript(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Module declarations → SCOPE.Component
	seen := make(map[string]bool)
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		key := "module:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "rescript",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let function bindings → SCOPE.Operation
	letSeen := make(map[string]bool)
	for _, m := range letRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := indent + ":let:" + name
		if letSeen[key] {
			continue
		}
		letSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)
		renders := collectRenders(body)

		var rels []types.RelationshipRecord
		rels = append(rels, calls...)
		rels = append(rels, renders...)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "let",
			SourceFile: filePath,
			Language:   "rescript",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  buildLetSig(src[m[0]:m[1]], name),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	// 3. type declarations → SCOPE.Component
	typeSeen := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(src[m[2]:m[3]]))
		endLine := startLine + strings.Count(body, "\n")

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "type",
			SourceFile: filePath,
			Language:   "rescript",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	return entities
}

// ---------------------------------------------------------------------------
// Helper: open statement collection
// ---------------------------------------------------------------------------

func collectOpenStatements(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	for _, m := range openRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimSpace(m[1])
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		imports = append(imports, mod)
	}
	return imports
}

// buildImportEntities creates SCOPE.Component stubs carrying IMPORTS edges.
func buildImportEntities(filePath string, imports []string) []types.EntityRecord {
	if len(imports) == 0 {
		return nil
	}
	out := make([]types.EntityRecord, 0, len(imports))
	seen := make(map[string]bool, len(imports))
	for _, mod := range imports {
		if seen[mod] {
			continue
		}
		seen[mod] = true
		out = append(out, types.EntityRecord{
			Name:       importDisplayName(mod),
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "rescript",
			Relationships: []types.RelationshipRecord{
				{
					FromID: filePath,
					ToID:   mod,
					Kind:   "IMPORTS",
				},
			},
		})
	}
	return out
}

// importDisplayName returns a short display name from an import path.
// e.g. "Belt.List" → "List", "React" → "React"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// ---------------------------------------------------------------------------
// Helper: body extraction (indent-based)
// ---------------------------------------------------------------------------

// extractIndentBody returns the body text following a declaration line.
// It collects lines that are more indented than baseIndent or are on the
// same declaration line (rest-of-line).
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	// ReScript uses 2-space indent conventionally.
	minBodyIndent := baseIndentLen + 2

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		indent := countIndent(line)
		if indent >= minBodyIndent {
			bodyLines = append(bodyLines, line)
		} else if indent <= baseIndentLen && strings.TrimSpace(line) != "" {
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs in a line.
func countIndent(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

// buildLetSig builds a signature string for a let binding.
func buildLetSig(decl, name string) string {
	sig := strings.TrimSpace(decl)
	if idx := strings.Index(sig, "="); idx >= 0 {
		sig = strings.TrimSpace(sig[:idx])
	}
	if sig == "" {
		return "let " + name
	}
	return sig
}

// ---------------------------------------------------------------------------
// Helper: CALLS edge collection
// ---------------------------------------------------------------------------

// collectCalls extracts CALLS relationships from a function body.
// It scans for:
//   - direct function calls: name(
//   - pipe-first chains: ->name or ->Module.name
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	addCall := func(target string) {
		if target == "" || target == callerName {
			return
		}
		if rescriptKeywords[target] {
			return
		}
		// Skip single-char or very short identifiers (likely params/vars)
		if len(target) <= 1 {
			return
		}
		if seen[target] {
			return
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}

	// Direct calls: identifier(
	for _, m := range callRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	// Pipe-first chains: ->name
	for _, m := range pipeFirstRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Helper: RENDERS edge collection (JSX)
// ---------------------------------------------------------------------------

// collectRenders extracts RENDERS relationships from JSX tags in a function body.
// Only uppercase-starting component names (Pascal case) generate RENDERS edges.
func collectRenders(body string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	for _, m := range jsxTagRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, types.RelationshipRecord{
			ToID: name,
			Kind: "RENDERS",
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Helper: strip strings and comments
// ---------------------------------------------------------------------------

// stripStringsAndComments replaces string literals and // line comments
// with spaces so the call scanner doesn't pick up tokens inside them.
// ReScript uses:
//   - " double-quoted strings with backslash escapes
//   - ` template/tagged strings
//   - // line comments
//   - /* block comments */
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote, '`'=template
	inBlock := false

	for i < len(src) {
		ch := src[i]

		if inBlock {
			out[i] = ' '
			if ch == '*' && i+1 < len(src) && src[i+1] == '/' {
				out[i+1] = ' '
				i += 2
				inBlock = false
				continue
			}
			i++
			continue
		}

		if inStr != 0 {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			i++
			continue
		}

		switch ch {
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				// Line comment
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				// Block comment
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				inBlock = true
				continue
			}
			out[i] = ch
			i++
		case '"':
			inStr = '"'
			out[i] = ' '
			i++
		case '`':
			inStr = '`'
			out[i] = ' '
			i++
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
