package engine

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// migration_schema_ops.go — the per-migration schema-OPERATION pass (#3628,
// epic #3625).
//
// WHY. A separate pass (migration_sequence.go) already restores migration
// APPLY-ORDER (the PRECEDES edge). The language extractors already detect the
// individual schema mutations a migration performs and emit them as entities:
//
//   - Alembic       SCOPE.Schema  framework=alembic   (table / column / index)
//   - Rails / AR    SCOPE.Evolution framework=activerecord (ddl_operation)
//   - TypeORM/knex/  SCOPE.Evolution (migration_op + table[/column])
//     Sequelize/objection
//   - Django        Migration (subtype=django) with an `operations` JSON array
//   - Flyway/Liqui   SCOPE.Datastore subtype=table carrying migration_file
//     (SQL DDL CREATE TABLE inside a versioned migration)
//
// But NONE of those entities was connected to the table it mutates by an edge
// that converges with the QUERY→table access axis (ACCESSES_TABLE → a
// SCOPE.DataAccess node carrying a `table` property). So "what touches table
// users" could see queries OR migration entities, but never on ONE node, and
// the shared-DB-coupling pass (which groups by normalised table key) ignored
// migrations entirely.
//
// WHAT THIS PASS DOES. For every migration schema-op entity it:
//
//  1. derives (op, table[, column]) from the entity's kind/framework/props;
//  2. gets-or-creates ONE synthetic SCOPE.Table convergence node per
//     (repo, normalised-table) — id = EntityID(repo,"SCOPE.Table",table,"");
//  3. emits a MODIFIES_TABLE edge migration-op-entity → SCOPE.Table carrying
//     {op, table, column}; and
//  4. so the convergence is real (not just same-key-in-properties), rewires
//     the QUERY side too: for every SCOPE.DataAccess on that same normalised
//     table it emits an ACCESSES_TABLE edge SCOPE.DataAccess → the SAME
//     SCOPE.Table node.
//
// The synthetic node's `table` property uses the SAME normTable() key the
// shared-DB-coupling pass and ACCESSES_TABLE attribution already use, so the
// migration→table and query→table axes now genuinely meet on one logical
// table.
//
// HONEST / IDEMPOTENT. Dynamic table names (variable, "", UNKNOWN) are
// skipped — no edge, no node. A re-run recomputes identical node ids and
// stable RelationshipIDs, so no duplicates are produced.

// MigrationSchemaOpsStats summarises an ApplyMigrationSchemaOps run.
type MigrationSchemaOpsStats struct {
	Skipped bool
	// OpsConsidered is the number of migration schema-op entities inspected.
	OpsConsidered int
	// TablesConverged is the number of distinct synthetic SCOPE.Table nodes
	// created.
	TablesConverged int
	// ModifiesEdges is the number of MODIFIES_TABLE edges emitted.
	ModifiesEdges int
	// AccessConvergedEdges is the number of ACCESSES_TABLE edges emitted that
	// rewire a SCOPE.DataAccess onto a converged SCOPE.Table node.
	AccessConvergedEdges int
}

// migrationSchemaOp is the normalised result of recognising one migration
// schema-op entity.
type migrationSchemaOp struct {
	op     string // create_table | add_column | drop_column | create_index | ...
	table  string // RAW table name (normalisation happens at convergence)
	column string // optional column name ("" when not column-scoped)
}

// ApplyMigrationSchemaOps emits MODIFIES_TABLE edges (and converging
// ACCESSES_TABLE edges) for every recognised migration schema-op entity in
// doc. Returns stats; sets Skipped when no migration schema-op entity exists.
func ApplyMigrationSchemaOps(doc *graph.Document) MigrationSchemaOpsStats {
	if doc == nil || len(doc.Entities) == 0 {
		return MigrationSchemaOpsStats{Skipped: true}
	}

	var stats MigrationSchemaOpsStats

	// tableNode caches the synthetic SCOPE.Table id per (repo, normalised-table)
	// so repeated ops on the same table converge on ONE node.
	type tk struct{ repo, table string }
	tableNodeID := make(map[tk]string)
	// existingEdge dedupes both MODIFIES_TABLE and ACCESSES_TABLE edges this
	// pass emits, keyed by their stable RelationshipID, so a re-run (or two
	// ops with the same from/to/kind) never duplicates an edge.
	existingEdge := make(map[string]struct{})
	for i := range doc.Relationships {
		existingEdge[doc.Relationships[i].ID] = struct{}{}
	}

	var newTables []graph.Entity
	var newEdges []graph.Relationship

	getTableNode := func(repo, normName string) string {
		key := tk{repo: repo, table: normName}
		if id, ok := tableNodeID[key]; ok {
			return id
		}
		id := graph.EntityID(repo, string(types.EntityKindTable), normName, "")
		tableNodeID[key] = id
		newTables = append(newTables, graph.Entity{
			ID:         id,
			Name:       normName,
			Kind:       string(types.EntityKindTable),
			Subtype:    "table",
			SourceFile: "",
			Language:   "",
			Properties: map[string]string{
				"table":      normName,
				"repo":       repo,
				"provenance": "SYNTHESIZED_TABLE_CONVERGENCE",
			},
		})
		stats.TablesConverged++
		return id
	}

	// addEdge emits an edge, deduped by a stable id. `idSalt` disambiguates
	// edges that share (from,to,kind) but differ semantically — e.g. several
	// distinct ops (create_table + add_column + drop_column) a single Django
	// migration performs on the SAME table converge on one table node yet must
	// remain distinct edges. The salt is folded into the kind for ID hashing
	// only (the stored Kind is unchanged).
	addEdge := func(fromID, toID, kind, idSalt string, props map[string]string) bool {
		id := graph.RelationshipID(fromID, toID, kind+"\x00"+idSalt)
		if _, dup := existingEdge[id]; dup {
			return false
		}
		existingEdge[id] = struct{}{}
		newEdges = append(newEdges, graph.Relationship{
			ID:         id,
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
		return true
	}

	for i := range doc.Entities {
		e := &doc.Entities[i]
		ops := recognizeMigrationSchemaOps(e)
		if len(ops) == 0 {
			continue
		}
		repo := entityRepo(doc, e)
		for _, op := range ops {
			norm := normTable(op.table)
			if norm == "" {
				continue // honest-partial: dynamic / unknown table
			}
			stats.OpsConsidered++
			tableID := getTableNode(repo, norm)

			props := map[string]string{
				"op":    op.op,
				"table": norm,
			}
			if op.column != "" {
				props["column"] = op.column
			}
			if addEdge(e.ID, tableID, string(types.RelationshipKindModifiesTable),
				op.op+":"+op.column, props) {
				stats.ModifiesEdges++
			}
		}
	}

	if stats.ModifiesEdges == 0 {
		return MigrationSchemaOpsStats{Skipped: true}
	}

	// Convergence step: rewire query-side accessors onto the same SCOPE.Table.
	// For every SCOPE.DataAccess on a table a migration touched, emit an
	// ACCESSES_TABLE edge to the converged node. This is what makes
	// "what touches table X" unify migrations + queries on ONE node.
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if e.Kind != sharedKindDataAccess {
			continue
		}
		norm := normTable(e.Properties["table"])
		if norm == "" {
			continue
		}
		repo := entityRepo(doc, e)
		key := tk{repo: repo, table: norm}
		tableID, ok := tableNodeID[key]
		if !ok {
			continue // no migration touched this table — leave it untouched
		}
		if addEdge(e.ID, tableID, sharedRelAccessTable, "", map[string]string{
			"table":      norm,
			"provenance": "CONVERGED_ONTO_MIGRATION_TABLE",
		}) {
			stats.AccessConvergedEdges++
		}
	}

	// Deterministic append order (stable across runs) for reproducible output.
	sort.Slice(newTables, func(a, b int) bool { return newTables[a].ID < newTables[b].ID })
	sort.Slice(newEdges, func(a, b int) bool {
		if newEdges[a].Kind != newEdges[b].Kind {
			return newEdges[a].Kind < newEdges[b].Kind
		}
		return newEdges[a].ID < newEdges[b].ID
	})

	doc.Entities = append(doc.Entities, newTables...)
	doc.Relationships = append(doc.Relationships, newEdges...)

	return stats
}

// recognizeMigrationSchemaOps inspects one entity and returns the schema
// operation(s) it represents, or nil when the entity is not a recognised
// migration schema-op. It is deliberately conservative: an op is only
// recognised when the entity's framework/provenance proves it came from a
// MIGRATION context (so a non-migration create_table-looking call is ignored).
func recognizeMigrationSchemaOps(e *graph.Entity) []migrationSchemaOp {
	if e == nil {
		return nil
	}
	p := e.Properties

	switch e.Kind {
	// --- Django: one Migration entity, operations in a JSON array -----------
	case "Migration":
		if e.Subtype != "django" {
			return nil
		}
		return djangoOps(p["operations"])

	// --- Rails / ActiveRecord: SCOPE.Evolution with ddl_operation -----------
	// --- JS knex/typeorm/sequelize/objection: SCOPE.Evolution with table ----
	case string(types.EntityKindEvolution):
		return evolutionOp(e, p)

	// --- Alembic: SCOPE.Schema framework=alembic ----------------------------
	case string(types.EntityKindSchema):
		if p == nil || p["framework"] != "alembic" {
			return nil
		}
		return alembicSchemaOp(e, p)

	// --- Flyway/Liquibase SQL DDL: SCOPE.Datastore table in a migration -----
	case string(types.EntityKindDatastore):
		if p == nil || p["migration_file"] == "" || e.Subtype != "table" {
			return nil
		}
		// CREATE TABLE inside a versioned SQL migration.
		return []migrationSchemaOp{{op: "create_table", table: e.Name}}
	}
	return nil
}

// evolutionOp recognises Rails-AR + JS-ORM SCOPE.Evolution schema ops.
func evolutionOp(e *graph.Entity, p map[string]string) []migrationSchemaOp {
	if p == nil {
		return nil
	}
	// Rails/ActiveRecord migration op (activerecord_deep.go).
	if p["framework"] == "activerecord" {
		op := p["ddl_operation"]
		if op == "" {
			op = e.Subtype
		}
		if op == "" {
			return nil
		}
		table := firstNonEmpty(p["table_name"], p["from_table"])
		if table == "" {
			return nil
		}
		return []migrationSchemaOp{{op: op, table: table, column: p["column_name"]}}
	}
	// JS query-builder / ORM migration op (knex, typeorm, sequelize,
	// objection). These carry `framework` + a `table` property and an op
	// subtype; `migration_op` is the raw method name.
	switch p["framework"] {
	case "knex", "typeorm", "sequelize", "objection", "mikroorm":
		op := e.Subtype
		if op == "" {
			op = p["migration_op"]
		}
		table := p["table"]
		if op == "" || table == "" {
			return nil
		}
		return []migrationSchemaOp{{op: op, table: table, column: p["column"]}}
	// Nim Allographer alter()/drop() migration op (#5029). The op subtype is the
	// already-normalised op name (add_column|drop_column|rename_column|
	// alter_column|drop_table|rename_table) so we read it directly.
	case "allographer":
		op := e.Subtype
		if op == "" {
			return nil
		}
		table := p["table"]
		if table == "" {
			return nil
		}
		return []migrationSchemaOp{{op: op, table: table, column: p["column"]}}
	// Nim Norm createTables/dropTables + raw-DDL migration op (#4991). The op
	// subtype is the normalised op (create_table|drop_table|alter_table); the
	// `table` property is either the literal DDL table name or the MODEL NAME (the
	// model-typed createTables form), the latter bound to its table convergence
	// node by the shared resolver.
	case "norm":
		op := e.Subtype
		if op == "" {
			return nil
		}
		table := p["table"]
		if table == "" {
			return nil
		}
		return []migrationSchemaOp{{op: op, table: table, column: p["column"]}}
	// Nim Debby createTable/dropTable(Model) migration op (#5367). The op subtype
	// is the normalised op (create_table|drop_table); the `table` property is the
	// MODEL NAME (Debby's table identity = the model type name, debby_orm.go),
	// bound to its table convergence node by the shared resolver.
	case "debby":
		op := e.Subtype
		if op == "" {
			return nil
		}
		table := p["table"]
		if table == "" {
			return nil
		}
		return []migrationSchemaOp{{op: op, table: table, column: p["column"]}}
	}
	return nil
}

// alembicSchemaOp maps an Alembic SCOPE.Schema entity (table/column/index) to
// its schema operation. The alembic extractor records pattern_type +
// table_name/parent_table.
func alembicSchemaOp(e *graph.Entity, p map[string]string) []migrationSchemaOp {
	switch p["pattern_type"] {
	case "table":
		if t := p["table_name"]; t != "" {
			return []migrationSchemaOp{{op: "create_table", table: t}}
		}
	case "column":
		// add_column: the column is attributed to its parent table. The
		// alembic extractor emits column entities both for create_table bodies
		// and for op.add_column; we model both as add_column on the parent
		// table. parent_table is always set; the bare column is the suffix of
		// the entity Name ("<table>.<col>").
		if t := p["parent_table"]; t != "" {
			col := p["column_name"]
			if col == "" {
				if idx := strings.LastIndex(e.Name, "."); idx >= 0 && idx+1 < len(e.Name) {
					col = e.Name[idx+1:]
				}
			}
			return []migrationSchemaOp{{op: "add_column", table: t, column: col}}
		}
	case "index":
		if t := p["parent_table"]; t != "" {
			return []migrationSchemaOp{{op: "create_index", table: t}}
		}
	}
	return nil
}

// djangoOpOperation is one element of the Django migration `operations` JSON
// array emitted by django_migration.go.
type djangoOpOperation struct {
	Type  string `json:"type"`
	Model string `json:"model"`
	Field string `json:"field"`
}

// djangoOps decodes the Django `operations` JSON property into schema ops.
// Django addresses tables by MODEL name (the ORM derives the table name from
// it); we use the model name as the convergence key. Dynamic/unknown models
// (empty) are skipped.
func djangoOps(raw string) []migrationSchemaOp {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var ops []djangoOpOperation
	if err := json.Unmarshal([]byte(raw), &ops); err != nil {
		return nil
	}
	var out []migrationSchemaOp
	for _, o := range ops {
		op := djangoOpType(o.Type)
		if op == "" {
			continue
		}
		table := firstNonEmpty(o.Model, "")
		if table == "" {
			continue
		}
		out = append(out, migrationSchemaOp{op: op, table: table, column: o.Field})
	}
	return out
}

// djangoOpType maps a Django operation class name to the shared op taxonomy.
func djangoOpType(t string) string {
	switch t {
	case "CreateModel":
		return "create_table"
	case "DeleteModel":
		return "drop_table"
	case "AddField":
		return "add_column"
	case "RemoveField":
		return "drop_column"
	case "AlterField":
		return "alter_column"
	case "RenameField":
		return "rename_column"
	case "RenameModel":
		return "rename_table"
	case "AddIndex":
		return "create_index"
	case "RemoveIndex":
		return "drop_index"
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
