package substrate

import "testing"

// dataflow_python_framework_sources_test.go — issue #3927: generalize
// request-source recognition for the python DATA_FLOWS_TO sniffer beyond
// DRF/Django-only idioms. The sink set + multi-hop machinery is unchanged;
// only SOURCE recognition is extended to Flask (request.form / request.args /
// request.values) and Django subscript form (request.POST['x'] /
// request.GET['x']). Each fixture asserts the RESOLVED sink AND the recovered
// request-source field — never a bare len>0 — and is the proving fixture for
// flipping the respective coverage cell.

// assertPyReqToSink is the shared assertion: the named handler must produce a
// flow of wantKind whose source field is wantField and whose sink callee is
// wantSink.
func assertPyReqToSink(t *testing.T, fw, src, handler, wantField string, wantKind DataFlowSinkKind, wantSink string) {
	t.Helper()
	flows := sniffDataFlowPython(src)
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == handler && f.SinkKind == wantKind
	})
	if got == nil {
		t.Fatalf("[%s] expected a %s flow in %s, got %+v", fw, wantKind, handler, flows)
	}
	if got.SourceField != wantField {
		t.Errorf("[%s] source field = %q, want %q", fw, got.SourceField, wantField)
	}
	if got.SinkName != wantSink {
		t.Errorf("[%s] sink = %q, want %q", fw, got.SinkName, wantSink)
	}
}

// TestDataFlowPython_Flask_FormSubscriptToDBWrite — Flask form access via the
// subscript form `request.form['email']` flowing into an ORM create.
func TestDataFlowPython_Flask_FormSubscriptToDBWrite(t *testing.T) {
	src := `
def create_user():
    email = request.form['email']
    User.objects.create(email=email)
    return Response('ok')
`
	assertPyReqToSink(t, "flask-form", src, "create_user", "email", DataFlowSinkDBWrite, "User.objects.create")
}

// TestDataFlowPython_Flask_FormGetToDBWrite — Flask `request.form.get('name')`.
func TestDataFlowPython_Flask_FormGetToDBWrite(t *testing.T) {
	src := `
def add_account():
    name = request.form.get('name')
    Account.save(name)
    return Response('ok')
`
	assertPyReqToSink(t, "flask-form-get", src, "add_account", "name", DataFlowSinkDBWrite, "Account.save")
}

// TestDataFlowPython_Flask_ArgsGetToResponse — Flask query `request.args.get`.
func TestDataFlowPython_Flask_ArgsGetToResponse(t *testing.T) {
	src := `
def search():
    q = request.args.get('q')
    return Response(q)
`
	assertPyReqToSink(t, "flask-args", src, "search", "q", DataFlowSinkResponse, "Response")
}

// TestDataFlowPython_Flask_ValuesGetToHTTPCall — Flask `request.values.get`
// flowing into an outbound HTTP call.
func TestDataFlowPython_Flask_ValuesGetToHTTPCall(t *testing.T) {
	src := `
def proxy():
    token = request.values.get('token')
    requests.post(token)
    return Response('ok')
`
	assertPyReqToSink(t, "flask-values", src, "proxy", "token", DataFlowSinkHTTPCall, "requests.post")
}

// TestDataFlowPython_Django_PostSubscriptToDBWrite — Django form access via
// the subscript form `request.POST['title']`.
func TestDataFlowPython_Django_PostSubscriptToDBWrite(t *testing.T) {
	src := `
def create_post(request):
    title = request.POST['title']
    Post.objects.create(title=title)
    return Response('ok')
`
	assertPyReqToSink(t, "django-post", src, "create_post", "title", DataFlowSinkDBWrite, "Post.objects.create")
}

// TestDataFlowPython_Django_GetSubscriptToResponse — Django query subscript
// `request.GET['page']`.
func TestDataFlowPython_Django_GetSubscriptToResponse(t *testing.T) {
	src := `
def list_items(request):
    page = request.GET['page']
    return JsonResponse(page)
`
	assertPyReqToSink(t, "django-get", src, "list_items", "page", DataFlowSinkResponse, "JsonResponse")
}

// --- Negatives: precision guards (honest-partial) ---

// TestDataFlowPython_NonRequestParam_NoFlow — a value that is NOT a request
// input (a plain local literal) must NOT produce a flow.
func TestDataFlowPython_NonRequestParam_NoFlow(t *testing.T) {
	src := `
def create_user():
    email = 'static@example.com'
    User.objects.create(email=email)
    return Response('ok')
`
	flows := sniffDataFlowPython(src)
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("static value must not flow, got %+v", got)
	}
}

// TestDataFlowPython_DynamicFormKey_NoFlow — a dynamic form key
// (`request.form[k]`) is not statically knowable and must be dropped.
func TestDataFlowPython_DynamicFormKey_NoFlow(t *testing.T) {
	src := `
def create_user(k):
    val = request.form[k]
    Other.objects.create(val=val)
    return Response('ok')
`
	flows := sniffDataFlowPython(src)
	if got := findFlow(flows, func(f DataFlow) bool {
		return f.SinkKind == DataFlowSinkDBWrite && f.SourceField != ""
	}); got != nil {
		t.Fatalf("dynamic form key must not yield a static-field flow, got %+v", got)
	}
}
