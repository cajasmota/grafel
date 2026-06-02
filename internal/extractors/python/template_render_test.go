package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func tplEdge(recs []types.EntityRecord, fromName, name string) bool {
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

func tplNode(recs []types.EntityRecord, name string) int {
	want := extractor.TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

func TestPyTemplate_FlaskRenderTemplate(t *testing.T) {
	src := `from flask import render_template

@app.route("/dashboard")
def dashboard():
    return render_template("dashboard.html", user=u)
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "dashboard", "dashboard.html") {
		t.Error("missing RENDERS(dashboard -> dashboard.html)")
	}
	if tplNode(recs, "dashboard.html") != 1 {
		t.Error("expected one SCOPE.Template node dashboard.html")
	}
}

func TestPyTemplate_DjangoRender(t *testing.T) {
	src := `from django.shortcuts import render

def home(request):
    return render(request, "home.html", {"x": 1})
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "home", "home.html") {
		t.Error("missing RENDERS(home -> home.html)")
	}
	if tplNode(recs, "home.html") != 1 {
		t.Error("expected one SCOPE.Template node home.html")
	}
}

func TestPyTemplate_DjangoTemplateView(t *testing.T) {
	src := `from django.views.generic import TemplateView

class AboutView(TemplateView):
    template_name = "about.html"
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "AboutView", "about.html") {
		t.Error("missing RENDERS(AboutView -> about.html)")
	}
	if tplNode(recs, "about.html") != 1 {
		t.Error("expected one SCOPE.Template node about.html")
	}
}

// Convergence: two views rendering the same template → one node.
func TestPyTemplate_Convergence(t *testing.T) {
	src := `from flask import render_template

def list_users():
    return render_template("users/list.html")

def search_users():
    return render_template("users/list.html")
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "list_users", "users/list.html") {
		t.Error("missing RENDERS(list_users -> users/list.html)")
	}
	if !tplEdge(recs, "search_users", "users/list.html") {
		t.Error("missing RENDERS(search_users -> users/list.html)")
	}
	if n := tplNode(recs, "users/list.html"); n != 1 {
		t.Fatalf("expected ONE converged template node, got %d", n)
	}
}

// Negative: a dynamic (variable / computed) template name yields no node/edge.
func TestPyTemplate_DynamicDropped(t *testing.T) {
	src := `from flask import render_template

def show(name):
    page = name + ".html"
    return render_template(page)
`
	recs := extractPy(t, src, "views.py")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			t.Fatalf("dynamic render_template must not create a template node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" {
				t.Fatalf("dynamic template name must not yield a RENDERS edge")
			}
		}
	}
}
