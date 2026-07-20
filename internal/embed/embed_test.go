package embed

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// fakeBackend is a deterministic, dim-N embedding backend for tests. The
// vector is hash-bag-of-words style: each token's SHA1 first 4 bytes select a
// dim and increment it. L2 normalization makes dot product == cosine, which
// gives a small but real semantic signal: queries that share tokens with an
// entity's embed text outrank others.
type fakeBackend struct{ dims int }

func (f *fakeBackend) Dims() int    { return f.dims }
func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Close() error { return nil }
func (f *fakeBackend) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, f.dims)
		for _, tok := range strings.Fields(strings.ToLower(t)) {
			h := sha1.Sum([]byte(tok))
			idx := binary.LittleEndian.Uint32(h[:4]) % uint32(f.dims)
			v[idx] += 1.0
		}
		out[i] = l2Normalize(v)
	}
	return out, nil
}

func TestConfig_EnvOverride(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvBackend, "http")
	t.Setenv(EnvURL, "http://example.test/v1")
	t.Setenv(EnvModel, "fake-model")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Backend != BackendHTTP || cfg.HTTP.Model != "fake-model" || cfg.HTTP.URL != "http://example.test/v1" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

// TestConfig_DefaultIsBuiltin verifies that a fresh install with no config
// file and no env vars defaults to bundled MiniLM mode (S6 / #2156).
func TestConfig_DefaultIsBuiltin(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	for _, e := range []string{EnvBackend, EnvURL, EnvModel, EnvAPIKey, EnvDims, EnvDisable} {
		t.Setenv(e, "")
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.Backend != BackendBuiltin {
		t.Fatalf("want builtin (MiniLM), got %+v err=%v", cfg, err)
	}
}

// TestConfig_DisableEnvOverrides verifies that GRAFEL_EMBEDDING_DISABLE
// overrides any other settings and forces BM25-only mode.
func TestConfig_DisableEnvOverrides(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvDisable, "true")
	t.Setenv(EnvBackend, "builtin")
	t.Setenv(EnvURL, "http://example.test/v1")
	// Even with builtin and URL set, DISABLE should take precedence.
	cfg, err := LoadConfig()
	if err != nil || cfg.Backend != BackendDisabled {
		t.Fatalf("want disabled (override), got %+v err=%v", cfg, err)
	}

	// Also test with "1" (common shell true value).
	t.Setenv(EnvDisable, "1")
	cfg, err = LoadConfig()
	if err != nil || cfg.Backend != BackendDisabled {
		t.Fatalf("want disabled (override with 1), got %+v err=%v", cfg, err)
	}
}

func TestEmbedTextAndHashStability(t *testing.T) {
	e := graph.EntityPtr(graph.Entity{Name: "Login", QualifiedName: "auth.Login"}.WithProperties(map[string]string{"docstring": "Verify bearer token and create session"}))
	a := EmbedText(e, "func Login(token string) (*Session, error) { ... }")
	b := EmbedText(e, "func Login(token string) (*Session, error) { ... }")
	if a != b {
		t.Fatal("EmbedText should be deterministic")
	}
	if ContentHash(a) != ContentHash(b) {
		t.Fatal("ContentHash should be stable for identical text")
	}
	// Bump-by-changing-snippet behavior.
	c := EmbedText(e, "func Login(token string) (*Session, error) { /* changed */ }")
	if ContentHash(a) == ContentHash(c) {
		t.Fatal("ContentHash should change when text changes")
	}
}

func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(4, "test")
	s.Put(Record{ID: "a", Hash: "h1", Vector: l2Normalize([]float32{1, 0, 0, 0})})
	s.Put(Record{ID: "b", Hash: "h2", Vector: l2Normalize([]float32{0, 1, 0, 0})})
	if err := s.Save(filepath.Join(dir, StoreFileName)); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(filepath.Join(dir, StoreFileName), 4)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 2 || loaded.Dims != 4 {
		t.Fatalf("loaded mismatch: len=%d dims=%d", loaded.Len(), loaded.Dims)
	}
	// Cosine: query == 'a' should outrank 'b'.
	hits := loaded.Search([]float32{1, 0, 0, 0}, 2)
	if len(hits) != 2 || hits[0].ID != "a" {
		t.Fatalf("search: want a first, got %+v", hits)
	}
}

func TestStore_DimMismatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(4, "test")
	s.Put(Record{ID: "a", Hash: "h", Vector: []float32{1, 0, 0, 0}})
	_ = s.Save(filepath.Join(dir, StoreFileName))
	got, err := Load(filepath.Join(dir, StoreFileName), 8)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 0 {
		t.Fatalf("dim mismatch should drop records, got len=%d", got.Len())
	}
}

func TestEmbedDocument_Incremental(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{dims: 64}
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "1", Name: "Login", Kind: "function", SourceFile: "auth.go"},
		{ID: "2", Name: "Logout", Kind: "function", SourceFile: "auth.go"},
	}}
	_, r1, err := EmbedDocument(context.Background(), doc, "", dir, be)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Embedded != 2 || r1.Reused != 0 {
		t.Fatalf("first run: %+v", r1)
	}
	// Second run with no changes — all reused.
	_, r2, err := EmbedDocument(context.Background(), doc, "", dir, be)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Embedded != 0 || r2.Reused != 2 {
		t.Fatalf("second run should reuse all: %+v", r2)
	}
	// Mutate one entity's signature → only it re-embeds.
	doc.Entities[0].Signature = "func Login(token string) (*Session, error)"
	_, r3, err := EmbedDocument(context.Background(), doc, "", dir, be)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Embedded != 1 || r3.Reused != 1 {
		t.Fatalf("after one mutation: want 1 embedded 1 reused, got %+v", r3)
	}
}

func TestFakeBackend_SemanticOrdering(t *testing.T) {
	// Sanity check that the fake backend gives nonzero semantic discrimination,
	// which the MCP-level RRF test relies on.
	be := &fakeBackend{dims: 64}
	vs, err := be.Embed(context.Background(), []string{
		"authenticate user bearer token",
		"compute the sum of integers",
	})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := be.Embed(context.Background(), []string{"verify bearer token authentication"})
	dot := func(a, b []float32) float64 {
		var s float64
		for i := range a {
			s += float64(a[i]) * float64(b[i])
		}
		return s
	}
	if dot(q[0], vs[0]) <= dot(q[0], vs[1]) {
		t.Fatalf("auth query should match auth doc better than math doc")
	}
}

// TestHTTPBackend_OptIn verifies that setting GRAFEL_EMBEDDING_URL routes
// through the HTTP backend and fetches+caches embeddings (S6 / #2156).
func TestHTTPBackend_OptIn(t *testing.T) {
	const dims = 4
	// Deterministic fake vectors the mock server will return.
	fakeVecs := map[string][]float32{
		"Login function auth": {0.1, 0.2, 0.3, 0.4},
		"Logout function":     {0.5, 0.6, 0.7, 0.8},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var resp embeddingsResponse
		for i, inp := range req.Input {
			v := fakeVecs[inp]
			if v == nil {
				v = make([]float32, dims)
			}
			resp.Data = append(resp.Data, struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{Embedding: v, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvURL, srv.URL+"/v1")
	t.Setenv(EnvDims, "4")
	// Clear other env vars to test URL-only opt-in.
	for _, e := range []string{EnvBackend, EnvModel, EnvAPIKey} {
		t.Setenv(e, "")
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Backend != BackendHTTP {
		t.Fatalf("want http backend via URL opt-in, got %q", cfg.Backend)
	}

	be, err := NewBackend(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	defer be.Close()

	// Embed two texts; each should come back as a non-zero vector.
	texts := []string{"Login function auth", "Logout function"}
	vecs, err := be.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != dims {
			t.Errorf("vec[%d]: want dims=%d, got %d", i, dims, len(v))
		}
	}

	// Verify cache integration: EmbedDocument should store vectors and reuse
	// them on a second call (0 backend calls).
	dir := t.TempDir()
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "1", Name: "Login", Kind: "function", SourceFile: "auth.go"},
		{ID: "2", Name: "Logout", Kind: "function", SourceFile: "auth.go"},
	}}
	cache, err := NewCache(filepath.Join(dir, "embeddings"))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	_, r1, err := EmbedDocumentWithCache(context.Background(), doc, "", dir, be, cache)
	if err != nil {
		t.Fatalf("EmbedDocumentWithCache first run: %v", err)
	}
	if r1.Embedded != 2 {
		t.Fatalf("first run: want 2 embedded, got %+v", r1)
	}
	// Second run: same doc → all served from cache (CacheHit), no backend call.
	_, r2, err := EmbedDocumentWithCache(context.Background(), doc, "", dir, be, cache)
	if err != nil {
		t.Fatalf("EmbedDocumentWithCache second run: %v", err)
	}
	if r2.Embedded != 0 {
		t.Fatalf("second run: want 0 embedded (all cached), got %+v", r2)
	}
}

// TestConfig_URLOnlyOptin verifies that GRAFEL_EMBEDDING_URL alone (no
// GRAFEL_EMBEDDING_BACKEND) is sufficient to activate HTTP mode.
func TestConfig_URLOnlyOptin(t *testing.T) {
	t.Setenv("GRAFEL_HOME", t.TempDir())
	t.Setenv(EnvURL, "http://localhost:11434/v1")
	for _, e := range []string{EnvBackend, EnvModel, EnvAPIKey, EnvDims} {
		t.Setenv(e, "")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Backend != BackendHTTP {
		t.Fatalf("want http via URL-only opt-in, got %q", cfg.Backend)
	}
	if cfg.HTTP.URL != "http://localhost:11434/v1" {
		t.Fatalf("HTTP.URL mismatch: %q", cfg.HTTP.URL)
	}
}

// TestMixedEmbeddingState verifies that a graph.fb with some entities carrying
// embedding_ref and others without (embedding_ref="") loads cleanly when the
// embedding backend is disabled. This is the common migration scenario where
// a user indexed with embeddings enabled and then downgraded or switched to
// BM25-only mode.
func TestMixedEmbeddingState(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{dims: 8}

	// Build an initial doc with 3 entities and embed it.
	doc := &graph.Document{Entities: []graph.Entity{
		{ID: "1", Name: "Alpha", Kind: "function", SourceFile: "a.go"},
		{ID: "2", Name: "Beta", Kind: "function", SourceFile: "b.go"},
		{ID: "3", Name: "Gamma", Kind: "function", SourceFile: "c.go"},
	}}
	_, _, err := EmbedDocument(context.Background(), doc, "", dir, be)
	if err != nil {
		t.Fatalf("EmbedDocument: %v", err)
	}

	// Verify all entities got an embedding_ref stamped.
	for i, e := range doc.Entities {
		if e.EmbeddingRef == "" {
			t.Errorf("entity[%d] (%s): expected EmbeddingRef, got empty", i, e.Name)
		}
	}

	// Now load the saved store — it should contain 3 vectors.
	store, err := Load(StorePath(dir), 8)
	if err != nil {
		t.Fatalf("Load store: %v", err)
	}
	if store.Len() != 3 {
		t.Fatalf("store should have 3 vectors, got %d", store.Len())
	}

	// Simulate "new entity added, no embedding yet" by appending an entity
	// with an empty EmbeddingRef — as would happen when the daemon reindexes
	// with embeddings disabled but old entities already have refs on disk.
	doc.Entities = append(doc.Entities, graph.Entity{
		ID: "4", Name: "Delta", Kind: "function", SourceFile: "d.go",
	})

	// Load succeeds and dim mismatch (8 vs queried 8) returns all entries.
	store2, err := Load(StorePath(dir), 8)
	if err != nil {
		t.Fatalf("Load mixed store: %v", err)
	}
	// Original 3 vectors still readable; entity 4 has no vector — this is fine.
	if store2.Len() != 3 {
		t.Fatalf("mixed-state load: want 3 vectors, got %d", store2.Len())
	}
	// Search still works for embedded entities.
	hits := store2.Search(l2Normalize([]float32{1, 0, 0, 0, 0, 0, 0, 0}), 3)
	if len(hits) == 0 {
		t.Fatal("search on mixed-state store returned no hits")
	}
}
