<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# scala

**Frameworks**: 9 · **Tools**: 3 · **ORMs**: 6 · **Other**: 0

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
| [Akka HTTP / Pekko HTTP](../detail/lang.scala.framework.akka-http.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Cask](../detail/lang.scala.framework.cask.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Cats Effect (concurrency runtime)](../detail/lang.scala.framework.cats-effect.md) | 🟢 1/1 | — | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/3 | |
| [Finatra (Twitter Finagle)](../detail/lang.scala.framework.finatra.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Lagom](../detail/lang.scala.framework.lagom.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [Scalatra](../detail/lang.scala.framework.scalatra.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [ZIO HTTP / ZIO](../detail/lang.scala.framework.zio-http.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |
| [http4s](../detail/lang.scala.framework.http4s.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/15 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Play Framework (Scala)](../detail/lang.scala.framework.play.md) | 🔴 0/2 | 🔴 0/3 | 🔴 0/1 | 🟡 14/21 | 🔴 0/7 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [Mill](../detail/build.mill.md) | 🔴 | — | — | 🔴 | |
| [SBT](../detail/build.sbt.md) | ✅ | — | — | ✅ | |
| [build.sbt](../detail/pkg.sbt.md) | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Doobie](../detail/lang.scala.orm.doobie.md) | 🟡 2/8 | |
| [Elastic4s](../detail/lang.scala.orm.elastic4s.md) | 🟡 2/7 | |
| [Quill](../detail/lang.scala.orm.quill.md) | 🟡 2/8 | |
| [ScalikeJDBC](../detail/lang.scala.orm.scalikejdbc.md) | 🟡 2/8 | |
| [Scanamo (DynamoDB)](../detail/lang.scala.orm.scanamo.md) | 🟡 2/7 | |
| [Slick](../detail/lang.scala.orm.slick.md) | 🟡 2/8 | |
