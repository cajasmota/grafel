package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/cajasmota/archigraph/internal/embed"
)

// queryEmbedder lazily initialises a process-wide embedding backend used to
// embed search queries at MCP request time. It is intentionally separate from
// the indexer's backend (which may run in a different process): both sides
// agree on which backend to use via the user's embeddings.json + env. When
// dims mismatch, the per-repo store load drops the index, so the worst case
// is a silent fallback to BM25-only.
type queryEmbedder struct {
	once    sync.Once
	backend embed.Backend
	disable bool
	initErr error
}

var globalQueryEmbedder = &queryEmbedder{}

// embedQuery returns the embedding of a single query string, or (nil, false)
// when no embedding backend is available — callers must then fall back to
// BM25-only.
func embedQuery(ctx context.Context, q string) ([]float32, bool) {
	be, ok := globalQueryEmbedder.get(ctx)
	if !ok {
		return nil, false
	}
	vecs, err := be.Embed(ctx, []string{q})
	if err != nil || len(vecs) == 0 {
		// Log once-ish to stderr for visibility; do not fail the call.
		fmt.Fprintf(os.Stderr, "archigraph: query embed failed (%v); falling back to BM25-only\n", err)
		return nil, false
	}
	return vecs[0], true
}

func (qe *queryEmbedder) get(ctx context.Context) (embed.Backend, bool) {
	qe.once.Do(func() {
		cfg, err := embed.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "archigraph: embedding config: %v\n", err)
		}
		if cfg.Backend == embed.BackendDisabled {
			qe.disable = true
			return
		}
		be, berr := embed.NewBackend(ctx, cfg)
		if berr != nil {
			if berr == embed.ErrDisabled {
				qe.disable = true
				return
			}
			qe.initErr = berr
			fmt.Fprintf(os.Stderr, "archigraph: embedding backend init failed: %v (falling back to BM25-only)\n", berr)
			return
		}
		qe.backend = be
	})
	return qe.backend, qe.backend != nil && !qe.disable
}
