package substrate

import "testing"

// dataflow_jsts_framework_sources_test.go — issue #3927: generalize
// request-source recognition for the jsts DATA_FLOWS_TO sniffer beyond
// Express/NestJS. The sink set + multi-hop machinery is unchanged; only SOURCE
// recognition is extended to:
//   - Hapi   : request.payload.X
//   - Koa    : ctx.query.X / ctx.params.X / ctx.request.query.X
//   - Fastify: request.body/query/params.X (confirmed via the request. prefix)
// Each fixture asserts the RESOLVED sink AND the recovered request-source
// field — never a bare len>0 — and is the proving fixture for flipping the
// respective coverage cell. assertReqBodyToDBWrite / findFlow are shared with
// dataflow_jsts_express_siblings_test.go.

// assertJstsReqToSink generalises assertReqBodyToDBWrite to any sink kind.
func assertJstsReqToSink(t *testing.T, fw, src, handler, wantField string, wantKind DataFlowSinkKind, wantSink string) {
	t.Helper()
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == handler && f.SinkKind == wantKind
	})
	if got == nil {
		t.Fatalf("[%s] expected a %s flow in %s, got %+v", fw, wantKind, handler, flows)
	}
	if got.SourceField != wantField {
		t.Errorf("[%s] source field = %q, want %q", fw, got.SourceField, wantField)
	}
	if got.SinkName != wantSink {
		t.Errorf("[%s] sink = %q, want %q", fw, got.SinkName, wantSink)
	}
}

// TestDataFlowJSTS_Hapi_PayloadToDBWrite — hapi route handlers read the parsed
// body via `request.payload`; the previous matcher only knew request.body.
func TestDataFlowJSTS_Hapi_PayloadToDBWrite(t *testing.T) {
	src := `
const server = Hapi.server();
function createUser(request, h) {
  const email = request.payload.email;
  await User.create({ email });
  return h.response('ok');
}
server.route({ method: 'POST', path: '/users', handler: createUser });
`
	assertJstsReqToSink(t, "hapi-payload", src, "createUser", "email", DataFlowSinkDBWrite, "User.create")
}

// TestDataFlowJSTS_Koa_CtxQueryToResponse — Koa exposes query string params on
// `ctx.query` (not under ctx.request).
func TestDataFlowJSTS_Koa_CtxQueryToResponse(t *testing.T) {
	src := `
async function search(ctx) {
  const q = ctx.query.q;
  ctx.body = q;
  res.json(q);
}
`
	assertJstsReqToSink(t, "koa-ctx-query", src, "search", "q", DataFlowSinkResponse, "res.json")
}

// TestDataFlowJSTS_Koa_CtxParamsToDBWrite — Koa route params on `ctx.params`.
func TestDataFlowJSTS_Koa_CtxParamsToDBWrite(t *testing.T) {
	src := `
async function updateItem(ctx) {
  const id = ctx.params.id;
  await Item.update({ id });
  ctx.status = 200;
}
`
	assertJstsReqToSink(t, "koa-ctx-params", src, "updateItem", "id", DataFlowSinkDBWrite, "Item.update")
}

// TestDataFlowJSTS_Koa_CtxRequestQueryToDBWrite — Koa also exposes the parsed
// query under ctx.request.query.
func TestDataFlowJSTS_Koa_CtxRequestQueryToDBWrite(t *testing.T) {
	src := `
async function track(ctx) {
  const ref = ctx.request.query.ref;
  await Visit.create({ ref });
}
`
	assertJstsReqToSink(t, "koa-ctx-request-query", src, "track", "ref", DataFlowSinkDBWrite, "Visit.create")
}

// TestDataFlowJSTS_Fastify_RequestBodyToDBWrite — Fastify handlers take
// (request, reply) and read request.body; the request. prefix already covers
// this. This fixture documents+locks the Fastify idiom explicitly.
func TestDataFlowJSTS_Fastify_RequestBodyToDBWrite(t *testing.T) {
	src := `
fastify.post('/users', async (request, reply) => {});
async function createUser(request, reply) {
  const name = request.body.name;
  await User.create({ name });
  reply.send('ok');
}
`
	assertJstsReqToSink(t, "fastify-body", src, "createUser", "name", DataFlowSinkDBWrite, "User.create")
}

// TestDataFlowJSTS_Fastify_RequestQueryToHTTPCall — Fastify request.query into
// an outbound fetch call.
func TestDataFlowJSTS_Fastify_RequestQueryToHTTPCall(t *testing.T) {
	src := `
async function proxy(request, reply) {
  const url = request.query.url;
  await fetch(url);
  reply.send('ok');
}
`
	assertJstsReqToSink(t, "fastify-query", src, "proxy", "url", DataFlowSinkHTTPCall, "fetch")
}

// --- Negatives: precision guards (honest-partial) ---

// TestDataFlowJSTS_NonRequestCtxField_NoFlow — `ctx.state.x` is server-set
// (auth context), NOT a request input, and must NOT be recognised as a source.
func TestDataFlowJSTS_NonRequestCtxField_NoFlow(t *testing.T) {
	src := `
async function whoami(ctx) {
  const uid = ctx.state.userId;
  await Audit.create({ uid });
}
`
	flows := sniffDataFlowJSTS(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("ctx.state (non-request) must not flow, got %+v", got)
	}
}

// TestDataFlowJSTS_DynamicPayloadKey_NoFlow — a dynamic payload key
// (`request.payload[k]`) is not statically knowable and must be dropped.
func TestDataFlowJSTS_DynamicPayloadKey_NoFlow(t *testing.T) {
	src := `
function createUser(request, h) {
  const v = request.payload[k];
  await User.create({ v });
}
`
	flows := sniffDataFlowJSTS(src)
	if got := findFlow(flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite && f.SourceField != ""
	}); got != nil {
		t.Fatalf("dynamic payload key must not yield a static-field flow, got %+v", got)
	}
}
