<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# go

**Frameworks**: 17 · **Tools**: 8 · **ORMs**: 17 · **Other**: 0

Back to [summary](../summary.md).

## Frameworks


### Backend HTTP

| Name | Routing | Security | Validation | Middleware | Testing | Observability | Data | Notes |
|---|---|---|---|---|---|---|---|---|
| [Beego](../detail/lang.go.framework.beego.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Buffalo](../detail/lang.go.framework.buffalo.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Echo](../detail/lang.go.framework.echo.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Fiber](../detail/lang.go.framework.fiber.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Gin](../detail/lang.go.framework.gin.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Gorilla Mux](../detail/lang.go.framework.gorilla-mux.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Hertz (CloudWeGo)](../detail/lang.go.framework.hertz.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Huma](../detail/lang.go.framework.huma.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Iris](../detail/lang.go.framework.iris.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Kratos (Bilibili)](../detail/lang.go.framework.kratos.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [Revel](../detail/lang.go.framework.revel.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [chi](../detail/lang.go.framework.chi.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [fasthttp](../detail/lang.go.framework.fasthttp.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [go-zero](../detail/lang.go.framework.go-zero.md) | ⚠️ 0/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |
| [net/http (stdlib)](../detail/lang.go.framework.net-http.md) | ✅ 2/2 | ❌ 0/1 | — | ❌ 0/1 | — | — | — | |


### Mobile

| Name | Structure | Navigation | Platform | Native Bridge | Data Flow | Type System | Lifecycle | Testing | Notes |
|---|---|---|---|---|---|---|---|---|---|
| [gomobile (mobile bindings)](../detail/lang.go.framework.gomobile.md) | ❌ 0/2 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ❌ 0/2 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | |


### Desktop

| Name | Process | Native | Updates | Notes |
|---|---|---|---|---|
| [Fyne (desktop GUI)](../detail/lang.go.framework.fyne.md) | ❌ 0/2 | ❌ 0/1 | — | |


## Tools

| Name | Dependency Graph | Lockfile Parsing | Manifest Parsing | Target Extraction | Notes |
|---|---|---|---|---|---|
| [Ginkgo](../detail/test.ginkgo.md) | ❌ | — | — | ❌ | |
| [Gomega](../detail/test.gomega.md) | ❌ | — | — | ❌ | |
| [Mage](../detail/build.mage.md) | ❌ | — | — | ❌ | |
| [Task (taskfile.dev)](../detail/build.task.md) | ❌ | — | — | ❌ | |
| [go modules (go.mod / go.sum)](../detail/build.go-modules.md) | ✅ | — | — | ✅ | |
| [go testing (stdlib)](../detail/test.go-testing.md) | ✅ | — | — | ✅ | |
| [go.mod](../detail/pkg.go-mod.md) | — | ⚠️ | ✅ | — | |
| [testify](../detail/test.testify.md) | ⚠️ | — | — | ⚠️ | |

## ORMs

| Name | Migration Parsing | Model Extraction | Query Attribution | Notes |
|---|---|---|---|---|
| [AWS SDK DynamoDB (Go)](../detail/lang.go.driver.dynamodb.md) | — | — | ⚠️ | |
| [Bun (uptrace)](../detail/lang.go.orm.bun.md) | ❌ | ⚠️ | ⚠️ | |
| [GORM](../detail/lang.go.orm.gorm.md) | ❌ | ✅ | ✅ | |
| [ent (Facebook)](../detail/lang.go.orm.ent.md) | ❌ | ✅ | ✅ | |
| [gen (gentleman / GORM gen)](../detail/lang.go.orm.gen.md) | ❌ | ❌ | ❌ | |
| [go-elasticsearch](../detail/lang.go.driver.elastic.md) | — | — | ⚠️ | |
| [go-redis](../detail/lang.go.driver.redis.md) | — | — | ⚠️ | |
| [go-sql-driver/mysql](../detail/lang.go.driver.mysql.md) | — | — | ⚠️ | |
| [gocql (Cassandra)](../detail/lang.go.driver.cassandra.md) | — | — | ⚠️ | |
| [golang-migrate](../detail/lang.go.orm.migrate.md) | ⚠️ | — | — | |
| [mattn/go-sqlite3](../detail/lang.go.driver.sqlite.md) | — | — | ⚠️ | |
| [mongo-go-driver](../detail/lang.go.driver.mongodb.md) | — | — | ⚠️ | |
| [neo4j-go-driver](../detail/lang.go.driver.neo4j.md) | — | — | ⚠️ | |
| [pgx (PostgreSQL driver)](../detail/lang.go.orm.pgx.md) | — | — | ⚠️ | |
| [sqlc (codegen)](../detail/lang.go.orm.sqlc.md) | ⚠️ | ⚠️ | ⚠️ | |
| [sqlx](../detail/lang.go.orm.sqlx.md) | ❌ | ⚠️ | ⚠️ | |
| [xo (codegen)](../detail/lang.go.orm.xo.md) | ❌ | ❌ | ❌ | |
