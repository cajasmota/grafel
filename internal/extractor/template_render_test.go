package extractor

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func tplEntityID(recs []types.EntityRecord, name string) string {
	want := TemplateName(name)
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			return recs[i].ID
		}
	}
	return ""
}

func tplCount(recs []types.EntityRecord, name string) int {
	want := TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

// TestTemplateRender_Convergence is the value-asserting test: two distinct
// handlers rendering the SAME template must produce two RENDERS edges that both
// point at ONE template node (deduped). That convergence IS the capability.
func TestTemplateRender_Convergence(t *testing.T) {
	recs := newRecords("views.py", "python", "list_view", "search_view")
	n := EmitTemplateEdges(&recs, "python", []TemplateEdge{
		{Name: "users/list.html", FromName: "list_view", Pattern: "render_template"},
		{Name: "users/list.html", FromName: "search_view", Pattern: "render_template"},
	})
	if n != 2 {
		t.Fatalf("expected 2 RENDERS edges, got %d", n)
	}
	if got := tplCount(recs, "users/list.html"); got != 1 {
		t.Fatalf("expected ONE converged template node, got %d", got)
	}
	id := tplEntityID(recs, "users/list.html")
	if id == "" {
		t.Fatal("template node users/list.html not found")
	}
	toID := TemplateTargetID("users/list.html")
	if !edgeTo(recs, "list_view", "RENDERS", toID) {
		t.Error("missing RENDERS(list_view -> users/list.html)")
	}
	if !edgeTo(recs, "search_view", "RENDERS", toID) {
		t.Error("missing RENDERS(search_view -> users/list.html)")
	}
}

func TestTemplateRender_Dedup(t *testing.T) {
	recs := newRecords("v.py", "python", "h")
	n := EmitTemplateEdges(&recs, "python", []TemplateEdge{
		{Name: "home.html", FromName: "h"},
		{Name: "home.html", FromName: "h"},
	})
	if n != 1 {
		t.Fatalf("expected 1 deduped edge, got %d", n)
	}
}

func TestNormalizeTemplateName(t *testing.T) {
	cases := map[string]string{
		"users/list.html":    "users/list.html",
		"users.list":         "users/list",   // Laravel dot-notation → slash
		"users/show":         "users/show",   // no extension, has slash → verbatim
		"./partials/nav":     "partials/nav", // leading ./ stripped
		"dashboard":          "dashboard",
		"x.html":             "x.html", // has ext → dots preserved
		"name_var + '.html'": "",       // concatenation → dynamic, drop
		"`p/${id}`":          "",       // template literal interpolation → drop
		"":                   "",
		"a b":                "", // space → drop
		"../etc/passwd":      "", // traversal → drop
	}
	for in, want := range cases {
		if got := NormalizeTemplateName(in); got != want {
			t.Errorf("NormalizeTemplateName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTemplateRender_DynamicDropped: a dynamic template name yields no edge and
// no node — honest-partial, precision over recall.
func TestTemplateRender_DynamicDropped(t *testing.T) {
	recs := newRecords("v.py", "python", "h")
	n := EmitTemplateEdges(&recs, "python", []TemplateEdge{
		{Name: "tpl_var + suffix", FromName: "h"},
	})
	if n != 0 {
		t.Fatalf("expected 0 edges for dynamic name, got %d", n)
	}
	if tplEntityID(recs, "tpl_var + suffix") != "" {
		t.Error("dynamic template must not create a node")
	}
}

func TestTemplateEntity_Synthetic(t *testing.T) {
	e := TemplateEntity("dashboard", "python")
	if e.SourceFile != TemplateSourceFile {
		t.Errorf("SourceFile = %q, want synthetic %q", e.SourceFile, TemplateSourceFile)
	}
	if e.QualifiedName != TemplateTargetID("dashboard") {
		t.Errorf("QualifiedName = %q, want %q", e.QualifiedName, TemplateTargetID("dashboard"))
	}
	if e.Kind != string(types.EntityKindTemplate) {
		t.Errorf("Kind = %q, want %q", e.Kind, types.EntityKindTemplate)
	}
}
