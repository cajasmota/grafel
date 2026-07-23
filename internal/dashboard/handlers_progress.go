package dashboard

// handlers_progress.go — SSE endpoint for real-time indexing progress
//
// Routes registered in server.go:
//
//	GET /api/index-progress          — all groups (daemon-wide)
//	GET /api/index-progress/{group}  — single group filtered stream
//
// The handler subscribes to the shared Broker on s.progressBroker, writes
// Server-Sent Events to the response body, and tears down cleanly on client
// disconnect. A 1-second heartbeat (#1527) keeps load-balancers and reverse
// proxies from closing idle connections and keeps fast streams looking live.
//
// Wire format (SSE):
//
//	event: connected
//	data: {"group":"<slug>","subscribed_at":<unix-ms>}\n\n
//
//	event: progress
//	data: <JSON-encoded progress.Event>\n\n
//
//	event: heartbeat
//	data: {}\n\n
//
//	event: close
//	data: {}\n\n   (sent before the server closes the stream)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

const (
	// heartbeatInterval is the SSE keepalive cadence for the index-progress
	// stream. Real progress events flow on every extraction tick; this is only
	// the idle keepalive. Dropped from 15s to 1s (#1527) so a fast repo that
	// finishes between ticks still produces a perceptibly live stream.
	heartbeatInterval = 1 * time.Second
	// sseWildcardGroup is the sentinel used internally when a caller subscribes
	// to all groups (the daemon-wide /api/index-progress endpoint).
	sseWildcardGroup = ""
)

// handleIndexProgressAll streams progress events from every group.
func (s *Server) handleIndexProgressAll(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, sseWildcardGroup)
}

// handleIndexProgressGroup streams progress events filtered to one group slug.
func (s *Server) handleIndexProgressGroup(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "missing group slug")
		return
	}
	s.serveSSE(w, r, group)
}

// serveSSE is the shared implementation for both SSE endpoints.
// group == sseWildcardGroup means "all groups".
func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, group string) {
	if s.progressBroker == nil {
		writeErr(w, http.StatusServiceUnavailable, "progress broker not available")
		return
	}

	// SSE requires the response to be flushable.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Proxy-friendliness headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Subscribe to the broker. For the wildcard endpoint we subscribe to
	// every group key by using an empty string; the broker treats that as its
	// own group bucket, which is fine for heartbeat-only — but to receive
	// events from all real groups we use BroadcastAll on the publish side.
	// Here we need a different approach: subscribe per-group is not possible
	// without knowing group names in advance. Instead, we maintain a dedicated
	// "wildcard" subscription: publish side will call BroadcastAll which sends
	// to every registered channel. We subscribe with the empty-string sentinel.
	var (
		ch     <-chan progress.Event
		cancel func()
	)
	if group == sseWildcardGroup {
		ch, cancel = s.progressBroker.SubscribeAll()
	} else {
		ch, cancel = s.progressBroker.Subscribe(group)
	}
	defer cancel()

	// Send the initial "connected" event so the client knows the stream is live.
	subscribedAt := time.Now().UnixMilli()
	connPayload := fmt.Sprintf(`{"group":%q,"subscribed_at":%d}`, group, subscribedAt)
	writeSSEEvent(w, "connected", connPayload)
	flusher.Flush()

	// #5326 — terminal-state guarantee. A rebuild emits its terminal event
	// (PhaseDone / PhaseError) exactly once, and Publish is best-effort
	// (drop-on-full): under load that single event can be dropped, leaving the
	// wizard UI frozen on the last mid-extraction frame and never showing
	// completion. We defend against that two ways for a concrete-group stream:
	//   1. On connect, replay any already-recorded terminal event (covers a
	//      client that connected/reconnected AFTER the rebuild finished).
	//   2. On every heartbeat, re-check the retained terminal event and forward
	//      it if we have not already (covers the in-flight drop-on-full case).
	// In both cases we then emit `close`, so the UI always reaches a terminal
	// render rather than silently freezing.
	//
	// #5937 — freshness gate. Broker.terminal is never cleared, and the
	// serve-start sidecar tailer republishes an entire pre-existing sidecar
	// (including a PRIOR run's terminal line) into the broker before any wizard
	// connects. Without a staleness check, (1) above replays that previous run's
	// corpse and immediately closes the stream, so the wizard never sees any
	// live event for the run it is actually watching. Only treat the retained
	// terminal as replayable when it is NOT older than this subscription — a
	// terminal that fires during the subscription is still guaranteed via the
	// live path below and the heartbeat re-assert.
	var terminalSent bool
	emitTerminalIfReady := func() (done bool) {
		if group == sseWildcardGroup || terminalSent {
			return false
		}
		te, ok := s.progressBroker.LastTerminal(group)
		if !ok {
			return false
		}
		if te.TS < subscribedAt {
			// Stale terminal from a run that already ended before this
			// subscription started (or before serve even started). Do not
			// replay, and do not close — stay attached for live events.
			return false
		}
		if data, err := json.Marshal(te); err == nil {
			writeSSEEvent(w, "progress", string(data))
			writeSSEEvent(w, "close", "{}")
			flusher.Flush()
			terminalSent = true
			return true
		}
		return false
	}
	if emitTerminalIfReady() {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected. Send a close event (best-effort; the
			// write may fail if the connection is already gone, which is fine).
			writeSSEEvent(w, "close", "{}")
			flusher.Flush()
			return

		case e, ok := <-ch:
			if !ok {
				// Broker closed the channel (e.g. daemon shutdown).
				writeSSEEvent(w, "close", "{}")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			writeSSEEvent(w, "progress", string(data))
			flusher.Flush()
			// If this was the terminal event itself, record that we delivered it
			// and close — no need to wait for a heartbeat re-assert.
			if isTerminalEventPhase(e.Phase) {
				terminalSent = true
				writeSSEEvent(w, "close", "{}")
				flusher.Flush()
				return
			}

		case <-heartbeat.C:
			// Re-assert the terminal state if the live event was dropped.
			if emitTerminalIfReady() {
				return
			}
			writeSSEEvent(w, "heartbeat", "{}")
			flusher.Flush()
		}
	}
}

// isTerminalEventPhase reports whether an SSE progress event represents a
// terminal indexing state (done/error). Mirrors progress.isTerminalPhase, which
// is unexported.
func isTerminalEventPhase(phase string) bool {
	return phase == progress.PhaseDone || phase == progress.PhaseError
}

// writeSSEEvent writes a single SSE event block to w.
// It does NOT flush — callers must flush after writing.
func writeSSEEvent(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}
