package rust_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/rust"
)

// extractRecords returns the raw EntityRecords (with Relationships) for a
// custom extractor — the entitySummary helper drops edges.
func extractRecords(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// findRel returns the first relationship of kind k with the given from/to
// symbols, plus whether it was found.
func findRel(ents []types.EntityRecord, kind, from, to string) (types.RelationshipRecord, bool) {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == kind && r.FromID == from && r.ToID == to {
				return r, true
			}
		}
	}
	return types.RelationshipRecord{}, false
}

func findBinding(ents []types.EntityRecord, injectedType string) (types.EntityRecord, bool) {
	for _, e := range ents {
		if e.Subtype == "di_binding" && e.Properties["injected_type"] == injectedType {
			return e, true
		}
	}
	return types.EntityRecord{}, false
}

// #4963: axum .with_state(AppState) binds the app-singleton state and a handler
// `State(s): State<AppState>` extracts it. Assert: di_binding entity (scope=
// singleton), BINDS edge from the type to the registration site, and an
// INJECTED_INTO edge from AppState -> the handler.
func TestAxumWithStateBindingAndInjection(t *testing.T) {
	src := `
async fn handler(State(s): State<AppState>) -> String { String::new() }

fn main() {
    let app = Router::new()
        .route("/", get(handler))
        .with_state(AppState::new());
}
`
	ents := extractRecords(t, "custom_rust_di_graph", fi("app.rs", "rust", src))

	b, ok := findBinding(ents, "AppState")
	if !ok {
		t.Fatal("expected di_binding entity for AppState")
	}
	if b.Properties["scope"] != "singleton" {
		t.Errorf("AppState scope = %q, want singleton", b.Properties["scope"])
	}
	if b.Properties["mechanism"] != "state" || b.Properties["di_framework"] != "axum" {
		t.Errorf("binding props = %v", b.Properties)
	}

	if _, ok := findRel(ents, string(types.RelationshipKindBinds), "AppState", "axum_registration:AppState"); !ok {
		t.Error("expected BINDS edge AppState -> registration site")
	}
	rel, ok := findRel(ents, string(types.RelationshipKindInjectedInto), "AppState", "handler")
	if !ok {
		t.Fatal("expected INJECTED_INTO edge AppState -> handler")
	}
	if rel.Properties["scope"] != "singleton" || rel.Properties["mechanism"] != "state" {
		t.Errorf("injection edge props = %v", rel.Properties)
	}
}

// #4963: axum .layer(Extension(value)) binds a REQUEST-scoped extension; the
// handler `Extension(u): Extension<CurrentUser>` extracts it.
func TestAxumExtensionLayerBindingIsRequestScoped(t *testing.T) {
	src := `
async fn handler(Extension(user): Extension<CurrentUser>) -> String { String::new() }

fn main() {
    let app = Router::new()
        .route("/me", get(handler))
        .layer(Extension(CurrentUser::default()));
}
`
	ents := extractRecords(t, "custom_rust_di_graph", fi("ext.rs", "rust", src))

	b, ok := findBinding(ents, "CurrentUser")
	if !ok {
		t.Fatal("expected di_binding entity for CurrentUser")
	}
	if b.Properties["scope"] != "request" {
		t.Errorf("CurrentUser scope = %q, want request (Extension is request-scoped)", b.Properties["scope"])
	}
	if _, ok := findRel(ents, string(types.RelationshipKindInjectedInto), "CurrentUser", "handler"); !ok {
		t.Error("expected INJECTED_INTO edge CurrentUser -> handler")
	}
}

// #4963: actix App::app_data(web::Data::new(DbPool)) binds the app-singleton
// data value extracted by `db: web::Data<DbPool>`.
func TestActixAppDataBindingAndInjection(t *testing.T) {
	src := `
async fn handler(db: web::Data<DbPool>) -> impl Responder { HttpResponse::Ok() }

fn main() {
    HttpServer::new(|| {
        App::new()
            .app_data(web::Data::new(DbPool::connect()))
            .route("/users", web::get().to(handler))
    });
}
`
	ents := extractRecords(t, "custom_rust_di_graph", fi("actix.rs", "rust", src))

	b, ok := findBinding(ents, "DbPool")
	if !ok {
		t.Fatal("expected di_binding entity for DbPool")
	}
	if b.Properties["scope"] != "singleton" || b.Properties["di_framework"] != "actix_web" {
		t.Errorf("DbPool binding props = %v", b.Properties)
	}
	if _, ok := findRel(ents, string(types.RelationshipKindBinds), "DbPool", "actix_web_registration:DbPool"); !ok {
		t.Error("expected BINDS edge DbPool -> registration site")
	}
	if _, ok := findRel(ents, string(types.RelationshipKindInjectedInto), "DbPool", "handler"); !ok {
		t.Error("expected INJECTED_INTO edge DbPool -> handler")
	}
}

// Negative: a file with no registration site produces no binding edges (the
// di_injection_point pattern is owned by axum.go/actix_web.go, not this graph).
func TestRustDINoRegistrationNoBinding(t *testing.T) {
	src := `
async fn handler(State(s): State<AppState>) -> String { String::new() }
`
	ents := extractRecords(t, "custom_rust_di_graph", fi("h.rs", "rust", src))
	if _, ok := findBinding(ents, "AppState"); ok {
		t.Error("must not fabricate a binding without a .with_state registration site")
	}
}
