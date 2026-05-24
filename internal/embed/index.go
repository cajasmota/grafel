package embed

import (
	"context"
	"fmt"
	"log"

	"github.com/cajasmota/archigraph/internal/graph"
)

// embedBatchSize bounds how many texts are sent to the backend per call.
const embedBatchSize = 32

// Result summarizes one EmbedDocument run for logging / stats.
type Result struct {
	Backend     string
	Dims        int
	Total       int // entities considered (embeddable)
	Embedded    int // entities (re)embedded this run
	Reused      int // entities served from the existing per-ref sidecar (hash hit)
	CacheHit    int // entities served from the cross-ref content-hash cache (PH8)
	Evicted     int // stale entities dropped from the sidecar
}

// embeddable reports whether an entity is worth embedding. We skip container
// shadows and zero-line nodes which carry no code snippet and pollute recall.
func embeddable(e *graph.Entity) bool {
	switch e.Kind {
	case "file", "module", "directory", "package_dir":
		return false
	}
	return e.Name != ""
}

// EmbedDocument (re)embeds the entities in doc using backend, reusing vectors
// from the cross-ref content-hash cache (PH8) and then from the per-ref
// sidecar (existing behaviour). Only entities with no cache entry are sent
// to the backend.
//
// The function also records the embedding_ref on each entity in doc so the
// fbwriter can persist the hash pointer in graph.fb.
//
// repoRoot is used to read source snippets for the embed text. The returned
// Store is the freshly-updated in-memory per-ref index.
func EmbedDocument(ctx context.Context, doc *graph.Document, repoRoot, stateDir string, backend Backend) (*Store, Result, error) {
	return EmbedDocumentWithCache(ctx, doc, repoRoot, stateDir, backend, nil)
}

// EmbedDocumentWithCache is the PH8-aware variant. cache may be nil, in which
// case behaviour is identical to the pre-PH8 EmbedDocument.
func EmbedDocumentWithCache(ctx context.Context, doc *graph.Document, repoRoot, stateDir string, backend Backend, cache *Cache) (*Store, Result, error) {
	res := Result{Backend: backend.Name(), Dims: backend.Dims()}

	prev, err := Load(StorePath(stateDir), backend.Dims())
	if err != nil {
		return nil, res, fmt.Errorf("load existing embeddings: %w", err)
	}
	next := NewStore(backend.Dims(), backend.Name())
	sr := newSnippetReader(repoRoot)

	type pending struct {
		idx  int    // index into doc.Entities
		id   string
		hash string
		text string
	}
	var batch []pending
	live := map[string]bool{}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		texts := make([]string, len(batch))
		for i, p := range batch {
			texts[i] = p.text
		}
		vecs, err := backend.Embed(ctx, texts)
		if err != nil {
			return err
		}
		if len(vecs) != len(batch) {
			return fmt.Errorf("backend returned %d vectors for %d inputs", len(vecs), len(batch))
		}
		for i, p := range batch {
			next.Put(Record{ID: p.id, Hash: p.hash, Vector: vecs[i]})
			res.Embedded++
			// Store into the cross-ref cache so future branch switches reuse it.
			if cache != nil {
				if putErr := cache.Put(p.hash, vecs[i]); putErr != nil {
					// Non-fatal: cache write failure degrades to no dedup, not
					// to a broken index.
					log.Printf("embed cache: put %s: %v", p.hash, putErr)
				}
			}
			// Stamp the entity with its embedding_ref for persistence in graph.fb.
			doc.Entities[p.idx].EmbeddingRef = p.hash
		}
		batch = batch[:0]
		return nil
	}

	for i := range doc.Entities {
		e := &doc.Entities[i]
		if !embeddable(e) {
			continue
		}
		res.Total++
		live[e.ID] = true

		text := EmbedText(e, sr.snippet(e))
		hash := ContentHash(text)

		// 1. Per-ref sidecar hit (unchanged since last reindex of this ref).
		if old, ok := prev.Get(e.ID); ok && old.Hash == hash && len(old.Vector) == backend.Dims() {
			next.Put(old)
			e.EmbeddingRef = hash
			res.Reused++
			continue
		}

		// 2. Cross-ref cache hit (PH8): same body text exists from another
		//    branch or ref — skip backend call entirely.
		if cache != nil {
			if vec, ok := cache.Get(hash); ok && len(vec) == backend.Dims() {
				next.Put(Record{ID: e.ID, Hash: hash, Vector: vec})
				e.EmbeddingRef = hash
				res.CacheHit++
				res.Reused++ // counts as a reuse for backward-compat logging
				continue
			}
		}

		// 3. Must compute — enqueue for batch backend call.
		batch = append(batch, pending{idx: i, id: e.ID, hash: hash, text: text})
		if len(batch) >= embedBatchSize {
			if err := flush(); err != nil {
				return nil, res, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, res, err
	}

	res.Evicted = prev.Len() - (res.Reused - res.CacheHit)
	if res.Evicted < 0 {
		res.Evicted = 0
	}

	if err := next.Save(StorePath(stateDir)); err != nil {
		return nil, res, fmt.Errorf("save embeddings: %w", err)
	}
	return next, res, nil
}
