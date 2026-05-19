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

// TestWave1Measurement_PythonJavaConsumerCounts is a measurement harness
// rather than an assertion test: it walks the developer-local Python and
// Java fixtures and reports the number of consumer-side http_endpoint
// entities and FETCHES edges the detector emits per-language. It only
// runs when ARCHIGRAPH_W1_MEASURE=1 is set in the environment so the
// regular CI run is unaffected.
//
// Why a test instead of a binary: the engine.Detect API is package-private
// to internal/engine. A test here is the cleanest way to exercise the
// exact extraction pipeline the indexer uses without spinning up the
// daemon.
//
// Outputs to stderr in a stable, grep-friendly format:
//
//	[wave1-measure] lang=python repo=client-fixture-a endpoints=N fetches=M
//	[wave1-measure] lang=java repo=client-fixture-d endpoints=N fetches=M
func TestWave1Measurement_PythonJavaConsumerCounts(t *testing.T) {
	if os.Getenv("ARCHIGRAPH_W1_MEASURE") != "1" {
		t.Skip("set ARCHIGRAPH_W1_MEASURE=1 to run wave-1 corpus measurement")
	}
	home, _ := os.UserHomeDir()
	cases := []struct {
		lang string
		exts []string
		root string
	}{
		{"python", []string{".py"}, filepath.Join(home, "private/archigraph-fixtures/client-fixture-a")},
		{"java", []string{".java"}, filepath.Join(home, "private/archigraph-fixtures/client-fixture-d")},
	}
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)

	for _, c := range cases {
		if _, err := os.Stat(c.root); err != nil {
			t.Logf("[wave1-measure] skip lang=%s root=%s (not present)", c.lang, c.root)
			continue
		}
		var endpoints, fetches int
		err := filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			matched := false
			for _, e := range c.exts {
				if ext == e {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			res, err := det.Detect(context.Background(), extractor.FileInput{
				Path:     p,
				Content:  body,
				Language: c.lang,
			})
			if err != nil {
				return nil
			}
			for _, e := range res.Entities {
				if e.Kind == httpEndpointKind && e.Properties != nil &&
					e.Properties["pattern_type"] == patternTypeConsumerW1 {
					endpoints++
				}
			}
			for _, r := range res.Relationships {
				if r.Kind == fetchesEdgeKind {
					fetches++
				}
			}
			return nil
		})
		if err != nil {
			t.Logf("[wave1-measure] walk error lang=%s: %v", c.lang, err)
		}
		t.Logf("[wave1-measure] lang=%s repo=%s endpoints=%d fetches=%d",
			c.lang, filepath.Base(c.root), endpoints, fetches)
	}
}

// patternTypeConsumerW1 mirrors the literal value used by the makeEmit
// closure in http_endpoint_synthesis.go for consumer-side synthetics.
// Duplicated here to avoid a cycle with the internal links package.
const patternTypeConsumerW1 = "http_endpoint_client_synthesis"
