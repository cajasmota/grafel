package substrate

import "testing"

// ---- intra-fn flows (value-asserting on real Crow/Drogon/libpqxx forms) ----

// Crow handler: req.url_params.get("id") → libpqxx txn.exec("..."+id): the
// request-source field "id" reaches the db-write sink txn.exec. Asserts BOTH
// ends — the specific request key and the specific db-write callee.
func TestDataFlowCCPP_Crow_UrlParam_LibpqxxExec_DBWrite(t *testing.T) {
	src := "" +
		"crow::response handle(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    txn.exec(\"DELETE FROM users WHERE id=\" + id);\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "handle" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "txn.exec"
	})
	if got == nil {
		t.Fatalf("expected libpqxx db_write flow id→txn.exec, got %+v", flows)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id (from req.url_params.get(\"id\"))", got.SourceField)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn, got hop=%q", got.HopVia)
	}
}

// Drogon handler: req->getParameter("name") → response callback carrying it.
func TestDataFlowCCPP_Drogon_Param_Response(t *testing.T) {
	src := "" +
		"void echo(const HttpRequestPtr& req, Callback&& callback) {\n" +
		"    auto name = req->getParameter(\"name\");\n" +
		"    callback(name);\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "echo" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "callback"
	})
	if got == nil {
		t.Fatalf("expected response flow name→callback, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name", got.SourceField)
	}
}

// Whole-body read req.body() → command exec system(...): field "" (no static
// key), command-exec sink surfaced as db_write-class. The classic C++ RCE.
func TestDataFlowCCPP_Body_SystemExec_WholeBody(t *testing.T) {
	src := "" +
		"void run(const crow::request& req) {\n" +
		"    std::string cmd = req.body();\n" +
		"    system(cmd);\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "run" && f.SinkName == "system"
	})
	if got == nil {
		t.Fatalf("expected command-exec flow body→system, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want \"\" (whole-body read)", got.SourceField)
	}
}

// res.body = <tainted> member-assignment response sink.
func TestDataFlowCCPP_ResponseBodyAssign(t *testing.T) {
	src := "" +
		"void show(const crow::request& req, crow::response& res) {\n" +
		"    auto q = req.url_params.get(\"q\");\n" +
		"    res.body = q;\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "show" && f.SinkKind == DataFlowSinkResponse && f.SinkName == "res.body"
	})
	if got == nil {
		t.Fatalf("expected res.body= response flow, got %+v", flows)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
}

// cpr outbound POST carrying a tainted body → http_call sink.
func TestDataFlowCCPP_OutboundCprPost_HTTPCall(t *testing.T) {
	src := "" +
		"void forward(const crow::request& req) {\n" +
		"    auto payload = req.body();\n" +
		"    cpr::Post(cpr::Url{\"http://x\"}, cpr::Body{payload});\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "forward" && f.SinkKind == DataFlowSinkHTTPCall && f.SinkName == "cpr::Post"
	})
	if got == nil {
		t.Fatalf("expected outbound cpr::Post http_call flow, got %+v", flows)
	}
}

// ---- multi-hop (≤ DataFlowMaxHops) ----

// handler reads req.url_params.get("id") → passes to local helper persist(id) →
// helper does txn.exec(...): one local hop, HopPath=[persist], field carried.
func TestDataFlowCCPP_MultiHop_LocalHelper_DBWrite(t *testing.T) {
	src := "" +
		"void persist(std::string v) {\n" +
		"    txn.exec(\"INSERT INTO t VALUES(\" + v + \")\");\n" +
		"}\n" +
		"void create(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    persist(id);\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "txn.exec"
	})
	if got == nil {
		t.Fatalf("expected multi-hop db_write flow create→persist→txn.exec, got %+v", flows)
	}
	if len(got.HopPath) != 1 || got.HopPath[0] != "persist" {
		t.Errorf("hop path = %v, want [persist]", got.HopPath)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
}

// ---- cross-file boundary + continuation ----

// A tainted value passed into a callee NOT in this file becomes a boundary the
// links pass resolves; the continuation binds it into the callee's param.
func TestDataFlowCCPP_CrossFile_Boundary(t *testing.T) {
	src := "" +
		"void create(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    saveToDb(id);\n" +
		"}\n"
	res := sniffDataFlowCCPPEx(src)
	if len(res.Boundaries) == 0 {
		t.Fatalf("expected a cross-file boundary for saveToDb(id), got %+v", res)
	}
	b := res.Boundaries[0]
	if b.Function != "create" || b.Callee != "saveToDb" || b.ArgIndex != 0 || b.SourceField != "id" {
		t.Errorf("boundary = %+v, want create→saveToDb arg0 field id", b)
	}
}

// The continuation enters the resolved callee, binds the tainted arg into its
// first parameter, and finds the db-write sink there.
func TestDataFlowCCPP_CrossFile_Continuation(t *testing.T) {
	callee := "" +
		"void saveToDb(std::string v) {\n" +
		"    txn.exec(\"INSERT INTO t VALUES(\" + v + \")\");\n" +
		"}\n"
	res := continueDataFlowCCPP(callee, "saveToDb", 0, "id", 1)
	got := findFlow(res.Flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite && f.SinkName == "txn.exec"
	})
	if got == nil {
		t.Fatalf("expected continued db_write flow in saveToDb, got %+v", res.Flows)
	}
	if got.SourceField != "id" {
		t.Errorf("carried field = %q, want id", got.SourceField)
	}
}

// ---- negatives (honest-partial / no fabrication) ----

// A request read that is only logged, never reaching a sink → no flow.
func TestDataFlowCCPP_Negative_LoggedNotSunk(t *testing.T) {
	src := "" +
		"void handle(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    std::cout << id << std::endl;\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a logged-only read, got %+v", flows)
	}
}

// A db-write sink fed by a constant string (no request provenance) → no flow.
func TestDataFlowCCPP_Negative_ConstantSink(t *testing.T) {
	src := "" +
		"void seed(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    txn.exec(\"INSERT INTO t VALUES(1)\");\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows for a constant-fed sink, got %+v", flows)
	}
}

// A reassignment that breaks the chain drops the taint (no flow).
func TestDataFlowCCPP_Negative_ReassignBreaksChain(t *testing.T) {
	src := "" +
		"void handle(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    id = \"safe\";\n" +
		"    txn.exec(\"DELETE FROM t WHERE id=\" + id);\n" +
		"}\n"
	flows := sniffDataFlowCCPP(src)
	if len(flows) != 0 {
		t.Fatalf("expected no flows after reassignment, got %+v", flows)
	}
}

// A whole-request value embedded in an arithmetic/non-bare expression passed to
// a local call is NOT positionally bound — no boundary fabricated.
func TestDataFlowCCPP_Negative_EmbeddedExpression(t *testing.T) {
	src := "" +
		"void handle(const crow::request& req) {\n" +
		"    auto id = req.url_params.get(\"id\");\n" +
		"    helper(\"prefix\" + id + \"suffix\");\n" +
		"}\n"
	res := sniffDataFlowCCPPEx(src)
	if len(res.Boundaries) != 0 {
		t.Fatalf("expected no boundary for an embedded expression, got %+v", res.Boundaries)
	}
}
