// Package golang provides regex-based custom extractors for Go source files.
// Each extractor targets a specific framework and registers via init().
package golang

import (
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

func lineOf(source string, offset int) int {
	return strings.Count(source[:offset], "\n") + 1
}

// itoa is a local alias for strconv.Itoa, used to build collision-resistant
// synthetic entity names that fold the source line into the name.
func itoa(n int) string { return strconv.Itoa(n) }

// submatch returns the capture-group text at submatch-index pair (g, g+1)
// from a FindAllStringSubmatchIndex match, or "" when the group did not
// participate in the match (index -1).
func submatch(src string, m []int, g int) string {
	if g+1 >= len(m) || m[g] < 0 || m[g+1] < 0 {
		return ""
	}
	return src[m[g]:m[g+1]]
}

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

func setProps(e *types.EntityRecord, kv ...string) {
	if len(kv)%2 != 0 {
		return
	}
	for i := 0; i < len(kv); i += 2 {
		e.Properties[kv[i]] = kv[i+1]
	}
}
