//go:build !simplego

package embed

import (
	"context"
	"fmt"
)

// newBuiltinBackend is the default-build stub. The pure-Go MiniLM backend
// pulls in the hugot/gomlx ONNX runtime, which we keep behind the `simplego`
// build tag so the standard binary stays lean and free of that dependency
// tree (ADR-0001 / ADR-0019). When the builtin backend is requested in a
// non-simplego build we return an actionable error rather than silently
// degrading — the caller (MCP server) maps this to a BM25-only fallback and
// logs the reason.
func newBuiltinBackend(_ context.Context) (Backend, error) {
	return nil, fmt.Errorf(
		"builtin embedding backend not compiled in: rebuild with `-tags simplego`, "+
			"or set %s to an OpenAI-compatible endpoint (e.g. Ollama), "+
			"or set backend:disabled for BM25-only search", EnvURL)
}

// BuiltinCompiledIn reports whether the pure-Go MiniLM backend is available
// in this build. Used for diagnostics (e.g. `archigraph doctor`).
func BuiltinCompiledIn() bool { return false }
