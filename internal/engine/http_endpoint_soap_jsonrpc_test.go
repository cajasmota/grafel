package engine

import (
	"sort"
	"testing"
)

// findEndpointEntity returns the http_endpoint_definition or http_endpoint_call
// entity with the given ID and kind, or nil.
func findEndpointEntity(res *DetectResult, id, kind string) *entityForTest {
	for i := range res.Entities {
		e := res.Entities[i]
		if e.ID == id && e.Kind == kind {
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

// callIDs returns the sorted list of http_endpoint_call IDs in res (for
// diagnostics on failure).
func callIDs(res *DetectResult) []string {
	var got []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind {
			got = append(got, e.ID)
		}
	}
	sort.Strings(got)
	return got
}

// defIDs returns the sorted list of http_endpoint_definition IDs in res.
func defIDs(res *DetectResult) []string {
	var got []string
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind {
			got = append(got, e.ID)
		}
	}
	sort.Strings(got)
	return got
}

// hasFetchesEdge reports whether a FETCHES edge targets the given endpoint ID.
func hasFetchesEdge(res *DetectResult, toID string) bool {
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind && r.ToID == toID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// SOAP
// ---------------------------------------------------------------------------

// TestSynth_SOAP_JavaServer_WebMethod asserts the JAX-WS producer emits both the
// fully-qualified service/op endpoint and the service-less alias.
func TestSynth_SOAP_JavaServer_WebMethod(t *testing.T) {
	src := `
package com.acme.ws;
import javax.jws.WebService;
import javax.jws.WebMethod;

@WebService(serviceName = "UserService")
public class UserEndpoint {
    @WebMethod
    public User GetUser(int id) { return null; }

    @WebMethod
    public void DeleteUser(int id) {}
}
`
	_, res := runDetect(t, "java", "UserEndpoint.java", src)

	for _, want := range []string{
		"http:SOAP:/soap/UserService/GetUser",
		"http:SOAP:/soap/GetUser", // service-less alias
		"http:SOAP:/soap/UserService/DeleteUser",
		"http:SOAP:/soap/DeleteUser",
	} {
		if findEndpointEntity(res, want, httpEndpointDefinitionKind) == nil {
			t.Errorf("missing SOAP producer endpoint %q (got defs: %v)", want, defIDs(res))
		}
	}
	e := findEndpointEntity(res, "http:SOAP:/soap/UserService/GetUser", httpEndpointDefinitionKind)
	if e != nil && e.verb != "SOAP" {
		t.Errorf("verb = %q, want SOAP", e.verb)
	}
}

// TestSynth_SOAP_PyZeepClient is the cross-repo parity oracle: a zeep
// `client.service.GetUser(id)` must emit a client-call whose ID matches the
// server's service-less alias (http:SOAP:/soap/GetUser).
func TestSynth_SOAP_PyZeepClient(t *testing.T) {
	src := `
from zeep import Client

def fetch_user(uid):
    client = Client("http://svc/users?wsdl")
    return client.service.GetUser(uid)
`
	_, res := runDetect(t, "python", "client.py", src)

	const wantID = "http:SOAP:/soap/GetUser"
	e := findEndpointEntity(res, wantID, httpEndpointCallKind)
	if e == nil {
		t.Fatalf("missing SOAP client-call %q (got calls: %v)", wantID, callIDs(res))
	}
	if e.verb != "SOAP" {
		t.Errorf("verb = %q, want SOAP", e.verb)
	}
	if e.sourceCaller != "Function:fetch_user" {
		t.Errorf("source_caller = %q, want Function:fetch_user", e.sourceCaller)
	}
	if !hasFetchesEdge(res, wantID) {
		t.Errorf("missing FETCHES edge → %q", wantID)
	}
}

// TestSynth_SOAP_PyZeepClient_DynamicSkipped is the negative case: a dynamic
// operation name (attribute via getattr-style / variable) must NOT fabricate an
// endpoint. zeep dynamic dispatch `getattr(client.service, op)()` carries no
// literal op, so no `client.service.<literal>(` match exists.
func TestSynth_SOAP_PyZeepClient_DynamicSkipped(t *testing.T) {
	src := `
from zeep import Client

def call(op, uid):
    client = Client("http://svc?wsdl")
    method = getattr(client.service, op)
    return method(uid)
`
	_, res := runDetect(t, "python", "dyn.py", src)
	for _, id := range callIDs(res) {
		if len(id) >= 10 && id[:10] == "http:SOAP:" {
			t.Errorf("fabricated SOAP endpoint from dynamic op: %q", id)
		}
	}
}

// TestSynth_SOAP_JSNodeSoapClient asserts node-soap `client.GetUserAsync(...)`
// strips the Async suffix and emits http:SOAP:/soap/GetUser.
func TestSynth_SOAP_JSNodeSoapClient(t *testing.T) {
	src := `
const soap = require('soap');

async function getUser(id) {
  const client = await soap.createClientAsync('http://svc?wsdl');
  return client.GetUserAsync({ id });
}
`
	_, res := runDetect(t, "javascript", "soapClient.js", src)

	const wantID = "http:SOAP:/soap/GetUser"
	e := findEndpointEntity(res, wantID, httpEndpointCallKind)
	if e == nil {
		t.Fatalf("missing SOAP client-call %q (got calls: %v)", wantID, callIDs(res))
	}
	if e.framework != "node-soap" {
		t.Errorf("framework = %q, want node-soap", e.framework)
	}
}

// TestSynth_SOAP_JavaPortClient asserts a JAX-WS generated-port operation call
// on a resolved port handle emits http:SOAP:/soap/<op>.
func TestSynth_SOAP_JavaPortClient(t *testing.T) {
	src := `
package com.acme.client;
public class Caller {
    public String run() {
        UserService service = new UserService();
        UserPort port = service.getUserPort();
        return port.GetUser(42);
    }
}
`
	_, res := runDetect(t, "java", "Caller.java", src)

	const wantID = "http:SOAP:/soap/GetUser"
	if findEndpointEntity(res, wantID, httpEndpointCallKind) == nil {
		t.Fatalf("missing SOAP client-call %q (got calls: %v)", wantID, callIDs(res))
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC
// ---------------------------------------------------------------------------

// TestSynth_JSONRPC_JaysonServer asserts a jayson server method map emits one
// producer endpoint per method, including a dotted method name.
func TestSynth_JSONRPC_JaysonServer(t *testing.T) {
	src := `
const jayson = require('jayson');
const server = jayson.server({
  add: function (args, cb) { cb(null, args[0] + args[1]); },
  'user.get': function (args, cb) { cb(null, {}); },
});
`
	_, res := runDetect(t, "javascript", "server.js", src)

	for _, want := range []string{
		"http:JSONRPC:/jsonrpc/add",
		"http:JSONRPC:/jsonrpc/user.get",
	} {
		if findEndpointEntity(res, want, httpEndpointDefinitionKind) == nil {
			t.Errorf("missing JSON-RPC producer endpoint %q (got defs: %v)", want, defIDs(res))
		}
	}
}

// TestSynth_JSONRPC_JaysonClient is the parity oracle: `client.request('add', …)`
// must emit a client-call matching the server method endpoint.
func TestSynth_JSONRPC_JaysonClient(t *testing.T) {
	src := `
const jayson = require('jayson');
const client = jayson.client.http('http://svc:3000');

function compute() {
  return client.request('add', [1, 2]);
}
`
	_, res := runDetect(t, "javascript", "client.js", src)

	const wantID = "http:JSONRPC:/jsonrpc/add"
	e := findEndpointEntity(res, wantID, httpEndpointCallKind)
	if e == nil {
		t.Fatalf("missing JSON-RPC client-call %q (got calls: %v)", wantID, callIDs(res))
	}
	if e.verb != "JSONRPC" {
		t.Errorf("verb = %q, want JSONRPC", e.verb)
	}
	if e.sourceCaller != "Function:compute" {
		t.Errorf("source_caller = %q, want Function:compute", e.sourceCaller)
	}
	if !hasFetchesEdge(res, wantID) {
		t.Errorf("missing FETCHES edge → %q", wantID)
	}
}

// TestSynth_JSONRPC_JaysonClient_DynamicSkipped is the negative case: a dynamic
// method name (variable first arg) must NOT fabricate an endpoint.
func TestSynth_JSONRPC_JaysonClient_DynamicSkipped(t *testing.T) {
	src := `
const jayson = require('jayson');
const client = jayson.client.http('http://svc');

function call(method, params) {
  return client.request(method, params);
}
`
	_, res := runDetect(t, "javascript", "dyn.js", src)
	for _, id := range callIDs(res) {
		if len(id) >= 13 && id[:13] == "http:JSONRPC:" {
			t.Errorf("fabricated JSON-RPC endpoint from dynamic method: %q", id)
		}
	}
}

// TestSynth_JSONRPC_PyServerProxyClient asserts xmlrpc `ServerProxy(url).<m>()`
// emits http:JSONRPC:/jsonrpc/<m> matching a `register_function` producer.
func TestSynth_JSONRPC_PyServerProxyClient(t *testing.T) {
	src := `
from xmlrpc.client import ServerProxy

def total():
    proxy = ServerProxy("http://svc:8000")
    return proxy.add(1, 2)
`
	_, res := runDetect(t, "python", "rpc_client.py", src)

	const wantID = "http:JSONRPC:/jsonrpc/add"
	e := findEndpointEntity(res, wantID, httpEndpointCallKind)
	if e == nil {
		t.Fatalf("missing JSON-RPC client-call %q (got calls: %v)", wantID, callIDs(res))
	}
	if e.sourceCaller != "Function:total" {
		t.Errorf("source_caller = %q, want Function:total", e.sourceCaller)
	}
}

// TestSynth_JSONRPC_PyServer asserts `server.register_function(fn, "name")`
// emits a producer endpoint matching the ServerProxy client shape.
func TestSynth_JSONRPC_PyServer(t *testing.T) {
	src := `
from xmlrpc.server import SimpleXMLRPCServer

def add(a, b):
    return a + b

server = SimpleXMLRPCServer(("0.0.0.0", 8000))
server.register_function(add, "add")
`
	_, res := runDetect(t, "python", "rpc_server.py", src)

	const wantID = "http:JSONRPC:/jsonrpc/add"
	if findEndpointEntity(res, wantID, httpEndpointDefinitionKind) == nil {
		t.Fatalf("missing JSON-RPC producer endpoint %q (got defs: %v)", wantID, defIDs(res))
	}
}
