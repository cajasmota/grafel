# archigraph MCP — Performance & Token-Overhead Strategy

**Date:** 2026-05-26
**Inputs:** quality benchmark, daemon RPC logs
**Scope:** MCP server, query handlers, response serializers, bridge transport. Extractor/linker correctness is **out-of-scope** (parallel agent).

---

## Headline numbers being targeted

| Dimension | Today | Target (after structural fixes) |
|---|---|---|
| Per-call wall time (median) | **7–9 s** | < 250 ms p50 / < 1.5 s p99 |
| Total tokens vs grep+read (10 questions) | **+44.6%** (136 554 vs 94 411) | **−40%** vs grep+read |
| Tool calls per cross-stack trace | 5–7 | 2–3 |

Two of these are independent: tokens come from **response shape**, latency from a few **algorithmic hotspots** and the **bridge serialization chain**. Fixing tokens does not fix latency, and vice-versa. Both must ship.

---

## 1. Latency teardown

### 1.1 Where the 7–9s/call actually goes

The daemon RPC log measures only the in-handler portion. Anything between the bridge stdin and that line — plus serialization back through multiple JSON encoders — is invisible. Empirically the in-handler portion of light tools should be tens of ms on a 19k-entity graph; the rest of the budget is being eaten elsewhere.

The dominant suspects, in descending expected impact:

#### **L1. Per-call full-relationship scans inside handlers** *(high impact)*
The fixture group has ~19 409 entities and tens of thousands of relationships. Several handlers iterate the full relationship list on every call.

- `internal/mcp/tools.go:578-588`: for each of up-to-10 top hits, the code runs a linear scan looking for edges between visible nodes. That is `O(top_hits × |R|)` per find call — on this corpus ~250–500k iterations per call.
- `internal/mcp/tools.go:867`: linear scan of all relationships on **every** inspect to find specific edge types. The Adjacency index is already built but not used here.
- `internal/mcp/tools.go:617`: scans **every entity in every repo** to find the highest fallback whenever a query returns 0 hits.

**Fix:** Use the already-built `Adjacency` index for these scans. For specific edge-type lookups, build a specialized index at reload time alongside `Adjacency`. Expected: **−2–4 s p99 on find/inspect** on graphs of this size.

#### **L2. O(N²) token-budget loops in `renderCompact`** *(medium impact)*
The renderer has a loop that re-encodes a growing slice from scratch on each iteration to estimate whether adding the next item would exceed a token budget.

**Fix:** Maintain a running byte count; encode incrementally; binary-search the cutoff if needed. Similar pattern in `capByRenderedBytes` — it marshals on every binary-search midpoint. Expected: **−200–500 ms** on any tool returning > 50 rows.

#### **L3. `archigraph_get_source` reads from line 1 every time** *(medium impact)*
Source code is read line-by-line from byte 0 until the desired window is reached. On a 3 000-line file this reads ~3 000 lines just to emit 60 of them. Plus a per-call semaphore that can be a serialization bottleneck under concurrent calls.

**Fix:** Build a small line-offset cache at index time (byte offset for every 100 lines), then start scanning from the nearest checkpoint. For typical entity windows this cuts I/O by ~10–50×. Expected: **−100–400 ms per get_source on large files**.

#### **L4. Bridge transport serializes ALL calls per session** *(structural, high impact under chaining)*
A single shared client between bridge and daemon means **chained tool calls cannot pipeline** — and cross-stack traces in the bench made 5–7 sequential calls. If each call has a 1-second floor, a 7-call chain still costs 7 s wall time.

**Fix (structural):** Allow multiplexed calls per client. At minimum, expose a `archigraph_batch(calls=[...])` server-side macro that resolves a chain in one round-trip — would collapse multi-call traces from 7 calls to 1. Expected: **−40–60% wall on cross-stack questions**.

#### **L5. Per-call JSON encode/decode round-trip × 3** *(medium impact)*
For every call the response is: handler builds a map → first marshal to text → parser injects metadata by re-parsing to map → second marshal → RPC encoding, the bridge decodes, then re-encodes as JSON-RPC 2.0 to stdout. Up to **5 round-trips** of marshal/unmarshal per call, on payloads that can reach 1.3 MiB.

**Fix:** Have handlers return a map directly, inject metadata as a key set on that map, and marshal **once** at the wire boundary. Expected: **−50–200 ms per call**, more on large payloads.

### 1.2 Latency-fix ranking

| # | Fix | Effort | Expected p50 cut | Expected p99 cut |
|---|---|---|---|---|
| L1 | Adjacency-index hot scans (find/inspect/fallback) | 1–2 days | −30% | −40% |
| L4 | `archigraph_batch` macro + drop redundant encoding | 2–3 days | −20% chains | −60% chains |
| L5 | Single-marshal response path | 1 day | −15% | −20% |
| L2 | Drop O(N²) budget loops | < 1 day | −10% | −25% |
| L3 | Line-offset cache for `get_source` | 1 day | −10% on get_source | −30% on large files |

---

## 2. Token teardown

### 2.1 Representative payloads + bloat sources

#### **T1. Endpoint tool ships terse AND verbose simultaneously**
Cited in cross-repo HTTP questions. Current endpoint definitions handler emits:

- Full structure array (even in terse mode)
- ALSO emits string array with same data
- Plus 7 envelope keys

At 473 definitions the bench logged 473 full definition objects **and** 473 string line representations. Estimated ~22 kB of literal duplication on a single call.

**Smaller shape:** In terse mode, ship strings only — drop full objects entirely (or move it behind `format=full`). Drop envelope keys that duplicate request context. Expected: **−40% on this tool's payload**, applied across ~3 tool calls in the bench.

#### **T2. Cluster query returns every cluster's full entity slice**
Cited in architecture overview question. Current handler emits `{repo, id, size, modularity, top_entities}` for **all 34** Louvain communities. `top_entities` is typically 10 entity records each — so ~340 entity records on a single call when the agent only needed the top 5–8 to label subsystems.

**Smaller shape:** Default `top_entities_limit=3`, `min_size=20`. Add `cluster_id` argument for drill-down. Expected: **−60% on this tool**.

#### **T3. Find query default returns BFS-expanded neighbors**
Current handler BFS-expands depth=3 from each top-10 hit by default. For a single-symbol question the agent only wanted the entity itself. Even when the BFS produces few visible edges, the cost is paid in node serialization.

**Smaller shape:** Default `depth=0` (the question text steers the user; if they want neighbors they pass `depth=2`). Default `max_results=10`, not 50. Expected: **−25% on find** when not chained.

#### **T4. Inspect query always emits metadata fields**
Current handler returns multiple envelope keys (qualified_name, kind, findings, graph_meta, cwd_ref_meta) even in non-verbose mode. The git-metadata blocks repeat across every inspect call in a session — pure waste once the agent has them.

**Smaller shape:** Skip empty findings. Move graph_meta/cwd_ref_meta to a once-per-session `archigraph_whoami` response only — strip from inspect entirely. Expected: **−15–20% per inspect call**.

#### **T5. ID-interning helps; it should be the default**
ID-interning (#1740) replaces repeated IDs with `@1`, `@2`, … There's an opt-out but the bench transcript shows full 28-char prefixed IDs everywhere — either interning is off by default or only kicks in when an id occurs N+ times. With IDs routinely appearing 5–10 times per response, this is a big lever.

**Smaller shape:** Set interning threshold to 2 (currently presumably 3+). Apply across the entire envelope. Expected: **−10–15% on multi-entity payloads**.

#### **T6. Truncation notes are prose**
Multiple tools produce free-text truncation notes of 80–150 chars. Replace with structured fields `{"truncated": true, "omitted": N, "max_results": M}`. Expected: **−2–5% sitewide**.

### 2.2 Token-fix ranking

| # | Fix | Effort | Expected token cut (workload-weighted) |
|---|---|---|---|
| T1 | Endpoint tool: drop double-emission | < 1 day | −12% session |
| T2 | Clusters: default top_entities_limit=3 | < 1 day | −8% session |
| T3 | Find: default depth=0, max_results=10 | < 1 day | −10% session |
| T4 | Inspect: drop graph_meta/cwd_ref_meta from per-call | < 1 day | −5% session |
| T5 | ID-interning threshold=2 by default | < 1 day | −7% session |
| T6 | Replace prose truncation notes with structured fields | < 1 day | −3% session |

Cumulative model: roughly **−35 to −45% session tokens** if all six ship.

---

## 3. Quick-win / structural split

### Quick wins (1 week, no schema break)

1. **Adjacency-index the per-call relationship scans** (L1) — touches three handlers, zero wire changes.
2. **Single-marshal response path** (L5) — refactor response wrapping and metadata injection to operate on maps, not serialized text.
3. **Default-on aggressive ID-interning** (T5).
4. **Drop O(N²) budget enforcement loops** (L2, T1, T2, T3 all share the same fix pattern).
5. **Trim envelope keys** (T4, T6) — additive defaults, can ship behind `verbose=true`.

### Structural (2–4 weeks, schema/transport changes)

1. **`archigraph_batch` macro** (L4) — collapses chains into one round-trip. Highest single lever on cross-stack questions.
2. **Line-offset cache for `get_source`** (L3) — needs sidecar generated at index time. Coordinate with indexer team.
3. **Response-shape v2**: rename to definitions/lines mutually exclusive, qualified_name opt-in, deprecate envelope metadata duplication. Versioned via `archigraph_whoami.api_version`.
4. **Replace RPC serialization with a multiplexed transport** so the bridge does not serialize chained calls (L4 root cause).

---

## 4. Verification plan

1. **Headline metric**: re-run the benchmark after each numbered fix lands; track `tokens_total` and `wall_time_ms` from the report's telemetry table.
2. **Per-tool latency**: enable daemon RPC logging; aggregate daemon logs into a per-tool p50/p99 table.
3. **Wire-size**: add a debug envelope key so the bench harness can compare wire sizes without re-tokenizing.
4. **Regression gate**: a CI script that fails when the MCP/grep+read token ratio exceeds 1.10 (today: 1.45). Below 1.0 = MCP genuinely saves tokens.
5. **Latency budget**: budget per call ≤ 250 ms p50 / 1.5 s p99. Anything above triggers profile capture.
6. **Cumulative-impact ledger**: each PR records measured impact so the team can see whether the projected −45% materializes.

---

## 5. Risks / unknowns

- **The 7–9s estimate is the agent's wall-clock impression** — it conflates handler time, bridge serialization, and Claude Code's own per-tool round-trip overhead. Step zero of the latency work should be to capture the daemon's own RPC log during a fresh bench run and split actual handler time from transport time. If handler time turns out to be < 500 ms p50, all the L1–L3 fixes still help but L4 (transport) becomes the dominant lever.
- **ID-interning percentages** assume the bench transcript faithfully reflects the wire-format. If the host strips ID tables before logging, T5 may already be partly active.
- **No flame graph was captured** (Go source-reading only). The ranked impact estimates are bounded by inspection of hot loops + corpus size, not profiling.

---

## File-line evidence summary

| Fix | File:line (abbreviated) |
|---|---|
| L1 find-relationship scan | `tools.go:578-588` |
| L1 inspect edge-type scan | `tools.go:867-892` |
| L1 fallback scan | `tools.go:613-628` |
| L2 renderCompact O(N²) | `render.go:85-91, 114` |
| L2 capByRenderedBytes O(N log N) | `endpoint_tools.go:387-405` |
| L3 sequential line scan | `read_source_unix.go:122-135` |
| L4 single-flight RPC | `mcp_bridge.go:168-202` |
| L5 reparse-to-inject metadata | `server.go:794-808, 816-889` |
| T1 tool double-emit | `endpoint_tools.go:301-321` |
| T2 cluster always-full array | `tools.go:1180-1190` |
| T3 find defaults | `tools.go:376-398, 565-588` |
| T4 inspect envelope bloat | `tools.go:750-760, 822-855` |
| T5 ID-interning gating | `server.go:800-804`, `id_interning.go` |
| T6 prose truncation notes | `tools.go:517-521`, `flow_tools.go:315-318`, `endpoint_tools.go:314-317` |
