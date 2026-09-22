// Package dart provides regex-based custom extractors for Dart source files.
// Each extractor targets a specific framework and registers via init().
package dart

import (
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

func makeEntity(name, kind, subtype, filePath, language string, lineNum int) types.EntityRecord {
	types.ValidateProducedEntityKind("internal/custom/dart.makeEntity", kind)
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

// findBraceEndByte returns the byte offset of the matching closing `}` for the
// brace at bracePos (best-effort: string/char-literal braces are not stripped).
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
	if len(src) == 0 {
		return 0
	}
	return len(src) - 1
}

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}
