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
