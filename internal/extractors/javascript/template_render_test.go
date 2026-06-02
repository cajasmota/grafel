package javascript_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func jsTplEdge(recs []types.EntityRecord, fromName, name string) bool {
	want := extreg.TemplateTargetID(name)
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

func jsTplNode(recs []types.EntityRecord, name string) int {
	want := extreg.TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

// TestJSTemplate_ExpressResRender: res.render('profile') in a named handler.
func TestJSTemplate_ExpressResRender(t *testing.T) {
	src := []byte(`function profile(req, res) {
  res.render('profile', { user: req.user });
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTplEdge(recs, "profile", "profile") {
		t.Error("missing RENDERS(profile -> profile)")
	}
	if jsTplNode(recs, "profile") != 1 {
		t.Error("expected one SCOPE.Template node profile")
	}
}

// TestJSTemplate_PathArrow: arrow handler rendering a nested view path.
func TestJSTemplate_PathArrow(t *testing.T) {
	src := []byte(`const listUsers = (req, res) => {
  res.render("users/list");
};
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTplEdge(recs, "listUsers", "users/list") {
		t.Error("missing RENDERS(listUsers -> users/list)")
	}
}

// TestJSTemplate_Convergence: two handlers render the same view → one node.
func TestJSTemplate_Convergence(t *testing.T) {
	src := []byte(`function a(req, res) { res.render('home'); }
function b(req, res) { res.render('home'); }
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTplEdge(recs, "a", "home") || !jsTplEdge(recs, "b", "home") {
		t.Fatal("both handlers must RENDERS home")
	}
	if n := jsTplNode(recs, "home"); n != 1 {
		t.Fatalf("expected ONE converged template node, got %d", n)
	}
}

// TestJSTemplate_DynamicDropped: variable view name yields no edge/node.
func TestJSTemplate_DynamicDropped(t *testing.T) {
	src := []byte("function show(req, res) {\n  const view = req.params.view;\n  res.render(view);\n  res.render(`p/${req.id}`);\n}\n")
	recs := extract(t, src, "javascript", parseJS(t, src))
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			t.Fatalf("dynamic render must not create a template node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" {
				t.Fatal("dynamic view name must not yield a RENDERS edge")
			}
		}
	}
}

// TestJSTemplate_NonResponseReceiver: a non-response receiver `.render()` is not
// a server-side view render (avoid false positives on component .render()).
func TestJSTemplate_NonResponseReceiver(t *testing.T) {
	src := []byte(`function draw(widget) {
  widget.render('whatever');
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			t.Fatalf("widget.render must not create a template node, got %q", recs[i].Name)
		}
	}
}
