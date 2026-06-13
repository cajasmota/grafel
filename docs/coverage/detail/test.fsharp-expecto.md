<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.fsharp-expecto` — Expecto / xUnit (F#)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [F#](../by-language/fsharp.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🔴 `missing` | — | 3828 | — | No build-graph/target extraction yet for this test-runner; tracked in #3828. |
| Target extraction | ✅ `full` | `2026-06-14` | 5034 | `internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_fsharp.go`<br>`internal/extractors/cross/testmap/frameworks_fsharp_test.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/patterns/assertion_lib_detector.go`<br>`internal/patterns/fsharp_test_records_5114_test.go`<br>`internal/patterns/property_test_detector.go` | #4906: F# test->SUT linkage via the cross-language testmap extractor. detectFSharpExpecto (frameworks_fsharp.go) detects Expecto `testList "Subject" [ testCase "..." <| fun _ -> ... ]` / testCaseAsync / ptestCase / ftestCase leaves AND xUnit/NUnit `[<Fact>]/[<Theory>]/[<Test>] let ``name`` () = ...` attributed bindings (precededByTestAttr binds the attribute to its let). Each case's off-side-rule body (extractNimBlockBody, the shared indentation block extractor) is scanned by the resolver: PAREN-style .NET-interop calls (`OrderService()`, `svc.PlaceOrder(...)`) resolve to high-confidence TESTS edges, and the `testList` subject (or the `XxxTests`-module naming convention via fsharpModuleSubject) seeds a medium-confidence describe-subject edge. The F#/Expecto/Unquote/FsUnit assertion + case-combinator DSL (testCase/Expect.*/should/...) is denylisted in resolver.go so it never surfaces as the production subject. Framework entry is FILENAME/PATH gated (`*Tests.fs` / `Test*.fs` / `_test.fs` / `/tests?/`) since F# uses `open` (not import) — the detector self-confirms (a non-test .fs yields zero cases). Proven by TestFSharpExpecto_SubjectLinkage / _ParenCallHighConfidence, TestFSharpXUnit_AttributedLetBinding, TestFSharp_NonTestFileNoop / _TestFilenameButNoCasesNoop. #5034: F# SPACE-APPLIED function application (`createUser "ada"`, `notify u` — the dominant functional call idiom) is now captured. The resolver runs a gated space-application head-symbol scan (fsharpSpaceAppRE in resolver.go, a faithful port of the base extractor's spaceAppRE: clause-starter prefix + argument-starter follower + lower-case-head restriction) ONLY for tf.lang=="fsharp" (the F# detector now stamps testFunction.lang), so other languages' false-positive rates are untouched; captured heads pass the F# keyword gate AND the shared isStopword denylist (Expecto/FsUnit/xUnit combinators), then the same import-aware high/medium confidence gate as the paren pass. Proven by TestFSharp_SpaceAppHappyPath (high-confidence space-applied edge) / _SpaceAppWrongLanguageNoop (Go body does NOT fire the F# pass) / _SpaceAppNoMatchNoop (only stop-worded combinators present -> no spurious edge). Honest residual (out of scope for regex per #5034): the head scanner does not distinguish partial application from full application (no type/arity resolution), and an upper-case module-qualified space-applied head (`Calc.add 2 3`) is excluded as a likely DU-case/type, mirroring the extractor. #5114 (non-db tail of #4941): F# property-test and assertion-library RECORDS are now emitted as SCOPE.Pattern entities by the pattern detectors — FsCheck (`[<Property>]`/`Check.Quick`/`Prop.forAll`) + Hedgehog (`property { ... }`/`Property.check`) via property_test_detector.go (library=fscheck/hedgehog), and Unquote (`test <@ ... @>`) + FsUnit (`x |> should equal y`) via assertion_lib_detector.go (library=unquote/fsunit). Both are F#-only gated so the F#-shaped tokens never misfire on another language. Proven by TestPropertyTest_FSharpFsCheck/_FSharpHedgehog + TestAssertionLib_FSharpUnquote/_FSharpFsUnit (+ wrong-language/no-match no-op tests). |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.fsharp-expecto ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
