package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// columnSchemaExtractor extracts ORM column/field schema definitions.
// Matches Python column_schema_extractor.py.
type columnSchemaExtractor struct{}

var columnOrmTokenSets = map[string][]string{
	"hibernate":   {"@Column", "@Entity", "@Table", "@Id", "@GeneratedValue"},
	"sqlalchemy":  {"Column(", "db.Column(", "mapped_column(", "relationship("},
	"prisma":      {"model ", "@@map", "@default", "@unique", "@relation"},
	"typeorm":     {"@Column(", "@Entity(", "@PrimaryColumn(", "@ManyToOne(", "@OneToMany("},
	"efcore":      {"HasColumn(", "Property(", "HasKey(", "HasIndex("},
	"eloquent":    {"$fillable", "$casts", "$table", "protected $"},
	"ecto":        {"field :", "belongs_to :", "has_many :", "cast("},
}

var columnHibernateRE   = regexp.MustCompile(`@Column\s*\(\s*(?:name\s*=\s*["']([^"']+)["'])?`)
var columnSQLAlchemyRE  = regexp.MustCompile(`(\w+)\s*=\s*(?:db\.)?Column\s*\(([^)]+)\)`)
var columnPrismaFieldRE = regexp.MustCompile(`(?m)^\s+(\w+)\s+(\w+)(?:\?|!)?(?:\s+@\w+.*)?$`)

func detectColumnORM(src string) string {
	for orm, tokens := range columnOrmTokenSets {
		for _, tok := range tokens {
			if strings.Contains(src, tok) {
				return orm
			}
		}
	}
	return ""
}

func (c *columnSchemaExtractor) Category() string { return "column_schema" }

func (c *columnSchemaExtractor) AppliesTo(src string) bool {
	return detectColumnORM(src) != ""
}

func (c *columnSchemaExtractor) Detect(filePath, language, src string) []types.EntityRecord {
	orm := detectColumnORM(src)
	if orm == "" {
		return nil
	}

	var results []types.EntityRecord
	seen := map[string]bool{}

	switch orm {
	case "hibernate":
		for idx, m := range columnHibernateRE.FindAllStringSubmatchIndex(src, -1) {
			colName := fmt.Sprintf("col_%d", idx)
			if m[2] >= 0 {
				colName = src[m[2]:m[3]]
			}
			key := "hibernate:" + colName
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"column_"+colName, "SCOPE.Component", "column_schema", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "column_schema", "orm": "hibernate", "column_name": colName}))
		}
	case "sqlalchemy":
		for _, m := range columnSQLAlchemyRE.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			colDef := src[m[4]:m[5]]
			key := "sqlalchemy:" + fieldName
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"column_"+fieldName, "SCOPE.Component", "column_schema", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "column_schema", "orm": "sqlalchemy", "column_name": fieldName, "column_def": colDef}))
		}
	case "prisma":
		for _, m := range columnPrismaFieldRE.FindAllStringSubmatchIndex(src, -1) {
			fieldName := src[m[2]:m[3]]
			fieldType := src[m[4]:m[5]]
			key := "prisma:" + fieldName
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				"column_"+fieldName, "SCOPE.Component", "column_schema", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "column_schema", "orm": "prisma", "column_name": fieldName, "column_type": fieldType}))
		}
	default:
		results = append(results, makeEntity(filePath,
			"column_schema_"+orm, "SCOPE.Component", "column_schema", language, 1,
			map[string]string{"kind": "column_schema", "orm": orm}))
	}

	return results
}

func init() {
	Register(&columnSchemaExtractor{})
}
