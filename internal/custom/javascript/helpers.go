// Package javascript provides regex-based custom extractors for JavaScript and
// TypeScript source files. Each extractor targets a specific framework or
// library and supplements the YAML-driven engine rules with logic that
// requires multi-pass or context-aware regex matching.
//
// All extractors implement extractor.Extractor and register via init().
package javascript

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// lineOf returns the 1-indexed line number for a byte offset within source.
func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// makeEntity builds a minimal EntityRecord with all required fields set.
func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	e := types.EntityRecord{
		Name:             name,
		Kind:             kind,
		Subtype:          subtype,
		SourceFile:       filePath,
		StartLine:        lineNum,
		EndLine:          lineNum,
		Language:         language,
		EnrichmentStatus: types.StatusPending,
		QualityScore:     1.0,
		Properties: map[string]string{
			"kind":    kind,
			"subtype": subtype,
		},
	}
	e.ID = e.ComputeID()
	return e
}

// setProps merges extra key-value pairs into entity.Properties.
func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}

// isQuoteOrSpace returns true for characters that commonly surround string literals
// in JavaScript/TypeScript source (quotes, backtick, space, tab).
func isQuoteOrSpace(r rune) bool {
	return r == '\'' || r == '"' || r == '`' || r == ' ' || r == '\t'
}
