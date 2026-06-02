// Package lifecycle provides shared, pure detection of ORM model
// data-lifecycle traits — soft-delete, created/updated timestamps, and
// created-by/updated-by audit columns — so per-language ORM extractors can
// stamp a uniform set of flat properties onto the model entity they already
// emit. This lets the graph answer "which models soft-delete?" and "which
// track timestamps?" for rewrite data-lifecycle parity.
//
// HONESTY BOUNDARY: detection requires a recognised soft-delete library /
// column convention or an explicit timestamp/audit column name. It never
// guesses soft-delete from an arbitrary "deleted"-named boolean — a plain
// `deleted` flag with no paranoia scope / library / `deleted_at`-style column
// is NOT reported as soft-delete. Ambiguous signals are omitted (honest
// partial) rather than asserted.
package lifecycle

import (
	"regexp"
	"strings"
)

// Traits is the resolved data-lifecycle trait set for one ORM model. Zero
// value means "no recognised lifecycle traits" — every field is then omitted
// from the entity properties by Stamp.
type Traits struct {
	SoftDelete       bool     // model performs soft-deletes
	SoftDeleteColumn string   // the soft-delete marker column, when known
	Timestamps       bool     // model tracks created/updated timestamps
	AuditColumns     []string // created_by / updated_by style audit columns
}

// PropSetter is the minimal interface the per-language extractors expose for
// stamping flat string properties (matches custom.setProps semantics: even
// key/value pairs). Implemented inline by each caller via a closure.
type PropSetter func(kv ...string)

// Stamp writes the recognised traits as flat properties via set. Only
// asserted traits are written, so a model with no recognised lifecycle traits
// gets no lifecycle properties at all (honest absence).
func (t Traits) Stamp(set PropSetter) {
	if t.SoftDelete {
		set("soft_delete", "true")
		if t.SoftDeleteColumn != "" {
			set("soft_delete_column", t.SoftDeleteColumn)
		}
	}
	if t.Timestamps {
		set("timestamps", "true")
	}
	if len(t.AuditColumns) > 0 {
		set("audit_columns", strings.Join(t.AuditColumns, ","))
	}
}

// auditColumnNames is the closed convention set for created-by/updated-by
// audit columns. We require an explicit, conventional name — we do NOT infer
// audit semantics from arbitrary "*_by" identifiers.
var auditColumnNames = []string{
	"created_by", "updated_by", "creator_id", "updater_id",
	"deleted_by", "created_by_id", "updated_by_id",
}

// collectAuditColumns returns the conventional audit columns present in cols,
// preserving auditColumnNames order and de-duplicating. cols are matched
// case-insensitively against the convention set.
func collectAuditColumns(cols []string) []string {
	have := make(map[string]bool, len(cols))
	for _, c := range cols {
		have[strings.ToLower(strings.TrimSpace(c))] = true
	}
	var out []string
	for _, name := range auditColumnNames {
		if have[name] {
			out = append(out, name)
		}
	}
	return out
}

// --- GORM (Go) -------------------------------------------------------------

// GORMInput carries the GORM-model facts a Go extractor has already parsed:
// whether the struct embeds gorm.Model (which contributes CreatedAt /
// UpdatedAt / DeletedAt), the column name of any `gorm.DeletedAt`-typed field,
// and the resolved column names of every field on the struct.
type GORMInput struct {
	EmbedsGormModel bool     // struct embeds gorm.Model
	DeletedAtColumn string   // column of a gorm.DeletedAt-typed field, if any
	HasCreatedAt    bool     // a CreatedAt column/field is present
	HasUpdatedAt    bool     // an UpdatedAt column/field is present
	Columns         []string // all resolved column names on the struct
}

// GORM resolves lifecycle traits for a GORM model.
//
//   - gorm.Model embed contributes DeletedAt (soft-delete, column deleted_at)
//     AND CreatedAt+UpdatedAt (timestamps).
//   - an explicit gorm.DeletedAt field is soft-delete with that field's column.
//   - explicit CreatedAt + UpdatedAt columns (without the embed) are timestamps.
func GORM(in GORMInput) Traits {
	var t Traits
	switch {
	case in.DeletedAtColumn != "":
		t.SoftDelete = true
		t.SoftDeleteColumn = in.DeletedAtColumn
	case in.EmbedsGormModel:
		// gorm.Model embeds `DeletedAt gorm.DeletedAt` → column deleted_at.
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	}
	if in.EmbedsGormModel || (in.HasCreatedAt && in.HasUpdatedAt) {
		t.Timestamps = true
	}
	t.AuditColumns = collectAuditColumns(in.Columns)
	return t
}

// --- ActiveRecord (Ruby) ---------------------------------------------------

// RailsModelTraits resolves lifecycle traits from the body of a Rails model
// class. timestamps live in the migration/schema (not the model body) so this
// stays honest-partial on timestamps — it only asserts soft-delete and audit
// columns, both of which are observable in the model source:
//
//   - `acts_as_paranoid` (paranoia / acts_as_paranoid gems) → soft-delete.
//   - `default_scope { where(deleted_at: nil) }` (and variants) → soft-delete,
//     column deleted_at.
//
// A bare `deleted` boolean with no scope/lib is NOT soft-delete. columns are
// any conventional audit columns referenced in the model body (e.g. via a
// belongs_to or attribute reference); when none are observable the list is
// empty (honest partial — audit columns usually live in the schema).
func RailsModelTraits(body string, columns []string) Traits {
	var t Traits
	low := body

	if strings.Contains(low, "acts_as_paranoid") {
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	}
	// default_scope { where(deleted_at: nil) } / where("deleted_at IS NULL")
	if !t.SoftDelete && railsSoftDeleteScope(low) {
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	}
	t.AuditColumns = collectAuditColumns(columns)
	return t
}

// railsSoftDeleteScope reports whether the model body declares a default scope
// that filters out soft-deleted rows on a deleted_at column. Requires both the
// default_scope macro AND a deleted_at predicate so an unrelated default_scope
// (e.g. ordering) is not mistaken for soft-delete.
func railsSoftDeleteScope(body string) bool {
	if !strings.Contains(body, "default_scope") {
		return false
	}
	return strings.Contains(body, "deleted_at: nil") ||
		strings.Contains(body, "deleted_at IS NULL") ||
		strings.Contains(body, "deleted_at is null")
}

// --- TypeORM (TypeScript) --------------------------------------------------

// reTypeORMDeleteDateColumn captures the property name of a @DeleteDateColumn()
// decorated field — TypeORM's first-class soft-delete marker. The column name
// is the property identifier (TypeORM defaults the DB column to it).
var reTypeORMDeleteDateColumn = regexp.MustCompile(
	`@DeleteDateColumn\s*\([^)]*\)\s+(\w+)`,
)

// reTypeORMColumnProp captures the property name of a plain @Column()/@Column(...)
// decorated field, used to surface conventional audit columns declared on the
// entity body.
var reTypeORMColumnProp = regexp.MustCompile(
	`@Column\s*\([^)]*\)\s+(\w+)`,
)

// TypeORM resolves lifecycle traits from the body of a single TypeORM @Entity
// class. Detection is decorator-driven (honest):
//
//   - @DeleteDateColumn() deletedAt → soft-delete, column = the property name.
//   - @CreateDateColumn() AND @UpdateDateColumn() both present → timestamps.
//   - conventional @Column() audit fields (created_by/createdBy/…) → audit_columns.
//
// A plain `deleted` boolean @Column with no @DeleteDateColumn is NOT soft-delete.
func TypeORM(body string) Traits {
	var t Traits
	if m := reTypeORMDeleteDateColumn.FindStringSubmatch(body); m != nil {
		t.SoftDelete = true
		t.SoftDeleteColumn = m[1]
	}
	if strings.Contains(body, "@CreateDateColumn") && strings.Contains(body, "@UpdateDateColumn") {
		t.Timestamps = true
	}
	var cols []string
	for _, m := range reTypeORMColumnProp.FindAllStringSubmatch(body, -1) {
		cols = append(cols, m[1])
	}
	t.AuditColumns = collectAuditColumns(normalizeCamelColumns(cols))
	return t
}

// --- Sequelize (TypeScript / JavaScript) -----------------------------------

// reSequelizeBoolOption matches a `<key>: true|false` entry inside a Sequelize
// model options object (the second arg of define()/init()). Group 1 = key,
// group 2 = boolean literal.
var reSequelizeBoolOption = regexp.MustCompile(
	`(?m)\b(paranoid|timestamps)\s*:\s*(true|false)\b`,
)

// SequelizeInput carries the Sequelize model-options facts an extractor parsed:
// the options-object source blob (for paranoid/timestamps flags) and the
// resolved column names declared in the model attributes object.
type SequelizeInput struct {
	OptionsBlob string   // the model-options object source (second define/init arg)
	Columns     []string // attribute/column names declared on the model
}

// Sequelize resolves lifecycle traits for a Sequelize model.
//
//   - `paranoid: true` → soft-delete (column deletedAt unless overridden).
//   - timestamps are ON by default; only `timestamps: false` disables them.
//   - conventional audit columns among the attributes → audit_columns.
//
// paranoid:true with no explicit timestamps:false also implies timestamps
// (Sequelize requires timestamps for paranoid mode).
func Sequelize(in SequelizeInput) Traits {
	var t Traits
	paranoid := false
	timestampsDisabled := false
	for _, m := range reSequelizeBoolOption.FindAllStringSubmatch(in.OptionsBlob, -1) {
		switch m[1] {
		case "paranoid":
			paranoid = m[2] == "true"
		case "timestamps":
			if m[2] == "false" {
				timestampsDisabled = true
			}
		}
	}
	if paranoid {
		t.SoftDelete = true
		t.SoftDeleteColumn = sequelizeDeletedAtOverride(in.OptionsBlob)
	}
	// Timestamps default ON; disabled only by an explicit timestamps:false.
	// paranoid mode forces timestamps on regardless.
	if !timestampsDisabled || paranoid {
		t.Timestamps = true
	}
	t.AuditColumns = collectAuditColumns(normalizeCamelColumns(in.Columns))
	return t
}

// reSequelizeDeletedAtOpt captures a `deletedAt: 'column_name'` override in the
// Sequelize options object, which renames the soft-delete marker column.
var reSequelizeDeletedAtOpt = regexp.MustCompile(
	`deletedAt\s*:\s*['"]([A-Za-z0-9_]+)['"]`,
)

// sequelizeDeletedAtOverride returns the soft-delete column for a paranoid
// model: the `deletedAt: 'x'` override when present, else the Sequelize default
// `deletedAt`.
func sequelizeDeletedAtOverride(optionsBlob string) string {
	if m := reSequelizeDeletedAtOpt.FindStringSubmatch(optionsBlob); m != nil {
		return m[1]
	}
	return "deletedAt"
}

// --- Django (Python) -------------------------------------------------------

// reDjangoField matches a model field assignment line: `name = models.XxxField(`,
// `name = SomethingField(`, or a relation field (`models.ForeignKey(`). Group 1 =
// field name, group 2 = field type.
var reDjangoField = regexp.MustCompile(
	`(?m)^\s+(\w+)\s*=\s*(?:models\.)?(\w*Field|ForeignKey)\s*\(`,
)

// reDjangoSoftDeleteBase matches a django-safedelete base class in the model's
// base list, the conventional library marker for soft-delete.
var reDjangoSoftDeleteBase = regexp.MustCompile(
	`\bSafeDelete(?:Model|MPTTModel|NewManager)?\b`,
)

// Django resolves lifecycle traits from a single Django model class. `bases` is
// the base-class list (between the parens of `class X(...)`), `body` the class
// body. Detection requires a recognised soft-delete convention (honest):
//
//   - a SafeDeleteModel / django-safedelete base → soft-delete.
//   - a `deleted_at` DateTimeField or an `is_deleted` BooleanField → soft-delete.
//   - auto_now_add + auto_now, OR created_at + updated_at DateTimeFields → timestamps.
//   - created_by / updated_by FK fields → audit_columns.
//
// A plain `deleted` boolean with no deleted_at/is_deleted/library is NOT
// soft-delete.
func Django(bases, body string) Traits {
	var t Traits

	fields := map[string]bool{}
	for _, m := range reDjangoField.FindAllStringSubmatch(body, -1) {
		fields[strings.ToLower(m[1])] = true
	}

	switch {
	case reDjangoSoftDeleteBase.MatchString(bases):
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	case fields["deleted_at"]:
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	case fields["is_deleted"]:
		t.SoftDelete = true
		t.SoftDeleteColumn = "is_deleted"
	}

	if (strings.Contains(body, "auto_now_add=True") && strings.Contains(body, "auto_now=True")) ||
		(fields["created_at"] && fields["updated_at"]) {
		t.Timestamps = true
	}

	cols := make([]string, 0, len(fields))
	for f := range fields {
		cols = append(cols, f)
	}
	t.AuditColumns = collectAuditColumns(cols)
	return t
}

// --- SQLAlchemy (Python) ---------------------------------------------------

// reSQLAColumn matches an attribute assigned a Column(...)/mapped_column(...),
// optionally via a Mapped[...] annotation. Group 1 = attribute name. Captures
// both `x = Column(...)` and `x: Mapped[...] = mapped_column(...)`.
var reSQLAColumn = regexp.MustCompile(
	`(?m)^\s+(\w+)\s*(?::\s*Mapped\[[^\]]*\])?\s*=\s*(?:Column|mapped_column)\s*\(`,
)

// reSQLASoftDeleteMixin matches a conventional soft-delete mixin in the base
// list (SoftDeleteMixin / SoftDeleteable / etc.).
var reSQLASoftDeleteMixin = regexp.MustCompile(
	`\bSoftDelete(?:Mixin|able|Model)?\b`,
)

// SQLAlchemy resolves lifecycle traits from a single SQLAlchemy declarative
// model. `bases` is the base-class list, `body` the class body. Detection is
// convention-driven (honest):
//
//   - a SoftDeleteMixin base → soft-delete, column deleted_at.
//   - a `deleted_at` Column/mapped_column → soft-delete with that column.
//   - created_at + updated_at columns whose definitions carry server_default /
//     onupdate (timestamp semantics) → timestamps.
//   - created_by / updated_by columns → audit_columns.
//
// A plain `deleted` boolean Column with no mixin/deleted_at is NOT soft-delete.
func SQLAlchemy(bases, body string) Traits {
	var t Traits

	cols := map[string]bool{}
	for _, m := range reSQLAColumn.FindAllStringSubmatch(body, -1) {
		cols[strings.ToLower(m[1])] = true
	}

	switch {
	case reSQLASoftDeleteMixin.MatchString(bases):
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	case cols["deleted_at"]:
		t.SoftDelete = true
		t.SoftDeleteColumn = "deleted_at"
	}

	// Timestamps require created_at + updated_at columns AND a timestamp-default
	// signal (server_default / default / onupdate) so a pair of plain nullable
	// DateTime columns is not over-claimed.
	if cols["created_at"] && cols["updated_at"] &&
		(strings.Contains(body, "server_default") ||
			strings.Contains(body, "onupdate") ||
			strings.Contains(body, "default=func") ||
			strings.Contains(body, "default=datetime")) {
		t.Timestamps = true
	}

	names := make([]string, 0, len(cols))
	for c := range cols {
		names = append(names, c)
	}
	t.AuditColumns = collectAuditColumns(names)
	return t
}

// normalizeCamelColumns maps camelCase identifiers (createdBy) to the snake_case
// audit-convention names (created_by) so the shared collectAuditColumns set
// matches JS/TS property identifiers as well as snake_case columns.
func normalizeCamelColumns(cols []string) []string {
	out := make([]string, 0, len(cols)*2)
	for _, c := range cols {
		out = append(out, c, camelToSnake(c))
	}
	return out
}

// camelToSnake converts a camelCase identifier to snake_case (createdById →
// created_by_id). ASCII-only, sufficient for the closed audit-convention set.
func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
