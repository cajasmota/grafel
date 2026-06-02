// JSON-RPC (and XML-RPC) server-side (producer) + client-side (consumer)
// synthetic http_endpoint emission for cross-repo matching (epic #3628,
// "[protocol] SOAP + JSON-RPC client cross-link").
//
// Like the SOAP pass (http_endpoint_soap.go) this reuses the existing
// Name-based cross-repo HTTP linker (links/http_pass.go): a consumer-side
// `http_endpoint_call` whose ID is byte-for-byte identical to a producer-side
// `http_endpoint_definition` joins with NO new linker code.
//
// Canonical JSON-RPC id
// ---------------------
// We adopt the synthetic verb JSONRPC and the canonical path
//
//	/jsonrpc/<method>
//
// giving the id  http:JSONRPC:/jsonrpc/<method>. JSON-RPC method names are flat
// strings (often dotted, e.g. `user.get`); the dotted form is preserved as a
// single path segment so producer and consumer ids match exactly.
//
// Producer side (server)
// ----------------------
//   - JS/TS jayson: `jayson.server({ <method>: fn, … })` /
//     `new jayson.Server({ … })` — each method key in the method map →
//     http:JSONRPC:/jsonrpc/<method>.
//   - Python: a method registered on an xmlrpc/jsonrpc dispatcher via
//     `server.register_function(fn, "<name>")` → http:JSONRPC:/jsonrpc/<name>.
//
// Consumer side (client)
// ----------------------
//   - JS/TS jayson: `client.request('<method>', params)` → the first string-
//     literal argument is the method name.
//   - Python: `ServerProxy(url).<method>(...)` (xmlrpc.client / jsonrpclib) →
//     the attribute accessed on the proxy handle is the remote method.
//
// Honest-partial / non-fabrication
//   - Dynamic method names (variable / template literal / non-literal first
//     arg to .request) are SKIPPED — no string, no fabricated endpoint.
//   - Proxy accessor/lifecycle names (close, system.*, __*) are skipped.
//   - All passes are file-signal gated so they no-op on ordinary files.

package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// jsonRPCVerb is the synthetic HTTP verb shared by the JSON-RPC producer and
// all consumer passes so their ids join.
const jsonRPCVerb = "JSONRPC"

// jsonRPCCanonical returns the canonical, slash-normalised endpoint path for a
// JSON-RPC method. FrameworkExpress only normalises slashes here (the method is
// a single, param-free segment), matching the SOAP/GraphQL/WS passes.
func jsonRPCCanonical(method string) string {
	return httproutes.Canonicalize(httproutes.FrameworkExpress, "/jsonrpc/"+method)
}

// isDynamicJSONRPCName reports whether a method token is a stable string
// literal. Anything carrying interpolation/template/whitespace markers is
// dynamic → honest-partial skip.
func isDynamicJSONRPCName(name string) bool {
	return name == "" || strings.ContainsAny(name, "${}`+ \t\r\n(")
}

// jsonRPCReservedMethod is the set of method names that are NOT application
// JSON-RPC methods — proxy accessors and JSON-RPC system/introspection methods.
var jsonRPCReservedMethod = map[string]bool{
	"close": true, "request": true, "notify": true, "batch": true,
	"system.listMethods": true, "system.methodHelp": true,
	"system.methodSignature": true, "system.multicall": true,
}

// ---------------------------------------------------------------------------
// Producer side — JS/TS jayson server
// ---------------------------------------------------------------------------

// jsonRPCServerOpenRE locates the opening of a jayson server method-map call:
//
//	jayson.server({ add: fn, subtract: fn })
//	new jayson.Server({ ... })
//
// The match END is positioned at (or just before) the method-map `{`; the
// synthesizer then balance-scans the object so nested function bodies don't
// truncate the body the way a `[^}]*` regex would.
var jsonRPCServerOpenRE = regexp.MustCompile(
	`(?:jayson\s*\.\s*server|new\s+jayson\s*\.\s*Server)\s*\(\s*`)

// jsonRPCTopLevelKeyRE captures an object-literal key `<name>:` (bare ident or
// quoted, possibly dotted). Applied ONLY to the depth-1 slices of a method map
// (nested function bodies are skipped by the balanced walk), so it never picks
// up an identifier from inside a handler body. Capture 1 = quoted key, 2 = bare.
var jsonRPCTopLevelKeyRE = regexp.MustCompile(
	`^['"]([A-Za-z_$][\w$.]*)['"]\s*:|^([A-Za-z_$][\w$]*)\s*:`)

// synthesizeJSJSONRPCServer scans a JS/TS file for a jayson server method map
// and emits one producer-side http_endpoint_definition per top-level method
// key, keyed http:JSONRPC:/jsonrpc/<method>.
func synthesizeJSJSONRPCServer(content string, emit emitFn) {
	if !strings.Contains(content, "jayson") {
		return
	}
	seen := map[string]bool{}
	for _, loc := range jsonRPCServerOpenRE.FindAllStringIndex(content, -1) {
		// The next non-space byte at loc[1] must be the method-map `{`.
		open := loc[1]
		for open < len(content) && (content[open] == ' ' || content[open] == '\t' ||
			content[open] == '\n' || content[open] == '\r') {
			open++
		}
		if open >= len(content) || content[open] != '{' {
			continue
		}
		close := findMatchingBrace(content, open)
		if close < 0 {
			continue
		}
		for _, method := range jsonRPCTopLevelKeys(content[open+1 : close]) {
			if isDynamicJSONRPCName(method) || jsonRPCReservedMethod[method] {
				continue
			}
			if seen[method] {
				continue
			}
			seen[method] = true
			emit(jsonRPCVerb, jsonRPCCanonical(method), "jayson", "Method", method)
		}
	}
}

// jsonRPCTopLevelKeys walks an object-literal body and returns the keys defined
// at depth 0 (directly under the method-map braces). Nested object literals,
// parenthesised argument lists, and string literals are skipped so a key name
// inside a handler body is never mistaken for a method-map entry.
func jsonRPCTopLevelKeys(body string) []string {
	var out []string
	i, n := 0, len(body)
	atEntryStart := true // true when the next ident:`:` is a depth-0 key
	for i < n {
		c := body[i]
		switch {
		case c == '{' || c == '(' || c == '[':
			// Skip a balanced nested block.
			open, closeByte := c, matchingCloser(c)
			depth := 0
			for i < n {
				if body[i] == open {
					depth++
				} else if body[i] == closeByte {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
			atEntryStart = false
		case c == ',':
			atEntryStart = true
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			if atEntryStart {
				if km := jsonRPCTopLevelKeyRE.FindStringSubmatch(body[i:]); km != nil {
					method := km[1]
					if method == "" {
						method = km[2]
					}
					if method != "" {
						out = append(out, method)
					}
				}
			}
			atEntryStart = false
			i++
		}
	}
	return out
}

// matchingCloser returns the closing bracket byte for an opening bracket.
func matchingCloser(open byte) byte {
	switch open {
	case '{':
		return '}'
	case '(':
		return ')'
	case '[':
		return ']'
	}
	return open
}

// ---------------------------------------------------------------------------
// Producer side — Python register_function
// ---------------------------------------------------------------------------

// jsonRPCPyRegisterRE matches `<server>.register_function(fn, "name")` and
// captures the explicit registered name (capture 1). The single-arg form
// `register_function(fn)` (name defaults to fn.__name__) is honest-partial
// skipped — the wire name is not statically present.
var jsonRPCPyRegisterRE = regexp.MustCompile(
	`\.\s*register_function\s*\(\s*[^,]+,\s*['"]([A-Za-z_][\w.]*)['"]\s*\)`)

// synthesizePyJSONRPCServer scans a Python file for xmlrpc/jsonrpc
// `register_function(fn, "name")` registrations and emits one producer-side
// http_endpoint_definition per registered name.
func synthesizePyJSONRPCServer(content string, emit emitFn) {
	if !strings.Contains(content, "register_function") {
		return
	}
	seen := map[string]bool{}
	for _, m := range jsonRPCPyRegisterRE.FindAllStringSubmatch(content, -1) {
		method := m[1]
		if isDynamicJSONRPCName(method) || jsonRPCReservedMethod[method] {
			continue
		}
		if seen[method] {
			continue
		}
		seen[method] = true
		emit(jsonRPCVerb, jsonRPCCanonical(method), "xmlrpc", "Method", method)
	}
}

// ---------------------------------------------------------------------------
// Consumer side — JS/TS jayson client
// ---------------------------------------------------------------------------

// jsonRPCJSRequestRE matches `<handle>.request('<method>', …)` — the jayson /
// generic JSON-RPC client request idiom. Capture 1 = the string-literal method
// name (single arg). A non-literal first argument does not match and is skipped.
var jsonRPCJSRequestRE = regexp.MustCompile(
	`\.\s*request\s*\(\s*['"]([A-Za-z_$][\w$.]*)['"]`)

// synthesizeJSJSONRPCClient scans a JS/TS file for jayson `client.request(
// '<method>', params)` calls and emits one consumer-side http_endpoint_call per
// method, keyed http:JSONRPC:/jsonrpc/<method>.
func synthesizeJSJSONRPCClient(content string, funcs []jsFuncSpan, emit emitFn) {
	if !strings.Contains(content, ".request(") {
		return
	}
	// Require a JSON-RPC client signal so this is a no-op on REST `.request(`
	// (e.g. graphql-request, supertest) callers.
	if !strings.Contains(content, "jayson") && !strings.Contains(content, "jsonrpc") &&
		!strings.Contains(content, "json-rpc") {
		return
	}
	seen := map[string]bool{}
	for _, m := range jsonRPCJSRequestRE.FindAllStringSubmatchIndex(content, -1) {
		method := content[m[2]:m[3]]
		if isDynamicJSONRPCName(method) || jsonRPCReservedMethod[method] {
			continue
		}
		if seen[method] {
			continue
		}
		seen[method] = true
		caller := enclosingJSFuncAt(funcs, m[0])
		emit(jsonRPCVerb, jsonRPCCanonical(method), "jayson", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// Consumer side — Python ServerProxy
// ---------------------------------------------------------------------------

// jsonRPCPyProxyAssignRE captures a variable bound to an xmlrpc/jsonrpc
// ServerProxy: `proxy = ServerProxy(url)` / `s = xmlrpc.client.ServerProxy(url)`
// / `c = jsonrpclib.Server(url)`. Capture 1 = handle var name.
var jsonRPCPyProxyAssignRE = regexp.MustCompile(
	`([A-Za-z_]\w*)\s*=\s*(?:[\w.]*\bServerProxy|jsonrpclib\s*\.\s*Server|[\w.]*\bServer)\s*\(`)

// jsonRPCPyProxyCallRE matches `<handle>.<method>(` on a resolved proxy handle.
// Capture 1 = handle ident, 2 = method name (may be dotted, e.g. `user.get`,
// which xmlrpc supports via nested _Method proxies — captured greedily here).
var jsonRPCPyProxyCallRE = regexp.MustCompile(
	`\b([A-Za-z_]\w*)\s*\.\s*([A-Za-z_][\w.]*)\s*\(`)

// synthesizePyJSONRPCClient scans a Python file for xmlrpc/jsonrpc
// `ServerProxy(url).<method>()` calls and emits one consumer-side
// http_endpoint_call per method, keyed http:JSONRPC:/jsonrpc/<method>.
func synthesizePyJSONRPCClient(content string, emit emitFn) {
	if !strings.Contains(content, "ServerProxy") && !strings.Contains(content, "jsonrpclib") {
		return
	}
	proxyVars := map[string]bool{}
	for _, m := range jsonRPCPyProxyAssignRE.FindAllStringSubmatch(content, -1) {
		if m[1] != "" {
			proxyVars[m[1]] = true
		}
	}
	if len(proxyVars) == 0 {
		return
	}
	funcs := indexPyEnclosingFunctions(content)
	seen := map[string]bool{}
	for _, m := range jsonRPCPyProxyCallRE.FindAllStringSubmatchIndex(content, -1) {
		handle := content[m[2]:m[3]]
		method := content[m[4]:m[5]]
		if !proxyVars[handle] {
			continue
		}
		// Drop a trailing dot artifact and reject dunder/system accessors.
		method = strings.TrimRight(method, ".")
		if isDynamicJSONRPCName(method) || jsonRPCReservedMethod[method] ||
			strings.HasPrefix(method, "_") {
			continue
		}
		if seen[method] {
			continue
		}
		seen[method] = true
		caller := enclosingPyFuncAt(funcs, m[0])
		emit(jsonRPCVerb, jsonRPCCanonical(method), "jsonrpc", "Function", caller)
	}
}
