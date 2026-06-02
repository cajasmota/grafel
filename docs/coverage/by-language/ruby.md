<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# ruby

**Frameworks**: 9 · **Tools**: 6 · **ORMs**: 14 · **Other**: 7

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
| [Cuba](../detail/lang.ruby.framework.cuba.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Grape](../detail/lang.ruby.framework.grape.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Hanami](../detail/lang.ruby.framework.hanami.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Padrino](../detail/lang.ruby.framework.padrino.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Roda](../detail/lang.ruby.framework.roda.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 6/11 | |
| [Ruby on Rails](../detail/lang.ruby.framework.rails.md) | 🟡 3/6 | ✅ 1/1 | — | ✅ 1/1 | 🟡 23/25 | 🟡 8/12 | |
| [Sinatra](../detail/lang.ruby.framework.sinatra.md) | 🟡 3/6 | 🟢 1/1 | — | 🟢 1/1 | 🟡 22/25 | 🟡 7/12 | |
| [dry-rb (ecosystem)](../detail/lang.ruby.framework.dry-rb.md) | 🟡 2/5 | — | 🟢 1/1 | 🟢 1/1 | 🟡 22/25 | 🟡 5/10 | |
| [graphql-ruby (GraphQL)](../detail/lang.ruby.framework.graphql-ruby.md) | 🟡 3/6 | 🔴 0/1 | 🔴 0/4 | 🔴 0/1 | 🟡 1/24 | 🔴 0/12 | |


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
| [AWS SDK DynamoDB (Ruby)](../detail/lang.ruby.driver.dynamodb.md) | 🟡 1/3 | |
| [ActiveRecord](../detail/lang.ruby.orm.activerecord.md) | ✅ 10/10 | |
| [DataMapper / Hanami Model (legacy)](../detail/lang.ruby.orm.datamapper.md) | 🟡 8/10 | |
| [Mongoid](../detail/lang.ruby.orm.mongoid.md) | 🟡 5/8 | |
| [ROM (Ruby Object Mapper)](../detail/lang.ruby.orm.rom-rb.md) | 🟡 8/10 | |
| [Sequel](../detail/lang.ruby.orm.sequel.md) | 🟡 8/10 | |
| [cassandra-driver (Ruby)](../detail/lang.ruby.driver.cassandra.md) | 🟡 1/3 | |
| [elasticsearch-ruby](../detail/lang.ruby.driver.elastic.md) | 🟡 1/4 | |
| [mongo Ruby Driver](../detail/lang.ruby.driver.mongodb.md) | 🟡 1/3 | |
| [mysql2 (Ruby driver)](../detail/lang.ruby.driver.mysql.md) | 🔴 0/3 | |
| [neo4j-ruby-driver / activegraph OGM](../detail/lang.ruby.driver.neo4j.md) | 🟡 3/6 | |
| [pg (Ruby driver)](../detail/lang.ruby.driver.postgres.md) | 🔴 0/3 | |
| [redis-rb](../detail/lang.ruby.driver.redis.md) | 🟡 1/3 | |
| [sqlite3 (Ruby driver)](../detail/lang.ruby.driver.sqlite.md) | 🔴 0/3 | |


## Other

| Name | Category | Status | Notes |
|---|---|---|---|
| [ORM model lifecycle-hook → handler TRIGGERS (ActiveRecord callbacks)](../detail/msg.orm-lifecycle-hooks-ruby.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Rails ActionCable](../detail/msg.actioncable.md) | [message_broker](../by-category/message_broker.md) | ✅ | |
| [Resque (Ruby task queue)](../detail/msg.resque.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [Ruby AASM (FSM topology)](../detail/infra.state-machine.aasm.md) | [platform](../by-category/platform.md) | 🟢 | |
| [Sidekiq (Ruby task queue)](../detail/msg.sidekiq.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [rufus-scheduler (Ruby in-process scheduler)](../detail/msg.rufus-scheduler.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
| [whenever (Ruby cron / config/schedule.rb)](../detail/msg.whenever.md) | [message_broker](../by-category/message_broker.md) | 🟢 | |
