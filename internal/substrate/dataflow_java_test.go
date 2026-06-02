package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real Spring MVC / WebFlux forms) ----

// @RequestBody dto → repo.save(new User(dto.getEmail())): the field is lifted
// from the getter (getEmail → email), the sink is the JPA write.
func TestDataFlowJava_RequestBody_GetterField_DBWrite(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestBody UserDto dto) {\n" +
		"    repo.save(new User(dto.getEmail()));\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (from getEmail)", got.SourceField)
	}
	if got.SinkName != "repo.save" {
		t.Errorf("sink = %q, want repo.save", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

// @RequestParam("q") → ResponseEntity.ok(q): response sink, field q.
func TestDataFlowJava_RequestParam_Response_Field(t *testing.T) {
	src := "" +
		"@GetMapping\n" +
		"public ResponseEntity<String> find(@RequestParam(\"q\") String q) {\n" +
		"    return ResponseEntity.ok(q);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "find" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "ResponseEntity.ok"
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
}

// @PathVariable("id") → repo.save(...) carries field id.
func TestDataFlowJava_PathVariable_Field_DBWrite(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void touch(@PathVariable(\"id\") Long id) {\n" +
		"    repo.save(new Audit(id));\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "touch" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
	if got.SinkName != "repo.save" {
		t.Errorf("sink = %q, want repo.save", got.SinkName)
	}
}

// @RequestParam with no literal infers the field from the parameter name.
func TestDataFlowJava_RequestParam_NoLiteral_NameField(t *testing.T) {
	src := "" +
		"@GetMapping\n" +
		"public ResponseEntity<String> echo(@RequestParam String term) {\n" +
		"    return ResponseEntity.ok(term);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "echo" && f.SinkKind == DataFlowSinkResponse
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "term" {
		t.Errorf("source field = %q, want term (inferred from param name)", got.SourceField)
	}
}

// Direct field access (record / public field) lifts the member as the field.
func TestDataFlowJava_RequestBody_MemberField(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void save(@RequestBody UserDto dto) {\n" +
		"    String e = dto.email;\n" +
		"    repo.save(new User(e));\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "save" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (from dto.email)", got.SourceField)
	}
}

// Local propagation through a typed declaration: String e = dto.getEmail().
func TestDataFlowJava_LocalPropagation_DeclAssign(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void save(@RequestBody UserDto dto) {\n" +
		"    String e = dto.getEmail();\n" +
		"    entityManager.persist(e);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "save" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "entityManager.persist"
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
}

// jdbcTemplate.update(sql, q) with a tainted value flows as a db_write.
func TestDataFlowJava_JdbcTemplate_Update(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void ins(@RequestParam(\"name\") String name) {\n" +
		"    jdbcTemplate.update(\"insert into t values (?)\", name);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "ins" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "jdbcTemplate.update"
	})
	if got == nil {
		t.Fatalf("expected jdbcTemplate db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

// Outbound HTTP with a tainted body is an http_call sink.
func TestDataFlowJava_RestTemplate_Outbound(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void forward(@RequestBody UserDto dto) {\n" +
		"    restTemplate.postForObject(\"http://x/y\", dto, Void.class);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "forward" && f.SinkKind == DataFlowSinkHTTPCall
	})
	if got == nil {
		t.Fatalf("expected http_call flow, got %+v", flows)
	}
	if got.SinkName != "restTemplate.postForObject" {
		t.Errorf("sink = %q, want restTemplate.postForObject", got.SinkName)
	}
}

// A whole-object @RequestBody flowing straight to save() has no derivable
// field → field="" (honest-partial), but the flow IS emitted.
func TestDataFlowJava_RequestBody_WholeObject_EmptyField(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestBody User user) {\n" +
		"    repo.save(user);\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole-object honest-partial)", got.SourceField)
	}
}

// ---- multi-hop (one local-method hop) ----

func TestDataFlowJava_OneHop_LocalMethod(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestBody UserDto dto) {\n" +
		"    persist(dto.getEmail());\n" +
		"}\n" +
		"private void persist(String e) {\n" +
		"    repo.save(new User(e));\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected one-hop db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
	if got.HopVia != "persist" {
		t.Errorf("hop = %q, want persist", got.HopVia)
	}
	if len(got.HopPath) != 1 || got.HopPath[0] != "persist" {
		t.Errorf("hop path = %v, want [persist]", got.HopPath)
	}
}

// Cross-file boundary: a tainted value passed into a non-local callee is
// recorded as a boundary for the links pass (not an in-file flow).
func TestDataFlowJava_CrossFile_Boundary(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestBody UserDto dto) {\n" +
		"    helper(dto.getEmail());\n" +
		"}\n"
	res := sniffDataFlowJavaEx(src)
	if len(res.Boundaries) == 0 {
		t.Fatalf("expected a cross-file boundary, got %+v", res)
	}
	b := res.Boundaries[0]
	if b.Function != "create" || b.Callee != "helper" || b.ArgIndex != 0 {
		t.Errorf("boundary = %+v, want create→helper arg0", b)
	}
	if b.SourceField != "email" {
		t.Errorf("boundary field = %q, want email", b.SourceField)
	}
}

// Cross-file continuation binds the tainted value into the callee's param and
// finds the sink there.
func TestDataFlowJava_Continue_BindsParam(t *testing.T) {
	callee := "" +
		"public void persist(String e) {\n" +
		"    repo.save(new User(e));\n" +
		"}\n"
	res := continueDataFlowJava(callee, "persist", 0, "email", 1)
	got := findFlow(res.Flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected continued db_write flow, got %+v", res)
	}
	if got.SourceField != "email" {
		t.Errorf("continued field = %q, want email", got.SourceField)
	}
}

// ---- negatives (honest-partial / no fabrication) ----

// A static / constant value reaching a sink is NOT a flow.
func TestDataFlowJava_Negative_StaticValue(t *testing.T) {
	src := "" +
		"@GetMapping\n" +
		"public ResponseEntity<String> ping() {\n" +
		"    return ResponseEntity.ok(\"pong\");\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a static value, got %+v", flows)
	}
}

// An @Autowired service param (not a request binder) is NOT a source.
func TestDataFlowJava_Negative_NonRequestParam(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@Autowired UserService svc) {\n" +
		"    repo.save(svc.build());\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a non-request param, got %+v", flows)
	}
}

// A reassignment that breaks the chain drops the taint (no flow).
func TestDataFlowJava_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestParam(\"q\") String q) {\n" +
		"    q = \"constant\";\n" +
		"    repo.save(new Audit(q));\n" +
		"}\n"
	flows := sniffDataFlowJava(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows after reassignment, got %+v", flows)
	}
}

// A dynamic / whole-object flow with no static field still emits a flow but
// with field="" — already covered by WholeObject; here assert a getter chain
// embedded in arithmetic does NOT bind positionally (honest-partial drop).
func TestDataFlowJava_Negative_EmbeddedExpression(t *testing.T) {
	src := "" +
		"@PostMapping\n" +
		"public void create(@RequestParam(\"n\") String n) {\n" +
		"    helper(n + \"x\");\n" +
		"}\n"
	res := sniffDataFlowJavaEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected no boundary for an embedded expression, got %+v", res.Boundaries)
	}
}
