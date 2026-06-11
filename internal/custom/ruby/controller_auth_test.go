package ruby

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runControllerAuth(t *testing.T, src string) map[string]types.EntityRecord {
	t.Helper()
	ents, err := (&railsControllerAuthExtractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "app/controllers/users_controller.rb", Language: "ruby", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("controller_auth extract: %v", err)
	}
	out := map[string]types.EntityRecord{}
	for _, e := range ents {
		out[e.Properties["controller_action"]] = e
	}
	return out
}

// TestControllerAuthDeviseAllActions — the canonical #3734 case:
// `before_action :authenticate_user!` → every action auth_required.
func TestControllerAuthDeviseAllActions(t *testing.T) {
	src := `class UsersController < ApplicationController
  before_action :authenticate_user!

  def index
  end

  def show
  end
end
`
	eps := runControllerAuth(t, src)

	for _, h := range []string{"users#index", "users#show"} {
		e, ok := eps[h]
		if !ok {
			t.Fatalf("missing protected op for %s (got %v)", h, keysOf(eps))
		}
		if e.Properties["auth_required"] != "true" {
			t.Errorf("%s: auth_required=%q, want true", h, e.Properties["auth_required"])
		}
		if e.Properties["auth_guard"] != "authenticate_user!" {
			t.Errorf("%s: auth_guard=%q, want authenticate_user!", h, e.Properties["auth_guard"])
		}
		if e.Properties["auth_method"] != "before_action" {
			t.Errorf("%s: auth_method=%q, want before_action", h, e.Properties["auth_method"])
		}
		if e.Properties["auth_confidence"] != "high" {
			t.Errorf("%s: auth_confidence=%q, want high", h, e.Properties["auth_confidence"])
		}
	}
}

// TestControllerAuthOnlyScope — `only: [:edit, :update]` protects ONLY those.
func TestControllerAuthOnlyScope(t *testing.T) {
	src := `class PostsController < ApplicationController
  before_action :authenticate_user!, only: [:edit, :update]

  def index
  end

  def edit
  end

  def update
  end
end
`
	eps := runControllerAuth(t, src)

	if _, ok := eps["posts#edit"]; !ok {
		t.Errorf("posts#edit should be protected by only: scope (got %v)", keysOf(eps))
	}
	if _, ok := eps["posts#update"]; !ok {
		t.Errorf("posts#update should be protected by only: scope")
	}
	// Negative: index is NOT in only: → must not be emitted as protected.
	if _, ok := eps["posts#index"]; ok {
		t.Errorf("posts#index: auth_required, want unprotected (outside only: scope)")
	}
}

// TestControllerAuthExceptScope — `except: [:index]` protects all but index.
func TestControllerAuthExceptScope(t *testing.T) {
	src := `class ArticlesController < ApplicationController
  before_action :authenticate_user!, except: [:index, :show]

  def index
  end

  def show
  end

  def create
  end
end
`
	eps := runControllerAuth(t, src)

	if _, ok := eps["articles#create"]; !ok {
		t.Errorf("articles#create should be protected (not in except:)")
	}
	if _, ok := eps["articles#index"]; ok {
		t.Errorf("articles#index: protected, want unprotected (in except:)")
	}
	if _, ok := eps["articles#show"]; ok {
		t.Errorf("articles#show: protected, want unprotected (in except:)")
	}
}

// TestControllerAuthSkip — `skip_before_action :authenticate_user!, only: [:index]`
// removes index from a fully-protected controller.
func TestControllerAuthSkip(t *testing.T) {
	src := `class DashboardController < ApplicationController
  before_action :authenticate_user!
  skip_before_action :authenticate_user!, only: [:index]

  def index
  end

  def settings
  end
end
`
	eps := runControllerAuth(t, src)

	if _, ok := eps["dashboard#settings"]; !ok {
		t.Errorf("dashboard#settings should remain protected")
	}
	if _, ok := eps["dashboard#index"]; ok {
		t.Errorf("dashboard#index: protected, want unprotected (skip_before_action)")
	}
}

// TestControllerAuthUnprotected — a controller with no auth guard emits nothing
// (negative case: no false-positive protection).
func TestControllerAuthUnprotected(t *testing.T) {
	src := `class PublicController < ApplicationController
  def index
  end

  def about
  end
end
`
	eps := runControllerAuth(t, src)
	if len(eps) != 0 {
		t.Errorf("unprotected controller emitted %d protection ops, want 0 (%v)", len(eps), keysOf(eps))
	}
}

// TestControllerAuthCanCanCan — controller-level load_and_authorize_resource
// protects all actions with method=cancancan.
func TestControllerAuthCanCanCan(t *testing.T) {
	src := `class ProjectsController < ApplicationController
  load_and_authorize_resource

  def index
  end

  def destroy
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["projects#destroy"]
	if !ok {
		t.Fatalf("projects#destroy should be protected by cancancan (got %v)", keysOf(eps))
	}
	if e.Properties["auth_method"] != "cancancan" {
		t.Errorf("projects#destroy: auth_method=%q, want cancancan", e.Properties["auth_method"])
	}
	if e.Properties["auth_guard"] != "load_and_authorize_resource" {
		t.Errorf("projects#destroy: auth_guard=%q, want load_and_authorize_resource", e.Properties["auth_guard"])
	}
}

// TestControllerAuthPunditPerAction — a per-action `authorize @x` protects only
// that action at medium confidence; an action without it stays unprotected.
func TestControllerAuthPunditPerAction(t *testing.T) {
	src := `class ReportsController < ApplicationController
  def index
  end

  def show
    @report = Report.find(params[:id])
    authorize @report
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["reports#show"]
	if !ok {
		t.Fatalf("reports#show should be protected by per-action authorize (got %v)", keysOf(eps))
	}
	if e.Properties["auth_method"] != "pundit" {
		t.Errorf("reports#show: auth_method=%q, want pundit", e.Properties["auth_method"])
	}
	if e.Properties["auth_confidence"] != "medium" {
		t.Errorf("reports#show: auth_confidence=%q, want medium", e.Properties["auth_confidence"])
	}
	if _, ok := eps["reports#index"]; ok {
		t.Errorf("reports#index: protected, want unprotected (no authorize call)")
	}
}

// TestControllerAuthPunditExplicitAction — `authorize @post, :destroy?` must
// capture the specific policy action on auth_permissions (#authz).
func TestControllerAuthPunditExplicitAction(t *testing.T) {
	src := `class PostsController < ApplicationController
  def destroy
    @post = Post.find(params[:id])
    authorize @post, :destroy?
    @post.destroy
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["posts#destroy"]
	if !ok {
		t.Fatalf("posts#destroy should be protected (got %v)", keysOf(eps))
	}
	if e.Properties["auth_permissions"] != "destroy" {
		t.Errorf("posts#destroy: auth_permissions=%q, want destroy (props: %v)", e.Properties["auth_permissions"], e.Properties)
	}
	if e.Properties["auth_method"] != "pundit" {
		t.Errorf("posts#destroy: auth_method=%q, want pundit", e.Properties["auth_method"])
	}
}

// TestControllerAuthCanCanCanBang — `authorize! :destroy, @post` captures the
// CanCanCan ability on auth_permissions.
func TestControllerAuthCanCanCanBang(t *testing.T) {
	src := `class ArticlesController < ApplicationController
  def destroy
    @article = Article.find(params[:id])
    authorize! :destroy, @article
    @article.destroy
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["articles#destroy"]
	if !ok {
		t.Fatalf("articles#destroy should be protected (got %v)", keysOf(eps))
	}
	if e.Properties["auth_permissions"] != "destroy" {
		t.Errorf("articles#destroy: auth_permissions=%q, want destroy (props: %v)", e.Properties["auth_permissions"], e.Properties)
	}
	if e.Properties["auth_method"] != "cancancan" {
		t.Errorf("articles#destroy: auth_method=%q, want cancancan", e.Properties["auth_method"])
	}
}

// Negative: a coarse `authorize @report` with no explicit action symbol must
// NOT fabricate a permission (it is protected but the action is implicit).
func TestControllerAuthPunditNoExplicitActionNoPermission(t *testing.T) {
	src := `class ReportsController < ApplicationController
  def show
    @report = Report.find(params[:id])
    authorize @report
  end
end
`
	eps := runControllerAuth(t, src)
	e := eps["reports#show"]
	if v := e.Properties["auth_permissions"]; v != "" {
		t.Errorf("reports#show: expected no auth_permissions for implicit authorize, got %q", v)
	}
}

// TestControllerAuthNamespaced — Admin::UsersController → admin/users handler.
func TestControllerAuthNamespaced(t *testing.T) {
	src := `class Admin::UsersController < ApplicationController
  before_action :authenticate_admin!

  def index
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["admin/users#index"]
	if !ok {
		t.Fatalf("admin/users#index expected (got %v)", keysOf(eps))
	}
	if e.Properties["auth_guard"] != "authenticate_admin!" {
		t.Errorf("admin/users#index: auth_guard=%q, want authenticate_admin!", e.Properties["auth_guard"])
	}
}

// TestControllerAuthPrivateExcluded — private helper methods are not actions.
func TestControllerAuthPrivateExcluded(t *testing.T) {
	src := `class OrdersController < ApplicationController
  before_action :authenticate_user!

  def index
  end

  private

  def set_order
  end
end
`
	eps := runControllerAuth(t, src)
	if _, ok := eps["orders#index"]; !ok {
		t.Errorf("orders#index should be protected")
	}
	if _, ok := eps["orders#set_order"]; ok {
		t.Errorf("orders#set_order: emitted, want excluded (private helper, not an action)")
	}
}

func keysOf(m map[string]types.EntityRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestControllerAuthPunditLiteral_4751 — a per-action `authorize @post, :update?`
// stamps pundit_policy/pundit_action so the rails resolver decodes the exact
// action grant (#4751).
func TestControllerAuthPunditLiteral_4751(t *testing.T) {
	src := `class PostsController < ApplicationController
  def update
    @post = Post.find(params[:id])
    authorize @post, :update?
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["posts#update"]
	if !ok {
		t.Fatalf("no posts#update endpoint; got %+v", eps)
	}
	if e.Properties["pundit_policy"] != "Post" {
		t.Errorf("pundit_policy=%q, want Post", e.Properties["pundit_policy"])
	}
	if e.Properties["pundit_action"] != "update" {
		t.Errorf("pundit_action=%q, want update", e.Properties["pundit_action"])
	}
	if e.Properties["controller_source"] == "" {
		t.Errorf("controller_source not stamped")
	}
}

// TestControllerAuthCanCanLiteral_4751 — controller-level
// load_and_authorize_resource stamps cancancan_ability = the action name (#4751).
func TestControllerAuthCanCanLiteral_4751(t *testing.T) {
	src := `class WidgetsController < ApplicationController
  load_and_authorize_resource

  def destroy
  end
end
`
	eps := runControllerAuth(t, src)
	e, ok := eps["widgets#destroy"]
	if !ok {
		t.Fatalf("no widgets#destroy endpoint; got %+v", eps)
	}
	if e.Properties["cancancan_ability"] != "destroy" {
		t.Errorf("cancancan_ability=%q, want destroy", e.Properties["cancancan_ability"])
	}
}
