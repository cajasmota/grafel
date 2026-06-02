package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real Laravel/Symfony forms) --------

func TestDataFlowPHP_Laravel_EloquentCreate_Field(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    User::create(['email' => $request->input('email')]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "store" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
	if got.SinkName != "User::create" {
		t.Errorf("sink = %q, want User::create", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

func TestDataFlowPHP_Laravel_BoundVar_EloquentCreate(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $email = $request->input('email');\n" +
		"    User::create(['email' => $email]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.SinkName == "User::create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow via bound var, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
}

func TestDataFlowPHP_Laravel_QueryToJsonResponse_Field(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function search(Request $request) {\n" +
		"    $q = $request->query('q');\n" +
		"    return response()->json($q);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkResponse })
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
	if got.SinkName != "response()->json" {
		t.Errorf("sink = %q, want response()->json", got.SinkName)
	}
}

func TestDataFlowPHP_Symfony_RequestGet_DoctrinePersist(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function create(Request $request) {\n" +
		"    $name = $request->request->get('name');\n" +
		"    $user->setName($name);\n" +
		"    $em->persist($name);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "$em->persist"
	})
	if got == nil {
		t.Fatalf("expected doctrine persist flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

func TestDataFlowPHP_Symfony_BareGet_Response(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function show(Request $request) {\n" +
		"    $id = $request->get('id');\n" +
		"    return view('user', $id);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "view" })
	if got == nil {
		t.Fatalf("expected view response flow, got %+v", flows)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
	if got.SinkKind != DataFlowSinkResponse {
		t.Errorf("sink kind = %q, want response", got.SinkKind)
	}
}

func TestDataFlowPHP_Superglobal_POST_RawSQL(t *testing.T) {
	src := "" +
		"<?php\n" +
		"function save() {\n" +
		"    $name = $_POST['name'];\n" +
		"    DB::insert($name);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "DB::insert"
	})
	if got == nil {
		t.Fatalf("expected raw-SQL db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

func TestDataFlowPHP_PropertyAccess_Source(t *testing.T) {
	// $request->email dynamic property access → field "email".
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    User::create(['email' => $request->email]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "User::create" })
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
}

func TestDataFlowPHP_GuzzleOutbound_Field(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function forward(Request $request) {\n" +
		"    $payload = $request->input('body');\n" +
		"    $client->post($payload);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkHTTPCall })
	if got == nil {
		t.Fatalf("expected http_call flow, got %+v", flows)
	}
	if got.SourceField != "body" {
		t.Errorf("source field = %q, want body", got.SourceField)
	}
}

func TestDataFlowPHP_AllWholeArray_EmptyField(t *testing.T) {
	// $request->all() whole-array mass-assignment: field NOT derivable → "".
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $data = $request->all();\n" +
		"    User::create($data);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "User::create" })
	if got == nil {
		t.Fatalf("expected db_write flow for whole-array, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole-array mass-assign)", got.SourceField)
	}
}

func TestDataFlowPHP_AllThenIndexedKey_RecoversField(t *testing.T) {
	// $request->all() taints with field="", but a later static index recovers it.
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $data = $request->all();\n" +
		"    User::create($data['email']);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "User::create" })
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (recovered from index)", got.SourceField)
	}
}

// ---- multi-hop (one local-method hop) --------------------------------------

func TestDataFlowPHP_OneHop_ThisMethod(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $email = $request->input('email');\n" +
		"    $this->persistUser($email);\n" +
		"}\n" +
		"private function persistUser($value) {\n" +
		"    User::create(['email' => $value]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "store" && f.SinkName == "User::create"
	})
	if got == nil {
		t.Fatalf("expected one-hop db_write flow attributed to store, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
	if got.HopVia != "persistUser" {
		t.Errorf("hop via = %q, want persistUser", got.HopVia)
	}
}

func TestDataFlowPHP_FreeFunctionHop_Boundary(t *testing.T) {
	// A tainted value passed into a NON-local (imported) function → a cross-file
	// boundary is emitted (not an in-file flow) for the links pass to resolve.
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $email = $request->input('email');\n" +
		"    persistUserExternal($email);\n" +
		"}\n"
	res := sniffDataFlowPHPEx(src)
	if len(res.Boundaries) != 1 {
		t.Fatalf("expected 1 boundary, got %+v", res.Boundaries)
	}
	b := res.Boundaries[0]
	if b.Callee != "persistUserExternal" {
		t.Errorf("boundary callee = %q, want persistUserExternal", b.Callee)
	}
	if b.SourceField != "email" {
		t.Errorf("boundary field = %q, want email", b.SourceField)
	}
	if b.ArgIndex != 0 {
		t.Errorf("boundary arg index = %d, want 0", b.ArgIndex)
	}
}

func TestDataFlowPHP_CrossFile_Continue(t *testing.T) {
	// The links pass would call continueDataFlowPHP on the resolved callee file.
	callee := "" +
		"<?php\n" +
		"function persistUserExternal($value) {\n" +
		"    User::create(['email' => $value]);\n" +
		"}\n"
	res := continueDataFlowPHP(callee, "persistUserExternal", 0, "email", 1)
	got := findFlow(res.Flows, func(f DataFlow) bool { return f.SinkName == "User::create" })
	if got == nil {
		t.Fatalf("expected continued db_write flow, got %+v", res.Flows)
	}
	if got.SourceField != "email" {
		t.Errorf("continued field = %q, want email", got.SourceField)
	}
}

// ---- negatives (honest-partial: drop, never fabricate) ---------------------

func TestDataFlowPHP_StaticValue_NoFlow(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    User::create(['email' => 'static@example.com']);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected no flow for static value, got %+v", got)
	}
}

func TestDataFlowPHP_NonRequestVar_NoFlow(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $email = $someService->fetch();\n" +
		"    User::create(['email' => $email]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected no flow for non-request var, got %+v", got)
	}
}

func TestDataFlowPHP_DynamicKey_NoKeyedFlow(t *testing.T) {
	// Dynamic key $request->input($key): field not statically derivable. The
	// keyed source must not match; no flow is emitted (honest-partial).
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $val = $request->input($key);\n" +
		"    User::create(['x' => $val]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected no flow for dynamic key, got %+v", got)
	}
}

func TestDataFlowPHP_Reassignment_BreaksTaint(t *testing.T) {
	src := "" +
		"<?php\n" +
		"public function store(Request $request) {\n" +
		"    $email = $request->input('email');\n" +
		"    $email = 'overwritten';\n" +
		"    User::create(['email' => $email]);\n" +
		"}\n"
	flows := sniffDataFlowPHP(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected no flow after reassignment, got %+v", got)
	}
}
