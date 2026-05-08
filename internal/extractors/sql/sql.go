// Package sql implements the tree-sitter–based extractor for SQL source files.
//
// Extracted entities:
//   - CREATE TABLE  → Kind="SCOPE.Datastore", Subtype="table"
//   - CREATE VIEW   → Kind="SCOPE.Datastore", Subtype="view"
//   - CREATE INDEX  → Kind="SCOPE.Datastore", Subtype="index"
//   - dbt {{ ref('model') }}    → Kind="SCOPE.Component",  Subtype="dbt_ref"
//   - dbt {{ source('s','t') }} → Kind="SCOPE.Datastore",  Subtype="dbt_source"
//   - dbt {{ config(...) }}     → Kind="SCOPE.Component",  Subtype="dbt_config"
//
// dbt model files are SQL files containing Jinja templating. They are classified
// as "sql" by the file classifier and receive enhanced entity extraction here.
//
// Uses the sql grammar from smacker/go-tree-sitter.
// Falls back to regex extraction when no tree is available.
// Registers itself via init() and is imported by registry_gen.go.
package sql

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("sql", &Extractor{})
}

// Extractor implements extractor.Extractor for SQL.
type Extractor struct{}

// Language returns the canonical language name.
func (e *Extractor) Language() string { return "sql" }

// Regex patterns — mirrors Python SqlParser regexes.
var (
	tableRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)\s*\(`,
	)
	viewRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:MATERIALIZED\s+)?VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)\s+AS\b`,
	)
	indexRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:UNIQUE\s+)?(?:CONCURRENTLY\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+ON\s+(?:\w+\.)?(\w+)`,
	)
	funcRE = regexp.MustCompile(
		`(?i)(?m)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:AGGREGATE\s+|PROCEDURE\s+|FUNCTION\s+)(\w+)\s*\(`,
	)

	// dbt Jinja patterns.

	// {{ ref('model_name') }} or {{ ref("model_name") }}
	dbtRefRE = regexp.MustCompile(`\{\{-?\s*ref\s*\(\s*['"](\w+)['"]\s*\)\s*-?\}\}`)

	// {{ source('source_name', 'table_name') }} or double-quoted variants
	dbtSourceRE = regexp.MustCompile(`\{\{-?\s*source\s*\(\s*['"](\w+)['"]\s*,\s*['"](\w+)['"]\s*\)\s*-?\}\}`)

	// {{ config(materialized='table', ...) }} — capture first key=value or keyword arg
	dbtConfigRE = regexp.MustCompile(`\{\{-?\s*config\s*\(([^)]+)\)\s*-?\}\}`)

	// config key=value pairs inside {{ config(...) }}
	dbtConfigKeyRE = regexp.MustCompile(`(\w+)\s*=`)
)

// Extract uses regex-based extraction (tree-sitter SQL grammar node names vary widely
// by dialect; regex gives parity with the Python SqlParser).
//
// dbt model detection: if the file contains Jinja template markers ({{ ref(...)}}
// or {{ source(...) }} or {{ config(...) }}), dbt-specific entities are emitted in
// addition to any standard SQL entities.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	entities := extractSQL(src, file.Path)
	if isDbtModel(src) {
		entities = append(entities, extractDbt(src, file.Path)...)
	}
	return entities, nil
}

// isDbtModel returns true when the SQL file contains Jinja template markers
// characteristic of dbt model files.
func isDbtModel(src string) bool {
	return dbtRefRE.MatchString(src) || dbtSourceRE.MatchString(src) || dbtConfigRE.MatchString(src)
}

func extractSQL(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// Tables.
	for _, m := range tableRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "table:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "table",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE TABLE %s", name),
			EnrichmentRequired: false,
		})
	}

	// Views.
	for _, m := range viewRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "view:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findStmtEnd(src, m[0])
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "view",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE VIEW %s", name),
			EnrichmentRequired: false,
		})
	}

	// Indexes.
	for _, m := range indexRE.FindAllStringSubmatchIndex(src, -1) {
		indexName := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		key := "index:" + indexName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findStmtEnd(src, m[0])
		entities = append(entities, types.EntityRecord{
			Name:               indexName,
			Kind:               "SCOPE.Datastore",
			Subtype:            "index",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE INDEX %s ON %s", indexName, tableName),
			EnrichmentRequired: false,
		})
	}

	// Functions / Procedures.
	for _, m := range funcRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		key := "function:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		endLine := findBlockEnd(src, m[1]-1)
		entities = append(entities, types.EntityRecord{
			Name:               name,
			Kind:               "SCOPE.Datastore",
			Subtype:            "function",
			SourceFile:         filePath,
			Language:           "sql",
			StartLine:          startLine,
			EndLine:            endLine,
			Signature:          fmt.Sprintf("CREATE FUNCTION %s", name),
			EnrichmentRequired: false,
		})
	}

	return entities
}

// extractDbt extracts dbt-specific entities from a SQL file containing Jinja.
//
// Emitted entity kinds:
//   - {{ ref('model') }}          → SCOPE.Component / dbt_ref
//   - {{ source('src','tbl') }}   → SCOPE.Datastore / dbt_source
//   - {{ config(key=val, ...) }}  → SCOPE.Component / dbt_config (one per config key)
func extractDbt(src, filePath string) []types.EntityRecord {
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	// ref() — model dependency references.
	for _, m := range dbtRefRE.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		key := "dbt_ref:" + modelName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:          modelName,
			Kind:          "SCOPE.Component",
			Subtype:       "dbt_ref",
			QualifiedName: "dbt/ref/" + modelName,
			SourceFile:    filePath,
			Language:      "sql",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     fmt.Sprintf("ref('%s')", modelName),
			QualityScore:  0.75,
		})
	}

	// source() — source table references.
	for _, m := range dbtSourceRE.FindAllStringSubmatchIndex(src, -1) {
		sourceName := src[m[2]:m[3]]
		tableName := src[m[4]:m[5]]
		qualName := sourceName + "." + tableName
		key := "dbt_source:" + qualName
		if seen[key] {
			continue
		}
		seen[key] = true
		startLine := strings.Count(src[:m[0]], "\n") + 1
		entities = append(entities, types.EntityRecord{
			Name:          qualName,
			Kind:          "SCOPE.Datastore",
			Subtype:       "dbt_source",
			QualifiedName: "dbt/source/" + qualName,
			SourceFile:    filePath,
			Language:      "sql",
			StartLine:     startLine,
			EndLine:       startLine,
			Signature:     fmt.Sprintf("source('%s', '%s')", sourceName, tableName),
			QualityScore:  0.75,
		})
	}

	// config() — configuration block keys.
	for _, m := range dbtConfigRE.FindAllStringSubmatchIndex(src, -1) {
		configBody := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1

		// Extract individual key names from the config body.
		seenKeys := make(map[string]bool)
		for _, km := range dbtConfigKeyRE.FindAllStringSubmatch(configBody, -1) {
			keyName := km[1]
			if seenKeys[keyName] {
				continue
			}
			seenKeys[keyName] = true
			globalKey := "dbt_config:" + keyName
			if seen[globalKey] {
				continue
			}
			seen[globalKey] = true
			entities = append(entities, types.EntityRecord{
				Name:          keyName,
				Kind:          "SCOPE.Component",
				Subtype:       "dbt_config",
				QualifiedName: "dbt/config/" + keyName,
				SourceFile:    filePath,
				Language:      "sql",
				StartLine:     startLine,
				EndLine:       startLine,
				Signature:     fmt.Sprintf("config(%s=...)", keyName),
				QualityScore:  0.65,
			})
		}
	}

	return entities
}

// findBlockEnd returns the line number of the closing ) for a CREATE TABLE body.
func findBlockEnd(src string, openPos int) int {
	depth := 0
	for i, ch := range src[openPos:] {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.Count(src[:openPos+i], "\n") + 1
			}
		}
	}
	return strings.Count(src, "\n") + 1
}

// findStmtEnd returns the line number of the next semicolon after startPos.
func findStmtEnd(src string, startPos int) int {
	idx := strings.Index(src[startPos:], ";")
	if idx < 0 {
		return strings.Count(src, "\n") + 1
	}
	return strings.Count(src[:startPos+idx], "\n") + 1
}
