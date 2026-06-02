package python_test

import "testing"

// findEndpoint returns the route-endpoint extractResult whose path property
// matches, or fails the test.
func findEndpoint(t *testing.T, ents []extractResult, path string) extractResult {
	t.Helper()
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "endpoint" && e.Props["path"] == path {
			return e
		}
	}
	t.Fatalf("no endpoint with path %q (got %d entities)", path, len(ents))
	return extractResult{}
}

// ---------------------------------------------------------------------------
// FastAPI endpoint protection (#3628 area #6)
// ---------------------------------------------------------------------------

func TestFastAPIAuth_DependsCurrentUser(t *testing.T) {
	src := `
from fastapi import FastAPI, Depends
app = FastAPI()

@app.get("/me")
def read_me(user = Depends(get_current_user)):
    return user
`
	ents := extract(t, "python_fastapi", src)
	e := findEndpoint(t, ents, "/me")
	if e.Props["auth_required"] != "true" {
		t.Fatalf("/me auth_required = %q, want true", e.Props["auth_required"])
	}
	if e.Props["auth_guard"] != "get_current_user" {
		t.Fatalf("/me auth_guard = %q, want get_current_user", e.Props["auth_guard"])
	}
	if e.Props["auth_method"] != "dependency" {
		t.Fatalf("/me auth_method = %q, want dependency", e.Props["auth_method"])
	}
}

func TestFastAPIAuth_SecurityScopes(t *testing.T) {
	src := `
from fastapi import FastAPI, Security
app = FastAPI()

@app.get("/items")
def list_items(user = Security(get_user, scopes=["items:read", "items:list"])):
    return []
`
	ents := extract(t, "python_fastapi", src)
	e := findEndpoint(t, ents, "/items")
	if e.Props["auth_required"] != "true" {
		t.Fatalf("/items auth_required = %q, want true", e.Props["auth_required"])
	}
	if e.Props["auth_guard"] != "get_user" {
		t.Fatalf("/items auth_guard = %q, want get_user", e.Props["auth_guard"])
	}
	if e.Props["auth_scopes"] != "items:list,items:read" {
		t.Fatalf("/items auth_scopes = %q, want items:list,items:read", e.Props["auth_scopes"])
	}
}

func TestFastAPIAuth_DependenciesKwarg(t *testing.T) {
	src := `
from fastapi import FastAPI, Depends
app = FastAPI()

@app.post("/admin", dependencies=[Depends(verify_token)])
def do_admin():
    return {}
`
	ents := extract(t, "python_fastapi", src)
	e := findEndpoint(t, ents, "/admin")
	if e.Props["auth_required"] != "true" {
		t.Fatalf("/admin auth_required = %q, want true", e.Props["auth_required"])
	}
	if e.Props["auth_guard"] != "verify_token" {
		t.Fatalf("/admin auth_guard = %q, want verify_token", e.Props["auth_guard"])
	}
}

// Negative: a plain (non-auth) dependency must NOT mark the endpoint protected.
func TestFastAPIAuth_PlainDependencyNotProtected(t *testing.T) {
	src := `
from fastapi import FastAPI, Depends
app = FastAPI()

@app.get("/widgets")
def list_widgets(db = Depends(get_db)):
    return []
`
	ents := extract(t, "python_fastapi", src)
	e := findEndpoint(t, ents, "/widgets")
	if v, ok := e.Props["auth_required"]; ok {
		t.Fatalf("/widgets should have no auth_required, got %q", v)
	}
	if v, ok := e.Props["auth_guard"]; ok {
		t.Fatalf("/widgets should have no auth_guard, got %q", v)
	}
}

// Negative: an endpoint with no dependency at all is unprotected.
func TestFastAPIAuth_NoDependencyUnprotected(t *testing.T) {
	src := `
from fastapi import FastAPI
app = FastAPI()

@app.get("/health")
def health():
    return {"ok": True}
`
	ents := extract(t, "python_fastapi", src)
	e := findEndpoint(t, ents, "/health")
	if _, ok := e.Props["auth_required"]; ok {
		t.Fatalf("/health should be unprotected, got auth_required=%q", e.Props["auth_required"])
	}
}

// ---------------------------------------------------------------------------
// Flask endpoint protection (#3628 area #6)
// ---------------------------------------------------------------------------

func TestFlaskAuth_LoginRequired(t *testing.T) {
	src := `
from flask import Flask
from flask_login import login_required
app = Flask(__name__)

@app.route("/dashboard")
@login_required
def dashboard():
    return "ok"
`
	ents := extract(t, "python_flask", src)
	e := findEndpoint(t, ents, "/dashboard")
	if e.Props["auth_required"] != "true" {
		t.Fatalf("/dashboard auth_required = %q, want true", e.Props["auth_required"])
	}
	if e.Props["auth_guard"] != "login_required" {
		t.Fatalf("/dashboard auth_guard = %q, want login_required", e.Props["auth_guard"])
	}
	if e.Props["auth_method"] != "decorator" {
		t.Fatalf("/dashboard auth_method = %q, want decorator", e.Props["auth_method"])
	}
}

func TestFlaskAuth_RolesRequired(t *testing.T) {
	src := `
from flask import Flask
from flask_security import roles_required
app = Flask(__name__)

@app.get("/admin")
@roles_required('admin', 'superuser')
def admin_panel():
    return "ok"
`
	ents := extract(t, "python_flask", src)
	e := findEndpoint(t, ents, "/admin")
	if e.Props["auth_required"] != "true" {
		t.Fatalf("/admin auth_required = %q, want true", e.Props["auth_required"])
	}
	if e.Props["auth_roles"] != "admin,superuser" {
		t.Fatalf("/admin auth_roles = %q, want admin,superuser", e.Props["auth_roles"])
	}
}

// Django `@permission_required('orders.delete_order')` on a function view must
// stamp the specific required permission on auth_permissions (cross-language
// flat contract), answering "what permission does this route require?".
func TestDjangoAuth_PermissionRequiredView(t *testing.T) {
	src := `
from django.contrib.auth.decorators import permission_required

@permission_required('orders.delete_order')
def delete_order(request, pk):
    return None
`
	ents := extract(t, "python_django", src)
	var found bool
	for _, e := range ents {
		if e.Props["decorator"] == "permission_required" {
			found = true
			if e.Props["auth_permissions"] != "orders.delete_order" {
				t.Fatalf("auth_permissions = %q, want orders.delete_order", e.Props["auth_permissions"])
			}
			if e.Props["auth_required"] != "true" {
				t.Fatalf("auth_required = %q, want true", e.Props["auth_required"])
			}
		}
	}
	if !found {
		t.Fatalf("no permission_required view emitted (got %d entities)", len(ents))
	}
}

// Negative: an unguarded Flask route is unprotected.
func TestFlaskAuth_NoDecoratorUnprotected(t *testing.T) {
	src := `
from flask import Flask
app = Flask(__name__)

@app.route("/public")
def public():
    return "hi"
`
	ents := extract(t, "python_flask", src)
	e := findEndpoint(t, ents, "/public")
	if _, ok := e.Props["auth_required"]; ok {
		t.Fatalf("/public should be unprotected, got auth_required=%q", e.Props["auth_required"])
	}
}
