// Package graphql implements a regex-based extractor for GraphQL schema/operation files.
//
// Extracted entities:
//   - type  definitions        → Kind="SCOPE.Schema", Subtype="type"
//   - interface definitions    → Kind="SCOPE.Schema", Subtype="interface"
//   - enum  definitions        → Kind="SCOPE.Schema", Subtype="enum"
//   - union definitions        → Kind="SCOPE.Schema", Subtype="union"
//   - input definitions        → Kind="SCOPE.Schema", Subtype="input"
//   - scalar definitions       → Kind="SCOPE.Schema", Subtype="scalar"
//   - query operations         → Kind="SCOPE.Schema", Subtype="query"
//   - mutation operations      → Kind="SCOPE.Schema", Subtype="mutation"
//   - subscription operations  → Kind="SCOPE.Schema", Subtype="subscription"
//   - fragment definitions     → Kind="SCOPE.Schema", Subtype="fragment"
//
// No tree-sitter grammar for GraphQL is bundled in smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package graphql

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("graphql", &Extractor{})
}

// Extractor implements extractor.Extractor for GraphQL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "graphql" }

// Patterns for GraphQL constructs.
var (
	// type Foo { / interface Foo { / enum Foo { / union Foo / input Foo { / scalar Foo
	typeDefRE = regexp.MustCompile(
		`(?m)^(?:type|interface|enum|union|input|scalar)\s+(\w+)`,
	)
	// query|mutation|subscription Name(
	operationRE = regexp.MustCompile(
		`(?m)^(query|mutation|subscription)\s+(\w+)`,
	)
	// fragment Name on Type {
	fragmentRE = regexp.MustCompile(
		`(?m)^fragment\s+(\w+)\s+on\s+\w+`,
	)
	// Keyword before name — used to determine subtype.
	keywordRE = regexp.MustCompile(
		`^(type|interface|enum|union|input|scalar)`,
	)
)

// Extract processes the GraphQL source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	return extractGraphQL(string(file.Content), file.Path), nil
}

func extractGraphQL(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// Type system definitions.
	for _, m := range typeDefRE.FindAllStringSubmatchIndex(src, -1) {
		line := src[m[0]:m[1]]
		name := src[m[2]:m[3]]
		key := name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[0])

		kw := keywordRE.FindString(strings.TrimSpace(line))
		subtype := kw

		sig := kw + " " + name
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            subtype,
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          sig,
			EnrichmentRequired: false,
		})
	}

	// Operation definitions.
	for _, m := range operationRE.FindAllStringSubmatchIndex(src, -1) {
		opType := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		key := opType + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[0])
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            opType,
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          opType + " " + name,
			EnrichmentRequired: false,
		})
	}

	// Fragment definitions.
	for _, m := range fragmentRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "fragment:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[0])
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Schema",
			Subtype:            "fragment",
			SourceFile:         filePath,
			Language:           "graphql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "fragment " + name,
			EnrichmentRequired: false,
		})
	}

	return entities
}

// findBlockEnd returns the line where the { ... } block starting after pos closes.
// For scalars/unions without braces it returns startLine.
func findBlockEnd(src string, startPos int) int {
	bracePos := strings.Index(src[startPos:], "{")
	if bracePos < 0 {
		return strings.Count(src[:startPos], "\n") + 1
	}
	abs := startPos + bracePos
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
