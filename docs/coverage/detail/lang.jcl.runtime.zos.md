<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.jcl.runtime.zos` — IBM z/OS JCL (JES2/JES3)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [JCL](../by-language/jcl.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 4

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 2843 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go` | Every CALLS edge carries Properties["line"] (1-based physical line of the EXEC card): EXEC PGM=, EXEC PROC=/positional proc, and the recovered TSO CALL edge all stamp the line. Proven by TestExtractor_ExecPgmCallsEdge / TestExtractor_TSOCallEdge. |
| Core extraction | ✅ `full` | `2026-06-12` | 2843 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go`<br>`internal/extractors/jcl/testdata/payjob.jcl` | Line-oriented card parser (no tree-sitter JCL grammar; mirrors cobol/verilog precedent). joinStatements collapses col-72-bounded continuation cards into logical statements (trailing-comma signal). Verb switch emits: //NAME JOB → SCOPE.Component/job; //STEP EXEC → SCOPE.Operation/step; //NAME PROC..PEND → SCOPE.Component/proc (inline-proc scope tracking); //DD DSN= → SCOPE.Datastore/dataset. CONTAINS wires job/proc → their steps and step → dataset (ownerForStep follows the open inline PROC, else the JOB). Operand-only (nameless) cards are re-shifted name→verb via isStatementKeyword so `//  INCLUDE MEMBER=X` parses correctly. Proven by TestExtractor_JobAndSteps / _ProcDefinition / _Datasets / _LanguageTagged. |
| Fs effect | ✅ `full` | `2026-06-12` | 4907 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go` | DD statements with a DSN=/DSNAME= operand emit a SCOPE.Datastore/dataset entity (ddDsnRe) and a step→dataset data-flow edge keyed off the DD disposition: DISP=NEW/MOD → WRITES_TO, DISP=OLD/SHR/default → READS_FROM. DSN-less DDs (SYSOUT/DUMMY/instream `*`) are skipped. Proven by TestExtractor_Datasets (PROD.PAYROLL.MASTER read via DISP=SHR, PROD.PAYROLL.RESULTS written via DISP=NEW). WEAK (follow-up #4944): only the first DSN per DD is matched — concatenated DDs, GDG (+1), and DSN=LIB(MEMBER) member granularity are not yet split. |
| Import resolution quality | ✅ `full` | `2026-06-12` | 4907 | `internal/extractors/jcl/extractor.go`<br>`internal/extractors/jcl/extractor_test.go` | Three cross-file/cross-language link kinds, all bound by the by-name resolver with no new linker code. (1) The JCL→COBOL bridge (#2843): EXEC PGM=<prog> emits CALLS via="EXEC PGM" external=true cross_language=cobol whose bare ToID binds to the sibling COBOL PROGRAM-ID — proven end-to-end by TestCrossLanguageBridge_JCLtoCOBOL (resolves PAYROLL across internal/extractors/jcl/testdata/payjob.jcl ↔ ../cobol/testdata/payroll.cbl). (2) NEW in #4907: //  INCLUDE MEMBER=<name> emits IMPORTS import_kind=include — the spliced PROCLIB/JCLLIB member, a real cross-file dep previously dropped (TestExtractor_IncludeImports). (3) NEW in #4907: a TSO terminal-monitor step (PGM=IKJEFT01/IKJEFT1B/IKJEFT1A) has its SYSTSIN instream `CALL 'lib(MEMBER)'` control card scanned (tsoCalledPrograms) to recover the indirect JCL→program edge the shell utility hides — via="TSO CALL" host_program=IKJEFTxx (TestExtractor_TSOCallEdge). EXEC PROC=/positional-proc invocation also emits CALLS via="EXEC PROC". Follow-up #4944 covers IDCAMS/DSNUTILB SYSIN control-card awareness, JCLLIB/SET symbolics, and COND/IF conditional flow. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.jcl.runtime.zos ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
