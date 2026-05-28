// Backend-HTTP observability recording sweep (#2905).
//
// Proves that observabilityJSTSExtractor attributes the expected
// observability signal (log / trace / metric) and library to a small
// hand-written fixture for each of the 12 backend-HTTP frameworks the
// Observability column tracks. These tests are the proving artefact for the
// honest-greening rule: each passes BEFORE the corresponding registry
// Observability cell is flipped to full.
//
// The extractor matches logger/tracer/metric import + call idioms, not the
// routing DSL, so one fixture per framework demonstrates the capability is
// real for that framework's idiomatic logging style.
package patterns

import (
	"os"
	"path/filepath"
	"testing"
)

func backendObservabilityFixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "extractors", "javascript", "testdata", "substrate_backend_observability"))
	if err != nil {
		t.Fatalf("cannot resolve fixture dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixture dir missing at %s: %v", dir, err)
	}
	return dir
}

func readObservabilityFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(backendObservabilityFixtureDir(t), name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read fixture %s: %v", path, err)
	}
	return string(b)
}

// TestBackendObservability asserts the observability extractor emits a
// SCOPE.Config(observability) entity carrying the expected signal + library
// for each framework fixture.
func TestBackendObservability(t *testing.T) {
	cases := []struct {
		framework string
		fixture   string
		signal    string
		library   string
	}{
		{"express", "express.ts", "log", "winston"},
		{"fastify", "fastify.ts", "log", "pino"},
		{"koa", "koa.ts", "log", "pino"},
		{"nestjs", "nestjs.ts", "trace", "opentelemetry"},
		{"adonisjs", "adonisjs.ts", "log", "pino"},
		{"feathers", "feathers.ts", "log", "winston"},
		{"hapi", "hapi.ts", "log", "pino"},
		{"hono", "hono.ts", "log", "console"},
		{"marblejs", "marblejs.ts", "log", "pino"},
		{"polka", "polka.ts", "log", "morgan"},
		{"restify", "restify.ts", "log", "bunyan"},
		{"sails", "sails.ts", "log", "winston"},
	}
	d := &observabilityJSTSExtractor{}
	for _, tc := range cases {
		t.Run(tc.framework, func(t *testing.T) {
			src := readObservabilityFixture(t, tc.fixture)
			if !d.AppliesTo(src) {
				t.Fatalf("AppliesTo returned false for %s fixture", tc.framework)
			}
			results := d.Detect(tc.fixture, "typescript", src)
			if len(results) == 0 {
				t.Fatalf("no observability entities emitted for %s fixture", tc.framework)
			}
			found := false
			for _, e := range results {
				if e.Kind != "SCOPE.Config" || e.Subtype != "observability" {
					t.Errorf("unexpected entity kind/subtype: %s/%s", e.Kind, e.Subtype)
				}
				if e.Properties["signal"] == tc.signal && e.Properties["library"] == tc.library {
					found = true
				}
			}
			if !found {
				t.Errorf("expected signal=%s library=%s, got entities %+v", tc.signal, tc.library, results)
			}
		})
	}
}

// TestObservabilityMetricSignal proves the metric branch fires independently
// of the log/trace branches (prom-client + OTel meter).
func TestObservabilityMetricSignal(t *testing.T) {
	d := &observabilityJSTSExtractor{}
	src := `import client from "prom-client";
import { metrics } from "@opentelemetry/api";
const counter = metrics.getMeter("svc").createCounter("requests");
const reg = new client.Registry();`
	results := d.Detect("metrics.ts", "typescript", src)
	var gotProm, gotOtel bool
	for _, e := range results {
		if e.Properties["signal"] != "metric" {
			continue
		}
		switch e.Properties["library"] {
		case "prom-client":
			gotProm = true
		case "opentelemetry":
			gotOtel = true
		}
	}
	if !gotProm {
		t.Error("expected prom-client metric signal")
	}
	if !gotOtel {
		t.Error("expected opentelemetry metric signal")
	}
}

// TestObservabilityIgnoresNonJSTS ensures the extractor is JS/TS-scoped and
// does not emit for other languages even when token-like text appears.
func TestObservabilityIgnoresNonJSTS(t *testing.T) {
	d := &observabilityJSTSExtractor{}
	src := `import "pino"` + "\n" + `console.log("x")`
	if got := d.Detect("logger.go", "go", src); len(got) != 0 {
		t.Errorf("expected no entities for go source, got %d", len(got))
	}
	if got := d.Detect("logger.py", "python", src); len(got) != 0 {
		t.Errorf("expected no entities for python source, got %d", len(got))
	}
}
