<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# rust

**Frameworks**: 11 · **Tools**: 6 · **ORMs**: 14 · **Other**: 0

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
| [Actix Web](../detail/lang.rust.framework.actix.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Axum](../detail/lang.rust.framework.axum.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Gotham](../detail/lang.rust.framework.gotham.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Poem](../detail/lang.rust.framework.poem.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Rocket](../detail/lang.rust.framework.rocket.md) | ✅ 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Salvo](../detail/lang.rust.framework.salvo.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Tide](../detail/lang.rust.framework.tide.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Tower (service abstraction)](../detail/lang.rust.framework.tower.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Warp](../detail/lang.rust.framework.warp.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [hyper](../detail/lang.rust.framework.hyper.md) | 🟢 3/3 | 🟢 1/1 | ✅ 4/4 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Tauri (desktop)](../detail/lang.rust.framework.tauri.md) | 🟢 10/10 | 🟢 3/3 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [Cargo (Cargo.toml)](../detail/build.cargo.md) | ✅ | — | — | ✅ | |
| [Cargo.toml](../detail/pkg.cargo.md) | — | 🔴 | ✅ | — | |
| [cargo test (stdlib)](../detail/test.cargo-test.md) | ✅ | — | — | ✅ | |
| [criterion (benchmark)](../detail/test.criterion.md) | ✅ | — | — | ✅ | |
| [mockall](../detail/test.mockall.md) | ✅ | — | — | ✅ | |
| [proptest](../detail/test.proptest.md) | ✅ | — | — | ✅ | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Diesel](../detail/lang.rust.orm.diesel.md) | 🟢 7/7 | |
| [Rbatis](../detail/lang.rust.orm.rbatis.md) | 🟢 4/4 | |
| [SeaORM](../detail/lang.rust.orm.seaorm.md) | 🟢 8/8 | |
| [aws-sdk-dynamodb (Rust)](../detail/lang.rust.driver.dynamodb.md) | 🟢 1/1 | |
| [cdrs / scylla-rust-driver](../detail/lang.rust.driver.cassandra.md) | 🟢 1/1 | |
| [elasticsearch-rs](../detail/lang.rust.driver.elastic.md) | 🟢 1/1 | |
| [mongodb (Rust driver)](../detail/lang.rust.driver.mongodb.md) | 🟢 1/1 | |
| [mysql / mysql_async](../detail/lang.rust.driver.mysql.md) | 🟢 1/1 | |
| [neo4rs](../detail/lang.rust.driver.neo4j.md) | 🟢 1/1 | |
| [redis-rs](../detail/lang.rust.driver.redis.md) | 🟢 1/1 | |
| [rusqlite](../detail/lang.rust.orm.rusqlite.md) | ✅ 1/1 | |
| [sqlite (Rust)](../detail/lang.rust.driver.sqlite.md) | 🟢 1/1 | |
| [sqlx (Rust)](../detail/lang.rust.orm.sqlx.md) | 🟢 4/4 | |
| [tokio-postgres / postgres](../detail/lang.rust.driver.postgres.md) | 🟢 1/1 | |
