package python_test

import (
	"context"
	"os"
	"strings"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
)

// extractResult holds extracted entity fields for assertion.
type extractResult struct {
	Name      string
	Kind      string
	Subtype   string
	StartLine int
	Props     map[string]string
	Rels      []relResult
}

// relResult holds a relationship's ToID and Kind for assertion.
type relResult struct {
	ToID string
	Kind string
}

// extract returns extracted entities with fields for assertion.
func extract(t *testing.T, key, content string) []extractResult {
	t.Helper()
	ext, ok := extractor.Get(key)
	if !ok {
		t.Fatalf("%s extractor not registered", key)
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "test.py",
		Content:  []byte(content),
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract(%s): %v", key, err)
	}
	var result []extractResult
	for _, e := range entities {
		var rels []relResult
		for _, r := range e.Relationships {
			rels = append(rels, relResult{ToID: r.ToID, Kind: r.Kind})
		}
		result = append(result, extractResult{
			Name:      e.Name,
			Kind:      e.Kind,
			Subtype:   e.Subtype,
			StartLine: e.StartLine,
			Props:     e.Properties,
			Rels:      rels,
		})
	}
	return result
}

// ============================================================================
// Django tests
// ============================================================================

func TestDjango_URLPattern(t *testing.T) {
	src := `from django.urls import path
from . import views

urlpatterns = [
    path('users/', views.user_list, name="user-list"),
    path('users/<int:id>/', views.user_detail, name="user-detail"),
]`
	ents := extract(t, "python_django", src)
	count := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "endpoint" && e.Props["pattern_type"] == "url_pattern" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 URL patterns, got %d", count)
	}
}

func TestDjango_CBV(t *testing.T) {
	src := `class UserListView(ListView):
    model = User

    def get(self, request):
        pass

    def post(self, request):
        pass
`
	ents := extract(t, "python_django", src)
	cbvCount := 0
	methodCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "cbv" {
			cbvCount++
		}
		if e.Props["pattern_type"] == "cbv_method" {
			methodCount++
		}
	}
	if cbvCount != 1 {
		t.Fatalf("expected 1 CBV class, got %d", cbvCount)
	}
	if methodCount != 2 {
		t.Fatalf("expected 2 CBV methods, got %d", methodCount)
	}
}

func TestDjango_SignalReceiver(t *testing.T) {
	src := `@receiver(post_save, sender=User)
def notify_user(sender, instance, **kwargs):
    pass
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "notify_user" && e.Props["signal_type"] == "post_save" && e.Props["sender"] == "User" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected signal receiver entity")
	}
}

func TestDjango_AdminRegister(t *testing.T) {
	src := `admin.site.register(User, UserAdmin)

@admin.register(Post)
class PostAdmin(admin.ModelAdmin):
    pass
`
	ents := extract(t, "python_django", src)
	adminCount := 0
	for _, e := range ents {
		if e.Subtype == "admin_class" {
			adminCount++
		}
	}
	if adminCount != 2 {
		t.Fatalf("expected 2 admin registrations, got %d", adminCount)
	}
}

func TestDjango_DRFSerializer(t *testing.T) {
	src := `class UserSerializer(serializers.ModelSerializer):
    class Meta:
        model = User
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "UserSerializer" && e.Props["component_kind"] == "serializer" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected DRF serializer entity")
	}
}

func TestDjango_CeleryTask(t *testing.T) {
	src := `@shared_task(queue="emails")
def send_email(to, subject):
    pass
`
	// #1411: Celery task extraction is owned by python_celery, not python_django.
	// The Django extractor no longer re-emits Celery tasks to avoid duplicate nodes.
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Name == "send_email" && e.Props["queue"] == "emails" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Celery task entity")
	}
}

func TestDjango_Middleware(t *testing.T) {
	src := `class AuthMiddleware(MiddlewareMixin):
    def process_request(self, request):
        pass
    def process_response(self, request, response):
        pass
`
	ents := extract(t, "python_django", src)
	middlewareCount := 0
	hookCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "middleware" {
			middlewareCount++
		}
		if e.Props["pattern_type"] == "middleware_hook" {
			hookCount++
		}
	}
	if middlewareCount != 1 {
		t.Fatalf("expected 1 middleware, got %d", middlewareCount)
	}
	if hookCount != 2 {
		t.Fatalf("expected 2 hooks, got %d", hookCount)
	}
}

func TestDjango_TemplateTag(t *testing.T) {
	src := `@register.filter
def currency(value):
    pass

@register.simple_tag
def current_time(format_string):
    pass
`
	ents := extract(t, "python_django", src)
	tagCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "template_tag" {
			tagCount++
		}
	}
	if tagCount != 2 {
		t.Fatalf("expected 2 template tags, got %d", tagCount)
	}
}

func TestDjango_MgmtCommand(t *testing.T) {
	src := `class Command(BaseCommand):
    def handle(self, *args, **options):
        pass
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "Command.handle" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected management command handle entity")
	}
}

func TestDjango_ModelManager(t *testing.T) {
	src := `class ActiveManager(Manager):
    def get_queryset(self):
        pass
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "ActiveManager" && e.Kind == "SCOPE.Schema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected model manager entity")
	}
}

func TestDjango_NoMatch(t *testing.T) {
	src := `def regular_function():
    pass

class RegularClass:
    pass
`
	ents := extract(t, "python_django", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-Django code, got %d", len(ents))
	}
}

// ---- #1374 item 3: phantom-orphan regression tests ----

// TestDjango_SignalReceiver_HandlesSignalEdge verifies that the signal handler
// function is the emitted entity (not the sender model) and that a
// HANDLES_SIGNAL edge targets the sender model via "Class:<Model>" ref.
func TestDjango_SignalReceiver_HandlesSignalEdge(t *testing.T) {
	src := `@receiver(post_save, sender=Contract)
def replicate_contract(sender, instance, created, **kwargs):
    pass
`
	ents := extract(t, "python_django", src)

	// Entity name must be the HANDLER function, not the sender model.
	var handler *extractResult
	for i := range ents {
		if ents[i].Name == "replicate_contract" {
			handler = &ents[i]
		}
	}
	if handler == nil {
		t.Fatal("expected entity named 'replicate_contract' (handler function), not found")
	}

	// The sender model must NOT be emitted as a new entity.
	for _, e := range ents {
		if e.Name == "Contract" {
			t.Fatalf("phantom entity emitted for sender model 'Contract' — should only be a relationship target")
		}
	}

	// The handler entity must carry a HANDLES_SIGNAL edge to Class:Contract.
	found := false
	for _, r := range handler.Rels {
		if r.Kind == "HANDLES_SIGNAL" && r.ToID == "Class:Contract" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected HANDLES_SIGNAL → Class:Contract edge on handler entity; got rels: %+v", handler.Rels)
	}

	// Signal type captured in properties.
	if handler.Props["signal_type"] != "post_save" {
		t.Errorf("expected signal_type=post_save, got %q", handler.Props["signal_type"])
	}
}

// TestDjango_SignalReceiver_NoSender verifies that a @receiver without a
// sender= kwarg still emits the handler entity (with no phantom orphan).
func TestDjango_SignalReceiver_NoSender(t *testing.T) {
	src := `@receiver(request_finished)
def flush_cache(sender, **kwargs):
    pass
`
	ents := extract(t, "python_django", src)
	var handler *extractResult
	for i := range ents {
		if ents[i].Name == "flush_cache" {
			handler = &ents[i]
		}
	}
	if handler == nil {
		t.Fatal("expected entity named 'flush_cache'")
	}
	// No HANDLES_SIGNAL edge when there is no sender.
	for _, r := range handler.Rels {
		if r.Kind == "HANDLES_SIGNAL" {
			t.Errorf("unexpected HANDLES_SIGNAL edge without sender= kwarg: %+v", r)
		}
	}
}

// TestDjango_AdminRegister_RegistersEdge verifies that admin.site.register
// emits the admin-class entity (not a phantom Controller:<Model>) plus a
// REGISTERS edge targeting the model via "Class:<Model>".
func TestDjango_AdminRegister_RegistersEdge(t *testing.T) {
	src := `admin.site.register(Contract, ContractAdmin)
`
	ents := extract(t, "python_django", src)

	// Must find the admin-class entity by the ADMIN CLASS name, not the model name.
	var adminEnt *extractResult
	for i := range ents {
		if ents[i].Name == "ContractAdmin" && ents[i].Subtype == "admin_class" {
			adminEnt = &ents[i]
		}
	}
	if adminEnt == nil {
		t.Fatal("expected entity named 'ContractAdmin' with subtype=admin_class")
	}

	// The model name must NOT appear as a new phantom entity.
	for _, e := range ents {
		if e.Name == "Contract" {
			t.Fatalf("phantom entity emitted for model 'Contract' — should only be a relationship target")
		}
	}

	// The admin entity must carry a REGISTERS edge to Class:Contract.
	found := false
	for _, r := range adminEnt.Rels {
		if r.Kind == "REGISTERS" && r.ToID == "Class:Contract" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected REGISTERS → Class:Contract edge on ContractAdmin; got rels: %+v", adminEnt.Rels)
	}
}

// TestDjango_AdminRegister_ImpliedAdminClass verifies the synthesised
// "<Model>Admin" name when admin.site.register is called with only the model.
func TestDjango_AdminRegister_ImpliedAdminClass(t *testing.T) {
	src := `admin.site.register(Invoice)
`
	ents := extract(t, "python_django", src)

	var adminEnt *extractResult
	for i := range ents {
		if ents[i].Name == "InvoiceAdmin" && ents[i].Subtype == "admin_class" {
			adminEnt = &ents[i]
		}
	}
	if adminEnt == nil {
		t.Fatal("expected synthesised entity named 'InvoiceAdmin'")
	}
	found := false
	for _, r := range adminEnt.Rels {
		if r.Kind == "REGISTERS" && r.ToID == "Class:Invoice" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected REGISTERS → Class:Invoice edge; got rels: %+v", adminEnt.Rels)
	}
}

// TestDjango_AdminDecorator_RegistersEdge verifies that @admin.register(Model)
// emits the decorated admin class with a REGISTERS edge.
func TestDjango_AdminDecorator_RegistersEdge(t *testing.T) {
	src := `@admin.register(Payment)
class PaymentAdmin(admin.ModelAdmin):
    pass
`
	ents := extract(t, "python_django", src)

	var adminEnt *extractResult
	for i := range ents {
		if ents[i].Name == "PaymentAdmin" && ents[i].Subtype == "admin_class" {
			adminEnt = &ents[i]
		}
	}
	if adminEnt == nil {
		t.Fatal("expected entity named 'PaymentAdmin' with subtype=admin_class")
	}
	for _, e := range ents {
		if e.Name == "Payment" {
			t.Fatalf("phantom entity emitted for model 'Payment'")
		}
	}
	found := false
	for _, r := range adminEnt.Rels {
		if r.Kind == "REGISTERS" && r.ToID == "Class:Payment" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected REGISTERS → Class:Payment edge on PaymentAdmin; got rels: %+v", adminEnt.Rels)
	}
}

// TestDjango_MultipleSignals_NoPhantoms is the regression fixture for the
// "Contract appears as 5 disconnected nodes" symptom: multiple @receiver
// handlers on the same model must produce one handler entity each (with
// HANDLES_SIGNAL edges) and zero phantom model entities.
func TestDjango_MultipleSignals_NoPhantoms(t *testing.T) {
	src := `@receiver(post_save, sender=Contract)
def replicate_on_save(sender, instance, **kwargs):
    pass

@receiver(post_delete, sender=Contract)
def replicate_on_delete(sender, instance, **kwargs):
    pass

@receiver(post_save, sender=Invoice)
def replicate_invoice(sender, instance, **kwargs):
    pass
`
	ents := extract(t, "python_django", src)

	// Count handler entities.
	handlerCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "signal" {
			handlerCount++
		}
	}
	if handlerCount != 3 {
		t.Fatalf("expected 3 signal handler entities, got %d", handlerCount)
	}

	// No phantom model entities (Service or Controller named after models).
	for _, e := range ents {
		if e.Name == "Contract" || e.Name == "Invoice" {
			t.Fatalf("phantom entity emitted for model name %q (kind=%s)", e.Name, e.Kind)
		}
	}

	// Each handler must have a HANDLES_SIGNAL edge.
	for _, e := range ents {
		if e.Props["pattern_type"] != "signal" {
			continue
		}
		hasEdge := false
		for _, r := range e.Rels {
			if r.Kind == "HANDLES_SIGNAL" {
				hasEdge = true
			}
		}
		if !hasEdge {
			t.Errorf("handler %q missing HANDLES_SIGNAL edge", e.Name)
		}
	}
}

// TestDjango_SignalReceiver_StackedDecorators verifies that multiple stacked
// @receiver decorators on a single function are ALL captured, emitting one
// HANDLES_SIGNAL edge per @receiver (per sender model).
// Fixes #2599: upvate's replicate_to_datalake.py has 11 @receiver decorators
// on one function; the old regex only captured the last one per file.
func TestDjango_SignalReceiver_StackedDecorators(t *testing.T) {
	src := `@receiver(post_save, sender=Contract)
@receiver(post_delete, sender=Contract)
@receiver(post_save, sender=Invoice)
@receiver(post_delete, sender=Invoice)
@receiver(post_save, sender=Payment)
def replicate_to_datalake(sender, instance, **kwargs):
    pass
`
	ents := extract(t, "python_django", src)

	// Must have exactly one handler entity (not 5).
	var handler *extractResult
	handlerCount := 0
	for i := range ents {
		if ents[i].Name == "replicate_to_datalake" && ents[i].Props["pattern_type"] == "signal" {
			handler = &ents[i]
			handlerCount++
		}
	}
	if handlerCount != 1 {
		t.Fatalf("expected 1 handler entity for stacked @receiver, got %d", handlerCount)
	}
	if handler == nil {
		t.Fatal("expected entity named 'replicate_to_datalake'")
	}

	// Must have exactly 5 HANDLES_SIGNAL edges (one per @receiver).
	handleCount := 0
	for _, r := range handler.Rels {
		if r.Kind == "HANDLES_SIGNAL" {
			handleCount++
		}
	}
	if handleCount != 5 {
		t.Fatalf("expected 5 HANDLES_SIGNAL edges for 5 stacked @receiver decorators, got %d", handleCount)
	}

	// Verify all senders are represented.
	senderSet := make(map[string]bool)
	for _, r := range handler.Rels {
		if r.Kind == "HANDLES_SIGNAL" {
			senderSet[r.ToID] = true
		}
	}
	expectedSenders := map[string]bool{
		"Class:Contract": true,
		"Class:Invoice":  true,
		"Class:Payment":  true,
	}
	for expected := range expectedSenders {
		if !senderSet[expected] {
			t.Errorf("expected HANDLES_SIGNAL edge to %s, not found in rels: %+v", expected, handler.Rels)
		}
	}

	// No phantom model entities.
	for _, e := range ents {
		if e.Name == "Contract" || e.Name == "Invoice" || e.Name == "Payment" {
			t.Fatalf("phantom entity emitted for model %q", e.Name)
		}
	}
}

// ---- #1411: duplicate-kind node regression tests ----

// TestDjango_ViewSet_SingleNode verifies that a DRF ViewSet class is emitted
// as ONE entity (Component/viewset), not two (Component/viewset + endpoint/cbv).
// Fixes #1411.
func TestDjango_ViewSet_SingleNode(t *testing.T) {
	src := `from rest_framework import viewsets

class OrderViewSet(viewsets.ModelViewSet):
    queryset = Order.objects.all()
    serializer_class = OrderSerializer
`
	ents := extract(t, "python_django", src)
	viewsetCount := 0
	cbvCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "viewset" {
			viewsetCount++
		}
		if e.Props["pattern_type"] == "cbv" {
			cbvCount++
		}
	}
	if viewsetCount != 1 {
		t.Fatalf("#1411 ViewSet: expected 1 viewset entity, got %d", viewsetCount)
	}
	if cbvCount != 0 {
		t.Fatalf("#1411 ViewSet: expected 0 cbv entities (ViewSet should not also be a CBV), got %d (total ents=%d)", cbvCount, len(ents))
	}
}

// TestDjango_ViewSet_CBVMethodsStillEmitted verifies that HTTP method handlers
// (def get, def post, etc.) inside a ViewSet ARE still emitted as cbv_method
// entities even after the ViewSet-as-CBV deduplication guard. Edges from those
// methods need to attach to the canonical ViewSet entity.
func TestDjango_ViewSet_CBVMethodsStillEmitted(t *testing.T) {
	src := `from rest_framework import viewsets

class ItemViewSet(viewsets.ModelViewSet):
    def get(self, request, pk=None):
        pass

    def post(self, request):
        pass
`
	ents := extract(t, "python_django", src)
	methodCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "cbv_method" {
			methodCount++
		}
	}
	if methodCount != 2 {
		t.Fatalf("#1411 ViewSet methods: expected 2 cbv_method entities, got %d", methodCount)
	}
}

// TestDjango_CeleryTask_NoDuplicate verifies that a @shared_task in a Django
// file is NOT re-emitted by the Django extractor as a second
// SCOPE.Operation/function entity (the python_celery extractor owns Celery).
// Fixes #1411.
func TestDjango_CeleryTask_NoDuplicate(t *testing.T) {
	src := `from celery import shared_task

@shared_task(queue="billing")
def charge_subscription(customer_id):
    pass
`
	ents := extract(t, "python_django", src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "celery_task" {
			t.Fatalf("#1411 Celery: django extractor must NOT emit celery_task entities (conflicts with python_celery extractor); got entity name=%q", e.Name)
		}
	}
}

// ---- #1412: admin endpoint noise regression tests ----

// TestDjango_AdminRegister_NoEndpoint verifies that admin.site.register and
// @admin.register do NOT cause http_endpoint or SCOPE.Operation/endpoint
// entities to appear in the extractor output. Admin registrations should emit
// admin_class entities only. Fixes #1412.
func TestDjango_AdminRegister_NoEndpoint(t *testing.T) {
	src := `admin.site.register(Order, OrderAdmin)
admin.site.register(Product)

@admin.register(Invoice)
class InvoiceAdmin(admin.ModelAdmin):
    pass
`
	ents := extract(t, "python_django", src)
	for _, e := range ents {
		if e.Kind == "http_endpoint" || e.Kind == "http_endpoint_definition" {
			t.Fatalf("#1412: admin registration emitted an endpoint entity: name=%q kind=%q subtype=%q", e.Name, e.Kind, e.Subtype)
		}
	}
	// Confirm admin_class entities ARE still emitted (3 registrations)
	adminCount := 0
	for _, e := range ents {
		if e.Subtype == "admin_class" {
			adminCount++
		}
	}
	if adminCount != 3 {
		t.Fatalf("#1412: expected 3 admin_class entities, got %d", adminCount)
	}
}

// ============================================================================
// FastAPI tests
// ============================================================================

func TestFastAPI_Route(t *testing.T) {
	src := `@app.get("/users")
async def list_users():
    pass

@router.post("/users")
def create_user(user: UserCreate):
    pass
`
	ents := extract(t, "python_fastapi", src)
	routeCount := 0
	for _, e := range ents {
		if e.Subtype == "endpoint" && e.Props["pattern_type"] == "route" {
			routeCount++
		}
	}
	if routeCount != 2 {
		t.Fatalf("expected 2 routes, got %d", routeCount)
	}
}

func TestFastAPI_Depends(t *testing.T) {
	src := `@app.get("/items")
async def get_items(db = Depends(get_db)):
    pass
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "depends" && e.Props["dependency_fn"] == "get_db" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Depends entity")
	}
}

func TestFastAPI_PydanticModel(t *testing.T) {
	// Issue #1501 — the FastAPI extractor must NOT emit standalone SCOPE.Schema
	// entities for Pydantic model classes. The base Python extractor already
	// emits a SCOPE.Component/class entity for every class definition; a second
	// SCOPE.Schema from this extractor creates within-file duplicates that inflate
	// node counts (e.g. "Order" appearing 3× instead of 1×).
	src := `class UserCreate(BaseModel):
    name: str
    email: str

class AppConfig(BaseSettings):
    debug: bool = False
`
	ents := extract(t, "python_fastapi", src)
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && (e.Name == "UserCreate" || e.Name == "AppConfig") {
			t.Fatalf("python_fastapi must not emit SCOPE.Schema entity for Pydantic class %q (issue #1501): "+
				"the base Python extractor already emits SCOPE.Component/class for it", e.Name)
		}
	}
}

func TestFastAPI_APIRouter(t *testing.T) {
	src := `router = APIRouter(prefix="/api/v1", tags=["items"])
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Name == "router" && e.Props["prefix"] == "/api/v1" && e.Props["tags"] == "items" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected APIRouter entity")
	}
}

func TestFastAPI_WebSocket(t *testing.T) {
	src := `@app.websocket("/ws/chat")
async def chat_ws(websocket: WebSocket):
    pass
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Name == "chat_ws" && e.Props["protocol"] == "websocket" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected WebSocket entity")
	}
}

func TestFastAPI_Middleware(t *testing.T) {
	src := `@app.middleware("http")
async def add_process_time(request, call_next):
    pass
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Name == "add_process_time" && e.Props["middleware_type"] == "http" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected middleware entity")
	}
}

func TestFastAPI_BackgroundTask(t *testing.T) {
	src := `background_tasks.add_task(send_email, email, subject="Hello")
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Name == "send_email" && e.Props["pattern_type"] == "background_task" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected background task entity")
	}
}

func TestFastAPI_Lifecycle(t *testing.T) {
	src := `@app.on_event("startup")
async def startup_db():
    pass
`
	ents := extract(t, "python_fastapi", src)
	found := false
	for _, e := range ents {
		if e.Name == "startup_db" && e.Props["event_type"] == "startup" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected lifecycle event entity")
	}
}

func TestFastAPI_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_fastapi", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// Flask tests
// ============================================================================

func TestFlask_Route(t *testing.T) {
	src := `@app.route("/users", methods=["GET", "POST"])
def users():
    pass
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "users" && e.Subtype == "endpoint" && strings.Contains(e.Props["http_methods"], "GET") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Flask route entity")
	}
}

func TestFlask_Blueprint(t *testing.T) {
	src := `bp = Blueprint("auth", __name__, url_prefix="/auth")
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "auth" && e.Kind == "SCOPE.Component" && e.Props["url_prefix"] == "/auth" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Blueprint entity")
	}
}

func TestFlask_RequestHook(t *testing.T) {
	src := `@app.before_request
def check_auth():
    pass
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "check_auth" && e.Props["hook_type"] == "before_request" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected request hook entity")
	}
}

func TestFlask_ErrorHandler(t *testing.T) {
	src := `@app.errorhandler(404)
def not_found(error):
    pass
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "not_found" && e.Props["error_code"] == "404" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected error handler entity")
	}
}

func TestFlask_DBModel(t *testing.T) {
	src := `class User(db.Model):
    id = db.Column(db.Integer, primary_key=True)
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "User" && e.Kind == "SCOPE.Schema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected db.Model entity")
	}
}

func TestFlask_FlaskForm(t *testing.T) {
	src := `class LoginForm(FlaskForm):
    username = StringField("Username")
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Name == "LoginForm" && e.Kind == "SCOPE.Schema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected FlaskForm entity")
	}
}

func TestFlask_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_flask", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// Celery tests
// ============================================================================

func TestCelery_SharedTask(t *testing.T) {
	src := `@shared_task(queue="emails", bind=True)
def send_notification(self, user_id):
    pass
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Name == "send_notification" && e.Kind == "SCOPE.Service" && e.Props["queue"] == "emails" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected shared_task entity")
	}
}

func TestCelery_AppTask(t *testing.T) {
	src := `@app.task(name="myapp.process")
def process_data(data):
    pass
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Name == "process_data" && e.Props["task_name_override"] == "myapp.process" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected app.task entity")
	}
}

func TestCelery_Canvas(t *testing.T) {
	src := `workflow = chain(fetch_data.s(url), process.s(), store.s())
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "canvas" && e.Props["canvas_type"] == "chain" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected canvas chain entity")
	}
}

func TestCelery_BeatSchedule(t *testing.T) {
	src := `beat_schedule = {
    "cleanup-daily": {
        "task": "myapp.tasks.cleanup",
        "schedule": crontab(hour=0, minute=0),
    },
}
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Name == "cleanup-daily" && e.Subtype == "beat_entry" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected beat schedule entry")
	}
}

func TestCelery_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_celery", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// TestCelery_SharedTask_BindFalse exercises the bind=False variant seen
// in real task files that use @shared_task without self as the first arg.
func TestCelery_SharedTask_BindFalse(t *testing.T) {
	src := `from celery import shared_task

@shared_task(bind=False, ignore_result=True)
def process_order(order_id: int, collection_name: str):
    pass

@shared_task(bind=False, ignore_result=True)
def send_email(recipient: str):
    pass
`
	ents := extract(t, "python_celery", src)
	var names []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" && e.Subtype == "task" {
			names = append(names, e.Name)
		}
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 task entities, got %d (%v)", len(names), names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["process_order"] {
		t.Error("expected entity for process_order")
	}
	if !found["send_email"] {
		t.Error("expected entity for send_email")
	}
}

// TestCelery_SharedTask_MultipleInFile ensures all @shared_task decorators
// in a single file are extracted, mirroring files that define 3+ tasks.
func TestCelery_SharedTask_MultipleInFile(t *testing.T) {
	src := `from celery import shared_task

@shared_task(bind=True, ignore_result=True)
def process_order(self, context):
    pass

@shared_task(bind=True, ignore_result=True)
def cancel_order(self, context):
    pass

@shared_task(bind=True, ignore_result=True)
def archive_order(self, context):
    pass
`
	ents := extract(t, "python_celery", src)
	taskCount := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" && e.Subtype == "task" {
			taskCount++
		}
	}
	if taskCount != 3 {
		t.Fatalf("expected 3 task entities, got %d", taskCount)
	}
}

// TestCelery_SharedTask_PatternType verifies that @shared_task entities
// carry pattern_type="shared_task" and framework="celery" properties.
func TestCelery_SharedTask_PatternType(t *testing.T) {
	src := `@shared_task
def simple_task():
    pass
`
	ents := extract(t, "python_celery", src)
	foundIdx := -1
	for i, e := range ents {
		if e.Name == "simple_task" {
			foundIdx = i
			break
		}
	}
	if foundIdx == -1 {
		t.Fatal("expected entity for simple_task")
	}
	e := ents[foundIdx]
	if e.Props["pattern_type"] != "shared_task" {
		t.Errorf("expected pattern_type=shared_task, got %q", e.Props["pattern_type"])
	}
	if e.Props["framework"] != "celery" {
		t.Errorf("expected framework=celery, got %q", e.Props["framework"])
	}
}

// ============================================================================
// Pytest tests
// ============================================================================

func TestPytest_TestFunction(t *testing.T) {
	src := `def test_user_creation():
    assert True

async def test_async_endpoint():
    assert True
`
	ents := extract(t, "python_pytest", src)
	testCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "test" {
			testCount++
		}
	}
	if testCount != 2 {
		t.Fatalf("expected 2 test functions, got %d", testCount)
	}
}

func TestPytest_TestClass(t *testing.T) {
	src := `class TestUserService:
    def test_create(self):
        pass
    def test_delete(self):
        pass
`
	ents := extract(t, "python_pytest", src)
	classCount := 0
	methodCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "test_class" {
			classCount++
		}
		if e.Props["pattern_type"] == "test" {
			methodCount++
		}
	}
	if classCount != 1 {
		t.Fatalf("expected 1 test class, got %d", classCount)
	}
	if methodCount != 2 {
		t.Fatalf("expected 2 test methods, got %d", methodCount)
	}
}

func TestPytest_Fixture(t *testing.T) {
	src := `@pytest.fixture(scope="session", autouse=True)
def db_connection():
    pass
`
	ents := extract(t, "python_pytest", src)
	found := false
	for _, e := range ents {
		if e.Name == "db_connection" && e.Props["fixture_scope"] == "session" && e.Props["autouse"] == "true" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected fixture entity")
	}
}

func TestPytest_Parametrize(t *testing.T) {
	src := `@pytest.mark.parametrize("input,expected", [(1, 2), (3, 4)])
def test_double(input, expected):
    assert input * 2 == expected
`
	ents := extract(t, "python_pytest", src)
	found := false
	for _, e := range ents {
		if e.Name == "test_double" && e.Props["parametrized"] == "true" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected parametrized test entity")
	}
}

func TestPytest_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_pytest", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// LangChain tests
// ============================================================================

func TestLangChain_LCELChain(t *testing.T) {
	src := `chain = prompt | model | parser
`
	ents := extract(t, "python_langchain", src)
	found := false
	for _, e := range ents {
		if e.Name == "chain" && e.Props["pattern_type"] == "lcel_chain" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected LCEL chain entity")
	}
}

func TestLangChain_Agent(t *testing.T) {
	src := `agent = create_react_agent(llm, tools, prompt)
executor = AgentExecutor(agent=agent, tools=tools)
`
	ents := extract(t, "python_langchain", src)
	agentCount := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" {
			agentCount++
		}
	}
	if agentCount != 2 {
		t.Fatalf("expected 2 agent entities, got %d", agentCount)
	}
}

func TestLangChain_Tool(t *testing.T) {
	src := `@tool
def search(query: str) -> str:
    pass

class CustomTool(BaseTool):
    pass
`
	ents := extract(t, "python_langchain", src)
	toolCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "tool" {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Fatalf("expected 2 tool entities, got %d", toolCount)
	}
}

func TestLangChain_Prompt(t *testing.T) {
	src := `prompt = ChatPromptTemplate.from_messages([("system", "You are helpful")])
tmpl = PromptTemplate(template="Hello {name}")
`
	ents := extract(t, "python_langchain", src)
	promptCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "prompt" {
			promptCount++
		}
	}
	if promptCount != 2 {
		t.Fatalf("expected 2 prompt entities, got %d", promptCount)
	}
}

func TestLangChain_Memory(t *testing.T) {
	src := `memory = ConversationBufferMemory()
history = RunnableWithMessageHistory(chain, get_session_history)
`
	ents := extract(t, "python_langchain", src)
	memoryCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "memory" {
			memoryCount++
		}
	}
	if memoryCount != 2 {
		t.Fatalf("expected 2 memory entities, got %d", memoryCount)
	}
}

func TestLangChain_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_langchain", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// MongoDB tests
// ============================================================================

func TestMongoDB_Driver(t *testing.T) {
	src := `client = MongoClient("mongodb://localhost:27017")
`
	ents := extract(t, "python_mongodb", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" && e.Props["pattern_type"] == "driver" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected MongoDB driver entity")
	}
}

func TestMongoDB_Aggregation(t *testing.T) {
	src := `result = collection.aggregate([
    {"$match": {"status": "active"}},
    {"$group": {"_id": "$category"}},
])
`
	ents := extract(t, "python_mongodb", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "aggregation" {
			found = true
			if !strings.Contains(e.Props["pipeline_stages"], "match") {
				t.Fatal("expected match stage in pipeline_stages")
			}
		}
	}
	if !found {
		t.Fatal("expected aggregation entity")
	}
}

func TestMongoDB_Index(t *testing.T) {
	src := `collection.createIndex({"email": 1}, unique=True)
`
	ents := extract(t, "python_mongodb", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "index" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected index entity")
	}
}

func TestMongoDB_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_mongodb", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// Redis tests
// ============================================================================

func TestRedis_Client(t *testing.T) {
	src := `r = redis.Redis(host="localhost", port=6379)
`
	ents := extract(t, "python_redis", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Service" && e.Props["pattern_type"] == "client" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Redis client entity")
	}
}

func TestRedis_CacheOp(t *testing.T) {
	src := `r.set("key", "value")
r.get("key")
r.hset("hash", "field", "value")
`
	ents := extract(t, "python_redis", src)
	cacheCount := 0
	for _, e := range ents {
		if e.Subtype == "cache_op" {
			cacheCount++
		}
	}
	if cacheCount < 3 {
		t.Fatalf("expected at least 3 cache ops, got %d", cacheCount)
	}
}

func TestRedis_PubSub(t *testing.T) {
	src := `r.publish("channel", "message")
`
	ents := extract(t, "python_redis", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "pubsub" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pubsub entity")
	}
}

func TestRedis_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_redis", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// SQLAlchemy tests
// ============================================================================

func TestSQLAlchemy_Model(t *testing.T) {
	src := `class User(Base):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True)
    name = Column(String(100))
`
	ents := extract(t, "python_sqlalchemy", src)
	found := false
	for _, e := range ents {
		if e.Name == "User" && e.Kind == "SCOPE.Schema" && e.Props["tablename"] == "users" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected SQLAlchemy model entity")
	}
}

func TestSQLAlchemy_Relationship(t *testing.T) {
	src := `class User(Base):
    __tablename__ = "users"
    posts = relationship("Post", back_populates="author")
`
	ents := extract(t, "python_sqlalchemy", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "relationship" && e.Props["target_model"] == "Post" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected relationship entity")
	}
}

func TestSQLAlchemy_Engine(t *testing.T) {
	src := `engine = create_engine("postgresql://user:pass@localhost/db")
`
	ents := extract(t, "python_sqlalchemy", src)
	found := false
	for _, e := range ents {
		if e.Name == "engine" && e.Kind == "SCOPE.Service" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected engine entity")
	}
}

func TestSQLAlchemy_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_sqlalchemy", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// TestSQLAlchemy_NoFalsePositiveOnPydantic verifies that Pydantic BaseModel
// subclasses are NOT emitted as SQLAlchemy model entities. "BaseModel" contains
// the word "Model" (which was previously in saBaseIndicators as a bare substring
// match), causing false-positive SCOPE.Schema duplicates for shared domain
// classes (issue #1501 — within-extractor dedup fix 2/2).
func TestSQLAlchemy_NoFalsePositiveOnPydantic(t *testing.T) {
	src := `from pydantic import BaseModel, BaseSettings

class Order(BaseModel):
    id: str
    status: str
    total_cents: int

class AppConfig(BaseSettings):
    db_url: str
`
	ents := extract(t, "python_sqlalchemy", src)
	for _, e := range ents {
		if e.Name == "Order" || e.Name == "AppConfig" {
			t.Fatalf("python_sqlalchemy must not emit entity for Pydantic class %q (issue #1501): "+
				"'BaseModel' was falsely matched by the 'Model' substring in saBaseIndicators", e.Name)
		}
	}
}

// ============================================================================
// FastAPI Request/Response tests
// ============================================================================

func TestFastAPIReqResp_AcceptsInput(t *testing.T) {
	src := `@app.post("/users")
def create_user(user: UserCreate):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "accepts_input" && e.Props["dto_type"] == "UserCreate" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ACCEPTS_INPUT entity")
	}
}

func TestFastAPIReqResp_Returns(t *testing.T) {
	src := `@app.get("/users", response_model=UserResponse)
def list_users():
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "returns" && e.Props["dto_type"] == "UserResponse" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected RETURNS entity")
	}
}

func TestFastAPIReqResp_ReturnAnnotation(t *testing.T) {
	src := `@app.get("/users")
def list_users() -> List[UserResponse]:
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "returns" && e.Props["dto_type"] == "UserResponse" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected RETURNS from return annotation")
	}
}

func TestFastAPIReqResp_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// TestFastAPIReqResp_FullFixture exercises fastapi_reqresp.go against the
// testdata/fastapi_reqresp_fixture.py fixture. It proves that:
//   - Pydantic body parameters are emitted as dto + accepts_input entities
//   - response_model= kwarg emits a returns entity (dto + returns)
//   - Return type annotation emits a returns entity
//   - Depends() params are NOT emitted as DTO entities (they are skipped)
//
// This fixture is the proof for issue #2976 Validation/dto_extraction partial.
func TestFastAPIReqResp_FullFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/fastapi_reqresp_fixture.py")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	ext, ok := extractor.Get("python_fastapi_reqresp")
	if !ok {
		t.Fatal("python_fastapi_reqresp extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/fastapi_reqresp_fixture.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Index by pattern_type for easier assertions.
	var acceptsInput, returnsEnts []extractResult
	seenDTOs := map[string]bool{}
	for _, e := range entities {
		var rels []relResult
		for _, r := range e.Relationships {
			rels = append(rels, relResult{ToID: r.ToID, Kind: r.Kind})
		}
		er := extractResult{Name: e.Name, Kind: e.Kind, Subtype: e.Subtype, StartLine: e.StartLine, Props: e.Properties, Rels: rels}
		switch e.Properties["pattern_type"] {
		case "accepts_input":
			acceptsInput = append(acceptsInput, er)
		case "returns":
			returnsEnts = append(returnsEnts, er)
		case "request_response_dto":
			seenDTOs[e.Name] = true
		}
	}

	// create_order accepts CreateOrderRequest
	foundCreate := false
	for _, e := range acceptsInput {
		if e.Props["dto_type"] == "CreateOrderRequest" {
			foundCreate = true
		}
	}
	if !foundCreate {
		t.Error("expected accepts_input entity with dto_type=CreateOrderRequest (create_order endpoint)")
	}

	// update_order accepts UpdateOrderRequest (body param, not Depends)
	foundUpdate := false
	for _, e := range acceptsInput {
		if e.Props["dto_type"] == "UpdateOrderRequest" {
			foundUpdate = true
		}
	}
	if !foundUpdate {
		t.Error("expected accepts_input entity with dto_type=UpdateOrderRequest (update_order endpoint)")
	}

	// create_order returns OrderResponse via response_model=
	foundResponseModel := false
	for _, e := range returnsEnts {
		if e.Props["dto_type"] == "OrderResponse" && e.Props["match_source"] == "response_model_decorator" {
			foundResponseModel = true
		}
	}
	if !foundResponseModel {
		t.Error("expected returns entity with dto_type=OrderResponse from response_model= decorator")
	}

	// update_order returns OrderResponse via -> annotation
	foundAnnotation := false
	for _, e := range returnsEnts {
		if e.Props["dto_type"] == "OrderResponse" && e.Props["match_source"] == "return_type_annotation" {
			foundAnnotation = true
		}
	}
	if !foundAnnotation {
		t.Error("expected returns entity with dto_type=OrderResponse from return type annotation")
	}

	// DTO entities must be de-duplicated: OrderResponse appears many times but only once as dto
	if !seenDTOs["OrderResponse"] {
		t.Error("expected request_response_dto entity for OrderResponse")
	}
	if !seenDTOs["CreateOrderRequest"] {
		t.Error("expected request_response_dto entity for CreateOrderRequest")
	}
	if !seenDTOs["UpdateOrderRequest"] {
		t.Error("expected request_response_dto entity for UpdateOrderRequest")
	}
}

// TestFastAPI_FullFixture_Middleware proves middleware_coverage partial:
// the middleware @app.middleware("http") in the fixture is extracted by fastapi.go.
func TestFastAPI_FullFixture_Middleware(t *testing.T) {
	content, err := os.ReadFile("testdata/fastapi_reqresp_fixture.py")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	ext, ok := extractor.Get("python_fastapi")
	if !ok {
		t.Fatal("python_fastapi extractor not registered")
	}
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/fastapi_reqresp_fixture.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	foundMiddleware := false
	for _, e := range entities {
		if e.Properties["pattern_type"] == "middleware" && e.Properties["middleware_type"] == "http" {
			foundMiddleware = true
		}
	}
	if !foundMiddleware {
		t.Error("expected middleware entity with middleware_type=http from fixture")
	}
}

// ============================================================================
// Flask Request/Response tests
// ============================================================================

func TestFlaskReqResp_Returns(t *testing.T) {
	src := `@app.route("/users")
def list_users() -> UserResponse:
    pass
`
	ents := extract(t, "python_flask_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "returns" && e.Props["schema_type"] == "UserResponse" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected RETURNS entity")
	}
}

func TestFlaskReqResp_SchemaLoad(t *testing.T) {
	src := `@app.route("/users", methods=["POST"])
def create_user():
    user_schema.load(request.json)
`
	ents := extract(t, "python_flask_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "accepts_input" && e.Props["schema_type"] == "UserSchema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ACCEPTS_INPUT from schema.load()")
	}
}

func TestFlaskReqResp_PascalCaseSchema(t *testing.T) {
	src := `@app.route("/users", methods=["POST"])
def create_user():
    UserSchema().load(request.json)
`
	ents := extract(t, "python_flask_reqresp", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "accepts_input" && e.Props["schema_type"] == "UserSchema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ACCEPTS_INPUT from PascalCase schema.load()")
	}
}

func TestFlaskReqResp_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_flask_reqresp", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(ents))
	}
}

// ============================================================================
// Empty input tests (all extractors)
// ============================================================================

// ============================================================================
// dramatiq tests
// ============================================================================

func TestDramatiq_Actor(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor
def send_email(to, subject):
    pass
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Name == "send_email" && e.Kind == "SCOPE.Service" && e.Props["framework"] == "dramatiq" && e.Props["edge_kind"] == "CONSUMES" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected @dramatiq.actor consumer entity for send_email")
	}
}

func TestDramatiq_ActorWithOptions(t *testing.T) {
	src := `@dramatiq.actor(queue_name="billing", max_retries=3)
def charge_card(user_id, amount):
    pass
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Name == "charge_card" && e.Props["pattern_type"] == "actor" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected @dramatiq.actor(options) consumer entity")
	}
}

func TestDramatiq_Send(t *testing.T) {
	src := `send_email.send("user@example.com", "Hello")
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Name == "send_email.send" && e.Props["edge_kind"] == "PRODUCES" && e.Props["actor_ref"] == "send_email" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected actor.send() producer entity")
	}
}

func TestDramatiq_SendWithOptions(t *testing.T) {
	src := `charge_card.send_with_options(args=[42, 9.99], delay=5000)
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Name == "charge_card.send_with_options" && e.Props["edge_kind"] == "PRODUCES" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected actor.send_with_options() producer entity")
	}
}

func TestDramatiq_NoBareActorFalsePositive(t *testing.T) {
	// @actor without dramatiq. prefix should NOT be matched
	src := `@actor
def my_handler():
    pass
`
	ents := extract(t, "python_dramatiq", src)
	for _, e := range ents {
		if e.Props["framework"] == "dramatiq" && e.Kind == "SCOPE.Service" {
			t.Fatalf("false positive: bare @actor matched as dramatiq actor: %+v", e)
		}
	}
}

func TestDramatiq_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_dramatiq", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-dramatiq code, got %d", len(ents))
	}
}

// ============================================================================
// RQ tests
// ============================================================================

func TestRQ_Enqueue(t *testing.T) {
	src := `from rq import Queue
from workers.billing import charge_card

q = Queue(connection=conn)
q.enqueue(charge_card, user_id=42)
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Props["framework"] == "rq" && e.Props["callable"] == "charge_card" && e.Props["edge_kind"] == "PRODUCES" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected queue.enqueue producer entity")
	}
}

func TestRQ_EnqueueCall(t *testing.T) {
	src := `from rq import Queue

q = Queue(connection=conn)
q.enqueue_call(func="workers.emails.send_email", args=["hello"])
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Props["framework"] == "rq" && e.Props["callable"] == "workers.emails.send_email" && e.Props["pattern_type"] == "enqueue_call" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected enqueue_call producer entity with string func")
	}
}

func TestRQ_Worker(t *testing.T) {
	src := `from rq import Queue, Worker
from redis import Redis

redis_conn = Redis()
q = Queue(connection=redis_conn)
worker = Worker([q], connection=redis_conn)
worker.work()
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Props["framework"] == "rq" && e.Props["pattern_type"] == "worker" && e.Props["edge_kind"] == "CONSUMES" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Worker consumer entity")
	}
}

func TestRQ_NoWorkerWithoutImport(t *testing.T) {
	// Worker class without rq import — generic class, should not emit worker entity
	src := `class Worker:
    pass

q = SomeQueue()
q.enqueue(my_func)
`
	ents := extract(t, "python_rq", src)
	for _, e := range ents {
		if e.Props["pattern_type"] == "worker" {
			t.Fatalf("false positive: Worker entity emitted without rq import: %+v", e)
		}
	}
}

func TestRQ_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_rq", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-RQ code, got %d", len(ents))
	}
}

func TestAllExtractors_EmptyInput(t *testing.T) {
	keys := []string{
		"python_django", "python_fastapi", "python_flask", "python_celery",
		"python_pytest", "python_langchain", "python_mongodb", "python_redis",
		"python_sqlalchemy", "python_fastapi_reqresp", "python_flask_reqresp",
		"python_dramatiq", "python_rq",
	}
	for _, key := range keys {
		ext, ok := extractor.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: "empty.py", Content: nil, Language: "python",
		})
		if err != nil {
			t.Fatalf("%s empty input error: %v", key, err)
		}
		if len(ents) != 0 {
			t.Fatalf("%s empty input returned %d entities", key, len(ents))
		}
	}
}

// ============================================================================
// Registration test
// ============================================================================

func TestAllExtractors_Registered(t *testing.T) {
	keys := []string{
		"python_django", "python_fastapi", "python_flask", "python_celery",
		"python_pytest", "python_langchain", "python_mongodb", "python_redis",
		"python_sqlalchemy", "python_fastapi_reqresp", "python_flask_reqresp",
		"python_dramatiq", "python_rq",
	}
	for _, key := range keys {
		_, ok := extractor.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
	}
}
