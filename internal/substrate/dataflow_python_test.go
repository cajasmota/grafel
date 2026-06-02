package substrate

import "testing"

func TestDataFlowPython_IntraFn_DBWrite_Field(t *testing.T) {
	src := "" +
		"def create_user(request):\n" +
		"    name = request.data['name']\n" +
		"    User.objects.create(name=name)\n"
	flows := sniffDataFlowPython(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create_user" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
	if got.SinkName != "User.objects.create" {
		t.Errorf("sink = %q, want User.objects.create", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

func TestDataFlowPython_PassThrough_Response(t *testing.T) {
	src := "" +
		"def search(request):\n" +
		"    return Response(request.GET.get('q'))\n"
	flows := sniffDataFlowPython(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkResponse })
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
	if got.SinkName != "Response" {
		t.Errorf("sink = %q, want Response", got.SinkName)
	}
}

func TestDataFlowPython_OneHop_LocalFunction(t *testing.T) {
	src := "" +
		"def handler(request):\n" +
		"    x = request.data['x']\n" +
		"    persist(x)\n" +
		"\n" +
		"def persist(v):\n" +
		"    repo.insert(v)\n"
	flows := sniffDataFlowPython(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "handler" && f.HopVia == "persist"
	})
	if got == nil {
		t.Fatalf("expected one-hop flow handler->persist, got %+v", flows)
	}
	if got.SinkKind != DataFlowSinkDBWrite || got.SinkName != "repo.insert" {
		t.Errorf("sink = %q/%s, want repo.insert/db_write", got.SinkName, got.SinkKind)
	}
	if got.SourceField != "x" {
		t.Errorf("source field = %q, want x", got.SourceField)
	}
}

func TestDataFlowPython_DRF_ValidatedData(t *testing.T) {
	src := "" +
		"def create(self, request):\n" +
		"    email = serializer.validated_data['email']\n" +
		"    Account.objects.create(email=email)\n"
	flows := sniffDataFlowPython(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite })
	if got == nil {
		t.Fatalf("expected db_write flow from validated_data, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
}

func TestDataFlowPython_Negative_StaticValue(t *testing.T) {
	src := "" +
		"def create_user(request):\n" +
		"    name = 'static'\n" +
		"    User.objects.create(name=name)\n"
	flows := sniffDataFlowPython(src)
	if len(flows) != 0 {
		t.Fatalf("expected NO flow for static value, got %+v", flows)
	}
}

func TestDataFlowPython_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"def create_user(request):\n" +
		"    name = request.data['name']\n" +
		"    name = 'override'\n" +
		"    User.objects.create(name=name)\n"
	flows := sniffDataFlowPython(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO db_write after reassign, got %+v", *got)
	}
}

// ---- multi-hop (within-file) ----

func TestDataFlowPython_TwoHop_LocalChain(t *testing.T) {
	src := "" +
		"def handler(request):\n" +
		"    x = request.data['x']\n" +
		"    a(x)\n" +
		"\n" +
		"def a(v):\n" +
		"    b(v)\n" +
		"\n" +
		"def b(w):\n" +
		"    repo.insert(w)\n"
	flows := sniffDataFlowPythonEx(src).Flows
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
}

func TestDataFlowPython_Negative_FourthHopDropped(t *testing.T) {
	src := "" +
		"def handler(request):\n" +
		"    x = request.data['x']\n" +
		"    a(x)\n" +
		"\n" +
		"def a(v):\n    b(v)\n" +
		"\n" +
		"def b(w):\n    c(w)\n" +
		"\n" +
		"def c(z):\n    d(z)\n" +
		"\n" +
		"def d(q):\n    Model.objects.create(name=q)\n"
	flows := sniffDataFlowPythonEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "Model.objects.create" }); got != nil {
		t.Fatalf("expected NO flow at 4th hop, got %+v", *got)
	}
}

func TestDataFlowPython_Negative_RecursionStops(t *testing.T) {
	src := "" +
		"def handler(request):\n" +
		"    x = request.data['x']\n" +
		"    rec(x)\n" +
		"\n" +
		"def rec(v):\n" +
		"    rec(v)\n" +
		"    repo.insert(v)\n"
	flows := sniffDataFlowPythonEx(src).Flows
	n := 0
	for _, f := range flows {
		if f.SinkName == "repo.insert" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 repo.insert flow, got %d: %+v", n, flows)
	}
}

// ---- cross-file boundary emission ----

func TestDataFlowPython_Boundary_ImportedHelper(t *testing.T) {
	src := "" +
		"from .svc import save\n" +
		"def handler(request):\n" +
		"    x = request.data['name']\n" +
		"    save(x)\n"
	res := sniffDataFlowPythonEx(src)
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

func TestDataFlowPython_Continue_BindsParamToSink(t *testing.T) {
	svc := "" +
		"def save(v):\n" +
		"    Model.objects.create(name=v)\n"
	res := continueDataFlowPython(svc, "save", 0, "name", 0)
	got := findFlow(res.Flows, func(f DataFlow) bool { return f.SinkName == "Model.objects.create" })
	if got == nil {
		t.Fatalf("expected continuation to reach Model.objects.create, got %+v", res.Flows)
	}
	if got.SourceField != "name" {
		t.Errorf("field = %q, want name", got.SourceField)
	}
}

func TestDataFlowPython_Negative_KwargArgNotBoundAsBoundary(t *testing.T) {
	// save(name=x) is a kwarg — positional binding is unsound → drop.
	src := "" +
		"from .svc import save\n" +
		"def handler(request):\n" +
		"    x = request.data['name']\n" +
		"    save(name=x)\n"
	res := sniffDataFlowPythonEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected NO boundary for kwarg call, got %+v", res.Boundaries)
	}
}
