package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// openAPIExtractor extracts OpenAPI/Swagger spec paths and operations.
// Matches Python openapi_extractor.py.
type openAPIExtractor struct{}

var (
	oaPathRE        = regexp.MustCompile(`(?m)^  (/[^\s:]+)\s*:`)
	oaOperationRE   = regexp.MustCompile(`(?m)^    (get|post|put|patch|delete|head|options)\s*:`)
	oaInfoTitleRE   = regexp.MustCompile(`(?m)^title\s*:\s*(.+)`)
	oaOpenAPIVerRE = regexp.MustCompile(`(?m)^openapi\s*:\s*(.+)`)
	oaSwaggerVerRE = regexp.MustCompile(`(?m)^swagger\s*:\s*(.+)`)
)

var oaActivationTokens = []string{
	"openapi:", "swagger:", "paths:", "components:", "x-openapi",
}

func (o *openAPIExtractor) Category() string { return "openapi" }

func (o *openAPIExtractor) AppliesTo(src string) bool {
	for _, tok := range oaActivationTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (o *openAPIExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Get spec version
	specVersion := ""
	if m := oaOpenAPIVerRE.FindStringSubmatch(src); m != nil {
		specVersion = strings.TrimSpace(m[1])
	} else if m := oaSwaggerVerRE.FindStringSubmatch(src); m != nil {
		specVersion = strings.TrimSpace(m[1])
	}

	// Get API title
	title := "api"
	if m := oaInfoTitleRE.FindStringSubmatch(src); m != nil {
		title = strings.TrimSpace(m[1])
	}

	// Spec-level entity
	if specVersion != "" {
		key := "spec:" + filePath
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"openapi_spec_"+title, "SCOPE.Config", "openapi_spec", language, 1,
				map[string]string{
					"kind":         "openapi",
					"title":        title,
					"spec_version": specVersion,
				}))
		}
	}

	// Extract paths
	lines := strings.Split(src, "\n")
	currentPath := ""
	for lineIdx, line := range lines {
		if m := oaPathRE.FindStringSubmatch(line); m != nil {
			currentPath = m[1]
		} else if currentPath != "" && oaOperationRE.MatchString(line) {
			if m := oaOperationRE.FindStringSubmatch(line); m != nil {
				method := m[1]
				key := fmt.Sprintf("op:%s:%s", method, currentPath)
				if !seen[key] {
					seen[key] = true
					results = append(results, makeEntity(filePath,
						fmt.Sprintf("openapi_op_%s_%s", method, strings.ReplaceAll(currentPath, "/", "_")),
						"SCOPE.Operation", "openapi_operation", language,
						lineIdx+1,
						map[string]string{
							"kind":   "openapi",
							"method": strings.ToUpper(method),
							"path":   currentPath,
						}))
				}
			}
		}
	}

	return results
}

func init() {
	Register(&openAPIExtractor{})
}
