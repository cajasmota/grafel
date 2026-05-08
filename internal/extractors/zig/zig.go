// Package zig implements a regex-based extractor for Zig source files.
//
// Extracted entities:
//   - fn declarations (pub or private)  → Kind="SCOPE.Operation", Subtype="function"
//   - const Name = struct { ... }       → Kind="SCOPE.Component", Subtype="struct"
//
// No tree-sitter grammar for Zig is bundled in smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
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
	// const Name = struct {
	structRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:pub\s+)?const\s+(\w+)\s*=\s*struct\s*\{`,
	)
	// @import("module")
	importRE = regexp.MustCompile(
		`@import\("([^"]+)"\)`,
	)
)

// Extract processes the Zig source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	return extractZig(string(file.Content), file.Path), nil
}

func extractZig(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	imports := collectImports(src)

	// Public functions.
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
		})
	}

	// Private functions (no pub prefix).
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
		})
	}

	// Structs.
	for _, m := range structRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1]-1)

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
		})
	}

	return entities
}

func collectImports(src string) []string {
	var imports []string
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		if len(m) > 1 {
			imports = append(imports, m[1])
		}
	}
	return imports
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
