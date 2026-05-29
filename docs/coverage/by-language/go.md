<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# go

**Frameworks**: 17 · **Tools**: 8 · **ORMs**: 17 · **Other**: 0

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
| [Beego](../detail/lang.go.framework.beego.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Buffalo](../detail/lang.go.framework.buffalo.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Echo](../detail/lang.go.framework.echo.md) | 🟡 2/3 | 🟢 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🟡 1/7 | |
| [Fiber](../detail/lang.go.framework.fiber.md) | 🟡 2/3 | 🟢 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🟡 1/7 | |
| [Gin](../detail/lang.go.framework.gin.md) | 🟡 2/3 | 🟢 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 20/21 | 🟡 1/6 | |
| [Gorilla Mux](../detail/lang.go.framework.gorilla-mux.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Hertz (CloudWeGo)](../detail/lang.go.framework.hertz.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Huma](../detail/lang.go.framework.huma.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Iris](../detail/lang.go.framework.iris.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Kratos (Bilibili)](../detail/lang.go.framework.kratos.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [Revel](../detail/lang.go.framework.revel.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [chi](../detail/lang.go.framework.chi.md) | 🟡 2/3 | 🟢 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🟡 1/7 | |
| [fasthttp](../detail/lang.go.framework.fasthttp.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [go-zero](../detail/lang.go.framework.go-zero.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |
| [net/http (stdlib)](../detail/lang.go.framework.net-http.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 6/20 | 🔴 0/7 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [gomobile (mobile bindings)](../detail/lang.go.framework.gomobile.md) | 🔴 0/3 | 🔴 0/1 | 🟡 3/21 | 🔴 0/9 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Fyne (desktop GUI)](../detail/lang.go.framework.fyne.md) | 🔴 0/10 | 🔴 0/3 | |


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
| [AWS SDK DynamoDB (Go)](../detail/lang.go.driver.dynamodb.md) | 🟡 1/6 | |
| [Bun (uptrace)](../detail/lang.go.orm.bun.md) | 🟢 7/7 | |
| [GORM](../detail/lang.go.orm.gorm.md) | 🟢 8/8 | |
| [ent (Facebook)](../detail/lang.go.orm.ent.md) | 🟢 7/7 | |
| [gen (gentleman / GORM gen)](../detail/lang.go.orm.gen.md) | 🔴 0/8 | |
| [go-elasticsearch](../detail/lang.go.driver.elastic.md) | 🟡 1/6 | |
| [go-redis](../detail/lang.go.driver.redis.md) | 🟡 1/6 | |
| [go-sql-driver/mysql](../detail/lang.go.driver.mysql.md) | 🟡 1/6 | |
| [gocql (Cassandra)](../detail/lang.go.driver.cassandra.md) | 🟡 1/6 | |
| [golang-migrate](../detail/lang.go.orm.migrate.md) | 🟡 1/6 | |
| [mattn/go-sqlite3](../detail/lang.go.driver.sqlite.md) | 🟢 4/4 | |
| [mongo-go-driver](../detail/lang.go.driver.mongodb.md) | 🟡 1/6 | |
| [neo4j-go-driver](../detail/lang.go.driver.neo4j.md) | 🟡 1/6 | |
| [pgx (PostgreSQL driver)](../detail/lang.go.orm.pgx.md) | 🟢 4/4 | |
| [sqlc (codegen)](../detail/lang.go.orm.sqlc.md) | 🟡 3/8 | |
| [sqlx](../detail/lang.go.orm.sqlx.md) | 🟢 5/5 | |
| [xo (codegen)](../detail/lang.go.orm.xo.md) | 🔴 0/8 | |
