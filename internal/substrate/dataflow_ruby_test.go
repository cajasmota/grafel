package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real Rails/Sinatra/Grape forms) ----

func TestDataFlowRuby_IntraFn_DBWrite_Field(t *testing.T) {
	src := "" +
		"def create\n" +
		"  @u = User.create(email: params[:email])\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
	if got.SinkName != "User.create" {
		t.Errorf("sink = %q, want User.create", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

func TestDataFlowRuby_StrongParams_PermitField(t *testing.T) {
	src := "" +
		"def create\n" +
		"  user_params = params.require(:user).permit(:name)\n" +
		"  User.create(user_params[:name])\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow from strong params, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
	if got.SinkName != "User.create" {
		t.Errorf("sink = %q, want User.create", got.SinkName)
	}
}

func TestDataFlowRuby_StrongParams_MassAssign_EmptyField(t *testing.T) {
	// Whole-hash mass-assignment of a strong-params var: the individual
	// attribute is NOT derivable at the call site → field="" (honest-partial).
	src := "" +
		"def create\n" +
		"  user_params = params.require(:user).permit(:name, :email)\n" +
		"  User.create(user_params)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkName == "User.create"
	})
	if got == nil {
		t.Fatalf("expected db_write flow for mass-assign, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (mass-assign)", got.SourceField)
	}
}

func TestDataFlowRuby_Sinatra_PassThrough_Render(t *testing.T) {
	src := "" +
		"def search\n" +
		"  render plain: params['q']\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkResponse })
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
	if got.SinkName != "render" {
		t.Errorf("sink = %q, want render", got.SinkName)
	}
}

func TestDataFlowRuby_RawSQL_Sink(t *testing.T) {
	src := "" +
		"def run\n" +
		"  id = params[:id]\n" +
		"  ActiveRecord::Base.connection.execute(id)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite })
	if got == nil {
		t.Fatalf("expected raw SQL db_write flow, got %+v", flows)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
	if got.SinkName != "ActiveRecord::Base.connection.execute" {
		t.Errorf("sink = %q, want ActiveRecord::Base.connection.execute", got.SinkName)
	}
}

func TestDataFlowRuby_HTTPCall_Sink(t *testing.T) {
	src := "" +
		"def proxy\n" +
		"  body = params[:payload]\n" +
		"  Faraday.post(body)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkHTTPCall })
	if got == nil {
		t.Fatalf("expected http_call flow, got %+v", flows)
	}
	if got.SourceField != "payload" {
		t.Errorf("source field = %q, want payload", got.SourceField)
	}
	if got.SinkName != "Faraday.post" {
		t.Errorf("sink = %q, want Faraday.post", got.SinkName)
	}
}

func TestDataFlowRuby_Grape_BoundVar_DBWrite(t *testing.T) {
	src := "" +
		"def create\n" +
		"  name = params[:name]\n" +
		"  User.create(name)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "User.create" })
	if got == nil {
		t.Fatalf("expected bound-var db_write, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

// ---- one-hop / multi-hop (within-file) ----

func TestDataFlowRuby_OneHop_LocalMethod(t *testing.T) {
	src := "" +
		"def create\n" +
		"  x = params[:x]\n" +
		"  persist(x)\n" +
		"end\n" +
		"\n" +
		"def persist(v)\n" +
		"  Record.create(v)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.HopVia == "persist"
	})
	if got == nil {
		t.Fatalf("expected one-hop flow create->persist, got %+v", flows)
	}
	if got.SinkKind != DataFlowSinkDBWrite || got.SinkName != "Record.create" {
		t.Errorf("sink = %q/%s, want Record.create/db_write", got.SinkName, got.SinkKind)
	}
	if got.SourceField != "x" {
		t.Errorf("source field = %q, want x", got.SourceField)
	}
}

func TestDataFlowRuby_TwoHop_LocalChain(t *testing.T) {
	src := "" +
		"def handler\n" +
		"  x = params[:x]\n" +
		"  a(x)\n" +
		"end\n" +
		"\n" +
		"def a(v)\n" +
		"  b(v)\n" +
		"end\n" +
		"\n" +
		"def b(w)\n" +
		"  Record.create(w)\n" +
		"end\n"
	flows := sniffDataFlowRubyEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "handler" && f.SinkName == "Record.create"
	})
	if got == nil {
		t.Fatalf("expected 2-hop flow handler->a->b->Record.create, got %+v", flows)
	}
	if got.SourceField != "x" {
		t.Errorf("field = %q, want x", got.SourceField)
	}
	if len(got.HopPath) != 2 || got.HopPath[0] != "a" || got.HopPath[1] != "b" {
		t.Errorf("hop_path = %v, want [a b]", got.HopPath)
	}
}

func TestDataFlowRuby_Negative_FourthHopDropped(t *testing.T) {
	src := "" +
		"def handler\n  x = params[:x]\n  a(x)\nend\n" +
		"\ndef a(v)\n  b(v)\nend\n" +
		"\ndef b(w)\n  c(w)\nend\n" +
		"\ndef c(z)\n  d(z)\nend\n" +
		"\ndef d(q)\n  Model.create(q)\nend\n"
	flows := sniffDataFlowRubyEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "Model.create" }); got != nil {
		t.Fatalf("expected NO flow at 4th hop, got %+v", *got)
	}
}

func TestDataFlowRuby_Negative_RecursionStops(t *testing.T) {
	src := "" +
		"def handler\n" +
		"  x = params[:x]\n" +
		"  rec(x)\n" +
		"end\n" +
		"\n" +
		"def rec(v)\n" +
		"  rec(v)\n" +
		"  Record.create(v)\n" +
		"end\n"
	flows := sniffDataFlowRubyEx(src).Flows
	n := 0
	for _, f := range flows {
		if f.SinkName == "Record.create" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 Record.create flow, got %d: %+v", n, flows)
	}
}

// ---- negatives ----

func TestDataFlowRuby_Negative_StaticValue(t *testing.T) {
	src := "" +
		"def create\n" +
		"  name = 'static'\n" +
		"  User.create(name)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	if len(flows) != 0 {
		t.Fatalf("expected NO flow for static value, got %+v", flows)
	}
}

func TestDataFlowRuby_Negative_NonParamVar(t *testing.T) {
	src := "" +
		"def create\n" +
		"  name = compute_default\n" +
		"  User.create(name)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO db_write for non-param var, got %+v", *got)
	}
}

func TestDataFlowRuby_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"def create\n" +
		"  name = params[:name]\n" +
		"  name = 'override'\n" +
		"  User.create(name)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO db_write after reassign, got %+v", *got)
	}
}

func TestDataFlowRuby_Negative_DynamicKeyNoField(t *testing.T) {
	// Dynamic key params[k] is not a static source → no flow.
	src := "" +
		"def create\n" +
		"  name = params[k]\n" +
		"  User.create(name)\n" +
		"end\n"
	flows := sniffDataFlowRuby(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO flow for dynamic key, got %+v", *got)
	}
}

// ---- cross-file boundary emission + continuation ----

func TestDataFlowRuby_Boundary_ImportedHelper(t *testing.T) {
	src := "" +
		"def handler\n" +
		"  x = params[:name]\n" +
		"  save(x)\n" +
		"end\n"
	res := sniffDataFlowRubyEx(src)
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

func TestDataFlowRuby_Continue_BindsParamToSink(t *testing.T) {
	svc := "" +
		"def save(v)\n" +
		"  Model.create(v)\n" +
		"end\n"
	res := continueDataFlowRuby(svc, "save", 0, "name", 0)
	got := findFlow(res.Flows, func(f DataFlow) bool { return f.SinkName == "Model.create" })
	if got == nil {
		t.Fatalf("expected continuation to reach Model.create, got %+v", res.Flows)
	}
	if got.SourceField != "name" {
		t.Errorf("field = %q, want name", got.SourceField)
	}
}

func TestDataFlowRuby_Negative_KwargArgNotBoundAsBoundary(t *testing.T) {
	// save(name: x) is a keyword arg — positional binding is unsound → drop.
	src := "" +
		"def handler\n" +
		"  x = params[:name]\n" +
		"  save(name: x)\n" +
		"end\n"
	res := sniffDataFlowRubyEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected NO boundary for kwarg call, got %+v", res.Boundaries)
	}
}
