package php_test

// template_render_test.go — value-asserting tests for the PHP/Laravel
// view-layer pass (epic #3628). Asserts the SPECIFIC template node + RENDERS
// edge, not len>0.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func phpTplEdge(recs []types.EntityRecord, fromName, name string) bool {
	want := extractor.TemplateTargetID(name)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func phpTplNode(recs []types.EntityRecord, name string) int {
	want := extractor.TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

func phpAnyTplNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			return true
		}
	}
	return false
}

// TestPHPTemplate_LaravelViewHelper: return view('welcome') in a controller
// action → RENDERS welcome.
func TestPHPTemplate_LaravelViewHelper(t *testing.T) {
	src := `<?php
class HomeController {
    public function index() {
        return view('welcome');
    }
}
`
	recs := extractPHPRecords(t, src)
	if !phpTplEdge(recs, "HomeController.index", "welcome") {
		t.Error("missing RENDERS(HomeController.index -> welcome)")
	}
	if phpTplNode(recs, "welcome") != 1 {
		t.Error("expected one SCOPE.Template node welcome")
	}
}

// TestPHPTemplate_DotNotation: view('users.list') → users/list (dot → slash).
func TestPHPTemplate_DotNotation(t *testing.T) {
	src := `<?php
class UserController {
    public function list() {
        return view('users.list', $data);
    }
}
`
	recs := extractPHPRecords(t, src)
	if !phpTplEdge(recs, "UserController.list", "users/list") {
		t.Error("missing RENDERS(UserController.list -> users/list) — dot-notation must normalize to slash")
	}
}

// TestPHPTemplate_ViewMakeFacade: return View::make('dashboard') → RENDERS.
func TestPHPTemplate_ViewMakeFacade(t *testing.T) {
	src := `<?php
class DashController {
    public function show() {
        return View::make('dashboard');
    }
}
`
	recs := extractPHPRecords(t, src)
	if !phpTplEdge(recs, "DashController.show", "dashboard") {
		t.Error("missing RENDERS(DashController.show -> dashboard)")
	}
}

// TestPHPTemplate_DynamicDropped: a variable view name is dropped.
func TestPHPTemplate_DynamicDropped(t *testing.T) {
	src := `<?php
class C {
    public function show($name) {
        return view($name);
    }
}
`
	recs := extractPHPRecords(t, src)
	if phpAnyTplNode(recs) {
		t.Error("dynamic view($name) must not produce a template node")
	}
}

// TestPHPTemplate_Convergence: two actions rendering the same view → one node.
func TestPHPTemplate_Convergence(t *testing.T) {
	src := `<?php
class C {
    public function a() { return view('users.list'); }
    public function b() { return view('users.list'); }
}
`
	recs := extractPHPRecords(t, src)
	if !phpTplEdge(recs, "C.a", "users/list") || !phpTplEdge(recs, "C.b", "users/list") {
		t.Fatal("both actions must RENDERS users/list")
	}
	if n := phpTplNode(recs, "users/list"); n != 1 {
		t.Fatalf("expected ONE converged template node, got %d", n)
	}
}
