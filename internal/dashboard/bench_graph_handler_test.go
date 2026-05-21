package dashboard

// bench_graph_handler_test.go — benchmark the /api/graph/{group} handler
// with a synthetic 100k-node graph document to measure before/after for
// the typed-struct + gzip improvements in #1249.
//
// Run with:
//
//	go test ./internal/dashboard/ -bench=BenchmarkServeGraphDense -benchmem -run=^$ -count=3
//
// The benchmark is intentionally self-contained: it generates a synthetic
// Document in memory so it runs on CI without any external fixture files.

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// makeSyntheticDoc builds a graph.Document with nEntities entities and
// approximately nEntities*avgDegree/2 relationships.
// Community assignments and PageRank values are deterministically randomised.
func makeSyntheticDoc(nEntities int, avgDegree int) *graph.Document {
	rng := rand.New(rand.NewSource(42))
	kinds := []string{
		"SCOPE.Function", "SCOPE.Class", "SCOPE.Module",
		"SCOPE.Method", "SCOPE.Interface",
	}
	langs := []string{"go", "python", "typescript", "java"}

	entities := make([]graph.Entity, nEntities)
	for i := range entities {
		cid := i / 200 // ~200 nodes per community
		pr := rng.Float64() * 0.01
		entities[i] = graph.Entity{
			ID:          fmt.Sprintf("ent-%06d", i),
			Name:        fmt.Sprintf("Entity%06d", i),
			Kind:        kinds[i%len(kinds)],
			SourceFile:  fmt.Sprintf("src/pkg%d/file%d.go", i/1000, i%1000),
			StartLine:   rng.Intn(500) + 1,
			Language:    langs[i%len(langs)],
			CommunityID: &cid,
			PageRank:    &pr,
		}
	}

	nRels := nEntities * avgDegree / 2
	rels := make([]graph.Relationship, nRels)
	for i := range rels {
		from := rng.Intn(nEntities)
		to := rng.Intn(nEntities)
		if from == to {
			to = (to + 1) % nEntities
		}
		rels[i] = graph.Relationship{
			FromID: fmt.Sprintf("ent-%06d", from),
			ToID:   fmt.Sprintf("ent-%06d", to),
			Kind:   "calls",
		}
	}

	nCommunities := nEntities / 200
	if nCommunities == 0 {
		nCommunities = 1
	}
	communities := make([]graph.CommunityResult, nCommunities)
	for i := range communities {
		top := []string{
			fmt.Sprintf("ent-%06d", i*200),
			fmt.Sprintf("ent-%06d", i*200+1),
			fmt.Sprintf("ent-%06d", i*200+2),
		}
		communities[i] = graph.CommunityResult{
			ID:          i,
			Size:        200,
			AutoName:    fmt.Sprintf("community-%d", i),
			TopEntities: top,
		}
	}

	return &graph.Document{
		Version:       graph.SchemaVersion,
		Repo:          "/tmp/synthetic-repo",
		Entities:      entities,
		Relationships: rels,
		Communities:   communities,
	}
}

// newBenchServer wires a Server with a pre-loaded in-memory graph group.
// The group is named "bench" and contains a single repo "r0" with doc.
func newBenchServer(b *testing.B, doc *graph.Document) *Server {
	b.Helper()
	store := newFakeStore()
	cfg := DefaultConfig()
	s, err := NewServer(cfg, store)
	if err != nil {
		b.Fatalf("NewServer: %v", err)
	}
	// Directly inject the synthetic document into the graph cache so there
	// is no disk I/O during the benchmark — we are measuring handler CPU.
	s.graphs.mu.Lock()
	s.graphs.entries["bench"] = &cacheEntry{
		group: &DashGroup{
			Name: "bench",
			Repos: map[string]*DashRepo{
				"r0": {
					Slug: "r0",
					Doc:  doc,
				},
			},
		},
		loadedAt: time.Now(),
	}
	s.graphs.mu.Unlock()
	return s
}

// BenchmarkServeGraphDense100k measures the end-to-end handler cost
// (entity iteration, JSON encoding, gzip compression) for a 100k-node graph.
func BenchmarkServeGraphDense100k(b *testing.B) {
	benchmarkServeGraphDense(b, 100_000, 4)
}

// BenchmarkServeGraphDense20k mirrors the current ~upvate production scale.
func BenchmarkServeGraphDense20k(b *testing.B) {
	benchmarkServeGraphDense(b, 20_000, 4)
}

func benchmarkServeGraphDense(b *testing.B, nEntities, avgDegree int) {
	b.Helper()
	doc := makeSyntheticDoc(nEntities, avgDegree)
	s := newBenchServer(b, doc)
	handler := s.routes()

	// Test both compressed and plain paths.
	for _, compress := range []bool{true, false} {
		name := "plain"
		if compress {
			name = "gzip"
		}
		b.Run(name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
			if compress {
				req.Header.Set("Accept-Encoding", "gzip")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rw := httptest.NewRecorder()
				handler.ServeHTTP(rw, req)
				if rw.Code != http.StatusOK {
					b.Fatalf("unexpected status %d body=%s", rw.Code, rw.Body.String())
				}
			}
		})
	}
}

// BenchmarkServeGraphDenseCacheHit measures the warm-path (payload-cache hit)
// for GET /api/graph/{group} so we can compare against the cold-path benchmarks.
//
// Run with:
//
//	go test ./internal/dashboard/ -bench=BenchmarkServeGraphDenseCacheHit -benchmem -run=^$ -count=3
func BenchmarkServeGraphDenseCacheHit20k(b *testing.B) {
	benchmarkServeGraphDenseCacheHit(b, 20_000, 4)
}

func BenchmarkServeGraphDenseCacheHit100k(b *testing.B) {
	benchmarkServeGraphDenseCacheHit(b, 100_000, 4)
}

func benchmarkServeGraphDenseCacheHit(b *testing.B, nEntities, avgDegree int) {
	b.Helper()
	doc := makeSyntheticDoc(nEntities, avgDegree)
	s := newBenchServer(b, doc)
	handler := s.routes()

	// Cold request to prime the payload cache.
	prime := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
	primew := httptest.NewRecorder()
	handler.ServeHTTP(primew, prime)
	if primew.Code != http.StatusOK {
		b.Fatalf("prime request status=%d", primew.Code)
	}

	// Warm path — plain and gzip.
	for _, compress := range []bool{true, false} {
		name := "plain"
		if compress {
			name = "gzip"
		}
		b.Run(name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
			if compress {
				req.Header.Set("Accept-Encoding", "gzip")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rw := httptest.NewRecorder()
				handler.ServeHTTP(rw, req)
				if rw.Code != http.StatusOK {
					b.Fatalf("unexpected status %d body=%s", rw.Code, rw.Body.String())
				}
			}
		})
	}
}

// BenchmarkPayloadCacheDirectHit measures the hot-path cost of a cache hit
// in serveGraphDense without gzip compression, to isolate the
// "map lookup + memcpy" cost from the "build + encode" cost.
//
// Cold: serveGraphDense 20k-plain ≈ 300-480 µs
// Warm: BenchmarkPayloadCacheDirect20k/plain ≈ <5 µs  (expected)
func BenchmarkPayloadCacheDirect20k(b *testing.B) {
	benchmarkPayloadCacheDirect(b, 20_000, 4)
}

func benchmarkPayloadCacheDirect(b *testing.B, nEntities, avgDegree int) {
	b.Helper()
	doc := makeSyntheticDoc(nEntities, avgDegree)
	s := newBenchServer(b, doc)
	handler := s.routes()

	// Seed the payload cache with one cold request.
	seedReq := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
	seedRW := httptest.NewRecorder()
	handler.ServeHTTP(seedRW, seedReq)
	if seedRW.Code != http.StatusOK {
		b.Fatalf("seed status=%d", seedRW.Code)
	}

	// Confirm the cache was populated.
	cacheKey := payloadCacheKey("bench", "", "", "", false, false)
	if _, hit := s.graphs.Payloads.Get(cacheKey); !hit {
		b.Fatal("payload cache was not populated after seed request")
	}

	b.Run("plain-cache-hit", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
			rw := httptest.NewRecorder()
			handler.ServeHTTP(rw, req)
			if rw.Code != http.StatusOK {
				b.Fatalf("status=%d", rw.Code)
			}
		}
	})

	b.Run("etag-304-cache-hit", func(b *testing.B) {
		// Get the ETag from the seed response.
		etag := seedRW.Header().Get("ETag")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/graph/bench", nil)
			req.Header.Set("If-None-Match", etag)
			rw := httptest.NewRecorder()
			handler.ServeHTTP(rw, req)
			if rw.Code != http.StatusNotModified {
				b.Fatalf("expected 304, got %d", rw.Code)
			}
		}
	})
}
