<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.groovy.orm.gorm` — GORM (Grails ORM)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [groovy](../by-language/groovy.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | GORM is the persistence layer of Grails. A persisted entity is a domain class (conventionally under grails-app/domain/) declaring typed persistent fields plus the GORM static DSL (hasMany/belongsTo/hasOne/constraints/mapping). custom/groovy/gorm_orm.go recognises each domain class (candidacy = path under grails-app/domain/ OR a static GORM DSL marker in the body) and emits one SCOPE.Schema/model + one SCOPE.Schema/table per domain (framework=gorm+provenance); table identity = explicit static mapping table arg else the class name. Proven by TestGormORM_DomainModelTableColumnsAssoc/ImplicitTableName/RecognisedByStaticMarkerOutsideDomainDir/NonDomainNoop. |
| Model lifecycle extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | GORM event/lifecycle hooks declared in a domain class (def beforeInsert/beforeUpdate/beforeDelete/beforeValidate/afterInsert/afterUpdate/afterDelete/afterLoad/onLoad/onSave) — the Grails analogue of JPA @PrePersist / ActiveRecord before_save — each emit a SCOPE.Operation/function entity stamping callback_type + owning model + framework=gorm (gormLifecycleRe), mirroring the crystal/granite callback shape. Proven by TestGormORM_LifecycleHooks (Book.beforeInsert, Book.afterUpdate). |
| Schema extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | Each typed persistent field (String title, BigDecimal price, Integer pages, Date dateCreated) becomes a SCOPE.Schema/column stamping column_type+model (gormFieldRe); the static GORM DSL members are excluded. Honest exclusions: per-column mapping overrides (column:/type:) beyond the table name, embedded/composite-id options, and constraints validation rules are not modelled. Proven by TestGormORM_DomainModelTableColumnsAssoc. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | static hasMany=[books:Book] / belongsTo=[author:Author] / hasOne=[profile:Profile] map declarations (gormStaticAssocRe) each emit a SCOPE.Schema/association entity (assoc_kind+target), plus the single-class static belongsTo=Author form (gormBelongsToClassRe). Proven by TestGormORM_HasManyHasOne + TestGormORM_SingleClassBelongsTo. |
| Foreign key extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | Each belongsTo entry (FK-owning side in GORM) yields a REFERENCES edge model->target stamping fk_field (association name)+to_model, for both the map form and the single-class form. hasMany/hasOne (non-owning) emit NO REFERENCES edge. Cross-file target resolution delegated to the shared resolver via the bare class name. Proven by TestGormORM_DomainModelTableColumnsAssoc (Book->Author), TestGormORM_SingleClassBelongsTo (Chapter->Book), hasMany negative assertion. |
| Lazy loading recognition | — `not_applicable` | — | — | — | GORM associations are lazy by default but the lazy/eager toggle lives in the static mapping/fetchMode DSL, not a per-association annotation; no dedicated lazy-load marker is recognised at the association-entity layer. |
| Relationship extraction | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | hasMany/belongsTo/hasOne are extracted as association entities; belongsTo additionally yields a REFERENCES FK edge. See association_extraction/foreign_key_extraction. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | A GORM query DSL call site referencing a known domain — dynamic finders (findByX/findAllByX/countByX/getByX), static query methods (get/read/list/findAll/count/findWhere/withCriteria/createCriteria/executeQuery/exists) and persistence verbs (save/merge->insert, delete->delete) — emits a QUERIES edge model->table stamped operation+table+model (gormQueryRe+gormQueryOp). Only receivers naming a domain declared in the file are attributed (honest, file-local). Proven by TestGormORM_QueryAttribution (Unknown skipped). |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | 🔴 `missing` | — | 5364 | — | GORM/Grails schema evolution is driven by the dbCreate config (create/update/validate) and the Grails Database Migration plugin (Liquibase changelogs), neither of which lives in the domain class — not parsed yet; honest follow-up. |
| Migration schema ops | 🔴 `missing` | — | 5364 | — | No migration->table schema-op convergence is emitted for GORM yet (no in-domain migration DSL to read; Liquibase changelog parsing is a separate cross-manifest concern). Honest follow-up. |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | ✅ `full` | `2026-06-24` | — | `internal/custom/groovy/gorm_orm.go`<br>`internal/custom/groovy/gorm_orm_test.go` | A Domain.withTransaction { } block whose receiver names a known domain emits one SCOPE.Pattern/transaction_boundary entity (transactional=true, framework=gorm, db_handle), mirroring the crystal/granite + Kotlin/Java @Transactional shape (gormTxRe). Proven by TestGormORM_Transaction. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.groovy.orm.gorm ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
