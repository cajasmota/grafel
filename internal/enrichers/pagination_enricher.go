package enrichers

// PaginationEnricher flags endpoint/operation entities with pagination metadata.
// Port of Python pagination_enricher.py (MX-611).

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

var paginationKeywords = map[string]bool{
	"page": true, "limit": true, "offset": true, "cursor": true,
	"per_page": true, "page_size": true, "skip": true, "take": true,
	"page_token": true, "next_token": true, "after": true, "before": true,
}

var keywordPatterns []*regexp.Regexp

func init() {
	for kw := range paginationKeywords {
		keywordPatterns = append(keywordPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(kw)+`\b`))
	}
}

func matchesAnyKeyword(text string) bool {
	for _, re := range keywordPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func nameContainsKeyword(name string) bool {
	var tokens []string
	current := ""
	runes := []rune(name)
	for i, ch := range runes {
		if ch == '_' || ch == '-' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
			continue
		}
		if i > 0 && ch >= 'A' && ch <= 'Z' && runes[i-1] >= 'a' && runes[i-1] <= 'z' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		}
		if i > 0 && ch >= 'A' && ch <= 'Z' && runes[i-1] >= 'A' && runes[i-1] <= 'Z' &&
			i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		}
		current += string(ch)
	}
	if current != "" {
		tokens = append(tokens, current)
	}
	for _, token := range tokens {
		if paginationKeywords[strings.ToLower(token)] {
			return true
		}
	}
	return false
}

// EnrichPagination detects pagination parameters on endpoint/operation entities.
func EnrichPagination(entities []types.EntityRecord) []types.EntityRecord {
	for i := range entities {
		e := &entities[i]
		if !endpointSubtypes[e.Subtype] {
			continue
		}
		enrichSinglePagination(e)
	}
	return entities
}

func enrichSinglePagination(e *types.EntityRecord) {
	if params, ok := e.Properties["parameters"]; ok && params != "" {
		for _, param := range strings.Split(params, ",") {
			if paginationKeywords[strings.ToLower(strings.TrimSpace(param))] {
				setPaginated(e)
				return
			}
		}
	}
	if schema, ok := e.Properties["parameter_schema"]; ok && schema != "" {
		if matchesAnyKeyword(schema) {
			setPaginated(e)
			return
		}
	}
	if nameContainsKeyword(e.Name) {
		setPaginated(e)
	}
}

func setPaginated(e *types.EntityRecord) {
	if e.Properties == nil {
		e.Properties = make(map[string]string)
	}
	e.Properties["paginated"] = "true"
}
