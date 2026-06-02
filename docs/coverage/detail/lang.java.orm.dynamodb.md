<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.java.orm.dynamodb` — AWS SDK DynamoDB (Java)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [java](../by-language/java.md)
- **Category:** [orm](../by-category/orm.md)
- **Subcategory:** ORM / Data Mapper
- **Capability cells:** 9

## Capabilities


### Models

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Model extraction | ✅ `full` | `2026-05-29` | 3099 | `internal/custom/java/dynamodb_java.go`<br>`internal/engine/rules/java/orms/dynamodb_java.yaml` | custom_java_dynamodb extractor emits SCOPE.Schema/model for @DynamoDbBean classes; YAML rule gates on Enhanced Client imports. |
| Schema extraction | 🟢 `partial` | `2026-05-29` | 3099 | `internal/custom/java/dynamodb_java.go` | Detects @DynamoDbBean/@DynamoDbPartitionKey/@DynamoDbSortKey/@DynamoDbAttribute for schema attributes; model and query covered by existing YAML rule. |

### Relationships

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Association extraction | — `not_applicable` | — | 3099 | — | DynamoDB is a key-value/document store with no relational ORM associations; this concept does not apply. |
| Foreign key extraction | — `not_applicable` | — | 3099 | — | DynamoDB has no foreign keys; relational FK concepts are not applicable to a key-value store. |
| Lazy loading recognition | — `not_applicable` | — | 3099 | — | DynamoDB Enhanced Client has no lazy-loading concept; items are fetched by key, not via ORM proxies. |
| Relationship extraction | — `not_applicable` | — | 3099 | — | DynamoDB uses single-table design or application-level denormalization; no ORM relationship annotations exist. |

### Queries

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Query attribution | ✅ `full` | `2026-05-29` | 3099 | `internal/engine/rules/java/orms/dynamodb_java.yaml` | YAML rule covers DynamoDbEnhancedClient scan/query operations; query_attribution fully covered by existing rule. |

### Migrations

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Migration parsing | — `not_applicable` | — | — | — | — |

### Transactions

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Transaction function stamping | 🔴 `missing` | — | 3628-transaction-function-stamping | — | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.java.orm.dynamodb ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
