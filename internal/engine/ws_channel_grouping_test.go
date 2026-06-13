// Tests for the WebSocket room/channel grouping pass ([realtime], child of
// #3628). Every positive case asserts the concrete SCOPE.Channel node ID, the
// edge KIND (JOINS_CHANNEL vs BROADCASTS_TO), the FromID (Function:<caller>),
// AND — the load-bearing property of a grouping layer — that a JOIN and a
// BROADCAST on the same literal room CONVERGE on one node. Negative cases
// assert honest-partial skips (dynamic room, array join). Never a bare len>0.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func runWSChannelDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyWSChannelGrouping(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// hasChannelNode reports whether a SCOPE.Channel node with the given ID + Name
// exists exactly once.
func channelNodeCount(ents []types.EntityRecord, id string) int {
	n := 0
	for _, e := range ents {
		if e.Kind == channelKind && e.ID == id {
			n++
		}
	}
	return n
}

func hasChannelNode(ents []types.EntityRecord, id, name string) bool {
	for _, e := range ents {
		if e.Kind == channelKind && e.ID == id && e.Name == name {
			return true
		}
	}
	return false
}

// hasChannelEdge reports whether an edge of the given kind from Function:<caller>
// → the channel node ID exists.
func hasChannelEdge(rels []types.RelationshipRecord, kind types.RelationshipKind, caller, channelID string) bool {
	for _, r := range rels {
		if r.Kind == string(kind) && r.FromID == "Function:"+caller && r.ToID == channelID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Socket.IO — convergence of join + broadcast on Channel:lobby
// ---------------------------------------------------------------------------

func TestSocketIORoomConvergence(t *testing.T) {
	src := `
import { Server } from 'socket.io';
const io = new Server();
io.on('connection', (socket) => {
  function onConnect() {
    socket.join('lobby');
  }
  function send() {
    io.to('lobby').emit('msg', { text: 'hi' });
  }
});
`
	ents, rels := runWSChannelDetect(t, "typescript", "server.ts", src)

	const id = "SCOPE.Channel:lobby"
	if !hasChannelNode(ents, id, "channel:lobby") {
		t.Fatalf("missing SCOPE.Channel:lobby node; ents=%v", ents)
	}
	if c := channelNodeCount(ents, id); c != 1 {
		t.Fatalf("expected exactly 1 lobby node (convergence), got %d", c)
	}
	if !hasChannelEdge(rels, types.RelationshipKindJoinsChannel, "onConnect", id) {
		t.Errorf("missing JOINS_CHANNEL(onConnect → %s)", id)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "send", id) {
		t.Errorf("missing BROADCASTS_TO(send → %s)", id)
	}
}

func TestSocketIOBroadcastVariants(t *testing.T) {
	src := `
const io = require('socket.io')();
io.on('connection', (socket) => {
  function a() { socket.broadcast.to('game:42').emit('move', {}); }
  function b() { io.in('game:42').emit('state', {}); }
});
`
	ents, rels := runWSChannelDetect(t, "javascript", "s.js", src)
	const id = "SCOPE.Channel:game:42"
	if !hasChannelNode(ents, id, "channel:game:42") {
		t.Fatalf("missing %s node", id)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "a", id) {
		t.Errorf("missing BROADCASTS_TO(a → %s) via socket.broadcast.to", id)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "b", id) {
		t.Errorf("missing BROADCASTS_TO(b → %s) via io.in", id)
	}
}

// Negative: dynamic room (bare var) → no edge; array .join → no node.
func TestSocketIONegatives(t *testing.T) {
	src := `
const io = require('socket.io')();
io.on('connection', (socket) => {
  function dyn(roomVar) { socket.join(roomVar); io.to(roomVar).emit('x'); }
  function arr() { const parts = ['a','b']; return parts.join(','); }
});
`
	ents, rels := runWSChannelDetect(t, "javascript", "s.js", src)
	if len(ents) != 0 {
		t.Errorf("expected no channel nodes for dynamic room / array join, got %v", ents)
	}
	if len(rels) != 0 {
		t.Errorf("expected no edges for dynamic room / array join, got %v", rels)
	}
}

func TestSocketIOInterpolatedRoomSkipped(t *testing.T) {
	src := "const io = require('socket.io')();\n" +
		"io.on('connection', (socket) => {\n" +
		"  function j(id) { socket.join(`room_${id}`); }\n" +
		"});\n"
	ents, rels := runWSChannelDetect(t, "javascript", "s.js", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("template-literal room must be skipped; ents=%v rels=%v", ents, rels)
	}
}

// Gate: a `.join` in a file with NO socket.io signal must not emit.
func TestNoSocketContextNoEmit(t *testing.T) {
	src := `function f() { const parts = ['a','b']; return parts.join(','); }`
	ents, rels := runWSChannelDetect(t, "javascript", "util.js", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("array join in non-socket file must not emit; ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// Rails ActionCable — stream_from join + server.broadcast converge on chat_1
// ---------------------------------------------------------------------------

func TestActionCableRoomConvergence(t *testing.T) {
	src := `
class ChatChannel < ApplicationCable::Channel
  def subscribed
    stream_from 'chat_1'
  end

  def notify
    ActionCable.server.broadcast('chat_1', message: 'hi')
  end
end
`
	ents, rels := runWSChannelDetect(t, "ruby", "chat_channel.rb", src)
	const id = "SCOPE.Channel:chat_1"
	if !hasChannelNode(ents, id, "channel:chat_1") {
		t.Fatalf("missing %s node; ents=%v", id, ents)
	}
	if c := channelNodeCount(ents, id); c != 1 {
		t.Fatalf("expected exactly 1 chat_1 node (convergence), got %d", c)
	}
	if !hasChannelEdge(rels, types.RelationshipKindJoinsChannel, "subscribed", id) {
		t.Errorf("missing JOINS_CHANNEL(subscribed → %s)", id)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "notify", id) {
		t.Errorf("missing BROADCASTS_TO(notify → %s)", id)
	}
}

func TestActionCableDynamicStreamSkipped(t *testing.T) {
	src := `
class RoomChannel < ApplicationCable::Channel
  def subscribed
    stream_for room
  end
end
`
	ents, rels := runWSChannelDetect(t, "ruby", "room_channel.rb", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("stream_for <var> (dynamic) must be skipped; ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// Django Channels — group_add join + group_send broadcast converge on chat
// ---------------------------------------------------------------------------

func TestDjangoChannelsGroupConvergence(t *testing.T) {
	src := `
class ChatConsumer(AsyncWebsocketConsumer):
    async def connect(self):
        await self.channel_layer.group_add('chat', self.channel_name)

    async def broadcast(self):
        await self.channel_layer.group_send('chat', {'type': 'msg'})
`
	ents, rels := runWSChannelDetect(t, "python", "consumers.py", src)
	const id = "SCOPE.Channel:chat"
	if !hasChannelNode(ents, id, "channel:chat") {
		t.Fatalf("missing %s node; ents=%v", id, ents)
	}
	if c := channelNodeCount(ents, id); c != 1 {
		t.Fatalf("expected exactly 1 chat node (convergence), got %d", c)
	}
	if !hasChannelEdge(rels, types.RelationshipKindJoinsChannel, "connect", id) {
		t.Errorf("missing JOINS_CHANNEL(connect → %s)", id)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "broadcast", id) {
		t.Errorf("missing BROADCASTS_TO(broadcast → %s)", id)
	}
}

func TestDjangoChannelsDynamicGroupSkipped(t *testing.T) {
	src := `
class ChatConsumer(AsyncWebsocketConsumer):
    async def connect(self):
        await self.channel_layer.group_add(self.group_name, self.channel_name)
`
	ents, rels := runWSChannelDetect(t, "python", "consumers.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("dynamic group name must be skipped; ents=%v rels=%v", ents, rels)
	}
}

// ---------------------------------------------------------------------------
// Phoenix — Endpoint.broadcast literal topic
// ---------------------------------------------------------------------------

func TestPhoenixEndpointBroadcast(t *testing.T) {
	src := `
defmodule MyAppWeb.RoomChannel do
  use Phoenix.Channel

  def handle_in("publish", payload, socket) do
    MyAppWeb.Endpoint.broadcast("room:42", "new_msg", payload)
    {:noreply, socket}
  end
end
`
	ents, rels := runWSChannelDetect(t, "elixir", "room_channel.ex", src)
	const id = "SCOPE.Channel:room:42"
	if !hasChannelNode(ents, id, "channel:room:42") {
		t.Fatalf("missing %s node; ents=%v", id, ents)
	}
	if !hasChannelEdge(rels, types.RelationshipKindBroadcastsTo, "handle_in", id) {
		t.Errorf("missing BROADCASTS_TO(handle_in → %s); rels=%v", id, rels)
	}
}

// ---------------------------------------------------------------------------
// SignalR outbound server→client push (csharp) — #5095
// ---------------------------------------------------------------------------

// signalREdge reports whether a BROADCASTS_TO edge Function:<caller> →
// SCOPE.Channel:<room> exists carrying the given scope + pushed method props.
func signalREdge(rels []types.RelationshipRecord, caller, channelID, scope, method string) bool {
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindBroadcastsTo) &&
			r.FromID == "Function:"+caller && r.ToID == channelID &&
			r.Properties["signalr_scope"] == scope && r.Properties["method"] == method {
			return true
		}
	}
	return false
}

func TestSignalROutboundScopes(t *testing.T) {
	src := `
using Microsoft.AspNetCore.SignalR;

public class ChatHub : Hub
{
    public async Task SendMessage(string user, string message)
    {
        await Clients.All.SendAsync("ReceiveMessage", user, message);
        await Clients.Caller.SendAsync("Ack");
        await Clients.Group("lobby").SendAsync("RoomMessage", message);
    }
}
`
	ents, rels := runWSChannelDetect(t, "csharp", "ChatHub.cs", src)

	// Clients.All → signalr:all
	if !hasChannelNode(ents, "SCOPE.Channel:signalr:all", "channel:signalr:all") {
		t.Fatalf("missing signalr:all node; ents=%v", ents)
	}
	if !signalREdge(rels, "SendMessage", "SCOPE.Channel:signalr:all", "All", "ReceiveMessage") {
		t.Errorf("missing BROADCASTS_TO(SendMessage → signalr:all, All/ReceiveMessage); rels=%v", rels)
	}
	// Clients.Caller → signalr:caller
	if !signalREdge(rels, "SendMessage", "SCOPE.Channel:signalr:caller", "Caller", "Ack") {
		t.Errorf("missing BROADCASTS_TO(SendMessage → signalr:caller, Caller/Ack); rels=%v", rels)
	}
	// Clients.Group("lobby") → literal group folded into node id
	if !hasChannelNode(ents, "SCOPE.Channel:signalr:group:lobby", "channel:signalr:group:lobby") {
		t.Fatalf("missing signalr:group:lobby node; ents=%v", ents)
	}
	if !signalREdge(rels, "SendMessage", "SCOPE.Channel:signalr:group:lobby", "Group", "RoomMessage") {
		t.Errorf("missing BROADCASTS_TO(SendMessage → signalr:group:lobby, Group/RoomMessage); rels=%v", rels)
	}
	// The literal group arg rides on the edge.
	foundArg := false
	for _, r := range rels {
		if r.ToID == "SCOPE.Channel:signalr:group:lobby" && r.Properties["scope_arg"] == "lobby" {
			foundArg = true
		}
	}
	if !foundArg {
		t.Errorf("expected scope_arg=lobby on the group broadcast edge; rels=%v", rels)
	}
}

func TestSignalROutboundHubContext(t *testing.T) {
	// A service holding IHubContext<XHub> pushes via _hubContext.Clients.*.
	src := `
public class NotificationService
{
    private readonly IHubContext<ChatHub> _hub;

    public async Task Notify(string msg)
    {
        await _hub.Clients.All.SendAsync("Notify", msg);
    }
}
`
	ents, rels := runWSChannelDetect(t, "csharp", "NotificationService.cs", src)
	if !hasChannelNode(ents, "SCOPE.Channel:signalr:all", "channel:signalr:all") {
		t.Fatalf("missing signalr:all node from IHubContext push; ents=%v", ents)
	}
	if !signalREdge(rels, "Notify", "SCOPE.Channel:signalr:all", "All", "Notify") {
		t.Errorf("missing BROADCASTS_TO(Notify → signalr:all, All/Notify); rels=%v", rels)
	}
}

func TestSignalROutboundDynamicGroupDegrades(t *testing.T) {
	// A dynamic Group(variable) must NOT guess a group name — it degrades to the
	// bare scope node and carries no scope_arg (honest-partial).
	src := `
using Microsoft.AspNetCore.SignalR;

public class ChatHub : Hub
{
    public async Task SendToRoom(string room, string message)
    {
        await Clients.Group(room).SendAsync("RoomMessage", message);
    }
}
`
	ents, rels := runWSChannelDetect(t, "csharp", "ChatHub.cs", src)
	// Bare-scope node only; no guessed group name.
	if !hasChannelNode(ents, "SCOPE.Channel:signalr:group", "channel:signalr:group") {
		t.Fatalf("expected degraded signalr:group node; ents=%v", ents)
	}
	for _, e := range ents {
		if e.ID == "SCOPE.Channel:signalr:group:room" {
			t.Errorf("dynamic Group(room) must NOT produce a literal-named node")
		}
	}
	for _, r := range rels {
		if r.ToID == "SCOPE.Channel:signalr:group" && r.Properties["scope_arg"] != "" {
			t.Errorf("dynamic group must carry no scope_arg; got %q", r.Properties["scope_arg"])
		}
	}
}

func TestSignalROutboundWrongLangNoOp(t *testing.T) {
	// The same text in a non-csharp file must be a no-op (lang-gated switch).
	src := `await Clients.All.SendAsync("ReceiveMessage", user);`
	ents, rels := runWSChannelDetect(t, "javascript", "x.js", src)
	for _, e := range ents {
		if e.Kind == channelKind && len(e.ID) >= len("SCOPE.Channel:signalr") &&
			e.ID[:len("SCOPE.Channel:signalr")] == "SCOPE.Channel:signalr" {
			t.Errorf("SignalR outbound must not fire on javascript; ents=%v", ents)
		}
	}
	_ = rels
}

func TestSignalROutboundNoMatchNoOp(t *testing.T) {
	// A csharp file with no SignalR push idiom emits nothing.
	src := `
public class Helper
{
    public void Run()
    {
        var clients = GetClients();
        clients.Process();
    }
}
`
	ents, rels := runWSChannelDetect(t, "csharp", "Helper.cs", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-SignalR csharp must be a no-op; ents=%v rels=%v", ents, rels)
	}
}

// All emitted edges must use a registered relationship kind, and all nodes a
// registered entity kind (producer-boundary contract).
func TestWSChannelKindsRegistered(t *testing.T) {
	if !types.IsValidEntityKind(channelKind) {
		t.Fatalf("%s is not a registered entity kind", channelKind)
	}
	for _, k := range []types.RelationshipKind{
		types.RelationshipKindJoinsChannel, types.RelationshipKindBroadcastsTo,
	} {
		if !types.IsValidRelationshipKind(string(k)) {
			t.Fatalf("%s is not a registered relationship kind", k)
		}
	}
}
