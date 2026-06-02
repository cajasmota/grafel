package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real ASP.NET Core / MVC forms) ----

// [FromBody] dto → _context.Users.Add(new User{Email=dto.Email}); SaveChanges:
// the field is lifted from the property (dto.Email → Email), sink is the EF write.
func TestDataFlowCSharp_FromBody_PropertyField_DBWrite(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromBody] UserDto dto) {\n" +
		"    _context.Users.Add(new User { Email = dto.Email });\n" +
		"    _context.SaveChanges();\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Create" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "_context.Users.Add"
	})
	if got == nil {
		t.Fatalf("expected EF db_write flow, got %+v", flows)
	}
	if got.SourceField != "Email" {
		t.Errorf("source field = %q, want Email (from dto.Email)", got.SourceField)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

// [FromQuery] string q → return Ok(q): response sink, field q (from param name).
func TestDataFlowCSharp_FromQuery_Response_Field(t *testing.T) {
	src := "" +
		"public IActionResult Find([FromQuery] string q) {\n" +
		"    return Ok(q);\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Find" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "Ok"
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
}

// [FromRoute] int id → EF write carries field id.
func TestDataFlowCSharp_FromRoute_Field_DBWrite(t *testing.T) {
	src := "" +
		"public IActionResult Touch([FromRoute] int id) {\n" +
		"    _context.Audits.Add(new Audit(id));\n" +
		"    _context.SaveChanges();\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Touch" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "_context.Audits.Add"
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
}

// [FromQuery(Name="term")] string raw → the attribute literal wins as the field.
func TestDataFlowCSharp_FromQuery_NameLiteral_Field(t *testing.T) {
	src := "" +
		"public IActionResult Echo([FromQuery(Name=\"term\")] string raw) {\n" +
		"    return Ok(raw);\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Echo" && f.SinkKind == DataFlowSinkResponse
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "term" {
		t.Errorf("source field = %q, want term (attribute literal)", got.SourceField)
	}
}

// [FromForm] string name → field from param name; Dapper Execute db_write.
func TestDataFlowCSharp_FromForm_Dapper_Execute(t *testing.T) {
	src := "" +
		"public IActionResult Save([FromForm] string name) {\n" +
		"    connection.Execute(\"insert into t values (@n)\", name);\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Save" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "connection.Execute"
	})
	if got == nil {
		t.Fatalf("expected Dapper db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

// Local propagation through a var declaration: var e = dto.Email;
func TestDataFlowCSharp_LocalPropagation_VarDecl(t *testing.T) {
	src := "" +
		"public IActionResult Save([FromBody] UserDto dto) {\n" +
		"    var e = dto.Email;\n" +
		"    _context.Users.Add(new User { Email = e });\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Save" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "Email" {
		t.Errorf("source field = %q, want Email", got.SourceField)
	}
}

// In-body Request.Query["x"] accessor seeds a local root with field "x".
func TestDataFlowCSharp_RequestQueryAccessor_Source(t *testing.T) {
	src := "" +
		"public IActionResult Search() {\n" +
		"    var term = Request.Query[\"x\"];\n" +
		"    return Ok(term);\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Search" && f.SinkKind == DataFlowSinkResponse
	})
	if got == nil {
		t.Fatalf("expected response flow from Request.Query, got %+v", flows)
	}
	if got.SourceField != "x" {
		t.Errorf("source field = %q, want x (Request.Query key)", got.SourceField)
	}
}

// Outbound HTTP with a tainted body is an http_call sink.
func TestDataFlowCSharp_HttpClient_Outbound(t *testing.T) {
	src := "" +
		"public IActionResult Forward([FromBody] UserDto dto) {\n" +
		"    httpClient.PostAsync(\"http://x/y\", dto);\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Forward" && f.SinkKind == DataFlowSinkHTTPCall && f.SinkName == "httpClient.PostAsync"
	})
	if got == nil {
		t.Fatalf("expected http_call flow, got %+v", flows)
	}
}

// A whole-object [FromBody] flowing straight to .Add has no derivable field →
// field="" (honest-partial), but the flow IS emitted.
func TestDataFlowCSharp_FromBody_WholeObject_EmptyField(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromBody] User user) {\n" +
		"    _context.Users.Add(user);\n" +
		"    _context.SaveChanges();\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Create" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "_context.Users.Add"
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole-object honest-partial)", got.SourceField)
	}
}

// `return <tainted>;` bare value is a response sink.
func TestDataFlowCSharp_ReturnBareTainted_Response(t *testing.T) {
	src := "" +
		"public string Raw([FromQuery] string q) {\n" +
		"    return q;\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Raw" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "return"
	})
	if got == nil {
		t.Fatalf("expected bare-return response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
}

// ---- multi-hop (one local-method hop) ----

func TestDataFlowCSharp_OneHop_LocalMethod(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromBody] UserDto dto) {\n" +
		"    Persist(dto.Email);\n" +
		"}\n" +
		"private void Persist(string e) {\n" +
		"    _context.Users.Add(new User { Email = e });\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected one-hop db_write flow, got %+v", flows)
	}
	if got.SourceField != "Email" {
		t.Errorf("source field = %q, want Email", got.SourceField)
	}
	if got.HopVia != "Persist" {
		t.Errorf("hop = %q, want Persist", got.HopVia)
	}
	if len(got.HopPath) != 1 || got.HopPath[0] != "Persist" {
		t.Errorf("hop path = %v, want [Persist]", got.HopPath)
	}
}

// Cross-file boundary: a tainted value passed into a non-local callee is
// recorded as a boundary for the links pass (not an in-file flow).
func TestDataFlowCSharp_CrossFile_Boundary(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromBody] UserDto dto) {\n" +
		"    Helper(dto.Email);\n" +
		"}\n"
	res := sniffDataFlowCSharpEx(src)
	if len(res.Boundaries) == 0 {
		t.Fatalf("expected a cross-file boundary, got %+v", res)
	}
	b := res.Boundaries[0]
	if b.Function != "Create" || b.Callee != "Helper" || b.ArgIndex != 0 {
		t.Errorf("boundary = %+v, want Create→Helper arg0", b)
	}
	if b.SourceField != "Email" {
		t.Errorf("boundary field = %q, want Email", b.SourceField)
	}
}

// Cross-file continuation binds the tainted value into the callee's param and
// finds the sink there.
func TestDataFlowCSharp_Continue_BindsParam(t *testing.T) {
	callee := "" +
		"public void Persist(string e) {\n" +
		"    _context.Users.Add(new User { Email = e });\n" +
		"}\n"
	res := continueDataFlowCSharp(callee, "Persist", 0, "Email", 1)
	got := findFlow(res.Flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected continued db_write flow, got %+v", res)
	}
	if got.SourceField != "Email" {
		t.Errorf("continued field = %q, want Email", got.SourceField)
	}
}

// ---- negatives (honest-partial / no fabrication) ----

// A static / constant value reaching a sink is NOT a flow.
func TestDataFlowCSharp_Negative_StaticValue(t *testing.T) {
	src := "" +
		"public IActionResult Ping() {\n" +
		"    return Ok(\"pong\");\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a static value, got %+v", flows)
	}
}

// An injected service param (not a request binder) is NOT a source.
func TestDataFlowCSharp_Negative_NonRequestParam(t *testing.T) {
	src := "" +
		"public IActionResult Create(IUserService svc) {\n" +
		"    _context.Users.Add(svc.Build());\n" +
		"    _context.SaveChanges();\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a non-request param, got %+v", flows)
	}
}

// A reassignment that breaks the chain drops the taint (no flow).
func TestDataFlowCSharp_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromQuery] string q) {\n" +
		"    q = \"constant\";\n" +
		"    _context.Audits.Add(new Audit(q));\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows after reassignment, got %+v", flows)
	}
}

// An embedded expression (`Helper(q + "x")`) does NOT bind positionally
// (honest-partial drop — no boundary).
func TestDataFlowCSharp_Negative_EmbeddedExpression(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromQuery] string n) {\n" +
		"    Helper(n + \"x\");\n" +
		"}\n"
	res := sniffDataFlowCSharpEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected no boundary for an embedded expression, got %+v", res.Boundaries)
	}
}

// A dynamic field access (`dto[key]`) yields no static field — whole-object
// flow with field="" rather than a fabricated field.
func TestDataFlowCSharp_DynamicAccess_WholeObjectField(t *testing.T) {
	src := "" +
		"public IActionResult Create([FromBody] UserDto dto) {\n" +
		"    _context.Users.Add(dto);\n" +
		"    _context.SaveChanges();\n" +
		"}\n"
	flows := sniffDataFlowCSharp(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "Create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (no static field)", got.SourceField)
	}
}
