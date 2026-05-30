<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# kotlin

**Frameworks**: 12 · **Tools**: 0 · **ORMs**: 7 · **Other**: 0

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


### JVM Backend

| Name | Routing | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|---|
| [Arrow (functional Kotlin)](../detail/lang.kotlin.framework.arrow.md) | 🟢 1/1 | — | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 3/3 | |
| [Javalin (Kotlin)](../detail/lang.kotlin.framework.javalin.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Ktor](../detail/lang.kotlin.framework.ktor.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 12/12 | |
| [Micronaut (Kotlin)](../detail/lang.kotlin.framework.micronaut.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 15/15 | |
| [Quarkus (Kotlin)](../detail/lang.kotlin.framework.quarkus.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 15/15 | |
| [Spring Boot (Kotlin)](../detail/lang.kotlin.framework.spring-boot.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 15/15 | |
| [http4k](../detail/lang.kotlin.framework.http4k.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [kotlinx.coroutines (structured concurrency)](../detail/lang.kotlin.framework.coroutines.md) | 🟢 1/1 | — | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 3/3 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Jetpack Compose (Android UI)](../detail/lang.kotlin.framework.compose.md) | ✅ 3/3 | 🟢 1/1 | 🟢 18/18 | 🟢 9/9 | |
| [Kotlin Multiplatform (KMP / KMM)](../detail/lang.kotlin.framework.kmp.md) | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 8/8 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Compose Desktop](../detail/lang.kotlin.framework.compose-desktop.md) | 🟢 10/10 | 🟢 1/1 | |
| [Compose Multiplatform](../detail/lang.kotlin.framework.compose-multiplatform.md) | 🟢 10/10 | 🟢 1/1 | |


## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Exposed (JetBrains)](../detail/lang.kotlin.orm.exposed.md) | 🟢 7/7 | |
| [Hibernate (Kotlin)](../detail/lang.kotlin.orm.hibernate.md) | 🟢 8/8 | |
| [Ktorm](../detail/lang.kotlin.orm.ktorm.md) | 🟢 7/7 | |
| [MongoDB (Kotlin driver)](../detail/lang.kotlin.orm.mongodb.md) | 🟢 2/2 | |
| [Room (Android)](../detail/lang.kotlin.orm.room.md) | 🟢 7/7 | |
| [SQLDelight](../detail/lang.kotlin.orm.sqldelight.md) | 🟢 7/7 | |
| [Spring Data (Kotlin)](../detail/lang.kotlin.orm.spring-data.md) | 🟢 8/8 | |
