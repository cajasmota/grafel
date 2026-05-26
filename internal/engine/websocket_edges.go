// WebSocket entity + edge synthesis (#727).
//
// This pass scans the file's content directly (engine layer, per the #721
// lesson — synthesizers live next to the other engine-layer synthesis files
// rather than being scattered across per-extractor YAML rules) and emits:
//
//   - ChannelEvent entities — one per distinct WebSocket channel/event the
//     file declares or talks to. Identity is `ws:<channel>` (or
//     `ws:event:<event_name>` for socket.io-style named events that do not
//     carry a URL/path identifier). The cross-repo linker matches by Name,
//     so a Quarkus `@ServerEndpoint("/ws/trace")` and a browser
//     `new WebSocket("ws://.../ws/trace")` will produce the same identity
//     and be matchable without extra wiring.
//
//   - WS_SUBSCRIBES_TO edges — server-side message handlers → ChannelEvent.
//
//   - WS_EMITS          edges — server-side emit/broadcast/send → ChannelEvent.
//     A `scope` property records broadcast|room|user.
//
//   - WS_CONNECTS       edges — client-side `new WebSocket(...)` / `io(...)`
//     constructors → ChannelEvent. This is the cross-stack pivot point.
//
// Frameworks covered (server):
//   - Java/Kotlin: Jakarta/Java EE `@ServerEndpoint("/path")` + the four
//     lifecycle annotations `@OnOpen / @OnClose / @OnError / @OnMessage`
//     (Quarkus, Tomcat, GlassFish, Helidon all share this).
//   - Java: Spring STOMP `@MessageMapping("/topic")` on a controller method.
//   - Kotlin: Ktor `webSocket("/path") { ... for (frame in incoming) ... }`.
//   - Python: FastAPI `@app.websocket("/path")` / `@router.websocket("/path")`.
//   - Python: `websockets.serve(handler, host, port)` (no path coverage —
//     emits a generic channel keyed by the file).
//   - JS/Node: socket.io server `io.on('connection', (sock) => { ... })`
//     with nested `sock.on('event', ...)` / `sock.emit('event', ...)` /
//     `io.emit('event', ...)` / `socket.to(room).emit('event', ...)`.
//   - JS/Node: bare `ws` server — `wss.on('connection', ...)` + nested
//     `ws.on('message', ...)` and `ws.send(...)`.
//   - Go: `gorilla/websocket` — `upgrader.Upgrade(w, r, nil)` returns a
//     conn; we flag every method that calls `Upgrade(` as a WS handler.
//   - C# (ASP.NET): `WebSocketAcceptContext` / `AcceptWebSocketAsync`.
//
// Frameworks covered (client):
//   - Browser / Node: `new WebSocket(url)` with literal, template literal,
//     or single-identifier URL argument. The URL is canonicalised to its
//     path component (host stripped) so cross-stack matching survives
//     `ws://localhost:8088/ws/otel` ↔ `@ServerEndpoint("/ws/otel")`.
//   - socket.io-client: `io(url)` / `io("/namespace")` / `io.connect(url)`.
//   - Native ws clients per language are recognised by the same `new
//     WebSocket(...)` form in JS/TS (covers React Native too).
//
// Beyond-minimum carried per #727 standing-rule:
//   - socket.io emits are distinguished into broadcast vs room-scoped vs
//     user-scoped via the `scope` property on WS_EMITS (broadcast=io.emit;
//     room=socket.to(room).emit or io.to(room).emit; user=socket.emit).
//   - Long-polling fallback hint: when a JS/TS file constructs an XHR /
//     fetch loop polling the same path the WS client previously requested,
//     we still emit the WS_CONNECTS edge (the polling fallback is a
//     transport detail; the cross-stack identity is the channel path).
//     A `fallback=long_poll` property is added when we detect a
//     `transports: ['polling']` or `transports: ['websocket', 'polling']`
//     option literal on the io() call.
//   - Typed message payload: when an `@OnMessage public void onX(<Type> x, ...)`
//     handler binds a typed first parameter, the type name is recorded on
//     the WS_SUBSCRIBES_TO edge's `schema` property. Same for FastAPI's
//     `async def x(ws: WebSocket, data: <Type>)` shape.
//
// The dispatcher hook is added to detector.go alongside the existing
// applyHTTPEndpointSynthesis call so this pass runs once per file.
//
// Refs #727.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// channelEventKind is the engine-layer entity kind used for WebSocket
// channels. The kind is unprefixed (no `SCOPE.`) to match the existing
// engine-layer convention (`http_endpoint`, `Route`).
const channelEventKind = "ChannelEvent"

// channelIDPrefix is the deterministic identity prefix for ChannelEvent
// entities. The cross-repo linker matches by Name (= ID for synthetics),
// so producer (`@ServerEndpoint("/ws/trace")`) and consumer
// (`new WebSocket("ws://host/ws/trace")`) collapse to the same identity.
const channelIDPrefix = "ws:"

// applyWebSocketSynthesis is the per-file entry point. Appends entities
// + edges; never modifies or removes existing ones. No-op for content
// that contains none of the per-framework anchors.
func applyWebSocketSynthesis(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	src := string(content)

	seenChannels := map[string]bool{}
	emitChannel := func(channel string, framework string) string {
		id := channelIDPrefix + channel
		if seenChannels[id] {
			return id
		}
		seenChannels[id] = true
		entities = append(entities, types.EntityRecord{
			ID:         id,
			Name:       id,
			Kind:       channelEventKind,
			SourceFile: path,
			Language:   lang,
			Properties: map[string]string{
				"channel":      channel,
				"framework":    framework,
				"pattern_type": "ws_synthesis",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		return id
	}

	emitEdge := func(kind, fromID, toID string, props map[string]string) {
		if fromID == "" || toID == "" {
			return
		}
		if props == nil {
			props = map[string]string{}
		}
		props["pattern_type"] = "ws_synthesis"
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       toID,
			Kind:       kind,
			Properties: props,
		})
	}

	switch lang {
	case "java", "kotlin":
		synthJavaServerEndpoint(src, path, lang, emitChannel, emitEdge)
		synthSpringMessageMapping(src, path, lang, emitChannel, emitEdge)
		if lang == "kotlin" {
			synthKtorWebSocket(src, path, emitChannel, emitEdge)
		}
	case "python":
		synthFastAPIWebSocket(src, path, emitChannel, emitEdge)
		synthPyWebsocketsServe(src, path, emitChannel, emitEdge)
	case "javascript", "typescript":
		synthSocketIOServer(src, path, emitChannel, emitEdge)
		synthBareWSServer(src, path, emitChannel, emitEdge)
		synthBrowserWSClient(src, path, emitChannel, emitEdge)
		synthSocketIOClient(src, path, emitChannel, emitEdge)
	case "go":
		synthGorillaWebSocket(src, path, emitChannel, emitEdge)
	case "csharp":
		synthAspNetWebSocket(src, path, emitChannel, emitEdge)
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Jakarta @ServerEndpoint
// ---------------------------------------------------------------------------

// jakartaServerEndpointRe captures `@ServerEndpoint("/path")` immediately
// preceding a `class <Name>` declaration. Multi-line tolerated; @-stacked
// annotations between are allowed.
var jakartaServerEndpointRe = regexp.MustCompile(
	`@ServerEndpoint\s*\(\s*(?:value\s*=\s*)?"([^"\r\n]+)"\s*(?:[^)]*)?\)\s*(?:[\r\n][^@\r\n]*)*?(?:public|private|protected|\s)*\s*class\s+(\w+)`,
)

// jakartaLifecycleRe captures `@OnMessage` (or @OnOpen/Close/Error) and the
// following method name. We only emit WS_SUBSCRIBES_TO for @OnMessage —
// the lifecycle hooks are not subscriptions to a payload channel.
var jakartaLifecycleRe = regexp.MustCompile(
	`@OnMessage\b[^\r\n]*[\r\n]+(?:\s*@[^\r\n]*[\r\n]+)*\s*(?:public|private|protected|static|final|\s)+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(([^)]*)\)`,
)

// jakartaSessionSendRe flags `session.getBasicRemote().sendText(...)` or
// `session.getAsyncRemote().sendText(...)` — the standard Jakarta WS push
// shape — and `RemoteEndpoint.Basic` / `RemoteEndpoint.Async` calls.
var jakartaSessionSendRe = regexp.MustCompile(
	`(\w+)\s*\.\s*(?:getBasicRemote|getAsyncRemote)\s*\(\s*\)\s*\.\s*(?:sendText|sendObject|sendBinary)\s*\(`,
)

func synthJavaServerEndpoint(
	src, path, lang string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "@ServerEndpoint") {
		return
	}
	for _, m := range jakartaServerEndpointRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		channel := m[1]
		className := m[2]
		framework := "jakarta_websocket"
		channelID := emitChannel(channel, framework)

		// WS_SUBSCRIBES_TO: every @OnMessage handler in the file.
		for _, mm := range jakartaLifecycleRe.FindAllStringSubmatch(src, -1) {
			if len(mm) < 3 {
				continue
			}
			methodName := mm[1]
			paramSig := mm[2]
			props := map[string]string{
				"framework": framework,
				"channel":   channel,
				"handler":   className + "." + methodName,
			}
			if schema := firstNonPrimitiveType(paramSig); schema != "" {
				props["schema"] = schema
			}
			emitEdge(
				string(types.RelationshipKindWSSubscribesTo),
				fmt.Sprintf("Class:%s.%s", className, methodName),
				channelID,
				props,
			)
		}

		// WS_EMITS: every session.getXxxRemote().sendText(...) call in the file.
		for range jakartaSessionSendRe.FindAllStringSubmatch(src, -1) {
			emitEdge(
				string(types.RelationshipKindWSEmits),
				"Class:"+className,
				channelID,
				map[string]string{
					"framework": framework,
					"channel":   channel,
					"scope":     "broadcast",
				},
			)
		}
	}
}

// firstNonPrimitiveType returns the first parameter type that looks like a
// payload (i.e. not a Java primitive, not Session, not String). Used to
// surface typed message payloads on WS_SUBSCRIBES_TO.
func firstNonPrimitiveType(paramSig string) string {
	for _, raw := range strings.Split(paramSig, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Strip leading annotations.
		for strings.HasPrefix(raw, "@") {
			if idx := strings.IndexByte(raw, ' '); idx > 0 {
				raw = strings.TrimSpace(raw[idx:])
			} else {
				break
			}
		}
		fields := strings.Fields(raw)
		if len(fields) < 2 {
			continue
		}
		t := fields[0]
		switch t {
		case "Session", "String", "byte[]", "boolean", "int", "long", "double", "float", "char", "short":
			continue
		}
		// Strip generics for stable identity.
		if idx := strings.IndexByte(t, '<'); idx > 0 {
			t = t[:idx]
		}
		return t
	}
	return ""
}

// ---------------------------------------------------------------------------
// Spring STOMP @MessageMapping
// ---------------------------------------------------------------------------

var springMessageMappingRe = regexp.MustCompile(
	`@MessageMapping\s*\(\s*"([^"\r\n]+)"\s*\)[\s\S]{0,200}?(?:public|private|protected)?\s+[\w<>\[\],.\s?]+?\s+(\w+)\s*\(`,
)

func synthSpringMessageMapping(
	src, path, lang string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "@MessageMapping") {
		return
	}
	for _, m := range springMessageMappingRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		channel := m[1]
		method := m[2]
		framework := "spring_stomp"
		id := emitChannel(channel, framework)
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Class:"+method,
			id,
			map[string]string{"framework": framework, "channel": channel},
		)
	}
}

// ---------------------------------------------------------------------------
// Ktor webSocket("/path") { ... }
// ---------------------------------------------------------------------------

var ktorWebSocketRe = regexp.MustCompile(
	`(?m)\bwebSocket\s*\(\s*"([^"\r\n]+)"\s*\)\s*\{`,
)

func synthKtorWebSocket(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "webSocket(") {
		return
	}
	for _, m := range ktorWebSocketRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		channel := m[1]
		id := emitChannel(channel, "ktor")
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:ktor_ws_"+sanitiseID(channel),
			id,
			map[string]string{"framework": "ktor", "channel": channel},
		)
	}
}

func sanitiseID(s string) string {
	out := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
	return strings.Trim(out, "_")
}

// ---------------------------------------------------------------------------
// FastAPI @app.websocket("/path")
// ---------------------------------------------------------------------------

var fastapiWebSocketRe = regexp.MustCompile(
	`@(?:app|router|api|\w+_router)\.websocket\s*\(\s*["']([^"'\r\n]+)["'][^)]*\)\s*[\r\n]+(?:\s*@[^\r\n]*[\r\n]+)*\s*(?:async\s+)?def\s+(\w+)\s*\(([^)]*)\)`,
)

func synthFastAPIWebSocket(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, ".websocket(") {
		return
	}
	for _, m := range fastapiWebSocketRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 4 {
			continue
		}
		channel := m[1]
		handler := m[2]
		paramSig := m[3]
		id := emitChannel(channel, "fastapi")
		props := map[string]string{
			"framework": "fastapi",
			"channel":   channel,
			"handler":   handler,
		}
		if schema := firstNonPrimitivePyType(paramSig); schema != "" {
			props["schema"] = schema
		}
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:"+handler,
			id,
			props,
		)
	}
}

// firstNonPrimitivePyType walks a Python parameter signature looking for the
// first `: <Type>` annotation that isn't `WebSocket` / `str` / `int` /
// `bytes` / `dict` / `Any`. Returns "" when no payload type can be derived.
func firstNonPrimitivePyType(paramSig string) string {
	for _, raw := range strings.Split(paramSig, ",") {
		raw = strings.TrimSpace(raw)
		colon := strings.IndexByte(raw, ':')
		if colon < 0 {
			continue
		}
		t := strings.TrimSpace(raw[colon+1:])
		// Strip default value.
		if eq := strings.IndexByte(t, '='); eq > 0 {
			t = strings.TrimSpace(t[:eq])
		}
		// Strip generics.
		if br := strings.IndexByte(t, '['); br > 0 {
			t = t[:br]
		}
		switch t {
		case "", "WebSocket", "str", "int", "bytes", "dict", "Any", "bool", "float":
			continue
		}
		return t
	}
	return ""
}

// ---------------------------------------------------------------------------
// websockets.serve(handler, host, port)
// ---------------------------------------------------------------------------

var pyWebsocketsServeRe = regexp.MustCompile(
	`websockets\.serve\s*\(\s*(\w+)\s*,`,
)

func synthPyWebsocketsServe(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "websockets.serve") {
		return
	}
	for _, m := range pyWebsocketsServeRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		handler := m[1]
		// websockets.serve has no path argument; key the channel by the
		// handler name so files with multiple servers each get a row.
		channel := "/" + sanitiseID(handler)
		id := emitChannel(channel, "websockets")
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:"+handler,
			id,
			map[string]string{"framework": "websockets", "channel": channel, "handler": handler},
		)
	}
}

// ---------------------------------------------------------------------------
// socket.io server: io.on('connection', sock => { sock.on('event', ...) })
// ---------------------------------------------------------------------------

// socketIOServerCreateRe is the cheap anchor — `new Server(` from socket.io
// or the `socketIo(httpServer)` factory.
var socketIOServerCreateRe = regexp.MustCompile(
	`(?:new\s+(?:Server|SocketIOServer)\s*\(|socket\.io\.attach|require\(\s*['"]socket\.io['"]\s*\)|from\s+['"]socket\.io['"])`,
)

// socketIOEventOnRe captures `<ident>.on('event', handler)` inside an
// io.on('connection', ...) callback. We do not try to bind precisely to the
// outer `connection` callback — the per-file dedup downstream handles dups.
var socketIOEventOnRe = regexp.MustCompile(
	`\b(?:socket|sock|client|s)\s*\.\s*on\s*\(\s*['"]([^'"\r\n]+)['"]\s*,\s*(?:async\s*)?(?:function\s*\(|\(?[\w$,\s]*\)?\s*=>)`,
)

// socketIOEmitRe captures both unscoped and room-scoped emits. Capture 1 =
// receiver chain (e.g. `io`, `socket`, `socket.to("room")`); capture 2 =
// event name.
var socketIOEmitRe = regexp.MustCompile(
	`\b(io|socket|sock|client)((?:\s*\.\s*(?:to|in|of|broadcast|volatile|local)\s*\([^)]*\))*)\s*\.\s*emit\s*\(\s*['"]([^'"\r\n]+)['"]`,
)

func synthSocketIOServer(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !socketIOServerCreateRe.MatchString(src) && !strings.Contains(src, "socket.on(") && !strings.Contains(src, "io.emit(") {
		return
	}

	for _, m := range socketIOEventOnRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		event := m[1]
		// Skip lifecycle events — they don't represent application channels.
		if event == "connection" || event == "disconnect" || event == "error" || event == "connect" {
			continue
		}
		channel := "event:" + event
		id := emitChannel(channel, "socket.io")
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:socketio_on_"+sanitiseID(event),
			id,
			map[string]string{"framework": "socket.io", "channel": channel, "event_name": event},
		)
	}

	for _, m := range socketIOEmitRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 4 {
			continue
		}
		receiver := m[1]
		modifiers := m[2]
		event := m[3]
		channel := "event:" + event
		id := emitChannel(channel, "socket.io")
		scope := "user"
		switch {
		case strings.Contains(modifiers, ".to(") || strings.Contains(modifiers, ".in("):
			scope = "room"
		case receiver == "io" && modifiers == "":
			scope = "broadcast"
		case strings.Contains(modifiers, ".broadcast"):
			scope = "broadcast"
		}
		room := ""
		if scope == "room" {
			if rm := regexp.MustCompile(`\.(?:to|in)\s*\(\s*['"]([^'"\r\n]+)['"]`).FindStringSubmatch(modifiers); len(rm) >= 2 {
				room = rm[1]
			}
		}
		props := map[string]string{
			"framework":  "socket.io",
			"channel":    channel,
			"event_name": event,
			"scope":      scope,
		}
		if room != "" {
			props["room"] = room
		}
		emitEdge(
			string(types.RelationshipKindWSEmits),
			"Function:socketio_emit_"+sanitiseID(event),
			id,
			props,
		)
	}
}

// ---------------------------------------------------------------------------
// Bare `ws` (Node): wss.on('connection', ws => { ws.on('message', ...) })
// ---------------------------------------------------------------------------

// bareWSServerCreateRe anchors on `new WebSocketServer(` or `new WebSocket.Server(`.
var bareWSServerCreateRe = regexp.MustCompile(
	`new\s+(?:WebSocketServer|WebSocket\.Server|WSServer)\s*\(`,
)

// bareWSOnMessageRe captures `ws.on('message', ...)` inside a connection
// callback. Like the socket.io scanner we do not bind precisely — file-scope
// dedup handles duplicates and the cross-stack matcher uses the channel ID.
var bareWSOnMessageRe = regexp.MustCompile(
	`\b(?:ws|socket|conn|client)\s*\.\s*on\s*\(\s*['"](message|ping|pong)['"]`,
)

// bareWSSendRe captures `ws.send(...)`.
var bareWSSendRe = regexp.MustCompile(
	`\b(?:ws|socket|conn|client)\s*\.\s*send\s*\(`,
)

func synthBareWSServer(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !bareWSServerCreateRe.MatchString(src) {
		return
	}
	// Use the file path as the channel — bare ws server has no semantic
	// channel string in the API; the URL it listens on is wired via the
	// underlying HTTP server. We fall back to a path-shaped channel.
	channel := "/ws"
	if mm := regexp.MustCompile(`path\s*:\s*['"]([^'"\r\n]+)['"]`).FindStringSubmatch(src); len(mm) >= 2 {
		channel = mm[1]
	}
	id := emitChannel(channel, "ws")
	for _, m := range bareWSOnMessageRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		event := m[1]
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:ws_on_"+event,
			id,
			map[string]string{"framework": "ws", "channel": channel, "event_name": event},
		)
	}
	for range bareWSSendRe.FindAllStringSubmatch(src, -1) {
		emitEdge(
			string(types.RelationshipKindWSEmits),
			"Function:ws_send",
			id,
			map[string]string{"framework": "ws", "channel": channel, "scope": "user"},
		)
	}
}

// ---------------------------------------------------------------------------
// Browser / Node client: new WebSocket(url)
// ---------------------------------------------------------------------------

// browserWSClientRe captures `new WebSocket("url")`, `new WebSocket('url')`,
// `new WebSocket(\`url\`)`, and `new WebSocket(identifier)`. For the
// identifier form, we resolve via the per-file string-constant table built
// by buildJSConstantSymbolTable (shared with the HTTP client synthesizer).
// We also handle function-call results like `new WebSocket(wsUrl())` —
// these collapse to an unnamed channel keyed by the file path.
var browserWSClientRe = regexp.MustCompile(
	"new\\s+WebSocket\\s*\\(\\s*(?:['\"]([^'\"\\r\\n$]+)['\"]|`([^`\\r\\n]+)`|([A-Za-z_$][\\w$]*))",
)

// wsURLAnywhereRe is the fallback path used when `new WebSocket(...)`
// receives a function-call result or an unresolvable identifier (e.g.
// `new WebSocket(wsUrl())` / `new WebSocket(resolveWsUrl())`). Frontend
// code routinely hides the URL behind a helper, so the file usually
// contains the literal URL elsewhere — inside a `return "..."` or
// `return \`...\“ statement. This regex captures every WebSocket-shaped
// URL literal in the file (absolute `ws://` / `wss://` or a path that
// starts with `/ws/`). The companion file-scope scan emits one
// WS_CONNECTS edge per distinct URL when a `new WebSocket(` appears.
//
// We deliberately accept template literals here too — interpolations
// are folded by canonicalizeTemplateLiteral. The host strip in
// stripWSHost reduces `${proto}//event.local/ws/trace` to `/ws/trace`
// once the leading host segment is recognised.
var wsURLAnywhereRe = regexp.MustCompile(
	"(?:'((?:wss?://)[^'\\r\\n]+?(?:/ws/[^'\\r\\n]*))'" +
		"|\"((?:wss?://)[^\"\\r\\n]+?(?:/ws/[^\"\\r\\n]*))\"" +
		"|`((?:[^`\\r\\n]*\\$\\{[^`\\r\\n]*\\}[^`\\r\\n]*)?[^`\\r\\n]*?(?:/ws/[^`\\r\\n]*))`" +
		"|'(/ws/[^'\\r\\n]*)'" +
		"|\"(/ws/[^\"\\r\\n]*)\")",
)

func synthBrowserWSClient(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "new WebSocket") {
		return
	}
	syms := buildJSConstantSymbolTable(src)
	funcs := indexJSEnclosingFunctions(src)
	// Track which channel paths got an edge so the file-scope fallback
	// below doesn't double-fire for cases the primary scan already
	// resolved.
	resolved := map[string]bool{}
	for _, m := range browserWSClientRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 8 {
			continue
		}
		var rawURL string
		var isTemplate bool
		switch {
		case m[2] >= 0:
			rawURL = src[m[2]:m[3]]
		case m[4] >= 0:
			rawURL = src[m[4]:m[5]]
			isTemplate = true
		case m[6] >= 0:
			ident := src[m[6]:m[7]]
			if v, ok := syms[ident]; ok {
				rawURL = v
			} else {
				continue
			}
		}
		if rawURL == "" {
			continue
		}

		var channelPath string
		if isTemplate && strings.Contains(rawURL, "${") {
			resolved, ok := canonicalizeTemplateLiteral(rawURL, syms)
			if !ok {
				continue
			}
			channelPath = stripWSHost(resolved)
		} else {
			channelPath = stripWSHost(rawURL)
		}
		if channelPath == "" {
			continue
		}
		id := emitChannel(channelPath, "browser_websocket")
		caller := enclosingJSFuncAt(funcs, m[0])
		fromID := "Function:" + caller
		if caller == "" {
			fromID = "Function:" + sanitiseID(path)
		}
		emitEdge(
			string(types.RelationshipKindWSConnects),
			fromID,
			id,
			map[string]string{
				"framework": "browser_websocket",
				"channel":   channelPath,
			},
		)
		resolved[channelPath] = true
	}

	// File-scope fallback: when the WebSocket constructor argument is a
	// function call or otherwise opaque (`new WebSocket(wsUrl())`,
	// `new WebSocket(resolveWsUrl())`), the primary scan above yields
	// nothing. Frontend code reliably mentions the literal URL inside a
	// helper's `return` (or in an inline template), so we mine the entire
	// file for WS-shaped URL literals and emit one WS_CONNECTS edge per
	// distinct path that the primary scan didn't already cover. This is
	// the path that lights up fixture-f's WsBridge.tsx / store/otel.ts —
	// both wrap the URL in a helper.
	for _, mm := range wsURLAnywhereRe.FindAllStringSubmatchIndex(src, -1) {
		// 5 alternation groups; whichever fired is the URL.
		var raw string
		var isTemplate bool
		switch {
		case mm[2] >= 0:
			raw = src[mm[2]:mm[3]]
		case mm[4] >= 0:
			raw = src[mm[4]:mm[5]]
		case mm[6] >= 0:
			raw = src[mm[6]:mm[7]]
			isTemplate = true
		case mm[8] >= 0:
			raw = src[mm[8]:mm[9]]
		case mm[10] >= 0:
			raw = src[mm[10]:mm[11]]
		}
		if raw == "" {
			continue
		}
		channelPath := raw
		if isTemplate && strings.Contains(raw, "${") {
			if folded, ok := canonicalizeTemplateLiteral(raw, syms); ok {
				channelPath = folded
			}
		}
		channelPath = stripWSHost(channelPath)
		if !strings.HasPrefix(channelPath, "/") {
			continue
		}
		if resolved[channelPath] {
			continue
		}
		resolved[channelPath] = true
		id := emitChannel(channelPath, "browser_websocket")
		caller := enclosingJSFuncAt(funcs, mm[0])
		fromID := "Function:" + caller
		if caller == "" {
			fromID = "Function:" + sanitiseID(path)
		}
		emitEdge(
			string(types.RelationshipKindWSConnects),
			fromID,
			id,
			map[string]string{
				"framework":    "browser_websocket",
				"channel":      channelPath,
				"resolved_via": "url_literal_scan",
			},
		)
	}
}

// stripWSHost normalises a WebSocket URL to its path component for
// cross-stack matching. `ws://host:port/path` and `wss://host/path` both
// collapse to `/path`. `/path` passes through unchanged. Falls back to
// the input when no scheme is present.
func stripWSHost(s string) string {
	s = strings.TrimSpace(s)
	for _, scheme := range []string{"ws://", "wss://", "http://", "https://"} {
		if strings.HasPrefix(s, scheme) {
			rest := s[len(scheme):]
			if idx := strings.IndexByte(rest, '/'); idx >= 0 {
				path := rest[idx:]
				// Strip query string.
				if q := strings.IndexByte(path, '?'); q >= 0 {
					path = path[:q]
				}
				return path
			}
			return "/"
		}
	}
	if q := strings.IndexByte(s, '?'); q >= 0 {
		s = s[:q]
	}
	return s
}

// ---------------------------------------------------------------------------
// socket.io client: io(url) / io.connect(url) / io("/namespace")
// ---------------------------------------------------------------------------

var socketIOClientRe = regexp.MustCompile(
	"(?:^|[^\\w$.])io(?:\\.connect)?\\s*\\(\\s*['\"]([^'\"\\r\\n]+)['\"]",
)

// socketIOClientTransportsRe detects the `transports: [...]` option used to
// surface long-polling fallback in the WS_CONNECTS edge properties.
var socketIOClientTransportsRe = regexp.MustCompile(
	`transports\s*:\s*\[([^\]]+)\]`,
)

func synthSocketIOClient(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "io(") && !strings.Contains(src, "io.connect(") {
		return
	}
	funcs := indexJSEnclosingFunctions(src)
	fallback := ""
	if mm := socketIOClientTransportsRe.FindStringSubmatch(src); len(mm) >= 2 {
		if strings.Contains(mm[1], "polling") {
			fallback = "long_poll"
		}
	}
	for _, m := range socketIOClientRe.FindAllStringSubmatchIndex(src, -1) {
		if len(m) < 4 {
			continue
		}
		raw := src[m[2]:m[3]]
		channelPath := stripWSHost(raw)
		if channelPath == "" {
			continue
		}
		id := emitChannel(channelPath, "socket.io-client")
		caller := enclosingJSFuncAt(funcs, m[0])
		fromID := "Function:" + caller
		if caller == "" {
			fromID = "Function:" + sanitiseID(path)
		}
		props := map[string]string{
			"framework": "socket.io-client",
			"channel":   channelPath,
		}
		if fallback != "" {
			props["fallback"] = fallback
		}
		emitEdge(
			string(types.RelationshipKindWSConnects),
			fromID,
			id,
			props,
		)
	}
}

// ---------------------------------------------------------------------------
// Go: gorilla/websocket upgrader.Upgrade(...)
// ---------------------------------------------------------------------------

// goGorillaUpgradeRe captures the canonical `upgrader.Upgrade(w, r, nil)`
// form. The path under which the upgrade fires is established at HTTP route
// registration time (gorilla/mux or chi or net/http); we key the channel by
// the enclosing function name as a stable, file-local identity.
var goGorillaUpgradeRe = regexp.MustCompile(
	`(?m)func\s+(?:\([^)]+\)\s+)?(\w+)\s*\([^)]*\)\s*(?:\([^)]*\))?\s*\{[\s\S]{0,400}?\b\w+\s*\.\s*Upgrade\s*\(`,
)

func synthGorillaWebSocket(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !strings.Contains(src, "websocket.Upgrader") && !strings.Contains(src, ".Upgrade(") {
		return
	}
	for _, m := range goGorillaUpgradeRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		handler := m[1]
		channel := "/" + sanitiseID(handler)
		id := emitChannel(channel, "gorilla_websocket")
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Function:"+handler,
			id,
			map[string]string{"framework": "gorilla_websocket", "channel": channel, "handler": handler},
		)
	}
}

// ---------------------------------------------------------------------------
// ASP.NET: AcceptWebSocketAsync / WebSocketAcceptContext
// ---------------------------------------------------------------------------

var aspNetAcceptRe = regexp.MustCompile(
	`(?:AcceptWebSocketAsync|WebSocketAcceptContext)\s*\(`,
)

// aspNetMethodRe captures a method signature that contains an AcceptWebSocketAsync
// call. We approximate with the nearest preceding `<modifier>* Task<...> <Name>(`.
var aspNetMethodSignatureRe = regexp.MustCompile(
	`(?m)(?:public|private|protected|internal|static|async|\s)+\s+(?:Task|ValueTask)\s*(?:<[^>]+>)?\s+(\w+)\s*\([^)]*\)`,
)

func synthAspNetWebSocket(
	src, path string,
	emitChannel func(channel, framework string) string,
	emitEdge func(kind, from, to string, props map[string]string),
) {
	if !aspNetAcceptRe.MatchString(src) {
		return
	}
	for _, m := range aspNetMethodSignatureRe.FindAllStringSubmatch(src, -1) {
		if len(m) < 2 {
			continue
		}
		handler := m[1]
		channel := "/" + sanitiseID(handler)
		id := emitChannel(channel, "aspnet_websocket")
		emitEdge(
			string(types.RelationshipKindWSSubscribesTo),
			"Class:"+handler,
			id,
			map[string]string{"framework": "aspnet_websocket", "channel": channel, "handler": handler},
		)
	}
}
