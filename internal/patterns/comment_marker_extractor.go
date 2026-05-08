package patterns

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// commentMarkerExtractor detects TODO/FIXME/HACK/NOTE code markers.
// Matches Python comment_marker_extractor.py.
type commentMarkerExtractor struct{}

var (
	cmMarkerRE    = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK|NOTE|XXX|BUG|OPTIMIZE|REVIEW|REFACTOR)\b[:\s]*(.*?)$`)
	cmSlashLineRE = regexp.MustCompile(`//[^\n]*`)
	cmHashLineRE  = regexp.MustCompile(`#[^\n]*`)
)

func (c *commentMarkerExtractor) Category() string { return "code_marker" }

func (c *commentMarkerExtractor) AppliesTo(src string) bool {
	upper := strings.ToUpper(src)
	for _, kw := range []string{"TODO", "FIXME", "HACK", "NOTE", "XXX", "BUG"} {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

func (c *commentMarkerExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Scan all comment lines
	commentLines := make([]string, 0)
	commentOffsets := make([]int, 0)

	for _, m := range cmSlashLineRE.FindAllStringIndex(src, -1) {
		commentLines = append(commentLines, src[m[0]:m[1]])
		commentOffsets = append(commentOffsets, m[0])
	}
	for _, m := range cmHashLineRE.FindAllStringIndex(src, -1) {
		commentLines = append(commentLines, src[m[0]:m[1]])
		commentOffsets = append(commentOffsets, m[0])
	}

	for i, line := range commentLines {
		matches := cmMarkerRE.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			marker := strings.ToUpper(m[1])
			msg := strings.TrimSpace(m[2])
			lineNum := lineOf(src, commentOffsets[i])
			key := marker + ":" + filePath + ":" + strconv.Itoa(lineNum)
			if seen[key] {
				continue
			}
			seen[key] = true

			name := marker + "@" + filePath + ":" + strconv.Itoa(lineNum)
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Pattern", "code_marker", language, lineNum,
				map[string]string{"kind": "code_marker", "marker": marker, "message": msg}))
		}
	}

	return results
}

func init() {
	Register(&commentMarkerExtractor{})
}
