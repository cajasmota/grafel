package rust_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/rust"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Actix-web
// ---------------------------------------------------------------------------

func TestActixMacroRoute(t *testing.T) {
	src := `
#[get("/users")]
async fn list_users() -> impl Responder { HttpResponse::Ok() }

#[post("/users")]
async fn create_user() -> impl Responder { HttpResponse::Created() }
`
	ents := extract(t, "custom_rust_actix_web", fi("handlers.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestActixHttpServer(t *testing.T) {
	src := `
let server = HttpServer::new(|| {
    App::new().service(list_users)
})
.bind("127.0.0.1:8080")?
.run();
`
	ents := extract(t, "custom_rust_actix_web", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "HttpServer") {
		t.Error("expected HttpServer SCOPE.Service")
	}
}

func TestActixScope(t *testing.T) {
	src := `
let app = App::new().service(
    web::scope("/api")
        .service(list_users)
);
`
	ents := extract(t, "custom_rust_actix_web", fi("app.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Component", "/api") {
		t.Error("expected /api scope component")
	}
}

func TestActixMiddleware(t *testing.T) {
	src := `
App::new()
    .wrap(Logger::default())
    .wrap(Compress::default())
`
	ents := extract(t, "custom_rust_actix_web", fi("app.rs", "rust", src))
	// middleware entity name includes the full captured path up to ::
	if !containsEntity(ents, "SCOPE.Pattern", "middleware:Logger::default") {
		t.Error("expected middleware:Logger::default pattern")
	}
}

func TestActixNoMatch(t *testing.T) {
	src := `fn main() { println!("hello"); }`
	ents := extract(t, "custom_rust_actix_web", fi("main.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Axum
// ---------------------------------------------------------------------------

func TestAxumRoute(t *testing.T) {
	src := `
let app = Router::new()
    .route("/users", get(list_users))
    .route("/users/:id", post(create_user));
`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
}

func TestAxumNest(t *testing.T) {
	src := `
let app = Router::new()
    .nest("/api", api_router);
`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	// nest entity name = "nest:" + prefix
	if !containsEntity(ents, "SCOPE.Component", "nest:/api") {
		t.Error("expected nest:/api nested component")
	}
}

func TestAxumLayer(t *testing.T) {
	src := `
let app = Router::new()
    .layer(TraceLayer::new_for_http());
`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	// layer entity name = "layer:" + full captured type path
	if !containsEntity(ents, "SCOPE.Pattern", "layer:TraceLayer::new_for_http") {
		t.Error("expected layer:TraceLayer::new_for_http pattern")
	}
}

func TestAxumServer(t *testing.T) {
	src := `axum::serve(listener, app).await.unwrap();`
	ents := extract(t, "custom_rust_axum", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "axum::serve") {
		t.Error("expected axum::serve service")
	}
}

func TestAxumNoMatch(t *testing.T) {
	src := `struct Foo { bar: u32 }`
	ents := extract(t, "custom_rust_axum", fi("types.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Rocket
// ---------------------------------------------------------------------------

func TestRocketMacroRoute(t *testing.T) {
	src := `
#[get("/users")]
fn list_users() -> &'static str { "users" }

#[post("/users")]
fn create_user() -> Status { Status::Created }
`
	ents := extract(t, "custom_rust_rocket", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users") {
		t.Error("expected GET /users route")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /users") {
		t.Error("expected POST /users route")
	}
}

func TestRocketCatcher(t *testing.T) {
	src := `
#[catch(404)]
fn not_found(req: &Request) -> String { format!("Not found: {}", req.uri()) }
`
	ents := extract(t, "custom_rust_rocket", fi("catchers.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Pattern", "catch:404") {
		t.Error("expected 404 catcher pattern")
	}
}

func TestRocketBuild(t *testing.T) {
	src := `rocket::build().mount("/", routes![list_users]).launch().await`
	ents := extract(t, "custom_rust_rocket", fi("main.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Service", "rocket::build") {
		t.Error("expected rocket::build service")
	}
}

func TestRocketNoMatch(t *testing.T) {
	src := `use std::collections::HashMap;`
	ents := extract(t, "custom_rust_rocket", fi("imports.rs", "rust", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
