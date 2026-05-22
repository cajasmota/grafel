# ADR-0019: Semantic embedding search via configurable backend + RRF fusion

- **Status**: Accepted
- **Date**: 2026-05-23
- **Deciders**: Jorge Cajas
- **Related**: #460 (BM25 keyword phase 1), #461 (this), ADR-0001 (single-binary), ADR-0006 (sidecar artifacts), ADR-0016 (graph.fb)

## Context

After #460 archigraph search is BM25 keyword-only. That handles "find by name", "find by file stem", and "find by docstring keyword" well — but breaks on the common agent question shape "where do we handle X?" where X is a concept that has no token-level overlap with the code (e.g. asking about "authentication" when the function is called `verifyBearer` and the docstring uses "bearer token" and "session").

We need a second ranker that scores entities by *meaning* and a fusion strategy that combines it with BM25 without re-tuning per query.

The semantic side has three hard constraints:
- Must work zero-config on a fresh install (ADR-0001 install-and-forget).
- Must not require a network call at query time once installed.
- Must let power users plug in a code-specific encoder for SOTA quality.

## Decision

Add an optional embedding pipeline behind a `Backend` interface with three implementations:

1. **`builtin`** (default) — `knights-analytics/hugot` with the `simplego` build tag. Pure Go ONNX runtime, no CGO, runs in-process. Bundled model: `sentence-transformers/all-MiniLM-L6-v2` (384-dim, quantized weights — `model_qint8_arm64.onnx` on Apple Silicon, `model_quint8_avx2.onnx` elsewhere). One-time ~23MB download into `~/.archigraph/models/` on first use; offline thereafter.
2. **`http`** (power-user) — any OpenAI-compatible `POST {url}/v1/embeddings`. Configured via `~/.archigraph/embeddings.json` or `ARCHIGRAPH_EMBEDDING_{URL,MODEL,API_KEY,DIMS}`. Routes through Ollama / OpenAI / text-embeddings-inference / Voyage / LM Studio transparently.
3. **`disabled`** — explicit opt-out; search degrades to BM25-only (#460).

Indexer side (Pass 9, skippable via `--skip-pass=embed`):
- Embed text = `name + qualified_name + signature + docstring + head-window code snippet (≤1200 chars on UTF-8 boundary)`.
- Content-hash invalidation: SHA1(EmbeddingTextVersion + embed text). Re-embed only entities whose hash changed.
- Vectors stored in `~/.archigraph/store/<repo>/embeddings.bin` — a separate sidecar (ADR-0006), never in graph.json/graph.fb.
- L2-normalized at write time so dot product == cosine.

Query side (MCP `archigraph_find`):
- BM25 top-50 + semantic top-50, fused via Reciprocal Rank Fusion: `score = Σ 1/(60 + rank)`. No score normalization across rankers (Cormack et al., canonical formulation).
- Per-result `Source` field records `bm25` / `semantic` / `bm25+semantic` for transparency.
- Backend failure / missing sidecar / dims mismatch all degrade silently to BM25-only — the MCP server logs once and serves BM25 results.

Build matrix:
- The `builtin` backend lives behind `//go:build simplego`. Default builds get a stub that returns an actionable error pointing at the HTTP backend or a simplego rebuild. This keeps the default binary lean (~79 MB) and isolates the hugot/gomlx dep tree behind one opt-in flag (~91 MB with simplego). Release builds will move to `-tags simplego` once #461 is accepted.

## Consequences

### Positive
- Default-install zero-config semantic search works after a one-time model download.
- Power users plug in code-specific encoders without a rebuild.
- ADR-0006 (sidecar artifacts) preserved: vectors live in their own file, graph.fb cold-start cost unchanged.
- Hash-based invalidation makes reindex of unchanged repos free for the embedding pass.
- RRF gives a deterministic single ranked list with no per-query tuning knobs.

### Negative / trade-offs
- One-time ~23MB model download instead of weights compiled into the binary. The original `#461` spec assumed bundle-as-Go-bytes; hugot v0.7.3 uses a model cache directory instead. We accept the cache directory: the user experience is still zero-config (downloaded silently on first index), and bundling 23MB into every release binary doubles install size without offsetting any user friction.
- Brute-force cosine is fine to ~500k entities; beyond that we'd need ANN. Out of scope for v1.
- Two backends to maintain. The `Backend` interface is small (Embed, Dims, Name, Close) so the surface is bounded.

## Alternatives considered
- Pure HTTP backend (skip builtin). Rejected: zero-config requirement.
- Bundle weights as Go bytes via `//go:embed`. Rejected for v1: hugot v0.7.3 does not expose an in-memory model loader; deferred until upstream supports it.
- Score-normalized fusion (CombSUM / convex combination). Rejected: requires per-query calibration. RRF is parameter-free and the literature consensus.
