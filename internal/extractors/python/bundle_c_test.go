// bundle_c_test.go — Python Bundle C coverage tests.
//
// One file, five tickets:
//
//	#1979 — Celery .delay() / .apply_async() emits CALLS edge caller→task.
//	#1980 — @shared_task decorator kwargs captured + is_task property.
//	#1984 — async def is_async property + channel_layer dispatch CALLS edge.
//	#1985 — Dead imports stamped live=false / dead_import=true.
//	#1991 — __init__.py from .X import Y annotated re_export / public / alias.
//
// Test fixtures use the synthetic "client-fixture-X" name for any pseudo-
// package reference. No real client / product names are leaked.
package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/python"
	"github.com/cajasmota/grafel/internal/types"
)

// extractPy is a small helper that drives the registered "python" extractor
// over the given source content + repo-relative path.
func extractBundleC(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	tree := parse(t, []byte(src))
	defer tree.Close()
	file := extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	}
	ents, err := ext.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func findEntityByLeaf(ents []types.EntityRecord, kind, leaf string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind != kind {
			continue
		}
		name := e.Name
		if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
			name = name[dot+1:]
		}
		if name == leaf {
			return e
		}
	}
	return nil
}

func anyCallEdge(ents []types.EntityRecord, fromLeaf, toContains string) bool {
	from := findEntityByLeaf(ents, "SCOPE.Operation", fromLeaf)
	if from == nil {
		return false
	}
	for _, r := range from.Relationships {
		if r.Kind == "CALLS" && strings.Contains(r.ToID, toContains) {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------
// #1980 — @shared_task decorator kwargs + dedup signal (is_task property).
// -----------------------------------------------------------------------
func TestCeleryDecoratorKwargsCaptured(t *testing.T) {
	src := `from celery import shared_task

@shared_task(bind=True, max_retries=3, autoretry_for=(Exception,), name="client-fixture-x.tasks.do_work", queue="critical")
def do_work(self, x):
    return x + 1
`
	ents := extractBundleC(t, "client_fixture_x/tasks.py", src)

	op := findEntityByLeaf(ents, "SCOPE.Operation", "do_work")
	if op == nil {
		t.Fatal("Operation entity for do_work not found")
	}
	if op.Properties["is_task"] != "true" {
		t.Errorf("expected is_task=true, got %q", op.Properties["is_task"])
	}
	wantPairs := map[string]string{
		"bind":          "True",
		"max_retries":   "3",
		"autoretry_for": "Exception,",
		"name":          "client-fixture-x.tasks.do_work",
		"queue":         "critical",
		"task_name":     "client-fixture-x.tasks.do_work",
		"framework":     "celery",
	}
	for k, want := range wantPairs {
		if got := op.Properties[k]; got != want {
			t.Errorf("Properties[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestCeleryDefaultTaskNameFromModulePath(t *testing.T) {
	src := `from celery import shared_task

@shared_task
def send_email():
    pass
`
	ents := extractBundleC(t, "client_fixture_x/notifications/tasks.py", src)
	op := findEntityByLeaf(ents, "SCOPE.Operation", "send_email")
	if op == nil {
		t.Fatal("Operation entity for send_email not found")
	}
	// filePathToModule strips no prefix (path doesn't start with src/lib/app)
	// so module = "client_fixture_x.notifications.tasks".
	wantTaskName := "client_fixture_x.notifications.tasks.send_email"
	if got := op.Properties["task_name"]; got != wantTaskName {
		t.Errorf("task_name = %q, want %q", got, wantTaskName)
	}
}

// -----------------------------------------------------------------------
// #1979 — .delay() / .apply_async() emit CALLS edges.
// -----------------------------------------------------------------------
func TestCeleryDispatchEmitsCallsEdgeSameFile(t *testing.T) {
	src := `from celery import shared_task

@shared_task
def process_payment(order_id):
    return order_id

def create_order(order_id):
    process_payment.delay(order_id)
    process_payment.apply_async((order_id,), countdown=10)
`
	ents := extractBundleC(t, "client_fixture_x/orders.py", src)
	if !anyCallEdge(ents, "create_order", "process_payment") {
		t.Error("expected CALLS edge create_order -> process_payment via .delay() / .apply_async()")
	}
}

// -----------------------------------------------------------------------
// #1984 — async semantics (is_async + channel_layer dispatch).
// -----------------------------------------------------------------------
func TestAsyncIsAsyncProperty(t *testing.T) {
	src := `import asyncio

async def connect(scope):
    await asyncio.sleep(0)

def sync_handler():
    pass
`
	ents := extractBundleC(t, "client_fixture_x/consumer.py", src)
	asyncOp := findEntityByLeaf(ents, "SCOPE.Operation", "connect")
	if asyncOp == nil {
		t.Fatal("connect Operation not found")
	}
	if asyncOp.Properties["is_async"] != "true" {
		t.Errorf("connect.is_async = %q, want true", asyncOp.Properties["is_async"])
	}
	syncOp := findEntityByLeaf(ents, "SCOPE.Operation", "sync_handler")
	if syncOp == nil {
		t.Fatal("sync_handler Operation not found")
	}
	if syncOp.Properties["is_async"] == "true" {
		t.Error("sync_handler should NOT have is_async=true")
	}
}

func TestAsyncChannelLayerDispatchEmitsCallsEdge(t *testing.T) {
	src := `class UserNotificationConsumer:
    async def connect(self):
        await self.channel_layer.group_add("notifications", self.channel_name)

    async def disconnect(self, code):
        await self.channel_layer.group_discard("notifications", self.channel_name)

    async def broadcast(self, message):
        await self.channel_layer.group_send("notifications", {"type": "msg", "text": message})
`
	ents := extractBundleC(t, "client_fixture_x/consumer.py", src)

	wantMethods := []struct {
		leaf   string
		method string
	}{
		{"connect", "group_add"},
		{"disconnect", "group_discard"},
		{"broadcast", "group_send"},
	}
	for _, w := range wantMethods {
		from := findEntityByLeaf(ents, "SCOPE.Operation", w.leaf)
		if from == nil {
			t.Errorf("%s Operation not found", w.leaf)
			continue
		}
		hit := false
		for _, r := range from.Relationships {
			if r.Kind == "CALLS" && r.ToID == "ext:channel_layer:"+w.method {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("%s did not emit CALLS -> ext:channel_layer:%s", w.leaf, w.method)
		}
	}
}

// -----------------------------------------------------------------------
// #1985 — Dead imports.
// -----------------------------------------------------------------------
func TestDeadImportFlaggedWhenSymbolUnused(t *testing.T) {
	src := `from drf.permissions import HasPermission, IsAuthenticated
from drf.views import APIView

class Foo(APIView):
    permission_classes = [IsAuthenticated]
    def get(self, request):
        return APIView()
`
	ents := extractBundleC(t, "client_fixture_x/views.py", src)
	// Locate the file entity which carries the IMPORTS edges.
	var fileEnt *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			fileEnt = &ents[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}

	check := map[string]string{
		"HasPermission":   "true", // expected dead
		"IsAuthenticated": "",     // live (used in permission_classes list)
		"APIView":         "",     // live (used as base + constructed)
	}
	seen := map[string]bool{}
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" || r.Properties == nil {
			continue
		}
		local := r.Properties["local_name"]
		want, tracked := check[local]
		if !tracked {
			continue
		}
		seen[local] = true
		got := r.Properties["dead_import"]
		if got != want {
			t.Errorf("import %q dead_import = %q, want %q", local, got, want)
		}
	}
	for k := range check {
		if !seen[k] {
			t.Errorf("expected an IMPORTS edge for %q, none seen", k)
		}
	}
}

// -----------------------------------------------------------------------
// #1991 — __init__.py re-export annotation.
// -----------------------------------------------------------------------
func TestInitPyReExportAnnotations(t *testing.T) {
	src := `from .celery import app as celery_app
from .models import User
__all__ = ("celery_app", "User")
`
	ents := extractBundleC(t, "client_fixture_x/__init__.py", src)
	var fileEnt *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			fileEnt = &ents[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}

	type want struct {
		local       string
		reExport    string
		packageInit string
		public      string
		alias       string
	}
	wants := []want{
		{local: "celery_app", reExport: "true", packageInit: "true", public: "true", alias: "celery_app"},
		{local: "User", reExport: "true", packageInit: "true", public: "true", alias: ""},
	}
	for _, w := range wants {
		hit := false
		for _, r := range fileEnt.Relationships {
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if r.Properties["local_name"] != w.local {
				continue
			}
			hit = true
			if r.Properties["re_export"] != w.reExport {
				t.Errorf("%s re_export = %q, want %q", w.local, r.Properties["re_export"], w.reExport)
			}
			if r.Properties["package_init"] != w.packageInit {
				t.Errorf("%s package_init = %q, want %q", w.local, r.Properties["package_init"], w.packageInit)
			}
			if r.Properties["public"] != w.public {
				t.Errorf("%s public = %q, want %q", w.local, r.Properties["public"], w.public)
			}
			if w.alias != "" && r.Properties["alias"] != w.alias {
				t.Errorf("%s alias = %q, want %q", w.local, r.Properties["alias"], w.alias)
			}
			if w.alias == "" && r.Properties["alias"] != "" {
				t.Errorf("%s alias = %q, want empty", w.local, r.Properties["alias"])
			}
		}
		if !hit {
			t.Errorf("no IMPORTS edge with local_name=%q", w.local)
		}
	}
}

// Re-exports listed in __all__ must NOT be flagged dead even if the
// __init__.py body never references them — coordinates #1985 with #1991.
func TestReExportSuppressesDeadImport(t *testing.T) {
	src := `from .celery import app as celery_app
__all__ = ("celery_app",)
`
	ents := extractBundleC(t, "client_fixture_x/__init__.py", src)
	var fileEnt *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			fileEnt = &ents[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatal("file entity not found")
	}
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" || r.Properties == nil {
			continue
		}
		if r.Properties["local_name"] != "celery_app" {
			continue
		}
		if r.Properties["dead_import"] == "true" {
			t.Error("celery_app should NOT be flagged dead (re-exported in __all__)")
		}
	}
}
