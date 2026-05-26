// Package nim implements a regex-based extractor for Nim source files.
//
// Extracted entities:
//   - proc/func/method/converter/template/macro declarations → Kind="SCOPE.Operation", Subtype="proc"
//   - type declarations (object, ref object, enum, tuple, distinct) → Kind="SCOPE.Component"
//   - IMPORTS edges for `import` and `include` statements
//   - CALLS edges for proc invocations inside bodies
//   - CONTAINS edges from type→method (method/proc attached to a type)
//
// No tree-sitter grammar for Nim is bundled in smacker/go-tree-sitter, so
// this extractor parses Nim with regular expressions. Nim is
// whitespace/indent-sensitive but for entity discovery purposes we only
// need to detect top-level declarations and their bodies via indentation
// heuristics (similar to how the fish extractor handles function bodies).
//
// Registers itself via init() and is imported by registry_gen.go.
package nim

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("nim", &Extractor{})
}

// Extractor implements extractor.Extractor for Nim.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "nim" }

// Patterns for Nim syntax.
var (
	// proc/func/method/converter/template/macro declarations.
	// Nim proc signatures: proc name*(params): ReturnType =
	// or just: proc name(params) =
	// Also handles template, macro, converter, iterator, func keywords.
	procRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:proc|func|method|converter|template|macro|iterator)\s+` +
			`([a-zA-Z_\x{0080}-\x{FFFF}][a-zA-Z0-9_\x{0080}-\x{FFFF}]*\*?)\s*` +
			`(?:\[[^\]]*\])?\s*` + // optional generic params
			`(\([^)]*\))?\s*` + // optional params
			`(?::\s*[^\n]+?)?\s*` + // optional return type annotation
			`(?:\{[^}]*\})?\s*=`, // optional pragma block e.g. {.async.}
	)

	// type block declarations — two forms:
	//   1. type block:  "  Name = object"  (indented under 'type')
	//   2. inline type: "type Name = object" (same line)
	// Handles optional export marker (*) and generic params ([T]).
	typeRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:type\s+)?([A-Z][a-zA-Z0-9_]*\*?)\s*(?:\[[^\]]*\])?\s*=\s*(object|ref\s+object|enum|tuple|distinct\s+\w+)`,
	)

	// typeBlockStartRE marks the start of a "type" keyword block (unused but kept for documentation)
	typeBlockRE = regexp.MustCompile(`(?m)^[ \t]*type\s*$|(?m)^[ \t]*type\s+`)

	// import statement: import module1, module2; import module1/sub
	importRE = regexp.MustCompile(
		`(?m)^[ \t]*import\s+([^\n#]+)`,
	)

	// include statement: include module
	includeRE = regexp.MustCompile(
		`(?m)^[ \t]*include\s+([^\n#]+)`,
	)

	// from X import Y
	fromImportRE = regexp.MustCompile(
		`(?m)^[ \t]*from\s+(\S+)\s+import\s`,
	)

	// call site: identifier( or identifier.method(
	callRE = regexp.MustCompile(
		`(?:^|[^\w.])([a-zA-Z_][a-zA-Z0-9_]*)(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?\s*\(`,
	)
)

// nimKeywords are tokens that the call regex picks up but are not real calls.
var nimKeywords = map[string]bool{
	"if": true, "elif": true, "else": true, "when": true, "while": true,
	"for": true, "case": true, "of": true, "try": true, "except": true,
	"finally": true, "raise": true, "return": true, "yield": true,
	"break": true, "continue": true, "block": true, "defer": true,
	"proc": true, "func": true, "method": true, "iterator": true,
	"converter": true, "template": true, "macro": true,
	"type": true, "var": true, "let": true, "const": true,
	"import": true, "include": true, "from": true, "export": true,
	"discard": true, "echo": true, "and": true, "or": true, "not": true,
	"in": true, "notin": true, "is": true, "isnot": true,
	"addr": true, "cast": true, "nil": true, "true": true, "false": true,
	"object": true, "enum": true, "tuple": true, "ref": true, "ptr": true,
	"concept": true, "mixin": true, "bind": true, "using": true,
	"static": true, "asm": true, "emit": true,
	// built-in procs that are effectively keywords
	"new": true, "newSeq": true, "newString": true,
}

// Extract processes the Nim source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractNim(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "nim")
	extractor.TagEntitiesLanguage(out, "nim")
	return out, nil
}

func extractNim(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	imports := collectImports(src)
	importEntities := buildImportEntities(filePath, imports)
	if len(importEntities) > 0 {
		entities = append(entities, importEntities...)
	}

	// 1. Proc/func/method/template/macro/iterator declarations.
	seen := make(map[string]bool)
	for _, m := range procRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 7 {
			continue
		}
		indent := src[m[2]:m[3]]
		name := strings.TrimSuffix(src[m[4]:m[5]], "*") // strip export marker
		params := ""
		if m[6] >= 0 && m[7] >= 0 {
			params = src[m[6]:m[7]]
		}
		key := indent + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractIndentBody(src, m[1], len(indent))
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)

		sig := buildSig(src[m[0]:m[1]], name, params)

		// Determine if this is a top-level proc or a method on a type.
		subtype := "proc"
		// Check if the keyword is "method" — Nim methods are dispatched on types
		kw := extractKeyword(src[m[0]:m[1]])
		if kw == "method" || kw == "template" || kw == "macro" || kw == "iterator" {
			subtype = kw
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "nim",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          sig,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 2. Type declarations — objects, enums, tuples.
	typeSeen := make(map[string]bool)
	for _, m := range typeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 5 {
			continue
		}
		name := strings.TrimSuffix(src[m[2]:m[3]], "*")
		kind := src[m[4]:m[5]]
		if typeSeen[name] {
			continue
		}
		typeSeen[name] = true

		startLine := strings.Count(src[:m[0]], "\n") + 1

		// Determine subtype from the kind clause.
		subtype := "object"
		if strings.HasPrefix(kind, "ref") {
			subtype = "ref object"
		} else if kind == "enum" {
			subtype = "enum"
		} else if kind == "tuple" {
			subtype = "tuple"
		} else if strings.HasPrefix(kind, "distinct") {
			subtype = "distinct"
		}

		body := extractIndentBody(src, m[1], 0)
		endLine := startLine + strings.Count(body, "\n")

		// Find methods declared for this type (methods take first param of this type).
		var rels []types.RelationshipRecord
		methodSeen := make(map[string]bool)
		for _, pm := range procRE.FindAllStringSubmatchIndex(src, -1) {
			if len(pm) < 7 {
				continue
			}
			procName := strings.TrimSuffix(src[pm[4]:pm[5]], "*")
			if methodSeen[procName] {
				continue
			}
			// Check if any parameter references this type name.
			params := ""
			if pm[6] >= 0 && pm[7] >= 0 {
				params = src[pm[6]:pm[7]]
			}
			if containsTypeName(params, name) {
				methodSeen[procName] = true
				ref := extractor.BuildOperationStructuralRef("nim", filePath, procName)
				rels = append(rels, types.RelationshipRecord{
					ToID: ref,
					Kind: "CONTAINS",
				})
			}
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "nim",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          name + " = " + kind,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	return entities
}

// extractKeyword returns the proc/func/method/etc keyword from the declaration line.
func extractKeyword(decl string) string {
	for _, kw := range []string{"method", "template", "macro", "iterator", "converter", "func", "proc"} {
		if strings.Contains(decl, kw+" ") || strings.Contains(decl, kw+"\t") {
			return kw
		}
	}
	return "proc"
}

// buildSig constructs a human-readable signature from the raw declaration prefix.
func buildSig(declPrefix, name, params string) string {
	kw := extractKeyword(declPrefix)
	if params != "" {
		return kw + " " + name + params
	}
	return kw + " " + name
}

// collectImports parses import/include/from statements and returns unique module paths.
func collectImports(src string) []string {
	seen := make(map[string]bool)
	var imports []string

	addModule := func(mod string) {
		mod = strings.TrimSpace(mod)
		// Strip inline comments
		if ci := strings.Index(mod, "#"); ci >= 0 {
			mod = strings.TrimSpace(mod[:ci])
		}
		if mod == "" {
			return
		}
		if !seen[mod] {
			seen[mod] = true
			imports = append(imports, mod)
		}
	}

	// import module1, module2, module3
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		parts := strings.Split(m[1], ",")
		for _, p := range parts {
			addModule(strings.TrimSpace(p))
		}
	}

	// include module
	for _, m := range includeRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		addModule(strings.TrimSpace(m[1]))
	}

	// from module import ...
	for _, m := range fromImportRE.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		addModule(strings.TrimSpace(m[1]))
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
			Language:   "nim",
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
// e.g. "std/strutils" → "strutils", "asyncdispatch" → "asyncdispatch"
func importDisplayName(mod string) string {
	mod = strings.TrimSpace(mod)
	// Nim uses / as path separator in imports
	if slash := strings.LastIndexByte(mod, '/'); slash >= 0 {
		mod = mod[slash+1:]
	}
	return mod
}

// extractIndentBody returns the body text following a declaration line.
// It collects lines that are more indented than baseIndent (the declaration's own indent level).
// For top-level procs (indent=0), collects all lines that start with at least one space/tab.
func extractIndentBody(src string, afterPos int, baseIndentLen int) string {
	rest := src[afterPos:]
	lines := strings.Split(rest, "\n")
	if len(lines) == 0 {
		return ""
	}

	var bodyLines []string
	// The first line after '=' may be on the same line or the next.
	// We want lines that are more indented than the declaration.
	minBodyIndent := baseIndentLen + 2 // Nim typically uses 2-space indent

	for i, line := range lines {
		if i == 0 && strings.TrimSpace(line) != "" {
			// Same-line body: "proc foo() = result"
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
			// Back to same or lesser indent — body ends
			break
		}
	}
	return strings.Join(bodyLines, "\n")
}

// countIndent counts leading spaces/tabs in a line (tabs count as 1).
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

// collectCalls extracts CALLS edges from a proc body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripStringsAndComments(body)
	matches := callRE.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		target := m[1]
		if target == "" {
			continue
		}
		if nimKeywords[target] {
			continue
		}
		if target == callerName {
			continue // skip self-recursion
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
		})
	}
	return out
}

// containsTypeName checks whether a parameter list string contains a reference
// to typeName (e.g. "self: MyType" or "x: var MyType").
func containsTypeName(params, typeName string) bool {
	if params == "" || typeName == "" {
		return false
	}
	// Simple check: type name appears after a colon in params
	return strings.Contains(params, typeName)
}

// stripStringsAndComments replaces string literals and #-line-comments
// with spaces so the call scanner doesn't pick up tokens inside them.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := byte(0) // 0=none, '"'=double-quote, '\''=single-quote
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
			// Check for triple-quoted string """..."""
			if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				// Find closing """
				end := strings.Index(src[i+3:], `"""`)
				if end >= 0 {
					for j := i; j < i+3+end+3; j++ {
						if j < len(out) {
							out[j] = ' '
						}
					}
					i = i + 3 + end + 3
					continue
				}
			}
			inStr = '"'
			out[i] = ' '
			i++
		case '\'':
			inStr = '\''
			out[i] = ' '
			i++
		case '#':
			// Nim comment: # to end of line
			for i < len(src) && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		default:
			out[i] = ch
			i++
		}
	}
	return string(out)
}
