<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.cobol.core` — COBOL

Auto-generated. Back to [summary](../summary.md).

- **Language:** [COBOL](../by-language/cobol.md)
- **Category:** [language](../by-category/language.md)
- **Capability cells:** 3

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Call line precision | ✅ `full` | `2026-06-12` | 2743 | `internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go` | Every CALLS edge carries Properties["line"] (1-based physical line). PERFORM <paragraph> emits an intra-program CALLS (via=PERFORM); PERFORM <a> THRU/THROUGH <b> additionally emits a CALLS to the range-end paragraph (via=PERFORM-THRU range_start=<a>, #4946); GO TO <para> (and the GO TO a b c DEPENDING ON x multi-target form) emits an intra-program control-flow CALLS per target (via=GO-TO, #4946); CALL '<program>' emits an inter-program CALLS (via=CALL external=true); CALL <data-item> a dynamic CALLS (via=CALL dynamic_ref=true). Proven by TestExtractor_PerformIsIntraCall / _PerformThruRange / _GoToControlFlow / _CallIsExternal. WEAK (follow-up #5040): dynamic CALL <identifier> ToID is still the bare variable name — MOVE-literal data-flow tracing to recover the program-id literal is not yet implemented, so the target binding for dynamic calls stays best-effort. |
| Core extraction | ✅ `full` | `2026-06-12` | 2743 | `internal/classifier/classifier.go`<br>`internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/payroll.cbl` | Line/column-oriented fixed+free-format parser (no tree-sitter COBOL grammar; mirrors jcl/verilog precedent). stripSequenceArea honours cols 1-6 sequence area + col-7 indicator (*//-/D comment+continuation), bounds cols 8-72. Emits: PROGRAM-ID → SCOPE.Component/program; IDENTIFICATION/ENVIRONMENT/DATA/PROCEDURE → SCOPE.Component/division; <NAME> SECTION → SCOPE.Component/section; PROCEDURE paragraph header → SCOPE.Operation/paragraph (reserved-word + all-digit gated); COPY → SCOPE.Component/copybook placeholder. Data hierarchy (#2838): 01/05/10/77/66/88 level items → SCOPE.Schema/field with a parent-level stack wiring CONTAINS group→field + parent/level/pic/redefines/occurs/group props; FILLER skipped; copybook (.cpy) data items extracted div-context-free. Proven by TestExtractor_ProgramAndDivisions / _Paragraphs / _DataItems / _DataHierarchy / _DataRedefinesOccurs. Control flow (#4946): PERFORM THRU/THROUGH range-end + GO TO (incl. DEPENDING ON) now emit CALLS edges (see call_line_precision); mutation effect expanded beyond MOVE/SET/COMPUTE to ADD|SUBTRACT|MULTIPLY|DIVIDE..GIVING, STRING|UNSTRING..INTO, INITIALIZE, INSPECT..REPLACING (proven by TestSniffEffectsCobol_MutationExpanded). |
| Import resolution quality | ✅ `full` | `2026-06-12` | 2838 | `internal/extractors/cobol/depth.go`<br>`internal/extractors/cobol/extractor.go`<br>`internal/extractors/cobol/extractor_test.go`<br>`internal/extractors/cobol/testdata/emprec.cpy` | COPY <book> → IMPORTS import edge. resolveCopybook (#2838) probes the using-program dir + conventional copybook sub-dirs (copybook/copybooks/copylib/cpy/include/copy) across multiple extensions (.cpy/.cbl/.cob/...) and case variants (as-written/UPPER/lower — COBOL COPY names are case-insensitive); on a hit the IMPORTS ToID binds to the resolved on-disk file path, else the bare name (still emitted, a placeholder copybook entity keeps it resolvable). REPLACING ==a==BY==b== pseudo-text pairs are parsed into a structured replacing_pairs "a=>b;..." property for copybook-drift analysis. Proven by TestExtractor_CopyIsImport / _CopybookResolution / _CopybookUnresolved / _CopybookReplacing. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.cobol.core ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
