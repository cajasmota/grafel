<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# C#

**Frameworks**: 15 · **Tools**: 7 · **ORMs**: 14 · **Other**: 0

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
| [ASP.NET Core](../detail/lang.csharp.framework.aspnet-core.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [ASP.NET MVC (legacy)](../detail/lang.csharp.framework.aspnet-mvc.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [Carter](../detail/lang.csharp.framework.carter.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [FastEndpoints](../detail/lang.csharp.framework.fastendpoints.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [NancyFX](../detail/lang.csharp.framework.nancyfx.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |
| [ServiceStack](../detail/lang.csharp.framework.servicestack.md) | 🟢 3/3 | 🟢 1/1 | ✅ 3/3 | 🟢 1/1 | 🟢 21/21 | 🟢 6/6 | |


### UI Frontend

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [Blazor Server](../detail/lang.csharp.framework.blazor-server.md) | ✅ 2/2 | 🟢 1/1 | 🟢 21/21 | 🟢 8/8 | |
| [Blazor Server / WebAssembly](../detail/lang.csharp.framework.blazor.md) | ✅ 2/2 | 🟢 1/1 | 🟢 21/21 | 🟢 8/8 | |
| [Blazor WebAssembly](../detail/lang.csharp.framework.blazor-wasm.md) | ✅ 2/2 | 🟢 1/1 | 🟢 21/21 | 🟢 8/8 | |


### Mobile

| Name | Type System | Testing | Substrate | Other capabilities | Notes |
|---|---|---|---|---|---|
| [.NET MAUI](../detail/lang.csharp.framework.net-maui.md) | 🟢 2/2 | 🟢 1/1 | 🟢 21/21 | 🟢 9/9 | |
| [Xamarin](../detail/lang.csharp.framework.xamarin.md) | 🟢 2/2 | 🟢 1/1 | 🟢 21/21 | 🟢 9/9 | |


### Desktop

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [Uno Platform](../detail/lang.csharp.framework.uno.md) | 🟢 10/10 | 🟢 3/3 | |
| [WPF](../detail/lang.csharp.framework.wpf.md) | 🟢 10/10 | 🟢 3/3 | |
| [Windows Forms](../detail/lang.csharp.framework.winforms.md) | 🟢 10/10 | 🟢 3/3 | |


### RPC Framework

| Name | Substrate | Other capabilities | Notes |
|---|---|---|---|
| [grpc-dotnet](../detail/lang.csharp.framework.grpc-net.md) | 🟢 21/21 | 🟢 4/4 | |


## Tools

| Name | Dependency graph | Lockfile parsing | Manifest parsing | Target extraction | Notes |
|---|---|---|---|---|---|
| [.csproj / packages.config](../detail/pkg.csproj.md) | — | ✅ | ✅ | — | |
| [FluentAssertions](../detail/test.fluentassertions.md) | 🔴 | — | — | 🔴 | |
| [MSTest](../detail/test.mstest.md) | 🟢 | — | — | 🟢 | |
| [NUnit](../detail/test.nunit.md) | 🟢 | — | — | 🟢 | |
| [NuGet](../detail/build.nuget.md) | 🟢 | — | — | 🟢 | |
| [dotnet CLI / MSBuild](../detail/build.dotnet.md) | ✅ | — | — | ✅ | |
| [xUnit](../detail/test.xunit.md) | 🟢 | — | — | 🟢 | |

## ORMs


### ORM / Data Mapper

| Name | Other capabilities | Notes |
|---|---|---|
| [AWSSDK.DynamoDBv2](../detail/lang.csharp.driver.dynamodb.md) | 🟢 2/2 | |
| [CassandraCSharpDriver](../detail/lang.csharp.driver.cassandra.md) | 🟢 2/2 | |
| [Dapper](../detail/lang.csharp.orm.dapper.md) | ✅ 3/3 | |
| [Entity Framework Core](../detail/lang.csharp.orm.efcore.md) | ✅ 8/8 | |
| [LINQ to SQL](../detail/lang.csharp.orm.linq-to-sql.md) | 🟢 7/7 | |
| [LinqToDB](../detail/lang.csharp.orm.linqtodb.md) | 🟢 6/6 | |
| [Microsoft.Data.Sqlite](../detail/lang.csharp.driver.sqlite.md) | 🟢 1/1 | |
| [MongoDB.Driver (C#)](../detail/lang.csharp.driver.mongodb.md) | 🟢 2/2 | |
| [MySQL.Data / MySqlConnector](../detail/lang.csharp.driver.mysql.md) | 🟢 1/1 | |
| [NEST (Elasticsearch .NET)](../detail/lang.csharp.driver.elastic.md) | 🟢 2/2 | |
| [NHibernate](../detail/lang.csharp.orm.nhibernate.md) | 🟢 7/7 | |
| [Neo4j.Driver (C#)](../detail/lang.csharp.driver.neo4j.md) | 🟢 1/1 | |
| [Npgsql (PostgreSQL)](../detail/lang.csharp.driver.npgsql.md) | 🟢 1/1 | |
| [StackExchange.Redis](../detail/lang.csharp.driver.redis.md) | 🟢 1/1 | |
