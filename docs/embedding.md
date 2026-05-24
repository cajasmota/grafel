# Semantic Embeddings

> **Default since S6 (#2156):** embeddings are **disabled**. BM25 keyword
> search works out of the box with no configuration. To add semantic (vector)
> search, follow the opt-in instructions below.

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

## Disabling embeddings explicitly

To confirm BM25-only mode (e.g. to override a config file):

```bash
export ARCHIGRAPH_EMBEDDING_BACKEND=disabled
```

---

## Migration: upgrading from a pre-S6 install

Versions before S6 defaulted to `backend: builtin`. After upgrading:

1. **If you had embeddings working** and want to keep them, add the env var or
   config file shown above. The existing per-repo `embeddings.bin` sidecars and
   cross-ref cache (`~/.archigraph/embeddings/`) are fully compatible — the
   daemon will reuse cached vectors on the next reindex and only fetch new ones
   from the backend.

2. **If embeddings were broken** (non-simplego build) or you don't need them,
   no action is needed. BM25 search works without changes and the `embedding_ref`
   field in `graph.fb` simply stays empty.

3. The `~/.archigraph/embeddings.json` config file is optional. Delete it to
   reset to the disabled default.

---

## Environment variable reference

| Variable                        | Default    | Description                              |
|---------------------------------|------------|------------------------------------------|
| `ARCHIGRAPH_EMBEDDING_URL`      | _(unset)_  | HTTP endpoint; sets `backend=http` automatically |
| `ARCHIGRAPH_EMBEDDING_BACKEND`  | `disabled` | `disabled` / `http` / `builtin`          |
| `ARCHIGRAPH_EMBEDDING_MODEL`    | _(unset)_  | Model name sent in the request body      |
| `ARCHIGRAPH_EMBEDDING_API_KEY`  | _(unset)_  | Bearer token for authenticated endpoints |
| `ARCHIGRAPH_EMBEDDING_DIMS`     | `384`      | Vector dimensionality (HTTP backend)     |
| `ARCHIGRAPH_EMBEDDING_TTL_DAYS` | `30`       | Cross-ref cache eviction window          |
