// Package sml implements a regex-based extractor for Standard ML source files.
//
// Extracted entities:
//   - `structure Foo : SIG = struct ... end` → SCOPE.Component (subtype="structure")
//   - `signature SIG = sig ... end`          → SCOPE.Component (subtype="signature")
//   - `functor Foo (X : SIG) = struct ... end` → SCOPE.Component (subtype="functor")
//   - `fun name args = body`                 → SCOPE.Operation (subtype="function")
//   - `val name = expr`                      → SCOPE.Operation (subtype="val")
//   - `datatype 'a tree = Leaf | Node`       → SCOPE.Component (subtype="datatype")
//   - `open Foo` statements                  → IMPORTS edges
//   - Function applications                  → CALLS edges
//
// File extensions handled: .sml, .sig, .fun
// NOTE: .ml is intentionally left mapped to OCaml — do NOT change that.
//
// No tree-sitter grammar for SML is available in smacker/go-tree-sitter, so
// this extractor uses regular expressions. SML uses explicit struct/sig/end
// block delimiters, making indentation-independent parsing feasible.
//
// Registers itself via init() and is imported by registry_gen.go.
package sml

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("sml", &Extractor{})
}

// Extractor implements extractor.Extractor for Standard ML.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "sml" }

// -----------------------------------------------------------------------
// Compiled regex patterns
// -----------------------------------------------------------------------

var (
	// structureRE matches structure declarations:
	//   structure Foo = struct ... end
	//   structure Foo : SIG = struct ... end
	structureRE = regexp.MustCompile(
		`(?m)^structure\s+([A-Za-z][A-Za-z0-9_']*)\s*(?::[^=]*)?\s*=`,
	)

	// signatureRE matches signature declarations:
	//   signature SIG = sig ... end
	signatureRE = regexp.MustCompile(
		`(?m)^signature\s+([A-Z][A-Za-z0-9_']*)\s*=`,
	)

	// functorRE matches functor declarations:
	//   functor Foo (X : SIG) = struct ... end
	//   functor Foo (X : SIG) : OUTSIG = struct ... end
	functorRE = regexp.MustCompile(
		`(?m)^functor\s+([A-Za-z][A-Za-z0-9_']*)\s*\(`,
	)

	// funRE matches top-level fun declarations (at column 0):
	//   fun name args = body
	//   fun name args = body | name args = body   (clausal)
	funRE = regexp.MustCompile(
		`(?m)^fun\s+([a-z_][A-Za-z0-9_']*)\s`,
	)

	// valRE matches top-level val declarations (at column 0):
	//   val name = expr
	//   val rec name = fn ...
	valRE = regexp.MustCompile(
		`(?m)^val(?:\s+rec)?\s+([a-z_][A-Za-z0-9_']*)\s*=`,
	)

	// datatypeRE matches datatype declarations:
	//   datatype 'a tree = Leaf | Node of 'a * 'a tree
	//   datatype color = Red | Green | Blue
	datatypeRE = regexp.MustCompile(
		`(?m)^datatype\s+(?:(?:'[A-Za-z_][A-Za-z0-9_']*\s+)+)?([A-Za-z][A-Za-z0-9_']*)\s*=`,
	)

	// openRE matches open statements:
	//   open Foo
	//   open Foo.Bar
	openRE = regexp.MustCompile(
		`(?m)^open\s+([A-Z][A-Za-z0-9_'.]*(?:\.[A-Z][A-Za-z0-9_']*)*)`,
	)

	// callDotRE matches module-qualified calls: Structure.function
	callDotRE = regexp.MustCompile(
		`\b([A-Z][A-Za-z0-9_']*(?:\.[A-Z][A-Za-z0-9_']*)*\.[a-z_][A-Za-z0-9_']*)\b`,
	)

	// callBareRE matches bare function applications: identifier <space>
	callBareRE = regexp.MustCompile(
		`\b([a-z_][A-Za-z0-9_']*)\s+`,
	)
)

// smlKeywords is the set of SML tokens to exclude from CALLS edges.
var smlKeywords = map[string]bool{
	// Core keywords
	"abstype": true, "and": true, "andalso": true, "as": true,
	"case": true, "datatype": true, "do": true,
	"else": true, "end": true, "eqtype": true, "exception": true,
	"fn": true, "fun": true, "functor": true,
	"handle": true,
	"if":     true, "in": true, "include": true, "infix": true, "infixr": true,
	"let": true, "local": true,
	"nonfix": true,
	"of":     true, "op": true, "open": true, "orelse": true,
	"raise": true, "rec": true,
	"sharing": true, "sig": true, "signature": true, "struct": true,
	"structure": true,
	"then":      true, "type": true,
	"val":   true,
	"where": true, "while": true, "with": true, "withtype": true,
	// Common pervasives
	"true": true, "false": true, "nil": true,
	"not": true, "hd": true, "tl": true, "null": true,
	"fst": true, "snd": true,
	"print": true, "ignore": true,
	"ref": true,
}

// Extract processes an SML source file and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractSML(string(file.Content), file.Path)
	extractor.TagRelationshipsLanguage(out, "sml")
	return out, nil
}

func extractSML(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord

	// Emit file-level entity.
	entities = append(entities, extractor.FileEntity(extractor.FileInput{
		Path:     filePath,
		Language: "sml",
	}))

	imports := collectOpenStatements(src)
	importEntities := buildImportEntities(filePath, imports)
	entities = append(entities, importEntities...)

	// 1. Structure declarations.
	seenStructures := make(map[string]bool)
	for _, m := range structureRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenStructures[name] {
			continue
		}
		// Must be at column 0.
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenStructures[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractBlockBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "structure",
			SourceFile: filePath,
			Language:   "sml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "structure " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 2. Signature declarations.
	seenSignatures := make(map[string]bool)
	for _, m := range signatureRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenSignatures[name] {
			continue
		}
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenSignatures[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractBlockBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "signature",
			SourceFile: filePath,
			Language:   "sml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "signature " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 3. Functor declarations.
	seenFunctors := make(map[string]bool)
	for _, m := range functorRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenFunctors[name] {
			continue
		}
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenFunctors[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractBlockBody(src, m[1])
		endLine := startLine + strings.Count(body, "\n")
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "functor",
			SourceFile: filePath,
			Language:   "sml",
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  "functor " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 4. Datatype declarations.
	seenDatatypes := make(map[string]bool)
	for _, m := range datatypeRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenDatatypes[name] {
			continue
		}
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenDatatypes[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:       name,
			Kind:       "SCOPE.Component",
			Subtype:    "datatype",
			SourceFile: filePath,
			Language:   "sml",
			StartLine:  startLine,
			EndLine:    startLine, // single logical line for datatypes
			Signature:  "datatype " + name,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// 5. fun function declarations (top-level only, at column 0).
	seenFuns := make(map[string]bool)
	for _, m := range funRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenFuns[name] {
			continue
		}
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenFuns[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractFunBody(src, m[0])
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)

		// Build signature from declaration line.
		sig := buildOneLinerSig(src, m[0])

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Operation",
			Subtype:       "function",
			SourceFile:    filePath,
			Language:      "sml",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     sig,
			Properties:    map[string]string{"imports": strings.Join(imports, ",")},
			Relationships: calls,
		})
	}

	// 6. val declarations (top-level only, at column 0).
	seenVals := make(map[string]bool)
	for _, m := range valRE.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		name := src[m[2]:m[3]]
		if seenVals[name] || seenFuns[name] {
			continue
		}
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		if lineStart != m[0] {
			continue
		}
		seenVals[name] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		body := extractFunBody(src, m[0])
		endLine := startLine + strings.Count(body, "\n")
		calls := collectCalls(body, name)
		sig := buildOneLinerSig(src, m[0])

		entities = append(entities, types.EntityRecord{
			Name:          name,
			Kind:          "SCOPE.Operation",
			Subtype:       "val",
			SourceFile:    filePath,
			Language:      "sml",
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     sig,
			Properties:    map[string]string{"imports": strings.Join(imports, ",")},
			Relationships: calls,
		})
	}

	return entities
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
		// Strip inline SML comments (* ... *)
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
			Language:   "sml",
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

// extractBlockBody extracts the body of an SML structure/signature/functor
// by counting struct/sig/let/local/fn...end depth.
func extractBlockBody(src string, afterPos int) string {
	if afterPos >= len(src) {
		return ""
	}
	rest := src[afterPos:]

	// depth-counting: track keywords that open new nesting levels.
	openKW := regexp.MustCompile(`\b(struct|sig|let|local|fn)\b`)
	closeKW := regexp.MustCompile(`\bend\b`)

	depth := 0
	found := false
	endPos := 0

	i := 0
	for i < len(rest) {
		// Skip SML block comment (* ... *) — not nested in SML.
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
		// Skip string literal "..."
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
		// Skip char literal #"."
		if i+2 < len(rest) && rest[i] == '#' && rest[i+1] == '"' {
			i += 2
			for i < len(rest) && rest[i] != '"' {
				i++
			}
			i++
			continue
		}

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
	// Fallback to indentation heuristics.
	return extractFunBody(src, afterPos)
}

// extractFunBody collects the body of a fun/val binding by scanning subsequent
// lines that are indented or blank until the next top-level declaration.
func extractFunBody(src string, startPos int) string {
	rest := src[startPos:]
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
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			body = append(body, line)
		} else {
			break
		}
	}
	return strings.Join(body, "\n")
}

// buildOneLinerSig extracts and trims the first line of a declaration.
func buildOneLinerSig(src string, startPos int) string {
	rest := src[startPos:]
	nl := strings.IndexByte(rest, '\n')
	var line string
	if nl >= 0 {
		line = strings.TrimSpace(rest[:nl])
	} else {
		line = strings.TrimSpace(rest)
	}
	// Trim trailing " ="
	if idx := strings.LastIndex(line, "="); idx > 0 {
		line = strings.TrimSpace(line[:idx])
	}
	return line
}

// -----------------------------------------------------------------------
// Helper: CALLS edge collection
// -----------------------------------------------------------------------

// collectCalls extracts CALLS relationships from a function body.
func collectCalls(body, callerName string) []types.RelationshipRecord {
	if body == "" {
		return nil
	}
	scrubbed := stripSMLStringsAndComments(body)
	seen := make(map[string]bool)
	var out []types.RelationshipRecord

	addCall := func(target string) {
		if target == "" || target == callerName {
			return
		}
		if smlKeywords[target] {
			return
		}
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

	// Qualified calls: Structure.function
	for _, m := range callDotRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}
	// Bare function calls
	for _, m := range callBareRE.FindAllStringSubmatch(scrubbed, -1) {
		if len(m) >= 2 {
			addCall(m[1])
		}
	}

	return out
}

// stripSMLStringsAndComments replaces SML string literals and (* ... *) block
// comments with spaces so the call scanner doesn't pick up tokens inside them.
func stripSMLStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	for i < len(src) {
		ch := src[i]

		// SML block comment: (* ... *) — NOT nested (unlike OCaml).
		if ch == '(' && i+1 < len(src) && src[i+1] == '*' {
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

		// String literal: "..."
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

		// Char literal: #"x"
		if ch == '#' && i+1 < len(src) && src[i+1] == '"' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(src) && src[i] != '"' {
				out[i] = ' '
				i++
			}
			if i < len(src) {
				out[i] = ' '
				i++
			}
			continue
		}

		out[i] = ch
		i++
	}
	return string(out)
}
