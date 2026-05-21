# Server-Sent Events (SSE) endpoints

archigraph exposes two SSE endpoints for real-time streaming. Both are served
by the embedded dashboard server on http://127.0.0.1:47274/.

---

## `/api/index-progress` — indexer progress stream

Streams progress events for all groups currently being indexed.

```
GET /api/index-progress
GET /api/index-progress/{group}
```

### Event shape

Each event is a JSON object sent as an `event: progress` SSE line:

```json
{
  "group":      "my-group",
  "repo":       "api-server",
  "phase":      "resolve",
  "pct":        62,
  "elapsed_ms": 4210,
  "done":       false
}
```

| Field | Type | Description |
|-------|------|-------------|
| `group` | string | Group slug |
| `repo` | string | Repo slug being processed |
| `phase` | string | Current pipeline phase (`parse`, `extract`, `resolve`, `write`) |
| `pct` | int | 0–100 completion percentage within the current phase |
| `elapsed_ms` | int | Milliseconds since the rebuild started |
| `done` | bool | `true` on the final event; client should close the connection |

### Notes

- Events are emitted by the in-process pub/sub broker (`internal/progress`).
- The dashboard `IndexingProgressModal` subscribes to this stream during
  `archigraph rebuild` and shows a live progress bar.
- The `archigraph rebuild` CLI also subscribes via the broker so the terminal
  can show real-time progress.
- If no rebuild is in progress, the connection is held open but no events are
  sent until a rebuild starts.

---

## `/api/mcp-activity/stream` — MCP activity stream (Jarvis)

Streams a notification for every MCP tool call the daemon processes.

```
GET /api/mcp-activity/stream
```

### Event shape

Each event is a JSON object sent as an `event: mcp-activity` SSE line:

```json
{
  "id":           "01HX4B8GQN5CMQRK",
  "tool":         "archigraph_find",
  "group":        "my-group",
  "repo_filter":  ["api-server"],
  "elapsed_ms":   31,
  "entity_ids":   ["a1b2c3d4e5f60718", "b2c3d4e5f6071829"],
  "ts":           "2026-05-21T07:42:01.234Z"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique call ID (ULID) |
| `tool` | string | Tool name (e.g. `archigraph_find`) |
| `group` | string | Resolved group slug |
| `repo_filter` | string[] | Repos in scope for this call |
| `elapsed_ms` | int | Server-side latency |
| `entity_ids` | string[] | Entity IDs returned by the tool, if any |
| `ts` | string | RFC 3339 timestamp |

### Notes

- The MCP Activity surface (`/mcp-activity`) subscribes to this stream and
  renders a live event log.
- The Graph canvas (`/graph`) also subscribes: nodes whose IDs appear in
  `entity_ids` are highlighted ("pulsed") in real time — Jarvis-style visual
  feedback while an agent explores the graph.
- History (last 500 events) is available at `GET /api/mcp-activity/history`.

---

## Client example (curl)

```bash
# Watch indexing progress for a group
curl -N http://127.0.0.1:47274/api/index-progress/my-group

# Stream MCP activity
curl -N http://127.0.0.1:47274/api/mcp-activity/stream
```
