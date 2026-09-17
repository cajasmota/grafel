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
	"compress/gzip"
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

// streamWarmDeadline bounds how long a COLD stream request blocks warming the
// group before it gives up. A bounded warm keeps the SSE connection open (with
// `warming` heartbeats) instead of returning a bare 503 that makes the browser
// eventually fall back to the uncapped, blocking full-payload blob. It is
// generous so a genuinely large cold corpus (hundreds of thousands of nodes)
// finishes warming inside it; only a truly stuck warm hits the ceiling.
//
// streamWarmHeartbeat is how often a `warming` progress event is flushed while
// the bounded warm proceeds, keeping the connection (and any intermediary
// proxies) alive so the browser stays in its "warming" state rather than
// erroring on an idle socket. Both are vars so tests can shrink the cadence.
// defaultStreamWarmDeadline is the named deadline knob (#50). With the
// non-blocking served-graph load (Fix A#1), a cold group becomes warm as soon
// as graph.fb is deserialized (Pass-4 fills in the background), so the stream
// gets data quickly and this ceiling is only reached by a genuinely stuck warm.
// It is raised well above the old 25s so a legitimately large corpus never trips
// warm_timeout during an ordinary cold start.
const defaultStreamWarmDeadline = 180 * time.Second

var (
	streamWarmDeadline  = defaultStreamWarmDeadline
	streamWarmHeartbeat = 1 * time.Second
)

// setStreamWarmTimingForTest overrides the bounded-warm cadence for tests and
// returns a restore func. Not for production use.
func setStreamWarmTimingForTest(deadline, heartbeat time.Duration) func() {
	pd, ph := streamWarmDeadline, streamWarmHeartbeat
	streamWarmDeadline, streamWarmHeartbeat = deadline, heartbeat
	return func() { streamWarmDeadline, streamWarmHeartbeat = pd, ph }
}

// warmWaitOutcome is the terminal state of waitForWarmGroup.
type warmWaitOutcome int

const (
	warmReady    warmWaitOutcome = iota // the group became warm
	warmFailed                          // a warm attempt failed (surface the error)
	warmTimedOut                        // the deadline elapsed while still warming
)

// waitForWarmGroup blocks until the group is warm, its warm attempt fails, or
// the deadline elapses — whichever comes first — invoking heartbeat once per
// poll tick while it waits (nil-safe) so the caller can keep an SSE connection
// alive. `cached` reports (grp,true) once warm; `lastErr` reports (err,true)
// once a warm attempt has genuinely failed. Polling (rather than a channel)
// keeps this a pure, dependency-free seam that unit-tests can drive with fake
// closures and a tiny tick.
func waitForWarmGroup(
	cached func() (*DashGroup, bool),
	lastErr func() (error, bool),
	heartbeat func(elapsed time.Duration),
	deadline, tick time.Duration,
) (*DashGroup, error, warmWaitOutcome) {
	if grp, ok := cached(); ok {
		return grp, nil, warmReady
	}
	if err, failed := lastErr(); failed {
		return nil, err, warmFailed
	}
	start := time.Now()
	t := time.NewTicker(tick)
	defer t.Stop()
	for range t.C {
		if grp, ok := cached(); ok {
			return grp, nil, warmReady
		}
		if err, failed := lastErr(); failed {
			return nil, err, warmFailed
		}
		elapsed := time.Since(start)
		if elapsed >= deadline {
			return nil, nil, warmTimedOut
		}
		if heartbeat != nil {
			heartbeat(elapsed)
		}
	}
	return nil, nil, warmTimedOut
}

// v2GraphStreamMeta is the first (`meta`) event payload. It carries the totals
// the frontend's progress counter needs plus the legend/filter metadata
// (communities + repos) so those panels can render before the node chunks land.
type v2GraphStreamMeta struct {
	TotalNodes     int                `json:"total_nodes"`
	TotalEdges     int                `json:"total_edges"`
	TotalNodeCount int                `json:"total_node_count"`
	TotalEdgeCount int                `json:"total_edge_count"`
	NodeTruncated  bool               `json:"node_truncated"`
	EdgeTruncated  bool               `json:"edge_truncated"`
	Limits         v2GraphLimits      `json:"limits"`
	Communities    []v2GraphCommunity `json:"communities"`
	Repos          []v2GraphRepo      `json:"repos"`
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
// Honours the same query parameters, including finite LoD limits, as
// handleV2Graph so the two endpoints agree on nodes, edges, and metadata.
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

	// #49 — gzip the SSE stream for clients that accept it. This is the exact
	// path a large cold corpus uses, and the JSON is highly repetitive (repeated
	// keys, kind strings, repo slugs) so gzip is ~8-12x here — the single biggest
	// payload win. withGzip (server.go) deliberately EXCLUDES `/stream` because a
	// naive buffering gzip breaks progressive delivery; instead we compress
	// through a FLUSH-COMPATIBLE wrapper that gzip-flushes THEN http-flushes after
	// every event, so each chunk still reaches the client promptly. Without the
	// header we behave exactly as before (uncompressed live flush). Set the
	// content-negotiation headers now, before any WriteHeader.
	if clientAcceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		sw, cleanup := newSSEGzipWriter(w, flusher)
		defer cleanup()
		w = sw
		flusher = sw
	}

	options := parseGraphRequestOptions(r)

	// Try the warm cache first (fast path: an already-loaded group streams
	// immediately). GetGroupCachedForRef also kicks a background warm when cold.
	grp, warm := s.graphs.GetGroupCachedForRef(group, options.Ref)

	// `connected` tracks whether the SSE preamble has already been written: the
	// cold-warm branch below opens the stream early so it can emit `warming`
	// heartbeats, and the common streaming path must not write it twice.
	connected := false

	if !warm {
		// #5722 — a genuine load FAILURE (not merely "still warming") is
		// distinguishable up front: surface it as a `connected`+`error` SSE
		// frame so EventSource (which cannot read a non-2xx body) sees why.
		if loadErr, failed := s.graphs.LastWarmError(group, options.Ref); failed {
			writeV2GraphStreamLoadError(w, loadErr)
			return
		}

		// Cold but warming. A bare 503 here used to make the browser retry a
		// few times, give up, and fall back to the full-payload blob — which
		// for a large cold group serialises the WHOLE graph before the first
		// byte and never returns (TTFB=0 forever). Instead, keep the SSE
		// connection OPEN and do a BOUNDED blocking warm, flushing `warming`
		// heartbeats so the client shows progress and stays connected.
		setV2SSEHeaders(w)
		w.WriteHeader(http.StatusOK)
		writeV2SSEConnected(w, flusher)
		connected = true

		emitWarming := func(elapsed time.Duration) {
			writeV2SSEEvent(w, "warming", jsonString(map[string]any{
				"warming":    true,
				"elapsed_ms": elapsed.Milliseconds(),
				"message":    "warming index…",
			}))
			flusher.Flush()
		}
		emitWarming(0) // immediate first frame so the browser paints "warming" now.

		g, loadErr, outcome := waitForWarmGroup(
			func() (*DashGroup, bool) { return s.graphs.GetGroupCachedForRef(group, options.Ref) },
			func() (error, bool) { return s.graphs.LastWarmError(group, options.Ref) },
			emitWarming,
			streamWarmDeadline, streamWarmHeartbeat,
		)
		switch outcome {
		case warmFailed:
			// A genuine failure discovered mid-warm — surface it (#5722) so the
			// client shows the real error instead of masking it.
			writeV2SSEEvent(w, "error", jsonString(v2GraphStreamError{
				Code:    "load_failed",
				Message: loadErr.Error(),
			}))
			flusher.Flush()
			return
		case warmTimedOut:
			writeV2SSEEvent(w, "error", jsonString(v2GraphStreamError{
				Code:    "warm_timeout",
				Message: "graph is taking too long to warm; retry shortly",
			}))
			flusher.Flush()
			return
		}
		grp = g
	}

	repos := sortedRepos(grp)
	if options.ReposParam != "" {
		slugSet := map[string]bool{}
		for _, sl := range strings.Split(options.ReposParam, ",") {
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

	nodeCap, edgeCap := lodLimits(options.LOD)
	resp := s.buildV2GraphWithLimits(repos, grp, options.FilterKind, options.IncludeExternal, options.IncludeModules, nodeCap, edgeCap)

	orderGraphStreamNodes(resp.Nodes)

	// The cold-warm branch already opened the stream (headers + `connected`) so
	// it could emit `warming` heartbeats; only the warm fast path writes them
	// here.
	if !connected {
		setV2SSEHeaders(w)
		w.WriteHeader(http.StatusOK)
		writeV2SSEConnected(w, flusher)
	}

	meta := v2GraphStreamMeta{
		TotalNodes: len(resp.Nodes), TotalEdges: len(resp.Edges),
		TotalNodeCount: resp.TotalNodeCount, TotalEdgeCount: resp.TotalEdgeCount,
		NodeTruncated: resp.NodeTruncated, EdgeTruncated: resp.EdgeTruncated, Limits: resp.Limits,
		Communities: resp.Communities, Repos: resp.Repos,
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

// writeV2SSEConnected writes and flushes the opening `connected` SSE frame.
func writeV2SSEConnected(w http.ResponseWriter, flusher http.Flusher) {
	writeV2SSEEvent(w, "connected", fmt.Sprintf(`{"subscribed_at":%d}`, time.Now().UnixMilli()))
	flusher.Flush()
}

// clientAcceptsGzip reports whether the request opted into gzip. Mirrors the
// check in withGzip (server.go) so the stream negotiates identically.
func clientAcceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

// sseGzipWriter is a FLUSH-COMPATIBLE gzip wrapper for an SSE ResponseWriter
// (#49). Writes are compressed through a pooled gzip.Writer; Flush() flushes the
// gzip framing (a sync flush, so everything written so far is immediately
// decodable by the client) and THEN the underlying http.Flusher — preserving
// the progressive, per-chunk delivery the stream depends on. Header()/
// WriteHeader() delegate to the embedded ResponseWriter so headers land on the
// real connection.
type sseGzipWriter struct {
	http.ResponseWriter
	gz    *gzip.Writer
	under http.Flusher
}

func (s *sseGzipWriter) Write(b []byte) (int, error) { return s.gz.Write(b) }

// Flush pushes the gzip sync-flush first (making buffered data decodable) then
// flushes the underlying writer so the bytes actually leave the server.
func (s *sseGzipWriter) Flush() {
	_ = s.gz.Flush()
	s.under.Flush()
}

// newSSEGzipWriter wraps w for gzip-compressed SSE, reusing the shared
// gzipWriterPool (no new dependency). The returned cleanup MUST be deferred: it
// closes the gzip writer (emitting the trailer) and returns it to the pool.
func newSSEGzipWriter(w http.ResponseWriter, under http.Flusher) (*sseGzipWriter, func()) {
	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(w)
	sw := &sseGzipWriter{ResponseWriter: w, gz: gz, under: under}
	cleanup := func() {
		_ = gz.Close()
		gzipWriterPool.Put(gz)
	}
	return sw, cleanup
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
