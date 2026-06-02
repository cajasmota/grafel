package javascript_test

// config_consumer_test.go — value-asserting tests for the JS/TS config-read
// pass (issue #3641, epic #3625). Asserts the SPECIFIC config key, not len>0.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func jsConfigKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
	keys := map[string]bool{}
	for i := range recs {
		e := &recs[i]
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
		for _, r := range e.Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" {
				keys[r.Properties["config_key"]] = true
			}
		}
	}
	return keys
}

func jsHasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestJSConfigConsumer_ProcessEnv(t *testing.T) {
	src := []byte(`function connect() {
  const url = process.env.API_URL;
  return url;
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	keys := jsConfigKeysFrom(recs, "connect")
	if !keys["API_URL"] {
		t.Fatalf("connect: expected process.env.API_URL read, got %v", keys)
	}
	if !jsHasConfigKeyEntity(recs, "API_URL") {
		t.Errorf("expected config_key entity for API_URL")
	}
	// Assert namespaced entity Name.
	found := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" && recs[i].Name == "config:API_URL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected config-key entity Name \"config:API_URL\"")
	}
}

func TestJSConfigConsumer_ProcessEnvSubscript(t *testing.T) {
	src := []byte(`function f() {
  return process.env['DATABASE_URL'];
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	keys := jsConfigKeysFrom(recs, "f")
	if !keys["DATABASE_URL"] {
		t.Fatalf("expected process.env['DATABASE_URL'] read, got %v", keys)
	}
}

func TestJSConfigConsumer_NodeConfigGet(t *testing.T) {
	src := []byte(`function dbHost() {
  return config.get('db.host');
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	keys := jsConfigKeysFrom(recs, "dbHost")
	if !keys["db.host"] {
		t.Fatalf("expected config.get('db.host') read, got %v", keys)
	}
	if !jsHasConfigKeyEntity(recs, "db.host") {
		t.Errorf("expected config_key entity for db.host")
	}
}

func TestTSConfigConsumer_ImportMetaEnv(t *testing.T) {
	src := []byte(`export function apiBase(): string {
  return import.meta.env.VITE_API_BASE;
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	keys := jsConfigKeysFrom(recs, "apiBase")
	if !keys["VITE_API_BASE"] {
		t.Fatalf("expected import.meta.env.VITE_API_BASE read, got %v", keys)
	}
}

func TestJSConfigConsumer_ArrowComponent(t *testing.T) {
	src := []byte(`const Widget = () => {
  const k = process.env.FEATURE_FLAG;
  return k;
};
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	keys := jsConfigKeysFrom(recs, "Widget")
	if !keys["FEATURE_FLAG"] {
		t.Fatalf("Widget: expected process.env.FEATURE_FLAG read, got %v", keys)
	}
}

// Negative: dynamic subscript key must not fabricate a config key.
func TestJSConfigConsumer_DynamicKeyIgnored(t *testing.T) {
	src := []byte(`function dyn(name) {
  return process.env[name];
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	keys := jsConfigKeysFrom(recs, "dyn")
	if len(keys) != 0 {
		t.Fatalf("dynamic process.env[name] must emit no config key, got %v", keys)
	}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" && recs[i].Subtype == "config_key" {
			t.Errorf("dynamic key must not create a config_key entity: %q", recs[i].Name)
		}
	}
}
