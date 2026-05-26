// Package dart implements a regex-based extractor for Dart source files.
//
// Extracted entities:
//   - class / abstract class / mixin / extension → Kind="SCOPE.Component", Subtype="class"
//   - method / top-level function                → Kind="SCOPE.Operation", Subtype="method"
//
// Issue #369 (PORT-RELS-DART) — emits the same three relationship kinds
// the other ported extractors emit:
//
//   - IMPORTS: every `import '...';` produces a SCOPE.Component placeholder
//     for the top dotted segment with an IMPORTS edge whose Properties
//     follow the Java/Python contract (#120/#93): local_name, source_module,
//     imported_name. Dart prefix imports (`import 'foo.dart' as fb;`) put
//     the prefix into local_name; bare module imports use the leaf segment
//     of the URI path. The full URI is preserved as ToID.
//   - CALLS: every bare `name(...)` and `recv.name(...)` invocation inside
//     a method body emits one CALLS edge per unique target. Self-recursion
//     and Dart control-flow keywords (if, for, while, ...) are filtered.
//   - CONTAINS: each class declaration attaches one CONTAINS edge per
//     method whose declaration falls inside its body, using the structural
//     -ref shape `scope:operation:method:dart:<file>:<name>` (Format A,
//     #144) so the resolver disambiguates same-named methods declared in
//     different files.
//
// No tree-sitter grammar for Dart is bundled in smacker/go-tree-sitter, so
// this extractor stays regex-based. Registers itself via init() and is
// imported by registry_gen.go.
package dart

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("dart", &Extractor{})
}

// Extractor implements extractor.Extractor for Dart.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "dart" }

// Patterns mirror Python DartParser.
var (
	classRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?(?:class|mixin|extension)\s+(\w+)` +
			`(?:\s+extends\s+\w+)?(?:\s+with\s+[\w,\s]+)?(?:\s+implements\s+[\w,\s]+)?\s*\{`,
	)
	// importRE captures the URI of `import '...';` / `import "...";` and
	// the optional `as <prefix>` alias. Submatches:
	//   1: single-quoted URI    2: double-quoted URI
	//   3: prefix after `as`    (alias, optional, applies to either form)
	importRE = regexp.MustCompile(
		`(?m)^[ \t]*import\s+(?:'([^']+)'|"([^"]+)")(?:\s+as\s+(\w+))?`,
	)
	methodRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:(?:static|async|override|final|const|@\w+\s+)*)` +
			`(?:[\w<>\[\]?]+\s+)?` + // return type (optional)
			`(\w+)\s*\(([^)]*)\)\s*` + // name + params
			`(?:async\s*)?(?:\*\s*)?` + // async/generator modifier
			`\{`,
	)
	// callRE matches `[recv.]name(` invocation heads inside a method body.
	// Submatches:
	//   1: optional receiver chain ending with `.` (e.g. "this.", "foo.",
	//      "a.b.c."). May be empty for bare calls.
	//   2: callee identifier
	//
	// The trailing `(` is consumed as a non-capturing positive lookahead via
	// a literal `\(` — Go's RE2 lacks lookaround but the literal byte is
	// matched and the regex is anchored only at word boundaries on the
	// callee. Whitespace before `(` is allowed; type-arg `<...>` segments
	// are NOT supported (rare in idiomatic Dart calls).
	callRE = regexp.MustCompile(
		`((?:[A-Za-z_][\w]*\.)+)?([A-Za-z_][\w]*)\s*\(`,
	)
)

// skipKeywords are Dart keywords that match the method/call patterns but
// are not real method declarations or call targets.
var skipKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true,
	"do": true, "switch": true, "try": true, "catch": true,
	"finally": true, "return": true, "assert": true, "throw": true,
	"import": true, "export": true, "class": true, "abstract": true,
	"mixin": true, "extension": true, "enum": true,
	// Common control-flow / built-in expression heads that the call regex
	// would otherwise match as bare invocations.
	"new": true, "await": true, "yield": true, "is": true, "as": true,
	"in": true, "var": true, "final": true, "const": true,
	"true": true, "false": true, "null": true, "void": true,
	"super": true, "this": true,
}

// callKeywordReceivers are receiver tokens that should be treated as
// "no receiver" for CALLS target resolution (e.g. `this.foo()` is the
// bare call `foo()`, `super.foo()` is `foo()`).
var callKeywordReceivers = map[string]bool{
	"this":  true,
	"super": true,
}

// Extract processes the Dart source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractDart(string(file.Content), file.Path)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(out, "dart")
	extractor.TagEntitiesLanguage(out, "dart")
	return out, nil
}

// classSpan tracks a class's start/end byte offsets and the index of its
// EntityRecord in the output slice so we can append CONTAINS edges to it
// after method-pass discovery.
type classSpan struct {
	name      string
	startByte int
	endByte   int // byte offset of the class's closing '}'
	startLine int
	endLine   int
	idx       int // index into the entities slice
}

func extractDart(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	imports := collectImports(src)
	importStrs := importStringsForLegacy(imports)

	// Imports — emit one Component+IMPORTS per import (issue #369).
	for _, imp := range imports {
		entities = append(entities, buildImportRecord(imp, filePath))
	}

	// Classes.
	classes := make([]classSpan, 0, 8)
	for _, m := range classRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endByte := findBraceEndByte(src, m[1]-1)
		endLine := strings.Count(src[:endByte], "\n") + 1
		idx := len(entities)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "class",
			SourceFile:         filePath,
			Language:           "dart",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "class " + name,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": importStrs,
			},
		})
		classes = append(classes, classSpan{
			name:      name,
			startByte: m[0],
			endByte:   endByte,
			startLine: startLine,
			endLine:   endLine,
			idx:       idx,
		})
	}

	// Methods / functions. For each, find the enclosing class (if any) by
	// byte-range containment so we can emit CONTAINS and scope CALLS
	// targets correctly.
	for _, m := range methodRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if skipKeywords[name] {
			continue
		}
		params := src[m[4]:m[5]]
		methodStart := m[0]
		bodyOpen := m[1] - 1 // byte offset of the opening '{'
		bodyEnd := findBraceEndByte(src, bodyOpen)
		startLine := strings.Count(src[:methodStart], "\n") + 1
		endLine := strings.Count(src[:bodyEnd], "\n") + 1

		// Skip method patterns that match a class-declaration line — the
		// regex can match `class Foo { ... }` heads when the class line
		// happens to lex like a method. classes are emitted separately.
		if methodStartIsClass(classes, methodStart) {
			continue
		}

		// Method body bytes: from after '{' up to the closing '}' (exclusive).
		bodyStart := bodyOpen + 1
		if bodyStart > bodyEnd {
			bodyStart = bodyEnd
		}
		body := src[bodyStart:bodyEnd]

		rec := types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "method",
			SourceFile:         filePath,
			Language:           "dart",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          name + "(" + params + ")",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": importStrs,
			},
		}
		// CALLS — scan the body for invocation heads.
		rec.Relationships = append(rec.Relationships,
			extractCallRelationships(body, name)...)
		methodEntityIdx := len(entities)
		entities = append(entities, rec)

		// CONTAINS — attach to enclosing class, if any.
		if cls := enclosingClass(classes, methodStart, bodyEnd); cls != nil {
			toID := extractor.BuildOperationStructuralRef("dart", filePath, name)
			entities[cls.idx].Relationships = append(entities[cls.idx].Relationships,
				types.RelationshipRecord{
					ToID: toID,
					Kind: "CONTAINS",
				})
		}
		_ = methodEntityIdx
	}

	return entities
}

// methodStartIsClass reports whether the byte offset is inside a class's
// declaration line (the `class Foo {` head itself), not inside its body.
// This guards against the method regex matching class declaration lines.
func methodStartIsClass(classes []classSpan, methodStart int) bool {
	for _, c := range classes {
		// methodStart equal to class start byte means the regex matched
		// the same line as the class — skip.
		if methodStart == c.startByte {
			return true
		}
	}
	return false
}

// enclosingClass returns the innermost class whose body contains the byte
// range [methodStart, methodEnd]. Returns nil for top-level functions.
func enclosingClass(classes []classSpan, methodStart, methodEnd int) *classSpan {
	var best *classSpan
	for i := range classes {
		c := &classes[i]
		// methodStart must be strictly inside (after the class's `{` line)
		// and methodEnd must be at-or-before the class's closing `}`.
		if methodStart > c.startByte && methodEnd <= c.endByte {
			if best == nil || c.startByte > best.startByte {
				best = c
			}
		}
	}
	return best
}

// importInfo holds the parsed shape of a single Dart import directive.
type importInfo struct {
	uri    string // raw URI inside the quotes
	prefix string // alias after `as` (may be empty)
}

// collectImports returns one importInfo per top-level import directive.
func collectImports(src string) []importInfo {
	var out []importInfo
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		uri := m[1]
		if uri == "" {
			uri = m[2]
		}
		if uri == "" {
			continue
		}
		out = append(out, importInfo{uri: uri, prefix: m[3]})
	}
	return out
}

// importStringsForLegacy returns the comma-joined URI list emitted as the
// per-entity `imports` property — preserves Python parity for the existing
// fixture layer.
func importStringsForLegacy(imports []importInfo) string {
	parts := make([]string, 0, len(imports))
	for _, i := range imports {
		parts = append(parts, i.uri)
	}
	return strings.Join(parts, ",")
}

// buildImportRecord produces a SCOPE.Component placeholder + IMPORTS edge
// for one Dart import directive. Properties match the Java/Python
// contract (#120/#93):
//
//	Properties["local_name"]    — the prefix when `import '...' as p;`,
//	                              else the leaf path segment of the URI
//	                              with any trailing `.dart` stripped.
//	Properties["source_module"] — the URI's path prefix (with the leaf
//	                              stripped) for `package:` imports, or
//	                              the full URI for `dart:` / relative
//	                              imports without a path separator.
//	Properties["imported_name"] — equal to local_name.
//
// The Component Name is the URI scheme + first path segment so multiple
// `package:flutter/...` imports merge to one Component per top-level
// package.
func buildImportRecord(imp importInfo, filePath string) types.EntityRecord {
	leaf := dartLeafName(imp.uri)
	mod := dartModulePrefix(imp.uri)
	local := leaf
	if imp.prefix != "" {
		local = imp.prefix
	}
	props := map[string]string{
		"local_name":    local,
		"source_module": mod,
		"imported_name": leaf,
	}
	if imp.prefix != "" {
		props["alias"] = imp.prefix
	}

	top := dartTopName(imp.uri)
	return types.EntityRecord{
		Name:       top,
		Kind:       "SCOPE.Component",
		SourceFile: filePath,
		Language:   "dart",
		Relationships: []types.RelationshipRecord{
			{
				FromID:     filePath,
				ToID:       imp.uri,
				Kind:       "IMPORTS",
				Properties: props,
			},
		},
	}
}

// dartLeafName extracts the leaf identifier of a Dart import URI:
//
//	"package:flutter/material.dart"          → "material"
//	"package:http/http.dart"                 → "http"
//	"dart:convert"                           → "convert"
//	"foo.dart"                               → "foo"
//	"src/util.dart"                          → "util"
func dartLeafName(uri string) string {
	s := uri
	// Strip scheme prefix (`package:`, `dart:`).
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	// Take the last path segment.
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	// Drop a trailing `.dart`.
	s = strings.TrimSuffix(s, ".dart")
	return s
}

// dartModulePrefix returns the source module portion of an import URI,
// matching the resolver's per-language module index expectations. For
// `package:flutter/material.dart` this is "package:flutter". For
// `dart:convert` it is "dart:convert" (no submodule). For relative paths
// (`src/util.dart`) it is the path prefix ("src") or the URI itself
// when there's no separator.
func dartModulePrefix(uri string) string {
	s := uri
	scheme := ""
	if i := strings.Index(s, ":"); i >= 0 {
		scheme = s[:i+1]
		s = s[i+1:]
	}
	if scheme == "dart:" {
		return uri
	}
	// For package: and relative URIs, drop the leaf path segment.
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return scheme + s[:i]
	}
	// No path separator — use the whole URI minus the .dart suffix.
	return strings.TrimSuffix(uri, ".dart")
}

// dartTopName returns the top-level identifier suitable for the placeholder
// Component Name — the first path segment of the URI after the scheme.
//
//	"package:flutter/material.dart" → "flutter"
//	"dart:convert"                  → "convert"
//	"foo.dart"                      → "foo"
func dartTopName(uri string) string {
	s := uri
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(s, ".dart")
}

// extractCallRelationships scans a method body and emits CALLS edges for
// each unique call target. Bare `name(...)` becomes ToID="name". A
// receiver-prefixed call `recv.name(...)` becomes ToID="name" (the leaf
// method); the receiver root is recorded under Properties["receiver_root"]
// when it is a non-keyword identifier so a future cross-file resolver
// pass can attempt receiver-type binding.
//
// Self-recursion (target == callerName) is dropped to match the dedup
// semantics of other extractors.
func extractCallRelationships(body, callerName string) []types.RelationshipRecord {
	if body == "" || callerName == "" {
		return nil
	}
	type key struct {
		target, recv string
	}
	seen := make(map[key]bool)
	var rels []types.RelationshipRecord
	for _, m := range callRE.FindAllStringSubmatchIndex(body, -1) {
		recvChain := ""
		if m[2] >= 0 && m[3] >= 0 {
			recvChain = body[m[2]:m[3]]
		}
		callee := body[m[4]:m[5]]
		if skipKeywords[callee] || callee == callerName {
			continue
		}
		// Reject matches that look like declarations rather than calls,
		// e.g. `void foo() {` — the callRE doesn't anchor to start of
		// statement so it can match declaration lines. A simple heuristic:
		// skip when the byte just before the match is an alphanumeric/
		// underscore character (likely the tail of a return type).
		if m[0] > 0 {
			prev := body[m[0]-1]
			if (prev >= 'A' && prev <= 'Z') || (prev >= 'a' && prev <= 'z') ||
				(prev >= '0' && prev <= '9') || prev == '_' {
				// Allow when previous char is `.` (already part of
				// recv chain — but FindAllStringSubmatchIndex would
				// have included it). Anything else is suspicious.
				continue
			}
		}
		// Strip trailing '.' and pick the leftmost root identifier.
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

// findBraceEndByte returns the byte offset of the matching closing `}`
// for the brace at bracePos.
func findBraceEndByte(src string, bracePos int) int {
	depth := 0
	for i := bracePos; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(src) - 1
}

// findBraceEnd returns the line of the closing } starting from bracePos.
// Retained for API stability; the relationship-emitting paths use
// findBraceEndByte directly.
func findBraceEnd(src string, bracePos int) int {
	endByte := findBraceEndByte(src, bracePos)
	return strings.Count(src[:endByte], "\n") + 1
}
