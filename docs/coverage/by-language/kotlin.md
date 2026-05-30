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
| [Arrow (functional Kotlin)](../detail/lang.kotlin.framework.arrow.md) | 🟢 1/1 | — | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/3 | |
| [Javalin (Kotlin)](../detail/lang.kotlin.framework.javalin.md) | 🟢 3/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Ktor](../detail/lang.kotlin.framework.ktor.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Micronaut (Kotlin)](../detail/lang.kotlin.framework.micronaut.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🟡 9/15 | |
| [Quarkus (Kotlin)](../detail/lang.kotlin.framework.quarkus.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🟡 9/15 | |
| [Spring Boot (Kotlin)](../detail/lang.kotlin.framework.spring-boot.md) | 🟡 2/3 | 🟢 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🟡 12/15 | |
| [http4k](../detail/lang.kotlin.framework.http4k.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [kotlinx.coroutines (structured concurrency)](../detail/lang.kotlin.framework.coroutines.md) | 🟢 1/1 | — | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/3 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Jetpack Compose (Android UI)](../detail/lang.kotlin.framework.compose.md) | 🔴 0/3 | 🔴 0/1 | 🟡 14/21 | 🔴 0/9 | |
| [Kotlin Multiplatform (KMP / KMM)](../detail/lang.kotlin.framework.kmp.md) | 🔴 0/3 | 🔴 0/1 | 🟡 14/21 | 🔴 0/9 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Compose Desktop](../detail/lang.kotlin.framework.compose-desktop.md) | 🟡 7/10 | 🔴 0/3 | |
| [Compose Multiplatform](../detail/lang.kotlin.framework.compose-multiplatform.md) | 🟡 7/10 | 🔴 0/3 | |


## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Exposed (JetBrains)](../detail/lang.kotlin.orm.exposed.md) | 🟡 2/8 | |
| [Hibernate (Kotlin)](../detail/lang.kotlin.orm.hibernate.md) | 🟡 5/8 | |
| [Ktorm](../detail/lang.kotlin.orm.ktorm.md) | 🟡 2/8 | |
| [MongoDB (Kotlin driver)](../detail/lang.kotlin.orm.mongodb.md) | 🟡 2/7 | |
| [Room (Android)](../detail/lang.kotlin.orm.room.md) | 🟡 2/8 | |
| [SQLDelight](../detail/lang.kotlin.orm.sqldelight.md) | 🟡 2/8 | |
| [Spring Data (Kotlin)](../detail/lang.kotlin.orm.spring-data.md) | 🟡 5/8 | |
