package rust_test

import "testing"

// findEnt returns the first entity matching kind+name, or nil.
func findEnt(ents []entitySummary, kind, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// async-graphql
// ---------------------------------------------------------------------------

func TestAsyncGraphQLResolverFields(t *testing.T) {
	src := `
struct Query;

#[Object]
impl Query {
    async fn user(&self, ctx: &Context<'_>, id: ID) -> Result<User> {
        Ok(User::default())
    }
    async fn users(&self, ctx: &Context<'_>) -> Result<Vec<User>> {
        Ok(vec![])
    }
}

struct Mutation;

#[Object]
impl Mutation {
    async fn create_user(&self, ctx: &Context<'_>, input: NewUser) -> Result<User> {
        Ok(User::default())
    }
}
`
	ents := extract(t, "custom_rust_async_graphql", fi("schema.rs", "rust", src))

	// Each resolver method becomes an addressable GRAPHQL endpoint.
	for _, want := range []string{
		"GRAPHQL /graphql/Query/user",
		"GRAPHQL /graphql/Query/users",
		"GRAPHQL /graphql/Mutation/create_user",
	} {
		e := findEnt(ents, "SCOPE.Operation", want)
		if e == nil {
			t.Fatalf("expected resolver endpoint %q", want)
		}
		if e.Props["verb"] != "GRAPHQL" {
			t.Errorf("%s: verb = %q, want GRAPHQL", want, e.Props["verb"])
		}
	}

	// Operation kind is derived from the impl root.
	if e := findEnt(ents, "SCOPE.Operation", "GRAPHQL /graphql/Query/user"); e != nil {
		if e.Props["graphql_operation"] != "Query" {
			t.Errorf("user: graphql_operation = %q, want Query", e.Props["graphql_operation"])
		}
		if e.Props["graphql_field"] != "user" {
			t.Errorf("user: graphql_field = %q, want user", e.Props["graphql_field"])
		}
		if e.Props["handler_name"] != "Query.user" {
			t.Errorf("user: handler_name = %q, want Query.user", e.Props["handler_name"])
		}
	}
	if e := findEnt(ents, "SCOPE.Operation", "GRAPHQL /graphql/Mutation/create_user"); e != nil {
		if e.Props["graphql_operation"] != "Mutation" {
			t.Errorf("create_user: graphql_operation = %q, want Mutation", e.Props["graphql_operation"])
		}
	}
}

func TestAsyncGraphQLDTOs(t *testing.T) {
	src := `
#[derive(SimpleObject)]
struct User {
    id: ID,
    name: String,
}

#[derive(InputObject)]
struct NewUser {
    name: String,
}

#[derive(Enum, Copy, Clone, Eq, PartialEq)]
enum Role {
    Admin,
    Member,
}
`
	ents := extract(t, "custom_rust_async_graphql", fi("model.rs", "rust", src))

	user := findEnt(ents, "SCOPE.Schema", "graphql_dto:User")
	if user == nil {
		t.Fatal("expected SimpleObject DTO graphql_dto:User")
	}
	if user.Props["graphql_dto_role"] != "object" {
		t.Errorf("User: role = %q, want object", user.Props["graphql_dto_role"])
	}

	newUser := findEnt(ents, "SCOPE.Schema", "graphql_dto:NewUser")
	if newUser == nil {
		t.Fatal("expected InputObject DTO graphql_dto:NewUser")
	}
	if newUser.Props["graphql_dto_role"] != "input" {
		t.Errorf("NewUser: role = %q, want input", newUser.Props["graphql_dto_role"])
	}

	role := findEnt(ents, "SCOPE.Schema", "graphql_dto:Role")
	if role == nil {
		t.Fatal("expected Enum DTO graphql_dto:Role")
	}
	if role.Props["graphql_dto_role"] != "enum" {
		t.Errorf("Role: role = %q, want enum", role.Props["graphql_dto_role"])
	}
}

func TestAsyncGraphQLSchemaRoot(t *testing.T) {
	src := `
fn build() {
    let schema = Schema::build(Query, Mutation, EmptySubscription::default()).finish();
}
`
	ents := extract(t, "custom_rust_async_graphql", fi("main.rs", "rust", src))

	e := findEnt(ents, "SCOPE.Service", "graphql_schema:Query,Mutation,EmptySubscription")
	if e == nil {
		t.Fatalf("expected graphql schema root service, got %+v", ents)
	}
	if e.Props["query_root"] != "Query" {
		t.Errorf("query_root = %q, want Query", e.Props["query_root"])
	}
	if e.Props["mutation_root"] != "Mutation" {
		t.Errorf("mutation_root = %q, want Mutation", e.Props["mutation_root"])
	}
	if e.Props["subscription_root"] != "EmptySubscription" {
		t.Errorf("subscription_root = %q, want EmptySubscription", e.Props["subscription_root"])
	}
}

func TestAsyncGraphQLNoMatch(t *testing.T) {
	src := `fn main() { println!("hello"); }`
	ents := extract(t, "custom_rust_async_graphql", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on plain rust, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// tonic (gRPC)
// ---------------------------------------------------------------------------

func TestTonicRpcMethods(t *testing.T) {
	src := `
#[tonic::async_trait]
impl Greeter for MyGreeter {
    async fn say_hello(
        &self,
        request: Request<HelloRequest>,
    ) -> Result<Response<HelloReply>, Status> {
        Ok(Response::new(HelloReply::default()))
    }

    async fn say_goodbye(
        &self,
        request: Request<GoodbyeRequest>,
    ) -> Result<Response<GoodbyeReply>, Status> {
        Ok(Response::new(GoodbyeReply::default()))
    }
}
`
	ents := extract(t, "custom_rust_tonic", fi("service.rs", "rust", src))

	hello := findEnt(ents, "SCOPE.Operation", "RPC /Greeter/say_hello")
	if hello == nil {
		t.Fatalf("expected RPC endpoint /Greeter/say_hello, got %+v", ents)
	}
	if hello.Props["verb"] != "RPC" {
		t.Errorf("say_hello: verb = %q, want RPC", hello.Props["verb"])
	}
	if hello.Props["grpc_service"] != "Greeter" {
		t.Errorf("say_hello: grpc_service = %q, want Greeter", hello.Props["grpc_service"])
	}
	if hello.Props["grpc_method"] != "say_hello" {
		t.Errorf("say_hello: grpc_method = %q, want say_hello", hello.Props["grpc_method"])
	}
	if hello.Props["request_message"] != "HelloRequest" {
		t.Errorf("say_hello: request_message = %q, want HelloRequest", hello.Props["request_message"])
	}
	if hello.Props["response_message"] != "HelloReply" {
		t.Errorf("say_hello: response_message = %q, want HelloReply", hello.Props["response_message"])
	}
	if hello.Props["handler_name"] != "MyGreeter.say_hello" {
		t.Errorf("say_hello: handler_name = %q, want MyGreeter.say_hello", hello.Props["handler_name"])
	}

	if findEnt(ents, "SCOPE.Operation", "RPC /Greeter/say_goodbye") == nil {
		t.Error("expected RPC endpoint /Greeter/say_goodbye")
	}
}

func TestTonicMessageDTOs(t *testing.T) {
	src := `
#[tonic::async_trait]
impl Greeter for MyGreeter {
    async fn say_hello(
        &self,
        request: Request<HelloRequest>,
    ) -> Result<Response<HelloReply>, Status> {
        unimplemented!()
    }
}
`
	ents := extract(t, "custom_rust_tonic", fi("service.rs", "rust", src))

	req := findEnt(ents, "SCOPE.Schema", "grpc_dto:HelloRequest")
	if req == nil {
		t.Fatal("expected request DTO grpc_dto:HelloRequest")
	}
	if req.Props["grpc_message_role"] != "request" {
		t.Errorf("HelloRequest: role = %q, want request", req.Props["grpc_message_role"])
	}

	resp := findEnt(ents, "SCOPE.Schema", "grpc_dto:HelloReply")
	if resp == nil {
		t.Fatal("expected response DTO grpc_dto:HelloReply")
	}
	if resp.Props["grpc_message_role"] != "response" {
		t.Errorf("HelloReply: role = %q, want response", resp.Props["grpc_message_role"])
	}
}

func TestTonicAddService(t *testing.T) {
	src := `
async fn main() {
    Server::builder()
        .add_service(GreeterServer::new(MyGreeter::default()))
        .serve(addr)
        .await?;
}
`
	ents := extract(t, "custom_rust_tonic", fi("main.rs", "rust", src))

	e := findEnt(ents, "SCOPE.Service", "grpc_service:GreeterServer")
	if e == nil {
		t.Fatalf("expected grpc service registration, got %+v", ents)
	}
	if e.Props["grpc_service"] != "Greeter" {
		t.Errorf("grpc_service = %q, want Greeter", e.Props["grpc_service"])
	}
	if e.Props["server_type"] != "GreeterServer" {
		t.Errorf("server_type = %q, want GreeterServer", e.Props["server_type"])
	}
}

func TestTonicNoMatch(t *testing.T) {
	src := `fn main() { let x = 1 + 2; }`
	ents := extract(t, "custom_rust_tonic", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on plain rust, got %d", len(ents))
	}
}
