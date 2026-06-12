<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.runtime.ibm-enterprise` — IBM Enterprise COBOL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 6

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-05-28` | 2743 | `internal/extractors/cobol/extractor.go` | — |
| Core extraction | ✅ `full` | `2026-05-28` | 2743 | `internal/classifier/classifier.go`<br>`internal/extractors/cobol/extractor.go` | — |
| DB effect | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/substrate/effect_sinks_cobol.go` | Embedded DB2 SQL: EXEC SQL DML effects + SCOPE.DataAccess table/cursor entities with ACCESSES_TABLE edges (see lang.cobol.embedded.db2-sql). WEAK (follow-up #4948): IMS DB/DC (DL/I) — EXEC DLI and CBLTDLI/AIBTDLI CALL (GU/GN/GHU/ISRT/REPL/DLET) segment I/O — is the 2nd most common mainframe data layer and is NOT yet recognised (execStartRe matches only SQL|CICS). |
| Fs effect | ✅ `full` | `2026-06-12` | 4908 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/payroll.cbl`<br>`internal/substrate/effect_sinks_cobol.go` | Native file I/O verbs (READ/OPEN INPUT|I-O/START → fs_read; WRITE/REWRITE/DELETE/OPEN OUTPUT|EXTEND → fs_write) are sniffed and attributed to the enclosing paragraph (effect_sinks_cobol.go). NEW in #4908: ENVIRONMENT ▸ FILE-CONTROL ▸ SELECT <logical> ASSIGN TO <ddname> [ORGANIZATION/ACCESS/RECORD KEY] now emits a resolvable SCOPE.Datastore/file entity (parseFileSelect/buildFileResourceEntity) with assign_to upper-cased to match the JCL DD coupling key, organization/access_mode/record_key props, and storage=vsam for INDEXED/RELATIVE/keyed clusters (else sequential). PROCEDURE-DIVISION OPEN/READ/WRITE on the logical file wire READS_FROM/WRITES_TO edges to that resource, so abstract file effects now bind to a physical dataset / VSAM cluster and shared-VSAM cross-program coupling is visible. Proven by TestExtractor_FileControlSelect / _FileIODataFlow / _VSAMKsds. |
| HTTP effect | ✅ `full` | `2026-05-28` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/substrate/effect_sinks_cobol.go` | — |
| Import resolution quality | ✅ `full` | `2026-05-28` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go` | — |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.runtime.ibm-enterprise ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
