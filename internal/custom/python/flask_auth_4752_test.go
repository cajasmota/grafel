package python

// flask_auth_4752_test.go — #4752 Flask roles/permission decorator stamping +
// view_source fallback: the engine now stamps auth_roles/auth_permissions/
// auth_page/auth_decorator + view_source so the authposture flask resolver
// decodes role/permission/admin postures structurally (not just unknown).

import "testing"

func TestFlaskAuth_RolesRequired_4752(t *testing.T) {
	src := `
from flask import Flask
from flask_security import roles_required

app = Flask(__name__)

@app.route('/admin')
@roles_required('admin')
def admin():
    return 'ok'
`
	eps := pyEndpointProps(t, &FlaskExtractor{}, "app.py", src)
	e, ok := eps["admin"]
	if !ok {
		t.Fatalf("no admin endpoint; got %+v", eps)
	}
	if e.Properties["auth_roles"] != "admin" {
		t.Errorf("auth_roles=%q, want admin", e.Properties["auth_roles"])
	}
	if e.Properties["auth_decorator"] != "roles_required" {
		t.Errorf("auth_decorator=%q, want roles_required", e.Properties["auth_decorator"])
	}
	if e.Properties["view_source"] == "" {
		t.Errorf("view_source not stamped")
	}
}

func TestFlaskAuth_PermissionRequired_4752(t *testing.T) {
	src := `
from flask import Flask
from flask_principal import permission_required

app = Flask(__name__)

@app.route('/export')
@permission_required('export')
def export():
    return 'ok'
`
	eps := pyEndpointProps(t, &FlaskExtractor{}, "app.py", src)
	e, ok := eps["export"]
	if !ok {
		t.Fatalf("no export endpoint; got %+v", eps)
	}
	if e.Properties["auth_permissions"] != "export" {
		t.Errorf("auth_permissions=%q, want export", e.Properties["auth_permissions"])
	}
	if e.Properties["auth_page"] != "export" {
		t.Errorf("auth_page=%q, want export", e.Properties["auth_page"])
	}
}

func TestFlaskAuth_AdminDecorator_4752(t *testing.T) {
	// A bare @admin_required (no role arg) — decoded by decorator NAME.
	src := `
from flask import Flask
from .auth import admin_required

app = Flask(__name__)

@app.route('/dashboard')
@admin_required
def dashboard():
    return 'ok'
`
	eps := pyEndpointProps(t, &FlaskExtractor{}, "app.py", src)
	e, ok := eps["dashboard"]
	if !ok {
		t.Fatalf("no dashboard endpoint; got %+v", eps)
	}
	if e.Properties["auth_decorator"] != "admin_required" {
		t.Errorf("auth_decorator=%q, want admin_required", e.Properties["auth_decorator"])
	}
}
