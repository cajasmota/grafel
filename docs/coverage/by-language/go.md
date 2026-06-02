<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# go

**Frameworks**: 20 · **Tools**: 8 · **ORMs**: 17 · **Other**: 0

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
| [Beego](../detail/lang.go.framework.beego.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Buffalo](../detail/lang.go.framework.buffalo.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Echo](../detail/lang.go.framework.echo.md) | 🟡 3/4 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Fiber](../detail/lang.go.framework.fiber.md) | 🟡 3/4 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Gin](../detail/lang.go.framework.gin.md) | 🟡 3/4 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 24/25 | 🟡 6/11 | |
| [Gorilla Mux](../detail/lang.go.framework.gorilla-mux.md) | 🟡 3/4 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Hertz (CloudWeGo)](../detail/lang.go.framework.hertz.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Huma](../detail/lang.go.framework.huma.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Iris](../detail/lang.go.framework.iris.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Kratos (Bilibili)](../detail/lang.go.framework.kratos.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Revel](../detail/lang.go.framework.revel.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 6/11 | |
| [chi](../detail/lang.go.framework.chi.md) | 🟡 3/4 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [fasthttp](../detail/lang.go.framework.fasthttp.md) | 🟡 3/4 | — | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 6/11 | |
| [go-zero](../detail/lang.go.framework.go-zero.md) | 🟡 3/4 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [google/wire (DI)](../detail/lang.go.framework.wire.md) | 🔴 0/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🟡 2/11 | |
| [gqlgen (GraphQL)](../detail/lang.go.framework.gqlgen.md) | 🟡 3/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🔴 0/12 | |
| [net/http (stdlib)](../detail/lang.go.framework.net-http.md) | 🟡 3/4 | — | 🟢 3/3 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [uber/fx (DI)](../detail/lang.go.framework.fx.md) | 🔴 0/4 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🟡 2/11 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [gomobile (mobile bindings)](../detail/lang.go.framework.gomobile.md) | 🟢 2/2 | 🟢 1/1 | 🟡 20/21 | 🟢 4/4 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Fyne (desktop GUI)](../detail/lang.go.framework.fyne.md) | 🟡 12/13 | 🟢 1/1 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Ginkgo](../detail/test.ginkgo.md) | 🟢 | — | — | — | ✅ | |
| [Gomega](../detail/test.gomega.md) | 🟢 | — | — | — | ✅ | |
| [Mage](../detail/build.mage.md) | ✅ | — | — | — | ✅ | |
| [Task (taskfile.dev)](../detail/build.task.md) | ✅ | — | — | — | ✅ | |
| [go modules (go.mod / go.sum)](../detail/build.go-modules.md) | ✅ | — | — | — | ✅ | |
| [go testing (stdlib)](../detail/test.go-testing.md) | ✅ | — | — | — | ✅ | |
| [go.mod](../detail/pkg.go-mod.md) | — | — | ✅ | ✅ | — | |
| [testify](../detail/test.testify.md) | ✅ | — | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Go)](../detail/lang.go.driver.dynamodb.md) | 🟡 3/4 | |
| [Bun (uptrace)](../detail/lang.go.orm.bun.md) | 🟡 8/9 | |
| [GORM](../detail/lang.go.orm.gorm.md) | 🟢 9/9 | |
| [ent (Facebook)](../detail/lang.go.orm.ent.md) | 🟡 8/9 | |
| [gen (gentleman / GORM gen)](../detail/lang.go.orm.gen.md) | 🟡 3/4 | |
| [go-elasticsearch](../detail/lang.go.driver.elastic.md) | 🟡 4/5 | |
| [go-redis](../detail/lang.go.driver.redis.md) | 🟡 2/3 | |
| [go-sql-driver/mysql](../detail/lang.go.driver.mysql.md) | 🟡 2/3 | |
| [gocql (Cassandra)](../detail/lang.go.driver.cassandra.md) | 🟡 2/3 | |
| [golang-migrate](../detail/lang.go.orm.migrate.md) | 🟡 1/2 | |
| [mattn/go-sqlite3](../detail/lang.go.driver.sqlite.md) | 🟡 4/5 | |
| [mongo-go-driver](../detail/lang.go.driver.mongodb.md) | 🟡 3/4 | |
| [neo4j-go-driver](../detail/lang.go.driver.neo4j.md) | 🟡 3/4 | |
| [pgx (PostgreSQL driver)](../detail/lang.go.orm.pgx.md) | 🟡 4/5 | |
| [sqlc (codegen)](../detail/lang.go.orm.sqlc.md) | 🟡 4/5 | |
| [sqlx](../detail/lang.go.orm.sqlx.md) | 🟢 6/6 | |
| [xo (codegen)](../detail/lang.go.orm.xo.md) | 🟡 6/7 | |
