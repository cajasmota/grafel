// Realtime-endpoint synthesis (#3682, epic #3628 area #7).
//
// The pre-existing realtime passes (websocket_edges.go, sse_edges.go,
// graphql_subscriptions.go) model realtime traffic as *edge annotations*
// onto bespoke `ChannelEvent` / `Stream` / `Subscription` entities. Those
// entities are NOT endpoint-shaped: they carry no `verb` + `route_path`
// identity, are not classified by the `endpoints` / `find` MCP tools, and
// cannot be cross-linked the way HTTP routes are.
//
// This pass adds the endpoint-shaped, queryable, cross-linkable view on
// top — it does NOT replace the channel/stream passes. For every realtime
// handler the extractor can statically identify on the PRODUCER side it
// emits a synthetic entity that is shaped exactly like an HTTP route:
//
//   - Kind = `http_endpoint_definition` (the same kind HTTP routes use) so
//     `endpoints` / `find` surface realtime endpoints with zero MCP-layer
//     changes. The `realtime=true` + `transport=...` properties let callers
//     filter realtime endpoints apart from REST when they want to.
//   - `verb` = `WS` (websocket / socket.io / signalr / channels) or `SSE`.
//   - `path` = the channel / route / event path, canonicalised.
//   - Synthetic ID `http:WS:<path>` / `http:SSE:<path>` built with the same
//     httproutes.SyntheticID the HTTP synthesizer uses, so a server's
//     `WS /ws/notifications` and a browser's `new WebSocket(".../ws/notifications")`
//     collapse onto one identity for the cross-repo linker (the WS client
//     side is still recorded as a WS_CONNECTS edge by websocket_edges.go;
//     the shared synthetic-ID path is the cross-stack pivot).
//   - A HANDLES edge from the handler symbol → the realtime endpoint entity,
//     mirroring the GraphQL operation-endpoint shape (spring_graphql.go).
//     The handler FromID uses the same `Function:<name>` / `Class:<name>`
//     convention the WS/SSE edge passes already emit, so the standard
//     resolve pass binds it to the stamped handler entity.
//
// Frameworks covered (producer side):
//
//   - JS/TS  NestJS  `@WebSocketGateway()` class + `@SubscribeMessage('evt')`
//     methods → `WS /<evt>` + HANDLES to the gateway method.
//     NestJS  `@Sse('path')` → `SSE /path` + HANDLES to the method.
//     socket.io `socket.on('evt', ...)` → `WS /<evt>`.
//     bare ws  `new WebSocketServer(...)` → `WS /ws` (or the literal
//     `path:` option when present).
//   - Python FastAPI `@app.websocket("/ws/...")` → `WS /ws/...` + HANDLES.
//     FastAPI/Starlette SSE: `EventSourceResponse(...)` (sse-starlette)
//     or `StreamingResponse(..., media_type="text/event-stream")`
//     returned from an `@app.get("/path")` handler → `SSE /path`.
//   - C#     SignalR `class ChatHub : Hub` invokable methods → `WS /<hub>/<method>`;
//     `app.MapHub<ChatHub>("/chat")` rebinds the hub's base path
//     so `ChatHub.SendMessage` mapped at `/chat` becomes
//     `WS /chat/SendMessage`.
//   - Elixir Phoenix `channel "room:*", RoomChannel` → `WS room:*` + HANDLES
//     to the channel module.
//
// Honest-partial: when a path/channel is fully dynamic (interpolated at
// runtime) the endpoint is emitted only if a stable literal segment can be
// derived; otherwise it is skipped rather than guessed.
//
// Refs #3682.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

// realtimeVerbWS / realtimeVerbSSE are the synthetic HTTP "verbs" used to
// distinguish realtime endpoints from REST routes while keeping them inside
// the http_endpoint_definition kind so `endpoints` / `find` surface them.
const (
	realtimeVerbWS  = "WS"
	realtimeVerbSSE = "SSE"
)

// applyRealtimeEndpointSynthesis is the per-file entry point. Append-only:
// it never modifies or removes existing entities/edges. No-op for content
// that contains none of the per-framework anchors.
func applyRealtimeEndpointSynthesis(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seen := map[string]bool{}

	// emitEndpoint appends one realtime endpoint entity (deduped per file by
	// synthetic ID) and a HANDLES edge from the handler symbol to it. The
	// handlerRef is a kind-qualified reference (`Function:foo` / `Class:Bar`)
	// that the standard resolve pass binds to the stamped handler entity; an
	// empty handlerRef emits the entity with no HANDLES edge (honest: no
	// statically-addressable handler).
	emitEndpoint := func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string) {
		canonical := canonicalizeRealtimePath(transport, rawPath)
		if canonical == "" {
			return
		}
		id := httproutes.SyntheticID(verb, canonical)
		if seen[id] {
			return
		}
		seen[id] = true

		props := map[string]string{
			"verb":         verb,
			"path":         canonical,
			"framework":    framework,
			"realtime":     "true",
			"transport":    transport,
			"pattern_type": "realtime_endpoint_synthesis",
		}
		for k, v := range extra {
			if v != "" {
				props[k] = v
			}
		}
		if handlerRef != "" {
			props["source_handler"] = handlerRef
		}
		entities = append(entities, types.EntityRecord{
			ID:                 id,
			Name:               id,
			QualifiedName:      id,
			Kind:               httpEndpointDefinitionKind,
			SourceFile:         path,
			Language:           lang,
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})

		if handlerRef != "" {
			relationships = append(relationships, types.RelationshipRecord{
				FromID: handlerRef,
				ToID:   id,
				Kind:   string(types.RelationshipKindHandles),
				Properties: map[string]string{
					"framework":    framework,
					"transport":    transport,
					"pattern_type": "realtime_endpoint_synthesis",
				},
			})
		}
	}

	switch lang {
	case "javascript", "typescript":
		synthNestWebSocketGateway(src, emitEndpoint)
		synthNestSse(src, emitEndpoint)
		synthSocketIORealtimeEndpoints(src, emitEndpoint)
		synthBareWSRealtimeEndpoint(src, emitEndpoint)
	case "python":
		synthFastAPIRealtimeWS(src, emitEndpoint)
		synthPythonSSEEndpoint(src, emitEndpoint)
	case "csharp":
		synthSignalRRealtimeEndpoints(src, emitEndpoint)
	case "elixir":
		synthPhoenixChannelEndpoints(src, emitEndpoint)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// canonicalizeRealtimePath normalises a realtime path/channel for stable
// synthetic identity. WebSocket / SSE URL forms are stripped to their path
// component (host removed) so a server `/ws/x` and a browser
// `wss://host/ws/x` collapse. socket.io event names and Phoenix channel
// topics (which are not URL paths, e.g. `chat` or `room:*`) pass through
// trimmed. Empty / placeholder-only inputs return "".
func canonicalizeRealtimePath(transport, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch transport {
	case "websocket", "sse", "signalr":
		// URL/path-shaped. Strip scheme+host if present.
		if strings.Contains(raw, "://") {
			raw = stripWSHost(raw)
		}
		// Drop query/fragment.
		if q := strings.IndexAny(raw, "?#"); q >= 0 {
			raw = raw[:q]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		// Run through the shared canonicaliser so `:id`/`{id}` placeholders
		// match the HTTP route convention. Express canonicaliser is path-safe.
		if strings.HasPrefix(raw, "/") {
			if c := httproutes.Canonicalize(httproutes.FrameworkExpress, raw); c != "" {
				return c
			}
		}
		return raw
	default:
		// Event/topic name (socket.io event, Phoenix topic). Keep as-is but
		// reject pure interpolation.
		if raw == "*" || strings.HasPrefix(raw, "${") || strings.HasPrefix(raw, "+") {
			return ""
		}
		return raw
	}
}

// ---------------------------------------------------------------------------
// NestJS — @WebSocketGateway() + @SubscribeMessage('event')
// ---------------------------------------------------------------------------

// nestSubscribeMessageRe captures `@SubscribeMessage('event')` immediately
// preceding a (possibly async) method declaration, capturing the event name
// and the handler method name. Stacked decorators between the
// @SubscribeMessage and the method (e.g. @UsePipes) are tolerated.
var nestSubscribeMessageRe = regexp.MustCompile(
	`@SubscribeMessage\s*\(\s*['"]([^'"\r\n]+)['"]\s*\)\s*(?:[\r\n]\s*@[^\r\n]*)*\s*(?:public\s+|private\s+|protected\s+)?(?:async\s+)?(\w+)\s*\(`,
)

func synthNestWebSocketGateway(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	// Gate on the gateway decorator so this is a no-op on non-Nest files
	// that happen to contain a `@SubscribeMessage` string in a comment.
	if !strings.Contains(src, "@WebSocketGateway") && !strings.Contains(src, "@SubscribeMessage") {
		return
	}
	for _, m := range nestSubscribeMessageRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		event := m[1]
		method := m[2]
		emit(
			realtimeVerbWS,
			"/"+strings.TrimPrefix(event, "/"),
			"nestjs",
			"websocket",
			"Function:"+method,
			map[string]string{"event": event},
		)
	}
}

// ---------------------------------------------------------------------------
// NestJS — @Sse('path')
// ---------------------------------------------------------------------------

var nestSseRe = regexp.MustCompile(
	`@Sse\s*\(\s*['"]([^'"\r\n]+)['"]\s*\)\s*(?:[\r\n]\s*@[^\r\n]*)*\s*(?:public\s+|private\s+|protected\s+)?(?:async\s+)?(\w+)\s*\(`,
)

func synthNestSse(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !strings.Contains(src, "@Sse(") {
		return
	}
	for _, m := range nestSseRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		path := m[1]
		method := m[2]
		emit(
			realtimeVerbSSE,
			"/"+strings.TrimPrefix(path, "/"),
			"nestjs",
			"sse",
			"Function:"+method,
			nil,
		)
	}
}

// ---------------------------------------------------------------------------
// socket.io server — socket.on('event', handler)
// ---------------------------------------------------------------------------

// socketIORealtimeOnRe mirrors socketIOEventOnRe in websocket_edges.go but is
// scoped to this pass's endpoint emission. We reuse the same anchor logic.
var socketIORealtimeOnRe = regexp.MustCompile(
	`\b(?:socket|sock|client|s)\s*\.\s*on\s*\(\s*['"]([^'"\r\n]+)['"]\s*,\s*(?:async\s*)?(?:function\s*\(|\(?[\w$,\s]*\)?\s*=>)`,
)

func synthSocketIORealtimeEndpoints(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !socketIOServerCreateRe.MatchString(src) && !strings.Contains(src, "socket.on(") {
		return
	}
	for _, m := range socketIORealtimeOnRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		event := m[1]
		switch event {
		case "connection", "disconnect", "connect", "error", "disconnecting":
			continue
		}
		emit(
			realtimeVerbWS,
			"/"+event,
			"socket.io",
			"websocket",
			"Function:socketio_on_"+sanitiseID(event),
			map[string]string{"event": event},
		)
	}
}

// ---------------------------------------------------------------------------
// bare ws server — new WebSocketServer(...)
// ---------------------------------------------------------------------------

func synthBareWSRealtimeEndpoint(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !bareWSServerCreateRe.MatchString(src) {
		return
	}
	channel := "/ws"
	if mm := regexp.MustCompile(`path\s*:\s*['"]([^'"\r\n]+)['"]`).FindStringSubmatch(src); len(mm) >= 2 {
		channel = mm[1]
	}
	emit(realtimeVerbWS, channel, "ws", "websocket", "Function:ws_on_message", nil)
}

// ---------------------------------------------------------------------------
// FastAPI — @app.websocket("/path")
// ---------------------------------------------------------------------------

// Reuses fastapiWebSocketRe from websocket_edges.go (capture 1 = path,
// capture 2 = handler name, capture 3 = param sig).
func synthFastAPIRealtimeWS(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !strings.Contains(src, ".websocket(") {
		return
	}
	for _, m := range fastapiWebSocketRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 4 {
			continue
		}
		path := m[1]
		handler := m[2]
		emit(realtimeVerbWS, path, "fastapi", "websocket", "Function:"+handler, nil)
	}
}

// ---------------------------------------------------------------------------
// Python SSE — sse-starlette EventSourceResponse / StreamingResponse
// ---------------------------------------------------------------------------

// pySSEHandlerRe captures a route decorator (`@app.get("/path")` /
// `@router.get(...)`) on a handler whose body returns an SSE response. We do
// a two-stage match: find the decorator + def, then confirm the body within a
// reasonable window mentions an SSE response constructor / media type.
var pySSERouteRe = regexp.MustCompile(
	`@(?:app|router|api|\w+_router)\.(?:get|post)\s*\(\s*['"]([^'"\r\n]+)['"][^)]*\)\s*[\r\n]+(?:\s*@[^\r\n]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)\s*\([^)]*\)\s*(?:->[^\r\n:]+)?:([\s\S]{0,800})`,
)

func synthPythonSSEEndpoint(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !strings.Contains(src, "EventSourceResponse") &&
		!strings.Contains(src, "text/event-stream") {
		return
	}
	for _, m := range pySSERouteRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 4 {
			continue
		}
		path := m[1]
		handler := m[2]
		body := m[3]
		if !strings.Contains(body, "EventSourceResponse") &&
			!strings.Contains(body, "text/event-stream") {
			continue
		}
		emit(realtimeVerbSSE, path, "fastapi", "sse", "Function:"+handler, nil)
	}
}

// ---------------------------------------------------------------------------
// SignalR — class XHub : Hub { ... } + app.MapHub<XHub>("/path")
// ---------------------------------------------------------------------------

// signalRHubClassRe captures `class ChatHub : Hub` (or `: Hub<T>`), capturing
// the hub class name.
var signalRHubClassRe = regexp.MustCompile(
	`\bclass\s+(\w+)\s*:\s*Hub\b(?:\s*<[^>]+>)?`,
)

// signalRMapHubRe captures `app.MapHub<ChatHub>("/chat")` (or
// `endpoints.MapHub<...>`), capturing the hub class name and the route path.
var signalRMapHubRe = regexp.MustCompile(
	`\.MapHub\s*<\s*(\w+)\s*>\s*\(\s*['"]([^'"\r\n]+)['"]`,
)

// signalRHubMethodRe captures public invokable methods inside a Hub class:
// `public async Task SendMessage(string user, string message)` /
// `public Task Send(...)` / `public void Notify(...)`. The SignalR client
// invokes these by name, so each one is a realtime endpoint
// `<hubBasePath>/<Method>`.
//
// Capture 1 (optional) is the `[HubMethodName("wire")]` override: SignalR
// exposes the method to clients under that wire name instead of the C#
// method name, so the realtime endpoint path uses it (#5003). The attribute
// may be stacked with other attributes between it and the method declaration
// (e.g. `[Authorize]`), so intervening attribute lines are tolerated.
// Capture 2 is the C# method name (always present; drives the handler ref).
var signalRHubMethodRe = regexp.MustCompile(
	`(?:(?:\[\s*HubMethodName\s*\(\s*["']([^"'\r\n]+)["']\s*\)\s*\]|\[[^\]\r\n]*\])[ \t]*[\r\n]\s*)*public\s+(?:async\s+)?(?:override\s+)?(?:Task|ValueTask|void)(?:\s*<[^>]+>)?\s+(\w+)\s*\(`,
)

func synthSignalRRealtimeEndpoints(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !strings.Contains(src, ": Hub") && !strings.Contains(src, ".MapHub") {
		return
	}

	// Build hubClass -> route base path from MapHub registrations anywhere in
	// the file (Startup/Program.cs often co-locates with the hub in fixtures).
	hubBase := map[string]string{}
	for _, m := range signalRMapHubRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		hubBase[m[1]] = m[2]
	}

	for _, hm := range signalRHubClassRe.FindAllStringSubmatchIndex(src, -1) {
		hubName := src[hm[2]:hm[3]]
		// Default base path is the conventional `/<hubNameWithoutHub-suffix>`
		// lower-cased; overridden by an explicit MapHub registration.
		base := hubBase[hubName]
		if base == "" {
			base = "/" + strings.ToLower(strings.TrimSuffix(hubName, "Hub"))
		}
		// Scope method scanning to the class body: from the class declaration
		// to the next top-level `class ` declaration (or EOF).
		bodyStart := hm[1]
		bodyEnd := len(src)
		if next := signalRHubClassRe.FindStringIndex(src[bodyStart:]); next != nil {
			bodyEnd = bodyStart + next[0]
		}
		body := src[bodyStart:bodyEnd]
		for _, mm := range signalRHubMethodRe.FindAllStringSubmatch(body, -1) {
			if len(mm) < 3 {
				continue
			}
			hubMethodName := mm[1] // [HubMethodName("wire")] override, may be ""
			method := mm[2]        // C# method name
			// Skip lifecycle overrides — not client-invokable RPC endpoints.
			switch method {
			case "OnConnectedAsync", "OnDisconnectedAsync", "Dispose":
				continue
			}
			// The wire/exposed name SignalR clients invoke is the
			// [HubMethodName] override when present, else the C# method name
			// (#5003). The endpoint path uses the wire name; the HANDLES edge
			// still points at the real C# method symbol.
			exposed := method
			if hubMethodName != "" {
				exposed = hubMethodName
			}
			extra := map[string]string{"hub": hubName, "method": method}
			if hubMethodName != "" {
				extra["hub_method_name"] = hubMethodName
			}
			emit(
				realtimeVerbWS,
				strings.TrimRight(base, "/")+"/"+exposed,
				"signalr",
				"signalr",
				"Class:"+hubName+"."+method,
				extra,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Phoenix Channels — channel "room:*", RoomChannel
// ---------------------------------------------------------------------------

// phoenixChannelRe captures `channel "topic:*", RoomChannel` inside a Phoenix
// socket module, capturing the topic pattern and the channel module name.
var phoenixChannelRe = regexp.MustCompile(
	`(?m)^\s*channel\s+["']([^"'\r\n]+)["']\s*,\s*([A-Z][\w.]*)`,
)

func synthPhoenixChannelEndpoints(
	src string,
	emit func(verb, rawPath, framework, transport, handlerRef string, extra map[string]string),
) {
	if !strings.Contains(src, "channel ") && !strings.Contains(src, "channel\t") {
		return
	}
	for _, m := range phoenixChannelRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		topic := m[1]
		module := m[2]
		emit(
			realtimeVerbWS,
			topic,
			"phoenix_channels",
			"channels",
			"Class:"+module,
			map[string]string{"topic": topic, "channel_module": module},
		)
	}
}
