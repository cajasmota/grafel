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

// DRF browsable-API render path: renderer_classes naming the browsable / HTML
// renderer emits a RENDERS edge to a drf/<Renderer> convergence node, while a
// JSON-only renderer in the same list contributes NO render path.
func TestPyTemplate_DRFRendererClasses(t *testing.T) {
	src := `from rest_framework.viewsets import ViewSet
from rest_framework.renderers import JSONRenderer, BrowsableAPIRenderer

class FooViewSet(ViewSet):
    renderer_classes = [JSONRenderer, BrowsableAPIRenderer]
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "FooViewSet", "drf/BrowsableAPIRenderer") {
		t.Error("missing RENDERS(FooViewSet -> drf/BrowsableAPIRenderer)")
	}
	// JSONRenderer is a data renderer, not an HTML/browsable render path.
	if tplEdge(recs, "FooViewSet", "drf/JSONRenderer") {
		t.Error("JSONRenderer must NOT yield an HTML render path")
	}
	if tplNode(recs, "drf/BrowsableAPIRenderer") != 1 {
		t.Error("expected one drf/BrowsableAPIRenderer convergence node")
	}
	if tplNode(recs, "drf/JSONRenderer") != 0 {
		t.Error("JSONRenderer must not create a render node")
	}
}

// DRF TemplateHTMLRenderer + template_name: BOTH the concrete template (via the
// framework-agnostic template_name detector) AND the renderer render-path node
// are recorded, and a dotted `renderers.BrowsableAPIRenderer` reference matches.
func TestPyTemplate_DRFTemplateHTMLRenderer(t *testing.T) {
	src := `from rest_framework.views import APIView
from rest_framework import renderers

class UserDetail(APIView):
    renderer_classes = [renderers.TemplateHTMLRenderer]
    template_name = "user_detail.html"

    def get(self, request, pk):
        return Response({"x": 1})
`
	recs := extractPy(t, src, "views.py")
	if !tplEdge(recs, "UserDetail", "user_detail.html") {
		t.Error("missing RENDERS(UserDetail -> user_detail.html)")
	}
	if !tplEdge(recs, "UserDetail", "drf/TemplateHTMLRenderer") {
		t.Error("missing RENDERS(UserDetail -> drf/TemplateHTMLRenderer) for dotted ref")
	}
}

// Negative: a DRF view that renders JSON only has NO HTML/browsable render path.
func TestPyTemplate_DRFJSONOnlyNoRender(t *testing.T) {
	src := `from rest_framework.viewsets import ModelViewSet
from rest_framework.renderers import JSONRenderer

class WidgetViewSet(ModelViewSet):
    renderer_classes = [JSONRenderer]
`
	recs := extractPy(t, src, "views.py")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			t.Fatalf("JSON-only DRF view must not create any render node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" {
				t.Fatalf("JSON-only DRF view must not yield a RENDERS edge")
			}
		}
	}
}

// Negative: a plain non-DRF class with no renderer_classes / template_name is
// never claimed as a render path.
func TestPyTemplate_NonDRFClassNoRender(t *testing.T) {
	src := `class PlainService:
    def compute(self):
        return 42
`
	recs := extractPy(t, src, "service.py")
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" {
				t.Fatalf("plain non-view class must not yield a RENDERS edge")
			}
		}
	}
}

// Negative: a dynamically built renderer_classes (not a list/tuple literal)
// yields nothing — precision over recall.
func TestPyTemplate_DRFDynamicRenderersDropped(t *testing.T) {
	src := `class DynView(APIView):
    renderer_classes = get_default_renderers()
`
	recs := extractPy(t, src, "views.py")
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			t.Fatalf("dynamic renderer_classes must not create a render node, got %q", recs[i].Name)
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" {
				t.Fatalf("dynamic renderer_classes must not yield a RENDERS edge")
			}
		}
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
