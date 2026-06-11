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

// #4474 — a DRF ViewSet with serializer_class gets ACCEPTS_INPUT + RETURNS
// edges to the serializer, and the serializer entity is not duplicated.
func TestDRF_ViewSetSerializerClassEdges(t *testing.T) {
	src := `from rest_framework import viewsets, serializers

class OrderSerializer(serializers.ModelSerializer):
    class Meta:
        model = Order
        fields = ["id", "sku"]

class OrderViewSet(viewsets.ModelViewSet):
    queryset = Order.objects.all()
    serializer_class = OrderSerializer
`
	ents := extract(t, "python_django", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:OrderSerializer") {
		t.Error("expected ACCEPTS_INPUT -> Class:OrderSerializer from OrderViewSet")
	}
	if !hasEntRel(ents, "RETURNS", "Class:OrderSerializer") {
		t.Error("expected RETURNS -> Class:OrderSerializer from OrderViewSet")
	}
	// No duplicate serializer node, and exactly one viewset node.
	var serCount, viewCount int
	for _, e := range ents {
		switch e.Name {
		case "OrderSerializer":
			serCount++
		case "OrderViewSet":
			viewCount++
		}
	}
	if serCount != 1 {
		t.Errorf("expected exactly 1 OrderSerializer node, got %d", serCount)
	}
	if viewCount != 1 {
		t.Errorf("expected exactly 1 OrderViewSet node, got %d", viewCount)
	}
}

// #4474 — a DRF APIView with inline serializer calls: `XSerializer(data=...)`
// → ACCEPTS_INPUT, `XSerializer(obj)` → RETURNS.
func TestDRF_APIViewInlineSerializerCalls(t *testing.T) {
	src := `from rest_framework.views import APIView
from rest_framework.response import Response

class OrderCreateView(APIView):
    def post(self, request):
        ser = OrderInputSerializer(data=request.data)
        ser.is_valid(raise_exception=True)
        obj = ser.save()
        return Response(OrderOutputSerializer(obj).data)
`
	ents := extract(t, "python_django", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:OrderInputSerializer") {
		t.Error("expected ACCEPTS_INPUT -> Class:OrderInputSerializer (data= call)")
	}
	if !hasEntRel(ents, "RETURNS", "Class:OrderOutputSerializer") {
		t.Error("expected RETURNS -> Class:OrderOutputSerializer (positional call)")
	}
}

// #4474 — drf-yasg @swagger_auto_schema(request_body=) → ACCEPTS_INPUT.
func TestDRF_SwaggerRequestBodyEdge(t *testing.T) {
	src := `from rest_framework.views import APIView
from drf_yasg.utils import swagger_auto_schema

class OrderView(APIView):
    @swagger_auto_schema(request_body=OrderRequestSerializer)
    def post(self, request):
        pass
`
	ents := extract(t, "python_django", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:OrderRequestSerializer") {
		t.Error("expected ACCEPTS_INPUT -> Class:OrderRequestSerializer from swagger_auto_schema")
	}
}

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

// pytestSuite returns the single collapsed test_suite entity emitted by the
// pytest/unittest extractor for a file (issue #4357), or fails.
func pytestSuite(t *testing.T, ents []extractResult) extractResult {
	t.Helper()
	var suites []extractResult
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "test_suite" {
			suites = append(suites, e)
		}
	}
	if len(suites) != 1 {
		t.Fatalf("expected exactly 1 test_suite entity, got %d", len(suites))
	}
	return suites[0]
}

// Issue #4357: the per-test/per-class/per-fixture nodes are collapsed into one
// test_suite entity per file with counts folded into properties.
func TestPytest_TestFunction(t *testing.T) {
	src := `def test_user_creation():
    assert True

async def test_async_endpoint():
    assert True
`
	ents := extract(t, "python_pytest", src)
	suite := pytestSuite(t, ents)
	if suite.Props["test_func_count"] != "2" {
		t.Fatalf("expected test_func_count=2, got %q", suite.Props["test_func_count"])
	}
	if suite.Props["toplevel_func_count"] != "2" {
		t.Fatalf("expected toplevel_func_count=2, got %q", suite.Props["toplevel_func_count"])
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
	suite := pytestSuite(t, ents)
	if suite.Props["test_class_count"] != "1" {
		t.Fatalf("expected test_class_count=1, got %q", suite.Props["test_class_count"])
	}
	if suite.Props["test_method_count"] != "2" {
		t.Fatalf("expected test_method_count=2, got %q", suite.Props["test_method_count"])
	}
}

func TestPytest_Fixture(t *testing.T) {
	src := `@pytest.fixture(scope="session", autouse=True)
def db_connection():
    pass

def test_uses_db(db_connection):
    assert True
`
	ents := extract(t, "python_pytest", src)
	suite := pytestSuite(t, ents)
	if suite.Props["fixture_count"] != "1" {
		t.Fatalf("expected fixture_count=1, got %q", suite.Props["fixture_count"])
	}
	// The fixture is no longer a standalone orphan node.
	for _, e := range ents {
		if e.Name == "db_connection" {
			t.Fatalf("fixture should not be a standalone entity, got %+v", e)
		}
	}
}

func TestPytest_Parametrize(t *testing.T) {
	src := `@pytest.mark.parametrize("input,expected", [(1, 2), (3, 4)])
def test_double(input, expected):
    assert input * 2 == expected
`
	ents := extract(t, "python_pytest", src)
	suite := pytestSuite(t, ents)
	if suite.Props["parametrize_count"] != "1" {
		t.Fatalf("expected parametrize_count=1, got %q", suite.Props["parametrize_count"])
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

// findCacheOpAtLine returns the cache_op entity on a given source line.
func findCacheOpAtLine(t *testing.T, ents []extractResult, line int) extractResult {
	t.Helper()
	for _, e := range ents {
		if e.Subtype == "cache_op" && e.StartLine == line {
			return e
		}
	}
	t.Fatalf("no cache_op found on line %d", line)
	return extractResult{}
}

func hasRel(rels []relResult, kind, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

func findKeyspace(ents []extractResult, label string) (extractResult, bool) {
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" && e.Props["keyspace"] == label {
			return e, true
		}
	}
	return extractResult{}, false
}

// TestRedis_CacheOp_LiteralKey asserts the SPECIFIC key `session:abc` is
// captured as a read access target with a READS_FROM edge.
func TestRedis_CacheOp_LiteralKey(t *testing.T) {
	src := `r.get("session:abc")
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key"] != "session:abc" {
		t.Fatalf("expected key=session:abc, got key=%q", op.Props["key"])
	}
	if !hasRel(op.Rels, "READS_FROM", "Datastore:redis:session:abc") {
		t.Fatalf("expected READS_FROM edge to Datastore:redis:session:abc, got %+v", op.Rels)
	}
	ks, ok := findKeyspace(ents, "session:abc")
	if !ok {
		t.Fatal("expected keyspace entity for session:abc")
	}
	if ks.Props["key_type"] != "key" {
		t.Fatalf("expected key_type=key, got %q", ks.Props["key_type"])
	}
}

// TestRedis_CacheOp_SetWritesTo asserts a write verb emits WRITES_TO.
func TestRedis_CacheOp_SetWritesTo(t *testing.T) {
	src := `r.set("user:42", value)
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key"] != "user:42" {
		t.Fatalf("expected key=user:42, got %q", op.Props["key"])
	}
	if !hasRel(op.Rels, "WRITES_TO", "Datastore:redis:user:42") {
		t.Fatalf("expected WRITES_TO edge to Datastore:redis:user:42, got %+v", op.Rels)
	}
}

// TestRedis_CacheOp_ConcatPrefix asserts `r.set("user:" + id, v)` yields a
// key-prefix `user:*`, not a fabricated concrete key.
func TestRedis_CacheOp_ConcatPrefix(t *testing.T) {
	src := `r.set("user:" + id, v)
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key_prefix"] != "user:*" {
		t.Fatalf("expected key_prefix=user:*, got %q", op.Props["key_prefix"])
	}
	if op.Props["key"] != "" {
		t.Fatalf("expected no concrete key, got key=%q", op.Props["key"])
	}
	if !hasRel(op.Rels, "WRITES_TO", "Datastore:redis:user:*") {
		t.Fatalf("expected WRITES_TO edge to Datastore:redis:user:*, got %+v", op.Rels)
	}
}

// TestRedis_CacheOp_FStringPrefix asserts `r.get(f"session:{sid}")` yields a
// key-prefix `session:*`.
func TestRedis_CacheOp_FStringPrefix(t *testing.T) {
	src := `r.get(f"session:{sid}")
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key_prefix"] != "session:*" {
		t.Fatalf("expected key_prefix=session:*, got %q", op.Props["key_prefix"])
	}
	if !hasRel(op.Rels, "READS_FROM", "Datastore:redis:session:*") {
		t.Fatalf("expected READS_FROM edge to Datastore:redis:session:*, got %+v", op.Rels)
	}
}

// TestRedis_CacheOp_DynamicKey is the negative case: a fully-dynamic key
// (variable only) must emit the op with NO key edge and NO fabricated key.
func TestRedis_CacheOp_DynamicKey(t *testing.T) {
	src := `r.get(k)
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key"] != "" {
		t.Fatalf("expected no concrete key for dynamic key, got %q", op.Props["key"])
	}
	if op.Props["key_prefix"] != "" {
		t.Fatalf("expected no key_prefix for dynamic key, got %q", op.Props["key_prefix"])
	}
	if len(op.Rels) != 0 {
		t.Fatalf("expected no access edge for fully-dynamic key, got %+v", op.Rels)
	}
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" {
			t.Fatalf("expected no keyspace target for dynamic key, got %q", e.Props["keyspace"])
		}
	}
}

// TestRedis_CacheOp_FStringNoStaticHead asserts an f-string whose first token
// is an interpolation (`f"{tenant}:cfg"`) is marked dynamic with no edge and
// no fabricated keyspace — the key arg matched but could not be resolved.
func TestRedis_CacheOp_FStringNoStaticHead(t *testing.T) {
	src := `r.get(f"{tenant}:cfg")
`
	ents := extract(t, "python_redis", src)
	op := findCacheOpAtLine(t, ents, 1)
	if op.Props["key"] != "<dynamic>" {
		t.Fatalf("expected key=<dynamic>, got %q", op.Props["key"])
	}
	if len(op.Rels) != 0 {
		t.Fatalf("expected no edge for unresolved f-string, got %+v", op.Rels)
	}
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" {
			t.Fatalf("expected no keyspace node, got %q", e.Props["keyspace"])
		}
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

// TestRedis_PubSub_Channel asserts `r.publish("events", m)` captures channel
// `events` with a PUBLISHES_TO edge.
func TestRedis_PubSub_Channel(t *testing.T) {
	src := `r.publish("events", m)
`
	ents := extract(t, "python_redis", src)
	var op extractResult
	for _, e := range ents {
		if e.Subtype == "pubsub" && e.StartLine == 1 {
			op = e
		}
	}
	if op.Props["channel"] != "events" {
		t.Fatalf("expected channel=events, got %q", op.Props["channel"])
	}
	if !hasRel(op.Rels, "PUBLISHES_TO", "Datastore:redis:events") {
		t.Fatalf("expected PUBLISHES_TO edge to Datastore:redis:events, got %+v", op.Rels)
	}
	ks, ok := findKeyspace(ents, "events")
	if !ok || ks.Props["key_type"] != "channel" {
		t.Fatalf("expected channel keyspace events, got %+v", ks)
	}
}

// TestRedis_PubSub_Subscribe asserts subscribe emits SUBSCRIBES_TO.
func TestRedis_PubSub_Subscribe(t *testing.T) {
	src := `pubsub.subscribe("notifications")
`
	ents := extract(t, "python_redis", src)
	var op extractResult
	for _, e := range ents {
		if e.Subtype == "pubsub" && e.StartLine == 1 {
			op = e
		}
	}
	if op.Props["channel"] != "notifications" {
		t.Fatalf("expected channel=notifications, got %q", op.Props["channel"])
	}
	if !hasRel(op.Rels, "SUBSCRIBES_TO", "Datastore:redis:notifications") {
		t.Fatalf("expected SUBSCRIBES_TO edge, got %+v", op.Rels)
	}
}

// TestRedis_Stream_Key asserts `r.xadd("orders", ...)` captures stream
// `orders` with a WRITES_TO edge.
func TestRedis_Stream_Key(t *testing.T) {
	src := `r.xadd("orders", {"id": 1})
`
	ents := extract(t, "python_redis", src)
	var op extractResult
	for _, e := range ents {
		if e.Subtype == "stream_op" && e.StartLine == 1 {
			op = e
		}
	}
	if op.Props["stream"] != "orders" {
		t.Fatalf("expected stream=orders, got %q", op.Props["stream"])
	}
	if !hasRel(op.Rels, "WRITES_TO", "Datastore:redis:orders") {
		t.Fatalf("expected WRITES_TO edge to Datastore:redis:orders, got %+v", op.Rels)
	}
}

// TestRedis_Keyspace_Dedup asserts two ops on the same key converge on a
// single keyspace target node.
func TestRedis_Keyspace_Dedup(t *testing.T) {
	src := `r.set("cart:1", v)
r.get("cart:1")
`
	ents := extract(t, "python_redis", src)
	count := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Datastore" && e.Props["keyspace"] == "cart:1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 keyspace node for cart:1, got %d", count)
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

// TestSQLAlchemy_LazyLoadingRecognition verifies that a relationship() with a
// lazy= kwarg is detected and the strategy is recorded on the entity.
// Issue #2986 — lazy_loading_recognition partial for SQLAlchemy.
func TestSQLAlchemy_LazyLoadingRecognition(t *testing.T) {
	src := `class User(Base):
    __tablename__ = "users"
    posts = relationship("Post", back_populates="author", lazy="dynamic")
    orders = relationship("Order", lazy='select')
`
	ents := extract(t, "python_sqlalchemy", src)
	var dynamicEnt, selectEnt *extractResult
	for i := range ents {
		e := &ents[i]
		if e.Props["pattern_type"] != "relationship" {
			continue
		}
		switch e.Props["target_model"] {
		case "Post":
			dynamicEnt = e
		case "Order":
			selectEnt = e
		}
	}
	if dynamicEnt == nil {
		t.Fatal("expected relationship entity for Post")
	}
	if dynamicEnt.Props["lazy_strategy"] != "dynamic" {
		t.Errorf("expected lazy_strategy=dynamic, got %q", dynamicEnt.Props["lazy_strategy"])
	}
	if selectEnt == nil {
		t.Fatal("expected relationship entity for Order")
	}
	if selectEnt.Props["lazy_strategy"] != "select" {
		t.Errorf("expected lazy_strategy=select, got %q", selectEnt.Props["lazy_strategy"])
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

// TestSQLModel_TableClass verifies that a SQLModel table=True class is
// extracted as a SCOPE.Schema ORM entity with framework=sqlmodel.
// Issue #2990 — schema_extraction partial promotion for SQLModel.
func TestSQLModel_TableClass(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field

class Hero(SQLModel, table=True):
    id: int = Field(default=None, primary_key=True)
    name: str
    age: int
`
	ents := extract(t, "python_sqlalchemy", src)
	var found *extractResult
	for i := range ents {
		if ents[i].Name == "Hero" && ents[i].Kind == "SCOPE.Schema" {
			found = &ents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected SQLModel table class entity for Hero")
	}
	if found.Props["framework"] != "sqlmodel" {
		t.Errorf("expected framework=sqlmodel, got %q", found.Props["framework"])
	}
}

// TestSQLModel_SchemaOnlyClass verifies that a SQLModel schema-only class
// (no table=True) is NOT emitted as a DB-table entity.
// Issue #2990 — schema-only classes must not be false-positive DB model entities.
func TestSQLModel_SchemaOnlyClass(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field

class HeroCreate(SQLModel):
    name: str
    age: int
`
	ents := extract(t, "python_sqlalchemy", src)
	for _, e := range ents {
		if e.Name == "HeroCreate" {
			t.Fatalf("python_sqlalchemy must not emit entity for SQLModel schema-only class %q", e.Name)
		}
	}
}

// TestSQLModel_RelationshipExtraction verifies that relationship() calls inside
// SQLModel table=True classes are extracted as relationship entities.
// Issue #3056 — SQLModel Relationships:relationship_extraction recording-win.
func TestSQLModel_RelationshipExtraction(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field
from sqlalchemy.orm import relationship
from typing import Optional, List

class Team(SQLModel, table=True):
    __tablename__ = "team"
    id: int = Field(default=None, primary_key=True)
    name: str
    heroes = relationship("Hero", back_populates="team")

class Hero(SQLModel, table=True):
    __tablename__ = "hero"
    id: int = Field(default=None, primary_key=True)
    name: str
    team = relationship("Team", back_populates="heroes")
`
	ents := extract(t, "python_sqlalchemy", src)
	var found []extractResult
	for _, e := range ents {
		if e.Props["pattern_type"] == "relationship" {
			found = append(found, e)
		}
	}
	if len(found) < 2 {
		t.Fatalf("expected at least 2 relationship entities for SQLModel table classes, got %d", len(found))
	}
	for _, e := range found {
		if e.Props["framework"] != "sqlalchemy" {
			t.Errorf("relationship entity %q: expected framework=sqlalchemy, got %q", e.Name, e.Props["framework"])
		}
	}
}

// TestSQLModel_ForeignKeyExtraction verifies that ForeignKey() calls inside
// SQLModel table=True class bodies are extracted as foreign_key entities.
// Issue #3056 — SQLModel Relationships:foreign_key_extraction recording-win.
func TestSQLModel_ForeignKeyExtraction(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field
from sqlalchemy import ForeignKey

class Hero(SQLModel, table=True):
    __tablename__ = "hero"
    id: int = Field(default=None, primary_key=True)
    team_id: int = Field(foreign_key=ForeignKey("team.id"))
    city_id: int = Field(foreign_key=ForeignKey("city.id"))
`
	ents := extract(t, "python_sqlalchemy", src)
	fkCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "foreign_key" {
			fkCount++
		}
	}
	if fkCount < 2 {
		t.Fatalf("expected at least 2 foreign_key entities for SQLModel table class, got %d", fkCount)
	}
}

// TestSQLModel_LazyLoadingRecognition verifies that lazy= kwarg on relationship()
// inside a SQLModel table=True class is detected and recorded.
// Issue #3056 — SQLModel Relationships:lazy_loading_recognition recording-win.
func TestSQLModel_LazyLoadingRecognition(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field
from sqlalchemy.orm import relationship
from typing import Optional, List

class Team(SQLModel, table=True):
    __tablename__ = "team"
    id: int = Field(default=None, primary_key=True)
    name: str
    heroes_dynamic = relationship("Hero", back_populates="team", lazy="dynamic")
    heroes_select = relationship("Member", lazy='select')
`
	ents := extract(t, "python_sqlalchemy", src)
	var dynamicEnt, selectEnt *extractResult
	for i := range ents {
		e := &ents[i]
		if e.Props["pattern_type"] != "relationship" {
			continue
		}
		switch e.Props["target_model"] {
		case "Hero":
			dynamicEnt = e
		case "Member":
			selectEnt = e
		}
	}
	if dynamicEnt == nil {
		t.Fatal("expected relationship entity for Hero (lazy=dynamic) in SQLModel table class")
	}
	if dynamicEnt.Props["lazy_strategy"] != "dynamic" {
		t.Errorf("expected lazy_strategy=dynamic, got %q", dynamicEnt.Props["lazy_strategy"])
	}
	if selectEnt == nil {
		t.Fatal("expected relationship entity for Member (lazy=select) in SQLModel table class")
	}
	if selectEnt.Props["lazy_strategy"] != "select" {
		t.Errorf("expected lazy_strategy=select, got %q", selectEnt.Props["lazy_strategy"])
	}
}

// TestSQLModel_AssociationExtraction verifies that association tables (Table())
// used alongside SQLModel table=True classes are extracted as association_table
// entities. Issue #3056 — SQLModel Relationships:association_extraction recording-win.
func TestSQLModel_AssociationExtraction(t *testing.T) {
	src := `from sqlmodel import SQLModel, Field
from sqlalchemy import Table, Column, ForeignKey, MetaData

metadata = MetaData()

hero_sidekick = Table("hero_sidekick", metadata,
    Column("hero_id", ForeignKey("hero.id"), primary_key=True),
    Column("sidekick_id", ForeignKey("hero.id"), primary_key=True),
)

class Hero(SQLModel, table=True):
    __tablename__ = "hero"
    id: int = Field(default=None, primary_key=True)
    name: str
`
	ents := extract(t, "python_sqlalchemy", src)
	var found *extractResult
	for i := range ents {
		if ents[i].Props["pattern_type"] == "association_table" && ents[i].Props["tablename"] == "hero_sidekick" {
			found = &ents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected association_table entity for hero_sidekick in SQLModel file")
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

// hasEntRel reports whether any entity carries an edge of (kind -> toID). #3629.
func hasEntRel(ents []extractResult, kind, toID string) bool {
	for _, e := range ents {
		for _, r := range e.Rels {
			if r.Kind == kind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// #3629: FastAPI body param emits an ACCEPTS_INPUT edge to the request DTO.
func TestFastAPIReqResp_AcceptsInputEdge(t *testing.T) {
	src := `@app.post("/users")
def create_user(user: UserCreate):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:UserCreate") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:UserCreate edge from create_user")
	}
}

// #4476: a Pydantic model injected via Depends() as a query model gets an
// ACCEPTS_INPUT edge (the FastAPI analog of the NestJS @Query() DTO).
func TestFastAPIReqResp_DependsQueryModelEdge(t *testing.T) {
	src := `@app.get("/items", response_model=ItemOut)
def list_items(q: FilterParams = Depends()):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:FilterParams") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:FilterParams from Depends() query model")
	}
	if !hasEntRel(ents, "RETURNS", "Class:ItemOut") {
		t.Fatal("expected RETURNS -> Class:ItemOut")
	}
}

// #4476: `Depends(FilterParams)` (explicit model arg) gets ACCEPTS_INPUT.
func TestFastAPIReqResp_DependsExplicitModelEdge(t *testing.T) {
	src := `@app.get("/items")
def list_items(q = Depends(FilterParams)):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:FilterParams") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:FilterParams from Depends(FilterParams)")
	}
}

// #4476: an `Annotated[Model, Query()]` query model gets ACCEPTS_INPUT.
func TestFastAPIReqResp_AnnotatedQueryModelEdge(t *testing.T) {
	src := `@app.get("/items")
def list_items(q: Annotated[FilterParams, Query()]):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:FilterParams") {
		t.Fatal("expected ACCEPTS_INPUT -> Class:FilterParams from Annotated[..., Query()]")
	}
}

// #4476: a Depends() resolving to a provider FUNCTION (not a model) must NOT
// emit an ACCEPTS_INPUT edge — conservative.
func TestFastAPIReqResp_DependsProviderNoEdge(t *testing.T) {
	src := `@app.get("/items")
def list_items(db = Depends(get_db), user = Depends(get_current_user)):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	for _, e := range ents {
		for _, r := range e.Rels {
			if r.Kind == "ACCEPTS_INPUT" {
				t.Fatalf("unexpected ACCEPTS_INPUT edge for provider dependency: %s", r.ToID)
			}
		}
	}
}

// #3629: FastAPI response_model= emits a RETURNS edge to the response DTO.
func TestFastAPIReqResp_ReturnsEdgeResponseModel(t *testing.T) {
	src := `@app.get("/users", response_model=UserOut)
def list_users():
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "RETURNS", "Class:UserOut") {
		t.Fatal("expected RETURNS -> Class:UserOut edge from response_model")
	}
}

// #3629: FastAPI return annotation emits a RETURNS edge (generic-unwrapped).
func TestFastAPIReqResp_ReturnsEdgeAnnotation(t *testing.T) {
	src := `@app.get("/users")
def list_users() -> List[UserOut]:
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	if !hasEntRel(ents, "RETURNS", "Class:UserOut") {
		t.Fatal("expected RETURNS -> Class:UserOut edge from return annotation")
	}
}

// #3629 negative: a primitive/path param emits no DTO edge.
func TestFastAPIReqResp_PrimitiveParamNoEdge(t *testing.T) {
	src := `@app.get("/items/{item_id}")
def get_item(item_id: int):
    pass
`
	ents := extract(t, "python_fastapi_reqresp", src)
	for _, e := range ents {
		if len(e.Rels) != 0 {
			t.Fatalf("expected no DTO edges for primitive param, got %+v", e.Rels)
		}
	}
}

// #3629: Flask marshmallow schema.load() emits ACCEPTS_INPUT to the schema.
func TestFlaskReqResp_AcceptsInputEdge(t *testing.T) {
	src := `@app.route("/orders", methods=["POST"])
def create_order():
    data = OrderSchema().load(request.json)
    return jsonify(data)
`
	ents := extract(t, "python_flask_reqresp", src)
	if !hasEntRel(ents, "ACCEPTS_INPUT", "Class:OrderSchema") {
		t.Fatalf("expected ACCEPTS_INPUT -> Class:OrderSchema edge, got %+v", ents)
	}
}

// #3629: Flask return annotation emits a RETURNS edge to the response type.
func TestFlaskReqResp_ReturnsEdge(t *testing.T) {
	src := `@app.get("/orders")
def list_orders() -> OrderResponse:
    pass
`
	ents := extract(t, "python_flask_reqresp", src)
	if !hasEntRel(ents, "RETURNS", "Class:OrderResponse") {
		t.Fatalf("expected RETURNS -> Class:OrderResponse edge, got %+v", ents)
	}
}

// #3629 negative: a Flask handler with no typed schema/return emits no edge.
func TestFlaskReqResp_UntypedNoEdge(t *testing.T) {
	src := `@app.route("/ping")
def ping():
    data = request.get_json()
    return jsonify({"ok": True})
`
	ents := extract(t, "python_flask_reqresp", src)
	for _, e := range ents {
		if len(e.Rels) != 0 {
			t.Fatalf("expected no edges for untyped Flask handler, got %+v", e.Rels)
		}
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

	// #4476 — list_orders accepts OrderFilterParams via Depends() query model,
	// and the get_current_user provider dependency yields NO edge.
	foundQueryModel := false
	for _, e := range acceptsInput {
		if e.Props["dto_type"] == "OrderFilterParams" && e.Props["match_source"] == "dependency_query_model" {
			foundQueryModel = true
		}
		if e.Props["dto_type"] == "get_current_user" || e.Props["dto_type"] == "user" {
			t.Errorf("provider dependency must not yield an accepts_input edge: %v", e.Props)
		}
	}
	if !foundQueryModel {
		t.Error("expected accepts_input entity with dto_type=OrderFilterParams (Depends() query model, #4476)")
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
		"python_dramatiq", "python_rq", "python_pydantic",
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
// Pydantic tests (issue #2984)
// ============================================================================

// findByName returns the first entity with the given Name, or a zero value and
// false if none matched.
func findByName(ents []extractResult, name string) (extractResult, bool) {
	for _, e := range ents {
		if e.Name == name {
			return e, true
		}
	}
	return extractResult{}, false
}

func TestPydantic_FieldValidator(t *testing.T) {
	src := `from pydantic import BaseModel, field_validator

class User(BaseModel):
    email: str

    @field_validator("email", mode="before")
    @classmethod
    def normalize_email(cls, v):
        return v.lower()
`
	ents := extract(t, "python_pydantic", src)
	e, ok := findByName(ents, "validate_normalize_email")
	if !ok {
		t.Fatalf("expected validate_normalize_email entity, got %+v", ents)
	}
	if e.Kind != "SCOPE.Pattern" {
		t.Fatalf("kind = %q, want SCOPE.Pattern", e.Kind)
	}
	if e.Props["pattern_type"] != "field_validator" {
		t.Fatalf("pattern_type = %q", e.Props["pattern_type"])
	}
	if e.Props["fields"] != "email" {
		t.Fatalf("fields = %q, want email", e.Props["fields"])
	}
	if e.Props["mode"] != "before" {
		t.Fatalf("mode = %q, want before", e.Props["mode"])
	}
	if e.Props["dialect"] != "v2" {
		t.Fatalf("dialect = %q, want v2", e.Props["dialect"])
	}
}

func TestPydantic_V1Validator(t *testing.T) {
	src := `from pydantic import BaseModel, validator

class Score(BaseModel):
    value: int

    @validator("value", pre=True)
    def coerce(cls, v):
        return int(v)
`
	ents := extract(t, "python_pydantic", src)
	e, ok := findByName(ents, "validate_coerce")
	if !ok {
		t.Fatalf("expected validate_coerce entity, got %+v", ents)
	}
	if e.Props["dialect"] != "v1" {
		t.Fatalf("dialect = %q, want v1", e.Props["dialect"])
	}
	if e.Props["mode"] != "before" {
		t.Fatalf("mode = %q (pre=True should map to before)", e.Props["mode"])
	}
}

func TestPydantic_ModelValidator(t *testing.T) {
	src := `from pydantic import BaseModel, model_validator

class Pair(BaseModel):
    a: int
    b: int

    @model_validator(mode="after")
    def check(self):
        return self
`
	ents := extract(t, "python_pydantic", src)
	e, ok := findByName(ents, "validate_check")
	if !ok {
		t.Fatalf("expected validate_check entity, got %+v", ents)
	}
	if e.Props["pattern_type"] != "model_validator" {
		t.Fatalf("pattern_type = %q", e.Props["pattern_type"])
	}
	if e.Props["mode"] != "after" {
		t.Fatalf("mode = %q, want after", e.Props["mode"])
	}
}

func TestPydantic_Constraints(t *testing.T) {
	src := `from pydantic import BaseModel, Field

class Account(BaseModel):
    username: str = Field(min_length=3, max_length=32, pattern=r"^[a-z]+$")
    age: int = Field(gt=0, le=150)
    note: str = Field(default="")
`
	ents := extract(t, "python_pydantic", src)
	u, ok := findByName(ents, "constraint_username")
	if !ok {
		t.Fatalf("expected constraint_username, got %+v", ents)
	}
	if u.Props["constraint_min_length"] != "3" {
		t.Fatalf("min_length = %q", u.Props["constraint_min_length"])
	}
	if u.Props["constraint_max_length"] != "32" {
		t.Fatalf("max_length = %q", u.Props["constraint_max_length"])
	}
	if u.Props["constraint_pattern"] == "" {
		t.Fatalf("expected pattern constraint, got %+v", u.Props)
	}
	a, ok := findByName(ents, "constraint_age")
	if !ok {
		t.Fatalf("expected constraint_age")
	}
	if a.Props["constraint_gt"] != "0" || a.Props["constraint_le"] != "150" {
		t.Fatalf("age constraints = %+v", a.Props)
	}
	// A bare Field(default=...) carries no constraint and must not be emitted.
	if _, ok := findByName(ents, "constraint_note"); ok {
		t.Fatal("constraint_note should not be emitted for a constraint-free Field()")
	}
}

func TestPydantic_ModelConfig(t *testing.T) {
	src := `from pydantic import BaseModel, ConfigDict

class S(BaseModel):
    model_config = ConfigDict(strict=True, str_strip_whitespace=True)
    x: int
`
	ents := extract(t, "python_pydantic", src)
	e, ok := findByName(ents, "model_config_model_config")
	if !ok {
		t.Fatalf("expected model_config entity, got %+v", ents)
	}
	if e.Props["dialect"] != "v2" {
		t.Fatalf("dialect = %q, want v2", e.Props["dialect"])
	}
	if !strings.Contains(e.Props["coercion_flags"], "strict") ||
		!strings.Contains(e.Props["coercion_flags"], "str_strip_whitespace") {
		t.Fatalf("coercion_flags = %q", e.Props["coercion_flags"])
	}
}

func TestPydantic_V1ConfigClass(t *testing.T) {
	src := `from pydantic import BaseModel

class S(BaseModel):
    x: int

    class Config:
        allow_population_by_field_name = True
        use_enum_values = True
`
	ents := extract(t, "python_pydantic", src)
	e, ok := findByName(ents, "model_config_Config")
	if !ok {
		t.Fatalf("expected v1 Config entity, got %+v", ents)
	}
	if e.Props["dialect"] != "v1" {
		t.Fatalf("dialect = %q, want v1", e.Props["dialect"])
	}
	if !strings.Contains(e.Props["coercion_flags"], "use_enum_values") {
		t.Fatalf("coercion_flags = %q", e.Props["coercion_flags"])
	}
}

func TestPydantic_NoClassDuplicate(t *testing.T) {
	// Issue #1501 discipline: the Pydantic extractor must never emit an entity
	// named after the model class itself (the base Python extractor owns that).
	src := `from pydantic import BaseModel, Field

class Widget(BaseModel):
    size: int = Field(gt=0)
`
	ents := extract(t, "python_pydantic", src)
	if _, ok := findByName(ents, "Widget"); ok {
		t.Fatal("python_pydantic must not emit an entity named after the class (issue #1501)")
	}
}

func TestPydantic_NonPydanticNoMatch(t *testing.T) {
	// A file that calls a function named Field() / validator() but never
	// references Pydantic must not produce constraint/validator nodes.
	src := `def Field(**kwargs):
    return None

x = Field(gt=0)
`
	ents := extract(t, "python_pydantic", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-Pydantic code, got %d: %+v", len(ents), ents)
	}
}

func TestPydantic_Fixture(t *testing.T) {
	content, err := os.ReadFile("testdata/pydantic_validators.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("python_pydantic")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/pydantic_validators.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}
	want := map[string]bool{
		"validate_normalize_email":   false, // @field_validator
		"validate_check_consistency": false, // @model_validator
		"validate_coerce_score":      false, // v1 @validator
		"constraint_username":        false, // Field(min_length, max_length, pattern)
		"constraint_age":             false, // Field(gt, le)
		"constraint_score":           false, // Field(ge, le)
		"model_config_model_config":  false, // ConfigDict (v2)
		"model_config_Config":        false, // class Config (v1)
	}
	for _, e := range ents {
		if _, tracked := want[e.Name]; tracked {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("fixture: expected entity %q not emitted", name)
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
		"python_dramatiq", "python_rq", "python_pydantic",
		"python_marshmallow", "python_attrs",
	}
	for _, key := range keys {
		_, ok := extractor.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
	}
}

// ============================================================================
// Marshmallow tests (issue #2985)
// ============================================================================

func TestMarshmallow_SchemaClass(t *testing.T) {
	src := `from marshmallow import Schema, fields

class UserSchema(Schema):
    name = fields.Str(required=True)
    email = fields.Email()
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Props["pattern_type"] == "schema_class" && e.Props["schema_name"] == "UserSchema" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected schema_class entity for UserSchema")
	}
}

func TestMarshmallow_Fields(t *testing.T) {
	src := `from marshmallow import Schema, fields

class ProductSchema(Schema):
    name = fields.Str(required=True)
    price = fields.Float()
    tags = fields.List(fields.Str())
`
	ents := extract(t, "python_marshmallow", src)
	fieldCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "field" {
			fieldCount++
		}
	}
	if fieldCount < 2 {
		t.Fatalf("expected at least 2 field entities, got %d", fieldCount)
	}
}

func TestMarshmallow_RequiredField(t *testing.T) {
	src := `from marshmallow import Schema, fields

class SignupSchema(Schema):
    username = fields.Str(required=True)
    email = fields.Email(required=True)
`
	ents := extract(t, "python_marshmallow", src)
	requiredCount := 0
	for _, e := range ents {
		if e.Props["required"] == "true" {
			requiredCount++
		}
	}
	if requiredCount < 2 {
		t.Fatalf("expected at least 2 required=true field entities, got %d", requiredCount)
	}
}

func TestMarshmallow_NestedField(t *testing.T) {
	src := `from marshmallow import Schema, fields

class AddressSchema(Schema):
    street = fields.Str()

class UserSchema(Schema):
    address = fields.Nested(AddressSchema)
    orders = fields.Nested("OrderSchema", many=True)
`
	ents := extract(t, "python_marshmallow", src)
	nestedCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "nested_field" {
			nestedCount++
			if e.Props["field"] == "address" && e.Props["nested_schema"] != "AddressSchema" {
				t.Errorf("nested_schema for address: got %q, want AddressSchema", e.Props["nested_schema"])
			}
		}
	}
	if nestedCount < 2 {
		t.Fatalf("expected at least 2 nested_field entities, got %d", nestedCount)
	}
}

func TestMarshmallow_ValidatesDecorator(t *testing.T) {
	src := `from marshmallow import Schema, fields, validates, ValidationError

class UserSchema(Schema):
    email = fields.Email()

    @validates("email")
    def validate_email(self, value):
        if "@" not in value:
            raise ValidationError("Not a valid email.")
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "field_validator" && e.Props["target_field"] == "email" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected field_validator entity for @validates('email')")
	}
}

func TestMarshmallow_ValidatesSchema(t *testing.T) {
	src := `from marshmallow import Schema, fields, validates_schema, ValidationError

class OrderSchema(Schema):
    amount = fields.Float()
    currency = fields.Str()

    @validates_schema
    def validate_order(self, data, **kwargs):
        if data["amount"] <= 0:
            raise ValidationError("Amount must be positive.")
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "schema_validator" && e.Props["validator_fn"] == "validate_order" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected schema_validator entity for @validates_schema")
	}
}

func TestMarshmallow_PostLoad(t *testing.T) {
	src := `from marshmallow import Schema, fields, post_load

class UserSchema(Schema):
    name = fields.Str()

    @post_load
    def make_user(self, data, **kwargs):
        return User(**data)
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "coercion_hook" && e.Props["hook_type"] == "post_load" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected coercion_hook entity for @post_load")
	}
}

// TestMarshmallow_Constraint_Range verifies validate.Range(min,max) extraction.
// Issue #3077.
func TestMarshmallow_Constraint_Range(t *testing.T) {
	src := `from marshmallow import Schema, fields, validate

class PriceSchema(Schema):
    price = fields.Float(validate=validate.Range(min=0, max=9999))
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_price" &&
			e.Props["constraint_validator"] == "Range" &&
			e.Props["constraint_min"] == "0" &&
			e.Props["constraint_max"] == "9999" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_price with Range(min=0, max=9999), got %+v", ents)
	}
}

// TestMarshmallow_Constraint_Length verifies validate.Length(min,max) extraction.
// Issue #3077.
func TestMarshmallow_Constraint_Length(t *testing.T) {
	src := `from marshmallow import Schema, fields, validate

class ItemSchema(Schema):
    name = fields.Str(validate=validate.Length(min=1, max=100))
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_name" &&
			e.Props["constraint_validator"] == "Length" &&
			e.Props["constraint_min"] == "1" &&
			e.Props["constraint_max"] == "100" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_name with Length(min=1, max=100), got %+v", ents)
	}
}

// TestMarshmallow_Constraint_OneOf verifies validate.OneOf([...]) extraction.
// Issue #3077.
func TestMarshmallow_Constraint_OneOf(t *testing.T) {
	src := `from marshmallow import Schema, fields, validate

class StatusSchema(Schema):
    status = fields.Str(validate=validate.OneOf(["active", "inactive"]))
`
	ents := extract(t, "python_marshmallow", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_status" &&
			e.Props["constraint_validator"] == "OneOf" &&
			e.Props["constraint_choices"] != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_status with OneOf, got %+v", ents)
	}
}

// TestMarshmallow_Constraint_NoLambda ensures a plain lambda validate= does NOT
// emit a constraint entity (no recognized validate.* validator call). Issue #3077.
func TestMarshmallow_Constraint_NoLambda(t *testing.T) {
	src := `from marshmallow import Schema, fields

class OrderSchema(Schema):
    amount = fields.Float(required=True, validate=lambda x: x > 0)
`
	ents := extract(t, "python_marshmallow", src)
	for _, e := range ents {
		if e.Name == "constraint_amount" {
			t.Fatal("constraint_amount must not be emitted for a lambda validate=")
		}
	}
}

// TestMarshmallow_Constraint_FullFixture exercises constraint extraction against
// testdata/marshmallow_nested.py. Proves constraint_extraction. Issue #3077.
func TestMarshmallow_Constraint_FullFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/marshmallow_nested.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("python_marshmallow")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/marshmallow_nested.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}

	// ProductSchema has price (Range), name (Length), status (OneOf).
	want := map[string]bool{
		"constraint_price":  false, // validate.Range(min=0, max=99999)
		"constraint_name":   false, // validate.Length(min=1, max=100)
		"constraint_status": false, // validate.OneOf([...])
	}
	for _, e := range ents {
		if _, tracked := want[e.Name]; tracked {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("fixture: expected constraint entity %q not emitted", name)
		}
	}
}

func TestMarshmallow_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_marshmallow", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-marshmallow code, got %d", len(ents))
	}
}

// TestMarshmallow_FullFixture exercises marshmallow.go against the
// testdata/marshmallow_nested.py fixture. Proves schema_extraction (full),
// nested_model_extraction (partial), and custom_validator_extraction (partial).
// Issue #2985.
func TestMarshmallow_FullFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/marshmallow_nested.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("python_marshmallow")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/marshmallow_nested.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}

	want := map[string]bool{
		"schema_AddressSchema":              false, // schema_class
		"schema_UserSchema":                 false, // schema_class
		"schema_OrderSchema":                false, // schema_class
		"nested_address":                    false, // nested_field (AddressSchema)
		"nested_orders":                     false, // nested_field (OrderSchema, many)
		"validate_validate_email":           false, // @validates("email")
		"validate_schema_validate_name_age": false, // @validates_schema
		"coerce_make_user":                  false, // @post_load
		"coerce_normalize_amount":           false, // @pre_load
	}
	for _, e := range ents {
		if _, tracked := want[e.Name]; tracked {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("fixture: expected entity %q not emitted", name)
		}
	}
}

// ============================================================================
// Attrs tests (issue #2985)
// ============================================================================

func TestAttrs_ClassDecorator_AttrS(t *testing.T) {
	src := `import attr

@attr.s
class Point:
    x = attr.ib()
    y = attr.ib()
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Props["pattern_type"] == "attrs_class" && e.Props["class_name"] == "Point" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected attrs_class entity for @attr.s Point")
	}
}

func TestAttrs_ClassDecorator_Define(t *testing.T) {
	src := `from attrs import define, field

@define
class User:
    name: str = field()
    email: str = field()
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "attrs_class" && e.Props["class_name"] == "User" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected attrs_class entity for @define User")
	}
}

func TestAttrs_Attrib(t *testing.T) {
	src := `import attr

@attr.s
class Item:
    name = attr.ib()
    price = attr.ib(default=0.0)
`
	ents := extract(t, "python_attrs", src)
	attribCount := 0
	for _, e := range ents {
		if e.Props["pattern_type"] == "attrib" {
			attribCount++
		}
	}
	if attribCount < 2 {
		t.Fatalf("expected at least 2 attrib entities, got %d", attribCount)
	}
}

func TestAttrs_ValidatorKwarg(t *testing.T) {
	src := `import attr

@attr.s
class User:
    age = attr.ib(validator=attr.validators.instance_of(int))
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "attrib" && e.Props["field"] == "age" && e.Props["validator"] != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected attrib entity with validator kwarg for age")
	}
}

func TestAttrs_FieldValidator(t *testing.T) {
	src := `from attrs import define, field

@define
class Order:
    amount: float = field()

    @amount.validator
    def validate_amount(self, attribute, value):
        if value <= 0:
            raise ValueError("Amount must be positive")
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "field_validator" && e.Props["target_field"] == "amount" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected field_validator entity for @amount.validator")
	}
}

func TestAttrs_ConverterKwarg(t *testing.T) {
	src := `from attrs import define, field

@define
class Payment:
    amount: float = field(converter=float)
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "attrib" && e.Props["field"] == "amount" && e.Props["converter"] == "float" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected attrib entity with converter=float for amount")
	}
}

// TestAttrs_Constraint_InstanceOf verifies that validators.instance_of() on an
// attrib() / field() emits a constraint_<field> entity. Issue #3077.
func TestAttrs_Constraint_InstanceOf(t *testing.T) {
	src := `import attr

@attr.s
class Address:
    street = attr.ib(validator=attr.validators.instance_of(str))
    zip_code = attr.ib(default="")
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_street" &&
			e.Props["pattern_type"] == "constraint" &&
			e.Props["constraint_validator"] == "instance_of" &&
			e.Props["constraint_type"] == "str" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_street with instance_of/str, got %+v", ents)
	}
	// zip_code has no validator= kwarg — must NOT emit a constraint entity.
	for _, e := range ents {
		if e.Name == "constraint_zip_code" {
			t.Fatal("constraint_zip_code should not be emitted for a field with no validator")
		}
	}
}

// TestAttrs_Constraint_In verifies validators.in_([...]) constraint extraction.
// Issue #3077.
func TestAttrs_Constraint_In(t *testing.T) {
	src := `import attr
import attrs

@attrs.define
class Product:
    status: str = field(validator=attr.validators.in_(["active", "inactive"]))
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_status" &&
			e.Props["constraint_validator"] == "in_" &&
			e.Props["constraint_values"] != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_status with in_ validator, got %+v", ents)
	}
}

// TestAttrs_Constraint_And verifies validators.and_(...) constraint extraction.
// Issue #3077.
func TestAttrs_Constraint_And(t *testing.T) {
	src := `import attr
import attrs
from attrs import define, field

@attrs.define
class Order:
    quantity: int = field(
        validator=attr.validators.and_(
            attr.validators.instance_of(int),
            attr.validators.in_([1, 5, 10]),
        )
    )
`
	ents := extract(t, "python_attrs", src)
	found := false
	for _, e := range ents {
		if e.Name == "constraint_quantity" && e.Props["constraint_validator"] == "and_" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_quantity with and_ validator, got %+v", ents)
	}
}

// TestAttrs_Constraint_FullFixture exercises the constraint entities against the
// testdata/attrs_validators.py fixture. Proves constraint_extraction. Issue #3077.
func TestAttrs_Constraint_FullFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/attrs_validators.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("python_attrs")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/attrs_validators.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}

	// street/city use instance_of(str); status uses in_(); quantity uses and_().
	want := map[string]bool{
		"constraint_street":   false, // instance_of(str)
		"constraint_city":     false, // instance_of(str)
		"constraint_status":   false, // in_(["active", ...])
		"constraint_quantity": false, // and_(...)
	}
	for _, e := range ents {
		if _, tracked := want[e.Name]; tracked {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("fixture: expected constraint entity %q not emitted", name)
		}
	}
}

func TestAttrs_NoMatch(t *testing.T) {
	src := `def regular():
    pass
`
	ents := extract(t, "python_attrs", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities for non-attrs code, got %d", len(ents))
	}
}

// TestAttrs_FullFixture exercises attrs.go against the
// testdata/attrs_validators.py fixture. Proves schema_extraction (partial),
// custom_validator_extraction (partial), and type_coercion_recognition (partial).
// Issue #2985.
func TestAttrs_FullFixture(t *testing.T) {
	content, err := os.ReadFile("testdata/attrs_validators.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ext, _ := extractor.Get("python_attrs")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "testdata/attrs_validators.py",
		Content:  content,
		Language: "python",
	})
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}

	want := map[string]bool{
		"schema_Address":           false, // @attr.s class
		"schema_User":              false, // @attrs.define class
		"schema_Order":             false, // @define class
		"validate_validate_email":  false, // @email.validator
		"validate_validate_age":    false, // @age.validator
		"validate_validate_amount": false, // @amount.validator
	}
	for _, e := range ents {
		if _, tracked := want[e.Name]; tracked {
			want[e.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("fixture: expected entity %q not emitted", name)
		}
	}
}

// ============================================================================
// Observability tests (log/metric/trace) — Issue #3063
// ============================================================================

func TestObservability_StdlibLogging(t *testing.T) {
	src := `import logging

logger = logging.getLogger(__name__)
app_log = logging.getLogger("myapp")

logger.info("Server started")
logger.debug("Debug message")
logger.error("Error occurred")
app_log.warning("Rate limit exceeded")
`
	ents := extract(t, "python_observability", src)

	var loggers, logStmts int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" {
			switch e.Subtype {
			case "logger":
				loggers++
				if e.Props["library"] != "logging" {
					t.Errorf("logger entity: expected library=logging, got %q", e.Props["library"])
				}
			case "log_statement":
				logStmts++
			}
		}
	}
	if loggers == 0 {
		t.Error("expected at least one logger entity for stdlib logging")
	}
	if logStmts == 0 {
		t.Error("expected at least one log_statement entity for stdlib logging")
	}
}

func TestObservability_Loguru(t *testing.T) {
	src := `from loguru import logger

logger.info("App started")
logger.debug("debug msg")
logger.error("Something went wrong")
`
	ents := extract(t, "python_observability", src)

	var loggers int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "logger" && e.Props["library"] == "loguru" {
			loggers++
		}
	}
	if loggers == 0 {
		t.Error("expected at least one logger entity for loguru")
	}
}

func TestObservability_Structlog(t *testing.T) {
	src := `import structlog

structlog.configure(processors=[structlog.processors.JSONRenderer()])
log = structlog.get_logger()
log.info("structlog info")
`
	ents := extract(t, "python_observability", src)

	var loggers int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "logger" && e.Props["library"] == "structlog" {
			loggers++
		}
	}
	if loggers == 0 {
		t.Error("expected at least one logger entity for structlog")
	}
}

func TestObservability_PrometheusClient(t *testing.T) {
	src := `from prometheus_client import Counter, Gauge, Histogram

REQUEST_COUNT = Counter("http_requests_total", "Total HTTP requests")
LATENCY = Histogram("http_request_duration_seconds", "Request latency")
IN_PROGRESS = Gauge("http_requests_in_progress", "In-progress requests")
`
	ents := extract(t, "python_observability", src)

	metricNames := map[string]bool{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric" {
			metricNames[e.Name] = true
		}
	}
	for _, want := range []string{"http_requests_total", "http_request_duration_seconds", "http_requests_in_progress"} {
		if !metricNames[want] {
			t.Errorf("expected metric entity %q, got: %v", want, metricNames)
		}
	}
}

func TestObservability_Statsd(t *testing.T) {
	src := `import statsd

client = statsd.StatsClient("localhost", 8125)
client.incr("page.views")
client.gauge("queue.size", 42)
client.timing("query.duration", 250)
`
	ents := extract(t, "python_observability", src)

	var metrics int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric" && e.Props["library"] == "statsd" {
			metrics++
		}
	}
	if metrics == 0 {
		t.Error("expected at least one metric entity for statsd")
	}
}

func TestObservability_Datadog(t *testing.T) {
	src := `from datadog import statsd

statsd.increment("web.page_views")
statsd.gauge("system.cpu.usage", 83.5)
statsd.histogram("api.response.time", 0.12)
`
	ents := extract(t, "python_observability", src)

	var metrics int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric" && e.Props["library"] == "datadog" {
			metrics++
		}
	}
	if metrics == 0 {
		t.Error("expected at least one metric entity for datadog")
	}
}

func TestObservability_OpenTelemetry(t *testing.T) {
	src := `from opentelemetry import trace

tracer = trace.get_tracer(__name__)

@tracer.start_as_current_span("process_request")
def handle_request(request):
    pass

def process_order(order_id):
    with tracer.start_as_current_span("process_order") as span:
        return fetch(order_id)
`
	ents := extract(t, "python_observability", src)

	spanNames := map[string]bool{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_span" {
			spanNames[e.Name] = true
		}
	}
	for _, want := range []string{"process_request", "process_order"} {
		if !spanNames[want] {
			t.Errorf("expected trace_span entity %q, got: %v", want, spanNames)
		}
	}
}

func TestObservability_DDTrace(t *testing.T) {
	src := `from ddtrace import tracer

@tracer.wrap("order_service.place")
def place_order(order):
    with tracer.trace("order_service.validate") as span:
        return validate(order)
`
	ents := extract(t, "python_observability", src)

	var spans int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_span" && e.Props["library"] == "ddtrace" {
			spans++
		}
	}
	if spans == 0 {
		t.Error("expected at least one trace_span entity for ddtrace")
	}
}

func TestObservability_JaegerClient(t *testing.T) {
	src := `import jaeger_client
from opentracing import tracer

config = jaeger_client.Config(
    config={"sampler": {"type": "const"}},
    service_name="order-service",
)

with tracer.start_span("order_lookup") as span:
    span.set_tag("order.id", "123")
`
	ents := extract(t, "python_observability", src)

	var spans int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_span" && e.Props["library"] == "jaeger_client" {
			spans++
		}
	}
	if spans == 0 {
		t.Error("expected at least one trace_span entity for jaeger_client")
	}
}

func TestObservability_NoFalsePositive(t *testing.T) {
	src := `from django.db import models

class Order(models.Model):
    amount = models.DecimalField(max_digits=10, decimal_places=2)
    status = models.CharField(max_length=20)
`
	ents := extract(t, "python_observability", src)
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && (e.Subtype == "logger" || e.Subtype == "metric" || e.Subtype == "trace_span" || e.Subtype == "log_statement") {
			t.Errorf("unexpected observability entity in non-observability file: %+v", e)
		}
	}
}

func TestObservability_FixtureLogging(t *testing.T) {
	content, err := os.ReadFile("testdata/observability_logging.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_observability", string(content))

	var loggers, logStmts int
	for _, e := range ents {
		if e.Kind != "SCOPE.Pattern" {
			continue
		}
		switch e.Subtype {
		case "logger":
			loggers++
		case "log_statement":
			logStmts++
		}
	}
	if loggers == 0 {
		t.Error("fixture: expected logger entities")
	}
	if logStmts == 0 {
		t.Error("fixture: expected log_statement entities")
	}
}

func TestObservability_FixtureMetrics(t *testing.T) {
	content, err := os.ReadFile("testdata/observability_metrics.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_observability", string(content))

	var metrics int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "metric" {
			metrics++
		}
	}
	if metrics == 0 {
		t.Error("fixture: expected metric entities")
	}
}

func TestObservability_FixtureTracing(t *testing.T) {
	content, err := os.ReadFile("testdata/observability_tracing.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_observability", string(content))

	var spans int
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "trace_span" {
			spans++
		}
	}
	if spans == 0 {
		t.Error("fixture: expected trace_span entities")
	}
}

// ============================================================================
// ORM relationship extractor tests — issue #3070
// ============================================================================

// ---- Peewee ----------------------------------------------------------------

func TestPeeweeRel_ForeignKeyField(t *testing.T) {
	src := `import peewee
from peewee import Model, ForeignKeyField, CharField

class Author(Model):
    name = CharField()

class Book(Model):
    title = CharField()
    author = ForeignKeyField(Author, backref="books")
`
	ents := extract(t, "python_peewee_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Book.author" && e.Props["pattern_type"] == "foreign_key" && e.Props["target_model"] == "Author" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Book.author foreign_key entity with target_model=Author")
	}
}

func TestPeeweeRel_ManyToManyField(t *testing.T) {
	src := `import peewee
from peewee import Model, ManyToManyField, CharField

class Tag(Model):
    name = CharField()

class Article(Model):
    title = CharField()
    tags = ManyToManyField(Tag, backref="articles")
`
	ents := extract(t, "python_peewee_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Article.tags" && e.Props["pattern_type"] == "many_to_many" && e.Props["target_model"] == "Tag" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Article.tags many_to_many entity with target_model=Tag")
	}
}

func TestPeeweeRel_NoFalsePositive(t *testing.T) {
	src := `class SomethingElse:
    name = "test"
`
	ents := extract(t, "python_peewee_rel", src)
	if len(ents) != 0 {
		t.Fatalf("expected 0 entities from non-peewee file, got %d", len(ents))
	}
}

func TestPeeweeRel_Fixture(t *testing.T) {
	src, err := os.ReadFile("testdata/peewee_relationships.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_peewee_rel", string(src))

	fkCount := 0
	mtmCount := 0
	for _, e := range ents {
		switch e.Props["pattern_type"] {
		case "foreign_key":
			fkCount++
		case "many_to_many":
			mtmCount++
		}
	}
	if fkCount < 3 {
		t.Errorf("expected >=3 foreign_key entities, got %d", fkCount)
	}
	if mtmCount < 1 {
		t.Errorf("expected >=1 many_to_many entity, got %d", mtmCount)
	}
}

// ---- Pony ORM --------------------------------------------------------------

func TestPonyRel_Required(t *testing.T) {
	src := `from pony.orm import Database, Required, Set

db = Database()

class Department(db.Entity):
    name = Required(str)
    employees = Set("Employee")

class Employee(db.Entity):
    name = Required(str)
    department = Required(Department)
`
	ents := extract(t, "python_pony_rel", src)
	foundDept := false
	foundSet := false
	for _, e := range ents {
		if e.Name == "Employee.department" && e.Props["pattern_type"] == "relationship" && e.Props["target_model"] == "Department" {
			foundDept = true
		}
		if e.Name == "Department.employees" && e.Props["pattern_type"] == "many_to_many" && e.Props["target_model"] == "Employee" {
			foundSet = true
		}
	}
	if !foundDept {
		t.Fatal("expected Employee.department relationship entity")
	}
	if !foundSet {
		t.Fatal("expected Department.employees many_to_many entity")
	}
}

func TestPonyRel_Optional(t *testing.T) {
	src := `from pony.orm import Database, Required, Optional

db = Database()

class Employee(db.Entity):
    name = Required(str)
    manager = Optional("Employee")
`
	ents := extract(t, "python_pony_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Employee.manager" && e.Props["pattern_type"] == "relationship" && e.Props["rel_kind"] == "Optional" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Employee.manager Optional relationship entity")
	}
}

func TestPonyRel_Fixture(t *testing.T) {
	src, err := os.ReadFile("testdata/pony_relationships.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_pony_rel", string(src))

	relCount := 0
	for _, e := range ents {
		if e.Props["framework"] == "pony" {
			relCount++
		}
	}
	if relCount < 4 {
		t.Errorf("expected >=4 pony relationship entities, got %d", relCount)
	}
}

// ---- Beanie ----------------------------------------------------------------

func TestBeanieRel_LinkField(t *testing.T) {
	src := `from beanie import Document, Link

class Category(Document):
    name: str

class Product(Document):
    title: str
    category: Link[Category]
`
	ents := extract(t, "python_beanie_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Product.category" && e.Props["pattern_type"] == "link" && e.Props["target_model"] == "Category" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Product.category link entity with target_model=Category")
	}
}

func TestBeanieRel_FetchLinks(t *testing.T) {
	src := `from beanie import Document, Link
from typing import List

class Category(Document):
    name: str

class Product(Document):
    category: Link[Category]

async def get(id):
    return await Product.get(id, fetch_links=True)
`
	ents := extract(t, "python_beanie_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Product.category" && e.Props["lazy_loading"] == "fetch_links" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected fetch_links lazy_loading annotation on Link field")
	}
}

func TestBeanieRel_Fixture(t *testing.T) {
	src, err := os.ReadFile("testdata/beanie_relationships.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_beanie_rel", string(src))

	linkCount := 0
	for _, e := range ents {
		if e.Props["framework"] == "beanie" && (e.Props["pattern_type"] == "link" || e.Props["pattern_type"] == "back_link") {
			linkCount++
		}
	}
	if linkCount < 3 {
		t.Errorf("expected >=3 beanie link entities, got %d", linkCount)
	}
}

// ---- MongoEngine -----------------------------------------------------------

func TestMongoEngineRel_ReferenceField(t *testing.T) {
	src := `import mongoengine
from mongoengine import Document, ReferenceField, StringField

class Author(Document):
    name = StringField()

class Book(Document):
    title = StringField()
    author = ReferenceField(Author)
`
	ents := extract(t, "python_mongoengine_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Book.author" && e.Props["pattern_type"] == "reference" && e.Props["target_model"] == "Author" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Book.author reference entity with target_model=Author")
	}
}

func TestMongoEngineRel_EmbeddedDocumentField(t *testing.T) {
	src := `from mongoengine import Document, EmbeddedDocument, EmbeddedDocumentField, StringField

class Address(EmbeddedDocument):
    street = StringField()

class Person(Document):
    name = StringField()
    address = EmbeddedDocumentField(Address)
`
	ents := extract(t, "python_mongoengine_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Person.address" && e.Props["pattern_type"] == "embedded" && e.Props["target_model"] == "Address" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Person.address embedded entity with target_model=Address")
	}
}

func TestMongoEngineRel_LazyReferenceField(t *testing.T) {
	src := `from mongoengine import Document, LazyReferenceField, StringField

class Category(Document):
    name = StringField()

class Post(Document):
    title = StringField()
    category = LazyReferenceField(Category)
`
	ents := extract(t, "python_mongoengine_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Post.category" && e.Props["pattern_type"] == "lazy_reference" && e.Props["lazy_loading"] == "lazy_reference" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Post.category lazy_reference entity with lazy_loading annotation")
	}
}

func TestMongoEngineRel_Fixture(t *testing.T) {
	src, err := os.ReadFile("testdata/mongoengine_relationships.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_mongoengine_rel", string(src))

	refCount := 0
	embCount := 0
	for _, e := range ents {
		switch e.Props["pattern_type"] {
		case "reference", "lazy_reference":
			refCount++
		case "embedded", "embedded_list":
			embCount++
		}
	}
	if refCount < 2 {
		t.Errorf("expected >=2 reference entities, got %d", refCount)
	}
	if embCount < 2 {
		t.Errorf("expected >=2 embedded entities, got %d", embCount)
	}
}

// ---- Tortoise ORM ----------------------------------------------------------

func TestTortoiseRel_ForeignKeyField(t *testing.T) {
	src := `from tortoise import fields
from tortoise.models import Model

class Tournament(Model):
    name = fields.CharField(max_length=255)

class Event(Model):
    name = fields.CharField(max_length=255)
    tournament = fields.ForeignKeyField("models.Tournament", related_name="events")
`
	ents := extract(t, "python_tortoise_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Event.tournament" && e.Props["pattern_type"] == "foreign_key" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Event.tournament foreign_key entity")
	}
}

func TestTortoiseRel_ManyToManyField(t *testing.T) {
	src := `from tortoise import fields
from tortoise.models import Model

class Team(Model):
    name = fields.CharField(max_length=255)

class Event(Model):
    name = fields.CharField(max_length=255)
    participants = fields.ManyToManyField("models.Team", related_name="events")
`
	ents := extract(t, "python_tortoise_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Event.participants" && e.Props["pattern_type"] == "many_to_many" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Event.participants many_to_many entity")
	}
}

func TestTortoiseRel_OneToOneField(t *testing.T) {
	src := `from tortoise import fields
from tortoise.models import Model

class Profile(Model):
    bio = fields.TextField()

class Player(Model):
    name = fields.CharField(max_length=255)
    profile = fields.OneToOneField("models.Profile", related_name="player")
`
	ents := extract(t, "python_tortoise_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Player.profile" && e.Props["pattern_type"] == "one_to_one" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Player.profile one_to_one entity")
	}
}

func TestTortoiseRel_ReverseRelation(t *testing.T) {
	src := `from tortoise import fields
from tortoise.models import Model

class Tournament(Model):
    name = fields.CharField(max_length=255)
    events: fields.ReverseRelation["Event"]
`
	ents := extract(t, "python_tortoise_rel", src)
	found := false
	for _, e := range ents {
		if e.Name == "Tournament.events" && e.Props["pattern_type"] == "reverse_relation" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Tournament.events reverse_relation entity")
	}
}

func TestTortoiseRel_Fixture(t *testing.T) {
	src, err := os.ReadFile("testdata/tortoise_relationships.py")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extract(t, "python_tortoise_rel", string(src))

	fkCount := 0
	mtmCount := 0
	o2oCount := 0
	revCount := 0
	for _, e := range ents {
		switch e.Props["pattern_type"] {
		case "foreign_key":
			fkCount++
		case "many_to_many":
			mtmCount++
		case "one_to_one":
			o2oCount++
		case "reverse_relation":
			revCount++
		}
	}
	if fkCount < 2 {
		t.Errorf("expected >=2 foreign_key entities, got %d", fkCount)
	}
	if mtmCount < 1 {
		t.Errorf("expected >=1 many_to_many entity, got %d", mtmCount)
	}
	if o2oCount < 1 {
		t.Errorf("expected >=1 one_to_one entity, got %d", o2oCount)
	}
	if revCount < 1 {
		t.Errorf("expected >=1 reverse_relation entity, got %d", revCount)
	}
}

// ============================================================================
// Issue #3346 deepening tests
// ============================================================================

// ---- Django per-field Form type introspection ----

// TestDjango_FormFieldTypeIntrospection verifies that CharField/IntegerField/etc.
// assignments inside a Form or ModelForm class are extracted as form_field entities
// with the correct field_type property (issue #3346).
func TestDjango_FormFieldTypeIntrospection(t *testing.T) {
	src := `from django import forms

class ContactForm(forms.Form):
    name = forms.CharField(max_length=100)
    age = forms.IntegerField(min_value=0)
    email = forms.EmailField(required=False)
`
	ents := extract(t, "python_django", src)

	type fieldCheck struct {
		name      string
		fieldType string
	}
	want := []fieldCheck{
		{"ContactForm.name", "CharField"},
		{"ContactForm.age", "IntegerField"},
		{"ContactForm.email", "EmailField"},
	}
	byName := map[string]extractResult{}
	for _, e := range ents {
		byName[e.Name] = e
	}
	for _, w := range want {
		e, ok := byName[w.name]
		if !ok {
			t.Errorf("expected form_field entity %q, not found (got %v)", w.name, byName)
			continue
		}
		if e.Props["pattern_type"] != "form_field" {
			t.Errorf("%q: expected pattern_type=form_field, got %q", w.name, e.Props["pattern_type"])
		}
		if e.Props["field_type"] != w.fieldType {
			t.Errorf("%q: expected field_type=%q, got %q", w.name, w.fieldType, e.Props["field_type"])
		}
	}

	// form_class summary entity must also be present.
	formClass, ok := byName["ContactForm"]
	if !ok {
		t.Fatal("expected form_class summary entity ContactForm")
	}
	if formClass.Props["pattern_type"] != "form_class" {
		t.Errorf("ContactForm: expected pattern_type=form_class, got %q", formClass.Props["pattern_type"])
	}
	if !strings.Contains(formClass.Props["field_names"], "name") {
		t.Errorf("ContactForm: expected field_names to contain 'name', got %q", formClass.Props["field_names"])
	}
}

// TestDjango_ModelFormFieldIntrospection tests form_field extraction from a ModelForm.
func TestDjango_ModelFormFieldIntrospection(t *testing.T) {
	src := `from django import forms

class UserProfileForm(forms.ModelForm):
    username = forms.CharField(max_length=50)
    birth_date = forms.DateField()
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "UserProfileForm.username" && e.Props["field_type"] == "CharField" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected UserProfileForm.username form_field with field_type=CharField")
	}
}

// ---- Django MIDDLEWARE settings-list parser ----

// TestDjango_MiddlewareSettingsParser verifies that MIDDLEWARE = [...] in settings.py
// is extracted as a middleware_settings config entity with the list of middleware
// dotted-paths (issue #3346).
func TestDjango_MiddlewareSettingsParser(t *testing.T) {
	src := `# Django settings

MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
    "django.middleware.common.CommonMiddleware",
    "myapp.middleware.RequestLoggingMiddleware",
]
`
	ents := extract(t, "python_django", src)
	var mwEnt *extractResult
	for i := range ents {
		if ents[i].Props["pattern_type"] == "middleware_settings" {
			mwEnt = &ents[i]
			break
		}
	}
	if mwEnt == nil {
		t.Fatal("expected middleware_settings entity from MIDDLEWARE = [...]")
	}
	if mwEnt.Name != "MIDDLEWARE" {
		t.Errorf("expected entity name MIDDLEWARE, got %q", mwEnt.Name)
	}
	if mwEnt.Kind != "SCOPE.Config" {
		t.Errorf("expected kind SCOPE.Config, got %q", mwEnt.Kind)
	}
	mwList := mwEnt.Props["middleware_list"]
	for _, expected := range []string{
		"django.middleware.security.SecurityMiddleware",
		"myapp.middleware.RequestLoggingMiddleware",
	} {
		if !strings.Contains(mwList, expected) {
			t.Errorf("middleware_list missing %q; got %q", expected, mwList)
		}
	}
}

// ---- DRF SerializerMethodField return-type inference ----

// TestDRF_SerializerMethodField verifies that a SerializerMethodField and its getter
// method are extracted with the return type inferred from the annotation (issue #3346).
func TestDRF_SerializerMethodField(t *testing.T) {
	src := `from rest_framework import serializers

class UserSerializer(serializers.ModelSerializer):
    full_name = serializers.SerializerMethodField()
    is_active = serializers.SerializerMethodField()

    def get_full_name(self, obj) -> str:
        return f"{obj.first_name} {obj.last_name}"

    def get_is_active(self, obj) -> bool:
        return obj.status == "active"
`
	ents := extract(t, "python_django", src)
	byName := map[string]extractResult{}
	for _, e := range ents {
		byName[e.Name] = e
	}
	fnEnt, ok := byName["UserSerializer.full_name"]
	if !ok {
		t.Fatal("expected UserSerializer.full_name serializer_field entity")
	}
	if fnEnt.Props["pattern_type"] != "serializer_method_field" {
		t.Errorf("expected pattern_type=serializer_method_field, got %q", fnEnt.Props["pattern_type"])
	}
	if fnEnt.Props["return_type"] != "str" {
		t.Errorf("expected return_type=str, got %q", fnEnt.Props["return_type"])
	}
	if fnEnt.Props["getter"] != "get_full_name" {
		t.Errorf("expected getter=get_full_name, got %q", fnEnt.Props["getter"])
	}

	iaEnt, ok := byName["UserSerializer.is_active"]
	if !ok {
		t.Fatal("expected UserSerializer.is_active serializer_field entity")
	}
	if iaEnt.Props["return_type"] != "bool" {
		t.Errorf("expected return_type=bool, got %q", iaEnt.Props["return_type"])
	}
}

// TestDRF_SerializerMethodField_NoAnnotation verifies extraction still works
// when the getter method has no return type annotation.
func TestDRF_SerializerMethodField_NoAnnotation(t *testing.T) {
	src := `from rest_framework import serializers

class OrderSerializer(serializers.ModelSerializer):
    total_display = serializers.SerializerMethodField()

    def get_total_display(self, obj):
        return f"${obj.total:.2f}"
`
	ents := extract(t, "python_django", src)
	found := false
	for _, e := range ents {
		if e.Name == "OrderSerializer.total_display" && e.Props["pattern_type"] == "serializer_method_field" {
			found = true
			// No return_type expected when annotation is absent.
			if e.Props["return_type"] != "" {
				t.Errorf("expected empty return_type for unannotated getter, got %q", e.Props["return_type"])
			}
		}
	}
	if !found {
		t.Fatal("expected OrderSerializer.total_display entity even without return type annotation")
	}
}

// ---- DRF DEFAULT_AUTHENTICATION_CLASSES / DEFAULT_THROTTLE_CLASSES ----

// TestDRF_DefaultAuthAndThrottleClasses verifies that REST_FRAMEWORK = {...} in
// settings is parsed to extract DEFAULT_AUTHENTICATION_CLASSES and
// DEFAULT_THROTTLE_CLASSES as SCOPE.Config/drf_setting entities (issue #3346).
func TestDRF_DefaultAuthAndThrottleClasses(t *testing.T) {
	src := `REST_FRAMEWORK = {
    "DEFAULT_AUTHENTICATION_CLASSES": [
        "rest_framework.authentication.SessionAuthentication",
        "rest_framework_simplejwt.authentication.JWTAuthentication",
    ],
    "DEFAULT_THROTTLE_CLASSES": [
        "rest_framework.throttling.AnonRateThrottle",
        "rest_framework.throttling.UserRateThrottle",
    ],
}
`
	ents := extract(t, "python_django", src)
	byName := map[string]extractResult{}
	for _, e := range ents {
		byName[e.Name] = e
	}
	authEnt, ok := byName["DEFAULT_AUTHENTICATION_CLASSES"]
	if !ok {
		t.Fatal("expected DEFAULT_AUTHENTICATION_CLASSES drf_setting entity")
	}
	if authEnt.Props["setting_key"] != "DEFAULT_AUTHENTICATION_CLASSES" {
		t.Errorf("setting_key mismatch: %q", authEnt.Props["setting_key"])
	}
	if !strings.Contains(authEnt.Props["classes"], "JWTAuthentication") {
		t.Errorf("expected JWTAuthentication in classes, got %q", authEnt.Props["classes"])
	}
	if !strings.Contains(authEnt.Props["classes"], "SessionAuthentication") {
		t.Errorf("expected SessionAuthentication in classes, got %q", authEnt.Props["classes"])
	}

	throttleEnt, ok := byName["DEFAULT_THROTTLE_CLASSES"]
	if !ok {
		t.Fatal("expected DEFAULT_THROTTLE_CLASSES drf_setting entity")
	}
	if !strings.Contains(throttleEnt.Props["classes"], "AnonRateThrottle") {
		t.Errorf("expected AnonRateThrottle in classes, got %q", throttleEnt.Props["classes"])
	}
}

// ---- Flask-WTF validate_on_submit() ----

// TestFlask_ValidateOnSubmit verifies that form.validate_on_submit() call sites
// are detected and emitted as SCOPE.Pattern/form_submit entities (issue #3346).
func TestFlask_ValidateOnSubmit(t *testing.T) {
	src := `from flask import render_template, redirect
from flask_wtf import FlaskForm

@app.route("/login", methods=["GET", "POST"])
def login():
    form = LoginForm()
    if form.validate_on_submit():
        return redirect("/dashboard")
    return render_template("login.html", form=form)
`
	ents := extract(t, "python_flask", src)
	found := false
	for _, e := range ents {
		if e.Props["pattern_type"] == "validate_on_submit" && e.Props["form_var"] == "form" {
			found = true
			if e.Name != "form.validate_on_submit" {
				t.Errorf("expected entity name 'form.validate_on_submit', got %q", e.Name)
			}
		}
	}
	if !found {
		t.Fatal("expected validate_on_submit entity for 'form.validate_on_submit()'")
	}
}

// TestFlask_ValidateOnSubmit_MultipleVars verifies that multiple distinct form
// variables in the same file each get their own entity, with deduplication for
// repeated calls to the same variable.
func TestFlask_ValidateOnSubmit_MultipleVars(t *testing.T) {
	src := `@app.route("/register", methods=["GET", "POST"])
def register():
    reg_form = RegisterForm()
    if reg_form.validate_on_submit():
        pass
    # second call same var — must not duplicate
    if reg_form.validate_on_submit():
        pass

@app.route("/profile", methods=["POST"])
def profile():
    prof_form = ProfileForm()
    if prof_form.validate_on_submit():
        pass
`
	ents := extract(t, "python_flask", src)
	votCount := 0
	vars := map[string]bool{}
	for _, e := range ents {
		if e.Props["pattern_type"] == "validate_on_submit" {
			votCount++
			vars[e.Props["form_var"]] = true
		}
	}
	if votCount != 2 {
		t.Fatalf("expected 2 distinct validate_on_submit entities (dedup same var), got %d", votCount)
	}
	if !vars["reg_form"] {
		t.Error("expected entity for reg_form.validate_on_submit")
	}
	if !vars["prof_form"] {
		t.Error("expected entity for prof_form.validate_on_submit")
	}
}

// ---- Celery TESTS edges ----

// TestCelery_TestsEdges verifies that a pytest test function calling task.delay() or
// task.apply_async() emits a TESTS relationship to the task (issue #3346).
func TestCelery_TestsEdges(t *testing.T) {
	src := `import pytest
from myapp.tasks import send_email, process_order

def test_send_email_task():
    result = send_email.delay("user@example.com", "Hello")
    assert result is not None

def test_process_order_task():
    result = process_order.apply_async(args=[42], countdown=10)
    assert result.id
`
	ents := extract(t, "python_pytest", src)
	// Issue #4357: celery TESTS edges are now folded onto the single test_suite.
	suite := pytestSuite(t, ents)
	foundDelay, foundApply := false, false
	for _, r := range suite.Rels {
		if r.Kind == "TESTS" && r.ToID == "Task:send_email" {
			foundDelay = true
		}
		if r.Kind == "TESTS" && r.ToID == "Task:process_order" {
			foundApply = true
		}
	}
	if !foundDelay {
		t.Errorf("expected TESTS → Task:send_email (via .delay()), got rels: %+v", suite.Rels)
	}
	if !foundApply {
		t.Errorf("expected TESTS → Task:process_order (via .apply_async()), got rels: %+v", suite.Rels)
	}
}

// TestCelery_TestsEdges_Apply verifies task.apply() also produces a TESTS edge.
func TestCelery_TestsEdges_Apply(t *testing.T) {
	src := `def test_sync_task():
    result = my_task.apply(args=[1, 2])
    assert result.result == 3
`
	ents := extract(t, "python_pytest", src)
	suite := pytestSuite(t, ents)
	found := false
	for _, r := range suite.Rels {
		if r.Kind == "TESTS" && r.ToID == "Task:my_task" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TESTS → Task:my_task (via .apply()), got rels: %+v", suite.Rels)
	}
}

// TestCelery_TestsEdges_NoFalsePositive verifies that non-task method calls like
// obj.apply_async() where there is no known task reference do not emit bogus TESTS
// edges, and that non-test functions are unaffected.
func TestCelery_TestsEdges_NoFalsePositive(t *testing.T) {
	// A helper function (not a test_* function) should not have TESTS edges.
	src := `def helper_function():
    result = my_task.delay(42)
    return result
`
	ents := extract(t, "python_pytest", src)
	// python_pytest only extracts test_* functions — helper_function is not extracted.
	for _, e := range ents {
		if e.Name == "helper_function" {
			t.Fatalf("pytest extractor should not emit entity for non-test_ function")
		}
	}
}
