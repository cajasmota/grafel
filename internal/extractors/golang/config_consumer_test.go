package golang_test

// config_consumer_test.go — value-asserting tests for the Go config-read pass
// (issue #3641, epic #3625). Asserts the SPECIFIC config key, not len>0.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractGoRaw returns the full entity records (file entity + config-key
// entities included) for a Go source string.
func extractGoRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	tree := parseGo(content)
	ext, ok := extractor.Get("go")
	if !ok {
		t.Fatal("go extractor not registered")
	}
	records, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "cfg.go",
		Content:  content,
		Language: "go",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return records
}

// configReadKeysFrom returns the set of config keys read by the entity whose
// Name == from (use "" for file/module scope, matched by Subtype="file").
func configReadKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
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

// hasConfigKeyEntity reports whether a SCOPE.Config/config_key entity for key
// exists.
func hasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestGoConfigConsumer_OsGetenv(t *testing.T) {
	src := `package main

import "os"

func loadDB() string {
	return os.Getenv("DATABASE_URL")
}
`
	recs := extractGoRaw(t, src)
	keys := configReadKeysFrom(recs, "loadDB")
	if !keys["DATABASE_URL"] {
		t.Fatalf("loadDB: expected DEPENDS_ON_CONFIG read of DATABASE_URL, got %v", keys)
	}
	if !hasConfigKeyEntity(recs, "DATABASE_URL") {
		t.Errorf("expected config_key entity for DATABASE_URL")
	}
}

func TestGoConfigConsumer_Viper(t *testing.T) {
	src := `package main

import "github.com/spf13/viper"

func cfgHost() string {
	return viper.GetString("db.host")
}
`
	recs := extractGoRaw(t, src)
	keys := configReadKeysFrom(recs, "cfgHost")
	if !keys["db.host"] {
		t.Fatalf("cfgHost: expected DEPENDS_ON_CONFIG read of db.host, got %v", keys)
	}
	if !hasConfigKeyEntity(recs, "db.host") {
		t.Errorf("expected config_key entity for db.host")
	}
	// Assert the config-key entity Name is the namespaced "config:db.host".
	found := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" && recs[i].Name == "config:db.host" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected config-key entity Name \"config:db.host\"")
	}
}

func TestGoConfigConsumer_ViperReceiver(t *testing.T) {
	src := `package main

func (s *Server) port() int {
	return v.GetInt("server.port")
}
`
	recs := extractGoRaw(t, src)
	keys := configReadKeysFrom(recs, "Server.port")
	if !keys["server.port"] {
		t.Fatalf("Server.port: expected read of server.port, got %v", keys)
	}
}

// Negative: a dynamic key (os.Getenv(varName)) must NOT fabricate a config key.
func TestGoConfigConsumer_DynamicKeyIgnored(t *testing.T) {
	src := `package main

import "os"

func dyn(name string) string {
	return os.Getenv(name)
}
`
	recs := extractGoRaw(t, src)
	keys := configReadKeysFrom(recs, "dyn")
	if len(keys) != 0 {
		t.Fatalf("dynamic os.Getenv(name) must emit no config key, got %v", keys)
	}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" && recs[i].Subtype == "config_key" {
			t.Errorf("dynamic key must not create a config_key entity: %q", recs[i].Name)
		}
	}
}

// Negative: a non-viper receiver method named GetString must not be detected.
func TestGoConfigConsumer_UnrelatedGetStringIgnored(t *testing.T) {
	src := `package main

func read(repo *Repo) string {
	return repo.GetString("not.a.config")
}
`
	recs := extractGoRaw(t, src)
	if hasConfigKeyEntity(recs, "not.a.config") {
		t.Errorf("repo.GetString must not be treated as a viper config read")
	}
}
