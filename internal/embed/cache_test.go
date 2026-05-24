package embed

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// makeVec returns a deterministic float32 slice for testing.
func makeVec(seed int, dims int) []float32 {
	r := rand.New(rand.NewSource(int64(seed)))
	v := make([]float32, dims)
	for i := range v {
		v[i] = r.Float32()
	}
	return v
}

func TestCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	hash := "abcdef1234567890abcdef1234567890abcdef12"
	vec := makeVec(42, 384)

	// Miss before Put.
	if got, ok := c.Get(hash); ok {
		t.Fatalf("expected cache miss, got vec len=%d", len(got))
	}

	// Put then Get.
	if err := c.Put(hash, vec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(hash)
	if !ok {
		t.Fatal("expected cache hit after Put")
	}
	if len(got) != len(vec) {
		t.Fatalf("len mismatch: want %d got %d", len(vec), len(got))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("element %d mismatch: want %f got %f", i, vec[i], got[i])
		}
	}
}

func TestCacheShardLayout(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	hash := "ff00aabbccddeeff00aabbccddeeff00aabbccdd"
	if err := c.Put(hash, makeVec(1, 8)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// File should be at <dir>/ff/00aabbccddeeff00aabbccddeeff00aabbccdd.vec
	expected := filepath.Join(dir, "ff", "00aabbccddeeff00aabbccddeeff00aabbccdd.vec")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("shard file not found at %s: %v", expected, err)
	}
}

func TestCacheConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	hash := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	vec := makeVec(99, 384)

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = c.Put(hash, vec)
		}()
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: Put error: %v", i, e)
		}
	}

	// Verify the written file is not corrupt.
	got, ok := c.Get(hash)
	if !ok {
		t.Fatal("cache miss after concurrent writes")
	}
	if len(got) != len(vec) {
		t.Fatalf("corrupt vec after concurrent writes: got len %d want %d", len(got), len(vec))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("corrupt element %d after concurrent writes", i)
		}
	}
}

func TestCacheSweep(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	// Write three hashes.
	hashes := []string{
		"aabbccddeeff00112233445566778899aabbccdd",
		"1122334455667788990011223344556677889900",
		"ffeeddccbbaa00112233445566778899ffeeddcc",
	}
	for i, h := range hashes {
		if err := c.Put(h, makeVec(i, 8)); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	// Back-date the first two so they look old.
	past := time.Now().AddDate(0, 0, -40) // 40 days ago
	for _, h := range hashes[:2] {
		p := c.vecPath(h)
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	// activeHashes: only the third is active.
	active := map[string]bool{hashes[2]: true}

	removed, err := c.Sweep(active, 30)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 2 {
		t.Fatalf("Sweep removed %d entries, want 2", removed)
	}

	// Verify: first two gone, third still present.
	for _, h := range hashes[:2] {
		if _, ok := c.Get(h); ok {
			t.Errorf("hash %s should have been swept", h)
		}
	}
	if _, ok := c.Get(hashes[2]); !ok {
		t.Errorf("active hash %s should still be present", hashes[2])
	}
}

func TestCacheSweepKeepsYoungInactive(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	hash := "cafe0000cafe0000cafe0000cafe0000cafe0000"
	if err := c.Put(hash, makeVec(5, 8)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// mtime is "now" — within TTL window even if not active.
	removed, sweepErr := c.Sweep(map[string]bool{}, 30)
	if sweepErr != nil {
		t.Fatalf("Sweep: %v", sweepErr)
	}
	if removed != 0 {
		t.Fatalf("young inactive entry should NOT be swept, got removed=%d", removed)
	}
	if _, ok := c.Get(hash); !ok {
		t.Error("young inactive entry should still be readable")
	}
}

func TestCacheMultipleHashes(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	const n = 100
	vecs := make([][]float32, n)
	hashes := make([]string, n)
	for i := 0; i < n; i++ {
		vecs[i] = makeVec(i, 16)
		hashes[i] = fmt.Sprintf("%040x", i)
		if err := c.Put(hashes[i], vecs[i]); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		got, ok := c.Get(hashes[i])
		if !ok {
			t.Fatalf("miss for hash %d", i)
		}
		for j := range vecs[i] {
			if got[j] != vecs[i][j] {
				t.Fatalf("hash %d element %d mismatch", i, j)
			}
		}
	}
}

func TestBodyHashSHA256(t *testing.T) {
	h1 := BodyHashSHA256("hello")
	h2 := BodyHashSHA256("hello")
	if h1 != h2 {
		t.Errorf("non-deterministic hash: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(h1))
	}
	if h1 == BodyHashSHA256("world") {
		t.Error("different inputs should produce different hashes")
	}
}
