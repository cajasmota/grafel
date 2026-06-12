package rust_test

// config_consumer_test.go — value-asserting tests for the Rust config-read pass
// (issue #5020, epic #3641). Asserts the SPECIFIC config key + DEPENDS_ON_CONFIG
// edge + shared SCOPE.Config node, not len>0.

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractRustForConfig(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("rust")
	if !ok {
		t.Fatal("rust extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "config.rs",
		Content:  []byte(src),
		Language: "rust",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

// rustConfigKeyEntity reports whether a shared SCOPE.Config / config_key node
// for the given key exists.
func rustConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" &&
			e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

// rustConfigEdgeFrom returns the set of (config_key, pattern) DEPENDS_ON_CONFIG
// edges emitted from the entity whose Name == from ("" => file entity).
func rustConfigEdgeFrom(recs []types.EntityRecord, from string) map[string]string {
	out := map[string]string{}
	for i := range recs {
		e := &recs[i]
		match := (from == "" && e.Kind == "SCOPE.Component" && e.Subtype == "file") ||
			(from != "" && e.Name == from)
		if !match {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" {
				out[r.Properties["config_key"]] = r.Properties["pattern"]
			}
		}
	}
	return out
}

func TestRustConfig_EnvVar(t *testing.T) {
	src := `
use std::env;

fn load() -> String {
    let url = env::var("DATABASE_URL").unwrap();
    let port = std::env::var("PORT").unwrap_or_else(|_| "8080".into());
    format!("{}:{}", url, port)
}
`
	recs := extractRustForConfig(t, src)

	if !rustConfigKeyEntity(recs, "DATABASE_URL") {
		t.Fatal("missing SCOPE.Config node for DATABASE_URL")
	}
	if !rustConfigKeyEntity(recs, "PORT") {
		t.Fatal("missing SCOPE.Config node for PORT")
	}
	edges := rustConfigEdgeFrom(recs, "load")
	if edges["DATABASE_URL"] != "env_var" {
		t.Fatalf("DATABASE_URL: want pattern env_var, got %q (edges=%v)", edges["DATABASE_URL"], edges)
	}
	if edges["PORT"] != "env_var" {
		t.Fatalf("PORT: want pattern env_var, got %q (edges=%v)", edges["PORT"], edges)
	}
}

func TestRustConfig_Dotenvy(t *testing.T) {
	src := `
fn init() {
    dotenvy::dotenv().ok();
    let key = dotenvy::var("API_KEY").expect("API_KEY must be set");
    println!("{}", key);
}
`
	recs := extractRustForConfig(t, src)
	if !rustConfigKeyEntity(recs, "API_KEY") {
		t.Fatal("missing SCOPE.Config node for API_KEY")
	}
	edges := rustConfigEdgeFrom(recs, "init")
	if edges["API_KEY"] != "dotenvy" {
		t.Fatalf("API_KEY: want pattern dotenvy, got %q (edges=%v)", edges["API_KEY"], edges)
	}
}

func TestRustConfig_FigmentPrefix(t *testing.T) {
	src := `
use figment::providers::Env;

fn figment() {
    let provider = Env::prefixed("APP_");
    let _ = provider;
}
`
	recs := extractRustForConfig(t, src)
	if !rustConfigKeyEntity(recs, "APP_") {
		t.Fatal("missing SCOPE.Config node for figment prefix APP_")
	}
	edges := rustConfigEdgeFrom(recs, "figment")
	if edges["APP_"] != "figment" {
		t.Fatalf("APP_: want pattern figment, got %q (edges=%v)", edges["APP_"], edges)
	}
}

func TestRustConfig_MethodHostName(t *testing.T) {
	src := `
use std::env;

struct Settings;

impl Settings {
    fn from_env() -> String {
        env::var("SECRET").unwrap()
    }
}
`
	recs := extractRustForConfig(t, src)
	edges := rustConfigEdgeFrom(recs, "Settings.from_env")
	if edges["SECRET"] != "env_var" {
		t.Fatalf("SECRET: want edge from Settings.from_env with pattern env_var, got %v", edges)
	}
}

func TestRustConfig_DynamicKeySkipped(t *testing.T) {
	src := `
use std::env;

fn dynamic(name: &str) -> String {
    env::var(name).unwrap_or_default()
}
`
	recs := extractRustForConfig(t, src)
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" {
			t.Fatalf("dynamic key must not emit a config node, got %q", recs[i].Name)
		}
	}
}
