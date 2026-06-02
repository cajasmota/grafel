package ruby_test

// template_render_test.go — value-asserting tests for the Rails view-layer pass
// (epic #3628). Asserts the SPECIFIC template node + RENDERS edge, not len>0.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func rubyTplEdge(recs []types.EntityRecord, fromName, name string) bool {
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

func rubyTplNode(recs []types.EntityRecord, name string) int {
	want := extractor.TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

func rubyAnyTplNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			return true
		}
	}
	return false
}

// TestRubyTemplate_ExplicitString: render 'users/show' → RENDERS users/show.
func TestRubyTemplate_ExplicitString(t *testing.T) {
	src := `class UsersController < ApplicationController
  def show
    render 'users/show'
  end
end`
	recs := extractRubyRecords(t, src)
	if !rubyTplEdge(recs, "show", "users/show") {
		t.Error("missing RENDERS(show -> users/show)")
	}
	if rubyTplNode(recs, "users/show") != 1 {
		t.Error("expected one SCOPE.Template node users/show")
	}
}

// TestRubyTemplate_TemplateKeyword: render template: 'admin/edit'.
func TestRubyTemplate_TemplateKeyword(t *testing.T) {
	src := `class AdminController < ApplicationController
  def edit
    render template: 'admin/edit'
  end
end`
	recs := extractRubyRecords(t, src)
	if !rubyTplEdge(recs, "edit", "admin/edit") {
		t.Error("missing RENDERS(edit -> admin/edit)")
	}
}

// TestRubyTemplate_PartialKeyword: render partial: 'shared/list'.
func TestRubyTemplate_PartialKeyword(t *testing.T) {
	src := `class ListController < ApplicationController
  def index
    render partial: 'shared/list', locals: {x: 1}
  end
end`
	recs := extractRubyRecords(t, src)
	if !rubyTplEdge(recs, "index", "shared/list") {
		t.Error("missing RENDERS(index -> shared/list)")
	}
}

// TestRubyTemplate_SymbolDropped: render :index is an implicit-convention action
// view (no literal path) — NO template node fabricated.
func TestRubyTemplate_SymbolDropped(t *testing.T) {
	src := `class C < ApplicationController
  def show
    render :index
  end
end`
	recs := extractRubyRecords(t, src)
	if rubyAnyTplNode(recs) {
		t.Error("render :index (symbol) must not fabricate a template node")
	}
}

// TestRubyTemplate_DynamicDropped: render(view) variable arg is dropped.
func TestRubyTemplate_DynamicDropped(t *testing.T) {
	src := `class C < ApplicationController
  def show
    view = params[:v]
    render view
  end
end`
	recs := extractRubyRecords(t, src)
	if rubyAnyTplNode(recs) {
		t.Error("render(variable) must not fabricate a template node")
	}
}

// TestRubyTemplate_Convergence: two actions rendering the same view → one node.
func TestRubyTemplate_Convergence(t *testing.T) {
	src := `class C < ApplicationController
  def a
    render 'shared/page'
  end
  def b
    render 'shared/page'
  end
end`
	recs := extractRubyRecords(t, src)
	if !rubyTplEdge(recs, "a", "shared/page") || !rubyTplEdge(recs, "b", "shared/page") {
		t.Fatal("both actions must RENDERS shared/page")
	}
	if n := rubyTplNode(recs, "shared/page"); n != 1 {
		t.Fatalf("expected ONE converged template node, got %d", n)
	}
}
