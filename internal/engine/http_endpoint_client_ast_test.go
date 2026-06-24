package engine

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// extractionMethodFor returns the extraction_method property of the first
// http_endpoint_call entity matching id, or ("", false) when absent.
func extractionMethodFor(res *DetectResult, id string) (string, bool) {
	for i := range res.Entities {
		e := &res.Entities[i]
		if e.ID != id {
			continue
		}
		if e.Kind != httpEndpointCallKind && e.Kind != httpEndpointKind {
			continue
		}
		if e.Properties == nil {
			return "", false
		}
		v, ok := e.Properties["extraction_method"]
		return v, ok
	}
	return "", false
}

func requireASTExtracted(t *testing.T, res *DetectResult, id string) {
	t.Helper()
	xm, ok := extractionMethodFor(res, id)
	if !ok || xm != "ast" {
		t.Errorf("expected %q extraction_method=ast (tree-sitter), got %q (present=%v)", id, xm, ok)
	}
}

// TestAST_Fetch_StaticURL proves fetch("/path", {method}) is AST-extracted with
// the correct verb + path and stamped extraction_method=ast (non-heuristic).
func TestAST_Fetch_StaticURL(t *testing.T) {
	src := `
export async function loadUsers() {
  const r = await fetch("/api/users");
  return r.json();
}
export async function createUser(body) {
  return fetch("/api/users", { method: "POST", body });
}
export async function removeUser() {
  return fetch("/api/users/1", { method: "DELETE" });
}
`
	ids, res := runDetect(t, "typescript", "client.ts", src)
	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
		"http:DELETE:/api/users/1",
	}
	requireContains(t, ids, want, "ast-fetch")
	for _, id := range want {
		requireASTExtracted(t, res, id)
	}
}

// TestAST_Axios_VerbMethods proves axios.<verb>("/path") member-call shapes are
// AST-extracted with the verb taken from the method name.
func TestAST_Axios_VerbMethods(t *testing.T) {
	src := `
import axios from "axios";
export async function listOrders() { return axios.get("/api/orders"); }
export async function createOrder(b) { return axios.post("/api/orders", b); }
export async function deleteOrder() { return axios.delete("/api/orders/1"); }
`
	ids, res := runDetect(t, "typescript", "axios.ts", src)
	want := []string{
		"http:GET:/api/orders",
		"http:POST:/api/orders",
		"http:DELETE:/api/orders/1",
	}
	requireContains(t, ids, want, "ast-axios")
	for _, id := range want {
		requireASTExtracted(t, res, id)
	}
}

// TestAST_Axios_ConfigObject proves axios({ url, method }) — the config-object
// form — is AST-extracted (the regex pass does not statically resolve this).
func TestAST_Axios_ConfigObject(t *testing.T) {
	src := `
import axios from "axios";
export async function putThing() {
  return axios({ url: "/api/things/1", method: "put" });
}
export async function getThing() {
  return axios({ url: "/api/things/2" });
}
`
	ids, res := runDetect(t, "typescript", "axios-config.ts", src)
	want := []string{
		"http:PUT:/api/things/1",
		"http:GET:/api/things/2",
	}
	requireContains(t, ids, want, "ast-axios-config")
	for _, id := range want {
		requireASTExtracted(t, res, id)
	}
}

// TestAST_ApiClientWrapper proves an api-client wrapper instance
// (apiClient.<verb>(...)) is AST-extracted as an http_client call.
func TestAST_ApiClientWrapper(t *testing.T) {
	src := `
const apiClient = makeClient();
export async function getProfile() { return apiClient.get("/profile"); }
export async function savePrefs(b) { return apiClient.post("/prefs", b); }
`
	ids, res := runDetect(t, "typescript", "api.ts", src)
	want := []string{"http:GET:/profile", "http:POST:/prefs"}
	requireContains(t, ids, want, "ast-apiclient")
	for _, id := range want {
		requireASTExtracted(t, res, id)
	}
}

// TestAST_TemplateLiteralStaysHeuristic proves the HONESTY boundary: a
// template-literal URL is NOT AST-extracted by this pass — it is left to the
// regex pass and therefore carries no extraction_method=ast stamp.
func TestAST_TemplateLiteralStaysHeuristic(t *testing.T) {
	src := "export async function getUser(id) { return fetch(`/api/users/${id}/profile`); }\n"
	ids, res := runDetect(t, "typescript", "tmpl.ts", src)
	id := "http:GET:/api/users/{id}/profile"
	requireContains(t, ids, []string{id}, "ast-tmpl")
	if xm, ok := extractionMethodFor(res, id); ok && xm == "ast" {
		t.Errorf("%s: template-literal URL must stay heuristic, but was stamped ast", id)
	}
}

// entityByID returns the first entity with the given ID, or nil.
func entityByID(ents []types.EntityRecord, id string) *types.EntityRecord {
	for i := range ents {
		if ents[i].ID == id {
			return &ents[i]
		}
	}
	return nil
}

// TestAST_CrossRepo_FrontBackPair is the end-to-end proof that a fetch client
// call and a matching Express server route synthesize the SAME canonical
// http_endpoint ID (the cross-repo linker's join key, matched by Name across
// repos) — so the pair resolves — AND that the client side is AST-extracted
// (extraction_method=ast) while the producer side is the server route. This is
// the Next/Nest/fetch↔back resolution the ticket targets, at higher confidence.
func TestAST_CrossRepo_FrontBackPair(t *testing.T) {
	// Consumer repo: a static fetch POST.
	clientSrc := `
export async function createOrder(body) {
  return fetch("/api/orders", { method: "POST", body });
}
`
	clientIDs, clientRes := runDetect(t, "typescript", "web/src/api.ts", clientSrc)

	// Producer repo: an Express route serving the same path+verb.
	serverSrc := `
const express = require("express");
const router = express.Router();
router.post("/api/orders", (req, res) => res.json({}));
module.exports = router;
`
	serverIDs, serverRes := runDetect(t, "typescript", "server/src/routes/orders.ts", serverSrc)

	const id = "http:POST:/api/orders"

	// Both sides synthesize the SAME canonical ID — this is what lets the
	// Name-keyed cross-repo HTTP linker pair them.
	requireContains(t, clientIDs, []string{id}, "xrepo-client")
	requireContains(t, serverIDs, []string{id}, "xrepo-server")

	// Client side: the consumer call, AST-extracted (non-heuristic).
	requireASTExtracted(t, clientRes, id)

	// Producer side: the server route is a definition entity, not a call. It
	// stays regex (Express routes are regex-extracted — out of scope), proving
	// we did NOT dishonestly stamp the producer.
	if e := entityByID(serverRes.Entities, id); e != nil {
		if e.Kind != httpEndpointDefinitionKind {
			t.Errorf("producer %s: want definition kind, got %q", id, e.Kind)
		}
		if e.Properties["extraction_method"] == "ast" {
			t.Errorf("producer %s: Express route must stay heuristic, was stamped ast", id)
		}
	}
}
