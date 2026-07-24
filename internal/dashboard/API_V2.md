# grafel API v2 — Contract Reference

> This document is the contract for the `/api/v2/...` surface.
> Screen-building agents MUST read this before implementing any v2 handler.
> When this doc and the code disagree, fix the code — the doc is authoritative.

---

## 1. Envelope — every response

All v2 responses are JSON objects with an `ok` boolean at the root.

### Success (non-paginated)

```json
{
  "ok": true,
  "data": { ... }
}
```

### Success (paginated list)

```json
{
  "ok": true,
  "data": [ ... ],
  "pagination": {
    "limit": 50,
    "offset": 0,
    "total": 312
  }
}
```

### Error

```json
{
  "ok": false,
  "error": {
    "code": "not_found",
    "message": "group 'acme' is not registered"
  }
}
```

**`code` values (canonical):**

| Code | HTTP status | Meaning |
|---|---|---|
| `not_found` | 404 | Resource does not exist |
| `bad_request` | 400 | Malformed query or body |
| `internal_error` | 500 | Server-side failure |
| `unavailable` | 503 | Dependency (broker, queue) not wired |
| `unauthorized` | 401 | Bearer token required (when auth is enabled) |

Go constructor: `v2Err(code, message)` → use `writeV2Err(w, status, code, message)`.

---

## 2. Pagination

Paginated list endpoints accept two query params:

| Param | Default | Max | Description |
|---|---|---|---|
| `limit` | 50 | 500 | Items per page |
| `offset` | 0 | — | Zero-based item offset |

Parse with `parsePagination(r.URL.Query(), total)`.
Wrap the result slice and pagination struct with `v2Page(items, pag)`.

**Example:** `GET /api/v2/groups?limit=20&offset=40`

---

## 3. SSE streaming convention

Streaming endpoints (live progress, audit feed, etc.) use Server-Sent Events.

**Required headers** (use `setV2SSEHeaders(w)` helper):

```
Content-Type: text/event-stream
Cache-Control: no-cache, no-transform
X-Accel-Buffering: no
Connection: keep-alive
```

**Wire format** (use `writeV2SSEEvent(w, eventType, data)` helper):

```
event: <type>
data: <JSON string>

```

**Standard event lifecycle:**

| Event type | Payload | When |
|---|---|---|
| `connected` | `{"subscribed_at": <unix-ms>}` | Immediately on subscribe |
| `<domain>` | domain-specific JSON object | When data arrives |
| `heartbeat` | `{}` | Every 15 s (keep-alive) |
| `close` | `{}` | Server closes stream (shutdown / error) |

**Note:** SSE endpoints must NOT be gzip-compressed. The `withGzip` middleware in
`server.go` already excludes paths ending in `/stream` and containing
`index-progress`/`mcp-activity`. New v2 SSE endpoints MUST have their paths end
in `/stream` OR be explicitly excluded in `withGzip`.

---

## 4. Error shape reference (Go)

```go
// Non-paginated success
writeV2JSON(w, http.StatusOK, v2OK(myPayload))

// Paginated success
pag := parsePagination(r.URL.Query(), len(allItems))
end := pag.Offset + pag.Limit
if end > len(allItems) {
    end = len(allItems)
}
writeV2JSON(w, http.StatusOK, v2Page(allItems[pag.Offset:end], pag))

// Error
writeV2Err(w, http.StatusNotFound, "not_found", "group 'x' not found")
```

---

## 5. Route naming conventions

- All v2 routes begin with `/api/v2/`.
- Group-scoped routes: `/api/v2/{group}/<resource>`.
- List → `GET /api/v2/{group}/nodes` (paginated).
- Detail → `GET /api/v2/{group}/nodes/{id}`.
- Streaming → `GET /api/v2/{group}/nodes/stream` (SSE).
- Mutations → `POST /api/v2/{group}/<resource>/<action>` (follow v1 convention).

Register all v2 routes in `server.go` under the `// --- API v2 routes ---` comment block.

---

## 6. Bootstrap endpoint

`GET /api/v2/meta`

Called once on app mount by WebUI v2. Response:

```json
{
  "ok": true,
  "data": {
    "version": "1.2.3",
    "api_versions": ["v1", "v2"],
    "groups": ["acme", "infra"]
  }
}
```

- `version`: daemon build version (`version.Version`).
- `api_versions`: surfaces supported. Always `["v1", "v2"]` in this binary.
- `groups`: list of registered group slugs. Empty array when no groups exist (→ show onboarding wizard).

---

## 6a. Group surface (Landing screen)

`/api/v2/meta` returns only group **slugs**. The Landing screen needs richer
per-group data, so two dedicated endpoints back it.

### `GET /api/v2/groups` (paginated)

Rich group list for the Landing cards grid. Each item:

```json
{
  "id": "acme",
  "name": "acme",
  "repos": ["core", "web"],
  "entityCount": 1200,
  "fidelity": 1.0,        // 0..1, or null when never indexed
  "indexedAt": 1716300000000, // unix-ms, or null when never indexed
  "health": "healthy"     // "healthy" | "warning" | "unindexed"
}
```

`health` + `fidelity` are derived server-side in `deriveGroupHealth`
(`v2_groups.go`) so the rules live in one place: a group with no entities and
no last-indexed time is `unindexed` (fidelity null); otherwise it is indexed.
**Note:** the daemon does not yet persist a real per-group fidelity score, so
indexed groups currently report `fidelity: 1.0` / `health: "healthy"`. When a
real score lands it slots straight into `deriveGroupHealth` with no wire change.

### `POST /api/v2/groups`

Creates an **empty** group (fleet.json + registry entry) from a name. Body:
`{ "name": "<slug>" }`. Returns `201` with the created group in a `v2OK`
envelope. Repo discovery / indexing (the full wizard) is **out of scope** for
this endpoint; the created group reports `health: "unindexed"`.

---

## 6b. Graph surface (Graph screen)

`GET /api/v2/graph/{group}`

Returns the full dependency graph for the WebUI v2 hero surface, wrapped in a
`v2OK` envelope:

```jsonc
{ "ok": true, "data": {
  "nodes": [{ "id", "label", "kind", "repo", "degree", "pagerank",
              "community_id?", "source_file?" }],
  "edges": [{ "source", "target", "kind" }],
  "communities": [{ "id", "label", "repo", "size", "color_index" }],
  "repos": [{ "id", "language", "color_index" }],
  "total_node_count": 1234
}}
```

Query params (mirror the v1 `/api/graph` handler): `repos=slug1,slug2`,
`filter_kind=`, `include_external=true`, `view=modules`, and
`lod=overview|normal|full` (`low|mid|high` remain accepted aliases). Capped LoD
requests select their connected node set through compact integer adjacency and
only then materialise response edges. The server therefore does not allocate a
full wire-edge payload merely to discard most of it for overview or normal
rendering.

This is a NEW endpoint, not a reuse of v1 `/api/graph/{group}`. Rationale: the
v1 tier-1 payload deliberately omits `pagerank` + `source_file` to keep its wire
shape tight, but the cosmos.gl renderer needs both (node sizing + the "module"
group-by). A clean v2 endpoint keeps the two UIs independent. Like v1, it shares
the server-side payload cache + strong ETag/304 + mux-level gzip (cache keys are
namespaced with a `v2:` prefix). Pre-serialised payloads are also persisted as
immutable, checksummed artifacts under
`~/.grafel/cache/dashboard/v1/<group-hash>/<source-hash>/`. The source hash
covers the group config, each repo's active graph generation (single-file or
segment-set), description and flow sidecars, the group algorithm overlay and
the cross-repo links file. A matching artifact can therefore be served after a
daemon restart without first materialising the graph; any watched source
change naturally selects a new source hash. Corrupt or unknown cache files are
treated as misses and rebuilt asynchronously after the live response is
generated. Retention is bounded to eight source versions per group and 64
parameter variants per source version. Entity detail for the inspector still
uses the v1 `GET /api/graph/{group}/entity/{id}` (unchanged, raw JSON).

---

## 6c. Action endpoints + async-job convention (#1512)

The Operations + Settings screens trigger CLI-equivalent mutating actions.
Every such endpoint is a thin REST wrapper over the SAME internal function the
corresponding `grafel` CLI command calls — no logic is duplicated.

### Async jobs (rebuild / reset)

Indexing is long-running and MUST NOT block the HTTP handler — the daemon has
to keep serving reads while a rebuild runs (the #1487 serving-mutex invariant).
So rebuild/reset endpoints are **asynchronous**:

1. The handler validates the group, fires the daemon `Rebuild` RPC in a
   background goroutine (same path as the CLI), and returns **`202 Accepted`**
   immediately with a job id.
2. Clients poll **`GET /api/v2/jobs/{id}`** or subscribe to the SSE feed
   **`GET /api/v2/jobs/{id}/stream`** to track status.

**202 ack body** (`v2OK` envelope):

```json
{ "ok": true, "data": {
  "job_id": "aj-1716300000000000000",
  "op": "rebuild",                   // "rebuild" | "reset"
  "group": "acme",
  "repo": "core",                    // omitted for whole-group rebuild
  "status": "queued",
  "progress_token": "web-1716300000000",
  "status_url": "/api/v2/jobs/aj-...",
  "stream_url": "/api/v2/jobs/aj-.../stream"
}}
```

**Job shape** (`GET /api/v2/jobs/{id}`):

```json
{ "ok": true, "data": {
  "id": "aj-...", "op": "rebuild", "group": "acme", "repo": "core",
  "status": "running",               // queued | running | done | failed
  "progress": 5,                     // 0..100, coarse
  "message": "indexing started",
  "error": "",
  "progress_token": "web-...",
  "queued_at": 1716300000000,        // unix-ms
  "started_at": 1716300000050,
  "finished_at": null
}}
```

Jobs are tracked in an in-memory, TTL-pruned registry (finished jobs drop after
30 min) so a long-lived daemon never accumulates unbounded state. Live per-file
indexing progress is still on the v1 `/api/index-progress/{group}` SSE stream,
keyed on `progress_token`.

**Job SSE** (`GET /api/v2/jobs/{id}/stream`) follows §3: `connected` →
`job` (one per status transition) → `heartbeat` every 15 s → `close` once the
job reaches a terminal state. The path ends in `/stream` so `withGzip` excludes
it automatically.

### Action endpoint reference

| Method + path | Wraps | Notes |
|---|---|---|
| `POST /api/v2/groups/{group}/rebuild` | `grafel rebuild <group>` | async → 202 + job id |
| `POST /api/v2/groups/{group}/repos/{repo}/rebuild` | `grafel rebuild <group> <repo>` | async → 202 + job id |
| `POST /api/v2/groups/{group}/repos/{repo}/reset` | `grafel rebuild --wipe` | async → 202 + job id (destructive) |
| `GET /api/v2/jobs/{id}` | — | job status/progress |
| `GET /api/v2/jobs/{id}/stream` | — | job SSE feed |
| `PATCH /api/v2/groups/{group}/repos/{repo}/monorepo` | module selection | persists to fleet.json **and** triggers a watcher `ForceRescan` so the running daemon re-reconciles (no longer persist-only). Reports `watcher_reloaded`. |
| `POST /api/v2/maintenance/cleanup` | `grafel cleanup` | body `{"dry_run":true}` (default) previews orphaned registry entries; `false` removes them |
| `POST /api/v2/update/apply` | `grafel update` | runs the updater as a subprocess (so this daemon is not replaced mid-request); returns `{exit_code, output[], applied}`. Version check stays at `GET /api/updates/check`. |
| `POST /api/v2/patterns/{group}/export` | `grafel patterns export` | body `{"file"}` or `{"repo"}` → writes approved patterns to CLAUDE.md |
| `POST /api/v2/patterns/{group}/gc` | `grafel patterns gc` | body `{"dry_run":true}` (default) previews; `false` prunes decayed candidates |

---

## 10. Intentionally CLI-only (no REST wrapper)

**Daemon install / uninstall** (`grafel daemon install|uninstall`) are NOT
exposed over REST. They register/unregister a launchd service (macOS) /
systemd unit (Linux), which requires elevated privileges and filesystem changes
outside the daemon's own process — there is no safe in-process REST trigger
(the daemon would be modifying the very service definition that supervises it).
These remain CLI-only by design; the Settings screen surfaces them as a CLI
hint rather than a button.

---

## 7. Adding a new v2 endpoint — checklist

- [ ] Handler file named `v2_<surface>.go`.
- [ ] Test file named `v2_<surface>_test.go`.
- [ ] Route registered in `server.go` `routes()` under the `// --- API v2 routes ---` comment block.
- [ ] Response uses `v2OK`, `v2Page`, or `writeV2Err` (never raw `writeJSON` / `writeErr`).
- [ ] Paginated list? Use `parsePagination(r.URL.Query(), total)`.
- [ ] Streaming? Path ends in `/stream`; call `setV2SSEHeaders(w)` and `writeV2SSEEvent(w, ...)`.
- [ ] This doc updated if a new pattern is introduced.

---

## 8. Helper reference (internal/dashboard/v2_helpers.go)

| Helper | Signature | Purpose |
|---|---|---|
| `v2OK` | `(data any) v2Envelope` | Wrap data in success envelope |
| `v2Err` | `(code, message string) v2ErrEnvelope` | Build error envelope |
| `v2Page` | `(data any, p V2Pagination) v2PageEnvelope` | Paginated success envelope |
| `parsePagination` | `(q url.Values, total int) V2Pagination` | Parse limit/offset from query |
| `writeV2JSON` | `(w, status, v)` | Write JSON with v2 Content-Type |
| `writeV2Err` | `(w, status, code, message)` | Write error response |
| `writeV2SSEEvent` | `(w, eventType, data string)` | Write one SSE event block (no flush) |
| `setV2SSEHeaders` | `(w)` | Set SSE response headers |
