package engine

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #4324 — anonymous / inline HTTP route handlers (arrow or function-expression)
// must be captured as endpoints AND linked to a handler node. Before the fix the
// route's http_endpoint_definition was emitted but left a graph ISLAND: the
// handler is an inline function with no addressable symbol, so no
// endpoint→handler IMPLEMENTS bridge formed and the route was untraceable.
//
// The long-term fix is handler-shape-agnostic: the producer synthesizer signals
// refKind="InlineHandler", and makeEmit synthesizes a stable inline-handler
// Operation entity (Name derived purely from verb+canonical path → merge-stable)
// plus a file-scoped structural IMPLEMENTS bridge that the CENTRAL resolver binds
// post-merge — the same mechanism #4319 uses for decorated handlers.

// detectInline runs the real engine detector (extract + synthesis) on a single
// file and returns its emitted entities + relationships.
func detectInline(t *testing.T, language, path, content string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	res, err := New(rules).Detect(context.Background(), extreg.FileInput{
		Path: path, Content: []byte(content), Language: language,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res.Entities, res.Relationships
}

// endpointByVerbPath returns the http_endpoint_definition with the given
// canonical (verb, path), or nil.
func endpointByVerbPath(ents []types.EntityRecord, verb, path string) *types.EntityRecord {
	for i := range ents {
		e := ents[i]
		if e.Kind == httpEndpointDefinitionKind && e.Properties != nil &&
			e.Properties["verb"] == verb && e.Properties["path"] == path {
			return &ents[i]
		}
	}
	return nil
}

// inlineHandlerEntity returns the synthesized inline-handler Operation for a
// (verb, canonicalPath), or nil.
func inlineHandlerEntity(ents []types.EntityRecord, verb, path string) *types.EntityRecord {
	want := inlineHandlerName(verb, path)
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == want {
			return &ents[i]
		}
	}
	return nil
}

// assertInlineEndpointBridged runs the full in-pipeline merge+resolve and proves
// the endpoint exists AND has a resolved inbound IMPLEMENTS edge from its
// synthesized inline-handler entity (i.e. is NOT an island).
func assertInlineEndpointBridged(t *testing.T, ents []types.EntityRecord, rels []types.RelationshipRecord, verb, path, framework string) {
	t.Helper()

	endpoint := endpointByVerbPath(ents, verb, path)
	if endpoint == nil {
		t.Fatalf("[%s] endpoint %s %s NOT emitted", framework, verb, path)
	}
	handler := inlineHandlerEntity(ents, verb, path)
	if handler == nil {
		t.Fatalf("[%s] no synthesized inline-handler entity for %s %s", framework, verb, path)
	}
	if handler.Properties["handler_kind"] != "inline" || handler.Properties["framework"] != framework {
		t.Errorf("[%s] inline handler props = %v", framework, handler.Properties)
	}

	// Mirror buildDocument: resolve the http-endpoint pass, stamp IDs, then run
	// the CENTRAL resolver over the synthesis relationships.
	//
	// cloneEnts: the resolve pass CONSUMES its input (it edits records in
	// place and compacts drops over the same backing array, #5954). Callers
	// invoke this helper repeatedly against the SAME fixture set, so each run
	// needs its own copy.
	merged, _ := ResolveHTTPEndpointHandlers(cloneEnts(ents))
	for i := range merged {
		merged[i].ID = merged[i].ComputeID()
	}
	ep := endpointByVerbPath(merged, verb, path)
	h := inlineHandlerEntity(merged, verb, path)
	if ep == nil || h == nil {
		t.Fatalf("[%s] endpoint/handler lost after http-endpoint resolve pass", framework)
	}

	idx := resolve.BuildIndex(merged)
	resolve.References(rels, idx)

	bridged := false
	for _, r := range rels {
		if r.Kind == implementsEdgeKind && r.ToID == ep.ID && r.FromID == h.ID &&
			r.Properties["handler_kind"] == "inline" {
			bridged = true
		}
	}
	if !bridged {
		t.Fatalf("[%s] ISLAND: inline endpoint %s %s has no resolved IMPLEMENTS edge from its handler (#4324)", framework, verb, path)
	}
}

// TestInline4324_ExpressArrowHandlers covers Express inline arrow / async-arrow /
// function-expression handlers on app and router receivers.
func TestInline4324_ExpressArrowHandlers(t *testing.T) {
	src := `const express = require('express');
const app = express();
const router = express.Router();

app.get('/health', (req, res) => res.send('ok'));

router.post('/widgets', async (req, res) => {
  const w = await create(req.body);
  res.status(201).json(w);
});

app.put('/items/:id', function (req, res) {
  res.json({ updated: req.params.id });
});

module.exports = app;
`
	ents, rels := detectInline(t, "javascript", "src/routes/app.js", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/health", "express")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/widgets", "express")
	assertInlineEndpointBridged(t, ents, rels, "PUT", "/items/{id}", "express")
}

// TestInline4324_FastifyArrowHandlers covers Fastify inline handlers.
func TestInline4324_FastifyArrowHandlers(t *testing.T) {
	src := `const Fastify = require('fastify');
const fastify = Fastify();

fastify.get('/ping', (request, reply) => {
  reply.send({ pong: true });
});

fastify.post('/orders', async (request, reply) => {
  return reply.code(201).send(await place(request.body));
});

module.exports = fastify;
`
	ents, rels := detectInline(t, "javascript", "src/server/fastify.js", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/ping", "fastify")
	assertInlineEndpointBridged(t, ents, rels, "POST", "/orders", "fastify")
}

// TestInline4324_KoaRouterArrowHandlers covers koa-router inline handlers, which
// are captured through the Express synthesizer (router receiver allowlist).
func TestInline4324_KoaRouterArrowHandlers(t *testing.T) {
	src := `const Koa = require('koa');
const Router = require('@koa/router');
const app = new Koa();
const router = new Router();

router.get('/status', async (ctx) => {
  ctx.body = 'ok';
});

router.delete('/sessions/:id', (ctx) => {
  ctx.status = 204;
});

app.use(router.routes());
module.exports = app;
`
	ents, rels := detectInline(t, "javascript", "src/koa/routes.js", src)
	assertInlineEndpointBridged(t, ents, rels, "GET", "/status", "express")
	assertInlineEndpointBridged(t, ents, rels, "DELETE", "/sessions/{id}", "express")
}

// TestInline4324_NamedHandlerStillNamedBridge guards no regression: a NAMED
// handler reference must still bridge through the #4319 named/qualified path and
// must NOT produce a synthesized inline-handler stand-in.
func TestInline4324_NamedHandlerStillNamedBridge(t *testing.T) {
	src := `const express = require('express');
const app = express();
function listUsers(req, res) { res.json([]); }
app.get('/users', listUsers);
module.exports = app;
`
	ents, _ := detectInline(t, "javascript", "src/named.js", src)
	if endpointByVerbPath(ents, "GET", "/users") == nil {
		t.Fatal("named-handler endpoint missing")
	}
	if inlineHandlerEntity(ents, "GET", "/users") != nil {
		t.Error("named handler must NOT synthesize an inline-handler stand-in")
	}
}
