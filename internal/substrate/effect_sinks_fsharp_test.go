package substrate

import "testing"

// fsharpEffectsByFn collapses sniffer output into fn -> set-of-effects.
func fsharpEffectsByFn(content string) map[string]map[Effect]bool {
	out := map[string]map[Effect]bool{}
	for _, m := range sniffEffectsFSharp(content) {
		if out[m.Function] == nil {
			out[m.Function] = map[Effect]bool{}
		}
		out[m.Function][m.Effect] = true
	}
	return out
}

// TestSniffEffectsFSharp_EFCore proves EF Core (F#) DbSet LINQ reads and
// SaveChanges/Add writes are classified and attributed to the enclosing
// let-binding.
func TestSniffEffectsFSharp_EFCore(t *testing.T) {
	src := `module UserRepo

let getUser (ctx: AppDbContext) id =
    ctx.Users.FirstOrDefaultAsync(fun u -> u.Id = id)

let listUsers (ctx: AppDbContext) =
    ctx.Users.AsNoTracking().ToListAsync()

let createUser (ctx: AppDbContext) user =
    ctx.Users.Add(user) |> ignore
    ctx.SaveChangesAsync()
`
	got := fsharpEffectsByFn(src)
	if !got["getUser"][EffectDBRead] {
		t.Errorf("getUser expected db_read, got %v", got["getUser"])
	}
	if !got["listUsers"][EffectDBRead] {
		t.Errorf("listUsers expected db_read, got %v", got["listUsers"])
	}
	if !got["createUser"][EffectDBWrite] {
		t.Errorf("createUser expected db_write, got %v", got["createUser"])
	}
}

// TestSniffEffectsFSharp_EFQueryCE proves the F# `query { for x in ... }`
// computation expression registers as db_read.
func TestSniffEffectsFSharp_EFQueryCE(t *testing.T) {
	src := `module Reports

let activeOrders (ctx: AppDbContext) =
    query {
        for o in ctx.Orders do
        where (o.Status = "active")
        select o
    }
`
	got := fsharpEffectsByFn(src)
	if !got["activeOrders"][EffectDBRead] {
		t.Errorf("activeOrders expected db_read (query CE), got %v", got["activeOrders"])
	}
}

// TestSniffEffectsFSharp_Dapper proves Dapper / Dapper.FSharp reads/writes
// (method calls + computation-expression CEs) are classified.
func TestSniffEffectsFSharp_Dapper(t *testing.T) {
	src := `module Data

let findOrders (conn: IDbConnection) =
    conn.QueryAsync<Order>("select * from orders")

let dapperSelect (conn: IDbConnection) =
    select {
        for o in orderTable
        where (o.id = 1)
    } |> conn.SelectAsync<Order>

let saveOrder (conn: IDbConnection) o =
    conn.ExecuteAsync("insert into orders values (@Id)", o)

let dapperInsert (conn: IDbConnection) o =
    insert {
        into orderTable
        value o
    } |> conn.InsertAsync
`
	got := fsharpEffectsByFn(src)
	if !got["findOrders"][EffectDBRead] {
		t.Errorf("findOrders expected db_read, got %v", got["findOrders"])
	}
	if !got["dapperSelect"][EffectDBRead] {
		t.Errorf("dapperSelect expected db_read, got %v", got["dapperSelect"])
	}
	if !got["saveOrder"][EffectDBWrite] {
		t.Errorf("saveOrder expected db_write, got %v", got["saveOrder"])
	}
	if !got["dapperInsert"][EffectDBWrite] {
		t.Errorf("dapperInsert expected db_write, got %v", got["dapperInsert"])
	}
}

// TestSniffEffectsFSharp_DapperExecuteVerb proves the AMBIGUOUS Dapper
// Execute* family (#5001) is classified by the leading SQL verb of its
// string-literal argument: a SELECT/WITH Execute is a read, a DML Execute is
// a write, and an Execute with no inspectable literal defaults to a write.
func TestSniffEffectsFSharp_DapperExecuteVerb(t *testing.T) {
	src := `module Data

let countOrders (conn: IDbConnection) =
    conn.ExecuteScalar<int>("SELECT count(*) FROM orders")

let cteReport (conn: IDbConnection) =
    conn.Execute("""
        WITH recent AS (SELECT * FROM orders)
        SELECT * FROM recent
    """)

let deleteStale (conn: IDbConnection) =
    conn.Execute("DELETE FROM orders WHERE stale = 1")

let runProc (conn: IDbConnection) sql =
    conn.Execute(sql, commandType = CommandType.StoredProcedure)
`
	got := fsharpEffectsByFn(src)
	if !got["countOrders"][EffectDBRead] || got["countOrders"][EffectDBWrite] {
		t.Errorf("countOrders expected db_read only (SELECT), got %v", got["countOrders"])
	}
	if !got["cteReport"][EffectDBRead] || got["cteReport"][EffectDBWrite] {
		t.Errorf("cteReport expected db_read only (WITH...SELECT), got %v", got["cteReport"])
	}
	if !got["deleteStale"][EffectDBWrite] || got["deleteStale"][EffectDBRead] {
		t.Errorf("deleteStale expected db_write only (DELETE), got %v", got["deleteStale"])
	}
	// No inspectable literal -> conservative write default.
	if !got["runProc"][EffectDBWrite] {
		t.Errorf("runProc expected db_write (no-literal default), got %v", got["runProc"])
	}

	// The classification basis is recorded in the Sink tag.
	sinks := fsharpSinksByFn(src)
	if !sinks["countOrders"]["dapper.execute.read"] {
		t.Errorf("countOrders expected sink dapper.execute.read, got %v", sinks["countOrders"])
	}
	if !sinks["deleteStale"]["dapper.execute.write"] {
		t.Errorf("deleteStale expected sink dapper.execute.write, got %v", sinks["deleteStale"])
	}
	if !sinks["runProc"]["dapper.execute.write?"] {
		t.Errorf("runProc expected sink dapper.execute.write?, got %v", sinks["runProc"])
	}
}

// TestSniffEffectsFSharp_DapperReceiverType proves receiver-type resolution
// (#5001): a Dapper call on a binding statically typed/constructed as an
// IDbConnection-family connection is classified even when its NAME is outside
// the conventional heuristic (conn/db/...).
func TestSniffEffectsFSharp_DapperReceiverType(t *testing.T) {
	src := `module Data
open System.Data
open Microsoft.Data.SqlClient

let loadAll (database: IDbConnection) =
    database.QueryAsync<Order>("select * from orders")

let writeOne (database: IDbConnection) o =
    database.Execute("insert into orders values (@Id)", o)

let scopedRead () =
    use sqlite = new SqliteConnection(connStr)
    sqlite.Query<Order>("select * from orders")
`
	got := fsharpEffectsByFn(src)
	if !got["loadAll"][EffectDBRead] {
		t.Errorf("loadAll expected db_read on typed `database` receiver, got %v", got["loadAll"])
	}
	if !got["writeOne"][EffectDBWrite] {
		t.Errorf("writeOne expected db_write on typed `database` receiver, got %v", got["writeOne"])
	}
	if !got["scopedRead"][EffectDBRead] {
		t.Errorf("scopedRead expected db_read on `new SqliteConnection` binding, got %v", got["scopedRead"])
	}
}

// TestSniffEffectsFSharp_DapperReceiverTypeNoLeak proves a call on a
// SIMILARLY-named but UNtyped binding does not earn a Dapper effect (the
// type resolver only credits resolved IDbConnection bindings; the static
// heuristic still gates the rest).
func TestSniffEffectsFSharp_DapperReceiverTypeNoLeak(t *testing.T) {
	src := `module Pure

let report (repository: OrderRepository) =
    repository.Query<Order>("anything")
`
	for _, m := range sniffEffectsFSharp(src) {
		if m.Effect == EffectDBRead || m.Effect == EffectDBWrite {
			t.Errorf("untyped `repository` must not earn a db effect, got %v (%s)", m.Effect, m.Sink)
		}
	}
}

// TestSniffEffectsFSharp_NpgsqlFSharp proves Npgsql.FSharp `Sql.query`
// literals are classified by their leading SQL verb.
func TestSniffEffectsFSharp_NpgsqlFSharp(t *testing.T) {
	src := `module Pg

let loadUsers conn =
    conn
    |> Sql.query "SELECT id, name FROM users"
    |> Sql.executeAsync (fun read -> read.int "id")

let insertUser conn name =
    conn
    |> Sql.query "INSERT INTO users (name) VALUES (@name)"
    |> Sql.parameters [ "@name", Sql.text name ]
    |> Sql.executeNonQueryAsync
`
	got := fsharpEffectsByFn(src)
	if !got["loadUsers"][EffectDBRead] {
		t.Errorf("loadUsers expected db_read, got %v", got["loadUsers"])
	}
	if !got["insertUser"][EffectDBWrite] {
		t.Errorf("insertUser expected db_write, got %v", got["insertUser"])
	}
	// A SELECT must not be misclassified as a write.
	if got["loadUsers"][EffectDBWrite] {
		t.Errorf("loadUsers must not be db_write, got %v", got["loadUsers"])
	}
}

// TestSniffEffectsFSharp_HTTP proves HttpClient / FsHttp outbound calls are
// classified as http_out and attributed to member bindings.
func TestSniffEffectsFSharp_HTTP(t *testing.T) {
	src := `type ApiClient(client: HttpClient) =
    member this.FetchUser id =
        client.GetStringAsync(sprintf "https://api/users/%d" id)
    member _.PushEvent payload =
        client.PostAsync("https://api/events", payload)
`
	got := fsharpEffectsByFn(src)
	if !got["FetchUser"][EffectHTTPOut] {
		t.Errorf("FetchUser expected http_out, got %v", got["FetchUser"])
	}
	if !got["PushEvent"][EffectHTTPOut] {
		t.Errorf("PushEvent expected http_out, got %v", got["PushEvent"])
	}
}

// fsharpSinksByFn collapses sniffer output into fn -> set-of-sink-tags, so
// table attribution (folded into the Sink tag) can be asserted.
func fsharpSinksByFn(content string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range sniffEffectsFSharp(content) {
		if out[m.Function] == nil {
			out[m.Function] = map[string]bool{}
		}
		out[m.Function][m.Sink] = true
	}
	return out
}

// TestSniffEffectsFSharp_SQLProvider proves SQLProvider type-provider queries
// (#4999): the `query { for x in ctx.Dbo.T ... }` CE + direct table
// enumeration register as db_read, and SubmitUpdates/.Create/.Delete()
// register as db_write, each with best-effort table attribution.
func TestSniffEffectsFSharp_SQLProvider(t *testing.T) {
	src := `module Repo

let listUsers (ctx: Sql.dataContext) =
    query {
        for u in ctx.Dbo.Users do
        where (u.Active = true)
        select u
    }

let allRoles (ctx: Sql.dataContext) =
    ctx.Dbo.Roles |> Seq.toList

let addUser (ctx: Sql.dataContext) name =
    ctx.Dbo.Users.` + "`" + `Create` + "`" + `(Name = name) |> ignore
    ctx.SubmitUpdates()

let deleteRole (ctx: Sql.dataContext) (row: Sql.dataContext.dbo.RolesEntity) =
    row.Delete()
    ctx.SubmitUpdatesAsync()
`
	got := fsharpEffectsByFn(src)
	if !got["listUsers"][EffectDBRead] {
		t.Errorf("listUsers expected db_read (query CE over provided ctx), got %v", got["listUsers"])
	}
	if !got["allRoles"][EffectDBRead] {
		t.Errorf("allRoles expected db_read (direct enumeration), got %v", got["allRoles"])
	}
	if got["allRoles"][EffectDBWrite] {
		t.Errorf("allRoles must not be db_write, got %v", got["allRoles"])
	}
	if !got["addUser"][EffectDBWrite] {
		t.Errorf("addUser expected db_write (.Create + SubmitUpdates), got %v", got["addUser"])
	}
	if !got["deleteRole"][EffectDBWrite] {
		t.Errorf("deleteRole expected db_write (.Delete + SubmitUpdatesAsync), got %v", got["deleteRole"])
	}

	// Best-effort table attribution: the enumeration read on ctx.Dbo.Roles
	// and the .Create write on ctx.Dbo.Users carry their table in the Sink.
	sinks := fsharpSinksByFn(src)
	if !sinks["allRoles"]["sqlprovider.read:Roles"] {
		t.Errorf("allRoles expected sink sqlprovider.read:Roles, got %v", sinks["allRoles"])
	}
	if !sinks["addUser"]["sqlprovider.write:Users"] {
		t.Errorf("addUser expected sink sqlprovider.write:Users, got %v", sinks["addUser"])
	}
}

// TestSniffEffectsFSharp_SQLProviderNoFalsePositive proves the SQLProvider
// patterns do not fire on unrelated F# member chains / collection pipelines.
func TestSniffEffectsFSharp_SQLProviderNoFalsePositive(t *testing.T) {
	src := `module Pure

let transform (xs: int list) =
    xs |> List.map (fun x -> x + 1) |> List.toArray

let label (cfg: Config) =
    cfg.Display.Title
`
	for _, m := range sniffEffectsFSharp(src) {
		if m.Effect == EffectDBRead || m.Effect == EffectDBWrite {
			t.Errorf("pure module must yield no db effect, got %v (%s) at %s", m.Effect, m.Sink, m.Function)
		}
	}
}

func TestSniffEffectsFSharp_Registered(t *testing.T) {
	if EffectSnifferFor("fsharp") == nil {
		t.Fatal("fsharp effect sniffer not registered")
	}
}

func TestSniffEffectsFSharp_Empty(t *testing.T) {
	if got := sniffEffectsFSharp(""); got != nil {
		t.Errorf("empty content must yield nil, got %v", got)
	}
}

// TestSniffEffectsFSharp_NonDataNoop proves a pure F# file with no
// data-access primitives yields no db effects (no false positives).
func TestSniffEffectsFSharp_NonDataNoop(t *testing.T) {
	src := `module Math

let add a b = a + b

let rec fib n =
    if n < 2 then n else fib (n - 1) + fib (n - 2)
`
	for _, m := range sniffEffectsFSharp(src) {
		if m.Effect == EffectDBRead || m.Effect == EffectDBWrite {
			t.Errorf("pure module must yield no db effect, got %v at %s", m.Effect, m.Function)
		}
	}
}
