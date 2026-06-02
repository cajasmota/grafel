<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# elixir

**Frameworks**: 14 · **Tools**: 5 · **ORMs**: 10 · **Other**: 2

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
| [Absinthe (GraphQL)](../detail/lang.elixir.framework.absinthe.md) | 🟢 3/3 | 🟢 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 6/6 | |
| [Ash Framework](../detail/lang.elixir.framework.ash.md) | 🟢 3/3 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 5/5 | |
| [Bandit](../detail/lang.elixir.framework.bandit.md) | 🟢 1/1 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 3/3 | |
| [Cowboy](../detail/lang.elixir.framework.cowboy.md) | 🟢 3/3 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 4/4 | |
| [Finch](../detail/lang.elixir.framework.finch.md) | 🟡 1/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 3/21 | 🔴 0/7 | |
| [Guardian](../detail/lang.elixir.framework.guardian.md) | 🔴 0/3 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/21 | 🔴 0/7 | |
| [Nerves (embedded)](../detail/lang.elixir.framework.nerves.md) | 🟢 1/1 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 1/1 | |
| [Oban (job queue)](../detail/lang.elixir.framework.oban.md) | 🟢 1/1 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 5/5 | |
| [Phoenix](../detail/lang.elixir.framework.phoenix.md) | ✅ 3/3 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 6/6 | |
| [Plug](../detail/lang.elixir.framework.plug.md) | 🟢 3/3 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/22 | 🟢 6/6 | |
| [Req](../detail/lang.elixir.framework.req.md) | 🟡 1/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 2/21 | 🔴 0/7 | |
| [Tesla](../detail/lang.elixir.framework.tesla.md) | 🟡 1/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 2/21 | 🟡 1/7 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Phoenix LiveView](../detail/lang.elixir.framework.phoenix-liveview.md) | 🟢 2/2 | 🟢 3/3 | ✅ 1/1 | 🟡 21/22 | 🟢 5/5 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [elixir-grpc](../detail/lang.elixir.framework.grpc.md) | 🟡 3/22 | 🟡 3/4 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [ExUnit](../detail/test.exunit.md) | ✅ | — | — | ✅ | |
| [Hex](../detail/build.hex.md) | 🟢 | — | — | 🟢 | |
| [Mix (mix.exs)](../detail/build.mix.md) | ✅ | — | — | ✅ | |
| [StreamData (property tests)](../detail/test.streamdata.md) | ✅ | — | — | ✅ | |
| [mix.exs](../detail/pkg.mix.md) | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Ecto](../detail/lang.elixir.orm.ecto.md) | ✅ 7/7 | |
| [ExAws DynamoDB](../detail/lang.elixir.driver.dynamodb.md) | 🔴 0/1 | |
| [MyXQL](../detail/lang.elixir.driver.myxql.md) | 🔴 0/1 | |
| [Postgrex](../detail/lang.elixir.driver.postgrex.md) | 🔴 0/1 | |
| [Redix](../detail/lang.elixir.driver.redix.md) | 🔴 0/1 | |
| [Xandra (Cassandra)](../detail/lang.elixir.driver.xandra.md) | 🔴 0/1 | |
| [bolt_sips (Neo4j)](../detail/lang.elixir.driver.neo4j.md) | 🔴 0/1 | |
| [ecto_sqlite3](../detail/lang.elixir.orm.ecto-sqlite3.md) | 🟢 7/7 | |
| [elasticsearch-elixir](../detail/lang.elixir.driver.elastic.md) | 🔴 0/1 | |
| [mongodb (Elixir driver)](../detail/lang.elixir.driver.mongodb.md) | 🔴 0/1 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Broadway (Elixir data pipelines)](../detail/lang.elixir.framework.broadway.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [Ueberauth (Elixir OAuth)](../detail/lang.elixir.framework.ueberauth.md) | [security](../by-category/security.md) | 🔴 | |
