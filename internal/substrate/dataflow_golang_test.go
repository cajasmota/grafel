package substrate

import "testing"

// findGoFlow returns the first flow whose sink name contains sinkSub, or nil.
func findGoFlow(flows []DataFlow, sinkSub string) *DataFlow {
	for i := range flows {
		if containsStr(flows[i].SinkName, sinkSub) {
			return &flows[i]
		}
	}
	return nil
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- POSITIVE: gin Query → gorm Create, field resolved -----------------

func TestGoDataFlow_GinQueryToDBCreate(t *testing.T) {
	src := `package h
func CreateItem(c *gin.Context) {
	q := c.Query("q")
	db.Create(&Item{Name: q})
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Create")
	if f == nil {
		t.Fatalf("expected a db.Create flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.Function != "CreateItem" {
		t.Errorf("origin = %q, want CreateItem", f.Function)
	}
	if f.SourceField != "q" {
		t.Errorf("source field = %q, want q", f.SourceField)
	}
}

// --- POSITIVE: gin ShouldBindJSON(&dto) → db.Save(&User{Email:dto.Email}) ---

func TestGoDataFlow_ShouldBindJSONToDBSave(t *testing.T) {
	src := `package h
func UpdateUser(c *gin.Context) {
	var dto UserDTO
	c.ShouldBindJSON(&dto)
	db.Save(&User{Email: dto.Email})
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Save")
	if f == nil {
		t.Fatalf("expected a db.Save flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.SourceField != "Email" {
		t.Errorf("source field = %q, want Email (lifted from dto.Email)", f.SourceField)
	}
}

// --- POSITIVE: net/http r.FormValue → w.Write (response flow) ----------

func TestGoDataFlow_NetHTTPFormValueToResponse(t *testing.T) {
	src := `package h
func handler(w http.ResponseWriter, r *http.Request) {
	x := r.FormValue("x")
	w.Write([]byte(x))
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "w.Write")
	if f == nil {
		t.Fatalf("expected a w.Write response flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkResponse {
		t.Errorf("sink kind = %q, want response", f.SinkKind)
	}
	if f.SourceField != "x" {
		t.Errorf("source field = %q, want x", f.SourceField)
	}
}

// --- POSITIVE: echo QueryParam → c.JSON response ----------------------

func TestGoDataFlow_EchoQueryParamToResponse(t *testing.T) {
	src := `package h
func search(c echo.Context) error {
	term := c.QueryParam("term")
	return c.JSON(200, term)
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "c.JSON")
	if f == nil {
		t.Fatalf("expected a c.JSON flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkResponse {
		t.Errorf("sink kind = %q, want response", f.SinkKind)
	}
	if f.SourceField != "term" {
		t.Errorf("source field = %q, want term", f.SourceField)
	}
}

// --- POSITIVE: chi URLParam → db.Exec (db_write) ----------------------

func TestGoDataFlow_ChiURLParamToExec(t *testing.T) {
	src := `package h
func del(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	db.Exec("DELETE FROM t WHERE id = " + id)
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Exec")
	if f == nil {
		t.Fatalf("expected a db.Exec flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.SourceField != "id" {
		t.Errorf("source field = %q, want id", f.SourceField)
	}
}

// --- POSITIVE: net/http json.Decode(&dto) → http.Post (http_call) -----

func TestGoDataFlow_JSONDecodeToOutboundHTTP(t *testing.T) {
	src := `package h
func proxy(w http.ResponseWriter, r *http.Request) {
	var dto Payload
	json.NewDecoder(r.Body).Decode(&dto)
	http.Post("https://api", "application/json", dto.Body)
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "http.Post")
	if f == nil {
		t.Fatalf("expected an http.Post flow, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkHTTPCall {
		t.Errorf("sink kind = %q, want http_call", f.SinkKind)
	}
	if f.SourceField != "Body" {
		t.Errorf("source field = %q, want Body (lifted from dto.Body)", f.SourceField)
	}
}

// --- POSITIVE: one local hop, handler→helper→sink ---------------------

func TestGoDataFlow_OneLocalHop(t *testing.T) {
	src := `package h
func handler(c *gin.Context) {
	q := c.Query("name")
	persist(q)
}
func persist(val string) {
	db.Create(&Item{Name: val})
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Create")
	if f == nil {
		t.Fatalf("expected a hopped db.Create flow, got %+v", flows)
	}
	if f.Function != "handler" {
		t.Errorf("origin = %q, want handler (flow attributed to originating handler)", f.Function)
	}
	if len(f.HopPath) != 1 || f.HopPath[0] != "persist" {
		t.Errorf("hop path = %v, want [persist]", f.HopPath)
	}
	if f.SourceField != "name" {
		t.Errorf("source field = %q, want name", f.SourceField)
	}
}

// --- NEGATIVE: static value → no flow ---------------------------------

func TestGoDataFlow_StaticValueNoFlow(t *testing.T) {
	src := `package h
func CreateItem(c *gin.Context) {
	name := "constant"
	db.Create(&Item{Name: name})
}`
	flows := sniffDataFlowGo(src)
	if f := findGoFlow(flows, "db.Create"); f != nil {
		t.Fatalf("expected no flow for a static value, got %+v", f)
	}
}

// --- NEGATIVE: non-request var → no source ----------------------------

func TestGoDataFlow_NonRequestVarNoSource(t *testing.T) {
	src := `package h
func CreateItem(c *gin.Context) {
	val := computeSomething()
	db.Create(&Item{Name: val})
}`
	flows := sniffDataFlowGo(src)
	if f := findGoFlow(flows, "db.Create"); f != nil {
		t.Fatalf("expected no flow for a non-request var, got %+v", f)
	}
}

// --- NEGATIVE: dynamic key → flow but NO field ------------------------

func TestGoDataFlow_DynamicKeyNoField(t *testing.T) {
	src := `package h
func CreateItem(c *gin.Context) {
	k := "x"
	q := c.Query(k)
	db.Create(&Item{Name: q})
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Create")
	if f == nil {
		t.Fatalf("expected a flow (the value IS request-derived), got %+v", flows)
	}
	if f.SourceField != "" {
		t.Errorf("source field = %q, want empty (dynamic key — honest-partial)", f.SourceField)
	}
}

// --- NEGATIVE: reassignment breaks the chain --------------------------

func TestGoDataFlow_ReassignmentBreaksChain(t *testing.T) {
	src := `package h
func CreateItem(c *gin.Context) {
	q := c.Query("q")
	q = "safe"
	db.Create(&Item{Name: q})
}`
	flows := sniffDataFlowGo(src)
	if f := findGoFlow(flows, "db.Create"); f != nil {
		t.Fatalf("expected no flow after reassignment to a constant, got %+v", f)
	}
}

// --- PARAM EXPANSION: shared trailing type ----------------------------

func TestGoExpandParams_SharedType(t *testing.T) {
	got := goExpandParams("a, b string, c int")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slot %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGoExpandParams_TypeOnlyUnnamed(t *testing.T) {
	got := goExpandParams("string, int")
	for i, n := range got {
		if n != "" {
			t.Errorf("slot %d = %q, want empty (unnamed type-only param)", i, n)
		}
	}
}

// ====================================================================
// wave4-go-request-sink-dataflow (#3872): per-framework VERIFY-FIRST
// tests proving the LIVE go sniffer recognises each flipped framework's
// REAL request-input idiom. Each asserts an EXACT source field + sink;
// a paired negative proves non-vacuousness (the flow only fires for the
// matched idiom — a divergent receiver/method yields NO flow).
// ====================================================================

// --- FIBER: handler is func(c *fiber.Ctx) error. Fiber's key-getter
// request reads c.Query("k") / c.FormValue("k") share the `c`-receiver
// + getter-name idiom the sniffer recognises (same shape as gin/echo).
// (c.BodyParser(&dto) does NOT match dfGoBindRe — honest-partial boundary.)

func TestGoDataFlow_FiberQueryToDBCreate(t *testing.T) {
	src := `package h
func ListItems(c *fiber.Ctx) error {
	name := c.Query("name")
	db.Create(&Item{Title: name})
	return nil
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Create")
	if f == nil {
		t.Fatalf("expected a db.Create flow from fiber c.Query, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.Function != "ListItems" {
		t.Errorf("origin = %q, want ListItems", f.Function)
	}
	if f.SourceField != "name" {
		t.Errorf("source field = %q, want name (from c.Query(\"name\"))", f.SourceField)
	}
}

func TestGoDataFlow_FiberFormValueToResponse(t *testing.T) {
	src := `package h
func Echo(c *fiber.Ctx) error {
	v := c.FormValue("msg")
	return c.JSON(v)
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "c.JSON")
	if f == nil {
		t.Fatalf("expected a c.JSON response flow from fiber c.FormValue, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkResponse {
		t.Errorf("sink kind = %q, want response", f.SinkKind)
	}
	if f.SourceField != "msg" {
		t.Errorf("source field = %q, want msg (from c.FormValue(\"msg\"))", f.SourceField)
	}
}

// NON-VACUOUSNESS for fiber: fiber's c.BodyParser(&dto) is NOT a recognised
// bind source (unlike gin c.ShouldBindJSON / echo c.Bind). With ONLY a
// BodyParser read and no key-getter, NO flow is produced — proving the
// flip is carried solely by the matched c.Query/c.FormValue idiom.
func TestGoDataFlow_FiberBodyParserNotASource(t *testing.T) {
	src := `package h
func Signup(c *fiber.Ctx) error {
	var dto UserDTO
	c.BodyParser(&dto)
	db.Save(&User{Email: dto.Email})
	return nil
}`
	flows := sniffDataFlowGo(src)
	if f := findGoFlow(flows, "db.Save"); f != nil {
		t.Errorf("expected NO flow (c.BodyParser is not a recognised bind source), got %+v", *f)
	}
}

// --- BUFFALO: handler is func(c buffalo.Context) error. Buffalo's bind
// idiom is c.Bind(&dto) — matched by dfGoBindRe (the bare `Bind` arm,
// same as echo). The bound root's later member access lifts the field.

func TestGoDataFlow_BuffaloBindToDBSave(t *testing.T) {
	src := `package h
func CreateOrder(c buffalo.Context) error {
	var req OrderReq
	c.Bind(&req)
	db.Save(&Order{Sku: req.Sku})
	return nil
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Save")
	if f == nil {
		t.Fatalf("expected a db.Save flow from buffalo c.Bind, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.Function != "CreateOrder" {
		t.Errorf("origin = %q, want CreateOrder", f.Function)
	}
	if f.SourceField != "Sku" {
		t.Errorf("source field = %q, want Sku (lifted from req.Sku via c.Bind root)", f.SourceField)
	}
}

// NON-VACUOUSNESS for buffalo: if the bind root is NOT populated by a
// recognised bind call (here a plain var, no c.Bind), the member access
// req.Sku is untainted and NO flow is produced.
func TestGoDataFlow_BuffaloNoBindNoFlow(t *testing.T) {
	src := `package h
func CreateOrder(c buffalo.Context) error {
	var req OrderReq
	db.Save(&Order{Sku: req.Sku})
	return nil
}`
	flows := sniffDataFlowGo(src)
	if f := findGoFlow(flows, "db.Save"); f != nil {
		t.Errorf("expected NO flow without a c.Bind source, got %+v", *f)
	}
}

// --- GORILLA-MUX: handlers are stdlib func(w, r *http.Request). gorilla
// is a router over net/http; request reads are the stdlib forms
// json.NewDecoder(r.Body).Decode(&dto) / r.URL.Query().Get / r.FormValue,
// all recognised by the net/http arms of the sniffer.

func TestGoDataFlow_GorillaDecodeBodyToDBCreate(t *testing.T) {
	src := `package h
func CreateUser(w http.ResponseWriter, r *http.Request) {
	var dto UserDTO
	json.NewDecoder(r.Body).Decode(&dto)
	db.Create(&User{Email: dto.Email})
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "db.Create")
	if f == nil {
		t.Fatalf("expected a db.Create flow from gorilla stdlib Decode, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkDBWrite {
		t.Errorf("sink kind = %q, want db_write", f.SinkKind)
	}
	if f.Function != "CreateUser" {
		t.Errorf("origin = %q, want CreateUser", f.Function)
	}
	if f.SourceField != "Email" {
		t.Errorf("source field = %q, want Email (lifted from dto.Email)", f.SourceField)
	}
}

func TestGoDataFlow_GorillaQueryGetToResponse(t *testing.T) {
	src := `package h
func Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	w.Write([]byte(q))
}`
	flows := sniffDataFlowGo(src)
	f := findGoFlow(flows, "w.Write")
	if f == nil {
		t.Fatalf("expected a w.Write response flow from gorilla r.URL.Query().Get, got %+v", flows)
	}
	if f.SinkKind != DataFlowSinkResponse {
		t.Errorf("sink kind = %q, want response", f.SinkKind)
	}
	if f.SourceField != "q" {
		t.Errorf("source field = %q, want q (from r.URL.Query().Get(\"q\"))", f.SourceField)
	}
}

// --- HONEST-MISSING GUARDS: prove the DIVERGENT idioms of the
// not-flipped frameworks genuinely produce NO flow, justifying their
// honest-missing verdict. These pin the boundary so a future sniffer
// extension that adds them will flip a RED test → forcing a registry update.

// fasthttp: receiver `ctx *fasthttp.RequestCtx`, ctx.QueryArgs().Peek("k").
func TestGoDataFlow_FasthttpCtxIdiomNotRecognised(t *testing.T) {
	src := `package h
func listUsers(ctx *fasthttp.RequestCtx) {
	q := ctx.QueryArgs().Peek("q")
	db.Create(&Item{Name: string(q)})
}`
	if f := findGoFlow(sniffDataFlowGo(src), "db.Create"); f != nil {
		t.Errorf("fasthttp ctx.QueryArgs idiom should NOT be recognised, got %+v", *f)
	}
}

// hertz: request receiver named `ctx *app.RequestContext` (the `c` param is
// stdlib context.Context); ctx.Query / ctx.BindAndValidate are divergent.
func TestGoDataFlow_HertzCtxIdiomNotRecognised(t *testing.T) {
	src := `package h
func createUser(c context.Context, ctx *app.RequestContext) {
	q := ctx.Query("q")
	db.Create(&Item{Name: q})
}`
	if f := findGoFlow(sniffDataFlowGo(src), "db.Create"); f != nil {
		t.Errorf("hertz ctx.Query idiom should NOT be recognised, got %+v", *f)
	}
}

// go-zero: r *http.Request receiver but bind via httpx.Parse(r,&req) — not a
// recognised bind helper (only .Decode(&x) matches), so honest-missing.
func TestGoDataFlow_GoZeroHttpxParseNotRecognised(t *testing.T) {
	src := `package h
func createUserHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateReq
	httpx.Parse(r, &req)
	db.Create(&User{Email: req.Email})
}`
	if f := findGoFlow(sniffDataFlowGo(src), "db.Create"); f != nil {
		t.Errorf("go-zero httpx.Parse bind should NOT be recognised, got %+v", *f)
	}
}

// revel: controller method func (c App) X(); params via c.Params.Get("k")
// (Params is a struct field, .Get a method) — does NOT match c.Param(.
func TestGoDataFlow_RevelParamsGetNotRecognised(t *testing.T) {
	src := `package h
func (c App) Show() revel.Result {
	id := c.Params.Get("id")
	db.Create(&Item{Ref: id})
	return c.Render()
}`
	if f := findGoFlow(sniffDataFlowGo(src), "db.Create"); f != nil {
		t.Errorf("revel c.Params.Get idiom should NOT be recognised, got %+v", *f)
	}
}
