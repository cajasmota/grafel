package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// sqlJoinCountExtractor counts JOIN complexity in SQL queries.
// Matches Python sql_join_count_extractor.py.
type sqlJoinCountExtractor struct{}

var (
	sqjSQLStringRE = regexp.MustCompile(`(?i)["'\x60]([^"'\x60]*(?:SELECT|INSERT|UPDATE|DELETE)[^"'\x60]*)["'\x60]`)
	sqjJoinRE      = regexp.MustCompile(`(?i)\bJOIN\b`)
)

func (s *sqlJoinCountExtractor) Category() string { return "sql_join_count" }

func (s *sqlJoinCountExtractor) AppliesTo(src string) bool {
	upper := strings.ToUpper(src)
	return strings.Contains(upper, "JOIN") && strings.Contains(upper, "SELECT")
}

func (s *sqlJoinCountExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// Find SQL string literals with JOINs
	for idx, m := range sqjSQLStringRE.FindAllStringSubmatchIndex(src, -1) {
		query := src[m[2]:m[3]]
		joinCount := len(sqjJoinRE.FindAllString(query, -1))
		if joinCount == 0 {
			continue
		}

		complexity := "low"
		if joinCount >= 5 {
			complexity = "high"
		} else if joinCount >= 3 {
			complexity = "medium"
		}

		key := fmt.Sprintf("join:%d:%d", lineOf(src, m[0]), idx)
		if seen[key] {
			continue
		}
		seen[key] = true

		snippet := query
		if len(snippet) > 100 {
			snippet = snippet[:100]
		}
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("sql_join_count_%d", idx),
			"SCOPE.Pattern", "sql_join_count", language,
			lineOf(src, m[0]),
			map[string]string{
				"kind":          "sql_join_count",
				"join_count":    fmt.Sprintf("%d", joinCount),
				"complexity":    complexity,
				"query_snippet": snippet,
			}))
	}

	return results
}

func init() {
	Register(&sqlJoinCountExtractor{})
}
