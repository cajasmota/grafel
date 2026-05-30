<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# ruby

**Frameworks**: 8 · **Tools**: 6 · **ORMs**: 13 · **Other**: 1

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
| [Cuba](../detail/lang.ruby.framework.cuba.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Grape](../detail/lang.ruby.framework.grape.md) | ✅ 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Hanami](../detail/lang.ruby.framework.hanami.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Padrino](../detail/lang.ruby.framework.padrino.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Roda](../detail/lang.ruby.framework.roda.md) | 🟢 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Ruby on Rails](../detail/lang.ruby.framework.rails.md) | ✅ 3/3 | ✅ 1/1 | — | ✅ 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Sinatra](../detail/lang.ruby.framework.sinatra.md) | ✅ 3/3 | 🟢 1/1 | — | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [dry-rb (ecosystem)](../detail/lang.ruby.framework.dry-rb.md) | 🟢 2/2 | — | 🟢 1/1 | 🟢 1/1 | 🟢 21/21 | 🟢 5/5 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [Bundler (Gemfile)](../detail/build.bundler.md) | ✅ | — | — | ✅ | |
| [Cucumber](../detail/test.cucumber.md) | 🔴 | — | — | 🔴 | |
| [Gemfile](../detail/pkg.gemfile.md) | — | 🔴 | ✅ | — | |
| [Minitest](../detail/test.minitest.md) | 🟢 | — | — | 🟢 | |
| [RSpec](../detail/test.rspec.md) | ✅ | — | — | ✅ | |
| [Rake](../detail/build.rake.md) | 🔴 | — | — | 🟢 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWS SDK DynamoDB (Ruby)](../detail/lang.ruby.driver.dynamodb.md) | 🟢 1/1 | |
| [ActiveRecord](../detail/lang.ruby.orm.activerecord.md) | ✅ 8/8 | |
| [DataMapper / Hanami Model (legacy)](../detail/lang.ruby.orm.datamapper.md) | 🟢 8/8 | |
| [Mongoid](../detail/lang.ruby.orm.mongoid.md) | 🟢 6/6 | |
| [ROM (Ruby Object Mapper)](../detail/lang.ruby.orm.rom-rb.md) | 🟢 8/8 | |
| [Sequel](../detail/lang.ruby.orm.sequel.md) | 🟢 8/8 | |
| [cassandra-driver (Ruby)](../detail/lang.ruby.driver.cassandra.md) | 🟢 1/1 | |
| [elasticsearch-ruby](../detail/lang.ruby.driver.elastic.md) | 🟢 2/2 | |
| [mysql2 (Ruby driver)](../detail/lang.ruby.driver.mysql.md) | 🟢 1/1 | |
| [neo4j-ruby-driver](../detail/lang.ruby.driver.neo4j.md) | 🟢 1/1 | |
| [pg (Ruby driver)](../detail/lang.ruby.driver.postgres.md) | 🟢 1/1 | |
| [redis-rb](../detail/lang.ruby.driver.redis.md) | 🟢 1/1 | |
| [sqlite3 (Ruby driver)](../detail/lang.ruby.driver.sqlite.md) | 🟢 1/1 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [Sidekiq (Ruby task queue)](../detail/msg.sidekiq.md) | [message_broker](../by-category/message_broker.md) | 🔴 | |
