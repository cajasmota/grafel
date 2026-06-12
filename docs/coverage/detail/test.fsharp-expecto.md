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
| Target extraction | 🟢 `partial` | `2026-06-12` | 4906 | `internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/frameworks_fsharp.go`<br>`internal/extractors/cross/testmap/frameworks_fsharp_test.go`<br>`internal/extractors/cross/testmap/resolver.go` | #4906: F# test->SUT linkage via the cross-language testmap extractor. detectFSharpExpecto (frameworks_fsharp.go) detects Expecto `testList "Subject" [ testCase "..." <| fun _ -> ... ]` / testCaseAsync / ptestCase / ftestCase leaves AND xUnit/NUnit `[<Fact>]/[<Theory>]/[<Test>] let ``name`` () = ...` attributed bindings (precededByTestAttr binds the attribute to its let). Each case's off-side-rule body (extractNimBlockBody, the shared indentation block extractor) is scanned by the resolver: PAREN-style .NET-interop calls (`OrderService()`, `svc.PlaceOrder(...)`) resolve to high-confidence TESTS edges, and the `testList` subject (or the `XxxTests`-module naming convention via fsharpModuleSubject) seeds a medium-confidence describe-subject edge. The F#/Expecto/Unquote/FsUnit assertion + case-combinator DSL (testCase/Expect.*/should/...) is denylisted in resolver.go so it never surfaces as the production subject. Framework entry is FILENAME/PATH gated (`*Tests.fs` / `Test*.fs` / `_test.fs` / `/tests?/`) since F# uses `open` (not import) — the detector self-confirms (a non-test .fs yields zero cases). Proven by TestFSharpExpecto_SubjectLinkage / _ParenCallHighConfidence, TestFSharpXUnit_AttributedLetBinding, TestFSharp_NonTestFileNoop / _TestFilenameButNoCasesNoop. PARTIAL (honest): F# SPACE-APPLIED function application (`createUser "ada"` — the dominant functional call idiom) is NOT captured by the shared paren-anchored directCallRE; only paren / subject signals link. Space-application capture is a documented follow-up. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.fsharp-expecto ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
