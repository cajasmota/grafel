<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.embedded.db2-sql` — COBOL Embedded DB2 SQL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| DB effect | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/ledger.cbl`<br>`internal/substrate/effect_sinks_cobol.go` | Beyond the read/write db effect: extractSQLEntities (depth.go) turns each EXEC SQL ... END-EXEC block into resolvable SCOPE.DataAccess entities — one per referenced table (SELECT FROM / INSERT INTO / UPDATE / DELETE FROM / JOIN, host-variable + schema-qualified names tolerated) carrying an ACCESSES_TABLE edge FromID=enclosing paragraph (orm=embedded-sql, operation, table, provenance=INFERRED_FROM_EXEC_SQL), plus DECLARE <name> CURSOR FOR → cursor SCOPE.DataAccess and OPEN/FETCH/CLOSE <cursor> REFERENCES traffic. Proven by TestExtractor_EmbeddedSQLTables / _EmbeddedSQLCursor. The block accumulator buffers multi-line statements to END-EXEC so wrapped DML is captured. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.embedded.db2-sql ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
