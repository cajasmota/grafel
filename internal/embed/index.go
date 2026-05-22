package embed

import (
	"context"
	"fmt"

	"github.com/cajasmota/archigraph/internal/graph"
)

// embedBatchSize bounds how many texts are sent to the backend per call.
const embedBatchSize = 32

// Result summarizes one EmbedDocument run for logging / stats.
type Result struct {
	Backend  string
	Dims     int
	Total    int // entities considered (embeddable)
	Embedded int // entities (re)embedded this run
	Reused   int // entities served from the existing sidecar (hash hit)
	Evicted  int // stale entities dropped from the sidecar
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
// from store whose content hash is unchanged (incremental invalidation), and
// persists the updated store to stateDir/embeddings.bin.
//
// repoRoot is used to read source snippets for the embed text. The returned
// Store is also the freshly-updated in-memory index.
func EmbedDocument(ctx context.Context, doc *graph.Document, repoRoot, stateDir string, backend Backend) (*Store, Result, error) {
	res := Result{Backend: backend.Name(), Dims: backend.Dims()}

	prev, err := Load(StorePath(stateDir), backend.Dims())
	if err != nil {
		return nil, res, fmt.Errorf("load existing embeddings: %w", err)
	}
	next := NewStore(backend.Dims(), backend.Name())
	sr := newSnippetReader(repoRoot)

	type pending struct {
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

		if old, ok := prev.Get(e.ID); ok && old.Hash == hash && len(old.Vector) == backend.Dims() {
			next.Put(old)
			res.Reused++
			continue
		}
		batch = append(batch, pending{id: e.ID, hash: hash, text: text})
		if len(batch) >= embedBatchSize {
			if err := flush(); err != nil {
				return nil, res, err
			}
		}
	}
	if err := flush(); err != nil {
		return nil, res, err
	}

	res.Evicted = prev.Len() - res.Reused
	if res.Evicted < 0 {
		res.Evicted = 0
	}

	if err := next.Save(StorePath(stateDir)); err != nil {
		return nil, res, fmt.Errorf("save embeddings: %w", err)
	}
	return next, res, nil
}
