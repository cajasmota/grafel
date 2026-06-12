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
