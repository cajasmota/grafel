package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ormDetector detects ORM usage patterns.
// Matches Python orm_detector.py.
type ormDetector struct{}

var ormDetectionMap = map[string][]string{
	"sqlalchemy":  {"from sqlalchemy", "import sqlalchemy", "db.Model", "Base = declarative_base"},
	"django_orm":  {"from django.db import models", "models.Model", "models.CharField"},
	"peewee":      {"from peewee import", "peewee.Model"},
	"tortoise":    {"from tortoise", "tortoise.models"},
	"hibernate":   {"@Entity", "@MappedSuperclass", "javax.persistence", "jakarta.persistence"},
	"jpa":         {"EntityManager", "@PersistenceContext", "TypedQuery"},
	"typeorm":     {"@Entity()", "Repository<", "getRepository(", "@Column("},
	"sequelize":   {"Sequelize.", "DataTypes.", "sequelize.define("},
	"prisma":      {"from '@prisma/client'", "PrismaClient()", "$connect()"},
	"gorm":        {"gorm.DB", "gorm.Open(", `"gorm.io/gorm"`},
	"ecto":        {"Ecto.Schema", "use Ecto.Schema", "schema do", "Repo."},
	"active_record": {"ActiveRecord::Base", "has_many :", "belongs_to :"},
	"eloquent":    {"extends Model", "Eloquent", "$table =", "protected $fillable"},
}

var ormQueryREs = map[string]*regexp.Regexp{
	"sqlalchemy":  regexp.MustCompile(`(?:session|db)\s*\.\s*(?:query|execute|add|commit|delete)\s*\(`),
	"gorm":        regexp.MustCompile(`db\s*\.\s*(?:Find|First|Create|Save|Delete|Where)\s*\(`),
	"typeorm":     regexp.MustCompile(`repository\s*\.\s*(?:find|findOne|save|delete|create)\s*\(`),
	"sequelize":   regexp.MustCompile(`\w+\s*\.\s*(?:findAll|findOne|create|update|destroy)\s*\(`),
}

func detectORMFramework(src string) string {
	for orm, tokens := range ormDetectionMap {
		for _, tok := range tokens {
			if strings.Contains(src, tok) {
				return orm
			}
		}
	}
	return ""
}

func (o *ormDetector) Category() string { return "orm" }

func (o *ormDetector) AppliesTo(src string) bool {
	return detectORMFramework(src) != ""
}

func (o *ormDetector) Detect(filePath, language, src string) []types.EntityRecord {
	// Dart uses generic type syntax (Repository<T>) that can trigger ORM token
	// matching spuriously. Python does not emit ORM entities for Dart.
	switch language {
	case "dart", "shell", "proto", "protobuf":
		return nil
	}

	orm := detectORMFramework(src)
	if orm == "" {
		return nil
	}

	var results []types.EntityRecord
	seen := map[string]bool{}

	key := "orm:" + orm
	if !seen[key] {
		seen[key] = true
		results = append(results, makeEntity(filePath,
			"orm_usage_"+orm, "SCOPE.Component", "orm", language, 1,
			map[string]string{"kind": "orm", "orm": orm}))
	}

	// Detect query operations
	if qre, ok := ormQueryREs[orm]; ok {
		for idx, m := range qre.FindAllStringIndex(src, -1) {
			opKey := orm + ":query:" + string(rune('0'+idx))
			if seen[opKey] {
				continue
			}
			seen[opKey] = true
			results = append(results, makeEntity(filePath,
				"orm_query_"+orm, "SCOPE.Operation", "orm_query", language,
				lineOf(src, m[0]),
				map[string]string{"kind": "orm", "orm": orm, "operation": "query"}))
			if idx >= 9 { // cap at 10
				break
			}
		}
	}

	return results
}

func init() {
	Register(&ormDetector{})
}
