<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# elixir

**Frameworks**: 9 · **Tools**: 5 · **ORMs**: 10 · **Other**: 0

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
| [Absinthe (GraphQL)](../detail/lang.elixir.framework.absinthe.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |
| [Ash Framework](../detail/lang.elixir.framework.ash.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |
| [Bandit](../detail/lang.elixir.framework.bandit.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |
| [Cowboy](../detail/lang.elixir.framework.cowboy.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |
| [Nerves (embedded)](../detail/lang.elixir.framework.nerves.md) | 🟡 1/2 | — | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/5 | |
| [Oban (job queue)](../detail/lang.elixir.framework.oban.md) | 🟡 1/2 | — | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/5 | |
| [Phoenix](../detail/lang.elixir.framework.phoenix.md) | 🟡 2/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |
| [Plug](../detail/lang.elixir.framework.plug.md) | 🔴 0/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 17/21 | 🔴 0/6 | |


### Meta Framework

| Name | Routing | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [Phoenix LiveView](../detail/lang.elixir.framework.phoenix-liveview.md) | 🔴 0/2 | 🔴 0/3 | 🔴 0/1 | 🟡 14/21 | 🔴 0/7 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [ExUnit](../detail/test.exunit.md) | ✅ | — | — | ✅ | |
| [Hex](../detail/build.hex.md) | 🟢 | — | — | 🟢 | |
| [Mix (mix.exs)](../detail/build.mix.md) | ✅ | — | — | ✅ | |
| [StreamData (property tests)](../detail/test.streamdata.md) | 🔴 | — | — | 🔴 | |
| [mix.exs](../detail/pkg.mix.md) | — | — | 🔴 | — | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [Ecto](../detail/lang.elixir.orm.ecto.md) | 🟡 2/8 | |
| [ExAws DynamoDB](../detail/lang.elixir.driver.dynamodb.md) | 🟡 1/6 | |
| [MyXQL](../detail/lang.elixir.driver.myxql.md) | 🟡 1/6 | |
| [Postgrex](../detail/lang.elixir.driver.postgrex.md) | 🟡 1/6 | |
| [Redix](../detail/lang.elixir.driver.redix.md) | 🟡 1/6 | |
| [Xandra (Cassandra)](../detail/lang.elixir.driver.xandra.md) | 🟡 1/6 | |
| [bolt_sips (Neo4j)](../detail/lang.elixir.driver.neo4j.md) | 🟡 1/6 | |
| [ecto_sqlite3](../detail/lang.elixir.orm.ecto-sqlite3.md) | 🟡 2/8 | |
| [elasticsearch-elixir](../detail/lang.elixir.driver.elastic.md) | 🟡 1/6 | |
| [mongodb (Elixir driver)](../detail/lang.elixir.driver.mongodb.md) | 🟡 1/6 | |
