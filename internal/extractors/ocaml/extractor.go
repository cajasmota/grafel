// Package ocaml implements a regex-based extractor for OCaml source files.
//
// Extracted entities:
//   - Module declarations (`module Foo = struct ... end`, file-level) → SCOPE.Component (subtype="module")
//   - `let`/`let rec` function definitions → SCOPE.Operation (subtype="function")
//   - `type` declarations → SCOPE.Component (subtype="type")
//   - `open Foo` statements → IMPORTS edges
//   - Function calls → CALLS edges
//   - CONTAINS edges (module → top-level declarations)
//
// No tree-sitter grammar for OCaml is bundled in smacker/go-tree-sitter, so
// this extractor uses regular expressions. OCaml uses a layout-insensitive
// syntax with explicit end/;; markers; for entity-discovery purposes we
// detect top-level declarations by their starting at column 0.
//
// Registers itself via init() and is imported by registry_gen.go.
package ocaml

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("ocaml", &Extractor{})
}

// Extractor implements extractor.Extractor for OCaml.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "ocaml" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// moduleRE matches explicit module declarations:
	//   module Foo = struct ... end
	//   module type Bar = sig ... end
	// We capture the module name only.
	moduleRE = regexp.MustCompile(
		`(?m)^module(?:\s+type)?\s+([A-Z][a-zA-Z0-9_']*)\s*(?:=|:)`,
	)

	// letRE matches top-level let/let rec bindings:
	//   let foo x y = ...
	//   let rec bar n = ...
	// Must be at column 0 (^ anchor) and NOT be "let () = ..." (main entry patterns).
	// We capture the function name.
	letRE = regexp.MustCompile(
		`(?m)^let(?:\s+rec)?\s+([a-z_][a-zA-Z0-9_']*)\s`,
	)

	// typeRE matches top-level type declarations:
	//   type 'a option = None | Some of 'a
	//   type point = { x: float; y: float }
	//   type t = int
	// Captures the type name (excluding leading type variables).
	typeRE = regexp.MustCompile(
		`(?m)^type\s+(?:(?:'[a-zA-Z_][a-zA-Z0-9_']*\s+)+)?([a-z_][a-zA-Z0-9_']*)\s*(?:=|$)`,
	)

	// openRE matches open statements:
	//   open Foo
	//   open Foo.Bar
	openRE = regexp.MustCompile(
		`(?m)^open\s+([A-Z][a-zA-Z0-9_'.]*(?:\.[A-Z][a-zA-Z0-9_']*)*)`,
	)

	// callRE matches function applications: qualified or unqualified identifiers
	// followed by arguments. Captures module-qualified calls like List.map, Lwt.bind, etc.
	// Also captures plain bare function calls.
	callDotRE = regexp.MustCompile(
		`\b([A-Z][a-zA-Z0-9_']*(?:\.[A-Z][a-zA-Z0-9_']*)*\.[a-z_][a-zA-Z0-9_']*)\b`,
	)
	callBareRE = regexp.MustCompile(
		`\b([a-z_][a-zA-Z0-9_']*)\s+`,
	)
)

// ocamlKeywords is the set of tokens to exclude from CALLS edges.
var ocamlKeywords = map[string]bool{
	// Core keywords
	"and": true, "as": true, "assert": true, "asr": true,
	"begin": true, "class": true, "constraint": true,
	"do": true, "done": true, "downto": true,
	"else": true, "end": true, "exception": true,
	"external": true, "false": true, "for": true,
	"fun": true, "function": true, "functor": true,
	"if": true, "in": true, "include": true,
	"inherit": true, "initializer": true,
	"land": true, "lazy": true, "let": true, "lor": true, "lsl": true, "lsr": true, "lxor": true,
	"match": true, "method": true, "mod": true, "module": true, "mutable": true,
	"new": true, "nonrec": true,
	"object": true, "of": true, "open": true, "or": true,
	"private": true, "rec": true, "sig": true, "struct": true,
	"then": true, "to": true, "true": true, "try": true,
	"type": true, "val": true, "virtual": true,
	"when": true, "while": true, "with": true,
	// Common pervasives
	"ignore": true, "raise": true, "failwith": true, "invalid_arg": true,
	"print_string": true, "print_int": true, "print_newline": true,
	"not": true, "ref": true, "fst": true, "snd": true,
}

// Extract processes the OCaml source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractOCaml(string(file.Content), file.Path)
	// Framework-aware depth pass (#5374): Caqti DB queries + alcotest suites.
	out = append(out, extractCaqtiQueries(string(file.Content), file.Path)...)
	out = append(out, extractAlcotestSuite(string(file.Content), file.Path)...)
	extractor.TagRelationshipsLanguage(out, "ocaml")
	extractor.TagEntitiesLanguage(out, "ocaml")
	return out, nil
}

func extractOCaml(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity (issue #577 pattern).
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "ocaml",
	}))

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Module declarations.
	seenModules := make(map[string]bool)
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenModules[name] {
			continue
		}
		seenModules[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractModuleBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "ocaml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "module " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let/let rec function definitions.
	letSeen := make(map[string]bool)
	for _, m := range letRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		// Skip unit-binding "let () =" which is main/entrypoint boilerplate.
		// (The pattern won't match "()" because it requires [a-z_] start.)
		if letSeen[name] {
			continue
		}
		// Skip if this looks like it's inside a module body (not at col 0).
		// We check the match start — if the offset of the line start equals
		// m[0], we're at column 0.
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		letSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractLetBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)
		var sig string
		// Build signature from the declaration line.
		lineEnd := strings.Index(src[m[0]:], "\n")
		if lineEnd >= 0 {
			sig = strings.TrimSpace(src[m[0] : m[0]+lineEnd])
		} else {
			sig = "let " + name
		}
		// Trim trailing " =" from signature
		if idx := strings.LastIndex(sig, "="); idx > 0 {
			sig = strings.TrimSpace(sig[:idx])
		}

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "function",
			SourceFile: filePath,
			Language:   "ocaml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  sig,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 3. Type declarations.
	typeSeen := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if typeSeen[name] {
			continue
		}
		// Must be at column 0.
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		typeSeen[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractLetBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "type",
			SourceFile: filePath,
			Language:   "ocaml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 4. Add CONTAINS edges from explicit module declarations to functions inside.
	// We use a simple heuristic: for each module declaration, find the let bindings
	// that follow within the module body.
	addModuleContains(src, filePath, &entities, seenModules, letSeen)

	return entities
}

// addModuleContains attaches CONTAINS edges to module entities.
func addModuleContains(src, filePath string, entities *[]types.EntityRecord, modules, letBindings map[string]bool) {
	if len(modules) == 0 || len(letBindings) == 0 {
		return
	}
	// For each module entity, find nested let bindings by scanning the module body.
	for i := range *entities {
		e := &(*entities)[i]
		if e.Kind != "SCOPE.Component" || e.Subtype != "module" {
			continue
		}
		moduleName := e.Name
		// Find the module's start offset.
		moduleMatchRE := regexp.MustCompile(fmt.Sprintf(`(?m)^module(?:\s+type)?\s+%s\s*(?:=|:)`, regexp.QuoteMeta(moduleName)))
		loc := moduleMatchRE.FindStringIndex(src)
		if loc == nil {
			continue
		}
		body := extractModuleBody(src, loc[1])
		// Find all let names within this body.
		var containsRels []types.RelationshipRecord
		seenRefs := make(map[string]bool)
		for _, lm := range letRE.FindAllStringSubmatch(body, -1) {
			if len(lm) < 2 {
				continue
			}
			fnName := lm[1]
			if seenRefs[fnName] {
				continue
			}
			seenRefs[fnName] = true
			ref := extractor.BuildOperationStructuralRef("ocaml", filePath, fnName)
			containsRels = append(containsRels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}
		e.Relationships = append(e.Relationships, containsRels...)
	}
}

// -----------------------------------------------------------------------
// Helper: import collection
// -----------------------------------------------------------------------

func collectOpenStatements(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	for _, m := range openRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimSpace(m[1])
		// Strip inline comments (OCaml uses (* ... *) but also (* for line comments)
		if ci := strings.Index(mod, "(*"); ci >= 0 {
			mod = strings.TrimSpace(mod[:ci])
		}
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		imports = append(imports, mod)
	}
	return imports
}

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
		displayName := importDisplayName(mod)
		out = append(out, types.EntityRecord{
			Name:       displayName,
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "ocaml",
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

func importDisplayName(mod string) string {
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// -----------------------------------------------------------------------
// Helper: body extraction
// -----------------------------------------------------------------------

// extractLetBody collects the body of a let binding by scanning subsequent lines
// that are indented relative to column 0. Stops at the next top-level binding.
func extractLetBody(src string, afterPos int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	var body []string
	for i, line := range lines {
		if i == 0 {
			body = append(body, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			body = append(body, line)
			continue
		}
		// Top-level items start at column 0 with a non-space char.
		if line[0] == ' ' || line[0] == '\t' {
			body = append(body, line)
		} else {
			// Next top-level declaration.
			break
		}
	}
	return strings.Join(body, "\n")
}

// extractModuleBody extracts the text of a module body between struct/sig and end.
// Falls back to indentation heuristics if struct/sig not found.
func extractModuleBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
	rest := src[afterPos:]

	// Try to find the matching "end" keyword for struct/sig/object blocks.
	// We do a simple depth-counting approach.
	depth := 0
	found := false
	endPos := 0

	// Keywords that open a new nesting level:
	openKW := regexp.MustCompile(`\b(struct|sig|object|begin)\b`)
	closeKW := regexp.MustCompile(`\bend\b`)

	// Scan character by character to handle nesting, skipping strings/comments.
	i := 0
	for i < len(rest) {
		// Check for block comment (* ... *)
		if i+1 < len(rest) && rest[i] == '(' && rest[i+1] == '*' {
			i += 2
			for i < len(rest) {
				if i+1 < len(rest) && rest[i] == '*' && rest[i+1] == ')' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		// Check for string literal "..."
		if rest[i] == '"' {
			i++
			for i < len(rest) && rest[i] != '"' {
				if rest[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		}
		// Check for char literal '.'
		if rest[i] == '\'' && i+2 < len(rest) && rest[i+2] == '\'' {
			i += 3
			continue
		}

		// Check for open/close keywords at current position.
		remaining := rest[i:]
		if om := openKW.FindStringIndex(remaining); om != nil && om[0] == 0 {
			depth++
			i += om[1]
			continue
		}
		if cm := closeKW.FindStringIndex(remaining); cm != nil && cm[0] == 0 {
			if depth == 0 {
				endPos = i
				found = true
				break
			}
			depth--
			i += cm[1]
			continue
		}
		i++
	}

	if found {
		return rest[:endPos]
	}
	// Fallback: use indentation heuristics (same as extractLetBody).
	return extractLetBody(src, afterPos)
}

// -----------------------------------------------------------------------
// Helper: CALLS edge collection
// -----------------------------------------------------------------------

// collectCalls extracts CALLS relationships from a function body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	addCall := func(target string, matchPos int) {
		if target == "" || target == callerName {
			return
		}
		if ocamlKeywords[target] {
			return
		}
		if len(target) <= 1 {
			return
		}
		if seen[target] {
			return
		}
		seen[target] = true
		// Compute line number by counting newlines up to match position
		lineNum := 1 + strings.Count(scrubbed[:matchPos], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: map[string]string{
				"line": strconv.Itoa(lineNum),
			},
		})
	}

	// Qualified calls: Module.function or Module.Sub.function
	for _, m := range callDotRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 && m[3] >= 0 {
			target := scrubbed[m[2]:m[3]]
			addCall(target, m[0])
		}
	}

	// Bare function calls: function_name <something>
	for _, m := range callBareRE.FindAllStringSubmatchIndex(scrubbed, -1) {
		if len(m) >= 4 && m[2] >= 0 && m[3] >= 0 {
			target := scrubbed[m[2]:m[3]]
			addCall(target, m[0])
		}
	}

	return out
}

// stripStringsAndComments replaces string literals and OCaml block comments
// with spaces so the call scanner doesn't pick up tokens inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0

	for i < len(src) {
		ch := src[i]

		// Block comment: (* ... *) — may be nested in OCaml
		if ch == '(' && i+1 < len(src) && src[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			depth := 1
			for i < len(src) && depth > 0 {
				if i+1 < len(src) && src[i] == '(' && src[i+1] == '*' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					depth++
				} else if i+1 < len(src) && src[i] == '*' && src[i+1] == ')' {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					depth--
				} else {
					out[i] = ' '
					i++
				}
			}
			continue
		}

		// Double-quoted string: "..."
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

		// Char literal: 'x' or '\n' etc.
		if ch == '\'' && i+2 < len(src) && src[i+2] == '\'' {
			out[i] = ' '
			out[i+1] = ' '
			out[i+2] = ' '
			i += 3
			continue
		}

		out[i] = ch
		i++
	}
	return string(out)
}
