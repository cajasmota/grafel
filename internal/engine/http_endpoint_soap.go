// SOAP server-side (producer) + client-side (consumer) synthetic http_endpoint
// emission for cross-repo matching (epic #3628, "[protocol] SOAP + JSON-RPC
// client cross-link").
//
// Background — the cross-repo client-link mechanism
// -------------------------------------------------
// The REST / GraphQL / WS consumer passes all share one trick: they emit a
// synthetic `http_endpoint_call` whose ID is byte-for-byte identical to the
// PRODUCER-side `http_endpoint_definition` for the same logical operation. The
// existing Name-based cross-repo HTTP linker (links/http_pass.go) then joins
// the two with NO new linker code, because it pairs synthetics across repos by
// their canonical `http:<VERB>:<path>` Name.
//
// Before this pass SOAP was invisible to that machinery. The Jakarta custom
// extractor (internal/custom/java/jakarta_ee_advanced.go) records a
// `@WebService` class and its `@WebMethod`s as plain SCOPE.Service /
// SCOPE.Operation structural entities — useful locally, but they live in a
// different ID space and can never join a client call.
//
// Canonical SOAP id
// -----------------
// We adopt the synthetic verb SOAP and the canonical path
//
//	/soap/<Service>/<Operation>
//
// giving the id  http:SOAP:/soap/<Service>/<Operation>. Both the producer
// (server) and consumer (client) sides emit this exact shape so they join.
// When the service binding name is not statically resolvable on the client
// side (the dominant case — the operation is invoked on a generated port /
// dynamic proxy whose WSDL service name is not in the file) we honest-partial
// to a service-less id  http:SOAP:/soap/<Operation>, and the server side ALSO
// registers that service-less alias so a client that only knows the operation
// still links.
//
// Producer side (server)
// ----------------------
//   - Java JAX-WS: `@WebService` class + `@WebMethod` (or any public method on
//     the annotated endpoint interface/class). Service name = the explicit
//     `@WebService(name="…"/serviceName="…")` attribute when present, else the
//     class name. Each operation → http:SOAP:/soap/<Service>/<op> AND the
//     service-less alias http:SOAP:/soap/<op>.
//
// Consumer side (client)
// ----------------------
//   - Python zeep: `client.service.<Op>(...)` on a zeep.Client. The service
//     binding name is not in the call expression, so these honest-partial to
//     http:SOAP:/soap/<Op>.
//   - JS/TS node-soap: `soap.createClient(...)` + `client.<Op>Async(...)` /
//     `client.<Op>(...)`. The `Async` suffix (node-soap's promise convention)
//     is stripped to recover the WSDL operation name. Service-less id.
//   - Java JAX-WS generated port: `port.<op>(...)` where `port` was obtained
//     from `service.get<Port>()` / a `@WebServiceRef`-injected proxy. We
//     resolve the port handle variables and emit http:SOAP:/soap/<op>.
//
// Honest-partial / non-fabrication
//   - Dynamic operation names (variable / template) are SKIPPED — no string,
//     no fabricated endpoint.
//   - Java/Python lifecycle/accessor calls on the handle (getPort, getClass,
//     toString, close, …) are skipped — they are not SOAP operations.
//   - The client passes are file-signal gated (zeep / soap.createClient /
//     a resolvable JAX-WS port) so they no-op on ordinary files.

package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// soapVerb is the synthetic HTTP verb used for every SOAP operation endpoint,
// shared by the producer and all consumer passes so their ids join.
const soapVerb = "SOAP"

// soapEndpointPath builds the canonical SOAP path for a (service, operation)
// pair. When service is empty the service-less honest-partial form is used.
func soapEndpointPath(service, op string) string {
	if service == "" {
		return "/soap/" + op
	}
	return "/soap/" + service + "/" + op
}

// soapCanonical returns the canonical, slash-normalised path for the given
// (service, op). FrameworkExpress is a no-op canonicaliser for our paths (no
// path params) — it only normalises slashes, matching the GraphQL/WS passes.
func soapCanonical(service, op string) string {
	return httproutes.Canonicalize(httproutes.FrameworkExpress, soapEndpointPath(service, op))
}

// soapNonOperationCall is the set of handle method names that are NOT SOAP
// operations — JVM/Python object accessors and zeep/node-soap lifecycle calls.
// Used by the client passes to avoid fabricating an endpoint from e.g.
// `port.toString()` or `client.wsdl`.
var soapNonOperationCall = map[string]bool{
	"toString": true, "hashCode": true, "equals": true, "getClass": true,
	"wait": true, "notify": true, "notifyAll": true, "clone": true,
	"close": true, "getPort": true, "create": true, "getClient": true,
	"describe": true, "setOptions": true, "addSoapHeader": true,
	"on": true, "wsdl": true,
}

// isDynamicSOAPName reports whether an extracted operation/service token is a
// stable string literal we can safely build an endpoint from. Anything carrying
// interpolation/template markers is dynamic → honest-partial skip.
func isDynamicSOAPName(name string) bool {
	return name == "" || strings.ContainsAny(name, "${}`+ \t\r\n(")
}

// ---------------------------------------------------------------------------
// Producer side — Java JAX-WS @WebService / @WebMethod
// ---------------------------------------------------------------------------

// soapWebServiceClassRE matches a `@WebService`-annotated class or interface and
// captures (1) the optional annotation attribute block and (2) the type name.
var soapWebServiceClassRE = regexp.MustCompile(
	`(?s)@WebService\b\s*(\([^)]*\))?[^{};]*?\b(?:class|interface)\s+(\w+)`)

// soapWSNameAttrRE extracts `name="…"` or `serviceName="…"` from a @WebService
// attribute block; serviceName is preferred when both are present.
var (
	soapWSNameAttrRE        = regexp.MustCompile(`\bname\s*=\s*"([^"]+)"`)
	soapWSServiceNameAttrRE = regexp.MustCompile(`\bserviceName\s*=\s*"([^"]+)"`)
)

// soapWebMethodRE matches a `@WebMethod`-annotated Java method and captures the
// method name. The optional `operationName="…"` attribute (captured separately)
// overrides the method name as the WSDL operation when present.
var soapWebMethodRE = regexp.MustCompile(
	`(?s)@WebMethod\b\s*(\([^)]*\))?[^;{]*?\b\w[\w<>\[\],.\s]*?\s+(\w+)\s*\(`)

var soapOperationNameAttrRE = regexp.MustCompile(`\boperationName\s*=\s*"([^"]+)"`)

// synthesizeJavaSOAPServer scans a Java file for JAX-WS `@WebService` endpoints
// and emits one producer-side http_endpoint_definition per `@WebMethod`
// operation, keyed http:SOAP:/soap/<Service>/<op> AND the service-less alias
// http:SOAP:/soap/<op> so a client that only knows the operation still joins.
//
// It is invoked from applyHTTPEndpointSynthesis (case "java").
func synthesizeJavaSOAPServer(content string, emit emitFn) {
	if !strings.Contains(content, "@WebService") {
		return
	}
	// Locate every @WebService type and the byte span of its class body so each
	// @WebMethod can be attributed to the right service. We use a simple
	// scan: an operation belongs to the nearest preceding @WebService class.
	type svc struct {
		name  string
		start int
	}
	var services []svc
	for _, m := range soapWebServiceClassRE.FindAllStringSubmatchIndex(content, -1) {
		attr := ""
		if m[2] >= 0 {
			attr = content[m[2]:m[3]]
		}
		typeName := content[m[4]:m[5]]
		name := typeName
		if sm := soapWSServiceNameAttrRE.FindStringSubmatch(attr); len(sm) == 2 {
			name = sm[1]
		} else if nm := soapWSNameAttrRE.FindStringSubmatch(attr); len(nm) == 2 {
			name = nm[1]
		}
		services = append(services, svc{name: name, start: m[0]})
	}
	if len(services) == 0 {
		return
	}

	serviceFor := func(pos int) string {
		name := services[0].name
		for _, s := range services {
			if s.start <= pos {
				name = s.name
			} else {
				break
			}
		}
		return name
	}

	seen := map[string]bool{}
	for _, m := range soapWebMethodRE.FindAllStringSubmatchIndex(content, -1) {
		attr := ""
		if m[2] >= 0 {
			attr = content[m[2]:m[3]]
		}
		opName := content[m[4]:m[5]]
		if om := soapOperationNameAttrRE.FindStringSubmatch(attr); len(om) == 2 {
			opName = om[1]
		}
		if isDynamicSOAPName(opName) || soapNonOperationCall[opName] {
			continue
		}
		service := serviceFor(m[0])
		// Emit the fully-qualified service/op endpoint.
		if !seen[service+"\x00"+opName] {
			seen[service+"\x00"+opName] = true
			emit(soapVerb, soapCanonical(service, opName), "jaxws", "Method", service+"."+opName)
		}
		// Emit the service-less alias so a client that only knows the operation
		// name (the common honest-partial client case) joins.
		if !seen["\x00"+opName] {
			seen["\x00"+opName] = true
			emit(soapVerb, soapCanonical("", opName), "jaxws", "Method", service+"."+opName)
		}
	}
}

// ---------------------------------------------------------------------------
// Consumer side — Python zeep
// ---------------------------------------------------------------------------

// soapPyZeepCallRE matches `<handle>.service.<Op>(` where <handle> is a zeep
// Client instance. Capture 1 = handle ident, 2 = operation name.
var soapPyZeepCallRE = regexp.MustCompile(
	`\b([A-Za-z_][\w]*)\s*\.\s*service\s*\.\s*([A-Za-z_]\w*)\s*\(`)

// synthesizePySOAPClient scans a Python file for zeep `client.service.<Op>(...)`
// SOAP calls and emits one consumer-side http_endpoint_call per operation,
// keyed http:SOAP:/soap/<Op> (service-less honest-partial — the WSDL service
// binding name is not present at the call site).
func synthesizePySOAPClient(content string, emit emitFn) {
	if !strings.Contains(content, "zeep") && !strings.Contains(content, ".service.") {
		return
	}
	funcs := indexPyEnclosingFunctions(content)
	seen := map[string]bool{}
	for _, m := range soapPyZeepCallRE.FindAllStringSubmatchIndex(content, -1) {
		op := content[m[4]:m[5]]
		if isDynamicSOAPName(op) || soapNonOperationCall[op] {
			continue
		}
		if seen[op] {
			continue
		}
		seen[op] = true
		caller := enclosingPyFuncAt(funcs, m[0])
		emit(soapVerb, soapCanonical("", op), "zeep", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// Consumer side — JS/TS node-soap
// ---------------------------------------------------------------------------

// soapJSOpCallRE matches `<handle>.<Op>(` / `<handle>.<Op>Async(` on a node-soap
// client. Capture 1 = handle ident, 2 = operation (possibly with Async suffix).
var soapJSOpCallRE = regexp.MustCompile(
	`\b([A-Za-z_$][\w$]*)\s*\.\s*([A-Za-z_$][\w$]*?)(Async)?\s*\(`)

// soapJSClientVarRE captures the variable bound to the node-soap client in the
// `soap.createClient(url, (err, client) => {…})` callback or
// `await soap.createClientAsync(url)` assignment. Capture 1 = callback client
// param, 2 = awaited-assignment var name.
var soapJSClientVarRE = regexp.MustCompile(
	`createClient(?:Async)?\s*\([^)]*?\(\s*[A-Za-z_$][\w$]*\s*,\s*([A-Za-z_$][\w$]*)\s*\)` +
		`|(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+\w+\.createClientAsync`)

// synthesizeJSSOAPClient scans a JS/TS file for node-soap operation calls and
// emits one consumer-side http_endpoint_call per operation, service-less
// (http:SOAP:/soap/<Op>). The `Async` promise-suffix is stripped to recover the
// WSDL operation name.
func synthesizeJSSOAPClient(content string, funcs []jsFuncSpan, emit emitFn) {
	if !strings.Contains(content, "createClient") || !strings.Contains(content, "soap") {
		return
	}
	// Resolve the set of node-soap client handle identifiers in the file.
	clientVars := map[string]bool{"client": true, "soapClient": true}
	for _, m := range soapJSClientVarRE.FindAllStringSubmatch(content, -1) {
		if m[1] != "" {
			clientVars[m[1]] = true
		}
		if m[2] != "" {
			clientVars[m[2]] = true
		}
	}
	seen := map[string]bool{}
	for _, m := range soapJSOpCallRE.FindAllStringSubmatchIndex(content, -1) {
		handle := content[m[2]:m[3]]
		op := content[m[4]:m[5]]
		if !clientVars[handle] {
			continue
		}
		if isDynamicSOAPName(op) || soapNonOperationCall[op] {
			continue
		}
		if seen[op] {
			continue
		}
		seen[op] = true
		caller := enclosingJSFuncAt(funcs, m[0])
		emit(soapVerb, soapCanonical("", op), "node-soap", "Function", caller)
	}
}

// ---------------------------------------------------------------------------
// Consumer side — Java JAX-WS generated port
// ---------------------------------------------------------------------------

// soapJavaPortAssignRE captures a JAX-WS port handle: a variable obtained from
// `service.get<Port>()` or `new <Service>().get<Port>()`, or a field/param
// annotated `@WebServiceRef`. Capture 1 = var from a getPort assignment,
// 2 = field name from a @WebServiceRef declaration.
var soapJavaPortAssignRE = regexp.MustCompile(
	`(?:[\w.<>\[\] ]+\s+)?([A-Za-z_]\w*)\s*=\s*[\w.]*\bget\w*Port\w*\s*\(\s*\)` +
		`|@WebServiceRef\b[^;]*?\b(\w+)\s*;`)

// soapJavaPortCallRE matches `<handle>.<op>(` on a resolved port handle.
// Capture 1 = handle ident, 2 = operation name.
var soapJavaPortCallRE = regexp.MustCompile(
	`\b([A-Za-z_]\w*)\s*\.\s*([A-Za-z_]\w*)\s*\(`)

// synthesizeJavaSOAPClient scans a Java file for JAX-WS generated-port operation
// invocations and emits one consumer-side http_endpoint_call per operation,
// service-less (http:SOAP:/soap/<op>). Only calls on a resolved port handle
// (getPort assignment or @WebServiceRef field) are considered, so ordinary
// method calls are not mistaken for SOAP operations.
func synthesizeJavaSOAPClient(content string, emit emitFn) {
	// Gate: require a JAX-WS port acquisition signal in the file.
	if !strings.Contains(content, "Port") && !strings.Contains(content, "@WebServiceRef") {
		return
	}
	portVars := map[string]bool{}
	for _, m := range soapJavaPortAssignRE.FindAllStringSubmatch(content, -1) {
		if m[1] != "" {
			portVars[m[1]] = true
		}
		if m[2] != "" {
			portVars[m[2]] = true
		}
	}
	if len(portVars) == 0 {
		return
	}
	methods := indexJavaEnclosingMethods(content)
	seen := map[string]bool{}
	for _, m := range soapJavaPortCallRE.FindAllStringSubmatchIndex(content, -1) {
		handle := content[m[2]:m[3]]
		op := content[m[4]:m[5]]
		if !portVars[handle] {
			continue
		}
		if isDynamicSOAPName(op) || soapNonOperationCall[op] {
			continue
		}
		if seen[op] {
			continue
		}
		seen[op] = true
		caller := enclosingJavaMethodAt(methods, m[0])
		emit(soapVerb, soapCanonical("", op), "jaxws-client", "Function", caller)
	}
}
