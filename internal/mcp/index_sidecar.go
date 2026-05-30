package mcp

// index_sidecar.go — persistence of the derived traversal indexes to disk so a
// (re)load reconstructs them from a compact binary blob instead of re-deriving
// them from the graph Document (#3368, the last mile after the lazy/debounce
// work in #3370).
//
// Background: the lazy getters in state.go (getAdjacency/getCallsAdj/getStepAdj/
// getTopKPageRank) still rebuild from lr.Doc on the FIRST use after every reload
// (and on every cold-tier load). This file lets the indexer persist a sidecar
// (graph-indexes.bin) next to graph.fb so those getters CAN reconstruct on first
// use instead of scanning Doc.Relationships.
//
// MEASURED OUTCOME (important): with #3370 already making builds lazy and with
// PageRank precomputed at index time, reconstructing these four indexes from the
// sidecar is ~1.5x SLOWER than rebuilding them from the resident Document — the
// Doc is already structured in RAM, and the sidecar adds file I/O + identity hash
// + varint decode that build-from-Doc avoids (see the BenchmarkFirstUse_* pair).
// So consumption is OFF by default (gated by ARCHIGRAPH_MCP_USE_INDEX_SIDECAR in
// state.go); the WRITE side ships so the artifact exists for external tooling and
// so a future faster reconstruction (e.g. an mmap'd index FlatBuffer) can flip
// the default without re-plumbing the indexer. The getter integration + identity
// validation + equality round-trip are all tested regardless of the gate.
//
// FORMAT — why a hand-rolled int-indexed binary, NOT gob:
//
//	The build-from-Doc path is already fast because the Document is RESIDENT in
//	RAM at reload time: every edge's endpoint string (r.FromID/r.ToID) is reused
//	by value — building adjacency is just map inserts of existing string headers,
//	zero new string bytes. A naive string-keyed sidecar (gob or otherwise) MUST
//	materialise a fresh copy of every endpoint string on decode, so it allocates
//	far MORE than build-from-Doc and loses (measured: gob decode ~2x slower than
//	build for a 20k-entity graph).
//
//	To actually beat build-from-Doc the sidecar stores ENTITY INDICES (int32 into
//	Doc.Entities), not strings. On load we rehydrate each index back to
//	doc.Entities[i].ID — reusing the resident Document's string backing exactly
//	like the build path does — so reconstruction allocates only the slices/maps,
//	never the strings. This is strictly cheaper than build-from-Doc (no O(R)
//	relationship scan, no edgeWeight property parsing, no PageRank re-sort) while
//	allocating the same string-free shape.
//
// IDENTITY: the sidecar is stamped with the SHA-256 of the graph.fb bytes it was
// derived from AND the entity-count/relationship-count. On load the getter
// recomputes the graph.fb hash and rejects any mismatch (stale / partially
// written / changed graph) — falling back to the intact build-from-Doc path.
// Because the payload is index-based, it is ONLY valid against a Doc whose
// entity/relationship ordering matches the one it was written from; the hash of
// the canonical graph.fb (written from the SAME sorted Doc) is exactly that
// identity, so a hash match guarantees index validity.

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cajasmota/archigraph/internal/graph"
)

// Sidecar on-disk constants. Bump sidecarVersion on ANY wire-format change so a
// stale-format file is rejected (→ build-from-Doc) rather than mis-decoded.
const (
	sidecarMagic   = "AGIDX1\n" // 7 bytes; trailing \n guards against accidental text reads
	sidecarVersion = 1
	sidecarName    = "graph-indexes.bin"
)

// indexSidecarPath returns the sidecar location for a state directory (next to
// graph.fb).
func indexSidecarPath(stateDir string) string {
	return filepath.Join(stateDir, sidecarName)
}

// hashFile returns the hex SHA-256 of a file's bytes, or "" + error.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// --- binary primitives ------------------------------------------------------

func putUvarint(w *bufio.Writer, v uint64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	_, _ = w.Write(buf[:n])
}

func putVarint(w *bufio.Writer, v int64) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], v)
	_, _ = w.Write(buf[:n])
}

func putF64(w *bufio.Writer, f float64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
	_, _ = w.Write(buf[:])
}

// atoiSafe parses a base-10 int, returning 0 on any error (matches the
// best-effort parse buildStepAdjacency does for step_index).
func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// --- WRITE ------------------------------------------------------------------

// WriteIndexSidecar builds the derived indexes for doc and writes the validated
// sidecar next to graph.fb in stateDir. Indexer write hook: call it AFTER
// graph.fb has been written so the identity hash matches the bytes on disk.
//
// repoTag is the slug stamped onto entities (passed to buildAdjacency for parity
// with the load-time getter). On any failure it returns an error; the indexer
// logs it non-fatally — a missing/failed sidecar only costs the lazy getters
// their original build-from-Doc path, never correctness.
func WriteIndexSidecar(stateDir, repoTag string, doc *graph.Document) error {
	if doc == nil {
		return fmt.Errorf("sidecar: nil document")
	}
	fbPath := filepath.Join(stateDir, "graph.fb")
	fbHash, err := hashFile(fbPath)
	if err != nil {
		return fmt.Errorf("sidecar: hash graph.fb: %w", err)
	}

	// Entity ID → dense index, so endpoint strings serialize as int32 indices.
	idIdx := make(map[string]int32, len(doc.Entities))
	for i := range doc.Entities {
		idIdx[doc.Entities[i].ID] = int32(i)
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("sidecar mkdir: %w", err)
	}
	out := indexSidecarPath(stateDir)
	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("sidecar create tmp: %w", err)
	}
	w := bufio.NewWriterSize(f, 1<<16)

	// Header: magic, version, hash, counts. Counts let the loader pre-size and
	// cross-check against the resident Doc.
	_, _ = w.WriteString(sidecarMagic)
	putUvarint(w, sidecarVersion)
	putUvarint(w, uint64(len(fbHash)))
	_, _ = w.WriteString(fbHash)
	putUvarint(w, uint64(len(doc.Entities)))
	putUvarint(w, uint64(len(doc.Relationships)))

	// Adjacency: emit per-source out-edges and in-edges as index lists. We write
	// the FULL forward/back adjacency by streaming relationships once — the
	// loader buckets them. This mirrors buildAdjacency exactly (every rel yields
	// one out-edge from FromID and one in-edge into ToID).
	//
	// Per relationship we store: fromIdx, toIdx, kindID, weight, relIdx. Kinds
	// are interned into a small table so each edge stores a uvarint kind id.
	kindTable := []string{}
	kindIdx := map[string]int{}
	internKind := func(k string) int {
		if id, ok := kindIdx[k]; ok {
			return id
		}
		id := len(kindTable)
		kindTable = append(kindTable, k)
		kindIdx[k] = id
		return id
	}
	// First pass: build the edge records in memory so we can write the kind
	// table up front (loader needs it before edges).
	type rec struct {
		from, to int32
		kind     int
		weight   float64
		relIdx   int
	}
	recs := make([]rec, 0, len(doc.Relationships))
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		fi, ok1 := idIdx[r.FromID]
		ti, ok2 := idIdx[r.ToID]
		if !ok1 || !ok2 {
			// Endpoint not in entity table (dangling edge). Skip — buildAdjacency
			// keeps these as string keys, but they are unreachable via entity
			// traversal anyway and rare; the hash still validates the graph. To
			// stay byte-equal with build we instead fall back: mark sidecar
			// invalid by returning an error so the loader rebuilds from Doc.
			_ = w.Flush()
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("sidecar: dangling edge endpoint (from=%q to=%q); skipping persist", r.FromID, r.ToID)
		}
		recs = append(recs, rec{from: fi, to: ti, kind: internKind(r.Kind), weight: edgeWeight(r), relIdx: i})
	}

	// Kind table.
	putUvarint(w, uint64(len(kindTable)))
	for _, k := range kindTable {
		putUvarint(w, uint64(len(k)))
		_, _ = w.WriteString(k)
	}
	// Edge records.
	putUvarint(w, uint64(len(recs)))
	for _, e := range recs {
		putVarint(w, int64(e.from))
		putVarint(w, int64(e.to))
		putUvarint(w, uint64(e.kind))
		putF64(w, e.weight)
		putVarint(w, int64(e.relIdx))
	}

	// TopKPageRank: list of entity indices (resolved from the same idIdx).
	topK := buildTopKPageRank(doc, 64)
	putUvarint(w, uint64(len(topK)))
	for _, id := range topK {
		// topK ids always come from Doc.Entities, so they resolve.
		putVarint(w, int64(idIdx[id]))
	}

	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sidecar flush: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("sidecar close: %w", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("sidecar rename: %w", err)
	}
	return nil
}

// --- READ -------------------------------------------------------------------

// loadedSidecar is the in-memory reconstruction of a validated sidecar, with
// every endpoint string rehydrated from the RESIDENT Document (string-reuse, no
// fresh allocation) — exactly the shape the build-from-Doc getters produce.
type loadedSidecar struct {
	adj          *adjacency
	callsAdj     map[string][]string
	stepAdj      map[string][]stepEdge
	topKPageRank []string
}

// loadValidSidecar reads + validates the sidecar in stateDir against the current
// graph.fb identity and reconstructs the indexes using doc for string reuse. It
// returns (nil, nil) — NOT an error — for the common absent / stale / mismatch
// cases so the caller treats them as a plain cache miss and falls back to
// build-from-Doc. A non-nil error is reserved for genuinely unexpected failures;
// the caller still falls back in that case.
//
// doc MUST be the resident Document for this repo (the one whose canonical
// graph.fb produced the validated hash). Endpoint indices are resolved against
// doc.Entities; a count mismatch is treated as a stale miss.
func loadValidSidecar(stateDir string, doc *graph.Document) (*loadedSidecar, error) {
	if doc == nil {
		return nil, nil
	}
	scPath := indexSidecarPath(stateDir)
	f, err := os.Open(scPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no sidecar — cache miss
		}
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)

	// Header.
	magic := make([]byte, len(sidecarMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != sidecarMagic {
		return nil, nil // wrong/old format — miss
	}
	ver, err := binary.ReadUvarint(r)
	if err != nil || ver != sidecarVersion {
		return nil, nil
	}
	hlen, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}
	hbuf := make([]byte, hlen)
	if _, err := io.ReadFull(r, hbuf); err != nil {
		return nil, nil
	}
	storedHash := string(hbuf)
	nEnt, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}
	nRel, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}

	// Validate identity against the current graph.fb AND the resident Doc shape.
	curHash, herr := hashFile(filepath.Join(stateDir, "graph.fb"))
	if herr != nil {
		return nil, herr
	}
	if curHash != storedHash {
		return nil, nil // stale: graph.fb changed since the sidecar was written
	}
	if int(nEnt) != len(doc.Entities) || int(nRel) > len(doc.Relationships) {
		return nil, nil // Doc shape diverged from the sidecar — miss
	}

	// resolveID maps an entity index back to the resident Doc's ID string,
	// reusing its backing array (no allocation).
	resolveID := func(i int64) (string, bool) {
		if i < 0 || int(i) >= len(doc.Entities) {
			return "", false
		}
		return doc.Entities[i].ID, true
	}

	// Kind table.
	nk, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}
	kinds := make([]string, nk)
	for i := range kinds {
		kl, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, nil
		}
		kb := make([]byte, kl)
		if _, err := io.ReadFull(r, kb); err != nil {
			return nil, nil
		}
		kinds[i] = string(kb)
	}

	// Edge records → adjacency + CALLS adjacency.
	ne, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}
	adj := &adjacency{
		out: make(map[string][]edge, len(doc.Entities)),
		in:  make(map[string][]edge, len(doc.Entities)),
	}
	callsAdj := make(map[string][]string)
	stepAdj := make(map[string][]stepEdge)
	for i := uint64(0); i < ne; i++ {
		from, e1 := binary.ReadVarint(r)
		to, e2 := binary.ReadVarint(r)
		kid, e3 := binary.ReadUvarint(r)
		wbuf := make([]byte, 8)
		_, e4 := io.ReadFull(r, wbuf)
		relIdx, e5 := binary.ReadVarint(r)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || int(kid) >= len(kinds) {
			return nil, nil
		}
		fromID, ok1 := resolveID(from)
		toID, ok2 := resolveID(to)
		if !ok1 || !ok2 {
			return nil, nil
		}
		weight := math.Float64frombits(binary.LittleEndian.Uint64(wbuf))
		kind := kinds[kid]
		adj.out[fromID] = append(adj.out[fromID], edge{target: toID, kind: kind, weight: weight, relIdx: int(relIdx)})
		adj.in[toID] = append(adj.in[toID], edge{target: fromID, kind: kind, weight: weight, relIdx: int(relIdx)})
		switch kind {
		case "CALLS":
			callsAdj[fromID] = append(callsAdj[fromID], toID)
		case stepInProcessEdge:
			// step_index lives in Doc.Relationships[relIdx].Properties — read it
			// from the resident Doc (cheaper than persisting it again).
			idx := 0
			if relIdx >= 0 && int(relIdx) < len(doc.Relationships) {
				if p := doc.Relationships[relIdx].Properties; p != nil {
					idx = atoiSafe(p["step_index"])
				}
			}
			stepAdj[fromID] = append(stepAdj[fromID], stepEdge{toID: toID, idx: idx})
		}
	}
	// CALLS targets are pre-sorted by buildCallsAdjacency; match that ordering.
	for k := range callsAdj {
		sortStrings(callsAdj[k])
	}

	// TopKPageRank.
	ntk, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, nil
	}
	topK := make([]string, 0, ntk)
	for i := uint64(0); i < ntk; i++ {
		v, verr := binary.ReadVarint(r)
		if verr != nil {
			return nil, nil
		}
		id, ok := resolveID(v)
		if !ok {
			return nil, nil
		}
		topK = append(topK, id)
	}

	return &loadedSidecar{adj: adj, callsAdj: callsAdj, stepAdj: stepAdj, topKPageRank: topK}, nil
}
