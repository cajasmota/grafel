<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.spring-data-redis` — Spring Data Redis

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 11

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-06-11` | [link](https://github.com/cajasmota/archigraph/issues/4283) | `internal/extractors/java/nosql_model.go`<br>`internal/extractors/java/nosql_model_test.go` | Native Go pass (#4283): a class-level @RedisHash("people") emits a SCOPE.Schema model entity (Subtype "schema") named after the class (the aggregate root), keyspace on the `keyspace` property and `store=redis`. Field membership reuses the base extractor's SCOPE.Schema/field children via CONTAINS (BuildSchemaFieldStructuralRef); @Id/@Indexed flags recorded in model Properties (field.<n>.id / field.<n>.indexed). Complements the existing scanJavaSpringDataRedis QUERIES keyspace edge (#3643). Honest limit: dynamic/non-literal keyspace -> model emitted without a `keyspace` property. |
| Model lifecycle extraction | 🔴 `missing` | — | 3628 | — | — |
| Schema extraction | 🔴 `missing` | `2026-05-29` | [link](https://github.com/cajasmota/archigraph/issues/3586) | `internal/custom/java/extractors_test.go`<br>`internal/custom/java/spring_ecosystem.go` | — |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Foreign key extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Lazy loading recognition | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |
| Relationship extraction | — `not_applicable` | — | 3095 | — | NoSQL store has no relational join/FK/lazy-load concept; not_applicable by design. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-06-06` | [link](https://github.com/cajasmota/archigraph/issues/4271) | `internal/engine/orm_queries.go`<br>`internal/engine/orm_queries_drivers_other.go`<br>`internal/engine/orm_queries_drivers_other_test.go` | scanJavaSpringDataRedis attributes RedisTemplate/StringRedisTemplate key-value access to the KEYSPACE the command touches (Redis is key-value: no table/collection). javaRedisOpsKeyRe matches opsForValue()/opsForHash()/opsForList()/...get|set|put|delete|leftPush|... ("user:42") and redisTemplate.delete("user:42"); @RedisHash("people") aggregate roots map to their keyspace. The keyspace = the key prefix before the first ':' (else the whole key) via redisKeyspaceFromLiteral + capitalisedSingular. QUERIES edge caller->Class:<keyspace>, orm=redis, op from the accessor method mapped to a command verb (get->find, set/put/push->create, increment/expire->update, delete->delete). Dynamic / variable / interpolated keys honest-skipped (only quoted literals captured). Value-asserting tests TestDriver_JavaRedisOpsForValueGet (load->Class:User find), TestDriver_JavaRedisOpsForValueSet (save->Class:Session create), TestDriver_JavaRedisOpsForHashGet (field->Class:User find), TestDriver_JavaRedisTemplateDelete (evict->Class:User delete), TestDriver_JavaRedisHashEntity (Person->Class:People); negative TestDriver_JavaRedisDynamicKeySkipped. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | `2026-05-29` | — | — | ORM model-definition layer; database migration files are owned by Flyway/Liquibase, not the ORM itself. Same rationale as lang.java.orm.jooq and lang.java.orm.neo4j N/A. |
| Migration schema ops | 🔴 `missing` | — | 3628 | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Related extraction records

This record provides code-level coverage for the
[`db.redis`](./db.redis.md) hub record (Redis (keys)),
which tracks the same technology at a higher level.

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.spring-data-redis ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
