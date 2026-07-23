package progress

import (
	"os"
	"strconv"
	"sync"
)

const (
	// defaultBufferSize is the channel capacity per subscriber. Progress events
	// are best-effort: a slow subscriber gets the most recent events once its
	// buffer drains — older events are silently dropped rather than blocking the
	// publisher (and therefore the indexer).
	defaultBufferSize = 64
)

// bufferSize returns the effective per-subscriber channel capacity. It can be
// overridden at process startup via the GRAFEL_PROGRESS_BUFFER env var.
func bufferSize() int {
	if v := os.Getenv("GRAFEL_PROGRESS_BUFFER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultBufferSize
}

// subscriber holds one consumer's channel and its position in the group slice.
type subscriber struct {
	ch chan Event
}

// Broker is a fan-out pub/sub bus keyed by group slug. One Broker instance lives
// for the lifetime of the daemon process. The indexer publishes events; CLI
// rebuild, dashboard SSE handlers, and future MCP tools subscribe to them.
//
// Publish is non-blocking: if a subscriber's buffer is full the oldest event is
// dropped so the indexer never stalls waiting for a slow HTTP client.
type Broker struct {
	mu   sync.RWMutex
	subs map[string][]*subscriber
	// terminal retains the most recent terminal event (PhaseDone / PhaseError)
	// per group slug. A terminal event is emitted exactly once by the indexer
	// (Tracker.Done / Tracker.Fail); because Publish is best-effort (drop-on-full),
	// that single event can be lost if a slow SSE subscriber's buffer is full at
	// the moment it fires — which leaves the wizard UI frozen on the last
	// mid-extraction frame, never learning the rebuild finished (#5326). Retaining
	// it lets the SSE handler (a) replay it to a late/reconnecting subscriber and
	// (b) re-assert it on the heartbeat so the terminal state is ALWAYS rendered.
	terminal map[string]Event
}

// NewBroker constructs an empty Broker ready for use.
func NewBroker() *Broker {
	return &Broker{
		subs:     make(map[string][]*subscriber),
		terminal: make(map[string]Event),
	}
}

// isTerminalPhase reports whether a phase label is a terminal indexing state.
func isTerminalPhase(phase string) bool {
	return phase == PhaseDone || phase == PhaseError
}

// LastTerminal returns the most recent terminal (done/error) event retained for
// the given group, and whether one has been recorded. Used by the SSE handler to
// guarantee the terminal state reaches the client even if the live event was
// dropped on a full buffer (#5326).
func (b *Broker) LastTerminal(group string) (Event, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	e, ok := b.terminal[group]
	return e, ok
}

// ClearTerminal removes any retained terminal (done/error) event for group.
//
// Invalidation is RUN-SCOPED: the caller (daemon.Service.Rebuild) calls this
// at the very start of every new rebuild for group — the single choke point
// every rebuild trigger passes through, in both split and monolith mode —
// so a terminal retained here can only ever be either (a) nothing, because no
// run has completed since the last clear, or (b) the group's own most
// recently completed run, never a PRIOR run's stale corpse (#5937). This is
// deliberately NOT a wall-clock/TTL-based expiry: a subscriber's connect time
// has no principled relationship to "is this terminal from the run I am
// watching" — any retained terminal is, by construction, always published
// before a given subscriber's subscribedAt, so gating replay on that
// comparison would make the legitimate late-reconnect-after-completion case
// (the whole point of retaining it, #5326) permanently unreachable. Safe to
// call even when no terminal is retained for group.
func (b *Broker) ClearTerminal(group string) {
	b.mu.Lock()
	delete(b.terminal, group)
	b.mu.Unlock()
}

// wildcardGroup is the internal key used to register subscribers that want
// events from every group. It must not be a valid group slug (group slugs are
// non-empty URL path segments).
const wildcardGroup = "\x00wildcard"

// Publish fans an event out to every subscriber registered for e.GroupSlug,
// plus any wildcard subscribers registered via SubscribeAll. Each send is
// attempted with a non-blocking select; if the subscriber's channel is full the
// event is discarded for that subscriber (drop-on-full, not drop-oldest). This
// keeps the publisher lock-free on the hot path.
//
// Publish implements the Publisher interface so the indexer can use the broker
// without importing a concrete type.
func (b *Broker) Publish(e Event) {
	if isTerminalPhase(e.Phase) {
		// Retain terminal events so the SSE handler can guarantee delivery even
		// if the live fan-out below drops this event on a full buffer (#5326).
		b.mu.Lock()
		b.terminal[e.GroupSlug] = e
		b.mu.Unlock()
	}

	b.mu.RLock()
	groupSubs := b.subs[e.GroupSlug]
	wildcardSubs := b.subs[wildcardGroup]
	// Snapshot both slices under the read lock so we can send without holding it.
	targets := make([]*subscriber, 0, len(groupSubs)+len(wildcardSubs))
	targets = append(targets, groupSubs...)
	targets = append(targets, wildcardSubs...)
	b.mu.RUnlock()

	for _, s := range targets {
		select {
		case s.ch <- e:
		default:
			// Subscriber buffer full — drop this event. Progress is best-effort.
		}
	}
}

// BroadcastAll delivers an event to every subscriber across all groups,
// including wildcard subscribers registered via SubscribeAll. Useful for
// daemon-level lifecycle events (e.g. daemon shutdown notice) and for the
// daemon-wide SSE endpoint.
func (b *Broker) BroadcastAll(e Event) {
	b.mu.RLock()
	var targets []*subscriber
	for _, subs := range b.subs {
		targets = append(targets, subs...)
	}
	b.mu.RUnlock()

	for _, s := range targets {
		select {
		case s.ch <- e:
		default:
		}
	}
}

// SubscribeAll registers a consumer that receives events from every group.
// It works by registering under the internal wildcardGroup key; Publish
// delivers to wildcardGroup in addition to the event's own group.
//
// Returns a receive-only channel and a cancel function. The caller must call
// cancel when done (e.g. when the HTTP connection closes).
func (b *Broker) SubscribeAll() (<-chan Event, func()) {
	return b.Subscribe(wildcardGroup)
}

// Subscribe registers a new consumer for progress events from the given group.
// It returns a receive-only channel and a cancel function. The caller must call
// cancel when it is done (e.g. when the HTTP connection closes) to free
// resources and close the channel. Calling cancel more than once is safe.
//
//	ch, cancel := broker.Subscribe("my-group")
//	defer cancel()
//	for e := range ch { ... }
func (b *Broker) Subscribe(group string) (<-chan Event, func()) {
	buf := bufferSize()
	s := &subscriber{ch: make(chan Event, buf)}

	b.mu.Lock()
	b.subs[group] = append(b.subs[group], s)
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.removeLocked(group, s)
		})
	}
	return s.ch, cancel
}

// removeLocked removes sub from the group slice and closes its channel.
// Must be called with b.mu held for writing.
func (b *Broker) removeLocked(group string, sub *subscriber) {
	subs := b.subs[group]
	for i, s := range subs {
		if s == sub {
			// Replace with last element and shrink slice.
			subs[i] = subs[len(subs)-1]
			subs[len(subs)-1] = nil
			b.subs[group] = subs[:len(subs)-1]
			break
		}
	}
	if len(b.subs[group]) == 0 {
		delete(b.subs, group)
	}
	close(sub.ch)
}

// Stats returns a snapshot of the number of active subscribers per group. It is
// intended for diagnostics (e.g. a /healthz or metrics endpoint) and takes a
// read lock so it does not interfere with ongoing publishes.
func (b *Broker) Stats() map[string]int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]int, len(b.subs))
	for group, subs := range b.subs {
		out[group] = len(subs)
	}
	return out
}
