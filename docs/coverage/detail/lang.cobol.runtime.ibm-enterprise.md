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
| DB effect | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/imsparts.cbl`<br>`internal/substrate/effect_sinks_cobol.go` | Two mainframe data layers are now recognised. (1) Embedded DB2 SQL: EXEC SQL DML effects + SCOPE.DataAccess table/cursor entities with ACCESSES_TABLE edges (see lang.cobol.embedded.db2-sql). (2) NEW in #4948 — IMS DB/DC (DL/I), the 2nd most common mainframe data layer: execStartRe now matches EXEC DLI, and extractDLIEntities (depth.go) turns each EXEC DLI GU|GN|GHU|GNP|GHN|ISRT|REPL|DLET SEGMENT(<seg>) block into a SCOPE.DataAccess segment entity (orm=ims-dli, segment = the hierarchical analog of a relational table) carrying an ACCESSES_TABLE edge FromID=enclosing paragraph (operation mapped GU/GN…→SELECT, ISRT→INSERT, REPL→UPDATE, DLET→DELETE; via=EXEC-DLI-<func>; provenance=INFERRED_FROM_IMS_DLI). The call-level form CALL 'CBLTDLI'/'AIBTDLI' USING <func> <pcb> <io> <ssa> is also handled (extractDLICall): the function-code literal classifies the operation and the segment is recovered from an inline SSA literal ('SEG(KEY=' or bare 'SEG'), with the CALLS edge to the interface module preserved. db_read/db_write effects fire for both forms (effect_sinks_cobol.go cobolDLIReadRe/cobolDLIWriteRe). Proven by TestExtractor_IMSDLISegments / _IMSDLICall. DEFERRED (follow-ups): IO-PCB message-queue GU/ISRT segment binding (no segment name without SSA parse), data-name SSA / data-name function-code resolution via working-storage VALUE tracing, and DBD/PSB (DBDGEN/PSBGEN) hierarchy extraction. |
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
