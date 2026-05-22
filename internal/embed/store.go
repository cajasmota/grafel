package embed

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// StoreFileName is the per-repo vector sidecar living next to graph.fb in the
// store dir (~/.archigraph/store/<slug>/embeddings.bin). It is intentionally
// separate from graph.json/graph.fb to keep cold-start cost predictable
// (ADR-0006: vectors in a sidecar) and to allow loading vectors lazily.
const StoreFileName = "embeddings.bin"

// Binary format (little-endian):
//
//	magic    [4]byte  = "AGEM"
//	version  uint32   = storeFormatVersion
//	dims     uint32
//	count    uint32
//	backend  uint32 len + bytes  (backend Name(), informational)
//	records  count × {
//	    idLen   uint16; id   [idLen]byte
//	    hashLen uint16; hash [hashLen]byte
//	    vec     dims × float32
//	}
const (
	storeMagic         = "AGEM"
	storeFormatVersion = 1
)

// Record is one entity's stored embedding.
type Record struct {
	ID     string
	Hash   string // ContentHash at embed time (invalidation key)
	Vector []float32
}

// Store is an in-memory vector index for one repo, backed by embeddings.bin.
type Store struct {
	Dims    int
	Backend string
	records []Record
	byID    map[string]int // id -> index into records
}

// NewStore creates an empty store for the given dimensionality.
func NewStore(dims int, backend string) *Store {
	return &Store{Dims: dims, Backend: backend, byID: map[string]int{}}
}

// StorePath returns embeddings.bin inside the per-repo state dir.
func StorePath(stateDir string) string {
	return filepath.Join(stateDir, StoreFileName)
}

// Get returns the record for an entity ID, if present.
func (s *Store) Get(id string) (Record, bool) {
	if i, ok := s.byID[id]; ok {
		return s.records[i], true
	}
	return Record{}, false
}

// Put inserts or replaces the record for an entity.
func (s *Store) Put(r Record) {
	if i, ok := s.byID[r.ID]; ok {
		s.records[i] = r
		return
	}
	s.byID[r.ID] = len(s.records)
	s.records = append(s.records, r)
}

// Len reports the number of stored vectors.
func (s *Store) Len() int { return len(s.records) }

// Retain drops any records whose ID is not in keep — used to evict embeddings
// for entities that no longer exist after a reindex.
func (s *Store) Retain(keep map[string]bool) {
	out := s.records[:0]
	s.byID = map[string]int{}
	for _, r := range s.records {
		if keep[r.ID] {
			s.byID[r.ID] = len(out)
			out = append(out, r)
		}
	}
	s.records = out
}

// SemanticHit is a cosine-scored entity ID.
type SemanticHit struct {
	ID    string
	Score float64 // cosine similarity in [-1, 1]
}

// Search returns the top-K entities by cosine similarity to the query vector.
// Vectors are L2-normalized at write time, so a dot product equals cosine.
// Brute-force is fine for <500k entities (#461).
func (s *Store) Search(query []float32, k int) []SemanticHit {
	if len(query) != s.Dims || s.Len() == 0 {
		return nil
	}
	q := l2Normalize(query)
	hits := make([]SemanticHit, 0, s.Len())
	for i := range s.records {
		v := s.records[i].Vector
		if len(v) != s.Dims {
			continue
		}
		var dot float64
		for j := range v {
			dot += float64(v[j]) * float64(q[j])
		}
		hits = append(hits, SemanticHit{ID: s.records[i].ID, Score: dot})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// Save writes the store atomically to embeddings.bin.
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if err := s.writeTo(w); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) writeTo(w io.Writer) error {
	if _, err := w.Write([]byte(storeMagic)); err != nil {
		return err
	}
	if err := putU32(w, storeFormatVersion); err != nil {
		return err
	}
	if err := putU32(w, uint32(s.Dims)); err != nil {
		return err
	}
	if err := putU32(w, uint32(len(s.records))); err != nil {
		return err
	}
	if err := putU32(w, uint32(len(s.Backend))); err != nil {
		return err
	}
	if _, err := w.Write([]byte(s.Backend)); err != nil {
		return err
	}
	buf := make([]byte, 4)
	for i := range s.records {
		r := &s.records[i]
		if err := putStr16(w, r.ID); err != nil {
			return err
		}
		if err := putStr16(w, r.Hash); err != nil {
			return err
		}
		if len(r.Vector) != s.Dims {
			return fmt.Errorf("record %s: vector dim %d != store dim %d", r.ID, len(r.Vector), s.Dims)
		}
		for _, x := range r.Vector {
			binary.LittleEndian.PutUint32(buf, math.Float32bits(x))
			if _, err := w.Write(buf); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load reads embeddings.bin. A missing file returns an empty store (not an
// error). A dims mismatch with wantDims (when wantDims > 0) returns an empty
// store so a backend/model switch transparently triggers a full re-embed.
func Load(path string, wantDims int) (*Store, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewStore(wantDims, ""), nil
	}
	if err != nil {
		return nil, err
	}
	st, err := decode(data)
	if err != nil {
		// Corrupt/old-format sidecar: treat as empty, force re-embed.
		return NewStore(wantDims, ""), nil
	}
	if wantDims > 0 && st.Dims != wantDims {
		return NewStore(wantDims, st.Backend), nil
	}
	return st, nil
}

func decode(data []byte) (*Store, error) {
	r := &reader{b: data}
	if string(r.take(4)) != storeMagic {
		return nil, fmt.Errorf("bad magic")
	}
	if r.u32() != storeFormatVersion {
		return nil, fmt.Errorf("unsupported version")
	}
	dims := int(r.u32())
	count := int(r.u32())
	backendLen := int(r.u32())
	backend := string(r.take(backendLen))
	if r.err != nil {
		return nil, r.err
	}
	s := NewStore(dims, backend)
	for i := 0; i < count; i++ {
		id := r.str16()
		hash := r.str16()
		vec := make([]float32, dims)
		for j := 0; j < dims; j++ {
			vec[j] = math.Float32frombits(r.u32())
		}
		if r.err != nil {
			return nil, r.err
		}
		s.Put(Record{ID: id, Hash: hash, Vector: vec})
	}
	return s, nil
}

func putU32(w io.Writer, v uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func putStr16(w io.Writer, s string) error {
	if len(s) > 0xFFFF {
		return fmt.Errorf("string too long: %d", len(s))
	}
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(len(s)))
	if _, err := w.Write(b[:]); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

type reader struct {
	b   []byte
	pos int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || r.pos+n > len(r.b) {
		r.err = io.ErrUnexpectedEOF
		return nil
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *reader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) str16() string {
	b := r.take(2)
	if b == nil {
		return ""
	}
	n := int(binary.LittleEndian.Uint16(b))
	return string(r.take(n))
}
