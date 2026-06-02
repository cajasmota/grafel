package php_test

// config_consumer_test.go — value-asserting tests for the PHP config-read pass
// (issue #3641, epic #3625). Asserts the SPECIFIC config key + edge, not len>0.

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func extractPHPRecords(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "cfg.php",
		Content:  []byte(src),
		Language: "php",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func phpConfigKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
	keys := map[string]bool{}
	for i := range recs {
		e := &recs[i]
		match := (from == "" && e.Kind == "SCOPE.Component" && e.Subtype == "file") ||
			(from != "" && e.Name == from)
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

func phpHasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestPHPConfigConsumer_GetenvAndSuperglobal(t *testing.T) {
	src := `<?php
class Db {
    public function connect(): void {
        $url = getenv('DATABASE_URL');
        $host = $_ENV['REDIS_HOST'];
    }
}
`
	recs := extractPHPRecords(t, src)
	keys := phpConfigKeysFrom(recs, "Db.connect")
	if !keys["DATABASE_URL"] {
		t.Errorf("expected DEPENDS_ON_CONFIG(Db.connect → DATABASE_URL); got %v", keys)
	}
	if !keys["REDIS_HOST"] {
		t.Errorf("expected DEPENDS_ON_CONFIG(Db.connect → REDIS_HOST) via $_ENV; got %v", keys)
	}
	if !phpHasConfigKeyEntity(recs, "DATABASE_URL") {
		t.Error("expected SCOPE.Config/config_key entity for DATABASE_URL")
	}
}

func TestPHPConfigConsumer_LaravelHelpers(t *testing.T) {
	src := `<?php
function boot(): void {
    $key = env('APP_KEY');
    $tz = config('app.timezone');
}
`
	recs := extractPHPRecords(t, src)
	keys := phpConfigKeysFrom(recs, "boot")
	if !keys["APP_KEY"] {
		t.Errorf("expected APP_KEY via Laravel env(); got %v", keys)
	}
	if !keys["app.timezone"] {
		t.Errorf("expected app.timezone via Laravel config(); got %v", keys)
	}
}

// Negative: a dynamic (variable) key must NOT produce an edge — honest-partial.
func TestPHPConfigConsumer_DynamicKeySkipped(t *testing.T) {
	src := `<?php
function read(string $name): void {
    getenv($name);
    env($name);
    $x = $_ENV[$name];
}
`
	recs := extractPHPRecords(t, src)
	keys := phpConfigKeysFrom(recs, "read")
	if len(keys) != 0 {
		t.Errorf("dynamic key must be skipped; got %v", keys)
	}
}
