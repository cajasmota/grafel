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

	t0 := time.Now()
	_, res, err := embed.EmbedDocument(ctx, doc, repoRoot, stateDir, backend)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"archigraph: embeddings backend=%s dims=%d total=%d embedded=%d reused=%d evicted=%d took=%s\n",
		res.Backend, res.Dims, res.Total, res.Embedded, res.Reused, res.Evicted, time.Since(t0).Round(time.Millisecond))
	return nil
}
