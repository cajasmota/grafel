package crystal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/crystal"
)

// ── enum ────────────────────────────────────────────────────────────────────

func TestCrystalEnum_HappyPath(t *testing.T) {
	src := `
enum Color
  Red
  Green = 2
  Blue = 3
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "color.cr", Content: []byte(src), Language: "crystal",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var found bool
	for _, r := range got {
		if r.Name == "Color" && r.Kind == "SCOPE.Enum" {
			found = true
			members := r.Properties["members"]
			if !strings.Contains(members, "Red") || !strings.Contains(members, "Green") || !strings.Contains(members, "Blue") {
				t.Errorf("expected members Red,Green,Blue got %q", members)
			}
			if r.Properties["member_count"] != "3" {
				t.Errorf("expected member_count=3 got %q", r.Properties["member_count"])
			}
			if !strings.Contains(r.Properties["values"], "Green=2") {
				t.Errorf("expected Green=2 in values, got %q", r.Properties["values"])
			}
			if r.Properties["kind_hint"] != "crystal_enum" {
				t.Errorf("expected kind_hint=crystal_enum got %q", r.Properties["kind_hint"])
			}
		}
	}
	if !found {
		t.Error("expected SCOPE.Enum entity Color")
	}
}

func TestCrystalEnum_NoMatchNoOp(t *testing.T) {
	src := `
class Plain
  def go
    "no enum here"
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "plain.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Kind == "SCOPE.Enum" {
			t.Errorf("expected no enum entity, got %q", r.Name)
		}
	}
}

func TestCrystalEnum_WrongLanguageNoOp(t *testing.T) {
	// A Go-style enum-ish const block must NOT be parsed as a Crystal enum when
	// the crystal extractor is fed it: there is no `enum X` header, so no node.
	src := `
const (
	Red   = iota
	Green
)
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "consts.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Kind == "SCOPE.Enum" {
			t.Errorf("expected no enum from non-crystal-enum source, got %q", r.Name)
		}
	}
}

// ── alias ───────────────────────────────────────────────────────────────────

func TestCrystalAlias_HappyPath(t *testing.T) {
	src := `
alias UserId = Int64
alias StringMap = Hash(String, String)
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "aliases.cr", Content: []byte(src), Language: "crystal",
	})
	var idFound, mapRef bool
	for _, r := range got {
		if r.Name == "UserId" && r.Kind == "SCOPE.Component" && r.Subtype == "alias" {
			idFound = true
			if r.Properties["alias_target"] != "Int64" {
				t.Errorf("expected alias_target=Int64 got %q", r.Properties["alias_target"])
			}
		}
		if r.Name == "StringMap" {
			for _, rel := range r.Relationships {
				if rel.Kind == "REFERENCES" && rel.ToID == "Hash" {
					mapRef = true
				}
			}
		}
	}
	if !idFound {
		t.Error("expected alias entity UserId")
	}
	if !mapRef {
		t.Error("expected REFERENCES edge from StringMap to Hash")
	}
}

func TestCrystalAlias_NoMatchNoOp(t *testing.T) {
	src := `
class C
  def m
    x = 1
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "c.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Subtype == "alias" {
			t.Errorf("expected no alias entity, got %q", r.Name)
		}
	}
}

func TestCrystalAlias_WrongLanguageNoOp(t *testing.T) {
	// A Ruby `alias foo bar` (no `=`) must not be parsed as a Crystal type alias.
	src := `
class C
  alias old_name new_name
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "ruby_alias.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Subtype == "alias" {
			t.Errorf("expected no Crystal type-alias from `alias a b` form, got %q", r.Name)
		}
	}
}

// ── Type.method receiver resolution ─────────────────────────────────────────

func TestCrystalReceiver_HappyPath(t *testing.T) {
	src := `
class Repo
  def self.find(id)
    nil
  end
end

class Service
  def run
    Repo.find(1)
    helper
  end

  def helper
    "ok"
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "svc.cr", Content: []byte(src), Language: "crystal",
	})
	var receiverStamped, qualifiedCall bool
	for _, r := range got {
		if r.Name == "run" {
			if r.Properties["receiver_type"] == "Service" {
				receiverStamped = true
			}
			for _, rel := range r.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "Repo.find" {
					qualifiedCall = true
				}
			}
		}
	}
	if !receiverStamped {
		t.Error("expected receiver_type=Service stamped on method run")
	}
	if !qualifiedCall {
		t.Error("expected class-qualified CALLS target Repo.find")
	}
}

func TestCrystalReceiver_NoMatchNoOp(t *testing.T) {
	// A top-level def has no owning type → no receiver_type stamp.
	src := `
def standalone
  helper
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "top.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Name == "standalone" {
			if _, ok := r.Properties["receiver_type"]; ok {
				t.Errorf("expected no receiver_type on top-level def, got %q", r.Properties["receiver_type"])
			}
		}
	}
}

// ── macro-generated methods ─────────────────────────────────────────────────

func TestCrystalMacroGen_HappyPath(t *testing.T) {
	src := `
macro define_getters(*names)
  {% for name in names %}
    def {{name.id}}
      @{{name.id}}
    end
  {% end %}
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "macros.cr", Content: []byte(src), Language: "crystal",
	})
	var found bool
	for _, r := range got {
		if r.Name == "define_getters" && r.Subtype == "macro" {
			found = true
			if r.Properties["macro_generated"] != "true" {
				t.Errorf("expected macro_generated=true got %q", r.Properties["macro_generated"])
			}
			if r.Properties["generated_method_count"] == "" || r.Properties["generated_method_count"] == "0" {
				t.Errorf("expected generated_method_count>0 got %q", r.Properties["generated_method_count"])
			}
			if r.Properties["generated_via"] != "macro_for_iteration" {
				t.Errorf("expected generated_via=macro_for_iteration got %q", r.Properties["generated_via"])
			}
		}
	}
	if !found {
		t.Error("expected macro entity define_getters")
	}
}

func TestCrystalMacroGen_NoMatchNoOp(t *testing.T) {
	// A macro that generates no methods carries no macro_generated marker.
	src := `
macro log_it(msg)
  puts {{msg}}
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "log.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Name == "log_it" {
			if _, ok := r.Properties["macro_generated"]; ok {
				t.Error("expected no macro_generated marker on a non-gen macro")
			}
		}
	}
}

// ── Spectator / spec test linkage ───────────────────────────────────────────

func TestCrystalSpecSuite_HappyPath(t *testing.T) {
	src := `
require "./spec_helper"

describe User do
  it "has a name" do
    user = User.new
    user.name.should eq("x")
  end

  context "validation" do
    it "rejects blanks" do
      User.validate("").should be_false
    end
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "spec/models/user_spec.cr", Content: []byte(src), Language: "crystal",
	})
	var suiteFound, testsEdge bool
	for _, r := range got {
		if r.Subtype == "test_suite" {
			suiteFound = true
			if r.Properties["framework"] != "spectator" {
				t.Errorf("expected framework=spectator got %q", r.Properties["framework"])
			}
			if r.Properties["example_count"] != "2" {
				t.Errorf("expected example_count=2 got %q", r.Properties["example_count"])
			}
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" && rel.ToID == "Class:User" {
					testsEdge = true
				}
			}
		}
	}
	if !suiteFound {
		t.Error("expected a test_suite entity for the spec file")
	}
	if !testsEdge {
		t.Error("expected TESTS edge to Class:User")
	}
}

func TestCrystalSpecSuite_StemSubjectFallback(t *testing.T) {
	// No `describe Const` — subject resolves from the spec-file stem.
	src := `
describe "GET /orders" do
  it "returns 200" do
    get "/orders"
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "spec/order_service_spec.cr", Content: []byte(src), Language: "crystal",
	})
	var testsEdge bool
	for _, r := range got {
		if r.Subtype == "test_suite" {
			for _, rel := range r.Relationships {
				if rel.Kind == "TESTS" && rel.ToID == "Class:OrderService" {
					testsEdge = true
				}
			}
		}
	}
	if !testsEdge {
		t.Error("expected stem-derived TESTS edge to Class:OrderService")
	}
}

func TestCrystalSpecSuite_NonSpecFileNoOp(t *testing.T) {
	// describe/it in a NON-spec file emits no suite (wrong-context no-op).
	src := `
describe User do
  it "x" do
  end
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "src/user.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Subtype == "test_suite" {
			t.Errorf("expected no test_suite for non-spec file, got %q", r.Name)
		}
	}
}

func TestCrystalSpecSuite_NoExamplesNoOp(t *testing.T) {
	// A spec file with a describe but no `it`/`pending` examples exercises
	// nothing → no suite.
	src := `
describe User do
end
`
	e := ext(t)
	got, _ := e.Extract(context.Background(), extractor.FileInput{
		Path: "spec/user_spec.cr", Content: []byte(src), Language: "crystal",
	})
	for _, r := range got {
		if r.Subtype == "test_suite" {
			t.Errorf("expected no test_suite for example-less spec, got %q", r.Name)
		}
	}
}
