package rust_test

// Tests for minor Rust framework routing extractors.
// Each test uses a representative fixture and verifies:
//   - endpoint_synthesis: SCOPE.Operation with correct method + path name
//   - handler_attribution: handler_name property is captured

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Poem
// ---------------------------------------------------------------------------

func TestPoemRoute(t *testing.T) {
	src := `
use poem::{Route, Server, EndpointExt};
use poem::web::Json;

#[handler]
async fn list_users() -> Json<Vec<String>> {
    Json(vec![])
}

#[handler]
async fn create_user() -> Json<String> {
    Json("ok".to_string())
}

let app = Route::new()
    .at("/users", get(list_users))
    .at("/users/:id", post(create_user));
`
	ents := extract(t, "custom_rust_poem", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users/{id}") {
		t.Error("expected POST /users/{id} endpoint (param normalised to {id})")
	}
	if !containsEntity(ents, "SCOPE.Function", "list_users") {
		t.Error("expected list_users handler function")
	}
}

func TestPoemNest(t *testing.T) {
	src := `
let app = Route::new()
    .nest("/api", api_routes);
`
	ents := extract(t, "custom_rust_poem", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "nest:/api") {
		t.Error("expected nest:/api component")
	}
}

func TestPoemServer(t *testing.T) {
	src := `
Server::new(TcpListener::bind("0.0.0.0:3000"))
    .run(app)
    .await?;
`
	ents := extract(t, "custom_rust_poem", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "poem::Server") {
		t.Error("expected poem::Server service")
	}
}

func TestPoemNoMatch(t *testing.T) {
	src := `fn main() { println!("hello"); }`
	ents := extract(t, "custom_rust_poem", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Warp
// ---------------------------------------------------------------------------

func TestWarpChain(t *testing.T) {
	src := `
let users = warp::path!("users")
    .and(warp::get())
    .and_then(list_users);

let create = warp::path!("users")
    .and(warp::post())
    .and_then(create_user);
`
	ents := extract(t, "custom_rust_warp", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users endpoint")
	}
}

func TestWarpServe(t *testing.T) {
	src := `warp::serve(routes).run(([127, 0, 0, 1], 3030)).await;`
	ents := extract(t, "custom_rust_warp", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "warp::serve") {
		t.Error("expected warp::serve service")
	}
}

func TestWarpMethodFilter(t *testing.T) {
	src := `let get_filter = warp::get();`
	ents := extract(t, "custom_rust_warp", fi("filters.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "filter:GET") {
		t.Error("expected filter:GET method pattern")
	}
}

func TestWarpNoMatch(t *testing.T) {
	src := `struct Config { port: u16 }`
	ents := extract(t, "custom_rust_warp", fi("config.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Tide
// ---------------------------------------------------------------------------

func TestTideRoute(t *testing.T) {
	src := `
let mut app = tide::new();
app.at("/users").get(list_users);
app.at("/users").post(create_user);
app.at("/users/:id").delete(delete_user);
`
	ents := extract(t, "custom_rust_tide", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "DELETE /users/{id}") {
		t.Error("expected DELETE /users/{id} endpoint (param normalised to {id})")
	}
}

func TestTideServer(t *testing.T) {
	src := `let mut app = tide::new();`
	ents := extract(t, "custom_rust_tide", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "tide::Server") {
		t.Error("expected tide::Server service")
	}
}

func TestTideMiddleware(t *testing.T) {
	src := `app.with(tide::log::LogMiddleware::new());`
	ents := extract(t, "custom_rust_tide", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "middleware:tide::log::LogMiddleware::new") {
		t.Error("expected middleware pattern")
	}
}

func TestTideNoMatch(t *testing.T) {
	src := `use std::io;`
	ents := extract(t, "custom_rust_tide", fi("lib.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Gotham
// ---------------------------------------------------------------------------

func TestGothamRoute(t *testing.T) {
	src := `
build_simple_router(|route| {
    route.get("/").to(index_handler);
    route.post("/users").to(create_user_handler);
    route.get("/users/:id").to(get_user_handler);
})
`
	ents := extract(t, "custom_rust_gotham", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /") {
		t.Error("expected GET / endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Component", "gotham::Router") {
		t.Error("expected gotham::Router component")
	}
}

func TestGothamStart(t *testing.T) {
	src := `gotham::start("127.0.0.1:7878", router).await.unwrap();`
	ents := extract(t, "custom_rust_gotham", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "gotham::start") {
		t.Error("expected gotham::start service")
	}
}

func TestGothamNoMatch(t *testing.T) {
	src := `struct State { counter: u32 }`
	ents := extract(t, "custom_rust_gotham", fi("state.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Hyper (raw)
// ---------------------------------------------------------------------------

func TestHyperMatchArm(t *testing.T) {
	src := `
async fn handle(req: Request<Body>) -> Result<Response<Body>, Infallible> {
    match (req.method(), req.uri().path()) {
        (&Method::GET, "/users") => get_users(req).await,
        (&Method::POST, "/users") => create_user(req).await,
        (&Method::GET, "/health") => health_check(req).await,
        _ => not_found(),
    }
}
`
	ents := extract(t, "custom_rust_hyper", fi("handler.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users endpoint")
	}
	if !containsEntity(ents, "SCOPE.Operation", "GET /health") {
		t.Error("expected GET /health endpoint")
	}
}

func TestHyperServiceFn(t *testing.T) {
	src := `
let make_svc = make_service_fn(|_conn| async {
    Ok::<_, Infallible>(service_fn(handle))
});
`
	ents := extract(t, "custom_rust_hyper", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Function", "handle") {
		t.Error("expected handle handler function")
	}
}

func TestHyperBind(t *testing.T) {
	src := `
let server = Server::bind(&addr).serve(make_svc);
`
	ents := extract(t, "custom_rust_hyper", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "hyper::Server") {
		t.Error("expected hyper::Server service")
	}
}

func TestHyperNoMatch(t *testing.T) {
	src := `use hyper::body::Bytes;`
	ents := extract(t, "custom_rust_hyper", fi("lib.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Salvo
// ---------------------------------------------------------------------------

func TestSalvoRoute(t *testing.T) {
	src := `
let router = Router::new()
    .path("/users")
    .get(list_users)
    .post(create_user);
`
	ents := extract(t, "custom_rust_salvo", fi("router.rs", "rust", src))
	// Salvo chains: path + get/post
	if !containsEntity(ents, "SCOPE.Component", "path:/users") {
		t.Error("expected path:/users component")
	}
}

func TestSalvoServer(t *testing.T) {
	src := `
Server::new(TcpListener::bind("0.0.0.0:5800")).serve(router).await;
`
	ents := extract(t, "custom_rust_salvo", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "salvo::Server") {
		t.Error("expected salvo::Server service")
	}
}

func TestSalvoHoop(t *testing.T) {
	src := `
let router = Router::new()
    .hoop(CorsLayer::permissive())
    .hoop(Logger::new());
`
	ents := extract(t, "custom_rust_salvo", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "hoop:CorsLayer::permissive") {
		t.Error("expected hoop:CorsLayer::permissive middleware pattern")
	}
}

func TestSalvoNoMatch(t *testing.T) {
	src := `use salvo::prelude::*;`
	ents := extract(t, "custom_rust_salvo", fi("lib.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Tower
// ---------------------------------------------------------------------------

func TestTowerServiceBuilder(t *testing.T) {
	src := `
let svc = ServiceBuilder::new()
    .layer(TraceLayer::new_for_http())
    .layer(TimeoutLayer::new(Duration::from_secs(10)))
    .service(my_service);
`
	ents := extract(t, "custom_rust_tower", fi("service.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "ServiceBuilder::new") {
		t.Error("expected ServiceBuilder::new service")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "layer:TraceLayer::new_for_http") {
		t.Error("expected TraceLayer pattern")
	}
	if !containsEntity(ents, "SCOPE.Component", "service:my_service") {
		t.Error("expected service:my_service component")
	}
}

func TestTowerServiceFn(t *testing.T) {
	src := `let svc = tower::service_fn(my_handler);`
	ents := extract(t, "custom_rust_tower", fi("service.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Function", "my_handler") {
		t.Error("expected my_handler handler function")
	}
}

func TestTowerNoMatch(t *testing.T) {
	src := `use tower::Service;`
	ents := extract(t, "custom_rust_tower", fi("lib.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Deep route-extraction tests for minor frameworks: param normalisation.
// ---------------------------------------------------------------------------

// warp::path!("users" / u32) — typed segment becomes a {param}.
func TestWarpTypedParam(t *testing.T) {
	src := `
let user = warp::path!("users" / u32)
    .and(warp::get())
    .and_then(get_user);
`
	ents := extract(t, "custom_rust_warp", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{u32}") {
		t.Error("expected GET /users/{u32} from typed warp segment")
	}
}

// poem .at("/users/:id", ...) — colon param normalised to {id}.
func TestPoemParamNormalised(t *testing.T) {
	src := `let app = Route::new().at("/users/:id", get(get_user));`
	ents := extract(t, "custom_rust_poem", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id}")
	}
}

// gotham route.get("/users/:id") — colon param normalised to {id}.
func TestGothamParamNormalised(t *testing.T) {
	src := `route.get("/users/:id").to(get_user);`
	ents := extract(t, "custom_rust_gotham", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id}")
	}
}

// salvo Router::with_path uses <id> rocket-style param -> {id}.
func TestSalvoParamNormalised(t *testing.T) {
	src := `let r = Router::with_path("/users").path("/<id>").get(get_user);`
	ents := extract(t, "custom_rust_salvo", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "path:/{id}") {
		t.Error("expected path:/{id} component (salvo <id> normalised)")
	}
}
