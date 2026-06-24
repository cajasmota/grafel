// gorm_orm.go — Grails GORM domain class → table/column/association schema
// synthesis (#5364, groovy coverage uplift; epic #5360).
//
// GORM (https://gorm.grails.org/) is the persistence layer of the Grails
// framework. A persisted entity is a *domain class* — a plain Groovy class
// (conventionally under `grails-app/domain/`) whose persistent fields are
// declared as typed properties and whose relations/mapping/constraints are
// declared with the GORM `static` DSL:
//
//	class Author {
//	    String name
//	    Integer age
//	    static hasMany = [books: Book]
//	    static constraints = {
//	        name blank: false
//	    }
//	    static mapping = {
//	        table 'authors'
//	    }
//	}
//
//	class Book {
//	    String title
//	    static belongsTo = [author: Author]
//	}
//
// This extractor mirrors the crystal/granite + ruby/rails ORM shape — it emits
// SCOPE.Schema entities carrying framework=gorm + provenance props so the shared
// engine ORM passes (model→table convergence, REFERENCES FK, QUERIES) consume
// them with no GORM-specific engine code:
//
//   - one SCOPE.Schema/model per recognised domain class
//   - one SCOPE.Schema/table per model (explicit `static mapping = { table '…' }`
//     argument when present, otherwise the model class name — GORM's runtime
//     default is the snake_case class name; we record the declared identity so
//     the explicit-override case is exact and the implicit case keys on the class)
//   - one SCOPE.Schema/column per typed persistent field (`String name`,
//     `Integer age`, `Date dateCreated`), stamping column_type + owning model.
//     Static/transient/relation-collection declarations are excluded.
//   - one SCOPE.Schema/association per `static hasMany = [x: T]` /
//     `static belongsTo = [y: T]` / `static hasOne = [z: T]` entry, stamping
//     assoc_kind + the resolved target model.
//   - a REFERENCES edge model→target for each belongsTo entry (the FK signal),
//     stamping fk_field + to_model.
//   - a QUERIES edge model→its table for each GORM query DSL call site that
//     references a known domain — the dynamic finders (`D.findByX`, `D.findAllBy…`,
//     `D.countBy…`, `D.get`, `D.list`, `D.findAll`, `D.read`, `D.count` →
//     select), persistence verbs (`d.save` → insert, `d.delete` → delete) — only
//     attributed to a domain declared in THIS file (honest, file-local), so an
//     arbitrary `Foo.list` on a non-domain class is never falsely counted.
//   - a SCOPE.Pattern/transaction_boundary per `Domain.withTransaction { … }`
//     block, mirroring the granite/Kotlin @Transactional boundary shape.
//
// Honest exclusions / follow-ups (no fabricated schema):
//   - map-style mapping column overrides (`columns { name column: 'full_name' }`)
//     beyond the table name are not read; the column identity stays the field name.
//   - embedded / composite-id / `static fetchMode` mapping options are skipped.
//   - constraints block contents (validation rules) are not modelled as schema.
//
// Registration key: "custom_groovy_gorm_orm".
package groovy

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	extractor.Register("custom_groovy_gorm_orm", &gormORMExtractor{})
}

type gormORMExtractor struct{}

func (e *gormORMExtractor) Language() string { return "custom_groovy_gorm_orm" }

var (
	// gormClassRe matches a top-level class declaration. Group 1 = the class name.
	// Domain candidacy (file path under grails-app/domain/ OR a GORM static DSL
	// marker in the body) is decided per-class below; this just enumerates classes.
	gormClassRe = regexp.MustCompile(`(?m)^[ \t]*(?:@\w+\s+)*class\s+([A-Z]\w*)\b`)

	// gormFieldRe matches a typed persistent field `Type name` (one per line),
	// e.g. `String title`, `Integer age`, `Date dateCreated`, `BigDecimal price`,
	// `List<String> tags`. Group 1 = the declared type; group 2 = the field name.
	// A trailing `=`/`(` (assignment or method) excludes methods and initialised
	// statics; `static`/`def`/`final transient` lines are filtered by the caller.
	gormFieldRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Z][\w.]*(?:<[^>]+>)?)\s+([a-z]\w*)\s*$`)

	// gormStaticAssocRe matches a `static hasMany|belongsTo|hasOne = [ … ]` map
	// declaration. Group 1 = the relation kind; group 2 = the bracketed map body
	// (`name: Type, other: Other`).
	gormStaticAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*static\s+(hasMany|belongsTo|hasOne)\s*=\s*\[([^\]]*)\]`)

	// gormBelongsToClassRe matches the single-class belongsTo form
	// `static belongsTo = Author` (no map), Group 1 = the target class.
	gormBelongsToClassRe = regexp.MustCompile(
		`(?m)^[ \t]*static\s+belongsTo\s*=\s*([A-Z]\w*)\s*$`)

	// gormAssocEntryRe pulls `name: Type` entries out of an association map body.
	// Group 1 = the association name; group 2 = the target class.
	gormAssocEntryRe = regexp.MustCompile(`([a-zA-Z_]\w*)\s*:\s*([A-Z]\w*)`)

	// gormMappingTableRe matches the `table '<name>'` directive inside a
	// `static mapping = { … }` block. The directive may sit at line start
	// (multi-line block) or after the opening brace (single-line block
	// `static mapping = { table 'books' }`), so it is anchored on a preceding
	// brace/line-start rather than line-start only. Group 1 = the table name.
	gormMappingTableRe = regexp.MustCompile(
		`(?m)(?:^|\{|;)\s*table\s+['"]([A-Za-z_]\w*)['"]`)

	// gormStaticDSLMarkerRe is the per-class candidacy marker: the body declares
	// one of the GORM static DSL members. Combined with the path heuristic so a
	// domain class is recognised even outside grails-app/domain/.
	gormStaticDSLMarkerRe = regexp.MustCompile(
		`\bstatic\s+(?:hasMany|belongsTo|hasOne|constraints|mapping|embedded|transients|namedQueries)\b`)

	// gormQueryRe matches a GORM query DSL call site `Domain.<verb>(…)`.
	// Group 1 = the domain receiver (validated against the known-domain set);
	// group 2 = the verb (a dynamic finder prefix or a static query method).
	gormQueryRe = regexp.MustCompile(
		`\b([A-Z]\w*)\s*\.\s*(findAllBy[A-Z]\w*|findBy[A-Z]\w*|countBy[A-Z]\w*|getBy[A-Z]\w*|findAll|findWhere|findAllWhere|list|get|read|count|exists|withCriteria|createCriteria|executeQuery|save|merge|delete)\b`)

	// gormTxRe matches a `Domain.withTransaction { … }` block header.
	// Group 1 = the domain receiver.
	gormTxRe = regexp.MustCompile(
		`(?m)\b([A-Z]\w*)\s*\.\s*withTransaction\s*\{`)

	// gormLifecycleRe matches a GORM event/lifecycle hook method declared inside a
	// domain class body — `def beforeInsert() { … }` / `def afterUpdate()` /
	// `def onLoad()` etc. These are GORM's persistence callbacks (the Grails
	// analogue of JPA @PrePersist / ActiveRecord before_save). Group 1 = the hook
	// name.
	gormLifecycleRe = regexp.MustCompile(
		`(?m)^[ \t]*def\s+(beforeInsert|beforeUpdate|beforeDelete|beforeValidate|afterInsert|afterUpdate|afterDelete|afterLoad|onLoad|onSave)\s*\(`)
)

// gormLifecycleHook is a parsed GORM event-callback method.
type gormLifecycleHook struct {
	name string
	line int
}

// gormReservedTypes are declared "Type name" shapes that are NOT persistent
// columns — they are the GORM static DSL members (handled separately) or common
// non-persistent helpers. Guards gormFieldRe from mis-counting them.
var gormReservedTypes = map[string]bool{
	"Closure": true, // `Closure constraints` etc. (rare typed form)
}

// gormQueryOp maps a GORM query verb to its canonical SQL operation.
func gormQueryOp(verb string) string {
	switch {
	case verb == "save" || verb == "merge":
		return "insert"
	case verb == "delete":
		return "delete"
	default: // every finder / list / get / count / criteria verb is a read
		return "select"
	}
}

func (e *gormORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "groovy" {
		return nil, nil
	}
	src := string(file.Content)
	pathIsDomain := strings.Contains(filepathSlashLower(file.Path), "grails-app/domain/")
	// Fast pre-filter: a GORM domain file either lives under grails-app/domain/
	// or declares a GORM static DSL member. Avoids scanning arbitrary Groovy.
	if !pathIsDomain && !gormStaticDSLMarkerRe.MatchString(src) {
		return nil, nil
	}

	models := collectGormModels(src, pathIsDomain)
	if len(models) == 0 {
		return nil, nil
	}
	domainNames := make(map[string]bool, len(models))
	for _, m := range models {
		domainNames[m.name] = true
	}
	queryOps := collectGormQueries(src, domainNames)

	var out []types.EntityRecord
	for _, m := range models {
		tableName := m.table
		if tableName == "" {
			tableName = m.name
		}

		// 1. model entity (+ REFERENCES FK edges + QUERIES edges).
		model := newGormSchema(m.name, "model", file.Path, m.line,
			"INFERRED_FROM_GORM_DOMAIN")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongsTo" {
				continue
			}
			rels = append(rels, types.RelationshipRecord{
				ToID: a.target,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"fk_field": a.name,
					"to_model": a.target,
				},
			})
		}
		if ops := queryOps[m.name]; len(ops) > 0 {
			for _, op := range gormQueryOpOrder(ops) {
				rels = append(rels, types.RelationshipRecord{
					ToID: tableName,
					Kind: "QUERIES",
					Properties: map[string]string{
						"operation": op,
						"table":     tableName,
						"model":     m.name,
					},
				})
			}
		}
		model.Relationships = rels
		model.ID = model.ComputeID()
		out = append(out, model)

		// 2. table entity.
		table := newGormSchema(tableName, "table", file.Path, m.line,
			"INFERRED_FROM_GORM_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities (one per typed persistent field).
		colSeen := make(map[string]bool)
		for _, c := range m.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			col := newGormSchema(c.name, "column", file.Path, c.line,
				"INFERRED_FROM_GORM_FIELD")
			col.Properties["column_type"] = c.typ
			col.Properties["model"] = m.name
			col.ID = col.ComputeID()
			out = append(out, col)
		}

		// 4. association entities (one per hasMany/belongsTo/hasOne entry).
		assocSeen := make(map[string]bool)
		for _, a := range m.assocs {
			key := a.kind + ":" + a.name
			if assocSeen[key] {
				continue
			}
			assocSeen[key] = true
			assoc := newGormSchema(a.name, "association", file.Path, a.line,
				"INFERRED_FROM_GORM_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = a.target
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}

		// 4b. GORM lifecycle/event hooks → SCOPE.Operation/function, mirroring the
		// granite/Rails callback shape so model_lifecycle_extraction is honest.
		hookSeen := make(map[string]bool)
		for _, h := range m.hooks {
			if hookSeen[h.name] {
				continue
			}
			hookSeen[h.name] = true
			ent := types.EntityRecord{
				Name:       m.name + "." + h.name,
				Kind:       "SCOPE.Operation",
				Subtype:    "function",
				SourceFile: file.Path,
				Language:   "groovy",
				StartLine:  h.line,
				EndLine:    h.line,
				Properties: map[string]string{
					"framework":     "gorm",
					"provenance":    "INFERRED_FROM_GORM_LIFECYCLE",
					"callback_type": h.name,
					"model":         m.name,
				},
			}
			ent.ID = ent.ComputeID()
			out = append(out, ent)
		}
	}

	// 5. transaction boundaries.
	out = append(out, collectGormTransactions(src, file.Path, domainNames)...)
	return out, nil
}

// filepathSlashLower normalises a path to forward slashes + lower case so the
// grails-app/domain/ heuristic is OS- and case-insensitive.
func filepathSlashLower(p string) string {
	return strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
}

// gormModel is a parsed GORM domain class.
type gormModel struct {
	name    string
	table   string
	line    int
	columns []gormColumn
	assocs  []gormAssoc
	hooks   []gormLifecycleHook
}

type gormColumn struct {
	name string
	typ  string
	line int
}

type gormAssoc struct {
	kind   string // hasMany / belongsTo / hasOne
	name   string
	target string
	line   int
}

// collectGormModels finds every domain class and the fields/associations/mapping
// in its brace-balanced body. When pathIsDomain is true every class in the file
// is treated as a domain; otherwise only classes whose body declares a GORM
// static DSL marker qualify (so a service/controller class in a domain dir is
// still the only false-positive risk, mitigated by the path convention).
func collectGormModels(src string, pathIsDomain bool) []gormModel {
	idx := gormClassRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []gormModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyStart := classBodyOpen(src, m[1])
		if bodyStart < 0 {
			continue
		}
		bodyEnd := braceMatch(src, bodyStart)
		body := src[bodyStart:bodyEnd]
		// Candidacy: path-based OR a GORM static DSL marker in the body.
		if !pathIsDomain && !gormStaticDSLMarkerRe.MatchString(body) {
			continue
		}
		bodyStartLine := strings.Count(src[:bodyStart], "\n") + 1

		gm := gormModel{name: name, line: startLine}

		if tm := gormMappingTableRe.FindStringSubmatch(body); tm != nil {
			gm.table = tm[1]
		}

		// Typed persistent fields.
		for _, fm := range gormFieldRe.FindAllStringSubmatchIndex(body, -1) {
			typ := body[fm[2]:fm[3]]
			fname := body[fm[4]:fm[5]]
			if gormReservedTypes[typ] {
				continue
			}
			gm.columns = append(gm.columns, gormColumn{
				name: fname,
				typ:  typ,
				line: bodyStartLine + strings.Count(body[:fm[0]], "\n"),
			})
		}

		// Map-form associations: static hasMany|belongsTo|hasOne = [name: Type].
		for _, am := range gormStaticAssocRe.FindAllStringSubmatchIndex(body, -1) {
			kind := body[am[2]:am[3]]
			mapBody := body[am[4]:am[5]]
			line := bodyStartLine + strings.Count(body[:am[0]], "\n")
			for _, em := range gormAssocEntryRe.FindAllStringSubmatch(mapBody, -1) {
				gm.assocs = append(gm.assocs, gormAssoc{
					kind:   kind,
					name:   em[1],
					target: em[2],
					line:   line,
				})
			}
		}
		// Single-class belongsTo: static belongsTo = Author.
		if bm := gormBelongsToClassRe.FindStringSubmatchIndex(body); bm != nil {
			target := body[bm[2]:bm[3]]
			gm.assocs = append(gm.assocs, gormAssoc{
				kind:   "belongsTo",
				name:   lowerFirst(target),
				target: target,
				line:   bodyStartLine + strings.Count(body[:bm[0]], "\n"),
			})
		}

		// GORM event/lifecycle hooks (def beforeInsert(){…}, def afterUpdate()…).
		for _, hm := range gormLifecycleRe.FindAllStringSubmatchIndex(body, -1) {
			gm.hooks = append(gm.hooks, gormLifecycleHook{
				name: body[hm[2]:hm[3]],
				line: bodyStartLine + strings.Count(body[:hm[0]], "\n"),
			})
		}

		models = append(models, gm)
	}
	return models
}

// classBodyOpen returns the byte offset of the `{` that opens a class body,
// scanning forward from fromByte (just past the class-name match). Returns -1
// when no opening brace is found before a statement terminator.
func classBodyOpen(src string, fromByte int) int {
	for i := fromByte; i < len(src); i++ {
		switch src[i] {
		case '{':
			return i
		case '\n':
			// `extends`/`implements` clauses keep the header on continuation
			// lines until the brace; only a stray terminator should abort.
		}
	}
	return -1
}

// braceMatch returns the byte offset just past the `}` matching the `{` at
// openByte. Falls back to len(src) when unbalanced.
func braceMatch(src string, openByte int) int {
	depth := 0
	for i := openByte; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// lowerFirst lower-cases the first rune of s (Author → author) for the
// convention belongsTo field name of the single-class form.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// collectGormQueries attributes each `Domain.<verb>(…)` query call site to its
// domain → the set of canonical SQL ops. Only receivers naming a recognised
// domain in this file are attributed (honest, file-local).
func collectGormQueries(src string, domainNames map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range gormQueryRe.FindAllStringSubmatch(src, -1) {
		recv, verb := m[1], m[2]
		if !domainNames[recv] {
			continue
		}
		op := gormQueryOp(verb)
		if out[recv] == nil {
			out[recv] = map[string]bool{}
		}
		out[recv][op] = true
	}
	return out
}

// gormQueryOpOrder returns ops in a stable order for deterministic emission.
func gormQueryOpOrder(ops map[string]bool) []string {
	var out []string
	for _, op := range []string{"select", "insert", "update", "delete"} {
		if ops[op] {
			out = append(out, op)
		}
	}
	return out
}

// collectGormTransactions emits a SCOPE.Pattern/transaction_boundary entity per
// `Domain.withTransaction { … }` block whose receiver names a known domain.
func collectGormTransactions(src, path string, domainNames map[string]bool) []types.EntityRecord {
	var out []types.EntityRecord
	seen := map[string]bool{}
	for _, m := range gormTxRe.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		if !domainNames[recv] {
			continue
		}
		if seen[recv] {
			continue
		}
		seen[recv] = true
		line := strings.Count(src[:m[0]], "\n") + 1
		ent := types.EntityRecord{
			Name:       recv + ".withTransaction",
			Kind:       "SCOPE.Pattern",
			Subtype:    "transaction_boundary",
			SourceFile: path,
			Language:   "groovy",
			StartLine:  line,
			EndLine:    line,
			Properties: map[string]string{
				"framework":     "gorm",
				"transactional": "true",
				"db_handle":     recv,
				"provenance":    "INFERRED_FROM_GORM_TRANSACTION",
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

// newGormSchema builds a SCOPE.Schema entity with framework=gorm + provenance.
func newGormSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "groovy",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "gorm",
			"provenance": provenance,
		},
	}
}
