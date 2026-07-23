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

	// subscriberToken is the caller's RebuildArgs.ProgressToken (see
	// internal/cli's progressToken()/subscribeSSE), passed as the `token` query
	// param — the same param name subscribeSSE writes and the one field name
	// callers already know the token by (ProgressToken / "progress_token" on the
	// job/onboard/maintenance JSON handlers). Empty for tokenless callers (e.g. a
	// dashboard late-reconnect with no run in flight), which preserves today's
	// behaviour exactly (#5326).
	//
	// #5937 chunk 2 — RUN-IDENTITY matching. Chunk 1 stamped every progress.Event
	// (including the retained terminal) with RunToken. This handler now uses
	// that to close BOTH failure modes a prior review found in the
	// subscribe-then-clear (ClearTerminal) design:
	//
	//   F1 (deterministic): the wizard subscribes to SSE BEFORE it triggers its
	//   own rebuild (see wizard_tui_run.go / wizard_split_progress.go). So by the
	//   time this handler's connect-time emitTerminalIfReady() call below runs,
	//   daemon.Service.Rebuild — and therefore its ClearTerminal(group) call —
	//   has NOT fired yet. A previous run's retained terminal is still sitting in
	//   the broker and gets replayed+closed immediately, before the new run has
	//   even started. ClearTerminal cannot fix this: it is called too late,
	//   inside Rebuild, which the wizard invokes only AFTER subscribing.
	//
	//   F2 (race): even once ClearTerminal has fired, a PREVIOUS run's terminal
	//   can still be delivered to this subscriber via the live fan-out path
	//   (case e := <-ch below) if it was already in that subscriber's channel
	//   buffer, or arrives from a late/retried Publish shortly after connect
	//   (observed up to ~120ms after a new subscriber attaches via the serve-
	//   start sidecar tailer). The old code treated ANY terminal-phase event on
	//   the live path as THIS subscriber's terminal and closed the stream —
	//   re-poisoning a brand-new subscription with a dead run's outcome.
	//
	// Token matching closes both: replay-on-connect only fires when the
	// retained terminal's RunToken matches (or the subscriber is tokenless), and
	// the live-forward path below applies the identical check so a foreign-run
	// terminal is silently dropped instead of closing the stream. See
	// emitTerminalIfReady and the live-path check in the event loop below.
	subscriberToken := r.URL.Query().Get("token")

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
	// #5937 — this replay is only safe because retained-terminal invalidation
	// is now RUN-SCOPED, not subscriber-scoped. Broker.terminal used to be
	// cleared NEVER, and the serve-start sidecar tailer republished an entire
	// pre-existing sidecar (including a PRIOR run's terminal line) into the
	// broker before any wizard connected — so (1) above would replay that
	// previous run's corpse and immediately close the stream, and the wizard
	// would never see a live event for the run it was actually watching.
	//
	// A wall-clock check here (`te.TS < subscribedAt`) cannot fix that: by
	// construction, ANY already-retained terminal was published before this
	// subscription's subscribedAt, INCLUDING the legitimate case branch (1)
	// exists for — a client that connects/reconnects after the CURRENT run's
	// rebuild already finished. Gating on that comparison does not narrow the
	// bug, it deletes the guarantee outright (verified: publish a terminal at
	// real `now`, subscribe immediately — the old gate withheld it forever,
	// heartbeats only, since re-checking the same comparison on every tick
	// never turns true).
	//
	// The actual fix is upstream: daemon.Service.Rebuild calls
	// Broker.ClearTerminal(group) at the START of every new run (the single
	// choke point every rebuild trigger passes through, in both split and
	// monolith mode — see that method's comment). That means whatever this
	// handler finds retained here can only be EITHER this group's own most
	// recent completed run (replay it — this is the #5326 guarantee) OR
	// nothing at all (a run is in flight or has never completed since the
	// last clear) — never a stale cross-run corpse. So once retained, replay
	// it unconditionally; a terminal that fires while this handler is already
	// attached is still delivered via the live path below and the heartbeat
	// re-assert.
	var terminalSent bool
	emitTerminalIfReady := func() (done bool) {
		if group == sseWildcardGroup || terminalSent {
			return false
		}
		te, ok := s.progressBroker.LastTerminal(group)
		if !ok {
			return false
		}
		// Token mismatch: the retained terminal belongs to a DIFFERENT run than
		// the one this subscriber is watching (#5937 F1). Do not replay it and do
		// not close — stay attached so the subscriber's own run's live events can
		// still arrive. A tokenless subscriber (subscriberToken == "") always
		// matches, preserving the #5326 late-reconnect guarantee unchanged.
		if subscriberToken != "" && te.RunToken != subscriberToken {
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
			// #5937 F2 — a terminal event arriving on the LIVE path (not just the
			// connect-time replay above) can belong to a DIFFERENT run than the one
			// this subscriber asked for: e.g. a prior run's Publish landing a few
			// ms after this subscriber attached. Drop it silently — do not forward
			// it as progress and do not close the stream — so this subscriber stays
			// attached to receive its OWN run's live events. A tokenless subscriber
			// always matches (preserves pre-#5937 behaviour for callers that don't
			// disambiguate by run).
			if isTerminalEventPhase(e.Phase) && subscriberToken != "" && e.RunToken != subscriberToken {
				continue
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
