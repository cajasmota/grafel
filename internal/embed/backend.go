package embed

import (
	"context"
	"fmt"
)

// Backend produces dense vector embeddings for text. Implementations must be
// safe for sequential use; the indexer and MCP query path call Embed with
// batches. All backends in archigraph return L2-normalized vectors so that a
// dot product equals cosine similarity.
type Backend interface {
	// Embed returns one vector per input text, each of length Dims().
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dims is the embedding dimensionality.
	Dims() int
	// Name is a short identifier used for logging and the vector-store header.
	Name() string
	// Close releases any held resources (model sessions, etc.).
	Close() error
}

// ErrDisabled is returned by NewBackend when embeddings are turned off. The
// MCP server treats this as a soft signal to fall back to BM25-only search.
var ErrDisabled = fmt.Errorf("embeddings disabled")

// NewBackend constructs the embedding backend selected by cfg. A "disabled"
// backend returns (nil, ErrDisabled). The "builtin" backend is only available
// when the binary is built with the `simplego` tag; otherwise it returns a
// clear error pointing the user at the HTTP backend or a simplego build.
func NewBackend(ctx context.Context, cfg Config) (Backend, error) {
	switch cfg.Backend {
	case BackendDisabled:
		return nil, ErrDisabled
	case BackendHTTP:
		return newHTTPBackend(cfg.HTTP)
	case BackendBuiltin:
		return newBuiltinBackend(ctx)
	default:
		return nil, fmt.Errorf("unknown embedding backend %q", cfg.Backend)
	}
}
