package csharp_test

// config_consumer_test.go — value-asserting tests for the C# / .NET config-read
// pass (issue #3641, epic #3625). Asserts the SPECIFIC config key + edge.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractCSharpRecords(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "cfg.cs",
		Content:  []byte(src),
		Language: "csharp",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func csharpConfigKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
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

func csharpHasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestCSharpConfigConsumer_IndexerAndGetters(t *testing.T) {
	src := `
class StartupService {
    void Configure() {
        var a = Configuration["App:Url"];
        var b = Configuration.GetValue<int>("App:Port");
        var c = _configuration.GetConnectionString("Default");
    }
}
`
	recs := extractCSharpRecords(t, src)
	keys := csharpConfigKeysFrom(recs, "StartupService.Configure")
	for _, want := range []string{"App:Url", "App:Port", "Default"} {
		if !keys[want] {
			t.Errorf("expected DEPENDS_ON_CONFIG(StartupService.Configure → %s); got %v", want, keys)
		}
	}
	if !csharpHasConfigKeyEntity(recs, "App:Url") {
		t.Error("expected SCOPE.Config/config_key entity for App:Url")
	}
}

func TestCSharpConfigConsumer_EnvironmentVariable(t *testing.T) {
	src := `
class Loader {
    void Load() {
        var p = Environment.GetEnvironmentVariable("DATABASE_URL");
    }
}
`
	recs := extractCSharpRecords(t, src)
	keys := csharpConfigKeysFrom(recs, "Loader.Load")
	if !keys["DATABASE_URL"] {
		t.Errorf("expected DATABASE_URL via Environment.GetEnvironmentVariable; got %v", keys)
	}
}

// Negative: a dynamic (variable) key must NOT produce an edge — honest-partial.
func TestCSharpConfigConsumer_DynamicKeySkipped(t *testing.T) {
	src := `
class Reader {
    void Read(string name) {
        var a = Configuration[name];
        var b = Configuration.GetValue<int>(name);
        var c = Environment.GetEnvironmentVariable(name);
    }
}
`
	recs := extractCSharpRecords(t, src)
	keys := csharpConfigKeysFrom(recs, "Reader.Read")
	if len(keys) != 0 {
		t.Errorf("dynamic key must be skipped; got %v", keys)
	}
}

// Cross-language convergence: the C# config-key entity ID for DATABASE_URL must
// equal the constant ID extractor.ConfigKeyEntity produces for any language, so
// a Node `process.env.DATABASE_URL` and a C# Environment read of the same key
// land on ONE shared SCOPE.Config node ("who depends on DATABASE_URL").
func TestCSharpConfigConsumer_CrossLanguageConvergence(t *testing.T) {
	src := `
class Loader {
    void Load() {
        var p = Environment.GetEnvironmentVariable("DATABASE_URL");
    }
}
`
	recs := extractCSharpRecords(t, src)

	// The DEPENDS_ON_CONFIG edge's ToID must equal the canonical target ID.
	wantTarget := extractor.ConfigKeyTargetID("DATABASE_URL")
	var foundEdge bool
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" && r.ToID == wantTarget {
				foundEdge = true
			}
		}
	}
	if !foundEdge {
		t.Fatalf("C# edge ToID must be the cross-language target ID %q", wantTarget)
	}

	// The emitted config-key entity ID must match what a Node/Python/Go
	// extractor would compute for the same key (synthetic SourceFile dedup).
	csharpKeyID := ""
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == "DATABASE_URL" {
			csharpKeyID = e.ID
		}
	}
	nodeKeyID := extractor.ConfigKeyEntity("DATABASE_URL", "javascript").ID
	if csharpKeyID == "" {
		t.Fatal("no DATABASE_URL config-key entity emitted by C# extractor")
	}
	if csharpKeyID != nodeKeyID {
		t.Errorf("cross-language convergence broken: C# key ID %q != Node key ID %q", csharpKeyID, nodeKeyID)
	}
}
