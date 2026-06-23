package ruby_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/ruby"
	"github.com/cajasmota/grafel/internal/types"
)

// extractRubyFile is a small helper to run the ruby extractor on an
// in-memory source string and return the resulting entities.
func extractRubyFile(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return got
}

// findEntity returns the first SCOPE.Component with the given name, or
// a zero record (the caller must still assert on fields to surface the
// failure). Using a pointer lets the caller mutate during assertions.
func findEntity(t *testing.T, got []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	for _, e := range got {
		if e.Name == name && e.Kind == "SCOPE.Component" {
			return e
		}
	}
	t.Fatalf("expected SCOPE.Component named %q in %d entities", name, len(got))
	return types.EntityRecord{}
}

func TestRails_ControllerBySuperclass(t *testing.T) {
	src := `
class PostsController < ApplicationController
  def index
    @posts = Post.all
  end
end
`
	got := extractRubyFile(t, "lib/posts_controller.rb", src)
	e := findEntity(t, got, "PostsController")

	if e.Properties["framework"] != "rails" {
		t.Errorf("framework=%q, want rails", e.Properties["framework"])
	}
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller", e.Properties["kind"])
	}
	if e.Properties["service_kind"] != "rails_service" {
		t.Errorf("service_kind=%q, want rails_service", e.Properties["service_kind"])
	}
	if e.Properties["superclass"] != "ApplicationController" {
		t.Errorf("superclass=%q, want ApplicationController", e.Properties["superclass"])
	}
	if !containsTag(e.Tags, "framework:rails") {
		t.Errorf("tags=%v, missing framework:rails", e.Tags)
	}
	if !containsTag(e.Tags, "rails:controller") {
		t.Errorf("tags=%v, missing rails:controller", e.Tags)
	}
}

func TestRails_ControllerByActionControllerBase(t *testing.T) {
	src := `
class HelloController < ActionController::Base
  def index
    render plain: "hi"
  end
end
`
	got := extractRubyFile(t, "hello.rb", src)
	e := findEntity(t, got, "HelloController")
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller", e.Properties["kind"])
	}
	if e.Properties["superclass"] != "ActionController::Base" {
		t.Errorf("superclass=%q, want ActionController::Base", e.Properties["superclass"])
	}
}

func TestRails_ControllerByActionControllerMetal(t *testing.T) {
	src := `
class MetalController < ActionController::Metal
end
`
	got := extractRubyFile(t, "metal.rb", src)
	e := findEntity(t, got, "MetalController")
	if e.Properties["framework"] != "rails" {
		t.Errorf("framework=%q, want rails", e.Properties["framework"])
	}
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller", e.Properties["kind"])
	}
}

func TestRails_ControllerByPath(t *testing.T) {
	src := `
class Api::UsersController
  def index; end
end
`
	got := extractRubyFile(t, "app/controllers/api/users_controller.rb", src)
	e := findEntity(t, got, "Api::UsersController")
	if e.Properties["kind"] != "controller" {
		t.Errorf("kind=%q, want controller (path-based)", e.Properties["kind"])
	}
	if e.Properties["framework"] != "rails" {
		t.Errorf("framework=%q, want rails", e.Properties["framework"])
	}
}

func TestRails_ModelByApplicationRecord(t *testing.T) {
	src := `
class Article < ApplicationRecord
  has_many :comments
end
`
	got := extractRubyFile(t, "app/models/article.rb", src)
	e := findEntity(t, got, "Article")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model", e.Properties["kind"])
	}
	if e.Properties["orm"] != "activerecord" {
		t.Errorf("orm=%q, want activerecord", e.Properties["orm"])
	}
	if e.Properties["superclass"] != "ApplicationRecord" {
		t.Errorf("superclass=%q, want ApplicationRecord", e.Properties["superclass"])
	}
}

func TestRails_ModelByActiveRecordBase(t *testing.T) {
	src := `
class LegacyUser < ActiveRecord::Base
end
`
	got := extractRubyFile(t, "legacy.rb", src)
	e := findEntity(t, got, "LegacyUser")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model", e.Properties["kind"])
	}
	if e.Properties["orm"] != "activerecord" {
		t.Errorf("orm=%q, want activerecord", e.Properties["orm"])
	}
}

func TestRails_ModelByPath(t *testing.T) {
	src := `
class Concern
end
`
	got := extractRubyFile(t, "app/models/concerns/concern.rb", src)
	e := findEntity(t, got, "Concern")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model (path-based)", e.Properties["kind"])
	}
	if e.Properties["orm"] != "activerecord" {
		t.Errorf("orm=%q, want activerecord", e.Properties["orm"])
	}
}

func TestRails_MigrationByPath(t *testing.T) {
	src := `
class CreateUsers < ActiveRecord::Migration[7.0]
  def change
    create_table :users
  end
end
`
	got := extractRubyFile(t, "db/migrate/20240101_create_users.rb", src)
	e := findEntity(t, got, "CreateUsers")
	if e.Properties["kind"] != "migration" {
		t.Errorf("kind=%q, want migration", e.Properties["kind"])
	}
	if e.Properties["framework"] != "rails" {
		t.Errorf("framework=%q, want rails", e.Properties["framework"])
	}
	// Migrations must not leak an ORM tag — they are schema, not a model.
	if e.Properties["orm"] != "" {
		t.Errorf("orm=%q, want empty for migration", e.Properties["orm"])
	}
}

func TestRails_RoutesFile(t *testing.T) {
	src := `
Rails.application.routes.draw do
  resources :posts
end
`
	// config/routes.rb does not typically define a class, but the rule
	// should still apply to any module/class nested inside. Assert via
	// a module to exercise the kind=route path.
	rsrc := `
module AppRoutes
  def self.draw; end
end
`
	got := extractRubyFile(t, "config/routes.rb", rsrc)
	e := findEntity(t, got, "AppRoutes")
	if e.Properties["kind"] != "route" {
		t.Errorf("kind=%q, want route", e.Properties["kind"])
	}
	if e.Properties["framework"] != "rails" {
		t.Errorf("framework=%q, want rails", e.Properties["framework"])
	}
	_ = src
}

func TestRails_NonRailsFileUntouched(t *testing.T) {
	src := `
class Logger
  def log(msg); end
end
`
	got := extractRubyFile(t, "lib/logger.rb", src)
	e := findEntity(t, got, "Logger")
	if e.Properties["framework"] != "" {
		t.Errorf("framework=%q, want empty for plain Ruby class", e.Properties["framework"])
	}
	if e.Properties["kind"] != "" {
		t.Errorf("kind=%q, want empty for plain Ruby class", e.Properties["kind"])
	}
	for _, tag := range e.Tags {
		if tag == "framework:rails" {
			t.Errorf("plain Ruby class should not carry framework:rails tag, got %v", e.Tags)
		}
	}
}

func TestRails_ModuleInModelsPath(t *testing.T) {
	src := `
module SharedValidations
end
`
	got := extractRubyFile(t, "app/models/shared_validations.rb", src)
	e := findEntity(t, got, "SharedValidations")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model (module-in-models-path)", e.Properties["kind"])
	}
}

func TestRails_WindowsPathSeparator(t *testing.T) {
	src := `
class Article < ApplicationRecord
end
`
	got := extractRubyFile(t, `app\models\article.rb`, src)
	e := findEntity(t, got, "Article")
	if e.Properties["kind"] != "model" {
		t.Errorf("kind=%q, want model on windows-style path", e.Properties["kind"])
	}
}

// containsTag is a small helper for tag-presence assertions.
func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
