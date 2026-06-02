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

// decoratorWindow returns the contiguous block of stacked decorator lines that
// immediately precede the byte offset `at` (the start of a route decorator
// match), plus everything from there up to `end`. It walks backwards over
// consecutive `@…` / comment / blank lines so a sibling decorator such as
// slowapi's `@limiter.limit("5/minute")` — which the route regex cannot include
// in its own match (the regex tail only permits comments before `def`) — is
// still visible to the rate-limit resolver. Used for endpoint-level throttle
// stamping (#3628 rate-limit child).
func decoratorWindow(source string, at, end int) string {
	if at < 0 || at > len(source) || end < at || end > len(source) {
		return ""
	}
	start := at
	// Walk back line-by-line while the preceding line is a decorator, comment,
	// or blank line (the conventional stacked-decorator block).
	for start > 0 {
		// Find the start of the line that ends just before `start`.
		lineEnd := start - 1 // index of the '\n' terminating the previous line
		if lineEnd < 0 || source[lineEnd] != '\n' {
			break
		}
		lineStart := strings.LastIndexByte(source[:lineEnd], '\n') + 1
		line := strings.TrimSpace(source[lineStart:lineEnd])
		if line == "" || strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			start = lineStart
			continue
		}
		break
	}
	return source[start:end]
}
