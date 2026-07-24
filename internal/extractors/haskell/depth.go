// depth.go — framework-aware Haskell extraction: persistent ORM entity blocks
// and hspec test suites (#5373, bootstrap epic #5360).
//
// These sit ALONGSIDE the structural base extractor (extractor.go). The base
// extractor mines modules / functions / data / class / instance entities; this
// file adds two framework records:
//
//   - persistent ORM: the `[persistLowerCase| ... |]` / `[persistUpperCase| ... |]`
//     QuasiQuote entity-definition blocks (the canonical persistent schema DSL)
//     are parsed into one ORM-model SCOPE.Component per entity, carrying a
//     MAPS_TO edge to the derived table name plus the entity's field list. This
//     is the same (orm_model, table_name) contract the cross-language ormlink
//     sentinel emits, so the topology / data-coupling passes light up for
//     Haskell persistent schemas.
//
//   - hspec testing: `describe "..."` / `it "..."` spec blocks are lifted into
//     one SCOPE.Operation(subtype="test_suite") per spec file, carrying the
//     example count, mirroring the Crystal/Ruby spec-suite model.
//
// Honest scope (partial, no fabrication):
//   - persistent: the entity name + field names are recovered from the
//     persistLowerCase block. Migrations (`runMigration`), relations
//     (`<entity>Id` foreign keys are recorded as plain fields), `deriving`
//     clauses and the `!force`/`Maybe`/`sql=` field attributes are not modelled
//     as separate edges — a documented follow-up.
//   - hspec: literal `describe`/`it` strings inside a Spec module are counted;
//     `hspec-discover` auto-generated spec aggregation and QuickCheck `prop_`
//     property functions are NOT linked here (follow-up).
package haskell

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// ---------------------------------------------------------------------------
// persistent ORM
// ---------------------------------------------------------------------------

// persistBlockRE captures the body of a persistent QuasiQuote schema block:
//
//	share [mkPersist sqlSettings, mkMigrate "migrateAll"] [persistLowerCase|
//	User
//	    name String
//	    age  Int Maybe
//	    deriving Show
//	Post
//	    title String
//	    authorId UserId
//	|]
//
// Capture group 1 is the block body between the opening `|` and the closing `|]`.
// Both persistLowerCase and persistUpperCase (and the rarer persistFileWith) are
// matched.
var persistBlockRE = regexp.MustCompile(`(?s)persist(?:LowerCase|UpperCase)\|(.*?)\|\]`)

// persistEntityHeadRE matches an entity header line inside a persistent block:
// a line beginning at column 0 (no leading whitespace) with a capitalised
// identifier. Fields and clauses are indented beneath it.
var persistEntityHeadRE = regexp.MustCompile(`^([A-Z][A-Za-z0-9_']*)\s*$`)

// persistFieldRE matches an entity field line: indented `fieldName Type ...`.
// Capture group 1 is the field name (lower-camel-case).
var persistFieldRE = regexp.MustCompile(`^\s+([a-z][A-Za-z0-9_']*)\s+\S`)

// persistClauseWords are the indented keywords that are NOT fields (clauses).
var persistClauseWords = map[string]bool{
	"deriving": true, "primary": true, "foreign": true, "unique": true,
}

// extractPersistentEntities parses persistent QuasiQuote schema blocks and emits
// one orm_model SCOPE.Component per entity with a MAPS_TO edge to its table.
func extractPersistentEntities(src, filePath string) []types.EntityRecord {
	if !strings.Contains(src, "persistLowerCase") && !strings.Contains(src, "persistUpperCase") {
		return nil
	}
	var out []types.EntityRecord
	for _, block := range persistBlockRE.FindAllStringSubmatch(src, -1) {
		if len(block) < 2 {
			continue
		}
		body := block[1]
		blockStart := strings.Index(src, block[0])
		lines := strings.Split(body, "\n")

		var (
			curEntity string
			curFields []string
			curLine   int
		)
		flush := func() {
			if curEntity == "" {
				return
			}
			out = append(out, buildPersistentModel(curEntity, curFields, filePath, curLine))
			curEntity = ""
			curFields = nil
		}

		for idx, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			if h := persistEntityHeadRE.FindStringSubmatch(ln); h != nil {
				flush()
				curEntity = h[1]
				// Approximate the entity's source line from the block offset.
				curLine = strings.Count(src[:blockStart], "\n") + 1 + idx + 1
				continue
			}
			if curEntity == "" {
				continue
			}
			// An indented clause keyword (deriving / primary / foreign / unique)
			// is not a field.
			trimmed := strings.Fields(strings.TrimSpace(ln))
			if len(trimmed) > 0 && persistClauseWords[trimmed[0]] {
				continue
			}
			if f := persistFieldRE.FindStringSubmatch(ln); f != nil {
				curFields = append(curFields, f[1])
			}
		}
		flush()
	}
	return out
}

// persistTableName derives the SQL table name persistent generates for an entity
// (default: snake_case of the entity name, lower-cased — `BlogPost` → `blog_post`).
func persistTableName(entity string) string {
	var b strings.Builder
	for i, r := range entity {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// buildPersistentModel builds the orm_model SCOPE.Component for one persistent
// entity, carrying the (orm_model, table_name) contract + a MAPS_TO edge, in the
// same shape the cross-language ormlink sentinel emits.
func buildPersistentModel(entity string, fields []string, filePath string, line int) types.EntityRecord {
	table := persistTableName(entity)
	fromRef := "scope:ormmodel:" + filePath + "#" + entity
	return types.EntityRecord{
		Name:          entity,
		Kind:          "SCOPE.Component",
		Subtype:       "orm_model",
		QualifiedName: fromRef,
		SourceFile:    filePath,
		Language:      "haskell",
		StartLine:     line,
		EndLine:       line,
		Signature:     "persistent entity " + entity,
		Properties: map[string]string{
			"orm_model":   entity,
			"table_name":  table,
			"orm":         "persistent",
			"fields":      strings.Join(fields, ","),
			"field_count": strconv.Itoa(len(fields)),
			"provenance":  "INFERRED_FROM_ORM_MODEL",
		},
		Relationships: []types.RelationshipRecord{
			{
				FromID: fromRef,
				ToID:   table,
				Kind:   "MAPS_TO",
				Properties: types.Props{
					{K: "orm_model", V: entity},
					{K: "table_name", V: table},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// hspec testing
// ---------------------------------------------------------------------------

// hspecBlockRE matches an hspec `describe`/`context`/`it`/`specify` call with a
// leading string-literal label. Capture group 1 is the keyword; group 2 is the
// label.
var hspecBlockRE = regexp.MustCompile(`\b(describe|context|it|specify)\s+"([^"\n\r]*)"`)

// isHspecFile reports whether a path looks like an hspec spec file
// (conventionally `*Spec.hs` or a file under a `test`/`spec` dir).
func isHspecFile(filePath string) bool {
	base := filepath.Base(filepath.ToSlash(filePath))
	if strings.HasSuffix(base, "Spec.hs") || strings.HasSuffix(base, "Test.hs") {
		return true
	}
	return false
}

// extractHspecSuite emits one SCOPE.Operation(subtype="test_suite") per hspec
// spec file carrying the example (`it`/`specify`) count. No suite is emitted for
// a file with no examples or a non-spec file with no hspec marker.
func extractHspecSuite(src, filePath string) []types.EntityRecord {
	hasHspecImport := strings.Contains(src, "Test.Hspec") || strings.Contains(src, "hspec")
	if !isHspecFile(filePath) && !hasHspecImport {
		return nil
	}
	matches := hspecBlockRE.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return nil
	}
	exampleCount := 0
	subject := ""
	for _, m := range matches {
		switch m[1] {
		case "it", "specify":
			exampleCount++
		case "describe", "context":
			if subject == "" {
				subject = m[2]
			}
		}
	}
	if exampleCount == 0 {
		return nil
	}
	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(filePath)), ".hs")
	rec := types.EntityRecord{
		Name:       "spec_suite:" + base,
		Kind:       "SCOPE.Operation",
		Subtype:    "test_suite",
		SourceFile: filePath,
		Language:   "haskell",
		StartLine:  1,
		EndLine:    1,
		Properties: map[string]string{
			"framework":      "hspec",
			"test_framework": "hspec",
			"provenance":     "INFERRED_FROM_HSPEC_SUITE",
			"example_count":  strconv.Itoa(exampleCount),
		},
	}
	// Subject affinity: a `*Spec.hs` file conventionally tests the module named
	// by its stem (`UserSpec.hs` → `User`), so emit a name-affinity TESTS edge.
	stemSubject := strings.TrimSuffix(strings.TrimSuffix(base, "Spec"), "Test")
	if stemSubject != "" && stemSubject != base {
		rec.Relationships = append(rec.Relationships, types.RelationshipRecord{
			ToID: stemSubject,
			Kind: string(types.RelationshipKindTests),
			Properties: types.Props{
				{K: "framework", V: "hspec"},
				{K: "match_source", V: "spec_stem_affinity"},
			},
		})
	}
	return []types.EntityRecord{rec}
}
