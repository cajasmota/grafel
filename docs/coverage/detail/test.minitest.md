<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `test.minitest` — Minitest

Auto-generated. Back to [summary](../summary.md).

- **Language:** [ruby](../by-language/ruby.md)
- **Category:** [build_system](../by-category/build_system.md)
- **Capability cells:** 2

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Dependency graph | 🟢 `partial` | `2026-05-28` | — | `internal/engine/tests_edges.go` | — |
| Target extraction | 🟢 `partial` | `2026-06-12` | — | `internal/engine/rules/ruby/test_patterns.yaml`<br>`internal/extractors/cross/testmap/extractor.go`<br>`internal/extractors/cross/testmap/frameworks.go`<br>`internal/extractors/cross/testmap/resolver.go`<br>`internal/extractors/ruby/tests.go`<br>`internal/extractors/ruby/tests_subject_4398_test.go` | #4919 doc-audit: Minitest/Test::Unit/ActiveSupport::TestCase test-suite extraction is more than a build_system dependency — emitRubyMinitestSuite (internal/extractors/ruby/tests.go) collapses class XTest < Minitest::Test (also Test::Unit::TestCase / ActiveSupport::TestCase, DSL test 'desc' do AND def test_*) to one namespaced test_suite per class (def test_* folded to test_count) AND emits a name-affinity TESTS->subject edge stripping the Test suffix (UserTest->User) else the *_test.rb path-stem convention (test/models/user_test.rb->User via railsTestCamelCase). Honest-partial: no edge when no subject resolves; plain non-test-case classes not collapsed; a test case with no def test_* emits no suite. Value-asserted in tests_subject_4398_test.go. Pairs with #4398/#4360. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update test.minitest ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
