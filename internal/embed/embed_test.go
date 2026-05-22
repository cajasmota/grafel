package embed

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
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
	t.Setenv("ARCHIGRAPH_HOME", t.TempDir())
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

func TestConfig_DefaultIsBuiltin(t *testing.T) {
	t.Setenv("ARCHIGRAPH_HOME", t.TempDir())
	for _, e := range []string{EnvBackend, EnvURL, EnvModel, EnvAPIKey, EnvDims} {
		t.Setenv(e, "")
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.Backend != BackendBuiltin {
		t.Fatalf("want builtin, got %+v err=%v", cfg, err)
	}
}

func TestEmbedTextAndHashStability(t *testing.T) {
	e := &graph.Entity{Name: "Login", QualifiedName: "auth.Login", Properties: map[string]string{"docstring": "Verify bearer token and create session"}}
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
