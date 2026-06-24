<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.alcotest` — alcotest (OCaml testing)

Auto-generated. Back to [summary](../summary.md).

- **Language:** [OCaml](../by-language/ocaml.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-06-24` | 5374 | `internal/extractors/ocaml/depth_test.go` | Each alcotest suite carries a stem-affinity TESTS edge to the module its filename names (math_test->math). Honest partial: direct-call resolution from inside the test_case body and e2e route TESTS edges (test->HTTP endpoint) are not yet wired for OCaml - follow-up. |
| Target extraction | ✅ `full` | `2026-06-24` | 5374 | `internal/extractors/ocaml/depth_test.go` | alcotest test_case "label" cases (alcotestCaseRE) are lifted into one SCOPE.Operation(subtype=test_suite) per OCaml test file (under a test/tests dir or named *_test.ml / test_*.ml, OR importing Alcotest), carrying example_count and framework=alcotest. A real Alcotest marker is required so a non-alcotest test_case helper never fabricates a suite; a case-less file emits nothing (honest). A stem-affinity TESTS edge links the suite to the tested module (math_test.ml->math). Proven by TestAlcotest_Suite + the two negative guards. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.alcotest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
