package python_test

import (
	"context"
	"strings"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
)

// extract returns extracted entities with fields for assertion.
func extract(t *testing.T, key, content string) []struct {
	Name      string
	Kind      string
	Subtype   string
	StartLine int
	Props     map[string]string
} {
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
	var result []struct {
		Name      string
		Kind      string
		Subtype   string
		StartLine int
		Props     map[string]string
	}
	for _, e := range entities {
		result = append(result, struct {
			Name      string
			Kind      string
			Subtype   string
			StartLine int
			Props     map[string]string
		}{e.Name, e.Kind, e.Subtype, e.StartLine, e.Properties})
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
	ents := extract(t, "python_django", src)
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
	src := `class UserCreate(BaseModel):
    name: str
    email: str

class AppConfig(BaseSettings):
    debug: bool = False
`
	ents := extract(t, "python_fastapi", src)
	schemaCount := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" {
			schemaCount++
		}
	}
	if schemaCount != 2 {
		t.Fatalf("expected 2 Pydantic schemas, got %d", schemaCount)
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

func TestAllExtractors_EmptyInput(t *testing.T) {
	keys := []string{
		"python_django", "python_fastapi", "python_flask", "python_celery",
		"python_pytest", "python_langchain", "python_mongodb", "python_redis",
		"python_sqlalchemy", "python_fastapi_reqresp", "python_flask_reqresp",
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
	}
	for _, key := range keys {
		_, ok := extractor.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
	}
}
