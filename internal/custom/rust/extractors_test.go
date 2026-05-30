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
		out = append(out, entitySummary{
			Kind:    ent.Kind,
			Subtype: ent.Subtype,
			Name:    ent.Name,
			Props:   ent.Properties,
		})
	}
	return out
}

type entitySummary struct {
	Kind, Subtype, Name string
	Props               map[string]string
}

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

// ---------------------------------------------------------------------------
// Deep route-extraction tests (value-asserting): param normalisation,
// nest/scope/mount prefix composition, axum method-router chains.
// ---------------------------------------------------------------------------

// Axum 0.7+ brace params are kept canonical; the exact (verb, path) is asserted.
func TestAxumBraceParam(t *testing.T) {
	src := `let app = Router::new().route("/users/{id}", get(get_user));`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id}")
	}
}

// Axum 0.6 colon params are normalised to canonical {id} form.
func TestAxumColonParamNormalised(t *testing.T) {
	src := `let app = Router::new().route("/users/:id", get(get_user));`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id} from :id param")
	}
}

// A chained method router get(h).post(h2).delete(h3) yields one endpoint per verb.
func TestAxumMethodRouterChain(t *testing.T) {
	src := `let app = Router::new()
        .route("/users/{id}", get(get_user).post(update_user).delete(delete_user));`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	for _, want := range []string{"GET /users/{id}", "POST /users/{id}", "DELETE /users/{id}"} {
		if !containsEntity(ents, "SCOPE.Operation", want) {
			t.Errorf("expected %q", want)
		}
	}
}

// .nest("/api", api) composes the prefix onto routes of the api sub-router.
func TestAxumNestPrefixComposed(t *testing.T) {
	src := `
let api = Router::new()
    .route("/users/{id}", get(get_user));
let app = Router::new()
    .nest("/api", api);
`
	ents := extract(t, "custom_rust_axum", fi("router.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users/{id}") {
		t.Error("expected nest prefix composed: GET /api/users/{id}")
	}
}

// actix attribute macro carries typed path param; normalised path asserted.
func TestActixAttrMacroParam(t *testing.T) {
	src := `
#[get("/users/{id}")]
async fn get_user(path: web::Path<u32>) -> impl Responder { HttpResponse::Ok() }
`
	ents := extract(t, "custom_rust_actix_web", fi("h.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /users/{id}") {
		t.Error("expected GET /users/{id}")
	}
}

// web::scope("/api") composes onto manual .route(...) on the same chain.
func TestActixScopePrefixComposed(t *testing.T) {
	src := `
let api = web::scope("/api").route("/users/{id}", web::get().to(get_user));
`
	ents := extract(t, "custom_rust_actix_web", fi("app.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users/{id}") {
		t.Error("expected scope prefix composed: GET /api/users/{id}")
	}
}

// Rocket <id> param normalised to {id}; mount prefix composed.
func TestRocketParamAndMount(t *testing.T) {
	src := `
#[get("/users/<id>")]
fn get_user(id: u32) -> &'static str { "u" }

#[post("/users", data = "<new>")]
fn create_user(new: Json<User>) -> Status { Status::Created }

rocket::build().mount("/api", routes![get_user, create_user]);
`
	ents := extract(t, "custom_rust_rocket", fi("routes.rs", "rust", src))
	if !containsEntity(ents, "SCOPE.Operation", "GET /api/users/{id}") {
		t.Error("expected GET /api/users/{id} (param + mount composed)")
	}
	if !containsEntity(ents, "SCOPE.Operation", "POST /api/users") {
		t.Error("expected POST /api/users (data= kwarg tolerated, mount composed)")
	}
}
