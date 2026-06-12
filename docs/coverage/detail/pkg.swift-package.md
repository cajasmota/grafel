<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `pkg.swift-package` — Package.swift / Podfile

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [package_manager](../by-category/package_manager.md)
- **Capability cells:** 1

## Capabilities

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Manifest parsing | 🟢 `partial` | — | 4913 | `internal/extractors/swift/package.go`<br>`internal/extractors/swift/package_test.go` | #4913 (#497): the SwiftPM Package.swift manifest IS fully parsed by the dedicated swift_package extractor (package.go): `.target` / `.executableTarget` / `.testTarget` -> SCOPE.Component(subtype=swiftpm_target, kind=executable|test), `.product(name:package:)` -> SCOPE.External(subtype=swiftpm_product), and `dependencies: [...]` -> DEPENDS_ON edges between targets. Proven by package_test.go. PARTIAL (honest split): only Package.swift is parsed — CocoaPods Podfile / Podfile.lock and Carthage Cartfile (both implied by this record's label) are NOT yet parsed; #3828 retained for those. Was incorrectly marked fully missing. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update pkg.swift-package ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
