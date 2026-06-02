<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# swift

**Frameworks**: 4 · **Tools**: 1 · **ORMs**: 0 · **Other**: 0

Back to [summary](../summary.md).

### Legend

Each group column shows `glyph covered/applicable` — **covered** = capabilities with extraction, **applicable** = covered + missing (not-applicable capabilities are excluded from both). The glyph is the group's **support level**:

| Glyph | Level | Meaning |
|---|---|---|
| ✅ | **Comprehensive** | every applicable capability is `full` — fixture-proven, resolves the general case |
| 🟢 | **Supported** | every applicable capability is extracted; some only *heuristically* (detected by pattern, not full AST/data-flow resolution) |
| 🟡 | **Partial** | some capabilities extracted, some still missing |
| 🔴 | **Not extracted** | nothing extracted yet |
| — | **N/A** | capability does not apply to this framework |

Examples: `🟢 20/20` = fully supported, some capabilities heuristic · `🟡 12/20` = 8 not yet extracted. Detail pages use the same palette **per cell** (✅ full · 🟢 heuristic/partial · 🔴 missing · — n/a).

## Frameworks


### Backend HTTP

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Vapor](../detail/lang.swift.framework.vapor.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/11 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [SwiftUI](../detail/lang.swift.framework.swiftui.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/24 | 🟡 3/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Alamofire (HTTP client)](../detail/lang.swift.framework.alamofire.md) | 🔴 0/3 | 🔴 0/1 | 🟡 1/24 | 🔴 0/9 | |
| [URLSession (HTTP client)](../detail/lang.swift.framework.urlsession.md) | 🔴 0/3 | 🔴 0/1 | 🟡 1/24 | 🔴 0/9 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Package.swift / Podfile](../detail/pkg.swift-package.md) | — | — | — | 🔴 | — | |
