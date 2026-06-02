<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# scala

**Frameworks**: 14 · **Tools**: 3 · **ORMs**: 6 · **Other**: 0

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
| [Akka HTTP / Pekko HTTP](../detail/lang.scala.framework.akka-http.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/9 | |
| [Apache Pekko HTTP](../detail/lang.scala.framework.pekko-http.md) | 🟡 3/6 | ✅ 1/1 | 🔴 0/4 | 🟢 1/1 | 🔴 0/24 | 🟡 6/19 | |
| [Caliban](../detail/lang.scala.framework.caliban.md) | 🟡 3/6 | 🔴 0/1 | 🟡 2/4 | 🔴 0/1 | 🟡 3/24 | 🟡 1/19 | |
| [Cask](../detail/lang.scala.framework.cask.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/9 | |
| [Cats Effect (concurrency runtime)](../detail/lang.scala.framework.cats-effect.md) | 🟡 1/4 | — | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 3/6 | |
| [Finatra (Twitter Finagle)](../detail/lang.scala.framework.finatra.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 9/12 | |
| [Lagom](../detail/lang.scala.framework.lagom.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 9/12 | |
| [Scalatra](../detail/lang.scala.framework.scalatra.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/9 | |
| [ZIO HTTP / ZIO](../detail/lang.scala.framework.zio-http.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 9/12 | |
| [http4s](../detail/lang.scala.framework.http4s.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/9 | |
| [sttp (HTTP client)](../detail/lang.scala.framework.sttp.md) | 🟡 2/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🔴 0/19 | |
| [tapir (endpoint DSL)](../detail/lang.scala.framework.tapir.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | 🟢 1/1 | 🟡 2/24 | 🟡 5/19 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Play Framework (Scala)](../detail/lang.scala.framework.play.md) | 🟢 2/2 | ✅ 3/3 | ✅ 1/1 | 🟡 21/24 | ✅ 2/2 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [ScalaPB / zio-grpc / fs2-grpc](../detail/lang.scala.framework.scalapb-grpc.md) | 🟡 2/24 | 🟢 4/4 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Mill](../detail/build.mill.md) | 🔴 | — | — | — | 🔴 | |
| [SBT](../detail/build.sbt.md) | ✅ | — | — | — | ✅ | |
| [build.sbt](../detail/pkg.sbt.md) | — | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Doobie](../detail/lang.scala.orm.doobie.md) | 🟡 3/6 | |
| [Elastic4s](../detail/lang.scala.orm.elastic4s.md) | 🟡 3/6 | |
| [Quill](../detail/lang.scala.orm.quill.md) | 🟡 5/8 | |
| [ScalikeJDBC](../detail/lang.scala.orm.scalikejdbc.md) | 🟡 6/9 | |
| [Scanamo (DynamoDB)](../detail/lang.scala.orm.scanamo.md) | 🟡 3/6 | |
| [Slick](../detail/lang.scala.orm.slick.md) | 🟡 7/10 | |
