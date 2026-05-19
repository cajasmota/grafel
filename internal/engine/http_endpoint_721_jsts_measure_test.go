package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// TestJSTSFetches_Measurement is a corpus-measurement harness for #721.
// It walks the developer-local JS/TS frontend fixtures and reports the
// number of consumer-side http_endpoint entities and FETCHES edges the
// detector emits per fixture. It only runs when ARCHIGRAPH_721_MEASURE=1
// is set in the environment so the regular CI run is unaffected.
//
// Outputs to stderr in a stable, grep-friendly format:
//
//	[721-measure] fixture=client-fixture-b lang=javascript/typescript endpoints=N fetches=M
func TestJSTSFetches_Measurement(t *testing.T) {
	if os.Getenv("ARCHIGRAPH_721_MEASURE") != "1" {
		t.Skip("set ARCHIGRAPH_721_MEASURE=1 to run #721 corpus measurement")
	}
	home, _ := os.UserHomeDir()
	cases := []struct {
		fixture string
		root    string
	}{
		{"client-fixture-b", filepath.Join(home, "private/archigraph-fixtures/client-fixture-b")},
		{"client-fixture-c", filepath.Join(home, "private/archigraph-fixtures/client-fixture-c")},
		{"client-fixture-e", filepath.Join(home, "private/archigraph-fixtures/client-fixture-e")},
	}
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	jstsExts := map[string]bool{
		".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	}

	for _, c := range cases {
		if _, err := os.Stat(c.root); err != nil {
			t.Logf("[721-measure] skip fixture=%s root=%s (not present)", c.fixture, c.root)
			continue
		}
		var endpoints, fetches, runtimeDynamic int
		var sampleChain string
		_ = filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !jstsExts[ext] {
				return nil
			}
			lang := "javascript"
			if ext == ".ts" || ext == ".tsx" {
				lang = "typescript"
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			res, err := det.Detect(context.Background(), extractor.FileInput{
				Path:     p,
				Content:  body,
				Language: lang,
			})
			if err != nil {
				return nil
			}
			for _, e := range res.Entities {
				if e.Kind == httpEndpointKind && e.Properties != nil &&
					e.Properties["pattern_type"] == "http_endpoint_client_synthesis" {
					endpoints++
					if e.Properties["runtime_dynamic"] == "true" {
						runtimeDynamic++
					}
				}
			}
			for _, r := range res.Relationships {
				if r.Kind == fetchesEdgeKind {
					fetches++
					if sampleChain == "" {
						sampleChain = r.FromID + " → " + r.ToID
					}
				}
			}
			return nil
		})
		t.Logf("[721-measure] fixture=%s endpoints=%d fetches=%d runtime_dynamic=%d sample_chain=%q",
			c.fixture, endpoints, fetches, runtimeDynamic, sampleChain)
	}
}
