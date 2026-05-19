// Microbenchmarks for ADR-0016: JSON vs FlatBuffers on a real graph.
//
// The fixture path is configurable via the ARCHIGRAPH_BENCH_FIXTURE env
// var so the bench is reproducible on any machine. When unset, falls
// back to the developer's standard local fixture. If neither exists,
// the bench is skipped (so `go test ./...` stays green on CI).
//
// Run with:
//   go test ./internal/graph/ -bench=. -benchmem -run=^$ -count=3
package graph_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/graph/fbreader"
	"github.com/cajasmota/archigraph/internal/graph/fbwriter"
)

const defaultFixture = "/tmp/archigraph-bench-fixture.json"

// fixturePaths returns (jsonPath, fbPath) and skips if the fixture
// isn't present. The fb sidecar is materialized once next to the json.
func fixturePaths(tb testing.TB) (string, string) {
	tb.Helper()
	p := os.Getenv("ARCHIGRAPH_BENCH_FIXTURE")
	if p == "" {
		p = defaultFixture
	}
	if _, err := os.Stat(p); err != nil {
		tb.Skipf("fixture %s not present: %v", p, err)
	}
	fbPath := filepath.Join(filepath.Dir(p), "graph.fb")
	if _, err := os.Stat(fbPath); err != nil {
		// Materialize the .fb once.
		data, err := os.ReadFile(p)
		if err != nil {
			tb.Fatalf("read fixture: %v", err)
		}
		var doc graph.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			tb.Fatalf("unmarshal fixture: %v", err)
		}
		if err := fbwriter.WriteAtomic(fbPath, &doc); err != nil {
			tb.Fatalf("write fb: %v", err)
		}
	}
	return p, fbPath
}

func BenchmarkJSONUnmarshal(b *testing.B) {
	jsonPath, _ := fixturePaths(b)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var doc graph.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFBOpen(b *testing.B) {
	_, fbPath := fixturePaths(b)
	info, _ := os.Stat(fbPath)
	b.SetBytes(info.Size())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := fbreader.Open(fbPath)
		if err != nil {
			b.Fatal(err)
		}
		_ = r.EntityCount()
		_ = r.RelationshipCount()
		r.Close()
	}
}

func BenchmarkJSONLookupEntity(b *testing.B) {
	jsonPath, _ := fixturePaths(b)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var doc graph.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			b.Fatal(err)
		}
		target := doc.Entities[len(doc.Entities)/2].ID
		found := ""
		for j := range doc.Entities {
			if doc.Entities[j].ID == target {
				found = doc.Entities[j].Name
				break
			}
		}
		_ = found
	}
}

func BenchmarkFBLookupEntity(b *testing.B) {
	_, fbPath := fixturePaths(b)
	// Pick a target id once, outside the timed loop, so we benchmark
	// open + lookup rather than benchmark a moving target.
	rInit, err := fbreader.Open(fbPath)
	if err != nil {
		b.Fatal(err)
	}
	mid := rInit.EntityAt(rInit.EntityCount() / 2)
	if mid == nil {
		b.Fatal("middle entity nil")
	}
	targetID := string(mid.Id())
	rInit.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := fbreader.Open(fbPath)
		if err != nil {
			b.Fatal(err)
		}
		ent := r.LookupEntityByID(targetID)
		if ent == nil {
			b.Fatalf("lookup miss for %s", targetID)
		}
		_ = ent.Name()
		r.Close()
	}
}

// BenchmarkFBLookupEntityHot isolates the lookup cost from the open
// cost — once the file is mmap'd, repeated lookups should be tens of
// nanoseconds (binary search over a sorted vector).
func BenchmarkFBLookupEntityHot(b *testing.B) {
	_, fbPath := fixturePaths(b)
	r, err := fbreader.Open(fbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	mid := r.EntityAt(r.EntityCount() / 2)
	targetID := string(mid.Id())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ent := r.LookupEntityByID(targetID)
		if ent == nil {
			b.Fatalf("lookup miss for %s", targetID)
		}
	}
}

// Size summary helper — surface the on-disk delta in the bench output.
func BenchmarkSizesReport(b *testing.B) {
	jsonPath, fbPath := fixturePaths(b)
	js, _ := os.Stat(jsonPath)
	fbs, _ := os.Stat(fbPath)
	b.ReportMetric(float64(js.Size()), "json_bytes")
	b.ReportMetric(float64(fbs.Size()), "fb_bytes")
	b.ReportMetric(float64(js.Size())/float64(fbs.Size()), "json/fb_ratio")
	fmt.Fprintf(os.Stderr, "[ADR-0016] json=%d fb=%d ratio=%.2fx\n",
		js.Size(), fbs.Size(), float64(js.Size())/float64(fbs.Size()))
}
