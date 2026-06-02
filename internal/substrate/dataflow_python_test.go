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
