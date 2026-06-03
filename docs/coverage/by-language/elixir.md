<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# elixir

**Frameworks**: 14 · **Tools**: 5 · **ORMs**: 10 · **Other**: 3

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
| [Absinthe (GraphQL)](../detail/lang.elixir.framework.absinthe.md) | 🟡 3/6 | 🟢 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/12 | |
| [Ash Framework](../detail/lang.elixir.framework.ash.md) | 🟡 3/6 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 5/10 | |
| [Bandit](../detail/lang.elixir.framework.bandit.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 3/8 | |
| [Cowboy](../detail/lang.elixir.framework.cowboy.md) | 🟡 3/6 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 4/9 | |
| [Finch](../detail/lang.elixir.framework.finch.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 10/24 | 🔴 0/12 | |
| [Guardian](../detail/lang.elixir.framework.guardian.md) | 🔴 0/6 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 11/25 | 🔴 0/11 | |
| [Nerves (embedded)](../detail/lang.elixir.framework.nerves.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 1/6 | |
| [Oban (job queue)](../detail/lang.elixir.framework.oban.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 5/10 | |
| [Phoenix](../detail/lang.elixir.framework.phoenix.md) | 🟡 3/6 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/11 | |
| [Plug](../detail/lang.elixir.framework.plug.md) | 🟡 3/6 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 21/25 | 🟡 6/11 | |
| [Req](../detail/lang.elixir.framework.req.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 9/24 | 🔴 0/12 | |
| [Tesla](../detail/lang.elixir.framework.tesla.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 10/24 | 🟡 1/12 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Phoenix LiveView](../detail/lang.elixir.framework.phoenix-liveview.md) | 🟢 2/2 | 🟢 3/3 | ✅ 1/1 | 🟡 21/24 | 🟢 5/5 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [elixir-grpc](../detail/lang.elixir.framework.grpc.md) | 🟡 11/24 | 🟡 3/4 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [ExUnit](../detail/test.exunit.md) | ✅ | — | — | — | ✅ | |
| [Hex](../detail/build.hex.md) | 🟢 | — | — | — | 🟢 | |
| [Mix (mix.exs)](../detail/build.mix.md) | ✅ | — | — | — | ✅ | |
| [StreamData (property tests)](../detail/test.streamdata.md) | ✅ | — | — | — | ✅ | |
| [mix.exs](../detail/pkg.mix.md) | — | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Ecto](../detail/lang.elixir.orm.ecto.md) | 🟡 7/10 | |
| [ExAws DynamoDB](../detail/lang.elixir.driver.dynamodb.md) | 🔴 0/4 | |
| [MyXQL](../detail/lang.elixir.driver.myxql.md) | 🔴 0/4 | |
| [Postgrex](../detail/lang.elixir.driver.postgrex.md) | 🔴 0/4 | |
| [Redix](../detail/lang.elixir.driver.redix.md) | 🔴 0/4 | |
| [Xandra (Cassandra)](../detail/lang.elixir.driver.xandra.md) | 🔴 0/4 | |
| [bolt_sips (Neo4j)](../detail/lang.elixir.driver.neo4j.md) | 🟡 3/6 | |
| [ecto_sqlite3](../detail/lang.elixir.orm.ecto-sqlite3.md) | 🟡 7/10 | |
| [elasticsearch-elixir](../detail/lang.elixir.driver.elastic.md) | 🔴 0/4 | |
| [mongodb (Elixir driver)](../detail/lang.elixir.driver.mongodb.md) | 🔴 0/4 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Broadway (Elixir data pipelines)](../detail/lang.elixir.framework.broadway.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [Phoenix Channels](../detail/msg.phoenix-channels.md) | [message_broker](../by-category/message_broker.md) | 🔴 | |
| [Ueberauth (Elixir OAuth)](../detail/lang.elixir.framework.ueberauth.md) | [security](../by-category/security.md) | 🔴 | |
