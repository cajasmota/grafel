// WebSocket room / channel grouping synthesis ([realtime], child of #3628).
//
// The per-event WS work (#3739, http_endpoint_ws_client.go +
// realtime_endpoint_synthesis.go) models a real-time *event* —
// `socket.emit('chat:message')` / `socket.on('notify')` — as an
// http:WS:/<event> endpoint. That is the message layer. Real-time systems
// also have a *grouping* layer on top of events: rooms / channels / groups /
// topics that participants JOIN and that messages are BROADCAST to. The same
// `emit('chat:message')` can be sent to room "lobby" or room "game:42"; the
// event identity does not capture which room. Before this pass the graph had
// no node for a room, so it could not answer "who joins / broadcasts to room
// X?".
//
// This append-only pass adds that grouping layer. For every statically
// identifiable literal room/channel name it emits a synthetic convergence
// node and an edge from the enclosing function:
//
//	SCOPE.Channel:<room>            the room/channel/group/topic node
//	JOINS_CHANNEL(fn → channel)     a participant is subscribed to the room
//	BROADCASTS_TO(fn → channel)     a message is published to the room
//
// Because the node ID is `SCOPE.Channel:<room>` (deduped per file by name), a
// JOIN and a BROADCAST on the SAME literal room CONVERGE on one node — the
// join that lets `expand`/`neighbors` answer the room-membership question.
//
// Frameworks (PRODUCER / server side — where rooms are managed):
//
//   - Socket.IO (JS/TS):
//       socket.join('room1')                       → JOINS_CHANNEL room1
//       io.to('room1').emit('ev')                  → BROADCASTS_TO room1
//       socket.broadcast.to('room1').emit('ev')    → BROADCASTS_TO room1
//       io.in('room1').emit('ev')                  → BROADCASTS_TO room1
//   - Rails ActionCable (ruby):
//       stream_from 'chat_1'                       → JOINS_CHANNEL chat_1
//       ActionCable.server.broadcast('chat_1', …)  → BROADCASTS_TO chat_1
//       ChatChannel.broadcast_to(room, …)          → (dynamic target → skip)
//   - Django Channels (python):
//       self.channel_layer.group_add('chat', …)    → JOINS_CHANNEL chat
//       self.channel_layer.group_send('chat', …)   → BROADCASTS_TO chat
//       async_to_sync(...group_send)('chat', …)    → BROADCASTS_TO chat
//   - Phoenix Channels (elixir):
//       broadcast(socket, "ev", payload)           → (topic from `topic "t"`/skip)
//       MyApp.Endpoint.broadcast("room:1", …)      → BROADCASTS_TO room:1
//
// Honest-partial / precision-first: a dynamic room name (a bare variable, a
// template literal, string interpolation) emits NO edge — a wrong membership
// edge would mislead the very question this pass exists to answer. A
// non-socket `.join` (e.g. `arr.join(',')`) is rejected by the receiver /
// file-context gates. All emissions are append-only — existing entities and
// edges are never touched, so this pass cannot regress surrounding passes.
//
// Refs #3628 (realtime), #3739 (per-event WS), realtime_endpoint_synthesis.go.

package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// channelKind is the entity kind for a single real-time room/channel node.
const channelKind = "SCOPE.Channel"

// ---------------------------------------------------------------------------
// Socket.IO (JS/TS)
// ---------------------------------------------------------------------------

// wsRoomSocketIOJoinRe captures `<recv>.join('room')` where the literal room
// is a single-quoted / double-quoted string. Capture 1 = receiver ident,
// 2 = room. Dynamic args (a bare ident or template literal) do not match.
var wsRoomSocketIOJoinRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\.\s*join\s*\(\s*['"]([^'"\r\n]+)['"]\s*\)`,
)

// wsRoomSocketIOBroadcastRe captures the room-targeted broadcast forms:
//
//	io.to('room').emit(...)        io.in('room').emit(...)
//	socket.to('room').emit(...)    socket.broadcast.to('room').emit(...)
//	socket.in('room').emit(...)
//
// Capture 1 = receiver ident (io|socket|…), 2 = the `to`/`in` selector room.
// The trailing `.emit(` anchor distinguishes a room broadcast from an
// unrelated `.to(` / `.in(` call. Dynamic rooms do not match.
var wsRoomSocketIOBroadcastRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*(?:\.\s*broadcast)?\s*\.\s*(?:to|in)\s*\(\s*['"]([^'"\r\n]+)['"]\s*\)\s*\.\s*emit\s*\(`,
)

// ---------------------------------------------------------------------------
// Rails ActionCable (ruby)
// ---------------------------------------------------------------------------

// wsRoomCableStreamFromRe captures `stream_from 'chat_1'` (literal channel).
var wsRoomCableStreamFromRe = regexp.MustCompile(
	`(?m)\bstream_from\s+['"]([^'"\r\n]+)['"]`,
)

// wsRoomCableServerBroadcastRe captures
// `ActionCable.server.broadcast('chat_1', …)` / `.server.broadcast("x", …)`
// with a literal channel name.
var wsRoomCableServerBroadcastRe = regexp.MustCompile(
	`\.\s*server\s*\.\s*broadcast\s*\(\s*['"]([^'"\r\n]+)['"]`,
)

// ---------------------------------------------------------------------------
// Django Channels (python)
// ---------------------------------------------------------------------------

// wsRoomChannelsGroupRe captures `…group_add('chat', …)` and
// `…group_send('chat', …)` (and group_discard, treated as a JOIN-side membership
// change). Capture 1 = method (group_add|group_send|group_discard), 2 = group.
// Anchored on the `group_` call so it is independent of how the channel layer
// is reached (`self.channel_layer.`, `get_channel_layer().`,
// `async_to_sync(self.channel_layer.group_send)` partial-application is handled
// separately below). Literal group only.
var wsRoomChannelsGroupRe = regexp.MustCompile(
	`\b(group_add|group_send|group_discard)\s*\(\s*['"]([^'"\r\n]+)['"]`,
)

// ---------------------------------------------------------------------------
// Phoenix Channels (elixir)
// ---------------------------------------------------------------------------

// wsRoomPhoenixEndpointBroadcastRe captures
// `MyApp.Endpoint.broadcast("room:1", …)` /
// `MyApp.Endpoint.broadcast_from(self(), "room:1", …)` — an explicit topic
// broadcast where the topic is a literal. Capture 1 = topic.
var wsRoomPhoenixEndpointBroadcastRe = regexp.MustCompile(
	`\.\s*broadcast(?:_from)?!?\s*\(\s*(?:[^,()"']+,\s*)?["']([^"'\r\n:]+:[^"'\r\n]+|[A-Za-z_][\w-]*)["']`,
)

// elixirDefRe captures Elixir `def`/`defp` function heads for enclosing-fn
// attribution (indexEnclosingFunctions has no elixir lane). Capture 1 = name.
var elixirDefRe = regexp.MustCompile(`(?m)^\s*defp?\s+([a-z_][\w?!]*)`)

// ---------------------------------------------------------------------------
// SignalR server→client push (csharp) — #5095, follow-up #5003
// ---------------------------------------------------------------------------

// wsRoomSignalRSendRe captures the SignalR *outbound* (server→client push)
// idiom inside a Hub method or an `IHubContext<XHub>`-holding service:
//
//	Clients.All.SendAsync("evt", ...)
//	Clients.Caller.SendAsync("evt", ...)
//	Clients.Others.InvokeAsync("evt", ...)
//	Clients.Group("g").SendAsync("evt", ...)
//	Clients.Client(id).SendAsync("evt", ...)
//	Clients.User("u").SendAsync("evt", ...)
//	_hubContext.Clients.All.SendAsync("evt", ...)
//
// Capture 1 = scope selector (All|Caller|Others|Group|Client|User|GroupExcept
// |AllExcept|OthersInGroup). Capture 2 (optional) = the RAW first argument to a
// parameterised scope (`Group("g")` / `Client(id)`), captured WITH its quotes
// (or unquoted for a variable) so the literal-vs-dynamic decision is made on
// the original token. Empty for the parameterless scopes (All/Caller/Others).
// Capture 3 = the pushed client method / event name (the first SendAsync/
// InvokeAsync string argument, always quoted-literal).
//
// The `.Clients.` prefix (vs a bare `Clients.`) is tolerated so an
// `IHubContext` field access matches too. The trailing `.SendAsync(`/
// `.InvokeAsync(` with a literal first arg anchors this to a real outbound
// push and rejects unrelated `Clients.` access.
var wsRoomSignalRSendRe = regexp.MustCompile(
	`\bClients\s*\.\s*(All|Caller|Others|Group|Client|User|GroupExcept|AllExcept|OthersInGroup)\s*(?:\(\s*([^,()\r\n]*?)\s*(?:,[^)]*)?\))?\s*\.\s*(?:SendAsync|InvokeAsync|SendCoreAsync)\s*\(\s*["']([^"'\r\n]+)["']`,
)

// applyWSChannelGrouping is the per-file entry point. Append-only.
func applyWSChannelGrouping(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seenNode := map[string]bool{}
	seenEdge := map[string]bool{}

	// emit registers a channel node (once per file) and one edge from the
	// enclosing function to it. room, caller and a valid edgeKind are all
	// required; an empty room or caller is skipped (honest-partial).
	emit := func(room, caller, framework, transport string, edgeKind types.RelationshipKind, line int) {
		room = strings.TrimSpace(room)
		if room == "" || caller == "" {
			return
		}
		nodeID := channelKind + ":" + room
		if !seenNode[nodeID] {
			seenNode[nodeID] = true
			entities = append(entities, types.EntityRecord{
				ID:         nodeID,
				Name:       "channel:" + room,
				Kind:       channelKind,
				SourceFile: path,
				Language:   lang,
				Properties: map[string]string{
					"channel":      room,
					"framework":    framework,
					"transport":    transport,
					"pattern_type": "ws_channel_grouping",
					"line":         strconv.Itoa(line),
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
		key := string(edgeKind) + "\x00" + "Function:" + caller + "\x00" + nodeID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "Function:" + caller,
			ToID:   nodeID,
			Kind:   string(edgeKind),
			Properties: map[string]string{
				"channel":      room,
				"framework":    framework,
				"transport":    transport,
				"pattern_type": "ws_channel_grouping",
			},
		})
	}

	// emitExtra is emit's variant that attaches per-call extra properties to the
	// edge (e.g. the SignalR pushed-event method name + scope). It shares the
	// same dedupe maps and node-emission behaviour; extra props are edge-only so
	// the converging channel node stays scope-identified, not method-identified.
	emitExtra := func(room, caller, framework, transport string, edgeKind types.RelationshipKind, line int, extra map[string]string) {
		room = strings.TrimSpace(room)
		if room == "" || caller == "" {
			return
		}
		nodeID := channelKind + ":" + room
		if !seenNode[nodeID] {
			seenNode[nodeID] = true
			entities = append(entities, types.EntityRecord{
				ID:         nodeID,
				Name:       "channel:" + room,
				Kind:       channelKind,
				SourceFile: path,
				Language:   lang,
				Properties: map[string]string{
					"channel":      room,
					"framework":    framework,
					"transport":    transport,
					"pattern_type": "ws_channel_grouping",
					"line":         strconv.Itoa(line),
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}
		// Dedupe key folds in the extra method name so two distinct pushed
		// events to the same scope from the same fn are both recorded.
		key := string(edgeKind) + "\x00" + "Function:" + caller + "\x00" + nodeID + "\x00" + extra["method"]
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		props := map[string]string{
			"channel":      room,
			"framework":    framework,
			"transport":    transport,
			"pattern_type": "ws_channel_grouping",
		}
		for k, v := range extra {
			if v != "" {
				props[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     "Function:" + caller,
			ToID:       nodeID,
			Kind:       string(edgeKind),
			Properties: props,
		})
	}

	lineAt := func(off int) int { return strings.Count(src[:off], "\n") + 1 }

	switch lang {
	case "javascript", "typescript":
		synthesizeSocketIORooms(src, indexEnclosingFunctions(lang, src), emit, lineAt)
	case "ruby":
		synthesizeActionCableRooms(src, indexEnclosingFunctions(lang, src), emit, lineAt)
	case "python":
		synthesizeDjangoChannelsGroups(src, indexEnclosingFunctions(lang, src), emit, lineAt)
	case "elixir":
		synthesizePhoenixRooms(src, emit, lineAt)
	case "csharp":
		synthesizeSignalROutbound(src, indexEnclosingFunctions(lang, src), emitExtra, lineAt)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// channelEmitFn is the closure shape the per-framework synthesizers receive.
type channelEmitFn func(room, caller, framework, transport string, edgeKind types.RelationshipKind, line int)

// dynamicRoom reports whether a captured room string is in fact a stable
// literal (false) or carries interpolation / placeholder syntax (true). The
// regexes already require quotes, so this catches interpolation *inside* a
// quoted string (`"chat_${id}"`, `"chat_#{id}"`, ERB-style, leading `+`).
func dynamicRoom(room string) bool {
	room = strings.TrimSpace(room)
	if room == "" {
		return true
	}
	return strings.ContainsAny(room, "`") ||
		strings.Contains(room, "${") ||
		strings.Contains(room, "#{") ||
		strings.HasPrefix(room, "+")
}

// ---------------------------------------------------------------------------
// Socket.IO synthesizer
// ---------------------------------------------------------------------------

// socketIORoomReceivers are the conventional socket.io handles that carry a
// connection/server and on which `.join` / `.to` / `.in` are meaningful. The
// receiver gate is what rejects a stray `arr.join(',')` (array join): `arr`
// is not a recognised socket handle, and the file-context gate below requires
// a socket.io marker to be present at all.
var socketIORoomReceivers = map[string]bool{
	"socket": true, "sock": true, "io": true, "client": true, "s": true, "ws": true,
}

func synthesizeSocketIORooms(src string, funcs []funcSpan, emit channelEmitFn, lineAt func(int) int) {
	// File-context gate: require a socket.io signal so a plain `.join`/`.to`
	// in an unrelated file is a no-op. Server create OR socket.io(-client)
	// import OR an `io.on('connection'` handler all qualify.
	if !socketIOServerCreateRe.MatchString(src) &&
		!wsClientConnectMarkerRe.MatchString(src) &&
		!strings.Contains(src, "socket.io") &&
		!strings.Contains(src, ".broadcast.to(") {
		return
	}

	// JOINS_CHANNEL: <recv>.join('room')
	for _, m := range wsRoomSocketIOJoinRe.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		room := src[m[4]:m[5]]
		if !socketIORoomReceivers[recv] {
			continue
		}
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(room, caller, "socket.io", "websocket", types.RelationshipKindJoinsChannel, lineAt(m[0]))
	}

	// BROADCASTS_TO: io.to('room').emit(...) / socket.broadcast.to('room').emit(...)
	for _, m := range wsRoomSocketIOBroadcastRe.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		room := src[m[4]:m[5]]
		if !socketIORoomReceivers[recv] {
			continue
		}
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(room, caller, "socket.io", "websocket", types.RelationshipKindBroadcastsTo, lineAt(m[0]))
	}
}

// ---------------------------------------------------------------------------
// Rails ActionCable synthesizer
// ---------------------------------------------------------------------------

func synthesizeActionCableRooms(src string, funcs []funcSpan, emit channelEmitFn, lineAt func(int) int) {
	// File-context gate: require an ActionCable signal.
	if !strings.Contains(src, "ActionCable") &&
		!strings.Contains(src, "Channel") &&
		!strings.Contains(src, "stream_from") {
		return
	}

	// JOINS_CHANNEL: stream_from 'chat_1'
	for _, m := range wsRoomCableStreamFromRe.FindAllStringSubmatchIndex(src, -1) {
		room := src[m[2]:m[3]]
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(room, caller, "actioncable", "websocket", types.RelationshipKindJoinsChannel, lineAt(m[0]))
	}

	// BROADCASTS_TO: ActionCable.server.broadcast('chat_1', …)
	for _, m := range wsRoomCableServerBroadcastRe.FindAllStringSubmatchIndex(src, -1) {
		room := src[m[2]:m[3]]
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		emit(room, caller, "actioncable", "websocket", types.RelationshipKindBroadcastsTo, lineAt(m[0]))
	}
}

// ---------------------------------------------------------------------------
// Django Channels synthesizer
// ---------------------------------------------------------------------------

func synthesizeDjangoChannelsGroups(src string, funcs []funcSpan, emit channelEmitFn, lineAt func(int) int) {
	// File-context gate: require a channel-layer signal.
	if !strings.Contains(src, "channel_layer") &&
		!strings.Contains(src, "group_add") &&
		!strings.Contains(src, "group_send") {
		return
	}

	for _, m := range wsRoomChannelsGroupRe.FindAllStringSubmatchIndex(src, -1) {
		method := src[m[2]:m[3]]
		room := src[m[4]:m[5]]
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		edgeKind := types.RelationshipKindJoinsChannel
		if method == "group_send" {
			edgeKind = types.RelationshipKindBroadcastsTo
		}
		emit(room, caller, "django_channels", "channels", edgeKind, lineAt(m[0]))
	}
}

// ---------------------------------------------------------------------------
// Phoenix Channels synthesizer
// ---------------------------------------------------------------------------

func synthesizePhoenixRooms(src string, emit channelEmitFn, lineAt func(int) int) {
	// File-context gate: require a Phoenix channel / endpoint broadcast signal.
	if !strings.Contains(src, "broadcast") ||
		(!strings.Contains(src, "Phoenix") &&
			!strings.Contains(src, "use ") &&
			!strings.Contains(src, "Endpoint")) {
		return
	}

	funcs := indexElixirDefs(src)

	// BROADCASTS_TO: MyApp.Endpoint.broadcast("room:1", …) — explicit literal topic.
	for _, m := range wsRoomPhoenixEndpointBroadcastRe.FindAllStringSubmatchIndex(src, -1) {
		room := src[m[2]:m[3]]
		if dynamicRoom(room) {
			continue
		}
		caller := enclosingFuncAt(funcs, m[0])
		if caller == "" {
			continue
		}
		emit(room, caller, "phoenix_channels", "channels", types.RelationshipKindBroadcastsTo, lineAt(m[0]))
	}
}

// ---------------------------------------------------------------------------
// SignalR outbound synthesizer (csharp) — #5095
// ---------------------------------------------------------------------------

// channelExtraEmitFn is emitExtra's closure shape: emit plus per-call edge
// properties.
type channelExtraEmitFn func(room, caller, framework, transport string, edgeKind types.RelationshipKind, line int, extra map[string]string)

// synthesizeSignalROutbound models SignalR server→client push calls
// (`Clients.<scope>.SendAsync("evt", ...)`) as BROADCASTS_TO edges onto a
// per-scope `SCOPE.Channel:signalr:<scope>` convergence node, so the graph can
// answer "which events does the server push, and to which client scope". The
// pushed event/method name + scope ride on the edge as properties.
//
// The channel node is keyed by client *scope* (All / Caller / Others / a named
// Group / a Client / a User), not by event — multiple events to the same scope
// converge on one node, mirroring the room-convergence model the JS/Ruby/Python
// lanes use. A parameterised scope with a stable literal argument
// (`Group("lobby")`) folds the literal into the node id (`signalr:group:lobby`)
// so per-group broadcasts stay distinct; a dynamic argument
// (`Group(roomVar)` / `Client(id)`) degrades to the bare scope node
// (`signalr:group`) rather than guessing a wrong group name.
func synthesizeSignalROutbound(src string, funcs []funcSpan, emit channelExtraEmitFn, lineAt func(int) int) {
	// File-context gate: require a SignalR push signal so a stray `Clients.`
	// access in an unrelated file is a no-op.
	if !strings.Contains(src, "Clients.") ||
		(!strings.Contains(src, "SendAsync") &&
			!strings.Contains(src, "InvokeAsync") &&
			!strings.Contains(src, "SendCoreAsync")) {
		return
	}

	for _, m := range wsRoomSignalRSendRe.FindAllStringSubmatchIndex(src, -1) {
		scope := src[m[2]:m[3]]
		rawArg := ""
		if m[4] >= 0 {
			rawArg = strings.TrimSpace(src[m[4]:m[5]])
		}
		method := src[m[6]:m[7]]

		// Resolve the scope argument: a quoted literal (`Group("lobby")`) folds
		// into the node id; a bare variable / interpolated value
		// (`Group(roomVar)`) is dynamic and degrades to the bare scope node
		// rather than guessing a wrong group name (honest-partial).
		litArg, isLiteral := signalRLiteralArg(rawArg)

		// Build the per-scope channel node id.
		room := "signalr:" + strings.ToLower(scope)
		if isLiteral && litArg != "" {
			room += ":" + litArg
		}

		caller := enclosingFuncAt(funcs, m[0])
		if caller == "" {
			continue
		}
		extra := map[string]string{
			"signalr_scope": scope,
			"method":        method,
		}
		if isLiteral && litArg != "" {
			extra["scope_arg"] = litArg
		}
		emit(room, caller, "signalr", "signalr", types.RelationshipKindBroadcastsTo, lineAt(m[0]), extra)
	}
}

// signalRLiteralArg inspects a raw scope argument (captured WITH any quotes).
// A single/double-quoted string is a stable literal — its unquoted value and
// true are returned. Anything else (a bare identifier, a method call, an
// interpolated string) is dynamic — "" and false are returned.
func signalRLiteralArg(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	q := raw[0]
	if (q != '"' && q != '\'') || raw[len(raw)-1] != q {
		return "", false
	}
	inner := raw[1 : len(raw)-1]
	if inner == "" || strings.ContainsAny(inner, "${}") || strings.Contains(inner, "#{") {
		return "", false
	}
	return inner, true
}

// indexElixirDefs returns def/defp spans for enclosing-fn attribution.
func indexElixirDefs(src string) []funcSpan {
	var out []funcSpan
	for _, m := range elixirDefRe.FindAllStringSubmatchIndex(src, -1) {
		out = append(out, funcSpan{offset: m[0], name: src[m[2]:m[3]]})
	}
	return out
}
