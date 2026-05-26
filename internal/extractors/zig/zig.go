// Package zig implements a regex-based extractor for Zig source files.
//
// Extracted entities:
//   - fn declarations (pub or private)  → Kind="SCOPE.Operation", Subtype="function"
//   - const Name = struct { ... }       → Kind="SCOPE.Component", Subtype="struct"
//   - @import("...") sites              → Kind="SCOPE.Component" stub carrying
//     an IMPORTS relationship
//
// Issue #382 — relationship parity with java/rust/cpp:
//
//   - IMPORTS edges are emitted from file.Path → imported module for every
//     `@import("...")` invocation. Both stdlib (`@import("std")`) and
//     relative paths (`@import("./foo.zig")`) are captured verbatim.
//   - CALLS edges are attached to each fn entity, one per unique callee
//     identifier discovered in its body. Qualified calls (`std.debug.print`,
//     `Foo.bar`) resolve to the rightmost identifier; zig keywords / control
//     forms and self-recursion are filtered out.
//   - CONTAINS edges are attached from a `const Name = struct { ... }` (and
//     enum/union variants) to every `pub fn` / `fn` declared in its
//     declaration body, using the Format A structural-ref
//     `scope:operation:method:zig:<file>:<name>`.
//
// No tree-sitter grammar for Zig is bundled in smacker/go-tree-sitter, so
// this extractor parses Zig with regular expressions plus a hand-rolled
// brace walker. Registers itself via init() and is imported by
// registry_gen.go.
package zig

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("zig", &Extractor{})
}

// Extractor implements extractor.Extractor for Zig.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "zig" }

// Patterns mirror Python ZigParser logic.
var (
	// pub fn name(...) or fn name(...)
	// Uses two separate patterns to avoid optional capture group returning -1.
	pubFnRE = regexp.MustCompile(
		`(?m)^[ \t]*pub\s+fn\s+(\w+)\s*(\([^)]*\))`,
	)
	privFnRE = regexp.MustCompile(
		`(?m)^[ \t]*fn\s+(\w+)\s*(\([^)]*\))`,
	)
	// const Name = struct {  (also enum / union — same CONTAINS semantics).
	structRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:pub\s+)?const\s+(\w+)\s*=\s*struct\s*\{`,
	)
	// @import("module")
	importRE = regexp.MustCompile(
		`@import\("([^"]+)"\)`,
	)
	// fn declarations anywhere (used to enumerate methods inside a struct
	// body). Captures the fn name. Matches both `pub fn` and `fn`.
	anyFnRE = regexp.MustCompile(
		`(?m)(?:^|\s)(?:pub\s+)?fn\s+(\w+)\s*\(`,
	)
	// Call-site head capture: a (possibly dotted) identifier path followed by
	// `(`. The path is captured; the call target is the rightmost segment.
	// Matches `helper(`, `Foo.bar(`, `std.debug.print(`. Excludes builtins
	// (which start with `@`) and indexed expressions.
	callRE = regexp.MustCompile(
		`(?:^|[^\w.@])([A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)*)\s*\(`,
	)
)

// zigKeywords lists tokens that look like calls to the bare regex walker
// but are control-flow / declaration keywords in Zig. Matches the keyword
// list the rust extractor's special-form filter plays for that language.
var zigKeywords = map[string]bool{
	"if": true, "else": true, "while": true, "for": true, "switch": true,
	"return": true, "break": true, "continue": true, "defer": true,
	"errdefer": true, "try": true, "catch": true, "orelse": true,
	"and": true, "or": true, "not": true, "fn": true, "pub": true,
	"const": true, "var": true, "comptime": true, "inline": true,
	"export": true, "extern": true, "test": true, "asm": true,
	"struct": true, "enum": true, "union": true, "opaque": true,
	"error": true, "unreachable": true, "noreturn": true,
	"true": true, "false": true, "null": true, "undefined": true,
	// Common type names that would otherwise be picked up from `Type(args)`
	// constructor-style invocations and tagged unions.
	"u8": true, "u16": true, "u32": true, "u64": true, "usize": true,
	"i8": true, "i16": true, "i32": true, "i64": true, "isize": true,
	"f16": true, "f32": true, "f64": true, "bool": true, "void": true,
	"anytype": true, "anyerror": true, "anyopaque": true, "type": true,
}

// Extract processes the Zig source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	out := extractZig(string(file.Content), file.Path)
	// Issue #90 — language tag for resolver dynamic-pattern dispatch.
	extractor.TagRelationshipsLanguage(out, "zig")
	extractor.TagEntitiesLanguage(out, "zig")
	return out, nil
}

func extractZig(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	imports := collectImports(src)

	// 1. Public functions.
	seen := make(map[string]bool)
	for _, m := range pubFnRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		params := src[m[4]:m[5]]
		key := name + ":" + params
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1])
		body := extractBraceBody(src, m[1])
		calls := collectCalls(body, name)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "zig",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "pub fn " + name + params,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 2. Private functions (no pub prefix).
	for _, m := range privFnRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		params := src[m[4]:m[5]]
		key := name + ":" + params
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1])
		body := extractBraceBody(src, m[1])
		calls := collectCalls(body, name)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "zig",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "fn " + name + params,
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: calls,
		})
	}

	// 3. Structs — emit Components and attach CONTAINS edges to every fn
	//    declared inside the brace body.
	for _, m := range structRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1]-1)
		body := extractBraceBody(src, m[1]-1)

		var rels []types.RelationshipRecord
		methodSeen := make(map[string]bool)
		for _, fm := range anyFnRE.FindAllStringSubmatch(body, -1) {
			if len(fm) < 2 {
				continue
			}
			methodName := fm[1]
			if methodSeen[methodName] {
				continue
			}
			methodSeen[methodName] = true
			ref := extractor.BuildOperationStructuralRef("zig", filePath, methodName)
			rels = append(rels, types.RelationshipRecord{
				ToID: ref,
				Kind: "CONTAINS",
			})
		}

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "struct",
			SourceFile:         filePath,
			Language:           "zig",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "const " + name + " = struct",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
			Relationships: rels,
		})
	}

	// 4. Import stubs — one SCOPE.Component per unique @import target,
	//    carrying the IMPORTS edge from file.Path → module.
	importEntities := buildImportEntities(filePath, imports)
	if len(importEntities) > 0 {
		entities = append(importEntities, entities...)
	}

	return entities
}

// collectImports returns the textual targets of every @import("...") call.
func collectImports(src string) []string {
	var imports []string
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) > 1 {
			imports = append(imports, m[1])
		}
	}
	return imports
}

// buildImportEntities turns each @import target into a SCOPE.Component
// stub carrying an IMPORTS edge from file.Path → module.
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
			Name:       importTopSegment(mod),
			Kind:       "SCOPE.Component",
			SourceFile: filePath,
			Language:   "zig",
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

// importTopSegment derives a display name for an @import target. For
// stdlib-style references like "std" it returns the bare token; for
// relative paths it returns the basename without extension so the entity
// name remains a sensible identifier.
func importTopSegment(mod string) string {
	// Strip leading "./" / "../" segments.
	trimmed := mod
	for strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		if strings.HasPrefix(trimmed, "./") {
			trimmed = trimmed[2:]
		} else {
			trimmed = trimmed[3:]
		}
	}
	if trimmed == "" {
		trimmed = mod
	}
	if slash := strings.LastIndexByte(trimmed, '/'); slash >= 0 {
		trimmed = trimmed[slash+1:]
	}
	if dot := strings.LastIndexByte(trimmed, '.'); dot > 0 {
		trimmed = trimmed[:dot]
	}
	if trimmed == "" {
		return mod
	}
	return trimmed
}

// extractBraceBody returns the textual content between the first '{' at or
// after pos and its matching '}'. Returns "" when no balanced pair is
// found.
func extractBraceBody(src string, pos int) string {
	bracePos := strings.Index(src[pos:], "{")
	if bracePos < 0 {
		return ""
	}
	abs := pos + bracePos
	depth := 0
	for i := abs; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[abs+1 : i]
			}
		}
	}
	return src[abs+1:]
}

// collectCalls walks body, extracts every callee identifier, drops Zig
// keywords / self-recursion, dedupes, and returns CALLS edges.
//
// Qualified calls (`std.debug.print`, `Foo.bar`) resolve to the rightmost
// identifier so cross-language resolution can match a free-standing
// function or method by its bare name.
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
		path := m[1]
		// Rightmost segment is the callee.
		target := path
		if dot := strings.LastIndexByte(path, '.'); dot >= 0 {
			target = path[dot+1:]
		}
		if target == "" {
			continue
		}
		if zigKeywords[target] {
			continue
		}
		if target == callerName {
			continue
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

// stripStringsAndComments replaces string literals and //-line-comments
// with spaces so the call-head scanner doesn't pick up tokens inside them.
// Zig multi-line strings (`\\` line prefix) start with `\\` and run to
// EOL — handled here as a comment-like pass.
func stripStringsAndComments(src string) string {
	out := make([]byte, len(src))
	i := 0
	inStr := false
	for i < len(src) {
		ch := src[i]
		if inStr {
			out[i] = ' '
			if ch == '\\' && i+1 < len(src) {
				out[i+1] = ' '
				i += 2
				continue
			}
			if ch == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch ch {
		case '"':
			inStr = true
			out[i] = ' '
			i++
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					out[i] = ' '
					i++
				}
				continue
			}
			out[i] = ch
			i++
		case '\\':
			// Zig multi-line string lines start with `\\`.
			if i+1 < len(src) && src[i+1] == '\\' {
				for i < len(src) && src[i] != '\n' {
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

// findBraceEnd returns the line of the closing } starting from pos.
// If pos is past a '{', it starts scanning from there.
func findBraceEnd(src string, pos int) int {
	// Find the opening brace at or after pos.
	bracePos := strings.Index(src[pos:], "{")
	if bracePos < 0 {
		return strings.Count(src[:pos], "\n") + 1
	}
	abs := pos + bracePos
	depth := 0
	for i, ch := range src[abs:] {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.Count(src[:abs+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}
