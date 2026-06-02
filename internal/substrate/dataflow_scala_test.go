package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real Play / Akka / http4s forms) ----

// Play handler: `val name = request.getQueryString("name")` → Slick
// `users += User(name)`: the field is lifted from the keyed accessor, the sink
// is the Slick insert. Asserts BOTH ends: source field "name", sink db_write.
func TestDataFlowScala_Play_QueryString_SlickInsert(t *testing.T) {
	src := "" +
		"def create() = Action { request =>\n" +
		"  val name = request.getQueryString(\"name\")\n" +
		"  users += User(name)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name (from getQueryString)", got.SourceField)
	}
	if got.SinkName != "users +=" {
		t.Errorf("sink = %q, want 'users +='", got.SinkName)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

// Play handler: `val body = request.body` flowed straight into `Ok(body)` is a
// response sink (whole-request, field="").
func TestDataFlowScala_Play_Body_Response(t *testing.T) {
	src := "" +
		"def echo() = Action { request =>\n" +
		"  val body = request.body\n" +
		"  Ok(body)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "echo" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "Ok"
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole request.body)", got.SourceField)
	}
}

// Akka/Pekko HTTP: `entity(as[User]) { dto => ... em.persist(dto) }` — the
// directive-bound lambda param is the request body, flowing into a JPA write.
func TestDataFlowScala_Akka_Entity_DBWrite(t *testing.T) {
	src := "" +
		"def route = post {\n" +
		"  entity(as[User]) { dto =>\n" +
		"    em.persist(dto)\n" +
		"  }\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "route" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "em.persist"
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole entity body)", got.SourceField)
	}
}

// Akka directive `parameter("q") { q => complete(q) }` carries field "q" into a
// response sink.
func TestDataFlowScala_Akka_Parameter_Field_Response(t *testing.T) {
	src := "" +
		"def route = get {\n" +
		"  parameter(\"q\") { q =>\n" +
		"    complete(q)\n" +
		"  }\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "route" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "complete"
	})
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
}

// Member-access field lift: `entity(as[User]) { dto => repo.update(dto.email) }`
// lifts "email" as the source field of the Slick update.
func TestDataFlowScala_Entity_MemberField_DBWrite(t *testing.T) {
	src := "" +
		"def route = post {\n" +
		"  entity(as[User]) { dto =>\n" +
		"    repo.update(dto.email)\n" +
		"  }\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "route" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "repo.update"
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (from dto.email)", got.SourceField)
	}
}

// Outbound HTTP with a tainted body is an http_call sink.
func TestDataFlowScala_Sttp_Outbound(t *testing.T) {
	src := "" +
		"def forward() = Action { request =>\n" +
		"  val body = request.body\n" +
		"  basicRequest.post(uri).body(body)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "forward" && f.SinkKind == DataFlowSinkHTTPCall
	})
	if got == nil {
		t.Fatalf("expected http_call flow, got %+v", flows)
	}
}

// Local propagation: `val e = dto.email` then `users += User(e)` — taint flows
// through the intermediate val (single-hop, in-fn).
func TestDataFlowScala_LocalPropagation_Val(t *testing.T) {
	src := "" +
		"def route = post {\n" +
		"  entity(as[User]) { dto =>\n" +
		"    val e = dto.email\n" +
		"    users += User(e)\n" +
		"  }\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "route" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email", got.SourceField)
	}
}

// ---- multi-hop (≤ DataFlowMaxHops) ----

// source → local helper → sink: `persist(name)` where persist is a same-file
// def writing to the DB. Asserts the hop chain and field.
func TestDataFlowScala_OneHop_LocalMethod(t *testing.T) {
	src := "" +
		"def create() = Action { request =>\n" +
		"  val name = request.getQueryString(\"name\")\n" +
		"  persist(name)\n" +
		"}\n" +
		"def persist(e: String) = {\n" +
		"  users += User(e)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected one-hop db_write flow, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
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
func TestDataFlowScala_CrossFile_Boundary(t *testing.T) {
	src := "" +
		"def create() = Action { request =>\n" +
		"  val name = request.getQueryString(\"name\")\n" +
		"  saveExternal(name)\n" +
		"}\n"
	res := sniffDataFlowScalaEx(src)
	if len(res.Boundaries) == 0 {
		t.Fatalf("expected a cross-file boundary, got %+v", res)
	}
	b := res.Boundaries[0]
	if b.Function != "create" || b.Callee != "saveExternal" || b.ArgIndex != 0 {
		t.Errorf("boundary = %+v, want create→saveExternal arg0", b)
	}
	if b.SourceField != "name" {
		t.Errorf("boundary field = %q, want name", b.SourceField)
	}
}

// Cross-file continuation binds the tainted value into the callee's param and
// finds the sink there.
func TestDataFlowScala_Continue_BindsParam(t *testing.T) {
	callee := "" +
		"def persist(e: String) = {\n" +
		"  users += User(e)\n" +
		"}\n"
	res := continueDataFlowScala(callee, "persist", 0, "name", 1)
	got := findFlow(res.Flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected continued db_write flow, got %+v", res)
	}
	if got.SourceField != "name" {
		t.Errorf("continued field = %q, want name", got.SourceField)
	}
}

// ---- negatives (honest-partial / no fabrication) ----

// A request read that is LOGGED but never reaches a sink → no dataflow.
func TestDataFlowScala_Negative_LoggedNotSunk(t *testing.T) {
	src := "" +
		"def peek() = Action { request =>\n" +
		"  val name = request.getQueryString(\"name\")\n" +
		"  logger.info(name)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a logged-only read, got %+v", flows)
	}
}

// A db-write fed by a constant (no request provenance) → no dataflow.
func TestDataFlowScala_Negative_ConstantSink(t *testing.T) {
	src := "" +
		"def seed() = Action { request =>\n" +
		"  users += User(\"system\")\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a constant-fed sink, got %+v", flows)
	}
}

// A response with a static literal is NOT a flow.
func TestDataFlowScala_Negative_StaticResponse(t *testing.T) {
	src := "" +
		"def ping() = Action {\n" +
		"  Ok(\"pong\")\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a static response, got %+v", flows)
	}
}

// A reassignment that breaks the chain drops the taint (no flow).
func TestDataFlowScala_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"def create() = Action { request =>\n" +
		"  var name = request.getQueryString(\"name\")\n" +
		"  name = \"constant\"\n" +
		"  users += User(name)\n" +
		"}\n"
	flows := sniffDataFlowScala(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows after reassignment, got %+v", flows)
	}
}

// An embedded-expression argument cannot be bound positionally (drop, no
// boundary) — honest-partial.
func TestDataFlowScala_Negative_EmbeddedExpression(t *testing.T) {
	src := "" +
		"def create() = Action { request =>\n" +
		"  val name = request.getQueryString(\"name\")\n" +
		"  saveExternal(name + \"x\")\n" +
		"}\n"
	res := sniffDataFlowScalaEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected no boundary for an embedded expression, got %+v", res.Boundaries)
	}
}
