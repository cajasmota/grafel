package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// databaseIndexExtractor extracts database index definitions.
// Matches Python database_index_extractor.py.
type databaseIndexExtractor struct{}

var dbIndexActivationTokens = []string{
	"CREATE INDEX", "CREATE UNIQUE INDEX", "@Index", "@Table(indexes",
	"@javax.persistence.Index", "@jakarta.persistence.Index",
	"schema.Index", "db.Index(", "Index(",
}

var (
	dbSQLCreateIndexRE  = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+ON\s+(\w+)\s*\(([^)]+)\)`)
	dbHibernateIndexRE  = regexp.MustCompile(`@Index\s*\([^)]*\)`)
	dbHibernateNameRE   = regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)
	dbHibernateColsRE   = regexp.MustCompile(`columnList\s*=\s*["']([^"']+)["']`)
	dbSQLAlchemyIndexRE = regexp.MustCompile(`(?:schema\.)?Index\s*\(\s*["']([^"']+)["']`)
)

func (d *databaseIndexExtractor) Category() string { return "database_index" }

func (d *databaseIndexExtractor) AppliesTo(src string) bool {
	upper := strings.ToUpper(src)
	for _, tok := range dbIndexActivationTokens {
		if strings.Contains(upper, strings.ToUpper(tok)) {
			return true
		}
	}
	return false
}

func (d *databaseIndexExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	// SQL CREATE INDEX
	for idx, m := range dbSQLCreateIndexRE.FindAllStringSubmatchIndex(src, -1) {
		idxName := src[m[2]:m[3]]
		table := src[m[4]:m[5]]
		cols := src[m[6]:m[7]]
		key := fmt.Sprintf("sql:%s", idxName)
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			fmt.Sprintf("db_index_%s", idxName),
			"SCOPE.Component", "database_index", language,
			lineOf(src, m[0]),
			map[string]string{
				"kind":       "database_index",
				"index_name": idxName,
				"table_name": table,
				"columns":    cols,
				"source":     "sql",
				"index":      fmt.Sprintf("%d", idx),
			}))
	}

	// Hibernate @Index
	for idx, m := range dbHibernateIndexRE.FindAllStringSubmatchIndex(src, -1) {
		ann := src[m[0]:m[1]]
		idxName := fmt.Sprintf("idx_%d", idx)
		if nm := dbHibernateNameRE.FindStringSubmatch(ann); nm != nil {
			idxName = nm[1]
		}
		cols := ""
		if cm := dbHibernateColsRE.FindStringSubmatch(ann); cm != nil {
			cols = cm[1]
		}
		key := "hibernate:" + idxName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"db_index_"+idxName, "SCOPE.Component", "database_index", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "database_index", "index_name": idxName, "columns": cols, "source": "hibernate"}))
	}

	// SQLAlchemy Index("name", ...)
	for _, m := range dbSQLAlchemyIndexRE.FindAllStringSubmatchIndex(src, -1) {
		idxName := src[m[2]:m[3]]
		key := "sqlalchemy:" + idxName
		if seen[key] {
			continue
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"db_index_"+idxName, "SCOPE.Component", "database_index", language,
			lineOf(src, m[0]),
			map[string]string{"kind": "database_index", "index_name": idxName, "source": "sqlalchemy"}))
	}

	return results
}

func init() {
	Register(&databaseIndexExtractor{})
}
