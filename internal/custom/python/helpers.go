// Package python provides regex-based framework extractors for Python code.
//
// Each extractor targets a specific framework (Django, FastAPI, Flask, etc.)
// and registers itself with a key like "python_django". These extractors
// complement the tree-sitter base Python extractor by capturing framework-
// specific patterns (decorators, class-based views, ORM models, etc.) that
// tree-sitter grammars do not model.
package python

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// lineOf returns the 1-indexed line number for a byte offset in source.
func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// entity builds an EntityRecord with the common fields pre-filled.
func entity(name, kind, subtype, sourceFile string, startLine int, props map[string]string) types.EntityRecord {
	return types.EntityRecord{
		Name:               name,
		Kind:               kind,
		Subtype:            subtype,
		SourceFile:         sourceFile,
		StartLine:          startLine,
		Language:           "python",
		Properties:         props,
		EnrichmentRequired: true,
	}
}

// allMatchesIndex returns all matches with their byte positions.
func allMatchesIndex(re *regexp.Regexp, source string) [][]int {
	return re.FindAllStringSubmatchIndex(source, -1)
}
