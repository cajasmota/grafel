package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runDetectWS is the WS-test analogue of runDetect: it loads compiled YAML
// rules, runs Detect on a synthesised file, and returns ALL entities and
// relationships so the caller can assert on the WS-specific subset.
func runDetectWS(t *testing.T, language, path, content string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	det := New(rules)
	res, err := det.Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: language,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func filterEntities(ents []types.EntityRecord, kind string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func filterRels(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// WebSocket — Jakarta @ServerEndpoint (Quarkus / Tomcat / GlassFish)
// ---------------------------------------------------------------------------

func TestWS_JakartaServerEndpoint_EmitsChannelAndSubscribes(t *testing.T) {
	src := `package io.test;
import jakarta.websocket.*;
import jakarta.websocket.server.ServerEndpoint;

@ServerEndpoint("/ws/orders")
public class OrderEndpoint {
    @OnOpen
    public void onOpen(Session session) {}

    @OnMessage
    public void onMessage(OrderEvent payload, Session session) {
        session.getBasicRemote().sendText("ack");
    }
}
`
	res := runDetectWS(t, "java", "OrderEndpoint.java", src)
	chans := filterEntities(res.Entities, channelEventKind)
	if len(chans) < 1 {
		t.Fatalf("expected at least 1 ChannelEvent, got %d (entities=%v)", len(chans), res.Entities)
	}
	if chans[0].ID != "ws:/ws/orders" {
		t.Errorf("channel ID = %q, want ws:/ws/orders", chans[0].ID)
	}
	subs := filterRels(res.Relationships, "WS_SUBSCRIBES_TO")
	if len(subs) < 1 {
		t.Fatalf("expected WS_SUBSCRIBES_TO edge, got %d", len(subs))
	}
	if subs[0].Properties["schema"] != "OrderEvent" {
		t.Errorf("schema property = %q, want OrderEvent", subs[0].Properties["schema"])
	}
	emits := filterRels(res.Relationships, "WS_EMITS")
	if len(emits) < 1 {
		t.Fatalf("expected WS_EMITS edge from session.sendText, got %d", len(emits))
	}
}

// ---------------------------------------------------------------------------
// WebSocket — fixture-f scenario: both server endpoints + browser client
// ---------------------------------------------------------------------------

func TestWS_FixtureF_QuarkusServerEndpoints(t *testing.T) {
	otel := `package io.triage.broadcaster.websocket;
import jakarta.websocket.*;
import jakarta.websocket.server.ServerEndpoint;

@ServerEndpoint("/ws/otel")
public class OtelWebSocketEndpoint {
    @OnOpen public void onOpen(Session session) {}
    @OnMessage public void onMessage(String msg, Session session) {}
}`
	trace := `package io.triage.broadcaster.websocket;
import jakarta.websocket.*;
import jakarta.websocket.server.ServerEndpoint;

@ServerEndpoint("/ws/trace")
public class TraceWebSocketEndpoint {
    @OnOpen public void onOpen(Session session) {}
}`

	resA := runDetectWS(t, "java", "OtelWebSocketEndpoint.java", otel)
	resB := runDetectWS(t, "java", "TraceWebSocketEndpoint.java", trace)
	wantA := "ws:/ws/otel"
	wantB := "ws:/ws/trace"
	if len(filterEntities(resA.Entities, channelEventKind)) == 0 ||
		filterEntities(resA.Entities, channelEventKind)[0].ID != wantA {
		t.Errorf("otel: did not produce %q (got %v)", wantA, resA.Entities)
	}
	if len(filterEntities(resB.Entities, channelEventKind)) == 0 ||
		filterEntities(resB.Entities, channelEventKind)[0].ID != wantB {
		t.Errorf("trace: did not produce %q (got %v)", wantB, resB.Entities)
	}
}

func TestWS_FixtureF_BrowserClientWSConnects(t *testing.T) {
	src := "function setupTrace() {\n" +
		"  const url = \"ws://localhost:8088/ws/trace\";\n" +
		"  const ws = new WebSocket(url);\n" +
		"  ws.onmessage = (e) => {};\n" +
		"}\n" +
		"function setupOtel() {\n" +
		"  const socket = new WebSocket(\"ws://localhost:8088/ws/otel\");\n" +
		"}\n"
	res := runDetectWS(t, "typescript", "ws.ts", src)
	conns := filterRels(res.Relationships, "WS_CONNECTS")
	if len(conns) < 2 {
		t.Fatalf("expected ≥2 WS_CONNECTS edges, got %d (rels=%v)", len(conns), res.Relationships)
	}
	chans := filterEntities(res.Entities, channelEventKind)
	seen := map[string]bool{}
	for _, c := range chans {
		seen[c.ID] = true
	}
	if !seen["ws:/ws/trace"] || !seen["ws:/ws/otel"] {
		t.Errorf("expected both ws:/ws/trace and ws:/ws/otel ChannelEvents; got %v", chans)
	}
}

// Cross-stack: server channel ID and client channel ID must match by Name.
func TestWS_CrossStack_MatchByChannelID(t *testing.T) {
	server := `package x;
import jakarta.websocket.*;
import jakarta.websocket.server.ServerEndpoint;
@ServerEndpoint("/ws/trace")
public class TraceEp { @OnOpen public void onOpen(Session s) {} }`
	client := "function go() { new WebSocket(\"ws://api.example.com/ws/trace\"); }"
	srvRes := runDetectWS(t, "java", "x.java", server)
	clRes := runDetectWS(t, "typescript", "x.ts", client)

	srvID := ""
	if ee := filterEntities(srvRes.Entities, channelEventKind); len(ee) > 0 {
		srvID = ee[0].ID
	}
	clID := ""
	if ee := filterEntities(clRes.Entities, channelEventKind); len(ee) > 0 {
		clID = ee[0].ID
	}
	if srvID == "" || clID == "" || srvID != clID {
		t.Fatalf("cross-stack identity mismatch: server=%q client=%q", srvID, clID)
	}
}

// ---------------------------------------------------------------------------
// WebSocket — socket.io scope distinctions (beyond-minimum)
// ---------------------------------------------------------------------------

func TestWS_SocketIO_EmitScopes(t *testing.T) {
	src := `const { Server } = require('socket.io');
const io = new Server(httpServer);
io.on('connection', (socket) => {
  socket.on('chat', (msg) => {});
  socket.emit('welcome', {});
  socket.to('room1').emit('joined', {});
  io.emit('broadcast', {});
});
`
	res := runDetectWS(t, "javascript", "server.js", src)
	emits := filterRels(res.Relationships, "WS_EMITS")
	if len(emits) < 3 {
		t.Fatalf("expected ≥3 WS_EMITS edges, got %d (rels=%v)", len(emits), res.Relationships)
	}
	scopes := map[string]int{}
	for _, e := range emits {
		scopes[e.Properties["scope"]]++
	}
	if scopes["broadcast"] == 0 {
		t.Errorf("expected scope=broadcast emit, got %v", scopes)
	}
	if scopes["room"] == 0 {
		t.Errorf("expected scope=room emit, got %v", scopes)
	}
	if scopes["user"] == 0 {
		t.Errorf("expected scope=user emit, got %v", scopes)
	}
	// Look for the room name.
	foundRoom := false
	for _, e := range emits {
		if e.Properties["room"] == "room1" {
			foundRoom = true
		}
	}
	if !foundRoom {
		t.Errorf("expected room=room1 on a room-scoped emit; got %v", emits)
	}

	subs := filterRels(res.Relationships, "WS_SUBSCRIBES_TO")
	if len(subs) < 1 {
		t.Errorf("expected WS_SUBSCRIBES_TO from socket.on('chat')")
	}
}

func TestWS_SocketIOClient_TransportsFallback(t *testing.T) {
	src := `import io from 'socket.io-client';
function start() {
  const s = io("/orders", { transports: ['polling', 'websocket'] });
}`
	res := runDetectWS(t, "typescript", "client.ts", src)
	conns := filterRels(res.Relationships, "WS_CONNECTS")
	if len(conns) < 1 {
		t.Fatalf("expected WS_CONNECTS from io(\"/orders\"), got %d", len(conns))
	}
	if conns[0].Properties["fallback"] != "long_poll" {
		t.Errorf("expected fallback=long_poll, got %q", conns[0].Properties["fallback"])
	}
}

// ---------------------------------------------------------------------------
// WebSocket — FastAPI
// ---------------------------------------------------------------------------

func TestWS_FastAPIWebSocket(t *testing.T) {
	src := `from fastapi import FastAPI, WebSocket

app = FastAPI()

@app.websocket("/ws/notify")
async def notify(ws: WebSocket, channel: str):
    await ws.accept()
`
	res := runDetectWS(t, "python", "app.py", src)
	chans := filterEntities(res.Entities, channelEventKind)
	if len(chans) == 0 || chans[0].ID != "ws:/ws/notify" {
		t.Fatalf("expected ws:/ws/notify ChannelEvent; got %v", chans)
	}
	subs := filterRels(res.Relationships, "WS_SUBSCRIBES_TO")
	if len(subs) < 1 {
		t.Errorf("expected WS_SUBSCRIBES_TO edge")
	}
}

// ---------------------------------------------------------------------------
// WebSocket — Ktor + Spring STOMP
// ---------------------------------------------------------------------------

func TestWS_KtorWebSocket(t *testing.T) {
	src := `routing {
  webSocket("/ws/chat") {
    for (frame in incoming) { }
  }
}`
	res := runDetectWS(t, "kotlin", "Routes.kt", src)
	chans := filterEntities(res.Entities, channelEventKind)
	if len(chans) == 0 || chans[0].ID != "ws:/ws/chat" {
		t.Fatalf("expected ws:/ws/chat; got %v", chans)
	}
}

func TestWS_SpringMessageMapping(t *testing.T) {
	src := `@Controller
public class ChatController {
    @MessageMapping("/chat.send")
    public void send(Message msg) {}
}`
	res := runDetectWS(t, "java", "ChatController.java", src)
	chans := filterEntities(res.Entities, channelEventKind)
	found := false
	for _, c := range chans {
		if strings.Contains(c.ID, "/chat.send") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected channel for /chat.send; got %v", chans)
	}
}

// ---------------------------------------------------------------------------
// WebSocket — non-WS file produces NO ChannelEvent (parity)
// ---------------------------------------------------------------------------

func TestWS_NonWSFile_NoChannels(t *testing.T) {
	src := `function add(a, b) { return a + b; }
const total = add(1, 2);
`
	res := runDetectWS(t, "javascript", "math.js", src)
	if cc := filterEntities(res.Entities, channelEventKind); len(cc) > 0 {
		t.Errorf("non-WS file produced ChannelEvent entities: %v", cc)
	}
	if rr := filterRels(res.Relationships, "WS_SUBSCRIBES_TO"); len(rr) > 0 {
		t.Errorf("non-WS file produced WS_SUBSCRIBES_TO edges: %v", rr)
	}
	if rr := filterRels(res.Relationships, "WS_CONNECTS"); len(rr) > 0 {
		t.Errorf("non-WS file produced WS_CONNECTS edges: %v", rr)
	}
}
