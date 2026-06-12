<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# elixir

**Frameworks**: 14 · **Tools**: 9 · **ORMs**: 10 · **Other**: 5

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
| [Absinthe (GraphQL)](../detail/lang.elixir.framework.absinthe.md) | 🟡 3/6 | 🟢 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 7/12 | |
| [Ash Framework](../detail/lang.elixir.framework.ash.md) | 🟡 3/6 | — | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 5/10 | |
| [Bandit](../detail/lang.elixir.framework.bandit.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 3/8 | |
| [Cowboy](../detail/lang.elixir.framework.cowboy.md) | 🟡 3/6 | — | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 4/9 | |
| [Finch](../detail/lang.elixir.framework.finch.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🟡 13/24 | 🟡 3/12 | |
| [Guardian](../detail/lang.elixir.framework.guardian.md) | 🔴 0/6 | ✅ 1/1 | 🔴 0/4 | ✅ 1/1 | 🟡 14/25 | 🟡 3/11 | |
| [Nerves (embedded)](../detail/lang.elixir.framework.nerves.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 1/6 | |
| [Oban (job queue)](../detail/lang.elixir.framework.oban.md) | 🟡 1/4 | — | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 5/10 | |
| [Phoenix](../detail/lang.elixir.framework.phoenix.md) | 🟡 4/6 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 7/11 | |
| [Plug](../detail/lang.elixir.framework.plug.md) | 🟡 4/6 | ✅ 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 23/25 | 🟡 7/11 | |
| [Req](../detail/lang.elixir.framework.req.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🟡 12/24 | 🟡 3/12 | |
| [Tesla](../detail/lang.elixir.framework.tesla.md) | 🟡 1/6 | 🔴 0/1 | 🔴 0/4 | ✅ 1/1 | 🟡 13/24 | 🟡 5/12 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Phoenix LiveView](../detail/lang.elixir.framework.phoenix-liveview.md) | 🟢 2/2 | 🟢 3/3 | ✅ 1/1 | 🟡 23/24 | 🟢 5/5 | |


### RPC Framework

| Name | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [elixir-grpc](../detail/lang.elixir.framework.grpc.md) | 🟢 1/1 | 🟢 4/4 | ✅ 1/1 | 🟡 13/24 | 🟡 6/10 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Bypass](../detail/test.bypass.md) | 🔴 | — | — | — | 🔴 | |
| [ExUnit](../detail/test.exunit.md) | ✅ | — | — | — | ✅ | |
| [Faker / ExMachina](../detail/test.faker.md) | 🔴 | — | — | — | 🔴 | |
| [Hex](../detail/build.hex.md) | 🟢 | — | — | — | 🟢 | |
| [Mix (mix.exs)](../detail/build.mix.md) | ✅ | — | — | — | ✅ | |
| [Mox](../detail/test.mox.md) | 🔴 | — | — | — | 🔴 | |
| [StreamData (property tests)](../detail/test.streamdata.md) | ✅ | — | — | — | ✅ | |
| [Wallaby](../detail/test.wallaby.md) | 🔴 | — | — | — | 🔴 | |
| [mix.exs](../detail/pkg.mix.md) | — | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Ecto](../detail/lang.elixir.orm.ecto.md) | 🟡 7/10 | |
| [ExAws DynamoDB](../detail/lang.elixir.driver.dynamodb.md) | 🟡 1/4 | |
| [MyXQL](../detail/lang.elixir.driver.myxql.md) | 🔴 0/4 | |
| [Postgrex](../detail/lang.elixir.driver.postgrex.md) | 🔴 0/4 | |
| [Redix](../detail/lang.elixir.driver.redix.md) | 🟡 1/4 | |
| [Xandra (Cassandra)](../detail/lang.elixir.driver.xandra.md) | 🟡 1/4 | |
| [bolt_sips (Neo4j)](../detail/lang.elixir.driver.neo4j.md) | 🟡 3/6 | |
| [ecto_sqlite3](../detail/lang.elixir.orm.ecto-sqlite3.md) | 🟡 7/10 | |
| [elasticsearch-elixir](../detail/lang.elixir.driver.elastic.md) | 🟡 1/4 | |
| [mongodb (Elixir driver)](../detail/lang.elixir.driver.mongodb.md) | 🟡 1/4 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Elixir (base language)](../detail/lang.elixir.base.md) | [language](../by-category/language.md) | ✅ | |
| [Nebulex (cache)](../detail/db.nebulex.md) | [databases](../by-category/databases.md) | 🔴 | |
| [Ueberauth (Elixir OAuth)](../detail/lang.elixir.framework.ueberauth.md) | [security](../by-category/security.md) | 🔴 | |

### Brokers

| Name | Consumer extraction | Producer extraction | Topic attribution | Notes |
|---|---|---|---|---|
| [Broadway (Elixir data pipelines)](../detail/lang.elixir.framework.broadway.md) | 🟢 | 🟢 | 🟢 | |


### Realtime Channels

| Name | Consumer extraction | Producer extraction | Room channel grouping | Topic attribution | Notes |
|---|---|---|---|---|---|
| [Phoenix Channels](../detail/msg.phoenix-channels.md) | 🔴 | ✅ | ✅ | 🟢 | |
