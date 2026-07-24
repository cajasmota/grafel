package python_test

// config_consumer_test.go — fixture tests for issue #1982.
//
// Verifies DEPENDS_ON_CONFIG edges land on the enclosing entity for
// django.conf settings consumers and os.environ / os.getenv consumers.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// countConfigEdgesFrom returns the DEPENDS_ON_CONFIG edges emitted by the
// entity whose Name matches `from` (use "" for the file entity, identified
// by Subtype="file").
func countConfigEdgesFrom(entities []types.EntityRecord, filePath, from string) []types.RelationshipRecord {
	for i := range entities {
		e := &entities[i]
		if e.SourceFile != filePath {
			continue
		}
		match := false
		if from == "" && e.Kind == "SCOPE.Component" && e.Subtype == "file" {
			match = true
		}
		if from != "" && e.Name == from {
			match = true
		}
		if !match {
			continue
		}
		var out []types.RelationshipRecord
		for _, r := range e.Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" {
				out = append(out, r)
			}
		}
		return out
	}
	return nil
}

// TestConfigConsumer_DjangoSettings verifies that a view referencing
// settings.X emits a DEPENDS_ON_CONFIG edge with config_name="settings".
//
// Issue #1982.
func TestConfigConsumer_DjangoSettings(t *testing.T) {
	src := `from django.conf import settings

def get_email_host():
    return settings.EMAIL_HOST

class Mailer:
    def send(self):
        host = settings.EMAIL_HOST
        port = settings.EMAIL_PORT
`
	out := extractPy(t, src, "core/mail.py")

	// Function-scope edge.
	edges := countConfigEdgesFrom(out, "core/mail.py", "get_email_host")
	if len(edges) != 1 {
		t.Fatalf("get_email_host: expected 1 DEPENDS_ON_CONFIG edge, got %d", len(edges))
	}
	if edges[0].Properties.Get("config_name") != "settings" {
		t.Errorf("edge config_name = %q, want \"settings\"", edges[0].Properties.Get("config_name"))
	}
	if !strings.Contains(edges[0].Properties.Get("keys"), "EMAIL_HOST") {
		t.Errorf("edge keys = %q, want to contain EMAIL_HOST", edges[0].Properties.Get("keys"))
	}

	// Method-scope edge.
	methodEdges := countConfigEdgesFrom(out, "core/mail.py", "Mailer.send")
	if len(methodEdges) != 1 {
		t.Fatalf("Mailer.send: expected 1 DEPENDS_ON_CONFIG edge, got %d", len(methodEdges))
	}
	keys := methodEdges[0].Properties.Get("keys")
	if !strings.Contains(keys, "EMAIL_HOST") || !strings.Contains(keys, "EMAIL_PORT") {
		t.Errorf("Mailer.send keys = %q, want to contain EMAIL_HOST and EMAIL_PORT", keys)
	}
}

// TestConfigConsumer_AliasedImport verifies `from django.conf import
// settings as my_settings` is recognised via the import binding.
func TestConfigConsumer_AliasedImport(t *testing.T) {
	src := `from django.conf import settings as my_settings

def view():
    return my_settings.DEBUG
`
	out := extractPy(t, src, "app/views.py")
	edges := countConfigEdgesFrom(out, "app/views.py", "view")
	if len(edges) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON_CONFIG edge, got %d", len(edges))
	}
	if edges[0].Properties.Get("keys") != "DEBUG" {
		t.Errorf("keys = %q, want \"DEBUG\"", edges[0].Properties.Get("keys"))
	}
}

// TestConfigConsumer_OsEnviron verifies os.environ.get / os.getenv shapes
// produce edges with config_name=".env".
func TestConfigConsumer_OsEnviron(t *testing.T) {
	src := `import os

def boot():
    db = os.environ.get("DATABASE_URL")
    key = os.getenv("SECRET_KEY")
    return db, key
`
	out := extractPy(t, src, "app/boot.py")
	edges := countConfigEdgesFrom(out, "app/boot.py", "boot")
	if len(edges) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON_CONFIG edge, got %d", len(edges))
	}
	if edges[0].Properties.Get("config_name") != ".env" {
		t.Errorf("config_name = %q, want \".env\"", edges[0].Properties.Get("config_name"))
	}
	keys := edges[0].Properties.Get("keys")
	if !strings.Contains(keys, "DATABASE_URL") || !strings.Contains(keys, "SECRET_KEY") {
		t.Errorf("keys = %q, want to contain DATABASE_URL and SECRET_KEY", keys)
	}
}

// TestConfigConsumer_OsEnvironSubscript verifies os.environ["X"] shape.
func TestConfigConsumer_OsEnvironSubscript(t *testing.T) {
	src := `import os

def boot():
    return os.environ["MY_KEY"]
`
	out := extractPy(t, src, "app/boot.py")
	edges := countConfigEdgesFrom(out, "app/boot.py", "boot")
	if len(edges) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON_CONFIG edge, got %d", len(edges))
	}
	if edges[0].Properties.Get("keys") != "MY_KEY" {
		t.Errorf("keys = %q, want \"MY_KEY\"", edges[0].Properties.Get("keys"))
	}
}

// TestConfigConsumer_NoEdgesWithoutImport verifies that without the right
// import binding, no edge is emitted (we don't over-emit on bare names).
func TestConfigConsumer_NoEdgesWithoutImport(t *testing.T) {
	src := `# no django.conf import

def view():
    return settings.X
`
	out := extractPy(t, src, "app/views.py")
	edges := countConfigEdgesFrom(out, "app/views.py", "view")
	if len(edges) != 0 {
		t.Errorf("expected 0 DEPENDS_ON_CONFIG edges without import binding, got %d", len(edges))
	}
}
