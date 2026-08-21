<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# dart

**Frameworks**: 3 · **Tools**: 1 · **ORMs**: 6 · **Other**: 1

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
| [shelf_router / dart_frog / conduit (Dart HTTP)](../detail/lang.dart.framework.shelf.md) | 🟡 3/7 | 🔴 0/1 | 🟡 3/4 | ✅ 1/1 | 🔴 0/24 | 🔴 0/13 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Flutter](../detail/lang.dart.framework.flutter.md) | 🟢 3/3 | 🟢 1/1 | 🟡 14/24 | 🟡 6/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [graphql_flutter (GraphQL client)](../detail/lang.dart.framework.graphql-flutter.md) | 🟢 3/3 | 🔴 0/1 | 🟡 1/24 | 🔴 0/9 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [pubspec.yaml](../detail/pkg.pubspec.md) | — | — | ✅ | ✅ | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Hive (Dart key-value DB)](../detail/lang.dart.orm.hive.md) | 🟡 2/9 | |
| [Isar (Dart NoSQL embedded DB)](../detail/lang.dart.orm.isar.md) | 🟡 2/9 | |
| [ObjectBox (Dart NoSQL DB)](../detail/lang.dart.orm.objectbox.md) | 🟡 2/9 | |
| [drift (Dart SQLite ORM)](../detail/lang.dart.orm.drift.md) | 🟡 4/9 | |
| [floor (Room-style Dart ORM)](../detail/lang.dart.orm.floor.md) | 🟡 2/9 | |
| [sqflite (Dart raw SQLite)](../detail/lang.dart.orm.sqflite.md) | 🟡 2/7 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Dart (base language)](../detail/lang.dart.base.md) | [language](../by-category/language.md) | 🟢 | |
