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
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	//
	// #7190: the `type` keyword prefix is `type[ \t]+`, NOT `type\s+`. `\s`
	// matches a NEWLINE, so for form 1 the match used to START on the block
	// keyword's line — which made the first member's StartLine the keyword's
	// line and left group 1 (the indent) holding the KEYWORD's indentation
	// instead of the declaration's. Both of #7190's symptoms came from that one
	// wrong anchor. Group 1 is the DECLARATION's own indent and is what the
	// call site passes to extractIndentBody as baseIndentLen; it was hard-coded
	// 0 there, which is right only for a declaration at column 0.
	//
	// #7213: a member carrying a PRAGMA produced no entity at all. Only a generic
	// parameter list was admitted between the name and the `=`, and a pragma sits
	// in exactly that position, so `Alpha* {.packed.} = object` never matched.
	// Measured over 4431 .nim files (nim-lang/Nim, nimbus-eth2, pixie, nitter,
	// jester): 839 of 6980 type members — 12.0% — were dropped for this reason.
	//
	// The pragma group follows the generic group and never precedes it:
	// doc/grammar.txt gives `typeDef = identVisDot genericParamList? pragma?
	// ('=' optInd typeDefValue)?`, and nim-lang/Nim's own
	// tests/types/told_pragma_syntax2.nim asserts the reverse order is a compile
	// error. Its body is `[^}\n]*`, NOT `[^}]*`: `[^}]` matches a NEWLINE, so an
	// unterminated `{.` would run through every following declaration to the next
	// `.}` and absorb them. The cost of that choice is the multi-line pragma
	// (361 further sites, 4.9%), left unmatched deliberately.
	//
	// THE CLOSING DELIMITER IS `\.?\}`, NOT `\.\}` — the dot is optional. Nim's
	// compiler/parser.nim, parsePragma, accepts either token:
	//
	//	while p.tok.tokType notin {tkCurlyDotRi, tkCurlyRi, tkEof}: ...
	//	if p.tok.tokType in {tkCurlyDotRi, tkCurlyRi}: getTok(p)
	//
	// and doc/grammar.txt states it as `pragma = '{.' optInd (exprColonEqExpr
	// comma?)* optPar ('.}' | '}')`. 7 sites in the population close with a
	// plain `}` (`MyObject {.exportc: "ExtObject"} = object`). The OPENING
	// delimiter has no such latitude: the lexer has one token, tkCurlyDotLe, so
	// `{` alone never opens a pragma and neither does `{ .` — the two
	// characters must be adjacent.
	typeRE = regexp.MustCompile(
		`(?m)^([ \t]*)(?:type[ \t]+)?([A-Z][a-zA-Z0-9_]*\*?)\s*(?:\[[^\]]*\])?[ \t]*(?:\{\.[^}\n]*\.?\})?\s*=\s*(object|ref\s+object|enum|tuple|distinct\s+\w+)`,
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
	// #6815: buildImportEntities anchors every IMPORTS edge on file.Path with
	// nothing carrying that string as its Name. Emit the #577 file carrier when
	// — and only when — such an edge exists. See extractor.FileCarrierFor.
	out = extractor.PrependFileCarrier(file.Path, "nim", out)
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
		// #7212: measured from m[1], the same offset `body` starts at — NOT
		// from startLine, which is m[0]'s line. A wrapped parameter list makes
		// the match span lines and the two offsets diverge.
		endLine := spanEndLine(src, m[1], body)
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
		if len(m) < 8 {
			continue
		}
		indent := src[m[2]:m[3]] // #7190: the DECLARATION's own indent
		name := strings.TrimSuffix(src[m[4]:m[5]], "*")
		kind := src[m[6]:m[7]]
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

		// #7190: the base is the declaration's OWN column. It was hard-coded 0,
		// which is right only when the declaration sits at column 0 — the shape
		// every pre-existing fixture happened to use. In an idiomatic `type`
		// SECTION the members are indented, so at 0 every following sibling was
		// "more indented than the declaration" and got absorbed into the first
		// member's body.
		body := extractIndentBody(src, m[1], len(indent))
		// #7212: same origin as `body`. A `= object` clause broken after the
		// `=` or after `ref` puts m[1] lines below m[0], and those lines used
		// to fall into neither term of the sum.
		endLine := spanEndLine(src, m[1], body)

		// Find methods declared for this type (methods take first param of this type).
		var rels []types.RelationshipRecord

		// Inheritance: `= ref object of Base` (#6370). m[1] is the byte just
		// past the `object` keyword, which is the ONLY position an `of` can
		// mean inheritance in Nim — see hierarchy.go.
		if ext := baseOfEdge(src, m[1], kind, name, startLine); ext != nil {
			rels = append(rels, *ext)
		}
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
			Name: importDisplayName(mod),
			Kind: "SCOPE.Component",
			// #6481: resolve/refs.go keys the import-placeholder marker on
			// kind=="SCOPE.Component" && subtype=="import". Without it this stub
			// stays in the by-name index as a real declaration of its LAST PATH
			// SEGMENT and flips every colliding name AMBIGUOUS.
			Subtype:    "import",
			SourceFile: filePath,
			Language:   "nim",
			// The FULL module path, not the display name:
			// resolve.placeholderModuleSpecifier reads import_module first, and
			// the #6156 restore would otherwise record the bare last segment.
			Properties: map[string]string{"import_module": mod},
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

// #7212: the line on which byte offset `pos` sits, 1-based. Both call sites cut
// the declaration's body at the regex match END (m[1]) and then measure its
// length in newlines, so the line that length is added TO must be the line of
// that same offset. It used to be the line of the match START (m[0]): sound
// only while the match is confined to one line, and short by the match's own
// line count whenever it is not — a `= object` clause broken after the `=` or
// after `ref`, or a proc parameter list wrapped across lines. Not an
// off-by-one; the deficit is the clause's line count, so it is 2 at three
// lines. See span_origin_7212_test.go.
func lineOf(src string, pos int) int {
	return strings.Count(src[:pos], "\n") + 1
}

// spanEndLine assembles the end of a declaration's span from ONE origin: the
// line of `afterPos` — the offset `body` was cut from — plus the body's own
// line count. Callers keep deriving StartLine from the match start, which is
// the declaration's own line and is correct there.
func spanEndLine(src string, afterPos int, body string) int {
	return lineOf(src, afterPos) + strings.Count(body, "\n")
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
	// #7195: a source that ends in a newline makes strings.Split yield a FINAL
	// EMPTY ELEMENT — the empty remainder after the last '\n'. It is not a line
	// of the file, but it satisfies the `TrimSpace(line) == ""` arm below and was
	// appended as a blank body line. `endLine := startLine + Count(body, "\n")`
	// at both call sites then counted it, so the LAST declaration in every
	// newline-terminated file reported EndLine = lineCount + 1: a span past EOF.
	// Earlier declarations broke on their following sibling and never reached
	// this element, which is why only the last one was ever wrong.
	//
	// Drop EXACTLY that one element and nothing else — and test `== ""`, never
	// `strings.TrimSpace(...) == ""`. Every widening of this guard is the
	// PERMISSIVE direction and deletes a real line from a span:
	//
	//   - trimming trailing blank lines from the COLLECTED BODY shortens an
	//     earlier declaration whose body ends in blanks before a sibling —
	//     forbidden by TestEOF7195ForbiddenEarlierDeclUnchanged and its type
	//     twin (these do NOT grade the two routes below; a blank before a
	//     sibling is mid-split and unreachable from the end);
	//   - looping the drop over every trailing empty element shortens a body
	//     whose blanks run to EOF — forbidden by
	//     TestEOF7195ForbiddenTrailingBlankLinesAtEOFKept and its type twin;
	//   - keying on TrimSpace deletes the last line of any file whose final
	//     line is whitespace-only and which does NOT end in a newline, where
	//     that element IS the line and no phantom exists — forbidden by
	//     TestEOF7195ForbiddenWhitespaceLineAtEOFNoTrailingNewline and its type
	//     twin. This crossed cell was unforbidden in the first round and the
	//     TrimSpace mutant passed the whole package.
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	var bodyLines []string
	// The first line after '=' may be on the same line or the next.
	// We want lines that are more indented than the declaration.
	// #7185: the body continues at any column STRICTLY GREATER than the
	// declaration's own column, so the threshold is baseIndentLen+1 — not +2.
	//
	// It was +2, which left a DEAD BAND at exactly baseIndentLen+1: such a line
	// satisfied neither `indent >= minBodyIndent` nor `indent <= baseIndentLen`,
	// so the loop silently skipped it and kept scanning. The emitted body then had
	// a HOLE — the base+1 line gone while deeper lines below it were still
	// collected — which moved EndLine and dropped CALLS edges.
	//
	// +1 IS NIM'S RULE, NOT A STYLE GUESS, AND NOT F#'s. PR #7184 chose +1 for the
	// fsharp copy of this helper from the F# 4.1 offside rule; that argument is
	// about F# and does not transfer. Nim manual, Lexical Analysis -> Indentation:
	//
	//	"Nim's standard grammar describes an indentation sensitive language. This
	//	 means that all the control structures are recognized by indentation.
	//	 Indentation consists only of spaces; tabulators are not allowed."
	//
	// and the grammar pseudo-terminals that same section defines:
	//
	//	IND{>}	"denotes an indentation that consists of MORE SPACES than the
	//		 entry at the top of the stack"
	//	IND{=}	"an indentation that has the SAME number of spaces"
	//
	// An indented statement list is introduced by IND{>} — strictly more spaces
	// than the enclosing entry — while a SIBLING is IND{=}, i.e. exactly the
	// enclosing column. The manual names no minimum step, so "more spaces" is
	// satisfied by one. base+1 is therefore body, and only base-or-less can be a
	// sibling — which is exactly the `indent <= baseIndentLen` terminator below.
	// With +1 the two conditions are complementary and no band can exist.
	//
	// DERIVED-NOT-EXECUTED: no Nim toolchain exists on the build machine, so this
	// is read off the manual rather than compiled. FALSIFIER: a Nim program in
	// which a statement indented exactly one space deeper than its enclosing
	// declaration is rejected, or parses as that declaration's SIBLING.
	minBodyIndent := baseIndentLen + 1

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
	matches := callRE.FindAllStringSubmatchIndex(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]types.RelationshipRecord, 0, len(matches))
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		// m[2] and m[3] are the start and end indices of the first capturing group (the identifier)
		if m[2] < 0 || m[3] < 0 {
			continue
		}
		target := scrubbed[m[2]:m[3]]
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
		// Compute line number by counting newlines up to match position
		lineNum := 1 + strings.Count(scrubbed[:m[0]], "\n")
		out = append(out, types.RelationshipRecord{
			ToID: target,
			Kind: "CALLS",
			Properties: types.Props{
				{K: "line", V: strconv.Itoa(lineNum)},
			},
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
