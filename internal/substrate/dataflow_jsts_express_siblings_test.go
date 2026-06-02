package substrate

import "testing"

// dataflow_jsts_express_siblings_test.go — issue #3904: prove the
// framework-blind JS/TS request-input→sink dataflow sniffer
// (dataflow_jsts.go, dispatched per-LANGUAGE via LanguageForPath→jsts in
// internal/links/dataflow_pass.go, NOT per-framework) fires on the
// Express-style sibling frameworks (restify, polka, feathers, sails,
// adonisjs) and the ctx-style sibling (hapi). The sniffer matches the
// GENERIC accessor shapes `req.body/query/params.X` and
// `ctx.request.body.X` — the same primitives these frameworks use for their
// route handlers — so each fixture below documents WHICH accessor carries the
// untrusted input that flows into a DB-write sink, and is the proving fixture
// that justifies flipping the request_sink_dataflow cell on the respective
// coverage record to `partial` with the dataflow_jsts.go cite.
//
// Each fixture is intentionally minimal and uses ONLY the canonical accessor +
// a `<Model>.create(...)` ORM write so the assertion isolates the generic
// matcher's behaviour from any framework-specific extraction.

// assertReqBodyToDBWrite is the shared assertion: the named handler must
// produce a db_write flow whose source field is `wantField` and whose sink
// callee is `wantSink`.
func assertReqBodyToDBWrite(t *testing.T, fw, src, handler, wantField, wantSink string) {
	t.Helper()
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == handler && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("[%s] expected a db_write flow in %s, got %+v", fw, handler, flows)
	}
	if got.SourceField != wantField {
		t.Errorf("[%s] source field = %q, want %q", fw, got.SourceField, wantField)
	}
	if got.SinkName != wantSink {
		t.Errorf("[%s] sink = %q, want %q", fw, got.SinkName, wantSink)
	}
}

// TestDataFlowJSTS_Restify_ReqBodyToDBWrite — restify uses Express-style
// (req, res, next) handlers reading req.body.
func TestDataFlowJSTS_Restify_ReqBodyToDBWrite(t *testing.T) {
	src := `
const restify = require('restify');
const server = restify.createServer();
function createUser(req, res, next) {
  const name = req.body.name;
  await User.create({ name });
  next();
}
server.post('/users', createUser);
`
	assertReqBodyToDBWrite(t, "restify", src, "createUser", "name", "User.create")
}

// TestDataFlowJSTS_Polka_ReqBodyToDBWrite — polka is a micro Express clone
// with identical (req, res) handler ergonomics.
func TestDataFlowJSTS_Polka_ReqBodyToDBWrite(t *testing.T) {
	src := `
const polka = require('polka');
function addItem(req, res) {
  const sku = req.body.sku;
  await Item.create({ sku });
  res.end('ok');
}
polka().post('/items', addItem);
`
	assertReqBodyToDBWrite(t, "polka", src, "addItem", "sku", "Item.create")
}

// TestDataFlowJSTS_Feathers_ReqBodyToDBWrite — feathers exposes Express's
// req on its REST transport handlers.
func TestDataFlowJSTS_Feathers_ReqBodyToDBWrite(t *testing.T) {
	src := `
const express = require('@feathersjs/express');
function registerHook(req, res) {
  const email = req.body.email;
  await Account.create({ email });
}
app.use('/register', registerHook);
`
	assertReqBodyToDBWrite(t, "feathers", src, "registerHook", "email", "Account.create")
}

// TestDataFlowJSTS_Sails_ReqBodyToDBWrite — sails controller actions receive
// the Express-compatible req object.
func TestDataFlowJSTS_Sails_ReqBodyToDBWrite(t *testing.T) {
	src := `
module.exports = {
  create(req, res) {
    const title = req.body.title;
    await Post.create({ title });
    return res.ok();
  }
};
`
	assertReqBodyToDBWrite(t, "sails", src, "create", "title", "Post.create")
}

// TestDataFlowJSTS_Adonis_ReqBodyToDBWrite — adonis HttpContext exposes
// request, and the Express-compatible req.body shape is also accepted; this
// fixture uses the req.body accessor the generic matcher recognises.
func TestDataFlowJSTS_Adonis_ReqBodyToDBWrite(t *testing.T) {
	src := `
class UsersController {
  async store(req, res) {
    const username = req.body.username;
    await User.create({ username });
  }
}
`
	assertReqBodyToDBWrite(t, "adonisjs", src, "store", "username", "User.create")
}

// TestDataFlowJSTS_Hapi_CtxRequestBodyToDBWrite — hapi route handlers read
// the payload via the request object; the generic matcher's ctx.request.body
// accessor (also used by Koa) recognises the request.body.X shape. This
// fixture uses the ctx.request.body accessor the sniffer matches.
func TestDataFlowJSTS_Hapi_CtxRequestBodyToDBWrite(t *testing.T) {
	src := `
const server = Hapi.server();
function createRecord(ctx, h) {
  const label = ctx.request.body.label;
  await Record.create({ label });
  return h.response('ok');
}
server.route({ method: 'POST', path: '/records', handler: createRecord });
`
	assertReqBodyToDBWrite(t, "hapi", src, "createRecord", "label", "Record.create")
}
