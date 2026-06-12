<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# kotlin

**Frameworks**: 18 · **Tools**: 0 · **ORMs**: 7 · **Other**: 1

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
| [Arrow (functional Kotlin)](../detail/lang.kotlin.framework.arrow.md) | 🟡 1/4 | — | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 3/6 | |
| [Dagger / Hilt (Android DI)](../detail/lang.kotlin.framework.dagger-hilt.md) | — | — | 🔴 0/4 | 🔴 0/1 | 🟡 12/23 | 🟡 3/15 | |
| [Javalin (Kotlin)](../detail/lang.kotlin.framework.javalin.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 5/9 | |
| [Koin (Kotlin DI)](../detail/lang.kotlin.framework.koin.md) | — | — | 🔴 0/4 | 🔴 0/1 | 🟡 12/23 | 🟡 3/15 | |
| [Ktor](../detail/lang.kotlin.framework.ktor.md) | 🟡 4/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 13/15 | |
| [Micronaut (Kotlin)](../detail/lang.kotlin.framework.micronaut.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 13/18 | |
| [Quarkus (Kotlin)](../detail/lang.kotlin.framework.quarkus.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 13/18 | |
| [Retrofit (HTTP client)](../detail/lang.kotlin.framework.retrofit.md) | ✅ 2/2 | — | 🟡 1/4 | 🔴 0/1 | 🟡 13/24 | 🔴 0/15 | |
| [Spring Boot (Kotlin)](../detail/lang.kotlin.framework.spring-boot.md) | 🟡 5/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 8/18 | |
| [graphql-kotlin](../detail/lang.kotlin.framework.graphql-kotlin.md) | ✅ 3/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 14/24 | 🟡 2/18 | |
| [http4k](../detail/lang.kotlin.framework.http4k.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 6/9 | |
| [kotlinx.coroutines (structured concurrency)](../detail/lang.kotlin.framework.coroutines.md) | 🟡 1/4 | — | ✅ 4/4 | ✅ 1/1 | 🟡 22/25 | 🟡 3/6 | |
| [kotlinx.serialization (Kotlin DTO/serialization)](../detail/lang.kotlin.framework.kotlinx-serialization.md) | — | — | 🔴 0/4 | 🔴 0/1 | 🟡 12/23 | 🟡 1/15 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Jetpack Compose (Android UI)](../detail/lang.kotlin.framework.compose.md) | ✅ 3/3 | ✅ 1/1 | 🟡 19/21 | 🟢 9/9 | |
| [Kotlin Multiplatform (KMP / KMM)](../detail/lang.kotlin.framework.kmp.md) | ✅ 3/3 | ✅ 1/1 | 🟡 22/24 | 🟢 8/8 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Compose Desktop](../detail/lang.kotlin.framework.compose-desktop.md) | 🟡 11/13 | 🟢 1/1 | |
| [Compose Multiplatform](../detail/lang.kotlin.framework.compose-multiplatform.md) | 🟡 11/13 | 🟢 1/1 | |


### AI Integration

| Name | Other capabilities | Notes |
|---|---|---|
| [LangChain4J (Kotlin)](../detail/lang.kotlin.framework.langchain4j.md) | 🟢 4/4 | |


## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Exposed (JetBrains)](../detail/lang.kotlin.orm.exposed.md) | 🟡 7/10 | |
| [Hibernate (Kotlin)](../detail/lang.kotlin.orm.hibernate.md) | 🟡 8/11 | |
| [Ktorm](../detail/lang.kotlin.orm.ktorm.md) | 🟡 7/10 | |
| [MongoDB (Kotlin driver)](../detail/lang.kotlin.orm.mongodb.md) | 🟡 2/5 | |
| [Room (Android)](../detail/lang.kotlin.orm.room.md) | 🟡 7/10 | |
| [SQLDelight](../detail/lang.kotlin.orm.sqldelight.md) | 🟡 7/10 | |
| [Spring Data (Kotlin)](../detail/lang.kotlin.orm.spring-data.md) | 🟡 8/11 | |


## Other


### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [Bean Validation / konform / Valiktor (Kotlin)](../detail/lang.kotlin.validation.bean-validation.md) | 🟢 1/1 | 🟢 4/4 | |
