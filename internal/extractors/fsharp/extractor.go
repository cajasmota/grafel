// Package fsharp implements a regex-based extractor for F# source files.
//
// Extracted entities:
//   - module/namespace declarations → Kind="SCOPE.Component", Subtype="module"|"namespace"
//   - let/let rec/let mutable bindings (functions) → Kind="SCOPE.Operation", Subtype="let"
//   - member function definitions → Kind="SCOPE.Operation", Subtype="member"
//   - type declarations (record, discriminated union, class, interface, struct, alias)
//     → Kind="SCOPE.Component"
//   - open statements → IMPORTS edges
//   - function applications → CALLS edges
//   - Module CONTAINS members
//
// No tree-sitter grammar for F# is available in smacker/go-tree-sitter, so
// this extractor parses F# with regular expressions. F# is
// whitespace/indent-sensitive; for entity discovery purposes we rely on
// indentation heuristics similar to the Nim extractor.
//
// Registers itself via init() and is imported by registry_gen.go.
package fsharp

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("fsharp", &Extractor{})
}

// Extractor implements extractor.Extractor for F#.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "fsharp" }

// Regex patterns for F# syntax.
var (
	// module declaration: "module Foo" or "module Foo.Bar" or "module rec Foo"
	moduleRE = regexp.MustCompile(
		`(?m)^([ \t]*)module(?:\s+rec)?\s+([\w.]+)\s*$`,
	)

	// namespace declaration: "namespace Foo" or "namespace Foo.Bar"
	namespaceRE = regexp.MustCompile(
		`(?m)^([ \t]*)namespace\s+([\w.]+)\s*$`,
	)

	// let binding: "let [rec] [mutable] name [<params>] =" or "let name ="
	// Captures indentation and name. Handles generic type params like <'T>.
	letRE = regexp.MustCompile(
		`(?m)^([ \t]*)let(?:\s+rec)?(?:\s+mutable)?\s+([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// member: "member [this.]Name" or "member _.Name" or "override this.Name"
	memberRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:member|override|abstract member|default)\s+(?:[a-zA-Z_][a-zA-Z0-9_']*\.)?([a-zA-Z_][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:[^=\n]*)=`,
	)

	// type declaration: "type Foo =" or "type Foo<'T> ="
	// Matches record, DU, class, interface, struct, alias, exception types.
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+([A-Z][a-zA-Z0-9_']*)\s*(?:<[^>]*>)?\s*(?:\([^)]*\))?\s*=`,
	)

	// type kind after "=" — helps classify subtype
	typeKindRE = regexp.MustCompile(
		`(?m)^([ \t]*)type\s+[A-Z][a-zA-Z0-9_']*\s*(?:<[^>]*>)?\s*(?:\([^)]*\))?\s*=\s*(\{|interface|class|\|)`,
	)

	// open statement: "open Foo" or "open Foo.Bar"
	openRE = regexp.MustCompile(
		`(?m)^[ \t]*open\s+([\w.]+)`,
	)

	// function application call: identifier( or Module.function(
	// Also detects pipe targets: |> identifier or |> Module.name
	callRE = regexp.MustCompile(
		`(?:^|[^\w.'"])([A-Za-z_][A-Za-z0-9_.]*)(?:\s*<[^>]*>)?\s*\(`,
	)

	// pipe operator call: |> Module.name or |> name
	pipeCallRE = regexp.MustCompile(
		`\|>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)

	// compose operator: >> Module.name
	composeCallRE = regexp.MustCompile(
		`>>\s*([A-Za-z_][A-Za-z0-9_.]*)`,
	)
)

// fsharpKeywords are tokens the call regex picks up but are not real calls.
var fsharpKeywords = map[string]bool{
	"if": true, "elif": true, "else": true, "then": true,
	"while": true, "for": true, "do": true, "done": true,
	"match": true, "with": true, "when": true,
	"try": true, "finally": true,
	"raise": true, "failwith": true, "failwithf": true,
	"return": true, "yield": true, "and": true, "or": true, "not": true,
	"let": true, "in": true, "fun": true, "function": true,
	"type": true, "open": true, "module": true, "namespace": true,
	"begin": true, "end": true, "inherit": true, "interface": true,
	"member": true, "override": true, "default": true, "abstract": true,
	"static": true, "mutable": true, "rec": true, "new": true,
	"null": true, "true": true, "false": true,
	"async": true, "seq": true, "query": true,
	"upcast": true, "downcast": true, "typeof": true, "typedefof": true,
	"sizeof": true, "nameof": true, "use": true, "using": true,
	// common computation expression keywords
	"async.Return": true, "async.Bind": true, "async.Zero": true,
}

// Extract processes F# source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractFSharp(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "fsharp")
	extractor.TagEntitiesLanguage(out, "fsharp")
	return out, nil
}

func extractFSharp(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Module/namespace declarations → SCOPE.Component
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
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "module",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "module " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	for _, m := range namespaceRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		name := src[m[4]:m[5]]
		key := "namespace:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "namespace",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    startLine,
			Signature:  "namespace " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. let bindings (functions) → SCOPE.Operation
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

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "let",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  buildLetSig(src[m[0]:m[1]], name),
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 3. member definitions → SCOPE.Operation
	memberSeen := make(map[string]bool)
	for _, m := range memberRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 6 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		// Skip if same name already from let bindings (avoid double-counting)
		if letSeen[indent+":let:"+name] {
			continue
		}
		key := indent + ":member:" + name
		if memberSeen[key] {
			continue
		}
		memberSeen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Operation",
			Subtype:    "member",
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "member " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 4. type declarations → SCOPE.Component
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

		// Determine subtype
		subtype := classifyTypeSubtype(src[m[0]:m[1]], body)

		// Find members/functions that belong to this type (CONTAINS edges)
		var rels []types.RelationshipRecord
		memberRef := make(map[string]bool)
		// Check members declared at higher indentation after this type
		typeIndentLen := len(src[m[2]:m[3]])
		for _, pm := range memberRE.FindAllStringSubmatchIndex(src, -1) {
			if len(pm) < 6 {
				continue
			}
			pmIndentLen := len(src[pm[2]:pm[3]])
			if pmIndentLen <= typeIndentLen {
				continue
			}
			// Member must appear after the type declaration start
			if pm[0] < m[0] {
				continue
			}
			mName := src[pm[4]:pm[5]]
			if memberRef[mName] {
				continue
			}
			memberRef[mName] = true
			ref := extractor.BuildOperationStructuralRef("fsharp", filePath, mName)
			rels = append(rels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    subtype,
			SourceFile: filePath,
			Language:   "fsharp",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "type " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	return entities
}

// classifyTypeSubtype determines the F# type subtype from the declaration context.
func classifyTypeSubtype(decl, body string) string {
	// Check for "= {" → record
	if strings.Contains(decl, "= {") || strings.TrimSpace(body) != "" && strings.HasPrefix(strings.TrimSpace(body), "{") {
		return "record"
	}
	// Check for "= |" or body starting with "|" → discriminated union
	if strings.Contains(decl, "= |") {
		return "discriminated_union"
	}
	bodyTrimmed := strings.TrimSpace(body)
	if strings.HasPrefix(bodyTrimmed, "|") {
		return "discriminated_union"
	}
	// Check for interface/class keywords
	if strings.Contains(decl, "interface") || strings.HasPrefix(bodyTrimmed, "interface") {
		return "interface"
	}
	if strings.Contains(decl, "class") || strings.HasPrefix(bodyTrimmed, "class") {
		return "class"
	}
	if strings.Contains(decl, "struct") {
		return "struct"
	}
	return "type"
}

// buildLetSig builds a signature string for a let binding from the raw declaration.
func buildLetSig(decl, name string) string {
	// Trim whitespace and return a reasonable signature
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
		mod := strings.TrimSpace(m[1])
		// Strip inline comments
		if ci := strings.IndexAny(mod, "//"); ci >= 0 {
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
			Language:   "fsharp",
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
// e.g. "Microsoft.FSharp.Collections" → "Collections"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	if dot := strings.LastIndexByte(mod, '.'); dot >= 0 {
		return mod[dot+1:]
	}
	return mod
}

// extractIndentBody returns the body text following a declaration line.
// Collects lines that are more indented than baseIndent.
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	minBodyIndent := baseIndentLen + 2 // F# typically uses 4-space indent, but 2 is minimum

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			// Same-line body
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
		// Strip module qualifier if present (e.g. "List.map" → "List.map" kept as-is)
		if fsharpKeywords[target] {
			return
		}
		// Skip single-letter identifiers (usually type params)
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

	// Regular function calls: name(
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

	// Compose operator: >> name
	for _, m := range composeCallRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	return out
}

// stripStringsAndComments replaces string literals and //-line comments
// with spaces so the call scanner doesn't pick up tokens inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote
	inTriple := false
	for i < len(src) {
		ch := src[i]
		if inTriple {
			out[i] = ' '
			if i+2 < len(src) && ch == '"' && src[i+1] == '"' && src[i+2] == '"' {
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = false
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
		case '"':
			// Check for triple-quoted string
			if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				out[i] = ' '
				out[i+1] = ' '
				out[i+2] = ' '
				i += 3
				inTriple = true
				continue
			}
			// Check for verbatim string @"..."
			inStr = '"'
			out[i] = ' '
			i++
		case '/':
			// F# line comment: //
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		case '(':
			// F# block comment: (* ... *)
			if i+1 < len(src) && src[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				for i < len(src) {
					if i+1 < len(src) && src[i] == '*' && src[i+1] == ')' {
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
