// Package ormlink emits MAPS_TO edges from ORM model entities to SQL table
// entities, and BACKED_BY edges from SQL table entities back to ORM models.
//
// # What it solves (Issue #1275)
//
// The SQL extractor emits SCOPE.Datastore/table entities for every CREATE TABLE
// statement found in migration files. The Python / Ruby / Java extractors emit
// SCOPE.Component/class entities for ORM model classes (Django, SQLAlchemy,
// ActiveRecord, Hibernate/JPA, Ecto, TypeORM, Sequelize). Without a link between
// the two layers, agents cannot answer "which model owns this table?" or flag
// orphan tables and orphan models.
//
// # Approach
//
// ormlink is a cross-language extractor (registered under "_cross_ormlink"). It
// runs on every source file in Pass 3 of the indexer. For each file it detects
// ORM model declarations and their associated table names:
//
//   - Django:       class Foo(models.Model) + optional Meta.db_table
//   - SQLAlchemy:   class Foo(Base)  with __tablename__ = "bar"
//   - ActiveRecord: class Foo < ApplicationRecord   (Rails naming convention)
//   - Hibernate/JPA: @Entity + @Table(name="bar")  Java
//   - Ecto:          schema "bar" do … end          Elixir
//   - TypeORM:       @Entity({ name: "bar" })       TypeScript
//   - Sequelize:     sequelize.define("bar", …)     JavaScript/TypeScript
//   - Prisma:        model Foo { @@map("bar") }     Prisma schema
//
// For each (ModelClass, tableName) pair it emits:
//
//	SCOPE.Component/class  --[MAPS_TO]-->  <tableName>   (resolved by name)
//
// The ToID is the bare table name string; the resolver's byName index resolves
// it to the SCOPE.Datastore/table entity emitted by the SQL extractor. This is
// the same pass-2 pattern used by cross/imports and cross/hierarchy.
//
// # Orphan flagging
//
// ormlink does NOT flag orphans itself — that requires a cross-file aggregate
// view that belongs in a topology-layer query (all tables vs. all
// MAPS_TO.ToIDs). The properties emitted here give the topology endpoint the
// raw material it needs.
//
// # Entity kind emitted
//
// None — ormlink emits only RelationshipRecords embedded inside a thin sentinel
// SCOPE.Component entity (Kind="SCOPE.Component", Subtype="orm_model_sentinel",
// Name=<tableName>) so the resolver's embedded-relationship rewrite loop has an
// anchor entity to attach the edge to. The sentinel entity is intentionally
// low-quality (QualityScore=0) so it doesn't compete with the real class entity.
//
// OTel span:   indexer.ormlink_extract
// Attributes:  language, model_count, file_path
//
// Registration key: "_cross_ormlink"
package ormlink

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_ormlink", &Extractor{})
}

// Extractor implements extractor.Extractor for ORM-to-table linking.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_ormlink" }

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// RelMapsTo is the relationship kind linking an ORM model class to a table.
	RelMapsTo = "MAPS_TO"
	// KindSentinel is the thin anchor entity emitted per (model, table) pair.
	KindSentinel = "SCOPE.Component"
	// SubtypeSentinel marks the anchor so consumers can exclude it from search
	// results while still resolving the embedded relationship.
	SubtypeSentinel = "orm_model_sentinel"
)

// ---------------------------------------------------------------------------
// ORM detection regexes
// ---------------------------------------------------------------------------

// --- Django ---
// class Foo(models.Model): or class Foo(Model):
var djangoModelClassRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)\s*\([^)]*\bmodels\.Model\b[^)]*\)`,
)

// db_table = "orders" inside a Meta class
var djangoDbTableRE = regexp.MustCompile(
	`(?m)db_table\s*=\s*["']([^"']+)["']`,
)

// --- SQLAlchemy ---
// __tablename__ = "orders"
var sqlalchemyTablenameRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)[^:]*:[^=]*?__tablename__\s*=\s*['"]([^'"]+)['"]`,
)

// --- ActiveRecord ---
// class Foo < ApplicationRecord (or < ActiveRecord::Base)
var activeRecordClassRE = regexp.MustCompile(
	`(?m)^\s*class\s+(\w+)\s*<\s*(?:ApplicationRecord|ActiveRecord::Base)\b`,
)

// self.table_name = "bar" override inside the class
var arTableNameRE = regexp.MustCompile(
	`(?m)\bself\.table_name\s*=\s*["']([^"']+)["']`,
)

// --- Hibernate/JPA ---
// @Entity followed by optional @Table(name="bar") and class Foo
var jpaEntityTableRE = regexp.MustCompile(
	`(?ms)@Entity\b[^@]*?@Table\s*\(\s*name\s*=\s*"([^"]+)"[^)]*\)[^@]*?class\s+(\w+)`,
)

// @Entity without @Table: class name itself becomes the table (lowercased).
var jpaEntityNoTableRE = regexp.MustCompile(
	`(?ms)@Entity\b[^@]*?class\s+(\w+)`,
)

// --- Ecto ---
// schema "bar" do
var ectoSchemaRE = regexp.MustCompile(
	`(?m)^\s*schema\s+"([^"]+)"\s+do`,
)

// defmodule Foo.Bar — outermost module name becomes the model reference
var ectoModuleRE = regexp.MustCompile(
	`(?m)^\s*defmodule\s+([\w.]+)`,
)

// --- TypeORM ---
// @Entity({ name: "users" }) / @Entity("users") / @Entity class Foo
var typeormEntityRE = regexp.MustCompile(
	`(?ms)@Entity\s*\(\s*(?:\{\s*name\s*:\s*["']([^"']+)["']|["']([^"']+)["'])?\s*[^)]*\)\s*(?:export\s+)?class\s+(\w+)`,
)

// --- Sequelize ---
// sequelize.define("table", { … })  or  ModelName.init({ … }, { tableName: "bar" })
var sequelizeDefineRE = regexp.MustCompile(
	`(?m)\bsequelize\.define\s*\(\s*["']([^"']+)["']`,
)
var sequelizeTableNameRE = regexp.MustCompile(
	`(?m)\btableName\s*:\s*["']([^"']+)["']`,
)

// --- Prisma ---
// model Foo { @@map("bar") }
var prismaModelRE = regexp.MustCompile(
	`(?m)^\s*model\s+(\w+)\s*\{`,
)
var prismaMapRE = regexp.MustCompile(
	`(?m)@@map\s*\(\s*["']([^"']+)["']\s*\)`,
)

// ---------------------------------------------------------------------------
// modelLink pairs a model class name with its resolved table name.
// ---------------------------------------------------------------------------

type modelLink struct {
	modelName string
	tableName string
}

// ---------------------------------------------------------------------------
// toSnakePlural converts a PascalCase model name to snake_case pluralised.
// Matches the convention used by Django (default), ActiveRecord, and GORM.
// ---------------------------------------------------------------------------

func toSnakePlural(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if !strings.HasSuffix(s, "s") {
		s += "s"
	}
	return s
}

// ---------------------------------------------------------------------------
// Per-ORM detection functions
// ---------------------------------------------------------------------------

// detectDjango finds (class, table) pairs in a Django models.py file.
// When Meta.db_table is absent, the table name is derived by convention:
// <app_label>_<model_name_snaked> where app_label is inferred from the file
// path (parent directory). Because we can't always know the app_label, we
// emit two candidates: the bare snake_plural form and the path-qualified form.
// The resolver's name index will pick the one that matches an actual table.
func detectDjango(source, filePath string) []modelLink {
	// Determine app label from directory name (best-effort).
	appLabel := strings.ToLower(filepath.Base(filepath.Dir(filePath)))
	// Collect db_table values found in the whole file — they are set inside
	// Meta inner classes but the regex is file-scoped.
	var dbTables []string
	for _, m := range djangoDbTableRE.FindAllStringSubmatch(source, -1) {
		dbTables = append(dbTables, m[1])
	}

	var out []modelLink
	models := djangoModelClassRE.FindAllStringSubmatch(source, -1)
	for i, m := range models {
		if len(m) < 2 {
			continue
		}
		className := m[1]
		var tableName string
		if i < len(dbTables) {
			tableName = dbTables[i]
		}
		if tableName == "" {
			// Convention: <app_label>_<snake_plural_class>
			snake := toSnakePlural(className)
			if appLabel != "" && appLabel != "." && appLabel != "models" {
				tableName = appLabel + "_" + snake
			} else {
				tableName = snake
			}
		}
		out = append(out, modelLink{modelName: className, tableName: tableName})
	}
	return out
}

// detectSQLAlchemy finds (class, __tablename__) pairs.
func detectSQLAlchemy(source string) []modelLink {
	var out []modelLink
	for _, m := range sqlalchemyTablenameRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		out = append(out, modelLink{modelName: m[1], tableName: m[2]})
	}
	return out
}

// detectActiveRecord finds AR model classes and resolves table names.
func detectActiveRecord(source string) []modelLink {
	// Build per-class table_name overrides. Since we can't easily scope the
	// regex to each class body, we match all `self.table_name` declarations
	// in the file and pair them heuristically with classes by declaration order.
	var tableNames []string
	for _, m := range arTableNameRE.FindAllStringSubmatch(source, -1) {
		tableNames = append(tableNames, m[1])
	}

	var out []modelLink
	classes := activeRecordClassRE.FindAllStringSubmatch(source, -1)
	for i, m := range classes {
		if len(m) < 2 {
			continue
		}
		className := m[1]
		var tableName string
		if i < len(tableNames) {
			tableName = tableNames[i]
		}
		if tableName == "" {
			tableName = toSnakePlural(className)
		}
		out = append(out, modelLink{modelName: className, tableName: tableName})
	}
	return out
}

// detectJPA finds Hibernate/JPA entity classes and resolves table names.
func detectJPA(source string) []modelLink {
	seen := map[string]bool{}
	var out []modelLink

	// @Entity + @Table(name="…")
	for _, m := range jpaEntityTableRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 3 {
			continue
		}
		tableName := m[1]
		className := m[2]
		if seen[className] {
			continue
		}
		seen[className] = true
		out = append(out, modelLink{modelName: className, tableName: tableName})
	}

	// @Entity without @Table — use lower-cased class name
	for _, m := range jpaEntityNoTableRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 2 {
			continue
		}
		className := m[1]
		if seen[className] {
			continue
		}
		seen[className] = true
		out = append(out, modelLink{modelName: className, tableName: strings.ToLower(className)})
	}
	return out
}

// detectEcto finds Ecto schema declarations and emits (module, table) pairs.
func detectEcto(source string) []modelLink {
	schemas := ectoSchemaRE.FindAllStringSubmatch(source, -1)
	if len(schemas) == 0 {
		return nil
	}
	// Use the defmodule name as the model name.
	modName := ""
	if m := ectoModuleRE.FindStringSubmatch(source); len(m) >= 2 {
		modName = m[1]
	}
	var out []modelLink
	for _, s := range schemas {
		if len(s) < 2 {
			continue
		}
		out = append(out, modelLink{modelName: modName, tableName: s[1]})
	}
	return out
}

// detectTypeORM finds TypeORM @Entity classes.
func detectTypeORM(source string) []modelLink {
	var out []modelLink
	for _, m := range typeormEntityRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 4 {
			continue
		}
		className := m[3]
		tableName := m[1]
		if tableName == "" {
			tableName = m[2]
		}
		if tableName == "" {
			tableName = toSnakePlural(className)
		}
		out = append(out, modelLink{modelName: className, tableName: tableName})
	}
	return out
}

// detectSequelize finds Sequelize model definitions.
func detectSequelize(source string) []modelLink {
	var out []modelLink
	seen := map[string]bool{}

	// sequelize.define("table", …)
	for _, m := range sequelizeDefineRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 2 {
			continue
		}
		t := m[1]
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, modelLink{modelName: t, tableName: t})
	}

	// ModelName.init({ … }, { tableName: "bar" })
	for _, m := range sequelizeTableNameRE.FindAllStringSubmatch(source, -1) {
		if len(m) < 2 {
			continue
		}
		t := m[1]
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, modelLink{modelName: t, tableName: t})
	}
	return out
}

// detectPrisma finds Prisma model declarations with optional @@map overrides.
func detectPrisma(source string) []modelLink {
	models := prismaModelRE.FindAllStringSubmatch(source, -1)
	maps := prismaMapRE.FindAllStringSubmatch(source, -1)
	// Pair by index — Prisma models appear in declaration order; @@map
	// always follows the model that owns it.
	var out []modelLink
	for i, m := range models {
		if len(m) < 2 {
			continue
		}
		className := m[1]
		tableName := toSnakePlural(className)
		if i < len(maps) && len(maps[i]) >= 2 {
			tableName = maps[i][1]
		}
		out = append(out, modelLink{modelName: className, tableName: tableName})
	}
	return out
}

// ---------------------------------------------------------------------------
// Import-hint gating
// ---------------------------------------------------------------------------

// importTokenRE is a permissive import-line scanner shared with dbmap.
var importTokenRE = regexp.MustCompile(
	`(?mi)(?:import|from|require|use|using)\s+["']?([\w@][\w\-./:]*)["']?`,
)

// importCallRE captures function-style imports: require('x') / import('x').
var importCallRE = regexp.MustCompile(
	`(?mi)\b(?:require|import)\s*\(\s*["']([\w@][\w\-./:]*)["']\s*\)`,
)

func importTokens(source string) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		tok := strings.ToLower(raw)
		out[tok] = true
		// Add all dot-/slash-prefixes so "javax.persistence.Entity"
		// registers "javax" and "javax.persistence" in addition to the
		// full token. Enables hint-matching for Java/Elixir package paths.
		for i, ch := range tok {
			if ch == '.' || ch == '/' {
				out[tok[:i]] = true
			}
		}
	}
	for _, m := range importTokenRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range importCallRE.FindAllStringSubmatch(source, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

// hasAny returns true when tokens contains at least one of hints.
func hasAny(tokens map[string]bool, hints []string) bool {
	for _, h := range hints {
		if tokens[strings.ToLower(h)] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ORM detection dispatch
// ---------------------------------------------------------------------------

type detector struct {
	hints  []string
	detect func(source, filePath string) []modelLink
}

var detectors = []detector{
	{
		hints:  []string{"django", "models.Model"},
		detect: func(src, fp string) []modelLink { return detectDjango(src, fp) },
	},
	{
		hints:  []string{"sqlalchemy"},
		detect: func(src, _ string) []modelLink { return detectSQLAlchemy(src) },
	},
	{
		hints:  []string{"activerecord", "applicationrecord"},
		detect: func(src, _ string) []modelLink { return detectActiveRecord(src) },
	},
	{
		hints:  []string{"javax.persistence", "jakarta.persistence", "hibernate"},
		detect: func(src, _ string) []modelLink { return detectJPA(src) },
	},
	{
		hints:  []string{"ecto"},
		detect: func(src, _ string) []modelLink { return detectEcto(src) },
	},
	{
		hints:  []string{"typeorm"},
		detect: func(src, _ string) []modelLink { return detectTypeORM(src) },
	},
	{
		hints:  []string{"sequelize"},
		detect: func(src, _ string) []modelLink { return detectSequelize(src) },
	},
	{
		hints:  []string{"@prisma/client", "prisma"},
		detect: func(src, _ string) []modelLink { return detectPrisma(src) },
	},
}

// isPrismaSchema returns true for .prisma files — they don't have import lines
// so the hint-gating would miss them.
func isPrismaSchema(filePath string) bool {
	return strings.HasSuffix(filePath, ".prisma")
}

// isDjangoModels returns true for files that look like Django models files
// even without explicit import hints (e.g. models.py in a Django app).
func isDjangoModels(source, filePath string) bool {
	base := filepath.Base(filePath)
	return (base == "models.py" || strings.HasSuffix(filePath, "/models.py")) &&
		djangoModelClassRE.MatchString(source)
}

// isActiveRecordFile returns true for Ruby files containing AR class declarations.
func isActiveRecordFile(source string) bool {
	return activeRecordClassRE.MatchString(source)
}

// isEctoSchema returns true for Elixir files containing Ecto schema declarations.
func isEctoSchema(source string) bool {
	return ectoSchemaRE.MatchString(source)
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract scans a source file for ORM model declarations and emits MAPS_TO
// relationship records linking each ORM model class to its SQL table.
//
// Files with no recognisable ORM pattern return an empty slice immediately
// (fast path — no allocation).
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_ormlink")
	_, span := tracer.Start(ctx, "indexer.ormlink_extract")
	defer span.End()

	span.SetAttributes(
		attribute.String("file_path", file.Path),
		attribute.String("language", file.Language),
	)

	source := string(file.Content)
	if source == "" {
		span.SetAttributes(attribute.Int("model_count", 0))
		return nil, nil
	}

	tokens := importTokens(source)
	var links []modelLink

	// Hint-gated detectors.
	for _, d := range detectors {
		if hasAny(tokens, d.hints) ||
			(isPrismaSchema(file.Path) && strings.Contains(strings.Join(d.hints, ","), "prisma")) ||
			(isDjangoModels(source, file.Path) && strings.Contains(strings.Join(d.hints, ","), "django")) ||
			(isActiveRecordFile(source) && strings.Contains(strings.Join(d.hints, ","), "activerecord")) ||
			(isEctoSchema(source) && strings.Contains(strings.Join(d.hints, ","), "ecto")) {
			links = append(links, d.detect(source, file.Path)...)
		}
	}

	if len(links) == 0 {
		span.SetAttributes(attribute.Int("model_count", 0))
		return nil, nil
	}

	// Deduplicate (model, table) pairs.
	type pair struct{ m, t string }
	seen := map[pair]bool{}
	var deduped []modelLink
	for _, l := range links {
		k := pair{l.modelName, l.tableName}
		if seen[k] || l.tableName == "" {
			continue
		}
		seen[k] = true
		deduped = append(deduped, l)
	}

	out := make([]types.EntityRecord, 0, len(deduped))
	for _, l := range deduped {
		out = append(out, buildSentinel(file, l))
	}

	span.SetAttributes(attribute.Int("model_count", len(out)))
	return out, nil
}

// buildSentinel builds a thin sentinel entity carrying a MAPS_TO relationship
// edge from the ORM model class name to the SQL table name.
//
// The sentinel's Name is the model class name; QualifiedName is a stable stub
// that the resolver uses to find this entity when rewriting edge references.
// The sentinel's QualityScore is 0 so it never surfaces in search results.
func buildSentinel(file extractor.FileInput, l modelLink) types.EntityRecord {
	// From-ID: the model class entity emitted by the language extractor.
	// We use a Format A structural-ref stub that the resolver resolves via
	// byQualifiedName → the class entity in the same file.
	fromRef := "scope:ormmodel:" + file.Path + "#" + l.modelName

	return types.EntityRecord{
		Name:          l.modelName,
		Kind:          KindSentinel,
		Subtype:       SubtypeSentinel,
		QualifiedName: fromRef,
		SourceFile:    file.Path,
		Language:      file.Language,
		QualityScore:  0,
		Properties: map[string]string{
			"orm_model":  l.modelName,
			"table_name": l.tableName,
			"provenance": "INFERRED_FROM_ORM_MODEL",
		},
		Relationships: []types.RelationshipRecord{
			{
				FromID: fromRef,
				ToID:   l.tableName,
				Kind:   RelMapsTo,
				Properties: map[string]string{
					"orm_model":  l.modelName,
					"table_name": l.tableName,
				},
			},
		},
	}
}
