# Semantic Embeddings

> **Default since S6 (#2156):** embeddings use **bundled MiniLM** (via
> `-tags simplego` builds). Semantic (vector) search works out of the box with
> no configuration. Power users can opt into an HTTP endpoint or opt out
> entirely — see below.

---

## What are embeddings for?

Semantic embeddings power the vector half of the Reciprocal Rank Fusion (RRF)
search in the MCP server. With embeddings enabled, a query like
`"handles authentication"` will surface functions that implement auth logic
even if they don't literally contain the word "authentication". Without
embeddings, BM25 keyword search still works well for exact-name and
import-path queries.

---

## Opt-in: HTTP backend (recommended)

Point archigraph at any **OpenAI-compatible `/v1/embeddings` endpoint**.
Popular options:

| Server        | Example URL                                  |
|---------------|----------------------------------------------|
| Ollama        | `http://localhost:11434/v1`                  |
| LM Studio     | `http://localhost:1234/v1`                   |
| OpenAI        | `https://api.openai.com/v1`                  |
| Hugging Face  | `https://api-inference.huggingface.co/models/<model>/pipeline/feature-extraction` |

### Environment variable (quickest)

```bash
export ARCHIGRAPH_EMBEDDING_URL=http://localhost:11434/v1
# Optional: pick a specific model (default: no model field sent)
export ARCHIGRAPH_EMBEDDING_MODEL=nomic-embed-text
# Optional: tell archigraph the vector size if non-standard
export ARCHIGRAPH_EMBEDDING_DIMS=768
```

### Config file (persistent)

Create `~/.archigraph/embeddings.json`:

```json
{
  "backend": "http",
  "http": {
    "url": "http://localhost:11434/v1",
    "model": "nomic-embed-text",
    "dims": 768
  }
}
```

The config file is read at daemon start. After editing it, restart the daemon
(`archigraph stop && archigraph start`) or run `archigraph index` manually to
pick up the new config and re-embed.

---

## Opt-in: bundled MiniLM (simplego build)

If you built archigraph with `-tags simplego`, the **all-MiniLM-L6-v2** model
(384 dims) is available as an in-process backend. Activate it with:

```bash
export ARCHIGRAPH_EMBEDDING_BACKEND=builtin
```

or in `~/.archigraph/embeddings.json`:

```json
{ "backend": "builtin" }
```

The model weights (~23 MB) are downloaded from HuggingFace on first use into
`~/.archigraph/models/`. Subsequent runs are fully offline.

> **Note:** Standard release binaries do **not** include `-tags simplego`.
> The HTTP backend is the recommended path for most users.

---

## Opt-out: disable embeddings

To use BM25-only mode and skip embeddings entirely:

```bash
export ARCHIGRAPH_EMBEDDING_DISABLE=true
```

or in `~/.archigraph/embeddings.json`:

```json
{ "backend": "disabled" }
```

The `ARCHIGRAPH_EMBEDDING_DISABLE` env var always takes precedence, even if
other settings are configured.

---

## Migration: no breaking changes

S6 / #2156 restored the bundled MiniLM default. Users upgrading from older
versions with embeddings already cached will see no changes — the daemon
reuses existing per-repo `embeddings.bin` sidecars and cross-ref cache
(`~/.archigraph/embeddings/`).

If you prefer BM25-only search:

```bash
export ARCHIGRAPH_EMBEDDING_DISABLE=true
```

This disables all embedding operations while keeping the daemon and search
fully functional.

---

## Environment variable reference

| Variable                        | Default    | Description                              |
|---------------------------------|------------|------------------------------------------|
| `ARCHIGRAPH_EMBEDDING_DISABLE`  | _(unset)_  | Set to `true`/`1` to force BM25-only mode (overrides all other settings) |
| `ARCHIGRAPH_EMBEDDING_URL`      | _(unset)_  | HTTP endpoint; sets `backend=http` automatically |
| `ARCHIGRAPH_EMBEDDING_BACKEND`  | `builtin`  | `builtin` / `http` / `disabled`          |
| `ARCHIGRAPH_EMBEDDING_MODEL`    | _(unset)_  | Model name sent in the request body      |
| `ARCHIGRAPH_EMBEDDING_API_KEY`  | _(unset)_  | Bearer token for authenticated endpoints |
| `ARCHIGRAPH_EMBEDDING_DIMS`     | `384`      | Vector dimensionality (HTTP backend)     |
| `ARCHIGRAPH_EMBEDDING_TTL_DAYS` | `30`       | Cross-ref cache eviction window          |
