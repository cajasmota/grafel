// Package embed — cross-ref content-hash vector cache (PH8 of #2087).
//
// Cache deduplicates embedding computation across branches and worktrees by
// storing one raw float32 vector per unique entity body hash. Layout:
//
//	<rootDir>/embeddings/<first-2-hex>/<rest-of-hash>.vec
//
// Sharding into 256 sub-directories avoids large directory entry counts for
// repos with many unique entity bodies.
//
// Storage estimate (MiniLM, 384 dims, float32 = 4B):
//
//	1 entity body → 384 × 4 = 1 536 bytes per .vec file
//	50 000 unique bodies → ~75 MB
//
// Dedup win: N entities across M branches with identical bodies → ~unique-body-count
// files on disk, not N×M.
//
// Key design decisions:
//   - SHA-256 body hashes (from embed.ContentHash, which already uses SHA-1 —
//     the cache uses the same string key so no rehashing is needed).
//   - Atomic writes (tmp + rename) prevent partial reads.
//   - Concurrent writers for the same hash are safe: last write wins on the
//     rename, all writers compute the same vector so no corruption.
//   - No in-memory lock map needed: the OS atomically resolves the rename.
package embed

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvEmbeddingTTLDays is the environment variable controlling how long an
// unused cache entry survives before Sweep removes it.
const EnvEmbeddingTTLDays = "ARCHIGRAPH_EMBEDDING_TTL_DAYS"

// defaultTTLDays is the TTL used when ARCHIGRAPH_EMBEDDING_TTL_DAYS is unset.
const defaultTTLDays = 30

// Cache is a file-backed, content-hash keyed vector store shared across all
// repos and refs on this machine. It is concurrency-safe: multiple indexer
// goroutines may call Get/Put simultaneously; concurrent writers for the same
// hash are harmless because all compute identical vectors.
type Cache struct {
	rootDir string // e.g. ~/.archigraph/embeddings
}

// NewCache creates (if necessary) and returns a Cache rooted at rootDir.
// rootDir is typically ~/.archigraph/embeddings, derived from embed.homeDir().
func NewCache(rootDir string) (*Cache, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("embed cache: mkdir %s: %w", rootDir, err)
	}
	return &Cache{rootDir: rootDir}, nil
}

// DefaultCache returns a Cache rooted at the canonical location
// (<ARCHIGRAPH_HOME or ~/.archigraph>/embeddings).
func DefaultCache() (*Cache, error) {
	return NewCache(filepath.Join(homeDir(), "embeddings"))
}

// vecPath returns the shard path for a body hash.
//
// The body hash produced by ContentHash is a 40-hex SHA-1 string.
// We use the first 2 hex chars as a shard prefix for directory partitioning.
// For a 64-char SHA-256 we use the same first-2 convention for uniformity.
func (c *Cache) vecPath(bodyHash string) string {
	if len(bodyHash) < 4 {
		// Fallback for very short hashes (tests, unexpected input).
		return filepath.Join(c.rootDir, "misc", bodyHash+".vec")
	}
	shard := bodyHash[:2]
	rest := bodyHash[2:]
	return filepath.Join(c.rootDir, shard, rest+".vec")
}

// Get retrieves the float32 vector for the given body hash.
// Returns (nil, false) on cache miss or read error.
func (c *Cache) Get(bodyHash string) (vec []float32, ok bool) {
	p := c.vecPath(bodyHash)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	v, err := decodeVec(data)
	if err != nil {
		// Corrupt file — treat as miss; the next Put will overwrite it.
		return nil, false
	}
	return v, true
}

// Put writes the float32 vector for the given body hash atomically.
// Concurrent calls for the same hash are safe: each write uses a unique
// temp filename (via os.CreateTemp) so goroutines do not race on the tmp
// file. All writers compute the same vector; last rename wins.
func (c *Cache) Put(bodyHash string, vec []float32) error {
	p := c.vecPath(bodyHash)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("embed cache: mkdir shard: %w", err)
	}
	data := encodeVec(vec)
	// Use os.CreateTemp for a unique tmp name — safe under concurrent writers.
	f, err := os.CreateTemp(dir, "*.vec.tmp")
	if err != nil {
		return fmt.Errorf("embed cache: create tmp: %w", err)
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("embed cache: write tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("embed cache: close tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("embed cache: rename: %w", err)
	}
	return nil
}

// Sweep removes .vec files whose hash is NOT in activeHashes and whose mtime
// is older than ttlDays days (0 means use ARCHIGRAPH_EMBEDDING_TTL_DAYS or
// defaultTTLDays).
//
// It returns the number of files removed and the first non-permission error
// encountered (missing-file errors are ignored, as concurrent cleanup is safe).
func (c *Cache) Sweep(activeHashes map[string]bool, ttlDays int) (removed int, err error) {
	if ttlDays <= 0 {
		ttlDays = resolveTTLDays()
	}
	cutoff := time.Now().AddDate(0, 0, -ttlDays)

	walkErr := filepath.WalkDir(c.rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable dirs
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".vec") {
			return nil
		}
		// Reconstruct hash from path: shard/<2hex> + rest.
		rel, relErr := filepath.Rel(c.rootDir, path)
		if relErr != nil {
			return nil
		}
		// rel is like "ab/cdef...hash.vec"
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if len(parts) != 2 {
			return nil
		}
		shard := parts[0]
		base := strings.TrimSuffix(parts[1], ".vec")
		hash := shard + base

		if activeHashes[hash] {
			return nil // active — keep
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil // young enough — keep even if inactive
		}
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			if err == nil {
				err = rmErr
			}
			return nil
		}
		removed++
		return nil
	})
	if walkErr != nil && err == nil {
		err = walkErr
	}
	return removed, err
}

// BodyHashSHA256 computes a SHA-256 hex hash of an arbitrary string.
// Use this when you need a 64-char hash rather than the 40-char SHA-1
// produced by ContentHash. The cache accepts both formats.
func BodyHashSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// --- wire encoding ---------------------------------------------------------

// encodeVec serialises a float32 slice as raw little-endian bytes.
// 4 bytes per element, no header needed because the file path IS the key.
func encodeVec(vec []float32) []byte {
	out := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

// decodeVec deserialises raw little-endian bytes into a float32 slice.
func decodeVec(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("embed cache: vec file length %d not a multiple of 4", len(data))
	}
	vec := make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec, nil
}

// resolveTTLDays reads ARCHIGRAPH_EMBEDDING_TTL_DAYS or returns defaultTTLDays.
func resolveTTLDays() int {
	if v := os.Getenv(EnvEmbeddingTTLDays); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultTTLDays
}
