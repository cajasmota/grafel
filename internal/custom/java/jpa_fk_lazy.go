package java

// jpa_fk_lazy.go — JPA/Hibernate/EclipseLink/Ebean/Spring-Data-JPA
//
// Adds foreign_key_extraction and lazy_loading_recognition capability to the
// five JPA-family ORM extractors. Instead of duplicating each extractor's
// existing logic, this file provides:
//
//   - Shared regex patterns for @JoinColumn, @ForeignKey, FetchType enum,
//     and @Column(name/nullable/length) depth.
//   - ExtractJPAFKAndLazy — a single helper that accepts parsed source and
//     emits SCOPE.Component "foreign_key" and "fetch_config" entities.
//     Each existing extractor (hibernate.go, eclipselink.go, ebean.go,
//     spring_ecosystem.go, and the JPA default path) calls this helper after
//     its own entity/association pass.
//
// Supported patterns
// ------------------
//   @JoinColumn(name="col", referencedColumnName="id", nullable=false)
//   @JoinColumn(foreignKey = @ForeignKey(name="fk_order_customer"))
//   @ForeignKey(name="fk_order_customer")
//   @OneToMany(fetch = FetchType.LAZY)
//   @ManyToOne(fetch = FetchType.EAGER)
//   @Column(name="email", nullable=false, length=255)

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

var (
	// @JoinColumn(name="col" ...) — captures the column name value.
	jpaJoinColumnRE = regexp.MustCompile(
		`@JoinColumn\s*\([^)]*\bname\s*=\s*"([^"]+)"[^)]*\)`)

	// @JoinColumn with embedded @ForeignKey(name="fk_name") — captures FK constraint name.
	jpaForeignKeyInJoinColumnRE = regexp.MustCompile(
		`@JoinColumn\s*\([^)]*foreignKey\s*=\s*@ForeignKey\s*\([^)]*\bname\s*=\s*"([^"]+)"[^)]*\)[^)]*\)`)

	// Standalone @ForeignKey(name="fk_name") — captures FK constraint name.
	jpaForeignKeyRE = regexp.MustCompile(
		`@ForeignKey\s*\(\s*(?:[^)]*\bname\s*=\s*"([^"]+)"|"([^"]+)")[^)]*\)`)

	// FetchType.LAZY or FetchType.EAGER inside any annotation argument.
	jpaFetchTypeRE = regexp.MustCompile(
		`\bfetch\s*=\s*FetchType\.(LAZY|EAGER)`)

	// @Column(name="col", nullable=false, length=255) — captures name, nullable, length.
	jpaColumnRE = regexp.MustCompile(
		`@Column\s*\([^)]*\)`)
	jpaColumnNameRE     = regexp.MustCompile(`\bname\s*=\s*"([^"]+)"`)
	jpaColumnNullableRE = regexp.MustCompile(`\bnullable\s*=\s*(true|false)`)
	jpaColumnLengthRE   = regexp.MustCompile(`\blength\s*=\s*(\d+)`)
)

// JPAFKLazyResult holds the entities emitted by ExtractJPAFKAndLazy.
type JPAFKLazyResult struct {
	// ForeignKeys holds one entry per @JoinColumn / @ForeignKey occurrence.
	ForeignKeys []JPAForeignKeyEntry
	// FetchConfigs holds one entry per FetchType.LAZY / FetchType.EAGER occurrence.
	FetchConfigs []JPAFetchConfigEntry
	// Columns holds one entry per @Column occurrence.
	Columns []JPAColumnEntry
}

// JPAForeignKeyEntry represents a detected @JoinColumn / @ForeignKey.
type JPAForeignKeyEntry struct {
	ColumnName     string // value of name= in @JoinColumn, or ""
	ConstraintName string // value of name= in @ForeignKey, or ""
	OwnerEntity    string // nearest preceding @Entity class name, or ""
	Line           int
}

// JPAFetchConfigEntry represents a detected FetchType.LAZY / FetchType.EAGER.
type JPAFetchConfigEntry struct {
	FetchType   string // "LAZY" or "EAGER"
	OwnerEntity string
	Line        int
}

// JPAColumnEntry represents a detected @Column with attribute depth.
type JPAColumnEntry struct {
	ColumnName string // name= value or ""
	Nullable   string // "true" / "false" / ""
	Length     string // numeric string or ""
	OwnerEntity string
	Line        int
}

// ExtractJPAFKAndLazy scans src for JPA FK / fetch-type / column annotations
// and returns structured entries. It is framework-agnostic — each framework's
// extractor passes its own src and an ownerFn that resolves the enclosing
// @Entity class name from a byte offset.
func ExtractJPAFKAndLazy(src string, ownerFn func(offset int) string) JPAFKLazyResult {
	var result JPAFKLazyResult

	// --- @ForeignKey(name=) embedded inside @JoinColumn ---
	for _, m := range jpaForeignKeyInJoinColumnRE.FindAllStringSubmatchIndex(src, -1) {
		constraintName := src[m[2]:m[3]]
		// Also try to pick up the column name from the same @JoinColumn.
		colName := ""
		if cm := jpaJoinColumnRE.FindStringSubmatchIndex(src[m[0]:m[1]]); cm != nil {
			colName = src[m[0]+cm[2] : m[0]+cm[3]]
		}
		result.ForeignKeys = append(result.ForeignKeys, JPAForeignKeyEntry{
			ColumnName:     colName,
			ConstraintName: constraintName,
			OwnerEntity:    ownerFn(m[0]),
			Line:           lineOf(src, m[0]),
		})
	}

	// --- plain @JoinColumn(name="col") ---
	for _, m := range jpaJoinColumnRE.FindAllStringSubmatchIndex(src, -1) {
		colName := src[m[2]:m[3]]
		// Skip if already captured above as part of @ForeignKey-embedded.
		result.ForeignKeys = append(result.ForeignKeys, JPAForeignKeyEntry{
			ColumnName:  colName,
			OwnerEntity: ownerFn(m[0]),
			Line:        lineOf(src, m[0]),
		})
	}

	// --- standalone @ForeignKey(name=) not embedded in @JoinColumn ---
	for _, m := range jpaForeignKeyRE.FindAllStringSubmatchIndex(src, -1) {
		var constraintName string
		if m[2] >= 0 {
			constraintName = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			constraintName = src[m[4]:m[5]]
		}
		if constraintName == "" {
			continue
		}
		result.ForeignKeys = append(result.ForeignKeys, JPAForeignKeyEntry{
			ConstraintName: constraintName,
			OwnerEntity:    ownerFn(m[0]),
			Line:           lineOf(src, m[0]),
		})
	}

	// --- FetchType.LAZY / FetchType.EAGER ---
	for _, m := range jpaFetchTypeRE.FindAllStringSubmatchIndex(src, -1) {
		fetchType := src[m[2]:m[3]]
		result.FetchConfigs = append(result.FetchConfigs, JPAFetchConfigEntry{
			FetchType:   fetchType,
			OwnerEntity: ownerFn(m[0]),
			Line:        lineOf(src, m[0]),
		})
	}

	// --- @Column(...) depth ---
	for _, m := range jpaColumnRE.FindAllStringSubmatchIndex(src, -1) {
		snippet := src[m[0]:m[1]]
		var colName, nullable, length string
		if nm := jpaColumnNameRE.FindStringSubmatch(snippet); nm != nil {
			colName = nm[1]
		}
		if nu := jpaColumnNullableRE.FindStringSubmatch(snippet); nu != nil {
			nullable = nu[1]
		}
		if le := jpaColumnLengthRE.FindStringSubmatch(snippet); le != nil {
			length = le[1]
		}
		// Only emit if we captured at least one attribute.
		if colName == "" && nullable == "" && length == "" {
			continue
		}
		result.Columns = append(result.Columns, JPAColumnEntry{
			ColumnName:  colName,
			Nullable:    nullable,
			Length:      length,
			OwnerEntity: ownerFn(m[0]),
			Line:        lineOf(src, m[0]),
		})
	}

	return result
}

// emitJPAFKLazy converts a JPAFKLazyResult into SecondaryEntity records (old-style
// PatternResult path used by hibernate.go / spring_ecosystem.go) and appends them
// to *out. framework identifies the emitting ORM (e.g. "hibernate").
func emitJPAFKLazy(res JPAFKLazyResult, fp, language, framework string, out *[]SecondaryEntity, seen map[string]bool) {
	for _, fk := range res.ForeignKeys {
		label := fk.ColumnName
		if label == "" {
			label = fk.ConstraintName
		}
		if label == "" {
			continue
		}
		ref := "scope:component:jpa_fk:" + fp + ":" + fk.OwnerEntity + ":" + label + ":" + itoa(fk.Line)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		props := map[string]any{
			"framework":  framework,
			"provenance": "INFERRED_FROM_JPA_JOIN_COLUMN",
		}
		if fk.ColumnName != "" {
			props["column_name"] = fk.ColumnName
		}
		if fk.ConstraintName != "" {
			props["constraint_name"] = fk.ConstraintName
		}
		if fk.OwnerEntity != "" {
			props["owner_entity"] = fk.OwnerEntity
		}
		*out = append(*out, SecondaryEntity{
			Name:       label,
			Kind:       "SCOPE.Component",
			SourceFile: fp,
			LineStart:  fk.Line,
			LineEnd:    fk.Line,
			Provenance: "INFERRED_FROM_JPA_JOIN_COLUMN",
			Ref:        ref,
			Properties: props,
		})
	}

	for _, fc := range res.FetchConfigs {
		ref := "scope:component:jpa_fetch:" + fp + ":" + fc.OwnerEntity + ":" + fc.FetchType + ":" + itoa(fc.Line)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		props := map[string]any{
			"framework":  framework,
			"fetch_type": fc.FetchType,
			"provenance": "INFERRED_FROM_JPA_FETCH_TYPE",
		}
		if fc.OwnerEntity != "" {
			props["owner_entity"] = fc.OwnerEntity
		}
		*out = append(*out, SecondaryEntity{
			Name:       "fetch:" + fc.FetchType,
			Kind:       "SCOPE.Component",
			SourceFile: fp,
			LineStart:  fc.Line,
			LineEnd:    fc.Line,
			Provenance: "INFERRED_FROM_JPA_FETCH_TYPE",
			Ref:        ref,
			Properties: props,
		})
	}

	for _, col := range res.Columns {
		label := col.ColumnName
		if label == "" {
			label = "col@" + itoa(col.Line)
		}
		ref := "scope:component:jpa_column:" + fp + ":" + col.OwnerEntity + ":" + label + ":" + itoa(col.Line)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		props := map[string]any{
			"framework":  framework,
			"provenance": "INFERRED_FROM_JPA_COLUMN",
		}
		if col.ColumnName != "" {
			props["column_name"] = col.ColumnName
		}
		if col.Nullable != "" {
			props["nullable"] = col.Nullable
		}
		if col.Length != "" {
			props["length"] = col.Length
		}
		if col.OwnerEntity != "" {
			props["owner_entity"] = col.OwnerEntity
		}
		*out = append(*out, SecondaryEntity{
			Name:       label,
			Kind:       "SCOPE.Component",
			SourceFile: fp,
			LineStart:  col.Line,
			LineEnd:    col.Line,
			Provenance: "INFERRED_FROM_JPA_COLUMN",
			Ref:        ref,
			Properties: props,
		})
	}
}

// emitJPAFKLazyRegistry converts a JPAFKLazyResult into registry-shaped
// types.EntityRecord records (new-style path used by eclipselink.go /
// ebean.go) and appends them via the provided add callback.
func emitJPAFKLazyRegistry(res JPAFKLazyResult, fp, language, framework string, add func(types.EntityRecord)) {
	for _, fk := range res.ForeignKeys {
		label := fk.ColumnName
		if label == "" {
			label = fk.ConstraintName
		}
		if label == "" {
			continue
		}
		ent := makeEntity(label, "SCOPE.Component", "foreign_key", fp, language, fk.Line)
		kvs := []string{"framework", framework, "provenance", "INFERRED_FROM_JPA_JOIN_COLUMN"}
		if fk.ColumnName != "" {
			kvs = append(kvs, "column_name", fk.ColumnName)
		}
		if fk.ConstraintName != "" {
			kvs = append(kvs, "constraint_name", fk.ConstraintName)
		}
		if fk.OwnerEntity != "" {
			kvs = append(kvs, "owner_entity", fk.OwnerEntity)
		}
		setProps(&ent, kvs...)
		add(ent)
	}

	for _, fc := range res.FetchConfigs {
		ent := makeEntity("fetch:"+fc.FetchType, "SCOPE.Component", "fetch_config", fp, language, fc.Line)
		kvs := []string{"framework", framework, "fetch_type", fc.FetchType, "provenance", "INFERRED_FROM_JPA_FETCH_TYPE"}
		if fc.OwnerEntity != "" {
			kvs = append(kvs, "owner_entity", fc.OwnerEntity)
		}
		setProps(&ent, kvs...)
		add(ent)
	}

	for _, col := range res.Columns {
		label := col.ColumnName
		if label == "" {
			label = "col@" + itoa(col.Line)
		}
		ent := makeEntity(label, "SCOPE.Component", "column", fp, language, col.Line)
		kvs := []string{"framework", framework, "provenance", "INFERRED_FROM_JPA_COLUMN"}
		if col.ColumnName != "" {
			kvs = append(kvs, "column_name", col.ColumnName)
		}
		if col.Nullable != "" {
			kvs = append(kvs, "nullable", col.Nullable)
		}
		if col.Length != "" {
			kvs = append(kvs, "length", col.Length)
		}
		if col.OwnerEntity != "" {
			kvs = append(kvs, "owner_entity", col.OwnerEntity)
		}
		setProps(&ent, kvs...)
		add(ent)
	}
}
