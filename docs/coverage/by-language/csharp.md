<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# C#

**Frameworks**: 17 · **Tools**: 7 · **ORMs**: 14 · **Other**: 5

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
| [ASP.NET Core](../detail/lang.csharp.framework.aspnet-core.md) | 🟡 3/6 | ✅ 1/1 | ✅ 3/3 | ✅ 1/1 | 🟢 25/25 | 🟡 10/11 | |
| [ASP.NET MVC (legacy)](../detail/lang.csharp.framework.aspnet-mvc.md) | 🟡 3/6 | ✅ 1/1 | ✅ 3/3 | ✅ 1/1 | 🟢 25/25 | 🟡 10/11 | |
| [Carter](../detail/lang.csharp.framework.carter.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 25/25 | 🟡 10/11 | |
| [FastEndpoints](../detail/lang.csharp.framework.fastendpoints.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 25/25 | 🟡 10/11 | |
| [HotChocolate (GraphQL)](../detail/lang.csharp.framework.hotchocolate.md) | 🟡 3/6 | ✅ 1/1 | 🔴 0/4 | 🔴 0/1 | 🟡 21/24 | 🟡 2/13 | |
| [NancyFX](../detail/lang.csharp.framework.nancyfx.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 24/25 | 🟡 6/11 | |
| [ServiceStack](../detail/lang.csharp.framework.servicestack.md) | 🟡 3/6 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟡 24/25 | 🟡 6/11 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Blazor Server](../detail/lang.csharp.framework.blazor-server.md) | ✅ 2/2 | 🟢 1/1 | 🟢 24/24 | 🟢 8/8 | |
| [Blazor Server / WebAssembly](../detail/lang.csharp.framework.blazor.md) | ✅ 2/2 | 🟢 1/1 | 🟢 24/24 | 🟢 8/8 | |
| [Blazor WebAssembly](../detail/lang.csharp.framework.blazor-wasm.md) | ✅ 2/2 | 🟢 1/1 | 🟢 24/24 | 🟢 8/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [.NET MAUI](../detail/lang.csharp.framework.net-maui.md) | 🟢 2/2 | 🟢 1/1 | 🟢 24/24 | 🟢 9/9 | |
| [Xamarin](../detail/lang.csharp.framework.xamarin.md) | 🟢 2/2 | 🟢 1/1 | 🟢 24/24 | 🟢 9/9 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Uno Platform](../detail/lang.csharp.framework.uno.md) | 🟢 13/13 | 🟢 3/3 | |
| [WPF](../detail/lang.csharp.framework.wpf.md) | 🟢 13/13 | 🟢 3/3 | |
| [Windows Forms](../detail/lang.csharp.framework.winforms.md) | 🟢 13/13 | 🟢 3/3 | |


### RPC Framework

| Name | Auth | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|---|
| [WCF](../detail/lang.csharp.framework.wcf.md) | 🔴 0/1 | ✅ 3/3 | ✅ 1/1 | 🔴 0/25 | 🟡 3/14 | |
| [grpc-dotnet](../detail/lang.csharp.framework.grpc-net.md) | ✅ 1/1 | ✅ 3/3 | ✅ 1/1 | 🟢 24/24 | 🟢 10/10 | |


## Tools

| Name | Dependency graph | Dependency usage status | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|---|
| [.csproj / packages.config](../detail/pkg.csproj.md) | — | — | ✅ | ✅ | — | |
| [FluentAssertions](../detail/test.fluentassertions.md) | 🔴 | — | — | — | 🔴 | |
| [MSTest](../detail/test.mstest.md) | 🟢 | — | — | — | 🟢 | |
| [NUnit](../detail/test.nunit.md) | 🟢 | — | — | — | 🟢 | |
| [NuGet](../detail/build.nuget.md) | 🟢 | — | — | — | 🟢 | |
| [dotnet CLI / MSBuild](../detail/build.dotnet.md) | ✅ | — | — | — | ✅ | |
| [xUnit](../detail/test.xunit.md) | 🟢 | — | — | — | 🟢 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWSSDK.DynamoDBv2](../detail/lang.csharp.driver.dynamodb.md) | 🟡 2/5 | |
| [CassandraCSharpDriver](../detail/lang.csharp.driver.cassandra.md) | 🟡 2/5 | |
| [Dapper](../detail/lang.csharp.orm.dapper.md) | 🟡 3/6 | |
| [Entity Framework Core](../detail/lang.csharp.orm.efcore.md) | 🟡 8/11 | |
| [LINQ to SQL](../detail/lang.csharp.orm.linq-to-sql.md) | 🟡 7/10 | |
| [LinqToDB](../detail/lang.csharp.orm.linqtodb.md) | 🟡 6/9 | |
| [Microsoft.Data.Sqlite](../detail/lang.csharp.driver.sqlite.md) | 🔴 0/4 | |
| [MongoDB.Driver (C#)](../detail/lang.csharp.driver.mongodb.md) | 🟡 3/6 | |
| [MySQL.Data / MySqlConnector](../detail/lang.csharp.driver.mysql.md) | 🔴 0/4 | |
| [NEST (Elasticsearch .NET)](../detail/lang.csharp.driver.elastic.md) | 🟡 2/5 | |
| [NHibernate](../detail/lang.csharp.orm.nhibernate.md) | 🟡 7/10 | |
| [Neo4j.Driver (C#)](../detail/lang.csharp.driver.neo4j.md) | 🟡 1/4 | |
| [Npgsql (PostgreSQL)](../detail/lang.csharp.driver.npgsql.md) | 🔴 0/4 | |
| [StackExchange.Redis](../detail/lang.csharp.driver.redis.md) | 🟡 1/4 | |


## Other


### Schedulers

| Name | Consumer extraction | Notes |
|---|---|---|
| [Hangfire RecurringJob (.NET scheduled jobs)](../detail/msg.hangfire-recurring.md) | 🟢 | |
| [Quartz.NET (.NET job scheduler)](../detail/msg.quartz-net.md) | ✅ | |


### Brokers

| Name | Consumer extraction | Producer extraction | Topic attribution | Notes |
|---|---|---|---|---|
| [MassTransit (.NET cross-process service bus)](../detail/msg.masstransit.md) | ✅ | ✅ | 🟢 | |
| [MediatR (.NET in-process CQRS / mediator)](../detail/msg.mediatr.md) | ✅ | ✅ | ✅ | |
| [NServiceBus / Rebus (IHandleMessages<T> convention)](../detail/msg.nservicebus.md) | ✅ | ✅ | 🟢 | |
