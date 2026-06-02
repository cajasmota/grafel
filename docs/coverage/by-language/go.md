<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# go

**Frameworks**: 18 · **Tools**: 8 · **ORMs**: 17 · **Other**: 0

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
| [Beego](../detail/lang.go.framework.beego.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Buffalo](../detail/lang.go.framework.buffalo.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Echo](../detail/lang.go.framework.echo.md) | 🟢 3/3 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Fiber](../detail/lang.go.framework.fiber.md) | 🟢 3/3 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Gin](../detail/lang.go.framework.gin.md) | 🟢 3/3 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Gorilla Mux](../detail/lang.go.framework.gorilla-mux.md) | 🟢 3/3 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Hertz (CloudWeGo)](../detail/lang.go.framework.hertz.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Huma](../detail/lang.go.framework.huma.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Iris](../detail/lang.go.framework.iris.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Kratos (Bilibili)](../detail/lang.go.framework.kratos.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [Revel](../detail/lang.go.framework.revel.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 6/6 | |
| [chi](../detail/lang.go.framework.chi.md) | 🟢 3/3 | ✅ 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [fasthttp](../detail/lang.go.framework.fasthttp.md) | 🟢 3/3 | — | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 5/5 | |
| [go-zero](../detail/lang.go.framework.go-zero.md) | 🟢 3/3 | 🟢 1/1 | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 7/7 | |
| [gqlgen (GraphQL)](../detail/lang.go.framework.gqlgen.md) | 🟢 3/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/20 | 🔴 0/7 | |
| [net/http (stdlib)](../detail/lang.go.framework.net-http.md) | 🟢 3/3 | — | 🟢 3/3 | 🟢 1/1 | 🟢 20/20 | 🟢 6/6 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [gomobile (mobile bindings)](../detail/lang.go.framework.gomobile.md) | 🟢 2/2 | 🟢 1/1 | 🟢 18/18 | 🟢 4/4 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Fyne (desktop GUI)](../detail/lang.go.framework.fyne.md) | 🟢 10/10 | 🟢 1/1 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [Ginkgo](../detail/test.ginkgo.md) | 🟢 | — | — | ✅ | |
| [Gomega](../detail/test.gomega.md) | 🟢 | — | — | ✅ | |
| [Mage](../detail/build.mage.md) | ✅ | — | — | ✅ | |
| [Task (taskfile.dev)](../detail/build.task.md) | ✅ | — | — | ✅ | |
| [go modules (go.mod / go.sum)](../detail/build.go-modules.md) | ✅ | — | — | ✅ | |
| [go testing (stdlib)](../detail/test.go-testing.md) | ✅ | — | — | ✅ | |
| [go.mod](../detail/pkg.go-mod.md) | — | ✅ | ✅ | — | |
| [testify](../detail/test.testify.md) | ✅ | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Go)](../detail/lang.go.driver.dynamodb.md) | 🟢 3/3 | |
| [Bun (uptrace)](../detail/lang.go.orm.bun.md) | 🟢 8/8 | |
| [GORM](../detail/lang.go.orm.gorm.md) | 🟢 8/8 | |
| [ent (Facebook)](../detail/lang.go.orm.ent.md) | 🟢 8/8 | |
| [gen (gentleman / GORM gen)](../detail/lang.go.orm.gen.md) | 🟢 3/3 | |
| [go-elasticsearch](../detail/lang.go.driver.elastic.md) | 🟢 4/4 | |
| [go-redis](../detail/lang.go.driver.redis.md) | 🟢 2/2 | |
| [go-sql-driver/mysql](../detail/lang.go.driver.mysql.md) | 🟡 1/2 | |
| [gocql (Cassandra)](../detail/lang.go.driver.cassandra.md) | 🟢 2/2 | |
| [golang-migrate](../detail/lang.go.orm.migrate.md) | 🟢 1/1 | |
| [mattn/go-sqlite3](../detail/lang.go.driver.sqlite.md) | 🟢 4/4 | |
| [mongo-go-driver](../detail/lang.go.driver.mongodb.md) | 🟢 3/3 | |
| [neo4j-go-driver](../detail/lang.go.driver.neo4j.md) | 🟢 3/3 | |
| [pgx (PostgreSQL driver)](../detail/lang.go.orm.pgx.md) | 🟢 4/4 | |
| [sqlc (codegen)](../detail/lang.go.orm.sqlc.md) | 🟢 4/4 | |
| [sqlx](../detail/lang.go.orm.sqlx.md) | 🟢 5/5 | |
| [xo (codegen)](../detail/lang.go.orm.xo.md) | 🟢 6/6 | |
