// granite_orm.go — Crystal Granite ORM model → table/column/association
// schema synthesis (#4905).
//
// Granite (https://amberframework.github.io/granite/) is one of the most widely
// used Crystal ORMs (the default ORM for the Amber web framework). A persisted
// model is a class that inherits from `Granite::Base` and declares its mapping
// with a macro DSL:
//
//	require "granite/adapter/pg"
//
//	class User < Granite::Base
//	  connection pg
//	  table users
//
//	  column id : Int64, primary: true
//	  column name : String
//	  column email : String
//
//	  has_many :posts
//	end
//
//	class Post < Granite::Base
//	  table posts
//
//	  column id : Int64, primary: true
//	  column title : String
//
//	  belongs_to :user
//	end
//
// What this extractor emits (mirrors the Ruby/Rails + Nim/Norm ORM shape —
// SCOPE.Schema entities carrying framework+provenance props):
//   - one SCOPE.Schema/model per `class T < Granite::Base` declaration
//   - one SCOPE.Schema/table per model. The table identity is the explicit
//     `table <name>` macro argument when present, otherwise the model class
//     name (Granite's runtime default is the snake_case pluralisation; we record
//     the declared name so the explicit-override case is exact and the implicit
//     case is keyed by the class name).
//   - one SCOPE.Schema/column per `column <name> : <Type>[, primary: true]`
//     macro, stamping column_type, the owning model, and primary_key=true on the
//     primary column.
//   - a REFERENCES edge model → referenced model for each `belongs_to :other`
//     association (the FK signal), keyed by the CamelCased association name.
//   - an association SCOPE.Schema/association entity per belongs_to/has_many/
//     has_one carrying assoc_kind + target, so the association DSL is recorded
//     even when the target model lives in another file.
//
// Granite query DSL + transaction deepening (#4935):
//   - the `timestamps` macro synthesises the conventional created_at/updated_at
//     Time columns (graniteTimestampsRe), so the audit columns Granite injects at
//     runtime are recorded.
//   - Granite's class-method query DSL — `Model.all/find/find_by/where/first/
//     count/create/save/update/clear/delete` — at a call site referencing a known
//     model emits a QUERIES edge model → its table stamped with the canonical SQL
//     operation (select/insert/update/delete), mirroring the Nim/Norm shape.
//   - a Crystal-DB `db.transaction do … end` block emits a
//     SCOPE.Pattern/transaction_boundary entity (transactional=true), mirroring
//     the Nim/Norm + Kotlin/Java @Transactional boundary shape.
//
// Honest exclusions / follow-ups (no fabricated schema — #5032):
//   - `column` macro options beyond `primary: true` (converters, defaults) and
//     index declarations are not yet read.
//   - Granite migrations (`Granite::Migrator`, createTable/exec schema ops) are
//     not yet parsed.
//   - `has_many through:` / polymorphic associations and the explicit
//     `foreign_key:` override remain deferred.
//   - Jennifer / Clear / Avram / Crecto ORMs are deferred to #4936.
//
// Registration key: "custom_crystal_granite_orm".
package crystal

import (
	"context"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_crystal_granite_orm", &graniteORMExtractor{})
}

type graniteORMExtractor struct{}

func (e *graniteORMExtractor) Language() string { return "custom_crystal_granite_orm" }

var (
	// graniteModelRe matches a Granite model class declaration: a class that
	// inherits from `Granite::Base`. Capture group 1 is the model class name.
	graniteModelRe = regexp.MustCompile(
		`(?m)^[ \t]*(?:abstract\s+)?class\s+([A-Z]\w*)\s*<\s*Granite::Base\b`)

	// graniteTableRe matches the `table <name>` macro (name may be a bare
	// identifier or a symbol/string literal). Capture group 1 is the table name.
	graniteTableRe = regexp.MustCompile(
		`(?m)^[ \t]*table\s+:?["']?([A-Za-z_]\w*)["']?`)

	// graniteColumnRe matches a `column <name> : <Type>[, primary: true]` macro.
	// Group 1 = column name; group 2 = column type (`?` nilable marker trimmed
	// by the caller); group 3 = the trailing options (scanned for `primary`).
	graniteColumnRe = regexp.MustCompile(
		`(?m)^[ \t]*column\s+([a-z_]\w*)\s*:\s*([A-Za-z_][\w:]*\??)\s*(,.*)?$`)

	// graniteAssocRe matches a belongs_to / has_many / has_one association macro.
	// Group 1 = association kind; group 2 = association name (symbol/string).
	graniteAssocRe = regexp.MustCompile(
		`(?m)^[ \t]*(belongs_to|has_many|has_one)\s+:?["']?([a-z_]\w*)["']?`)

	// graniteTimestampsRe matches the `timestamps` macro, Granite's helper that
	// injects the conventional created_at/updated_at Time audit columns.
	graniteTimestampsRe = regexp.MustCompile(`(?m)^[ \t]*timestamps\b`)

	// graniteQueryRe matches a Granite class-method query DSL call site:
	// `User.all`, `Post.find(1)`, `User.find_by(email: x)`, `Post.where(...)`,
	// `User.create(...)`, `Post.delete`. Group 1 is the model class name (the
	// receiver, recognised against the known-model set), group 2 the query verb.
	graniteQueryRe = regexp.MustCompile(
		`(?m)\b([A-Z]\w*)\s*\.\s*(all|find_by|find|where|first|last|count|exists\?|create|save|update|import|clear|delete)\b`)

	// graniteTxRe matches a Crystal-DB transaction block header
	// `db.transaction do` / `conn.transaction do`. Group 1 is the receiver.
	graniteTxRe = regexp.MustCompile(
		`(?m)^[ \t]*([A-Za-z_]\w*)\s*\.\s*transaction\s+do\b`)
)

// graniteQueryOp maps a Granite query verb to its canonical SQL operation.
func graniteQueryOp(verb string) string {
	switch verb {
	case "create", "import":
		return "insert"
	case "save", "update":
		return "update"
	case "clear", "delete":
		return "delete"
	default: // all/find/find_by/where/first/last/count/exists?
		return "select"
	}
}

// graniteHasModel is a fast pre-filter: the file must reference Granite's base
// type to be worth scanning, so we never misfire on arbitrary Crystal classes.
func graniteHasModel(content string) bool {
	return strings.Contains(content, "Granite::Base")
}

func (e *graniteORMExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "crystal" {
		return nil, nil
	}
	src := string(file.Content)
	if !graniteHasModel(src) {
		return nil, nil
	}

	models := collectGraniteModels(src)
	if len(models) == 0 {
		return nil, nil
	}
	// Set of known model names — used to attribute a `Model.<verb>` query call
	// site to its owning model only when the receiver names a recognised model.
	modelNames := make(map[string]bool, len(models))
	for _, m := range models {
		modelNames[m.name] = true
	}
	// queryOps maps a model name → the set of canonical SQL ops attributed to it
	// by `Model.<verb>(…)` query DSL call sites elsewhere in the file.
	queryOps := collectGraniteQueries(src, modelNames)

	var out []types.EntityRecord
	for _, m := range models {
		tableName := m.table
		if tableName == "" {
			tableName = m.name
		}

		// 1. model entity.
		model := newGraniteSchema(m.name, "model", file.Path, m.line,
			"INFERRED_FROM_GRANITE_MODEL")
		var rels []types.RelationshipRecord
		for _, a := range m.assocs {
			if a.kind != "belongs_to" {
				continue
			}
			// belongs_to :user → REFERENCES User (CamelCased singular target).
			target := camelize(a.name)
			rels = append(rels, types.RelationshipRecord{
				ToID: target,
				Kind: "REFERENCES",
				Properties: map[string]string{
					"fk_field": a.name,
					"to_model": target,
				},
			})
		}
		// Query attribution: model → its table, one edge per attributed op.
		if ops := queryOps[m.name]; len(ops) > 0 {
			for _, op := range graniteQueryOpOrder(ops) {
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

		// 2. table entity (explicit `table <name>` or the class name).
		table := newGraniteSchema(tableName, "table", file.Path, m.line,
			"INFERRED_FROM_GRANITE_TABLE")
		table.Properties["model"] = m.name
		table.ID = table.ComputeID()
		out = append(out, table)

		// 3. column entities.
		colSeen := make(map[string]bool)
		for _, c := range m.columns {
			if colSeen[c.name] {
				continue
			}
			colSeen[c.name] = true
			provenance := "INFERRED_FROM_GRANITE_COLUMN"
			if c.auto {
				provenance = "INFERRED_FROM_GRANITE_TIMESTAMPS"
			}
			col := newGraniteSchema(c.name, "column", file.Path, c.line,
				provenance)
			col.Properties["column_type"] = c.typ
			col.Properties["model"] = m.name
			if c.primary {
				col.Properties["primary_key"] = "true"
			}
			if c.auto {
				col.Properties["auto_timestamp"] = "true"
			}
			col.ID = col.ComputeID()
			out = append(out, col)
		}

		// 4. association entities (one per belongs_to/has_many/has_one).
		assocSeen := make(map[string]bool)
		for _, a := range m.assocs {
			key := a.kind + ":" + a.name
			if assocSeen[key] {
				continue
			}
			assocSeen[key] = true
			assoc := newGraniteSchema(a.name, "association", file.Path, a.line,
				"INFERRED_FROM_GRANITE_ASSOCIATION")
			assoc.Properties["assoc_kind"] = a.kind
			assoc.Properties["model"] = m.name
			assoc.Properties["target"] = camelize(a.name)
			assoc.ID = assoc.ComputeID()
			out = append(out, assoc)
		}
	}

	// 5. transaction boundaries: one SCOPE.Pattern/transaction_boundary per
	// `<db>.transaction do … end` block.
	out = append(out, collectGraniteTransactions(src, file.Path)...)
	return out, nil
}

// graniteModel is a parsed Granite model with its table, columns, associations.
type graniteModel struct {
	name    string
	table   string
	line    int
	columns []graniteColumn
	assocs  []graniteAssoc
}

type graniteColumn struct {
	name    string
	typ     string
	primary bool
	auto    bool // synthesised by the `timestamps` macro (created_at/updated_at)
	line    int
}

type graniteAssoc struct {
	kind string // belongs_to / has_many / has_one
	name string
	line int
}

// collectGraniteModels finds every `class T < Granite::Base` declaration and the
// table/column/association macros in its `end`-terminated body.
func collectGraniteModels(src string) []graniteModel {
	idx := graniteModelRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var models []graniteModel
	for _, m := range idx {
		name := src[m[2]:m[3]]
		startLine := strings.Count(src[:m[0]], "\n") + 1
		bodyEnd := graniteClassEnd(src, m[1])
		body := src[m[1]:bodyEnd]
		bodyStartLine := startLine // body offsets converted to absolute lines below

		gm := graniteModel{name: name, line: startLine}

		if tm := graniteTableRe.FindStringSubmatch(body); tm != nil {
			gm.table = tm[1]
		}
		for _, cm := range graniteColumnRe.FindAllStringSubmatchIndex(body, -1) {
			cname := body[cm[2]:cm[3]]
			ctyp := strings.TrimSuffix(body[cm[4]:cm[5]], "?")
			opts := ""
			if cm[6] >= 0 {
				opts = body[cm[6]:cm[7]]
			}
			primary := strings.Contains(opts, "primary")
			cline := bodyStartLine + strings.Count(body[:cm[0]], "\n")
			gm.columns = append(gm.columns, graniteColumn{
				name: cname, typ: ctyp, primary: primary, line: cline,
			})
		}
		// The `timestamps` macro injects created_at/updated_at Time audit columns.
		if tsLoc := graniteTimestampsRe.FindStringIndex(body); tsLoc != nil {
			tsLine := bodyStartLine + strings.Count(body[:tsLoc[0]], "\n")
			for _, n := range []string{"created_at", "updated_at"} {
				gm.columns = append(gm.columns, graniteColumn{
					name: n, typ: "Time", auto: true, line: tsLine,
				})
			}
		}
		for _, am := range graniteAssocRe.FindAllStringSubmatchIndex(body, -1) {
			akind := body[am[2]:am[3]]
			aname := body[am[4]:am[5]]
			aline := bodyStartLine + strings.Count(body[:am[0]], "\n")
			gm.assocs = append(gm.assocs, graniteAssoc{
				kind: akind, name: aname, line: aline,
			})
		}
		models = append(models, gm)
	}
	return models
}

// graniteBlockRe matches Crystal block openers and the `end` closer so a class
// body can be delimited by tracking nesting depth.
var graniteBlockRe = regexp.MustCompile(
	`\b(class|module|struct|def|macro|lib|enum|annotation|begin|do|if|unless|case|while|until|for|end)\b`)

// graniteClassEnd scans forward from fromByte (just past the class header)
// tracking nested block openers and returns the byte offset just before the
// matching `end` that closes the class. Falls back to len(src) when malformed.
func graniteClassEnd(src string, fromByte int) int {
	sub := src[fromByte:]
	depth := 1
	pos := 0
	for pos < len(sub) {
		loc := graniteBlockRe.FindStringIndex(sub[pos:])
		if loc == nil {
			break
		}
		tok := sub[pos+loc[0] : pos+loc[1]]
		if tok == "end" {
			depth--
			if depth == 0 {
				return fromByte + pos + loc[0]
			}
		} else {
			depth++
		}
		pos += loc[1]
	}
	return len(src)
}

// collectGraniteQueries scans the whole file for Granite class-method query DSL
// call sites (`Model.all`, `Post.find(1)`, `User.create(…)`, …) and attributes
// each to its model → the set of canonical SQL operations. Only receivers that
// name a recognised model in this file are attributed (honest, file-local), so
// an arbitrary `Foo.find` on a non-model class is never falsely counted.
func collectGraniteQueries(src string, modelNames map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range graniteQueryRe.FindAllStringSubmatch(src, -1) {
		recv, verb := m[1], m[2]
		if !modelNames[recv] {
			continue
		}
		op := graniteQueryOp(verb)
		if out[recv] == nil {
			out[recv] = map[string]bool{}
		}
		out[recv][op] = true
	}
	return out
}

// graniteQueryOpOrder returns the operations in a stable order for deterministic
// edge emission.
func graniteQueryOpOrder(ops map[string]bool) []string {
	var out []string
	for _, op := range []string{"select", "insert", "update", "delete"} {
		if ops[op] {
			out = append(out, op)
		}
	}
	return out
}

// collectGraniteTransactions emits a SCOPE.Pattern/transaction_boundary entity
// per `<db>.transaction do … end` block in the file (transactional=true),
// mirroring the Nim/Norm + Kotlin/Java @Transactional boundary shape.
func collectGraniteTransactions(src, path string) []types.EntityRecord {
	idx := graniteTxRe.FindAllStringSubmatchIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	var out []types.EntityRecord
	for _, m := range idx {
		recv := src[m[2]:m[3]]
		line := strings.Count(src[:m[0]], "\n") + 1
		ent := types.EntityRecord{
			Name:       recv + ".transaction",
			Kind:       "SCOPE.Pattern",
			Subtype:    "transaction_boundary",
			SourceFile: path,
			Language:   "crystal",
			StartLine:  line,
			EndLine:    line,
			Properties: map[string]string{
				"framework":     "granite",
				"transactional": "true",
				"db_handle":     recv,
				"provenance":    "INFERRED_FROM_GRANITE_TRANSACTION",
			},
		}
		ent.ID = ent.ComputeID()
		out = append(out, ent)
	}
	return out
}

// camelize converts a snake_case association name to a CamelCase model name:
// `user` → `User`, `blog_post` → `BlogPost`. Used to map a belongs_to symbol to
// its target model class.
func camelize(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// newGraniteSchema builds a SCOPE.Schema entity with the Granite framework + the
// given provenance stamp.
func newGraniteSchema(name, subtype, path string, line int, provenance string) types.EntityRecord {
	return types.EntityRecord{
		Name:       name,
		Kind:       "SCOPE.Schema",
		Subtype:    subtype,
		SourceFile: path,
		Language:   "crystal",
		StartLine:  line,
		EndLine:    line,
		Properties: map[string]string{
			"framework":  "granite",
			"provenance": provenance,
		},
	}
}
