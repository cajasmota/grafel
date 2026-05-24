package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cajasmota/archigraph/internal/embed"
	"github.com/cajasmota/archigraph/internal/graph"
)

// writeEmbeddings is Pass 9: build the per-repo semantic vector sidecar
// (embeddings.bin) using the user-configured embedding backend. The pass is
// non-fatal — any error degrades archigraph search to BM25-only on this
// repo; subsequent reindexes will retry. See #461 / ADR-0019.
//
// PH8 (#2100): a shared content-hash cache (~/.archigraph/embeddings/) is
// opened before computing. Entities whose body text has already been embedded
// on any ref/branch reuse the cached vector rather than calling the backend.
func writeEmbeddings(doc *graph.Document, repoRoot, stateDir string) error {
	cfg, cfgErr := embed.LoadConfig()
	if cfgErr != nil {
		// Bad config file: log and proceed with the resolved defaults so
		// indexing is never blocked by an embeddings.json typo.
		fmt.Fprintf(os.Stderr, "archigraph: embeddings config: %v\n", cfgErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	backend, err := embed.NewBackend(ctx, cfg)
	if err != nil {
		if errors.Is(err, embed.ErrDisabled) {
			// Explicit opt-out: stay silent and skip.
			return nil
		}
		return fmt.Errorf("init backend (%s): %w", cfg.Backend, err)
	}
	defer backend.Close()

	// PH8: open the cross-ref content-hash cache. Failure here is non-fatal:
	// we fall back to per-ref sidecar behaviour (same as pre-PH8).
	cache, cacheErr := embed.DefaultCache()
	if cacheErr != nil {
		fmt.Fprintf(os.Stderr, "archigraph: embed cache unavailable: %v (continuing without cross-ref dedup)\n", cacheErr)
	}

	t0 := time.Now()
	_, res, err := embed.EmbedDocumentWithCache(ctx, doc, repoRoot, stateDir, backend, cache)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"archigraph: embeddings backend=%s dims=%d total=%d embedded=%d reused=%d cache_hit=%d evicted=%d took=%s\n",
		res.Backend, res.Dims, res.Total, res.Embedded, res.Reused, res.CacheHit, res.Evicted, time.Since(t0).Round(time.Millisecond))
	return nil
}
