<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# rust

**Frameworks**: 15 · **Tools**: 6 · **ORMs**: 15 · **Other**: 4

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
| [Actix Web](../detail/lang.rust.framework.actix.md) | 🟡 5/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 11/12 | |
| [Axum](../detail/lang.rust.framework.axum.md) | 🟡 5/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 11/12 | |
| [Gotham](../detail/lang.rust.framework.gotham.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Poem](../detail/lang.rust.framework.poem.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Rocket](../detail/lang.rust.framework.rocket.md) | 🟡 5/6 | ✅ 1/1 | ✅ 4/4 | ✅ 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Salvo](../detail/lang.rust.framework.salvo.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Tide](../detail/lang.rust.framework.tide.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [Tonic](../detail/lang.rust.framework.tonic.md) | 🟡 3/6 | 🔴 0/1 | 🟢 4/4 | 🔴 0/1 | 🟡 9/24 | 🟡 4/12 | |
| [Tower (service abstraction)](../detail/lang.rust.framework.tower.md) | 🟡 3/6 | ✅ 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 8/12 | |
| [Warp](../detail/lang.rust.framework.warp.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [async-graphql](../detail/lang.rust.framework.async-graphql.md) | 🟡 3/6 | 🔴 0/1 | ✅ 4/4 | 🔴 0/1 | 🟡 10/24 | 🟡 6/13 | |
| [hyper](../detail/lang.rust.framework.hyper.md) | 🟡 3/6 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟡 22/24 | 🟡 7/12 | |
| [juniper](../detail/lang.rust.framework.juniper.md) | 🟡 3/6 | 🔴 0/1 | ✅ 4/4 | 🔴 0/1 | 🟡 1/24 | 🟡 1/13 | |
| [utoipa](../detail/lang.rust.framework.utoipa.md) | 🟡 3/6 | 🔴 0/1 | ✅ 4/4 | 🔴 0/1 | 🟡 9/24 | 🟡 4/12 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Tauri (desktop)](../detail/lang.rust.framework.tauri.md) | 🟡 12/13 | 🟢 3/3 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Cargo (Cargo.toml)](../detail/build.cargo.md) | ✅ | — | — | — | ✅ | |
| [Cargo.toml](../detail/pkg.cargo.md) | — | — | 🔴 | ✅ | — | |
| [cargo test (stdlib)](../detail/test.cargo-test.md) | ✅ | — | — | — | ✅ | |
| [criterion (benchmark)](../detail/test.criterion.md) | ✅ | — | — | — | ✅ | |
| [mockall](../detail/test.mockall.md) | ✅ | — | — | — | ✅ | |
| [proptest](../detail/test.proptest.md) | ✅ | — | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Diesel](../detail/lang.rust.orm.diesel.md) | 🟡 7/10 | |
| [Rbatis](../detail/lang.rust.orm.rbatis.md) | 🟡 4/7 | |
| [SeaORM](../detail/lang.rust.orm.seaorm.md) | 🟡 8/11 | |
| [SeaQuery](../detail/lang.rust.framework.sea-query.md) | 🟡 3/11 | |
| [aws-sdk-dynamodb (Rust)](../detail/lang.rust.driver.dynamodb.md) | 🟡 1/4 | |
| [cdrs / scylla-rust-driver](../detail/lang.rust.driver.cassandra.md) | 🟡 1/4 | |
| [elasticsearch-rs](../detail/lang.rust.driver.elastic.md) | 🟡 1/4 | |
| [mongodb (Rust driver)](../detail/lang.rust.driver.mongodb.md) | 🟡 1/4 | |
| [mysql / mysql_async](../detail/lang.rust.driver.mysql.md) | 🟡 1/4 | |
| [neo4rs](../detail/lang.rust.driver.neo4j.md) | 🟡 3/6 | |
| [redis-rs](../detail/lang.rust.driver.redis.md) | 🟡 1/4 | |
| [rusqlite](../detail/lang.rust.orm.rusqlite.md) | 🟡 1/4 | |
| [sqlite (Rust)](../detail/lang.rust.driver.sqlite.md) | 🟡 1/4 | |
| [sqlx (Rust)](../detail/lang.rust.orm.sqlx.md) | 🟡 4/7 | |
| [tokio-postgres / postgres](../detail/lang.rust.driver.postgres.md) | 🟡 1/4 | |


## Other


### Brokers

| Name | Consumer extraction | Producer extraction | Topic attribution | Notes |
|---|---|---|---|---|
| [async-nats (NATS)](../detail/lang.rust.framework.async-nats.md) | 🟢 | 🟢 | 🟢 | |
| [lapin (AMQP/RabbitMQ)](../detail/lang.rust.framework.lapin.md) | 🟢 | 🟢 | 🟢 | |
| [rdkafka (Kafka)](../detail/lang.rust.framework.rdkafka.md) | 🟢 | 🟢 | 🟢 | |


### Validation

| Name | Testing | Other capabilities | Notes |
|---|---|---|---|
| [validator](../detail/lang.rust.validation.validator.md) | 🟢 1/1 | ✅ 4/4 | |
