<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# kotlin

**Frameworks**: 17 · **Tools**: 0 · **ORMs**: 7 · **Other**: 0

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
| [Arrow (functional Kotlin)](../detail/lang.kotlin.framework.arrow.md) | 🟢 1/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 3/4 | |
| [Dagger / Hilt (Android DI)](../detail/lang.kotlin.framework.dagger-hilt.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🟡 3/17 | |
| [Javalin (Kotlin)](../detail/lang.kotlin.framework.javalin.md) | 🔴 0/3 | 🔴 0/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 5/7 | |
| [Koin (Kotlin DI)](../detail/lang.kotlin.framework.koin.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🟡 3/17 | |
| [Ktor](../detail/lang.kotlin.framework.ktor.md) | ✅ 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 12/13 | |
| [Micronaut (Kotlin)](../detail/lang.kotlin.framework.micronaut.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 6/16 | |
| [Quarkus (Kotlin)](../detail/lang.kotlin.framework.quarkus.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 6/16 | |
| [Retrofit (HTTP client)](../detail/lang.kotlin.framework.retrofit.md) | 🟡 2/3 | 🔴 0/1 | 🟡 1/4 | 🔴 0/1 | 🟡 2/22 | 🔴 0/17 | |
| [Spring Boot (Kotlin)](../detail/lang.kotlin.framework.spring-boot.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 3/16 | |
| [graphql-kotlin](../detail/lang.kotlin.framework.graphql-kotlin.md) | ✅ 3/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🟡 1/17 | |
| [http4k](../detail/lang.kotlin.framework.http4k.md) | 🟢 3/3 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 6/7 | |
| [kotlinx.coroutines (structured concurrency)](../detail/lang.kotlin.framework.coroutines.md) | 🟢 1/1 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/23 | 🟡 3/4 | |
| [kotlinx.serialization (Kotlin DTO/serialization)](../detail/lang.kotlin.framework.kotlinx-serialization.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🟡 1/17 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Jetpack Compose (Android UI)](../detail/lang.kotlin.framework.compose.md) | ✅ 3/3 | ✅ 1/1 | 🟡 18/20 | 🟢 9/9 | |
| [Kotlin Multiplatform (KMP / KMM)](../detail/lang.kotlin.framework.kmp.md) | ✅ 3/3 | ✅ 1/1 | 🟡 21/23 | 🟢 8/8 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Compose Desktop](../detail/lang.kotlin.framework.compose-desktop.md) | 🟡 10/12 | 🟢 1/1 | |
| [Compose Multiplatform](../detail/lang.kotlin.framework.compose-multiplatform.md) | 🟡 10/12 | 🟢 1/1 | |


## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Exposed (JetBrains)](../detail/lang.kotlin.orm.exposed.md) | 🟡 7/8 | |
| [Hibernate (Kotlin)](../detail/lang.kotlin.orm.hibernate.md) | 🟡 7/9 | |
| [Ktorm](../detail/lang.kotlin.orm.ktorm.md) | 🟡 7/8 | |
| [MongoDB (Kotlin driver)](../detail/lang.kotlin.orm.mongodb.md) | 🟡 2/3 | |
| [Room (Android)](../detail/lang.kotlin.orm.room.md) | 🟡 7/8 | |
| [SQLDelight](../detail/lang.kotlin.orm.sqldelight.md) | 🟡 7/8 | |
| [Spring Data (Kotlin)](../detail/lang.kotlin.orm.spring-data.md) | 🟡 7/9 | |
