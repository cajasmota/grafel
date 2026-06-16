// Package mcp — activity.go
//
// MCPActivityEvent is the wire shape for a single MCP tool invocation.
// MCPActivityBroker fans events to SSE subscribers (Phase 1 of epic #1157:
// Jarvis-style real-time visualization). It follows the same drop-on-full,
// non-blocking pattern as internal/progress.Broker so no handler is ever
// stalled by a slow HTTP client.
package mcp

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Per-call ID collector (epic #1157)
// ---------------------------------------------------------------------------
//
// Many grafel tools render their results as markdown (grafel_find's
// per-repo summary, the compact node/edge view) or as JSON whose id field is
// just "id" rather than "entity_id". In both cases the post-hoc extractIDs
// text-parser cannot recover the entity ids the call actually touched, so the
// MCP-activity event reaches the SSE stream with empty returned_node_ids and
// the WebUI glow has nothing to highlight.
//
// idCollector lets a handler (or a shared render helper) record the prefixed
// ids it surfaced directly, while it still has them in hand. wrap() installs a
// collector into the request context; emitActivity reads it back. This is a
// side channel only — collected ids never enter the tool's wire response, so
// nothing leaks to the calling agent.

type idCollectorKey struct{}

// idCollector accumulates node/edge ids touched during a single tool call.
// Goroutine-safe so concurrent render helpers can record without coordination.
type idCollector struct {
	mu    sync.Mutex
	nodes []string
	edges []string
}

// withIDCollector returns a child context carrying a fresh collector plus the
// collector itself. emitActivity drains the collector after the handler runs.
func withIDCollector(ctx context.Context) (context.Context, *idCollector) {
	c := &idCollector{}
	return context.WithValue(ctx, idCollectorKey{}, c), c
}

// collectorFrom returns the collector installed in ctx, or nil when absent
// (e.g. unit tests that call a handler directly without wrap()).
func collectorFrom(ctx context.Context) *idCollector {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(idCollectorKey{}).(*idCollector)
	return c
}

// recordNodeIDs adds prefixed node ids to the collector in ctx (no-op when
// ctx carries no collector). Safe to call from any render path.
func recordNodeIDs(ctx context.Context, ids ...string) {
	if c := collectorFrom(ctx); c != nil {
		c.mu.Lock()
		c.nodes = append(c.nodes, ids...)
		c.mu.Unlock()
	}
}

// recordEdgeIDs adds edge ids to the collector in ctx (no-op when absent).
func recordEdgeIDs(ctx context.Context, ids ...string) {
	if c := collectorFrom(ctx); c != nil {
		c.mu.Lock()
		c.edges = append(c.edges, ids...)
		c.mu.Unlock()
	}
}

// drain returns the deduped collected node/edge ids.
func (c *idCollector) drain() (nodes, edges []string) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return dedup(c.nodes), dedup(c.edges)
}

// ---------------------------------------------------------------------------
// Event
// ---------------------------------------------------------------------------

// MCPActivityEvent describes one MCP tool call and the graph objects it touched.
// All fields are JSON-serialisable so the SSE endpoint can forward verbatim.
type MCPActivityEvent struct {
	// ToolName is the MCP tool that was invoked (e.g. "grafel_search_entities").
	ToolName string `json:"tool_name"`

	// QueryArgs is a shallow copy of the tool arguments that drove the call.
	// Values are the raw interface{} from the CallToolRequest arguments map.
	QueryArgs map[string]any `json:"query_args,omitempty"`

	// ReturnedNodeIDs is the list of entity IDs included in the response.
	// Empty when the tool does not operate on specific entities.
	ReturnedNodeIDs []string `json:"returned_node_ids,omitempty"`

	// ReturnedEdgeIDs is the list of relationship IDs included in the response.
	ReturnedEdgeIDs []string `json:"returned_edge_ids,omitempty"`

	// AgentID is derived from the MCP session or request context. Populated
	// by the wrap middleware; empty when the origin cannot be determined.
	// Phase 2 will use this for per-agent color tinting on the graph.
	AgentID string `json:"agent_id,omitempty"`

	// Timestamp is Unix milliseconds when the event was emitted.
	Timestamp int64 `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Broker
// ---------------------------------------------------------------------------

const (
	activityDefaultBuffer = 128
	activityWildcard      = "\x00wildcard"
)

func activityBufferSize() int {
	if v := os.Getenv("GRAFEL_ACTIVITY_BUFFER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return activityDefaultBuffer
}

type activitySub struct {
	ch chan MCPActivityEvent
}

// MCPActivityBroker is a fan-out pub/sub bus for MCP tool call events.
// One instance lives for the daemon process lifetime. Tool handlers publish
// after returning; SSE subscribers in the dashboard receive events.
//
// Publish is non-blocking: if a subscriber's buffer is full the event is
// silently dropped so no tool handler ever stalls.
type MCPActivityBroker struct {
	mu   sync.RWMutex
	subs map[string][]*activitySub

	// log is the optional disk sink. Nil when disk logging is disabled.
	log *ActivityLog
}

// NewMCPActivityBroker constructs an empty broker.
func NewMCPActivityBroker() *MCPActivityBroker {
	return &MCPActivityBroker{
		subs: make(map[string][]*activitySub),
	}
}

// SetLog attaches a disk log to the broker. After this call every event
// published to the broker is also written to disk asynchronously.
func (b *MCPActivityBroker) SetLog(l *ActivityLog) {
	b.mu.Lock()
	b.log = l
	b.mu.Unlock()
}

// CloseLog flushes and closes the attached disk log (if any) and detaches it
// from the broker. Idempotent and safe to call on daemon shutdown; after it
// returns the underlying file handle is released, which is required before the
// isolated test root (~/.grafel) can be removed on Windows.
func (b *MCPActivityBroker) CloseLog() {
	b.mu.Lock()
	l := b.log
	b.log = nil
	b.mu.Unlock()
	if l != nil {
		l.Close()
	}
}

// Publish fans e out to every subscriber and (optionally) appends it to the
// disk log. Non-blocking: slow subscribers are silently dropped.
func (b *MCPActivityBroker) Publish(e MCPActivityEvent) {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().UnixMilli()
	}
	b.mu.RLock()
	var targets []*activitySub
	targets = append(targets, b.subs[activityWildcard]...)
	b.mu.RUnlock()

	for _, s := range targets {
		select {
		case s.ch <- e:
		default:
			// subscriber buffer full — drop.
		}
	}

	// Disk log — best-effort, no lock needed (ActivityLog is goroutine-safe).
	b.mu.RLock()
	l := b.log
	b.mu.RUnlock()
	if l != nil {
		l.Append(e) // non-blocking
	}
}

// SubscribeAll returns a receive-only channel that receives every event
// published to this broker, regardless of tool name. The caller must call
// the returned cancel function when done (e.g. on HTTP disconnect).
func (b *MCPActivityBroker) SubscribeAll() (<-chan MCPActivityEvent, func()) {
	buf := activityBufferSize()
	s := &activitySub{ch: make(chan MCPActivityEvent, buf)}

	b.mu.Lock()
	b.subs[activityWildcard] = append(b.subs[activityWildcard], s)
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.removeLocked(s)
		})
	}
	return s.ch, cancel
}

func (b *MCPActivityBroker) removeLocked(sub *activitySub) {
	subs := b.subs[activityWildcard]
	for i, s := range subs {
		if s == sub {
			subs[i] = subs[len(subs)-1]
			subs[len(subs)-1] = nil
			b.subs[activityWildcard] = subs[:len(subs)-1]
			break
		}
	}
	if len(b.subs[activityWildcard]) == 0 {
		delete(b.subs, activityWildcard)
	}
	close(sub.ch)
}

// SubscriberCount returns the number of active SSE subscribers. Intended for
// diagnostics / healthz endpoints.
func (b *MCPActivityBroker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[activityWildcard])
}
