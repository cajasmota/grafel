<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# `lang.swift.framework.swiftui` — SwiftUI

Auto-generated. Back to [summary](../summary.md).

- **Language:** [swift](../by-language/swift.md)
- **Category:** [http_framework](../by-category/http_framework.md)
- **Subcategory:** UI Frontend
- **Capability cells:** 36

## Capabilities


### Structure

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Component extraction | ✅ `full` | `2026-06-01` | — | `internal/custom/swift/extractors_test.go`<br>`internal/custom/swift/swiftui.go` | struct/class conforming to View -> SCOPE.UIComponent (framework=swiftui, provenance INFERRED_FROM_SWIFTUI_VIEW); SwiftUI builtin views skipped. Regex/heuristic but catches the canonical View idiom; value-asserting tests TestSwiftUIView/TestSwiftUIBuiltinSkipped. |
| Context extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Data Flow

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Branch conditions | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Data fetching | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Prop extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| State management | 🟢 `partial` | `2026-06-01` | [link](https://github.com/cajasmota/archigraph/issues/3577) | `internal/custom/swift/extractors_test.go`<br>`internal/custom/swift/swiftui.go` | @StateObject/@ObservedObject/@EnvironmentObject -> USES edge View->type:<Observable> (via observable_object); @State/@Binding kept as local SCOPE.Pattern props (no edge, by design). PARTIAL: NavigationLink(value:) destinations resolve cross-file via .navigationDestination(for:) (confidence 0.6, partial prop) and observable type declarations are cross-file (synthetic type:/view: stubs linker-resolved); no in-file destination/observable body resolution. Tests: ProfileView USES type:ProfileViewModel/SettingsStore/AppState; TestSwiftUIStateNoEdge asserts @State emits no USES. |

### Navigation

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Router pattern | ✅ `full` | `2026-06-01` | — | `internal/custom/swift/extractors_test.go`<br>`internal/custom/swift/swiftui.go` | NavigationLink(destination:) and .sheet/.fullScreenCover { DestView() } -> NAVIGATES_TO edge enclosingView->view:<Dest> (via navigation_link / modal_sheet / modal_fullScreenCover). Plus standalone nav:/navdest: SCOPE.Operation entities. Value-asserting tests: ContentView NAVIGATES_TO view:DetailView, HomeView->SettingsView (sheet), RootView->OnboardingView (fullScreenCover). |

### Type System

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Enum extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Interface extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Type alias extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Lifecycle

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| State setter emission | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |

### Testing

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Tests linkage | ✅ `full` | `2026-06-03` | — | `internal/engine/rules/swift/test_patterns.yaml`<br>`internal/substrate/entry_points_swift.go` | XCTest is captured language-wide on .swift sources (SwiftUI app test targets included): test_patterns.yaml matches func test* / XCTestCase / XCUIApplication / *Tests.swift, and entry_points_swift.go marks test functions as EntryKindTestEntry for reachability seeding. Value-asserted by TestSwiftEntryPoints_XCTest. |

### Substrate

| Capability | Status | Verified at | Issue | Cites | Notes |
|------------|--------|-------------|-------|-------|-------|
| Confidence overlay | ✅ `full` | `2026-06-03` | — | `internal/graph/graph.go`<br>`internal/mcp/tools.go`<br>`internal/types/confidence.go` | Confidence overlay is language-agnostic infrastructure applied at graph-query time; all Swift/SwiftUI entities receive confidence scores from the same overlay mechanism as every other language. |
| Config consumption | 🔴 `missing` | — | 3641 | — | — |
| Constant propagation | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | constant_propagation.go is language-agnostic; swift.go (substrate) resolves Swift literal let bindings (let X = literal) and static let namespace bindings that fire on SwiftUI .swift sources (value-asserted in TestSwiftSniffer); partial because complex Swift computed properties are not statically resolved. |
| DB effect | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | effect_propagation.go is language-agnostic; effect_sinks_swift.go (sniffEffectsSwift, registered for swift) fires on any .swift in a SwiftUI app and classifies CoreData context.fetch/save + SQLite.swift as db_read/db_write. Value-asserted in TestSniffEffectsSwift_PrimitiveCoverage (EffectDBRead fetchUsers / EffectDBWrite saveUser). PARTIAL: comprehensive Swift effect coverage needs a broader corpus. |
| Dead code detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_swift.go` | reachability.go BFS from entry-points detects unreachable entities; entry_points_swift.go provides Swift sniffers for @main / App / static func main / XCTest methods / public-open exports that seed SwiftUI apps (value-asserted in TestSwiftEntryPoints_*); partial because comprehensive dead-code detection requires full Swift module resolution. |
| Def use chain extraction | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/def_use_pass.go`<br>`internal/substrate/def_use_swift.go` | def_use_pass.go runs on all indexed languages incl. Swift; def_use_swift.go (sniffDefUseSwift) provides Swift-specific let/var/identifier def-use sniffers that fire on SwiftUI .swift sources; partial because comprehensive Swift pattern coverage requires a broader corpus. |
| Env fallback recognition | ✅ `full` | `2026-06-03` | — | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | swift.go (substrate) recognises ProcessInfo.processInfo.environment[KEY] ?? default as ProvenanceEnvFallback (confidence 0.85) on any .swift incl. SwiftUI app config; constant_propagation.go promotes these into the graph. Value-asserted by TestSwiftSniffer. |
| Error flow | 🔴 `missing` | — | 3628 | — | — |
| Feature flag gating | 🔴 `missing` | — | feature_flag_gating:#3706-not-yet-extracted | — | — |
| Fs effect | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | effect_propagation.go is language-agnostic; effect_sinks_swift.go (sniffEffectsSwift, registered for swift) fires on any .swift in a SwiftUI app and classifies FileManager read/write primitives as fs_read/fs_write. Value-asserted in TestSniffEffectsSwift_PrimitiveCoverage (EffectFSRead readConfig / EffectFSWrite writeLog). PARTIAL: comprehensive Swift effect coverage needs a broader corpus. |
| HTTP effect | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | effect_propagation.go is language-agnostic; effect_sinks_swift.go (sniffEffectsSwift, registered for swift) fires on any .swift in a SwiftUI app and classifies URLSession.shared.data/dataTask/upload + Alamofire AF.request as http_out. Value-asserted in TestSniffEffectsSwift_PrimitiveCoverage (EffectHTTPOut downloadData). PARTIAL: comprehensive Swift effect coverage needs a broader corpus. |
| Import resolution quality | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/constant_propagation.go`<br>`internal/substrate/swift.go` | swift.go (substrate) sniffs Swift import declarations (import SwiftUI, import struct Foundation.Date) and maps them to local names (value-asserted in TestSwiftSniffer); partial because full cross-module resolution requires Package.swift integration. |
| Module cycle detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/module_cycle_pass.go` | module_cycle_pass.go is language-agnostic and runs on Swift IMPORTS edges; partial because Swift module granularity is coarser than package-level and cross-module cycles require framework-specific configuration. |
| Mutation effect | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/effect_propagation.go`<br>`internal/substrate/effect_sinks_swift.go` | effect_propagation.go is language-agnostic; effect_sinks_swift.go (sniffEffectsSwift, registered for swift) fires on any .swift in a SwiftUI app and classifies self.x = / property assignments as mutation. Value-asserted in TestSniffEffectsSwift_PrimitiveCoverage (EffectMutation saveUser). PARTIAL: comprehensive Swift effect coverage needs a broader corpus. |
| Pure function tagging | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/pure_function_pass.go` | pure_function_pass.go is language-agnostic and tags functions with no observed effect-sinks as pure; the Swift effect_sinks_swift.go feed defines impurity for SwiftUI .swift sources; partial because Swift async/await patterns can obscure purity. |
| Reachability analysis | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/reachability.go`<br>`internal/substrate/entry_points.go`<br>`internal/substrate/entry_points_swift.go` | reachability.go BFS seeds from entry-points and walks CALLS edges; entry_points_swift.go supplies @main / App / XCTest / exported-func seeds for SwiftUI apps (value-asserted in TestSwiftEntryPoints_AtMain/_XCTest); partial because full Swift module-boundary traversal requires package-manifest integration. |
| Request shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Response shape extraction | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Sanitizer recognition | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | taint_flow.go is language-agnostic; taint_sites_swift.go (sniffTaintSwift, registered for swift) detects Swift taint sources/sinks/sanitizers (incl. Process command sinks) on any .swift in a SwiftUI app. Value-asserted by TestTaintSniffer_Swift_ProcessIsCommandSink. PARTIAL: full Swift taint coverage needs a broader corpus. |
| Schema drift detection | 🔴 `missing` | — | backfill:dictionary-completeness | — | — |
| Taint sink detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | taint_flow.go is language-agnostic; taint_sites_swift.go (sniffTaintSwift, registered for swift) detects Swift taint sources/sinks/sanitizers (incl. Process command sinks) on any .swift in a SwiftUI app. Value-asserted by TestTaintSniffer_Swift_ProcessIsCommandSink. PARTIAL: full Swift taint coverage needs a broader corpus. |
| Taint source detection | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | taint_flow.go is language-agnostic; taint_sites_swift.go (sniffTaintSwift, registered for swift) detects Swift taint sources/sinks/sanitizers (incl. Process command sinks) on any .swift in a SwiftUI app. Value-asserted by TestTaintSniffer_Swift_ProcessIsCommandSink. PARTIAL: full Swift taint coverage needs a broader corpus. |
| Template pattern catalog | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/template_pattern_pass.go`<br>`internal/substrate/template_pattern_swift.go` | template_pattern_pass.go is language-agnostic; template_pattern_swift.go provides Swift-specific i18n (NSLocalizedString/Text), log-format (print/NSLog/os.Logger) and SQL-literal sniffers firing on SwiftUI .swift sources; partial pending broader corpus. |
| Vulnerability finding | 🟢 `partial` | `2026-06-03` | backfill:dictionary-completeness | `internal/links/taint_flow.go`<br>`internal/substrate/taint_sites_swift.go` | taint_flow.go is language-agnostic; taint_sites_swift.go (sniffTaintSwift, registered for swift) detects Swift taint sources/sinks/sanitizers (incl. Process command sinks) on any .swift in a SwiftUI app. Value-asserted by TestTaintSniffer_Swift_ProcessIsCommandSink. PARTIAL: full Swift taint coverage needs a broader corpus. |

## Provenance

This record is sourced from `docs/coverage/registry.json`. To update it, edit the JSON
(or use `go run ./tools/coverage update lang.swift.framework.swiftui ...`) then regenerate:

```
go run ./tools/coverage validate
go run ./tools/coverage gen
```
