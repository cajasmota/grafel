<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# ruby

**Frameworks**: 9 · **Tools**: 6 · **ORMs**: 13 · **Other**: 2

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
| [Cuba](../detail/lang.ruby.framework.cuba.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 6/6 | |
| [Grape](../detail/lang.ruby.framework.grape.md) | ✅ 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 6/6 | |
| [Hanami](../detail/lang.ruby.framework.hanami.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 6/6 | |
| [Padrino](../detail/lang.ruby.framework.padrino.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 6/6 | |
| [Roda](../detail/lang.ruby.framework.roda.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 6/6 | |
| [Ruby on Rails](../detail/lang.ruby.framework.rails.md) | ✅ 3/3 | ✅ 1/1 | — | ✅ 1/1 | 🟡 22/23 | 🟢 7/7 | |
| [Sinatra](../detail/lang.ruby.framework.sinatra.md) | ✅ 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟡 21/23 | 🟢 7/7 | |
| [dry-rb (ecosystem)](../detail/lang.ruby.framework.dry-rb.md) | 🟢 2/2 | — | 🟢 1/1 | 🟢 1/1 | 🟡 21/23 | 🟢 5/5 | |
| [graphql-ruby (GraphQL)](../detail/lang.ruby.framework.graphql-ruby.md) | 🟢 3/3 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🔴 0/22 | 🔴 0/7 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [Bundler (Gemfile)](../detail/build.bundler.md) | ✅ | — | — | — | ✅ | |
| [Cucumber](../detail/test.cucumber.md) | 🔴 | — | — | — | 🔴 | |
| [Gemfile](../detail/pkg.gemfile.md) | — | — | 🔴 | ✅ | — | |
| [Minitest](../detail/test.minitest.md) | 🟢 | — | — | — | 🟢 | |
| [RSpec](../detail/test.rspec.md) | ✅ | — | — | — | ✅ | |
| [Rake](../detail/build.rake.md) | 🔴 | — | — | — | 🟢 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Ruby)](../detail/lang.ruby.driver.dynamodb.md) | ✅ 1/1 | |
| [ActiveRecord](../detail/lang.ruby.orm.activerecord.md) | ✅ 8/8 | |
| [DataMapper / Hanami Model (legacy)](../detail/lang.ruby.orm.datamapper.md) | 🟢 8/8 | |
| [Mongoid](../detail/lang.ruby.orm.mongoid.md) | 🟡 5/6 | |
| [ROM (Ruby Object Mapper)](../detail/lang.ruby.orm.rom-rb.md) | 🟢 8/8 | |
| [Sequel](../detail/lang.ruby.orm.sequel.md) | 🟢 8/8 | |
| [cassandra-driver (Ruby)](../detail/lang.ruby.driver.cassandra.md) | ✅ 1/1 | |
| [elasticsearch-ruby](../detail/lang.ruby.driver.elastic.md) | 🟡 1/2 | |
| [mysql2 (Ruby driver)](../detail/lang.ruby.driver.mysql.md) | 🔴 0/1 | |
| [neo4j-ruby-driver / activegraph OGM](../detail/lang.ruby.driver.neo4j.md) | 🟡 3/4 | |
| [pg (Ruby driver)](../detail/lang.ruby.driver.postgres.md) | 🔴 0/1 | |
| [redis-rb](../detail/lang.ruby.driver.redis.md) | ✅ 1/1 | |
| [sqlite3 (Ruby driver)](../detail/lang.ruby.driver.sqlite.md) | 🔴 0/1 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Ruby AASM (FSM topology)](../detail/infra.state-machine.aasm.md) | [platform](../by-category/platform.md) | 🟢 | |
| [Sidekiq (Ruby task queue)](../detail/msg.sidekiq.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
