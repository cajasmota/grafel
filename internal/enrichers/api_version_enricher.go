package enrichers

// EnrichAPIVersion detects API version prefix in endpoint path property.
// Port of Python api_version_enricher.py (MX-610).
//
// Pattern priority (first match wins):
//  1. /api/vN/
//  2. /vN/
//  3. /api/vN$
//  4. /vN$
// Valid range: 1-99.

import (
	"regexp"
	"strconv"

	"github.com/cajasmota/archigraph/internal/types"
)

var apiVersionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/api/v(\d+)/`),
	regexp.MustCompile(`/v(\d+)/`),
	regexp.MustCompile(`/api/v(\d+)$`),
	regexp.MustCompile(`/v(\d+)$`),
}

const (
	apiVersionMin = 1
	apiVersionMax = 99
)

var endpointSubtypes = map[string]bool{
	"endpoint":  true,
	"operation": true,
}

// EnrichAPIVersion detects the API version prefix in entity path property.
// Only processes entities with Subtype "endpoint" or "operation".
func EnrichAPIVersion(entities []types.EntityRecord) []types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if !endpointSubtypes[e.Subtype] {
			continue
		}
		path, ok := e.Properties["path"]
		if !ok || path == "" {
			continue
		}
		for _, re := range apiVersionPatterns {
			m := re.FindStringSubmatch(path)
			if m == nil {
				continue
			}
			v, err := strconv.Atoi(m[1])
			if err != nil {
				break
			}
			if v >= apiVersionMin && v <= apiVersionMax {
				if e.Properties == nil {
					e.Properties = make(map[string]string)
				}
				e.Properties["api_version"] = strconv.Itoa(v)
			}
			break
		}
	}
	return entities
}
