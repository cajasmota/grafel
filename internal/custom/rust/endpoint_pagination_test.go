package rust_test

import (
	"testing"
)

// --- axum: Query<Struct> typed extractor -------------------------------------

func TestRustPagination_AxumQueryStructOffset(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct Pagination {
    limit: u32,
    offset: u32,
}

async fn list_users(Query(p): Query<Pagination>) -> impl IntoResponse {
    "ok"
}

fn app() -> Router {
    Router::new().route("/users", get(list_users))
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("axum.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /users")
	if op == nil {
		t.Fatalf("expected GET /users, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "offset")
	propEq(t, op, "pagination_params", "limit,offset")
	propEq(t, op, "framework", "axum")
}

func TestRustPagination_AxumQueryStructCursor(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct CursorParams {
    cursor: Option<String>,
    limit: u32,
}

async fn feed(Query(p): Query<CursorParams>) -> impl IntoResponse { "ok" }

fn app() -> Router { Router::new().route("/feed", get(feed)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("cursor.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /feed")
	if op == nil {
		t.Fatalf("expected GET /feed, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "cursor")
	propEq(t, op, "pagination_params", "cursor,limit")
}

func TestRustPagination_AxumNestComposes(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct Page { page: u32 }

async fn list(Query(p): Query<Page>) -> impl IntoResponse { "ok" }

fn build() -> Router {
    let api = Router::new().route("/items", get(list));
    Router::new().nest("/api/v1", api)
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("nest.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /api/v1/items")
	if op == nil {
		t.Fatalf("expected nest-composed GET /api/v1/items, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "page")
	propEq(t, op, "nest_prefix", "/api/v1")
}

// --- ORM signals -------------------------------------------------------------

func TestRustPagination_AxumDieselLimitOffset(t *testing.T) {
	src := `
async fn list_posts() -> impl IntoResponse {
    let results = posts::table
        .limit(per_page)
        .offset(per_page * page)
        .load::<Post>(&mut conn);
    Json(results)
}

fn app() -> Router { Router::new().route("/posts", get(list_posts)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("diesel.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /posts")
	if op == nil {
		t.Fatalf("expected GET /posts, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "offset")
	propEq(t, op, "pagination_params", "limit,offset")
	propEq(t, op, "pagination_source", "diesel/sqlx limit/offset")
}

func TestRustPagination_AxumSeaOrmPaginator(t *testing.T) {
	src := `
async fn list_orders() -> impl IntoResponse {
    let paginator = Order::find().paginate(&db, 50);
    Json(paginator.fetch_page(0).await.unwrap())
}

fn app() -> Router { Router::new().route("/orders", get(list_orders)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("seaorm.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /orders")
	if op == nil {
		t.Fatalf("expected GET /orders, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "page")
	propEq(t, op, "pagination_source", "sea_orm Paginator")
}

// --- actix: web::Query<Struct> -----------------------------------------------

func TestRustPagination_ActixQueryStruct(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct PageParams {
    page: u32,
    page_size: u32,
}

#[get("/items")]
async fn list_items(q: web::Query<PageParams>) -> impl Responder {
    HttpResponse::Ok().finish()
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("actix.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /items")
	if op == nil {
		t.Fatalf("expected GET /items, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "page")
	propEq(t, op, "pagination_params", "page,page_size")
	propEq(t, op, "framework", "actix_web")
}

// --- rocket: literal query reads + mount prefix ------------------------------

func TestRustPagination_RocketQueryStruct(t *testing.T) {
	src := `
#[derive(FromForm)]
struct Paging { limit: usize, offset: usize }

#[get("/posts")]
fn list_posts(q: Query<Paging>) -> Json<Vec<Post>> {
    Json(vec![])
}

#[launch]
fn rocket() -> _ {
    rocket::build().mount("/api/v1", routes![list_posts])
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("rocket.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /api/v1/posts")
	if op == nil {
		t.Fatalf("expected mount-composed GET /api/v1/posts, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "offset")
	propEq(t, op, "framework", "rocket")
	propEq(t, op, "mount_prefix", "/api/v1")
}

// --- handler-named minor frameworks ------------------------------------------

func TestRustPagination_PoemQueryStruct(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct Paging { limit: u32, offset: u32 }

async fn list(Query(p): Query<Paging>) -> Response { Response::default() }

fn route() -> Route { Route::new().at("/items", get(list)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("poem.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /items")
	if op == nil {
		t.Fatalf("expected GET /items, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "offset")
	propEq(t, op, "framework", "poem")
}

func TestRustPagination_TideLiteralReads(t *testing.T) {
	src := `
async fn list(req: Request<()>) -> tide::Result {
    let limit = req.query::<String>("limit").get("limit");
    let cursor = req.query::<String>("cursor").get("cursor");
    Ok(Response::new(200))
}

fn app() {
    let mut app = tide::new();
    app.at("/feed").get(list);
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("tide.rs", "rust", src))
	op := findRustDep(ents, "SCOPE.Operation", "GET /feed")
	if op == nil {
		t.Fatalf("expected GET /feed, got %+v", ents)
	}
	propEq(t, op, "paginated", "true")
	propEq(t, op, "pagination_style", "cursor")
	propEq(t, op, "framework", "tide")
}

// --- negatives / honest-partial ----------------------------------------------

func TestRustPagination_LoneLimitNotStamped(t *testing.T) {
	// A lone limit-like param is ambiguous and must NOT be stamped.
	src := `
#[derive(Deserialize)]
struct OnlyLimit { limit: u32 }

async fn list(Query(p): Query<OnlyLimit>) -> impl IntoResponse { "ok" }

fn app() -> Router { Router::new().route("/users", get(list)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("lone.rs", "rust", src))
	if findRustDep(ents, "SCOPE.Operation", "GET /users") != nil {
		t.Errorf("lone limit must NOT be stamped, got %+v", ents)
	}
}

func TestRustPagination_NoPaginationNotStamped(t *testing.T) {
	src := `
async fn list() -> impl IntoResponse { "ok" }
fn app() -> Router { Router::new().route("/users", get(list)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("plain.rs", "rust", src))
	if findRustDep(ents, "SCOPE.Operation", "GET /users") != nil {
		t.Errorf("handler with no pagination shape must NOT be stamped, got %+v", ents)
	}
}

func TestRustPagination_LimitOnlyOrmNotStamped(t *testing.T) {
	// A .limit() with no .offset() companion is ambiguous (a cap, not paging).
	src := `
async fn top() -> impl IntoResponse {
    let results = posts::table.limit(10).load::<Post>(&mut conn);
    Json(results)
}
fn app() -> Router { Router::new().route("/top", get(top)) }
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("limitonly.rs", "rust", src))
	if findRustDep(ents, "SCOPE.Operation", "GET /top") != nil {
		t.Errorf("lone .limit() must NOT be stamped, got %+v", ents)
	}
}

// --- hyper: match-arm dispatch (#5101) ---------------------------------------

// hyper — NAMED-handler arm: `(&Method::GET, "/p") => handler(req)`. The arm RHS
// names a handler whose body carries the pagination idiom (diesel limit+offset),
// resolved via the shared handler->verdict map.
func TestRustPagination_HyperNamedHandler(t *testing.T) {
	src := `
async fn list_users(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    let rows = users::table.limit(20).offset(40).load::<User>(&mut conn).unwrap();
    Ok(Response::new(Body::from(serde_json::to_string(&rows).unwrap())))
}
async fn create_user(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    Ok(Response::new(Body::empty()))
}
async fn router(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    match (req.method(), req.uri().path()) {
        (&Method::GET, "/users") => list_users(req).await,
        (&Method::POST, "/users") => create_user(req).await,
        _ => not_found(),
    }
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("hyper.rs", "rust", src))

	get := findRustDep(ents, "SCOPE.Operation", "GET /users")
	if get == nil {
		t.Fatalf("expected GET /users, got %+v", ents)
	}
	propEq(t, get, "paginated", "true")
	propEq(t, get, "pagination_style", "offset")
	propEq(t, get, "pagination_params", "limit,offset")
	propEq(t, get, "framework", "hyper")

	// honest-partial: create_user has no pagination shape -> not stamped.
	if findRustDep(ents, "SCOPE.Operation", "POST /users") != nil {
		t.Errorf("POST /users handler has no pagination shape; must NOT be stamped, got %+v", ents)
	}
}

// hyper — INLINE-block arm: the pagination idiom (typed Query<Struct>) is written
// directly in the arm body, no separate handler fn, clipped at the next arm so a
// sibling arm's signal does not bleed in.
func TestRustPagination_HyperInlineArm(t *testing.T) {
	src := `
#[derive(Deserialize)]
struct Page { page: u32 }
async fn router(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    match (req.method(), req.uri().path()) {
        (&Method::GET, "/feed") => {
            let q: Query<Page> = parse_query(req.uri());
            Ok(Response::new(Body::empty()))
        }
        (&Method::GET, "/health") => {
            Ok(Response::new(Body::from("ok")))
        }
        _ => Ok(Response::new(Body::empty())),
    }
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("hyper_inline.rs", "rust", src))

	feed := findRustDep(ents, "SCOPE.Operation", "GET /feed")
	if feed == nil {
		t.Fatalf("expected GET /feed, got %+v", ents)
	}
	propEq(t, feed, "paginated", "true")
	propEq(t, feed, "pagination_style", "page")
	propEq(t, feed, "pagination_params", "page")
	propEq(t, feed, "framework", "hyper")

	// honest-partial: the /health arm has no pagination shape -> not stamped, and
	// the clip prevents /feed's signal from leaking into it.
	if findRustDep(ents, "SCOPE.Operation", "GET /health") != nil {
		t.Errorf("GET /health arm has no pagination shape; must NOT be stamped, got %+v", ents)
	}
}

// hyper — wrong language is a no-op (the extractor only runs on rust files).
func TestRustPagination_HyperWrongLanguageNoOp(t *testing.T) {
	src := `(&Method::GET, "/users") => list_users(req).await,
fn list_users() { users::table.limit(20).offset(40); }`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("hyper.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("non-rust file must produce no entities, got %+v", ents)
	}
}

// hyper — honest-partial: a match-arm whose handler/body has no pagination shape
// is NOT re-emitted (no fabricated pagination).
func TestRustPagination_HyperNoPaginationNotStamped(t *testing.T) {
	src := `
async fn list_users(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    Ok(Response::new(Body::from("ok")))
}
async fn router(req: Request<Body>) -> Result<Response<Body>, hyper::Error> {
    match (req.method(), req.uri().path()) {
        (&Method::GET, "/users") => list_users(req).await,
        _ => not_found(),
    }
}
`
	ents := extract(t, "custom_rust_endpoint_pagination", fi("hyper_none.rs", "rust", src))
	if findRustDep(ents, "SCOPE.Operation", "GET /users") != nil {
		t.Errorf("hyper arm with no pagination shape must NOT be stamped, got %+v", ents)
	}
}
