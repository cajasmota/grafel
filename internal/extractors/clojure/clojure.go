// Package clojure implements a regex-based extractor for Clojure source files.
//
// Extracted entities:
//   - defn / defn-      → Kind="SCOPE.Operation", Subtype="function"
//   - defrecord / defprotocol / deftype / defmulti / definterface
//                       → Kind="SCOPE.Component", Subtype="class"
//
// No tree-sitter grammar for Clojure is bundled in smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package clojure

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("clojure", &Extractor{})
}

// Extractor implements extractor.Extractor for Clojure.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "clojure" }

// Patterns mirror Python ClojureParser.
var (
	// (defn name [...] ...) or (defn- name ...)
	defnRE = regexp.MustCompile(
		`(?m)^\s*\(defn-?\s+([\w\-\?!\*']+)\s*(?:\[[^\]]*\]|\()`,
	)
	// Top-level type declarations.
	deftypeRE = regexp.MustCompile(
		`(?m)^\s*\((?:defrecord|defprotocol|deftype|defmulti|definterface)\s+([\w\-\?!\*']+)`,
	)
	// Require blocks.
	nsRequireBlockRE = regexp.MustCompile(
		`(?s)\(:require\s+(.*?)(?:\)\s*\(|\)\s*\))`,
	)
	requireRE = regexp.MustCompile(
		`\[\s*([\w\-\./]+)\s*(?::as\s+\w+|:refer\s+\[)`,
	)
)

// Extract processes the Clojure source and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	return extractClojure(string(file.Content), file.Path), nil
}

func extractClojure(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	imports := collectImports(src)

	// defn / defn- → functions.
	for _, m := range defnRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findFormEnd(src, m[0])

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Operation",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "clojure",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "(defn " + name + " [...])",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	// defrecord / defprotocol / deftype → class-like types.
	for _, m := range deftypeRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findFormEnd(src, m[0])

		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Component",
			Subtype:            "class",
			SourceFile:         filePath,
			Language:           "clojure",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          "(defrecord " + name + " ...)",
			EnrichmentRequired: false,
			Properties: map[string]string{
				"imports": strings.Join(imports, ","),
			},
		})
	}

	return entities
}

// collectImports gathers required namespaces from :require blocks.
func collectImports(src string) []string {
	var imports []string
	for _, bm := range nsRequireBlockRE.FindAllStringSubmatchIndex(src, -1) {
		block := src[bm[2]:bm[3]]
		for _, m := range requireRE.FindAllStringSubmatch(block, -1) {
			if len(m) > 1 {
				imports = append(imports, m[1])
			}
		}
	}
	return imports
}

// findFormEnd returns the line number of the closing ) for the Lisp form at startPos.
func findFormEnd(src string, startPos int) int {
	openParen := strings.Index(src[startPos:], "(")
	if openParen < 0 {
		return strings.Count(src[:startPos], "\n") + 1
	}
	abs := startPos + openParen
	depth := 0
	for i, ch := range src[abs:] {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.Count(src[:abs+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}
