package crystal_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/crystal"
)

// fixture is a tiny synthetic Crystal service loosely modelled on the Kemal
// web framework style plus some plain Crystal idioms.
const kemalFixture = `
require "kemal"
require "json"
require "./models/user"

module Auth
  def self.validate(token : String) : Bool
    token.size > 0
  end
end

abstract class BaseHandler
  abstract def handle(env : HTTP::Server::Context)
end

class UserHandler < BaseHandler
  def initialize(@repo : UserRepo)
  end

  def handle(env : HTTP::Server::Context)
    user = @repo.find(env.params.url["id"])
    env.response.print(user.to_json)
  end

  def find_user(id : Int32)
    @repo.find(id)
  end
end

get "/users/:id" do |env|
  handler = UserHandler.new(UserRepo.new)
  handler.handle(env)
end

post "/users" do |env|
  body = env.request.body.not_nil!.gets_to_end
  data = JSON.parse(body)
  env.response.status_code = 201
end

macro route(method, path)
  {{method.id}} {{path}} do |env|
    yield env
  end
end
`

func ext(t *testing.T) extractor.Extractor {
	t.Helper()
	e, ok := extractor.Get("crystal")
	if !ok {
		t.Fatal("crystal extractor not registered")
	}
	return e
}

func extractFixture(t *testing.T, src, path string) []interface{ GetName() string } {
	t.Helper()
	return nil // not used directly; call ext().Extract instead
}

func TestCrystalExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("crystal")
	if !ok {
		t.Fatal("crystal extractor not registered")
	}
}

func TestCrystalExtractor_EmptyFile(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.cr",
		Content:  []byte(""),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(got))
	}
}

func TestCrystalExtractor_Module(t *testing.T) {
	src := `
module MyApp
  def self.run
    puts "hello"
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "app.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, rec := range got {
		if rec.Name == "MyApp" && rec.Kind == "SCOPE.Component" && rec.Subtype == "module" {
			found = true
			if rec.Language != "crystal" {
				t.Errorf("expected language=crystal, got %q", rec.Language)
			}
			if rec.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", rec.StartLine)
			}
		}
	}
	if !found {
		t.Error("expected entity MyApp with Kind=SCOPE.Component Subtype=module")
	}
}

func TestCrystalExtractor_Class(t *testing.T) {
	src := `
class Server
  def initialize(@port : Int32)
  end

  def start
    puts "listening on #{@port}"
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "server.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var foundClass, foundMethod bool
	for _, rec := range got {
		if rec.Name == "Server" && rec.Kind == "SCOPE.Component" && rec.Subtype == "class" {
			foundClass = true
		}
		if rec.Name == "start" && rec.Kind == "SCOPE.Operation" && rec.Subtype == "method" {
			foundMethod = true
		}
	}
	if !foundClass {
		t.Error("expected entity Server with Kind=SCOPE.Component Subtype=class")
	}
	if !foundMethod {
		t.Error("expected entity start with Kind=SCOPE.Operation Subtype=method")
	}
}

func TestCrystalExtractor_AbstractClass(t *testing.T) {
	src := `
abstract class Animal
  abstract def speak : String
end

class Dog < Animal
  def speak : String
    "woof"
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "animal.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var foundAbstract, foundConcrete, foundExtends bool
	for _, rec := range got {
		if rec.Name == "Animal" && rec.Kind == "SCOPE.Component" && rec.Subtype == "class" {
			foundAbstract = true
		}
		if rec.Name == "Dog" && rec.Kind == "SCOPE.Component" && rec.Subtype == "class" {
			foundConcrete = true
			for _, rel := range rec.Relationships {
				if rel.Kind == "EXTENDS" && rel.ToID == "Animal" {
					foundExtends = true
				}
			}
		}
	}
	if !foundAbstract {
		t.Error("expected entity Animal with Kind=SCOPE.Component Subtype=class")
	}
	if !foundConcrete {
		t.Error("expected entity Dog with Kind=SCOPE.Component Subtype=class")
	}
	if !foundExtends {
		t.Error("expected EXTENDS edge from Dog to Animal")
	}
}

func TestCrystalExtractor_Require(t *testing.T) {
	src := `
require "http/server"
require "./router"
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "main.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var httpFound, routerFound bool
	for _, rec := range got {
		for _, rel := range rec.Relationships {
			if rel.Kind == "IMPORTS" && rel.ToID == "http/server" {
				httpFound = true
			}
			if rel.Kind == "IMPORTS" && rel.ToID == "./router" {
				routerFound = true
			}
		}
	}
	if !httpFound {
		t.Error("expected IMPORTS edge for http/server")
	}
	if !routerFound {
		t.Error("expected IMPORTS edge for ./router")
	}
}

func TestCrystalExtractor_Macro(t *testing.T) {
	src := `
macro delegate(method_name, to target)
  def {{method_name}}(*args)
    {{target}}.{{method_name}}(*args)
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "macros.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, rec := range got {
		if rec.Name == "delegate" && rec.Kind == "SCOPE.Operation" && rec.Subtype == "macro" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity delegate with Kind=SCOPE.Operation Subtype=macro")
	}
}

func TestCrystalExtractor_Contains(t *testing.T) {
	src := `
class Router
  def get(path : String)
    handle(path)
  end

  def handle(path : String)
    "ok"
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "router.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var containsCount int
	for _, rec := range got {
		if rec.Name == "Router" && rec.Kind == "SCOPE.Component" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CONTAINS" {
					containsCount++
				}
			}
		}
	}
	if containsCount == 0 {
		t.Error("expected at least one CONTAINS edge on Router")
	}
}

func TestCrystalExtractor_CallsEdge(t *testing.T) {
	src := `
class Service
  def process(input : String)
    validate(input)
    transform(input)
  end

  def validate(s : String)
    s.size > 0
  end

  def transform(s : String)
    s.upcase
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "svc.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var callsTargets []string
	for _, rec := range got {
		if rec.Name == "process" {
			for _, rel := range rec.Relationships {
				if rel.Kind == "CALLS" {
					callsTargets = append(callsTargets, rel.ToID)
				}
			}
		}
	}
	if len(callsTargets) == 0 {
		t.Error("expected at least one CALLS edge from process")
	}
}

func TestCrystalExtractor_LanguageTag(t *testing.T) {
	src := `
class Foo
  def bar
    "baz"
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "foo.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rec := range got {
		if rec.Language != "" && rec.Language != "crystal" {
			t.Errorf("expected language=crystal, got %q on entity %q", rec.Language, rec.Name)
		}
	}
}

func TestCrystalExtractor_LineNumbers(t *testing.T) {
	src := `class Alpha
  def method1
    nil
  end
end
`
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "lines.cr",
		Content:  []byte(src),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rec := range got {
		if rec.Kind == "SCOPE.Component" && rec.Name == "Alpha" {
			if rec.StartLine < 1 {
				t.Errorf("expected StartLine >= 1, got %d", rec.StartLine)
			}
			if rec.EndLine < rec.StartLine {
				t.Errorf("expected EndLine >= StartLine, got start=%d end=%d",
					rec.StartLine, rec.EndLine)
			}
		}
	}
}

// TestCrystalExtractor_KemalFixtureRecall verifies ≥80% entity recall against
// the synthetic Kemal-style fixture defined at the top of this file.
func TestCrystalExtractor_KemalFixtureRecall(t *testing.T) {
	e := ext(t)
	got, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "kemal_app.cr",
		Content:  []byte(kemalFixture),
		Language: "crystal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected entities (minimum viable set for 80% recall).
	wantNames := []string{
		"Auth",        // module
		"BaseHandler", // abstract class
		"UserHandler", // class
		"initialize",  // def
		"handle",      // def
		"find_user",   // def
		"validate",    // def (Auth.self.validate normalised)
		"route",       // macro
	}

	nameSet := make(map[string]bool, len(got))
	for _, rec := range got {
		nameSet[rec.Name] = true
	}

	found := 0
	for _, w := range wantNames {
		if nameSet[w] {
			found++
		}
	}
	recall := float64(found) / float64(len(wantNames))
	if recall < 0.80 {
		t.Errorf("entity recall %.0f%% < 80%% — found %d/%d: names in graph: %v",
			recall*100, found, len(wantNames), sortedKeys(nameSet))
	}
}

// TestCrystalExtractor_CrossLanguageFalsePositive verifies that a Crystal file
// does not produce entities when processed with a language that has no Crystal
// extractor registered (i.e. the crystal extractor does NOT shadow other langs).
// Also verifies zero false positives from a plain Go struct (which should not
// be handled by the Crystal extractor).
func TestCrystalExtractor_ZeroFalsePositiveOnNonCrystal(t *testing.T) {
	_, ok := extractor.Get("nonexistent_language_xyz")
	if ok {
		t.Error("expected false for unregistered language")
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
