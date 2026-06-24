// Package reasonml implements a regex-based extractor for ReasonML source files.
//
// ReasonML is OCaml with curly-brace syntax — the same type system and runtime,
// but JavaScript/C-family surface syntax.  File extensions: .re (implementation),
// .rei (interface/signature).
//
// Extracted entities:
//   - module declarations: "module Foo = {" or "module Foo: Bar = {"
//     → Kind="SCOPE.Component", Subtype="module"
//   - let function bindings: "let foo = (x) => x + 1;"
//     → Kind="SCOPE.Operation", Subtype="let"
//   - type declarations (record, variant, alias):
//     "type person = { name: string, age: int };"
//     "type shape = Circle(float) | Square(float);"
//     → Kind="SCOPE.Component"
//   - open statements: "open Belt;" or "open React;"
//     → IMPORTS edges
//   - function calls (direct and pipe |>)
//     → CALLS edges
//
// Registers itself via init() and is imported by registry_gen.go.
package reasonml

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("reasonml", &Extractor{})
}

// Extractor implements extractor.Extractor for ReasonML.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "reasonml" }

// Regex patterns for ReasonML syntax.
var (
	// module declaration: "module Foo = {" or "module Foo: SomeType = {"
	// Also matches interface modules in .rei: "module Foo: { ... }"
	moduleRE = regexp.MustCompile(
		`(?m)^([ \t]*)module\s+([A-Z][a-zA-Z0-9_]*)\s*(?::[^=\n]*)?\s*=\s*\{`,
	)

	// let binding (function or value): "let foo = (x) => ..." or "let foo = ..."
	// Captures indentation and name. ReasonML functions use => arrow.
	letRE = regexp.MustCompile(
		`(?m)^([ \t]*)let\s+(?:rec\s+)?([a-zA-Z_][a-zA-Z0-9_]*)\s*(?::[^=\n]*)?\s*=`,
	)

	// type declaration: "type person = {" or "type shape =" or "type t = string;"
	// Type names in ReasonML are typically lowercase (unlike OCaml/F# convention).
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[a-zA-Z_][a-zA-Z0-9_',\s]*)?\s*=`,
	)

	// Abstract type declaration (no "="): "type t;" common in .rei interface files
	abstractTypeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*;`,
	)

	// open statement: "open Belt;" or "open React"
	openRE = regexp.MustCompile(
		`(?m)^[ \t]*open\s+([\w.]+)\s*;?`,
	)

	// function call: identifier( or Module.function(
	callRE = regexp.MustCompile(
		`(?:^|[^\w.'"])([A-Za-z_][A-Za-z0-9_.]*)(?:\s*\()`,
	)

	// pipe operator call: |> identifier or |> Module.name
	pipeCallRE = regexp.MustCompile(
		`\|>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)
)

// reasonmlKeywords are tokens the call regex picks up but are not real calls.
var reasonmlKeywords = map[string]bool{
	"if": true, "else": true, "switch": true, "while": true, "for": true,
	"try": true, "catch": true,
	"let": true, "in": true, "and": true,
	"type": true, "open": true, "module": true,
	"include": true, "functor": true,
	"true": true, "false": true,
	"fun": true, "function": true,
	"exception": true, "raise": true,
	// JSX / React
	"make": false, // "make" is a common ReasonReact component entry point — keep
}

// Extract processes ReasonML source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractReasonML(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "reasonml")
	extractor.TagEntitiesLanguage(out, "reasonml")
	return out, nil
}

func extractReasonML(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Module declarations → SCOPE.Component
	seen := make(map[string]bool)
	for _, m := range moduleRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		key := "module:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		// Find closing brace to estimate end line
		body := extractBraceBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "reasonml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "module " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let bindings → SCOPE.Operation
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
		// Extract body until semicolon at base indent or next let at same indent
		body := extractLetBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "let",
			SourceFile: filePath,
			Language:   "reasonml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  buildLetSig(src[m[0]:m[1]], name),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
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
		// Pass the raw suffix (after "=") for both body extraction and subtype
		rawSuffix := src[m[1]:]
		body := extractTypeBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		subtype := classifyTypeSubtype(rawSuffix)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    subtype,
			SourceFile: filePath,
			Language:   "reasonml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 4. Abstract type declarations (no "=", e.g. "type t;" in .rei files)
	for _, m := range abstractTypeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "abstract",
			SourceFile: filePath,
			Language:   "reasonml",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// Reason-React decoration pass (#5379): re-kind [@react.component]-annotated
	// `let` operations to SCOPE.UIComponent and record their prop set. No-op when
	// the file declares no [@react.component] component.
	applyReasonReact(src, entities)

	return entities
}

// classifyTypeSubtype determines the ReasonML type subtype from the raw text
// after the "=" in the type declaration.
func classifyTypeSubtype(suffix string) string {
	trimmed := strings.TrimSpace(suffix)
	// Record type: starts with "{"
	if strings.HasPrefix(trimmed, "{") {
		return "record"
	}
	// Variant type: starts with "|" or contains " | "
	if strings.HasPrefix(trimmed, "|") || strings.Contains(trimmed, " | ") || strings.Contains(trimmed, "\n  |") {
		return "variant"
	}
	return "type"
}

// buildLetSig builds a signature string from the raw declaration.
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

// collectOpenStatements parses "open" statements and returns unique module paths.
func collectOpenStatements(src string) []string {
	seen := make(map[string]bool)
	var imports []string
	for _, m := range openRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		mod := strings.TrimRight(strings.TrimSpace(m[1]), ";")
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
			Language:   "reasonml",
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

// importDisplayName returns a short display name for an import path.
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// extractBraceBody returns text between a "{" and its matching "}".
// afterPos should be the position right after the opening "{".
func extractBraceBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
	// Find opening brace
	openPos := strings.IndexByte(src[afterPos-1:], '{')
	if openPos < 0 {
		return ""
	}
	start := afterPos - 1 + openPos + 1

	depth := 1
	i := start
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	if i > start {
		return src[start:i]
	}
	return ""
}

// extractLetBody returns the body of a let binding.
// Collects until a semicolon at base indent level or next top-level binding.
func extractLetBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	for i, line := range lines {
		if i == 0 {
			bodyLines = append(bodyLines, line)
			// Single-line binding ending with ";"
			if strings.HasSuffix(strings.TrimSpace(line), ";") {
				break
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			bodyLines = append(bodyLines, line)
			continue
		}
		indent := countIndent(line)
		if indent <= baseIndentLen && trimmed != "" {
			// Same or lesser indent — end of this binding
			break
		}
		bodyLines = append(bodyLines, line)
		// End on a line that closes with ";" at reasonable indent
		if strings.HasSuffix(trimmed, ";") && indent <= baseIndentLen+2 {
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// extractTypeBody returns the body after a type declaration's "=".
func extractTypeBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
	rest := src[afterPos:]
	// If starts with "{", extract brace body
	trimmed := strings.TrimSpace(rest)
	if strings.HasPrefix(trimmed, "{") {
		idx := strings.IndexByte(rest, '{')
		return extractBraceBody(src, afterPos+idx+1)
	}
	// Otherwise collect until ";"
	lines := strings.Split(rest, "\n")
	var bodyLines []string
	for _, line := range lines {
		bodyLines = append(bodyLines, line)
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs.
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

// collectCalls extracts CALLS edges from a function body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)

	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	addCall := func(target string) {
		if target == "" || callerName == target {
			return
		}
		if reasonmlKeywords[target] {
			return
		}
		// Skip single-letter identifiers (usually type params or pattern vars)
		if len(target) == 1 {
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

	// Direct calls: name(
	for _, m := range callRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	// Pipe operator: |> name or |> Module.name
	for _, m := range pipeCallRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	return out
}

// stripStringsAndComments replaces string literals and line comments with spaces.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote
	for i < len(src) {
		ch := src[i]
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
		case '"':
			inStr = '"'
			out[i] = ' '
			i++
		case '\'':
			// ReasonML character literals: 'a'
			if i+2 < len(src) && src[i+2] == '\'' {
				out[i] = ' '
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				continue
			}
			out[i] = ch
			i++
		case '/':
			// ReasonML line comment: //
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			// Block comment: /* ... */
			if i+1 < len(src) && src[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < len(src) {
					if i+1 < len(src) && src[i] == '*' && src[i+1] == '/' {
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
			out[i] = ch
			i++
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
