package engine

import (
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// wsCallEntity finds the http_endpoint_call with the given ID, or nil.
func wsCallEntity(res *DetectResult, id string) *entityForTest {
	for i := range res.Entities {
		e := res.Entities[i]
		if e.ID == id && e.Kind == httpEndpointCallKind {
			return &entityForTest{
				id:           e.ID,
				name:         e.Name,
				qn:           e.QualifiedName,
				patternType:  e.Properties["pattern_type"],
				sourceCaller: e.Properties["source_caller"],
				verb:         e.Properties["verb"],
				framework:    e.Properties["framework"],
			}
		}
	}
	return nil
}

func wsCallIDs(res *DetectResult) []string {
	var got []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind {
			got = append(got, e.ID)
		}
	}
	sort.Strings(got)
	return got
}

// serverWSDefID runs the realtime producer pass on a socket.io SERVER fixture
// for the given event and returns the http:WS: definition ID it emits. This is
// the parity oracle: the client side must produce a byte-identical ID.
func serverWSDefID(t *testing.T, event string) string {
	t.Helper()
	src := "import { Server } from 'socket.io';\n" +
		"const io = new Server(httpServer);\n" +
		"io.on('connection', (socket) => {\n" +
		"  socket.on('" + event + "', (m) => { handle(m); });\n" +
		"});\n"
	_, res := runDetect(t, "typescript", "server.ts", src)
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind &&
			e.Properties["verb"] == "WS" &&
			e.Properties["event"] == event {
			return e.ID
		}
	}
	t.Fatalf("realtime producer pass emitted no WS definition for event %q", event)
	return ""
}

// TestSynth_WSClient_EmitMatchesServerID is the core parity-oracle test: a
// client `socket.emit('chat:message', m)` must emit a client-call whose ID
// EXACTLY matches the server-side handler endpoint
// (socket.on('chat:message')), including the `:`→`{}` canonicalisation, so the
// cross-repo linker joins them.
func TestSynth_WSClient_EmitMatchesServerID(t *testing.T) {
	wantID := serverWSDefID(t, "chat:message")
	if wantID != "http:WS:/chat{message}" {
		t.Fatalf("server id parity oracle changed: got %q", wantID)
	}

	src := `
import { io } from 'socket.io-client';

const socket = io('https://api.example.com');

function ChatBox() {
  function send(m) {
    socket.emit('chat:message', m);
  }
  return send;
}
`
	_, res := runDetect(t, "typescript", "ChatBox.tsx", src)

	e := wsCallEntity(res, wantID)
	if e == nil {
		t.Fatalf("missing client-call %q matching server endpoint shape (got calls: %v)",
			wantID, wsCallIDs(res))
	}
	if e.verb != "WS" {
		t.Errorf("verb = %q, want WS (must match server synthetic verb)", e.verb)
	}
	if e.patternType != "http_endpoint_client_synthesis" {
		t.Errorf("pattern_type = %q, want http_endpoint_client_synthesis", e.patternType)
	}
	if e.framework != "socket.io-client:emit" {
		t.Errorf("framework = %q, want socket.io-client:emit (publish role)", e.framework)
	}
	if e.sourceCaller != "Function:send" {
		t.Errorf("source_caller = %q, want Function:send (drives FETCHES edge)", e.sourceCaller)
	}
	if e.qn != wantID {
		t.Errorf("qualified_name = %q, want %q", e.qn, wantID)
	}
}

// TestSynth_WSClient_OnIsSubscription proves `socket.on('notify', cb)` emits a
// subscription-role client-call keyed to the server endpoint shape.
func TestSynth_WSClient_OnIsSubscription(t *testing.T) {
	wantID := serverWSDefID(t, "notify") // http:WS:/notify

	src := `
import { io } from 'socket.io-client';
const socket = io('/realtime');

function Bell() {
  socket.on('notify', (payload) => render(payload));
}
`
	_, res := runDetect(t, "typescript", "Bell.tsx", src)

	e := wsCallEntity(res, wantID)
	if e == nil {
		t.Fatalf("missing subscription client-call %q (got: %v)", wantID, wsCallIDs(res))
	}
	if e.framework != "socket.io-client:subscribe" {
		t.Errorf("framework = %q, want socket.io-client:subscribe (subscription role)", e.framework)
	}
	if e.sourceCaller != "Function:Bell" {
		t.Errorf("source_caller = %q, want Function:Bell", e.sourceCaller)
	}
}

// TestSynth_WSClient_FetchesEdge proves the source_caller resolves into a real
// FETCHES edge from the enclosing component to the WS client-call, end-to-end
// through ResolveHTTPEndpointHandlers.
func TestSynth_WSClient_FetchesEdge(t *testing.T) {
	wantID := serverWSDefID(t, "order:created") // http:WS:/order{created}

	src := `
import { io } from 'socket.io-client';
const socket = io('https://orders.example.com');

function OrdersFeed() {
  socket.emit('order:created', { id: 1 });
}
`
	_, res := runDetect(t, "typescript", "OrdersFeed.tsx", src)

	call := wsCallEntity(res, wantID)
	if call == nil {
		t.Fatalf("missing client-call %q (got: %v)", wantID, wsCallIDs(res))
	}
	if call.sourceCaller != "Function:OrdersFeed" {
		t.Fatalf("source_caller = %q, want Function:OrdersFeed", call.sourceCaller)
	}

	caller := types.EntityRecord{
		Kind:       "Function",
		Name:       "OrdersFeed",
		SourceFile: "OrdersFeed.tsx",
		Language:   "typescript",
	}
	merged := append([]types.EntityRecord{caller}, res.Entities...)
	out, stats := ResolveHTTPEndpointHandlers(merged)
	if stats.CallerResolved < 1 {
		t.Errorf("expected >=1 caller_resolved, got %d", stats.CallerResolved)
	}
	foundFetch := false
	for _, e := range out {
		for _, r := range e.Relationships {
			if r.Kind == "FETCHES" && r.ToID == "http_endpoint_call:"+wantID {
				if r.FromID != "Function:OrdersFeed" {
					t.Errorf("FETCHES from = %q, want Function:OrdersFeed", r.FromID)
				}
				foundFetch = true
			}
		}
	}
	if !foundFetch {
		t.Errorf("expected FETCHES edge Function:OrdersFeed → http_endpoint_call:%s", wantID)
	}
}

// TestSynth_WSClient_DynamicEventSkipped is the honest-partial negative: a
// dynamic event name (bare identifier or template literal) must NOT fabricate
// an endpoint.
func TestSynth_WSClient_DynamicEventSkipped(t *testing.T) {
	src := "import { io } from 'socket.io-client';\n" +
		"const socket = io('/x');\n" +
		"function send(eventName, room) {\n" +
		"  socket.emit(eventName, {});\n" +
		"  socket.emit(`room:${room}`, {});\n" +
		"}\n"
	_, res := runDetect(t, "typescript", "dyn.ts", src)
	if ids := wsCallIDs(res); len(ids) != 0 {
		t.Errorf("dynamic event names must emit no WS client-call, got: %v", ids)
	}
}

// TestSynth_WSClient_NonSocketEmitterIgnored is the negative for a plain Node
// EventEmitter on an unrelated var: there is no socket.io-client connection in
// the file, so `.emit('data')` must NOT be treated as a WS publish.
func TestSynth_WSClient_NonSocketEmitterIgnored(t *testing.T) {
	src := `
import { EventEmitter } from 'events';
const bus = new EventEmitter();

function publish() {
  bus.emit('data', 42);
}
`
	_, res := runDetect(t, "typescript", "bus.ts", src)
	if ids := wsCallIDs(res); len(ids) != 0 {
		t.Errorf("EventEmitter.emit without a socket.io-client connection must emit nothing, got: %v", ids)
	}
}

// TestSynth_WSClient_ServerFileNotDoubleEmitted proves a socket.io SERVER file
// (new Server + io.on('connection')) does NOT additionally emit a CLIENT-side
// http_endpoint_call for its handlers — the realtime producer pass owns those.
// We assert no http_endpoint_call (consumer) entity is produced; the producer
// definition is allowed.
func TestSynth_WSClient_ServerFileNotDoubleEmitted(t *testing.T) {
	src := `
import { Server } from 'socket.io';
const io = new Server(httpServer);
io.on('connection', (socket) => {
  socket.on('chat:message', (m) => handle(m));
});
`
	_, res := runDetect(t, "typescript", "server.ts", src)
	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind &&
			e.Properties["framework"] == "socket.io-client:subscribe" {
			t.Errorf("server file must not emit a CLIENT subscribe call, got %q", e.ID)
		}
	}
}
