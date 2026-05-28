<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# C#

**Frameworks**: 15 · **Tools**: 7 · **ORMs**: 14 · **Other**: 0

Back to [summary](../summary.md).

## Frameworks


### Backend HTTP

| Name | Routing | Security | Middleware | Substrate | Notes |
|---|---|---|---|---|---|
| [ASP.NET Core](../detail/lang.csharp.framework.aspnet-core.md) | ✅ 2/2 | ❌ 0/1 | ⚠️ 0/1 | ⚠️ 6/17 | |
| [ASP.NET MVC (legacy)](../detail/lang.csharp.framework.aspnet-mvc.md) | ✅ 2/2 | ❌ 0/1 | ❌ 0/1 | ⚠️ 8/17 | |
| [Carter](../detail/lang.csharp.framework.carter.md) | ❌ 0/2 | ❌ 0/1 | ❌ 0/1 | ⚠️ 8/17 | |
| [FastEndpoints](../detail/lang.csharp.framework.fastendpoints.md) | ❌ 0/2 | ❌ 0/1 | ❌ 0/1 | ⚠️ 8/17 | |
| [NancyFX](../detail/lang.csharp.framework.nancyfx.md) | ❌ 0/2 | ❌ 0/1 | ❌ 0/1 | ⚠️ 8/17 | |
| [ServiceStack](../detail/lang.csharp.framework.servicestack.md) | ❌ 0/2 | ❌ 0/1 | ❌ 0/1 | ⚠️ 8/17 | |


### UI Frontend

| Name | Structure | Data Flow | Navigation | Type System | Lifecycle | Testing | Substrate | Notes |
|---|---|---|---|---|---|---|---|---|
| [Blazor Server](../detail/lang.csharp.framework.blazor-server.md) | ❌ 0/3 | ❌ 0/4 | ❌ 0/1 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ⚠️ 5/14 | |
| [Blazor Server / WebAssembly](../detail/lang.csharp.framework.blazor.md) | ❌ 0/3 | ❌ 0/4 | ❌ 0/1 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ⚠️ 5/14 | |
| [Blazor WebAssembly](../detail/lang.csharp.framework.blazor-wasm.md) | ❌ 0/3 | ❌ 0/4 | ❌ 0/1 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ⚠️ 5/14 | |


### Mobile

| Name | Structure | Navigation | Platform | Native Bridge | Data Flow | Type System | Lifecycle | Testing | Substrate | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| [.NET MAUI](../detail/lang.csharp.framework.net-maui.md) | ❌ 0/1 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ❌ 0/2 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ⚠️ 5/14 | |
| [Xamarin](../detail/lang.csharp.framework.xamarin.md) | ❌ 0/1 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ❌ 0/2 | ❌ 0/3 | ❌ 0/1 | ❌ 0/1 | ⚠️ 5/14 | |


### Desktop

| Name | Process | Native | Substrate | Notes |
|---|---|---|---|---|
| [Uno Platform](../detail/lang.csharp.framework.uno.md) | ❌ 0/2 | ❌ 0/1 | ⚠️ 3/7 | |
| [WPF](../detail/lang.csharp.framework.wpf.md) | ❌ 0/2 | ❌ 0/1 | ⚠️ 3/7 | |
| [Windows Forms](../detail/lang.csharp.framework.winforms.md) | ❌ 0/2 | ❌ 0/1 | ⚠️ 3/7 | |


### RPC Framework

| Name | Schema | Codegen | Substrate | Notes |
|---|---|---|---|---|
| [grpc-dotnet](../detail/lang.csharp.framework.grpc-net.md) | ❌ 0/2 | ❌ 0/1 | ⚠️ 5/14 | |


## Tools

| Name | Dependency Graph | Lockfile Parsing | Manifest Parsing | Target Extraction | Notes |
|---|---|---|---|---|---|
| [.csproj / packages.config](../detail/pkg.csproj.md) | — | ❌ | ❌ | — | |
| [FluentAssertions](../detail/test.fluentassertions.md) | ❌ | — | — | ❌ | |
| [MSTest](../detail/test.mstest.md) | ⚠️ | — | — | ⚠️ | |
| [NUnit](../detail/test.nunit.md) | ⚠️ | — | — | ⚠️ | |
| [NuGet](../detail/build.nuget.md) | ⚠️ | — | — | ⚠️ | |
| [dotnet CLI / MSBuild](../detail/build.dotnet.md) | ✅ | — | — | ✅ | |
| [xUnit](../detail/test.xunit.md) | ⚠️ | — | — | ⚠️ | |

## ORMs

| Name | Migration Parsing | Model Extraction | Query Attribution | Notes |
|---|---|---|---|---|
| [AWSSDK.DynamoDBv2](../detail/lang.csharp.driver.dynamodb.md) | — | — | ⚠️ | |
| [CassandraCSharpDriver](../detail/lang.csharp.driver.cassandra.md) | — | — | ⚠️ | |
| [Dapper](../detail/lang.csharp.orm.dapper.md) | ❌ | ⚠️ | ⚠️ | |
| [Entity Framework Core](../detail/lang.csharp.orm.efcore.md) | ❌ | ✅ | ✅ | |
| [LINQ to SQL](../detail/lang.csharp.orm.linq-to-sql.md) | ❌ | ⚠️ | ⚠️ | |
| [LinqToDB](../detail/lang.csharp.orm.linqtodb.md) | ❌ | ❌ | ❌ | |
| [Microsoft.Data.Sqlite](../detail/lang.csharp.driver.sqlite.md) | — | — | ⚠️ | |
| [MongoDB.Driver (C#)](../detail/lang.csharp.driver.mongodb.md) | — | — | ⚠️ | |
| [MySQL.Data / MySqlConnector](../detail/lang.csharp.driver.mysql.md) | — | — | ⚠️ | |
| [NEST (Elasticsearch .NET)](../detail/lang.csharp.driver.elastic.md) | — | — | ⚠️ | |
| [NHibernate](../detail/lang.csharp.orm.nhibernate.md) | ❌ | ⚠️ | ⚠️ | |
| [Neo4j.Driver (C#)](../detail/lang.csharp.driver.neo4j.md) | — | — | ⚠️ | |
| [Npgsql (PostgreSQL)](../detail/lang.csharp.driver.npgsql.md) | — | — | ⚠️ | |
| [StackExchange.Redis](../detail/lang.csharp.driver.redis.md) | — | — | ⚠️ | |
