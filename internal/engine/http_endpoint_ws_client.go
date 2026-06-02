// WebSocket / Socket.IO client-side (consumer) synthetic http_endpoint_call
// emission for realtime cross-repo matching (epic #3628 area #7, realtime).
//
// The realtime-endpoint-synthesis pass (realtime_endpoint_synthesis.go) emits
// one PRODUCER-side synthetic per server handler. For a socket.io server
// `socket.on('chat:message', ...)` it emits:
//
//	http:WS:/<canonicalised event>     e.g. socket.on('chat:message')
//	                                   → canonical "/chat{message}"
//	                                   → id  http:WS:/chat{message}
//
// using the synthetic verb WS, with the event name first prefixed by `/` and
// then run through canonicalizeRealtimePath("websocket", ...) — the SAME
// canonicaliser the realtime pass uses (so a `:`-bearing event like
// `chat:message` collapses to `/chat{message}`, matching the server byte for
// byte). The REST/GraphQL consumer passes never modelled WS clients, so a
// browser / Node socket.io CLIENT emitted NOTHING at the event level. The only
// realtime client record was the single WS_CONNECTS edge (websocket_edges.go),
// which keys on a *different* `ws:<channel>` identity space and can never join
// the per-event `http:WS:` server endpoints above.
//
// This pass closes that gap. For every socket.io client operation it recognises
// the event name and emits a consumer-side http_endpoint_call whose ID EXACTLY
// matches the server endpoint shape:
//
//	socket.emit('chat:message', payload)  →  http_endpoint_call  http:WS:/chat{message}
//	socket.on('notify', cb)               →  http_endpoint_call  http:WS:/notify
//
// Because the ID is identical to the server-side definition's ID, the existing
// Name-based cross-repo HTTP linker (links/http_pass.go) joins them on reindex
// with NO new linker code — exactly like the REST and GraphQL consumer passes.
// `socket.emit(...)` is a publish (client → server handler), `socket.on(...)`
// is a subscription (server → client); both map onto the same event endpoint
// identity, so both are emitted as consumer-side calls and distinguished by the
// `ws_role` property (emit | subscribe).
//
// Recognised client idioms (JS/TS only — the dominant case):
//
//   - socket.io-client connection bound to a var:
//     const socket = io(url) / io.connect(url) / io("/namespace")
//     → the var name is resolved, and `<var>.emit('e')` / `<var>.on('e')`
//     calls on it are extracted.
//   - the conventional bare identifiers `socket` / `sock` / `client` are also
//     accepted as socket handles when a client connection marker is present in
//     the file, matching the server pass's symmetric identifier set.
//
// FETCHES edge: the enclosing function at the call site is recorded as
// `source_caller` (Function:<name>); ResolveHTTPEndpointHandlers turns that
// into the FETCHES edge — same mechanism as every other consumer pass.
//
// Honest-partial / non-fabrication rules:
//   - Dynamic event names (`socket.emit(eventName, …)`, template literals) are
//     SKIPPED — no event string, no fabricated endpoint.
//   - Lifecycle events (connect/disconnect/error/…) are skipped — they are not
//     application channels and the server pass skips them too.
//   - Native `WebSocket`'s `ws.send(...)` carries no event name → skipped
//     (the WS_CONNECTS pass already records the URL-level connection).
//   - SERVER files (those containing a socket.io server-create marker or an
//     `io.on('connection', …)` handler) are skipped entirely: their
//     `socket.on(...)` handlers are the PRODUCER side, already emitted by the
//     realtime pass. This pass is strictly the client (consumer) side.
//
// Refs epic #3628 (realtime cross-link), realtime_endpoint_synthesis.go (#3682).

package engine

import (
	"regexp"
	"strings"
)

// wsClientVerb is the synthetic HTTP verb used for all WebSocket realtime
// endpoints, matching the server side (realtimeVerbWS == "WS").
const wsClientVerb = "WS"

// wsClientConnectMarkerRe detects a socket.io-client (or native WebSocket)
// CONNECTION in the file — the signal that `<var>.emit/.on` calls are talking
// to a remote server rather than being server-side handler registrations.
var wsClientConnectMarkerRe = regexp.MustCompile(
	`(?:^|[^\w$.])io(?:\.connect)?\s*\(|\bfrom\s+['"]socket\.io-client['"]|require\(\s*['"]socket\.io-client['"]\s*\)`,
)

// wsServerCreateMarkerRe detects a socket.io SERVER create in the file. When
// present we treat the file as a server and let the realtime producer pass own
// its `socket.on(...)` handlers, so this client pass does not double-emit.
var wsServerCreateMarkerRe = regexp.MustCompile(
	`new\s+(?:Server|SocketIOServer)\s*\(|socket\.io\.attach|require\(\s*['"]socket\.io['"]\s*\)|from\s+['"]socket\.io['"]|\.on\s*\(\s*['"]connection['"]`,
)

// wsClientSocketAssignRe captures a socket-handle variable bound to an io(...)
// / io.connect(...) call: `const socket = io(url)`. Capture 1 = var name.
var wsClientSocketAssignRe = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*io(?:\.connect)?\s*\(`,
)

// wsClientEmitOnRe captures `<recv>.emit('event'` and `<recv>.on('event'` where
// <recv> is the socket handle. Capture 1 = receiver ident, 2 = method
// (emit|on), 3 = event name. The event must be a string literal — dynamic
// names (bare identifiers / template literals) do not match and are skipped.
var wsClientEmitOnRe = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\.\s*(emit|on)\s*\(\s*['"]([^'"\r\n]+)['"]`,
)

// wsClientLifecycleEvents are socket.io lifecycle/reserved events that do not
// represent application channels (mirrors the realtime producer pass).
var wsClientLifecycleEvents = map[string]bool{
	"connect": true, "connection": true, "disconnect": true,
	"disconnecting": true, "connect_error": true, "error": true,
	"reconnect": true, "reconnect_attempt": true, "reconnect_error": true,
	"reconnect_failed": true, "ping": true, "pong": true,
}

// synthesizeWSClientCalls scans a JS/TS file for socket.io CLIENT event
// publishes (`socket.emit('e')`) and subscriptions (`socket.on('e')`) and emits
// one http_endpoint_call per distinct (event, role), keyed to match the server
// endpoint shape http:WS:/<canonical event>, plus a source_caller for the
// FETCHES edge.
//
// It is invoked from synthesizeFetchAxios so it shares the JS/TS dispatch.
func synthesizeWSClientCalls(content string, funcs []jsFuncSpan, emit emitFn) {
	// File-signal gate: require a socket.io-client connection marker so this is
	// a no-op on ordinary REST/React files and on socket.io SERVER files.
	if !wsClientConnectMarkerRe.MatchString(content) {
		return
	}
	// Server files own their socket.on(...) handlers via the realtime producer
	// pass; do not double-emit the same event as a consumer here.
	if wsServerCreateMarkerRe.MatchString(content) {
		return
	}

	// Resolve the set of identifiers that hold a socket.io client connection.
	// Conventional bare handles are also accepted (the file already passed the
	// client-connection gate), matching the server pass's identifier set.
	socketVars := map[string]bool{
		"socket": true, "sock": true, "client": true, "s": true,
	}
	for _, m := range wsClientSocketAssignRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			socketVars[m[1]] = true
		}
	}

	// Per-(role,event) dedup within the file: the canonical id already dedups
	// at the emit layer per side, but we also avoid re-attributing the same
	// event to a different caller twice when the literal repeats verbatim.
	seen := map[string]bool{}

	for _, m := range wsClientEmitOnRe.FindAllStringSubmatchIndex(content, -1) {
		// Groups: m[2:4] recv, m[4:6] method, m[6:8] event.
		if len(m) < 8 {
			continue
		}
		recv := content[m[2]:m[3]]
		method := content[m[4]:m[5]]
		event := content[m[6]:m[7]]

		if !socketVars[recv] {
			continue
		}
		if wsClientLifecycleEvents[event] {
			continue
		}
		// Honest-partial: reject anything that is not a stable literal event.
		if event == "" || strings.ContainsAny(event, "${`") || event == "*" {
			continue
		}

		// Byte-identical id parity: run the event through the SAME canonicaliser
		// the realtime producer pass uses (event prefixed with `/`).
		canonical := canonicalizeRealtimePath("websocket", "/"+strings.TrimPrefix(event, "/"))
		if canonical == "" {
			continue
		}

		role := "emit"
		if method == "on" {
			role = "subscribe"
		}
		dedupKey := role + "\x00" + canonical
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		caller := enclosingJSFuncAt(funcs, m[0])
		framework := "socket.io-client"
		// The emit closure stamps verb/path/framework/source_caller and the
		// canonical http:WS:<path> id. The ws_role is folded into the framework
		// label so the existing emitFn signature is preserved without a schema
		// change; emit | subscribe is recoverable from `framework`.
		if role == "subscribe" {
			framework = "socket.io-client:subscribe"
		} else {
			framework = "socket.io-client:emit"
		}
		emit(wsClientVerb, canonical, framework, "Function", caller)
	}
}
