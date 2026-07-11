// v2_graph_stream.go — GET /api/v2/graph/{group}/stream
//
// Streaming counterpart to handleV2Graph (v2_graph.go). After indexing a large
// group the full-payload endpoint serialises the WHOLE graph before the browser
// can render anything, so a multi-second wait reads as "broken". Rendering is
// done by Cosmos (cosmos.gl, GPU) which is fine with big graphs — the bottleneck
// is *data delivery*. This endpoint streams the same node/edge shape
// progressively so a later frontend increment can feed Cosmos as data arrives.
//
// Increment 1 of epic #5446 — BACKEND ONLY. The existing /api/v2/graph/{group}
// full-payload endpoint is left untouched; the frontend can switch to this
// stream later with no data-model change because the per-node / per-edge JSON
// is byte-for-byte the same shape buildV2Graph produces (v2GraphNode /
// v2GraphEdge).
//
// ── Format: SSE (not NDJSON) ─────────────────────────────────────────────────
//
// The dashboard already has a documented SSE convention (API_V2.md §3:
// setV2SSEHeaders / writeV2SSEEvent, `connected`→domain→`close` lifecycle) used
// by index-progress, mcp-activity, audit and job feeds. The withGzip middleware
// in server.go already excludes any path ending in `/stream` from compression
// (so chunks actually flush). Choosing SSE over NDJSON keeps this endpoint
// consistent with the rest of the v2 streaming surface and reuses the existing
// helpers + gzip exclusion — no new infra. The path therefore ends in `/stream`.
//
// ── Event lifecycle ──────────────────────────────────────────────────────────
//
//	event: connected   data: {"subscribed_at": <unix-ms>}
//	event: meta        data: {"total_nodes":N,"total_edges":M,"communities":[…],"repos":[…]}
//	event: chunk       data: {"nodes":[…],"edges":[…]}        (repeated, important-first)
//	event: done        data: {"done":true}
//
// `meta` is always first (the frontend's progress counter reads total_nodes /
// total_edges); `chunk`s follow important-first; `done` is always last.
//
// ── Important-first ordering ─────────────────────────────────────────────────
//
// Nodes are streamed highest-importance first so the most structurally central
// part of the graph paints immediately. Importance is taken from the group-algo
// overlay when present: PageRank (centrality / god-nodes) descending, ties
// broken by served degree. When no overlay has run (every PageRank == 0) it
// falls back to served degree descending — pure connectivity. A node's edges
// are emitted in the first chunk by which BOTH of its endpoints have already
// been streamed, so the frontend never references a node it has not seen.
//
// ── No force-load ────────────────────────────────────────────────────────────
//
// Streams only from the already-warm DashGroup cache (GetGroupCachedForRef,
// which never loads from disk or recomputes algorithms). A cold group returns
// 503 `unavailable` — the same not-loaded signal other best-effort handlers use
// — and kicks off a background warm; the frontend handles warm-up separately.

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// graphStreamChunkSize is the number of nodes emitted per `chunk` event. Sized
// so each flush is a meaningful render step without being chatty; edges that
// become deliverable at a given chunk ride along in that chunk.
const graphStreamChunkSize = 750

// v2GraphStreamMeta is the first (`meta`) event payload. It carries the totals
// the frontend's progress counter needs plus the legend/filter metadata
// (communities + repos) so those panels can render before the node chunks land.
type v2GraphStreamMeta struct {
	TotalNodes  int                `json:"total_nodes"`
	TotalEdges  int                `json:"total_edges"`
	Communities []v2GraphCommunity `json:"communities"`
	Repos       []v2GraphRepo      `json:"repos"`
}

// v2GraphStreamChunk is a `chunk` event payload: a batch of nodes plus every
// edge that became deliverable (both endpoints already streamed) by this batch.
// Nodes/edges are the SAME shape as the full-payload endpoint.
type v2GraphStreamChunk struct {
	Nodes []v2GraphNode `json:"nodes"`
	Edges []v2GraphEdge `json:"edges"`
}

// handleV2GraphStream — GET /api/v2/graph/{group}/stream
//
// SSE stream of the v2 graph, important-first, from the warm cache only.
// Honours the same ?repos= / ?filter_kind= / ?include_external= / ?view=modules
// / ?ref= query params as handleV2Graph so the two endpoints agree on which
// nodes/edges exist. No ?lod= thinning — Cosmos handles scale on the GPU and the
// point of streaming is to deliver the full graph progressively.
func (s *Server) handleV2GraphStream(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeV2Err(w, http.StatusInternalServerError, "internal_error", "streaming unsupported")
		return
	}

	filterKind := r.URL.Query().Get("filter_kind")
	reposParam := r.URL.Query().Get("repos")
	includeExternal := r.URL.Query().Get("include_external") == "true"
	includeModules := r.URL.Query().Get("view") == "modules" ||
		r.URL.Query().Get("include") == "modules"
	refParam := r.URL.Query().Get("ref")

	// Warm-cache only — never force a disk load or algorithm recompute from a
	// stream request. A cold group returns the not-loaded signal (503) and warms
	// in the background; the frontend handles warm-up separately.
	grp, warm := s.graphs.GetGroupCachedForRef(group, refParam)
	if !warm {
		// #5722 — distinguish a genuine load FAILURE from the ordinary "still
		// warming" case. EventSource can't read a non-2xx body, so a bare 503
		// is indistinguishable from "warming" to the frontend and it retries
		// forever. When the last warm attempt for this group actually failed,
		// upgrade to SSE and emit a `connected` + distinguishable `error` event
		// carrying the failure detail instead of the opaque 503.
		if loadErr, failed := s.graphs.LastWarmError(group, refParam); failed {
			writeV2GraphStreamLoadError(w, loadErr)
			return
		}
		writeV2Err(w, http.StatusServiceUnavailable, "unavailable",
			"group not loaded; warming up — retry shortly")
		return
	}

	repos := sortedRepos(grp)
	if reposParam != "" {
		slugSet := map[string]bool{}
		for _, sl := range strings.Split(reposParam, ",") {
			slugSet[strings.TrimSpace(sl)] = true
		}
		var filtered []*DashRepo
		for _, rp := range repos {
			if slugSet[rp.Slug] {
				filtered = append(filtered, rp)
			}
		}
		repos = filtered
	}

	// Reuse the exact full-payload build so the streamed shape is identical.
	resp := s.buildV2Graph(repos, grp, filterKind, includeExternal, includeModules)

	orderGraphStreamNodes(resp.Nodes)

	setV2SSEHeaders(w)
	w.WriteHeader(http.StatusOK)

	writeV2SSEEvent(w, "connected", fmt.Sprintf(`{"subscribed_at":%d}`, time.Now().UnixMilli()))
	flusher.Flush()

	meta := v2GraphStreamMeta{
		TotalNodes:  len(resp.Nodes),
		TotalEdges:  len(resp.Edges),
		Communities: resp.Communities,
		Repos:       resp.Repos,
	}
	writeV2SSEEvent(w, "meta", jsonString(meta))
	flusher.Flush()

	streamGraphChunks(w, flusher, resp.Nodes, resp.Edges)

	writeV2SSEEvent(w, "done", jsonString(map[string]any{"done": true}))
	flusher.Flush()
}

// orderGraphStreamNodes sorts nodes important-first in place. Overlay present
// (any non-zero PageRank): PageRank desc, ties → degree desc. No overlay
// (all PageRank == 0): degree desc. Final tie-break on ID keeps the order
// deterministic across requests.
func orderGraphStreamNodes(nodes []v2GraphNode) {
	hasOverlay := false
	for i := range nodes {
		if nodes[i].PageRank != 0 {
			hasOverlay = true
			break
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if hasOverlay && a.PageRank != b.PageRank {
			return a.PageRank > b.PageRank
		}
		if a.Degree != b.Degree {
			return a.Degree > b.Degree
		}
		return a.ID < b.ID
	})
}

// streamGraphChunks emits the ordered nodes in batches of graphStreamChunkSize,
// flushing after each. An edge rides in the first chunk by which BOTH of its
// endpoints have already been streamed, so the frontend never sees an edge that
// references a node it has not received yet. Any edge whose endpoint never
// appears in the served node set (should not happen — buildV2Graph filters
// edges to visible nodes) is skipped.
func streamGraphChunks(w http.ResponseWriter, flusher http.Flusher, nodes []v2GraphNode, edges []v2GraphEdge) {
	// chunkIndex[id] = index of the chunk in which the node is streamed.
	chunkIndex := make(map[string]int, len(nodes))
	for i := range nodes {
		chunkIndex[nodes[i].ID] = i / graphStreamChunkSize
	}

	// Bucket each edge into the chunk at which both endpoints are available =
	// max(chunk(source), chunk(target)).
	totalChunks := 0
	if len(nodes) > 0 {
		totalChunks = (len(nodes)-1)/graphStreamChunkSize + 1
	}
	edgeBuckets := make([][]v2GraphEdge, totalChunks)
	for _, e := range edges {
		cs, okS := chunkIndex[e.Source]
		ct, okT := chunkIndex[e.Target]
		if !okS || !okT {
			continue
		}
		c := cs
		if ct > c {
			c = ct
		}
		edgeBuckets[c] = append(edgeBuckets[c], e)
	}

	for c := 0; c < totalChunks; c++ {
		start := c * graphStreamChunkSize
		end := start + graphStreamChunkSize
		if end > len(nodes) {
			end = len(nodes)
		}
		chunk := v2GraphStreamChunk{
			Nodes: nodes[start:end],
			Edges: edgeBuckets[c],
		}
		// Wire contract (#4516): nil slices serialize as [] not null, so the
		// frontend always gets arrays.
		if chunk.Edges == nil {
			chunk.Edges = []v2GraphEdge{}
		}
		writeV2SSEEvent(w, "chunk", jsonString(chunk))
		flusher.Flush()
	}
}

// v2GraphStreamError is the `error` event payload emitted when a PRIOR warm
// attempt for the group actually failed (see writeV2GraphStreamLoadError).
type v2GraphStreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeV2GraphStreamLoadError upgrades the response to SSE and emits a
// `connected` event followed by a distinguishable `error` event carrying
// loadErr's detail (#5722). EventSource cannot read a non-2xx response body,
// so a genuine load failure MUST be delivered as an SSE frame (status 200)
// rather than an HTTP error status, or the frontend has no way to tell it
// apart from a transient "still warming" 503 and retries forever.
func writeV2GraphStreamLoadError(w http.ResponseWriter, loadErr error) {
	flusher, ok := w.(http.Flusher)
	setV2SSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	writeV2SSEEvent(w, "connected", fmt.Sprintf(`{"subscribed_at":%d}`, time.Now().UnixMilli()))
	if ok {
		flusher.Flush()
	}
	writeV2SSEEvent(w, "error", jsonString(v2GraphStreamError{
		Code:    "load_failed",
		Message: loadErr.Error(),
	}))
	if ok {
		flusher.Flush()
	}
}

// jsonString marshals v to a compact JSON string for an SSE data field. On the
// (practically impossible) marshal error it returns an empty object so the
// stream stays well-formed.
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
