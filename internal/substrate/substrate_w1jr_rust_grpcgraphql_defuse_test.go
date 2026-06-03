package substrate

import "testing"

// Wave1-structural (epic #3872): prove the language-level rust def-use
// sniffer fires on the real idioms of the rust records tonic (gRPC
// service impls), async-graphql (resolver methods), and utoipa
// (OpenAPI-annotated axum handlers). The sniffer registers on the "rust"
// slug (init() in def_use_rust.go) and is framework-agnostic, so a flip
// of def_use_chain_extraction missing -> partial on these records is
// justified iff the framework's idiomatic .rs source yields concrete
// (fn, var) def/use pairs. Each assertion names the EXACT enclosing fn
// and variable.

// tonic gRPC service: a #[tonic::async_trait] impl method binds locals
// from the request. Assert def/use of `name` inside fn `say_hello`.
func TestW1jr_DefUseRust_TonicServiceMethod(t *testing.T) {
	src := `
#[tonic::async_trait]
impl Greeter for MyGreeter {
    async fn say_hello(&self, request: Request<HelloRequest>) -> Result<Response<HelloReply>, Status> {
        let name = request.into_inner().name;
        let greeting = name;
        Ok(Response::new(HelloReply { message: greeting }))
    }
}
`
	defs, uses := sniffDefUseRust(src)
	if !containsVarDef(defs, "say_hello", "name") {
		t.Fatalf("expected def of name in say_hello, got %+v", defs)
	}
	if !containsVarDef(defs, "say_hello", "greeting") {
		t.Fatalf("expected def of greeting in say_hello, got %+v", defs)
	}
	// `name` is bound then read into `greeting` — a real def->use chain.
	if !containsVarUse(uses, "say_hello", "name") {
		t.Fatalf("expected use of name in say_hello, got %+v", uses)
	}
}

// async-graphql resolver: an #[Object] impl method binds query locals.
// Assert def/use of `count` inside fn `users`.
func TestW1jr_DefUseRust_AsyncGraphqlResolver(t *testing.T) {
	src := `
#[Object]
impl QueryRoot {
    async fn users(&self, ctx: &Context<'_>, limit: i32) -> Vec<User> {
        let count = limit;
        let bounded = count;
        ctx.data_unchecked::<Db>().users(bounded)
    }
}
`
	defs, uses := sniffDefUseRust(src)
	if !containsVarDef(defs, "users", "count") {
		t.Fatalf("expected def of count in users, got %+v", defs)
	}
	if !containsVarDef(defs, "users", "bounded") {
		t.Fatalf("expected def of bounded in users, got %+v", defs)
	}
	if !containsVarUse(uses, "users", "count") {
		t.Fatalf("expected use of count in users, got %+v", uses)
	}
}

// utoipa OpenAPI-annotated handler: a #[utoipa::path] axum handler binds
// locals from the path. Assert def/use of `id` inside fn `get_item`.
func TestW1jr_DefUseRust_UtoipaHandler(t *testing.T) {
	src := `
#[utoipa::path(get, path = "/item/{id}", responses((status = 200, body = Item)))]
async fn get_item(Path(raw): Path<u64>) -> Json<Item> {
    let id = raw;
    let key = id;
    Json(store::lookup(key))
}
`
	defs, uses := sniffDefUseRust(src)
	if !containsVarDef(defs, "get_item", "id") {
		t.Fatalf("expected def of id in get_item, got %+v", defs)
	}
	if !containsVarDef(defs, "get_item", "key") {
		t.Fatalf("expected def of key in get_item, got %+v", defs)
	}
	if !containsVarUse(uses, "get_item", "id") {
		t.Fatalf("expected use of id in get_item, got %+v", uses)
	}
}
