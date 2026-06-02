package substrate

import "testing"

// findFlow returns the first flow matching the predicate, or nil.
func findFlow(flows []DataFlow, pred func(DataFlow) bool) *DataFlow {
	for i := range flows {
		if pred(flows[i]) {
			return &flows[i]
		}
	}
	return nil
}

func TestDataFlowJSTS_IntraFn_DBWrite_Field(t *testing.T) {
	src := `
function createUser(req, res) {
  const name = req.body.name;
  await User.create({ name });
}
`
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "createUser" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected a db_write flow in createUser, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
	if got.SinkName != "User.create" {
		t.Errorf("sink = %q, want User.create", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn (no hop), got hop=%q", got.HopVia)
	}
}

func TestDataFlowJSTS_PassThrough_Response(t *testing.T) {
	src := `
function search(req, res) {
  res.json(req.query.q);
}
`
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkResponse })
	if got == nil {
		t.Fatalf("expected a response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
	if got.SinkName != "res.json" {
		t.Errorf("sink = %q, want res.json", got.SinkName)
	}
}

func TestDataFlowJSTS_OneHop_LocalFunction(t *testing.T) {
	src := `
function handler(req, res) {
  const x = req.body.x;
  save(x);
}
function save(v) {
  repo.insert(v);
}
`
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "handler" && f.HopVia == "save"
	})
	if got == nil {
		t.Fatalf("expected a one-hop flow handler->save, got %+v", flows)
	}
	if got.SinkKind != DataFlowSinkDBWrite || got.SinkName != "repo.insert" {
		t.Errorf("sink = %q/%s, want repo.insert/db_write", got.SinkName, got.SinkKind)
	}
	if got.SourceField != "x" {
		t.Errorf("source field = %q, want x", got.SourceField)
	}
}

func TestDataFlowJSTS_Negative_StaticValue(t *testing.T) {
	src := `
function createUser(req, res) {
  const name = 'static';
  await User.create({ name });
}
`
	flows := sniffDataFlowJSTS(src)
	if len(flows) != 0 {
		t.Fatalf("expected NO flow for static value, got %+v", flows)
	}
}

func TestDataFlowJSTS_Negative_ReassignBreaksChain(t *testing.T) {
	src := `
function createUser(req, res) {
  let name = req.body.name;
  name = 'override';
  await User.create({ name });
}
`
	flows := sniffDataFlowJSTS(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite })
	if got != nil {
		t.Fatalf("expected NO db_write flow after chain-breaking reassign, got %+v", *got)
	}
}

func TestDataFlowJSTS_Negative_NoSource(t *testing.T) {
	src := `
function createUser(req, res) {
  const name = computeName();
  await User.create({ name });
}
`
	flows := sniffDataFlowJSTS(src)
	if len(flows) != 0 {
		t.Fatalf("expected NO flow when value not request-derived, got %+v", flows)
	}
}

// ---- multi-hop (within-file) ----

func TestDataFlowJSTS_TwoHop_LocalChain(t *testing.T) {
	src := `
function handler(req, res) {
  const x = req.body.x;
  a(x);
}
function a(v) {
  b(v);
}
function b(w) {
  repo.insert(w);
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "handler" && f.SinkName == "repo.insert"
	})
	if got == nil {
		t.Fatalf("expected 2-hop flow handler->a->b->repo.insert, got %+v", flows)
	}
	if got.SourceField != "x" {
		t.Errorf("field = %q, want x", got.SourceField)
	}
	if len(got.HopPath) != 2 || got.HopPath[0] != "a" || got.HopPath[1] != "b" {
		t.Errorf("hop_path = %v, want [a b]", got.HopPath)
	}
	if got.HopVia != "a" {
		t.Errorf("hop_via = %q, want a", got.HopVia)
	}
}

func TestDataFlowJSTS_ThreeHop_Boundary_Inclusive(t *testing.T) {
	// handler->a->b->c->sink would be 3 hops which is the max; reachable.
	src := `
function handler(req, res) {
  const x = req.body.x;
  a(x);
}
function a(v) { b(v); }
function b(w) { c(w); }
function c(z) { Model.create(z); }
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "Model.create" })
	if got == nil {
		t.Fatalf("expected 3-hop flow to Model.create, got %+v", flows)
	}
	if len(got.HopPath) != 3 {
		t.Errorf("hop_path len = %d (%v), want 3", len(got.HopPath), got.HopPath)
	}
}

func TestDataFlowJSTS_Negative_FourthHopDropped(t *testing.T) {
	// handler->a->b->c->d->sink is 4 hops > DataFlowMaxHops; must NOT reach.
	src := `
function handler(req, res) {
  const x = req.body.x;
  a(x);
}
function a(v) { b(v); }
function b(w) { c(w); }
function c(z) { d(z); }
function d(q) { Model.create(q); }
`
	flows := sniffDataFlowJSTSEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "Model.create" }); got != nil {
		t.Fatalf("expected NO flow at 4th hop, got %+v", *got)
	}
}

func TestDataFlowJSTS_Negative_RecursionStops(t *testing.T) {
	src := `
function handler(req, res) {
  const x = req.body.x;
  rec(x);
}
function rec(v) {
  rec(v);
  Model.create(v);
}
`
	// The direct sink inside rec at hop 1 is fine; the recursive self-call
	// must NOT loop or fabricate further flows.
	flows := sniffDataFlowJSTSEx(src).Flows
	n := 0
	for _, f := range flows {
		if f.SinkName == "Model.create" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 Model.create flow (no recursion blowup), got %d: %+v", n, flows)
	}
}

func TestDataFlowJSTS_Negative_EmbeddedArgNotBound(t *testing.T) {
	// helper(x + 1) is not a clean positional pass — must NOT hop.
	src := `
function handler(req, res) {
  const x = req.body.x;
  helper(x + 1);
}
function helper(v) { Model.create(v); }
`
	flows := sniffDataFlowJSTSEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "Model.create" }); got != nil {
		t.Fatalf("expected NO hop for embedded arg, got %+v", *got)
	}
}

func TestDataFlowJSTS_Negative_SpreadAmbiguousArg(t *testing.T) {
	// helper(...args) — spread makes positional binding ambiguous → no hop,
	// and (cross-file would-be) no boundary either.
	src := `
function handler(req, res) {
  const x = req.body.x;
  const args = [x];
  helper(...args);
}
function helper(v) { Model.create(v); }
`
	res := sniffDataFlowJSTSEx(src)
	if got := findFlow(res.Flows, func(f DataFlow) bool { return f.SinkName == "Model.create" }); got != nil {
		t.Fatalf("expected NO hop through spread, got %+v", *got)
	}
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected NO boundary for spread call, got %+v", res.Boundaries)
	}
}

// ---- cross-file boundary emission (resolution covered in links tests) ----

func TestDataFlowJSTS_Boundary_ImportedHelper(t *testing.T) {
	// `save` is not defined in this file → boundary, not a dropped flow.
	src := `
import { save } from './svc';
function handler(req, res) {
  const x = req.body.name;
  save(x);
}
`
	res := sniffDataFlowJSTSEx(src)
	if len(res.Boundaries) != 1 {
		t.Fatalf("expected 1 cross-file boundary, got %+v", res.Boundaries)
	}
	b := res.Boundaries[0]
	if b.Callee != "save" || b.ArgIndex != 0 || b.Function != "handler" {
		t.Errorf("boundary = %+v, want callee=save arg=0 fn=handler", b)
	}
	if b.SourceField != "name" {
		t.Errorf("boundary field = %q, want name", b.SourceField)
	}
}

func TestDataFlowJSTS_Continue_BindsParamToSink(t *testing.T) {
	// The continuation entry binds the value into save's param and finds sink.
	svc := `
export function save(v) {
  Model.create({ v });
}
`
	res := continueDataFlowJSTS(svc, "save", 0, "name", 0)
	got := findFlow(res.Flows, func(f DataFlow) bool { return f.SinkName == "Model.create" })
	if got == nil {
		t.Fatalf("expected continuation to reach Model.create, got %+v", res.Flows)
	}
	if got.SourceField != "name" {
		t.Errorf("field = %q, want name", got.SourceField)
	}
}
