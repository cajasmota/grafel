package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// rawSQLExtractor extracts raw SQL query patterns.
// Matches Python raw_sql_extractor.py.
type rawSQLExtractor struct{}

var rawSQLTriggerTokens = []string{
	"SELECT ", "INSERT INTO", "UPDATE ", "DELETE FROM", "CREATE TABLE",
	"cursor.execute", "db.execute", "conn.execute", ".rawQuery(", "QueryRaw(",
}

var (
	rawSelectRE     = regexp.MustCompile(`(?i)\bSELECT\b.*?\bFROM\b\s+(\w+)`)
	rawInsertRE     = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(\w+)`)
	rawUpdateRE     = regexp.MustCompile(`(?i)\bUPDATE\s+(\w+)\s+SET\b`)
	rawDeleteRE     = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+(\w+)`)
)

func (r *rawSQLExtractor) Category() string { return "raw_sql" }

func (r *rawSQLExtractor) AppliesTo(src string) bool {
	upper := strings.ToUpper(src)
	for _, tok := range rawSQLTriggerTokens {
		if strings.Contains(upper, strings.ToUpper(tok)) {
			return true
		}
	}
	return false
}

func (r *rawSQLExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	ops := []struct {
		re  *regexp.Regexp
		op  string
	}{
		{rawSelectRE, "SELECT"},
		{rawInsertRE, "INSERT"},
		{rawUpdateRE, "UPDATE"},
		{rawDeleteRE, "DELETE"},
	}

	for _, op := range ops {
		for idx, m := range op.re.FindAllStringSubmatchIndex(src, -1) {
			table := ""
			if m[2] >= 0 {
				table = src[m[2]:m[3]]
			}
			key := fmt.Sprintf("%s:%s:%d", op.op, table, idx)
			if seen[key] {
				continue
			}
			seen[key] = true
			snippet := src[m[0]:m[1]]
			if len(snippet) > 100 {
				snippet = snippet[:100]
			}
			results = append(results, makeEntity(filePath,
				fmt.Sprintf("raw_sql_%s_%s_%d", op.op, table, idx),
				"SCOPE.Operation", "raw_sql", language,
				lineOf(src, m[0]),
				map[string]string{
					"kind":          "raw_sql",
					"operation":     op.op,
					"table":         table,
					"query_snippet": snippet,
				}))
			if idx >= 19 { // cap
				break
			}
		}
	}

	return results
}

func init() {
	Register(&rawSQLExtractor{})
}
