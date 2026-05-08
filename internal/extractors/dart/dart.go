// Package dart implements a regex-based extractor for Dart source files.
//
// Extracted entities:
//   - class / abstract class / mixin / extension → Kind="SCOPE.Component", Subtype="class"
//   - method / top-level function                → Kind="SCOPE.Operation", Subtype="method"
//
// No tree-sitter grammar for Dart is bundled in smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
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
	importRE = regexp.MustCompile(
		`(?m)^import\s+'([^']+)'|^import\s+"([^"]+)"`,
	)
	methodRE = regexp.MustCompile(
		`(?m)^[ \t]*(?:(?:static|async|override|final|const|@\w+\s+)*)` +
			`(?:[\w<>\[\]?]+\s+)?` + // return type (optional)
			`(\w+)\s*\(([^)]*)\)\s*` + // name + params
			`(?:async\s*)?(?:\*\s*)?` + // async/generator modifier
			`\{`,
	)
)

// skipKeywords are Dart keywords that match the method pattern but are not functions.
var skipKeywords = map[string]bool{
	"if": true, "else": true, "for": true, "while": true,
	"do": true, "switch": true, "try": true, "catch": true,
	"finally": true, "return": true, "assert": true, "throw": true,
	"import": true, "export": true, "class": true, "abstract": true,
	"mixin": true, "extension": true, "enum": true,
}

// Extract processes the Dart source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	return extractDart(string(file.Content), file.Path), nil
}

func extractDart(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	imports := collectImports(src)

	// Classes.
	for _, m := range classRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1]-1)
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
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// Methods / functions.
	for _, m := range methodRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if skipKeywords[name] {
			continue
		}
		params := src[m[4]:m[5]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBraceEnd(src, m[1]-1)
		entities = append(entities, types.EntityRecord{
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
				"imports": strings.Join(imports, ","),
			},
		})
	}

	return entities
}

func collectImports(src string) []string {
	var imports []string
	for _, m := range importRE.FindAllStringSubmatch(src, -1) {
		imp := m[1]
		if imp == "" {
			imp = m[2]
		}
		if imp != "" {
			imports = append(imports, imp)
		}
	}
	return imports
}

// findBraceEnd returns the line of the closing } starting from bracePos.
func findBraceEnd(src string, bracePos int) int {
	depth := 0
	for i, ch := range src[bracePos:] {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.Count(src[:bracePos+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}
