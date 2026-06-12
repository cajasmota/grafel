package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// realtimeEndpoints returns every http_endpoint_definition entity emitted by
// the realtime synthesis pass (i.e. those carrying realtime=true).
func realtimeEndpoints(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["realtime"] == "true" {
			out = append(out, e)
		}
	}
	return out
}

// findRealtimeEndpoint returns the realtime endpoint with the given synthetic
// ID, or nil.
func findRealtimeEndpoint(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id {
			return &ents[i]
		}
	}
	return nil
}

// handlesEdgeTo reports whether a HANDLES edge with the given FromID targets
// the endpoint ID.
func handlesEdgeTo(rels []types.RelationshipRecord, fromID, toID string) bool {
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindHandles) && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

func TestRealtimeNestJSSubscribeMessage(t *testing.T) {
	src := `
import { WebSocketGateway, SubscribeMessage, MessageBody } from '@nestjs/websockets';

@WebSocketGateway()
export class ChatGateway {
  @SubscribeMessage('chat')
  handleChat(@MessageBody() data: string): string {
    return data;
  }

  @SubscribeMessage('typing')
  async handleTyping(client: Socket): Promise<void> {}
}
`
	res := runDetectWS(t, "typescript", "chat.gateway.ts", src)

	ep := findRealtimeEndpoint(res.Entities, "http:WS:/chat")
	if ep == nil {
		t.Fatalf("expected WS endpoint http:WS:/chat for @SubscribeMessage('chat'); realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["verb"] != "WS" {
		t.Errorf("verb=%q, want WS", ep.Properties["verb"])
	}
	if ep.Properties["path"] != "/chat" {
		t.Errorf("path=%q, want /chat", ep.Properties["path"])
	}
	if ep.Properties["transport"] != "websocket" {
		t.Errorf("transport=%q, want websocket", ep.Properties["transport"])
	}
	if ep.Properties["event"] != "chat" {
		t.Errorf("event=%q, want chat", ep.Properties["event"])
	}
	if !handlesEdgeTo(res.Relationships, "Function:handleChat", "http:WS:/chat") {
		t.Errorf("expected HANDLES edge Function:handleChat -> http:WS:/chat")
	}
	// The second event must also be emitted.
	if findRealtimeEndpoint(res.Entities, "http:WS:/typing") == nil {
		t.Errorf("expected WS endpoint http:WS:/typing for @SubscribeMessage('typing')")
	}
}

func TestRealtimeNestJSSse(t *testing.T) {
	src := `
import { Controller, Sse } from '@nestjs/common';

@Controller('events')
export class EventsController {
  @Sse('stream')
  stream(): Observable<MessageEvent> {
    return interval(1000).pipe(map(() => ({ data: 'tick' })));
  }
}
`
	res := runDetectWS(t, "typescript", "events.controller.ts", src)
	ep := findRealtimeEndpoint(res.Entities, "http:SSE:/stream")
	if ep == nil {
		t.Fatalf("expected SSE endpoint http:SSE:/stream; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["verb"] != "SSE" {
		t.Errorf("verb=%q, want SSE", ep.Properties["verb"])
	}
	if ep.Properties["transport"] != "sse" {
		t.Errorf("transport=%q, want sse", ep.Properties["transport"])
	}
	if !handlesEdgeTo(res.Relationships, "Function:stream", "http:SSE:/stream") {
		t.Errorf("expected HANDLES edge Function:stream -> http:SSE:/stream")
	}
}

func TestRealtimeFastAPIWebSocket(t *testing.T) {
	src := `
from fastapi import FastAPI, WebSocket

app = FastAPI()

@app.websocket("/ws/notifications")
async def notifications(websocket: WebSocket):
    await websocket.accept()
`
	res := runDetectWS(t, "python", "main.py", src)
	ep := findRealtimeEndpoint(res.Entities, "http:WS:/ws/notifications")
	if ep == nil {
		t.Fatalf("expected WS endpoint http:WS:/ws/notifications; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["path"] != "/ws/notifications" {
		t.Errorf("path=%q, want /ws/notifications", ep.Properties["path"])
	}
	if ep.Properties["framework"] != "fastapi" {
		t.Errorf("framework=%q, want fastapi", ep.Properties["framework"])
	}
	if !handlesEdgeTo(res.Relationships, "Function:notifications", "http:WS:/ws/notifications") {
		t.Errorf("expected HANDLES edge Function:notifications -> http:WS:/ws/notifications")
	}
}

func TestRealtimeFastAPISSE(t *testing.T) {
	src := `
from fastapi import FastAPI
from sse_starlette.sse import EventSourceResponse

app = FastAPI()

@app.get("/events")
async def events():
    async def gen():
        yield {"data": "hello"}
    return EventSourceResponse(gen())
`
	res := runDetectWS(t, "python", "sse.py", src)
	ep := findRealtimeEndpoint(res.Entities, "http:SSE:/events")
	if ep == nil {
		t.Fatalf("expected SSE endpoint http:SSE:/events; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["transport"] != "sse" {
		t.Errorf("transport=%q, want sse", ep.Properties["transport"])
	}
	if !handlesEdgeTo(res.Relationships, "Function:events", "http:SSE:/events") {
		t.Errorf("expected HANDLES edge Function:events -> http:SSE:/events")
	}
}

func TestRealtimeSignalRHub(t *testing.T) {
	src := `
using Microsoft.AspNetCore.SignalR;

public class ChatHub : Hub
{
    public async Task SendMessage(string user, string message)
    {
        await Clients.All.SendAsync("ReceiveMessage", user, message);
    }

    public override async Task OnConnectedAsync()
    {
        await base.OnConnectedAsync();
    }
}

public class Startup
{
    public void Configure(IEndpointRouteBuilder endpoints)
    {
        endpoints.MapHub<ChatHub>("/chat");
    }
}
`
	res := runDetectWS(t, "csharp", "ChatHub.cs", src)
	ep := findRealtimeEndpoint(res.Entities, "http:WS:/chat/SendMessage")
	if ep == nil {
		t.Fatalf("expected realtime endpoint http:WS:/chat/SendMessage; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["transport"] != "signalr" {
		t.Errorf("transport=%q, want signalr", ep.Properties["transport"])
	}
	if ep.Properties["hub"] != "ChatHub" || ep.Properties["method"] != "SendMessage" {
		t.Errorf("hub/method = %q/%q, want ChatHub/SendMessage", ep.Properties["hub"], ep.Properties["method"])
	}
	if !handlesEdgeTo(res.Relationships, "Class:ChatHub.SendMessage", "http:WS:/chat/SendMessage") {
		t.Errorf("expected HANDLES edge Class:ChatHub.SendMessage -> http:WS:/chat/SendMessage")
	}
	// Lifecycle override must NOT be emitted as an endpoint.
	if findRealtimeEndpoint(res.Entities, "http:WS:/chat/OnConnectedAsync") != nil {
		t.Errorf("OnConnectedAsync must not be emitted as a realtime endpoint")
	}
}

func TestRealtimeSignalRHubDefaultBasePath(t *testing.T) {
	// No MapHub registration in-file: base path defaults to /<hub-without-suffix>.
	src := `
public class NotificationHub : Hub
{
    public Task Ping() => Task.CompletedTask;
}
`
	res := runDetectWS(t, "csharp", "NotificationHub.cs", src)
	if findRealtimeEndpoint(res.Entities, "http:WS:/notification/Ping") == nil {
		t.Fatalf("expected default-base realtime endpoint http:WS:/notification/Ping; realtime=%v", realtimeEndpoints(res.Entities))
	}
}

func TestRealtimeSignalRHubMethodNameOverride(t *testing.T) {
	// #5003: [HubMethodName("wire")] rebinds the client-facing method name, so
	// the realtime endpoint path uses the wire name, not the C# method name.
	// The HANDLES edge still points at the real C# method symbol.
	src := `
using Microsoft.AspNetCore.SignalR;

public class ChatHub : Hub
{
    [HubMethodName("send")]
    public async Task SendMessage(string user, string message)
    {
        await Clients.All.SendAsync("ReceiveMessage", user, message);
    }

    [Authorize]
    [HubMethodName("broadcast")]
    public Task Notify(string msg) => Task.CompletedTask;

    public Task Ping() => Task.CompletedTask;
}

public class Startup
{
    public void Configure(IEndpointRouteBuilder endpoints)
    {
        endpoints.MapHub<ChatHub>("/chat");
    }
}
`
	res := runDetectWS(t, "csharp", "ChatHub.cs", src)

	// Wire name from [HubMethodName("send")] drives the path.
	ep := findRealtimeEndpoint(res.Entities, "http:WS:/chat/send")
	if ep == nil {
		t.Fatalf("expected wire-named endpoint http:WS:/chat/send; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["method"] != "SendMessage" || ep.Properties["hub_method_name"] != "send" {
		t.Errorf("method/hub_method_name = %q/%q, want SendMessage/send", ep.Properties["method"], ep.Properties["hub_method_name"])
	}
	// HANDLES edge still references the real C# method symbol.
	if !handlesEdgeTo(res.Relationships, "Class:ChatHub.SendMessage", "http:WS:/chat/send") {
		t.Errorf("expected HANDLES edge Class:ChatHub.SendMessage -> http:WS:/chat/send")
	}
	// The C#-method-named path must NOT be emitted when overridden.
	if findRealtimeEndpoint(res.Entities, "http:WS:/chat/SendMessage") != nil {
		t.Errorf("C#-method-named endpoint http:WS:/chat/SendMessage must not be emitted when [HubMethodName] overrides it")
	}

	// Stacked attributes ([Authorize] + [HubMethodName]) still honored.
	if findRealtimeEndpoint(res.Entities, "http:WS:/chat/broadcast") == nil {
		t.Errorf("expected stacked-attribute wire endpoint http:WS:/chat/broadcast; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if findRealtimeEndpoint(res.Entities, "http:WS:/chat/Notify") != nil {
		t.Errorf("C#-method-named endpoint http:WS:/chat/Notify must not be emitted when [HubMethodName] overrides it")
	}

	// A method with no override keeps its C# name.
	if findRealtimeEndpoint(res.Entities, "http:WS:/chat/Ping") == nil {
		t.Errorf("expected un-overridden endpoint http:WS:/chat/Ping; realtime=%v", realtimeEndpoints(res.Entities))
	}
}

func TestRealtimePhoenixChannel(t *testing.T) {
	src := `
defmodule MyAppWeb.UserSocket do
  use Phoenix.Socket

  channel "room:*", MyAppWeb.RoomChannel
  channel "user:*", MyAppWeb.UserChannel
end
`
	res := runDetectWS(t, "elixir", "user_socket.ex", src)
	ep := findRealtimeEndpoint(res.Entities, "http:WS:room:*")
	if ep == nil {
		t.Fatalf("expected realtime endpoint http:WS:room:*; realtime=%v", realtimeEndpoints(res.Entities))
	}
	if ep.Properties["transport"] != "channels" {
		t.Errorf("transport=%q, want channels", ep.Properties["transport"])
	}
	if ep.Properties["channel_module"] != "MyAppWeb.RoomChannel" {
		t.Errorf("channel_module=%q, want MyAppWeb.RoomChannel", ep.Properties["channel_module"])
	}
	if !handlesEdgeTo(res.Relationships, "Class:MyAppWeb.RoomChannel", "http:WS:room:*") {
		t.Errorf("expected HANDLES edge Class:MyAppWeb.RoomChannel -> http:WS:room:*")
	}
}

func TestRealtimeSocketIOEvent(t *testing.T) {
	src := `
const { Server } = require('socket.io');
const io = new Server(httpServer);

io.on('connection', (socket) => {
  socket.on('message', (data) => { console.log(data); });
});
`
	res := runDetectWS(t, "javascript", "server.js", src)
	if findRealtimeEndpoint(res.Entities, "http:WS:/message") == nil {
		t.Fatalf("expected WS endpoint http:WS:/message for socket.on('message'); realtime=%v", realtimeEndpoints(res.Entities))
	}
	// Lifecycle 'connection' must not be an endpoint.
	if findRealtimeEndpoint(res.Entities, "http:WS:/connection") != nil {
		t.Errorf("'connection' must not be emitted as a realtime endpoint")
	}
}

// TestRealtimeNoOpOnInertFile guards against false positives: a file with no
// realtime anchors must emit zero realtime endpoints.
func TestRealtimeNoOpOnInertFile(t *testing.T) {
	src := `export function add(a: number, b: number): number { return a + b; }`
	res := runDetectWS(t, "typescript", "math.ts", src)
	if got := realtimeEndpoints(res.Entities); len(got) != 0 {
		t.Fatalf("expected 0 realtime endpoints on inert file, got %d: %v", len(got), got)
	}
}
