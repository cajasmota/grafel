package ruby_test

// cuba_routing_test.go — tests for the ruby_cuba_routing extractor.
// Part of #3282.

import (
	"testing"
)

func cubaExtract(t *testing.T, src string) []entitySummary {
	t.Helper()
	return extract(t, "ruby_cuba_routing", fi("app.rb", "ruby", src))
}

// ---------------------------------------------------------------------------
// Endpoint synthesis: on "path" string segments
// ---------------------------------------------------------------------------

func TestCuba_OnStringPath(t *testing.T) {
	src := `
Cuba.define do
  on "users" do
    on get do
      res.write users.all.to_json
    end
    on post do
      res.write create_user(req.params).to_json
    end
  end
end
`
	ents := cubaExtract(t, src)
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:/users") {
		t.Error("expected cuba_on:/users endpoint entity")
	}
}

func TestCuba_NestedPaths(t *testing.T) {
	src := `
Cuba.define do
  on "api" do
    on "v1" do
      on "health" do
        on get do
          res.write "ok"
        end
      end
    end
  end
end
`
	ents := cubaExtract(t, src)
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:/api") {
		t.Error("expected cuba_on:/api endpoint entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:/v1") {
		t.Error("expected cuba_on:/v1 endpoint entity")
	}
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:/health") {
		t.Error("expected cuba_on:/health endpoint entity")
	}
}

// ---------------------------------------------------------------------------
// Endpoint synthesis: on :param parametric segments
// ---------------------------------------------------------------------------

func TestCuba_OnParam(t *testing.T) {
	src := `
Cuba.define do
  on "users" do
    on :id do
      on get do
        res.write user.to_json
      end
    end
  end
end
`
	ents := cubaExtract(t, src)
	if !containsEntity(ents, "SCOPE.Operation", "cuba_on:/:id") {
		t.Error("expected cuba_on:/:id parametric endpoint entity")
	}
}

// ---------------------------------------------------------------------------
// Handler attribution: on <verb> blocks
// ---------------------------------------------------------------------------

func TestCuba_HandlerVerbs(t *testing.T) {
	src := `
Cuba.define do
  on "posts" do
    on get do
      res.write posts.all.to_json
    end
    on post do
      res.write create_post(req.params).to_json
    end
    on delete do
      res.write delete_post(req.params[:id]).to_json
    end
  end
end
`
	ents := cubaExtract(t, src)
	foundGet := false
	foundPost := false
	foundDelete := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "handler" {
			switch e.Name {
			case "cuba_handler:GET":
				foundGet = true
			case "cuba_handler:POST":
				foundPost = true
			case "cuba_handler:DELETE":
				foundDelete = true
			}
		}
	}
	if !foundGet {
		t.Error("expected cuba_handler:GET entity")
	}
	if !foundPost {
		t.Error("expected cuba_handler:POST entity")
	}
	if !foundDelete {
		t.Error("expected cuba_handler:DELETE entity")
	}
}

// ---------------------------------------------------------------------------
// Sub-app mounting: run SubApp
// ---------------------------------------------------------------------------

func TestCuba_MountSubApp(t *testing.T) {
	src := `
Cuba.define do
  on "admin" do
    run AdminApp
  end
  on "api" do
    run ApiApp
  end
end
`
	ents := cubaExtract(t, src)
	if !containsEntity(ents, "SCOPE.Component", "cuba_mount:AdminApp") {
		t.Error("expected cuba_mount:AdminApp component entity")
	}
	if !containsEntity(ents, "SCOPE.Component", "cuba_mount:ApiApp") {
		t.Error("expected cuba_mount:ApiApp component entity")
	}
}

// ---------------------------------------------------------------------------
// Non-Cuba files → no entities
// ---------------------------------------------------------------------------

func TestCuba_SkipsNonCubaFiles(t *testing.T) {
	src := `
class MyApp < Sinatra::Base
  get '/health' do
    "ok"
  end
end
`
	ents := cubaExtract(t, src)
	for _, e := range ents {
		if e.Name[:7] == "cuba_on" || e.Name[:12] == "cuba_handler" {
			t.Errorf("unexpected Cuba entity in non-Cuba file: %+v", e)
		}
	}
}

func TestCuba_EmptyFile(t *testing.T) {
	ents := cubaExtract(t, "")
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty file, got %d", len(ents))
	}
}
