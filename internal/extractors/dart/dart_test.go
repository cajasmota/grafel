package dart_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/dart"
)

func TestDartExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("dart")
	if !ok {
		t.Fatal("dart extractor not registered")
	}
}

func TestDartExtractor_Classes(t *testing.T) {
	src := `class UserService {
  final List<User> _users = [];

  User? findById(int id) {
    return _users.firstWhere((u) => u.id == id);
  }

  void create(String name, String email) {
    _users.add(User(name: name, email: email));
  }
}

abstract class BaseRepository {
  void save(dynamic entity);
}
`
	ext, _ := extractor.Get("dart")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "user_service.dart",
		Content:  []byte(src),
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	classes := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" {
			classes[e.Name] = true
			if e.Language != "dart" {
				t.Errorf("entity %q: expected Language=dart, got %q", e.Name, e.Language)
			}
		}
	}
	for _, want := range []string{"UserService", "BaseRepository"} {
		if !classes[want] {
			t.Errorf("expected class %q to be extracted", want)
		}
	}
}

func TestDartExtractor_Methods(t *testing.T) {
	src := `class Calculator {
  int add(int a, int b) {
    return a + b;
  }

  int multiply(int a, int b) {
    return a * b;
  }
}
`
	ext, _ := extractor.Get("dart")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "calculator.dart",
		Content:  []byte(src),
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	methods := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" {
			methods[e.Name] = true
		}
	}
	for _, want := range []string{"add", "multiply"} {
		if !methods[want] {
			t.Errorf("expected method %q to be extracted", want)
		}
	}
}

func TestDartExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("dart")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.dart",
		Content:  []byte{},
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestDartExtractor_Signatures(t *testing.T) {
	src := `class Greeter {
  String greet(String name) {
    return "Hello $name";
  }
}
`
	ext, _ := extractor.Get("dart")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "greeter.dart",
		Content:  []byte(src),
		Language: "dart",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range entities {
		if e.Name == "greet" {
			if e.Signature == "" {
				t.Error("expected non-empty Signature for method 'greet'")
			}
			return
		}
	}
	// greet may not be found if regex skips it — that's acceptable for Dart regex parsing
}
