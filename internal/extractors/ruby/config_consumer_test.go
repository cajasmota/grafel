package ruby_test

// config_consumer_test.go — value-asserting tests for the Ruby config-read pass
// (issue #3641, epic #3625). Asserts the SPECIFIC config key + edge, not len>0.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractRubyRecords(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("ruby")
	if !ok {
		t.Fatal("ruby extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "cfg.rb",
		Content:  []byte(src),
		Language: "ruby",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

// configKeysFrom returns the set of config keys read by the entity whose
// Name == from (use "" for file scope, matched by Subtype="file").
func configKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
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

func hasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestRubyConfigConsumer_EnvElementReference(t *testing.T) {
	src := `
def connect
  url = ENV['DATABASE_URL']
  host = ENV["REDIS_HOST"]
end
`
	recs := extractRubyRecords(t, src)
	keys := configKeysFrom(recs, "connect")
	if !keys["DATABASE_URL"] {
		t.Errorf("expected DEPENDS_ON_CONFIG(connect → DATABASE_URL); got %v", keys)
	}
	if !keys["REDIS_HOST"] {
		t.Errorf("expected DEPENDS_ON_CONFIG(connect → REDIS_HOST); got %v", keys)
	}
	if !hasConfigKeyEntity(recs, "DATABASE_URL") {
		t.Error("expected SCOPE.Config/config_key entity for DATABASE_URL")
	}
}

func TestRubyConfigConsumer_EnvFetch(t *testing.T) {
	src := `
def settings
  ENV.fetch('SECRET_KEY')
  ENV.fetch('PORT', '3000')
end
`
	recs := extractRubyRecords(t, src)
	keys := configKeysFrom(recs, "settings")
	if !keys["SECRET_KEY"] {
		t.Errorf("expected SECRET_KEY via ENV.fetch; got %v", keys)
	}
	if !keys["PORT"] {
		t.Errorf("expected PORT via ENV.fetch with default; got %v", keys)
	}
}

// Negative: a dynamic (variable) key must NOT produce an edge — honest-partial.
func TestRubyConfigConsumer_DynamicKeySkipped(t *testing.T) {
	src := `
def read(name)
  ENV[name]
  ENV.fetch(name)
end
`
	recs := extractRubyRecords(t, src)
	keys := configKeysFrom(recs, "read")
	if len(keys) != 0 {
		t.Errorf("dynamic key must be skipped; got %v", keys)
	}
}

// Interpolated string keys are dynamic → skipped.
func TestRubyConfigConsumer_InterpolatedKeySkipped(t *testing.T) {
	src := "def read(p)\n  ENV[\"PREFIX_#{p}\"]\nend\n"
	recs := extractRubyRecords(t, src)
	keys := configKeysFrom(recs, "read")
	if len(keys) != 0 {
		t.Errorf("interpolated key must be skipped; got %v", keys)
	}
}
