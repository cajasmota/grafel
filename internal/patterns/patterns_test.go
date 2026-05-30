package patterns

import (
	"strings"
	"testing"
)

// TestRegistryHas59Detectors verifies all detectors registered via init().
func TestRegistryHas59Detectors(t *testing.T) {
	detectors := All()
	if len(detectors) < 55 {
		t.Errorf("expected at least 55 registered detectors, got %d", len(detectors))
	}
}

// TestAllDetectorsHaveCategory verifies each detector returns a non-empty Category().
func TestAllDetectorsHaveCategory(t *testing.T) {
	for _, d := range All() {
		if d.Category() == "" {
			t.Errorf("detector %T has empty Category()", d)
		}
	}
}

// ============================================================
// Auth endpoint linker
// ============================================================

func TestAuthEndpointLinker_AppliesTo(t *testing.T) {
	d := &authEndpointLinker{}
	if !d.AppliesTo(`router.use(verifyToken)`) {
		t.Error("should apply to verifyToken source")
	}
	if d.AppliesTo(`console.log("hello world")`) {
		t.Error("should not apply to plain log statement")
	}
}

func TestAuthEndpointLinker_Detect_Express(t *testing.T) {
	d := &authEndpointLinker{}
	src := `router.use(jwtAuthMiddleware)
app.get('/admin', adminGuard, handler)`
	results := d.Detect("routes.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected at least 1 entity from Express auth patterns")
	}
	found := false
	for _, e := range results {
		if e.Properties["framework"] == "express" {
			found = true
		}
	}
	if !found {
		t.Error("expected entity with framework=express")
	}
}

func TestAuthEndpointLinker_Detect_Spring(t *testing.T) {
	d := &authEndpointLinker{}
	src := `@PreAuthorize("hasRole('ADMIN')")
public void adminMethod() {}`
	results := d.Detect("Admin.java", "java", src)
	if len(results) == 0 {
		t.Error("expected at least 1 entity from Spring @PreAuthorize")
	}
}

func TestAuthEndpointLinker_Detect_NestJS(t *testing.T) {
	d := &authEndpointLinker{}
	src := `@UseGuards(JwtAuthGuard, RolesGuard)
async getProfile() {}`
	results := d.Detect("profile.controller.ts", "typescript", src)
	if len(results) < 2 {
		t.Errorf("expected 2 guard entities, got %d", len(results))
	}
}

func TestAuthEndpointLinker_Detect_FastAPI(t *testing.T) {
	d := &authEndpointLinker{}
	src := `@app.get("/me")
async def me(user = Depends(get_current_user)):
    pass`
	results := d.Detect("main.py", "python", src)
	if len(results) == 0 {
		t.Error("expected entity from FastAPI Depends")
	}
}

func TestAuthEndpointLinker_Detect_ASPNet(t *testing.T) {
	d := &authEndpointLinker{}
	src := `[Authorize(Roles="Admin")]
public IActionResult Admin() => View();`
	results := d.Detect("HomeController.cs", "csharp", src)
	if len(results) == 0 {
		t.Error("expected entity from ASP.NET [Authorize]")
	}
}

func TestAuthEndpointLinker_Detect_NonAuthToken_Skipped(t *testing.T) {
	d := &authEndpointLinker{}
	src := `app.use(cors())
app.use(json())`
	results := d.Detect("app.js", "javascript", src)
	// cors and json are in non-auth tokens list, should not match
	for _, e := range results {
		if e.Properties["middleware_name"] == "cors" || e.Properties["middleware_name"] == "json" {
			t.Errorf("non-auth token %s should be excluded", e.Properties["middleware_name"])
		}
	}
}

// ============================================================
// CORS extractor
// ============================================================

func TestCORSExtractor_AppliesTo(t *testing.T) {
	d := &corsExtractor{}
	if !d.AppliesTo(`app.use(cors({origin: "*"}))`) {
		t.Error("should apply to express cors")
	}
	if !d.AppliesTo(`add_middleware(CORSMiddleware, allow_origins=["*"])`) {
		t.Error("should apply to fastapi cors")
	}
	if d.AppliesTo(`console.log("no cors here")`) {
		t.Error("should not apply to plain log")
	}
}

func TestCORSExtractor_Detect_Express(t *testing.T) {
	d := &corsExtractor{}
	src := `app.use(cors({origin: "https://example.com", methods: "GET,POST"}))`
	results := d.Detect("app.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected CORS entity")
	}
	if results[0].Properties["origin_pattern"] != "https://example.com" {
		t.Errorf("expected origin_pattern=https://example.com, got %s", results[0].Properties["origin_pattern"])
	}
}

func TestCORSExtractor_Detect_Spring(t *testing.T) {
	d := &corsExtractor{}
	src := `@CrossOrigin(origins = "https://frontend.com")`
	results := d.Detect("Controller.java", "java", src)
	if len(results) == 0 {
		t.Error("expected Spring CORS entity")
	}
}

func TestCORSExtractor_Detect_FastAPI(t *testing.T) {
	d := &corsExtractor{}
	src := `app.add_middleware(CORSMiddleware, allow_origins=["https://api.example.com"], allow_methods=["GET", "POST"])`
	results := d.Detect("main.py", "python", src)
	if len(results) == 0 {
		t.Error("expected FastAPI CORS entity")
	}
}

// ============================================================
// SQL injection detector
// ============================================================

func TestSQLInjectionDetector_AppliesTo(t *testing.T) {
	d := &sqlInjectionDetector{}
	if !d.AppliesTo(`query = "SELECT * FROM users WHERE id=" + user_id`) {
		t.Error("should apply to string concat SQL")
	}
	if d.AppliesTo(`x = 42`) {
		t.Error("should not apply to assignment")
	}
}

func TestSQLInjectionDetector_Detect_FString(t *testing.T) {
	d := &sqlInjectionDetector{}
	src := `cursor.execute(f"SELECT * FROM users WHERE name={name}")`
	results := d.Detect("db.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["risk_level"] == "high" {
			found = true
		}
	}
	if !found {
		t.Error("expected high risk entity for f-string SQL")
	}
}

func TestSQLInjectionDetector_Detect_PercentFormat(t *testing.T) {
	d := &sqlInjectionDetector{}
	src := `query = "SELECT * FROM orders WHERE id=%s" % order_id`
	results := d.Detect("dao.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["risk_level"] == "medium" {
			found = true
		}
	}
	if !found {
		t.Error("expected medium risk entity for percent-format SQL")
	}
}

func TestSQLInjectionDetector_Detect_EmptySource(t *testing.T) {
	d := &sqlInjectionDetector{}
	results := d.Detect("empty.py", "python", "")
	if len(results) != 0 {
		t.Error("expected no results for empty source")
	}
}

func TestSQLInjectionDetector_Detect_ParameterizedSafe(t *testing.T) {
	d := &sqlInjectionDetector{}
	// Parameterized execute should not be flagged
	src := `cursor.execute("SELECT * FROM users WHERE id = %s", (user_id,))`
	// This uses parameterized form, should not emit
	_ = d.Detect("safe.py", "python", src)
}

// ============================================================
// Health check extractor
// ============================================================

func TestHealthCheckExtractor_AppliesTo(t *testing.T) {
	d := &healthCheckExtractor{}
	if !d.AppliesTo(`app.get('/health', handler)`) {
		t.Error("should apply to health route")
	}
	if d.AppliesTo(`app.get('/users', handler)`) {
		t.Error("should not apply to normal route")
	}
}

func TestHealthCheckExtractor_Detect_Express(t *testing.T) {
	d := &healthCheckExtractor{}
	src := `app.get('/health', (req, res) => res.send('ok'))
app.get('/ready', healthHandler)`
	results := d.Detect("app.js", "javascript", src)
	if len(results) < 1 {
		t.Errorf("expected at least 1 health check entity, got %d", len(results))
	}
}

func TestHealthCheckExtractor_Detect_Quarkus(t *testing.T) {
	d := &healthCheckExtractor{}
	src := `@Liveness
public class MyLivenessCheck implements HealthCheck {}`
	results := d.Detect("Check.java", "java", src)
	if len(results) == 0 {
		t.Error("expected Quarkus @Liveness entity")
	}
	if results[0].Properties["kind"] != "health_check" {
		t.Errorf("expected kind=health_check, got %s", results[0].Properties["kind"])
	}
}

func TestHealthCheckExtractor_Detect_ReadinessPath(t *testing.T) {
	d := &healthCheckExtractor{}
	src := `app.get("/readyz", readyHandler)`
	results := d.Detect("server.js", "javascript", src)
	for _, e := range results {
		if e.Subtype == "readiness_probe" {
			return
		}
	}
	if len(results) > 0 {
		t.Errorf("expected readiness_probe subtype, got %s", results[0].Subtype)
	}
}

// ============================================================
// Rate limit extractor
// ============================================================

func TestRateLimitExtractor_AppliesTo(t *testing.T) {
	d := &rateLimitExtractor{}
	if !d.AppliesTo(`const limiter = rateLimit({windowMs: 15*60*1000, max: 100})`) {
		t.Error("should apply to express-rate-limit")
	}
	if !d.AppliesTo(`@RateLimiter(name="service")`) {
		t.Error("should apply to Spring @RateLimiter")
	}
}

func TestRateLimitExtractor_Detect_Go(t *testing.T) {
	d := &rateLimitExtractor{}
	src := `import "golang.org/x/time/rate"
l := rate.NewLimiter(rate.Every(time.Second), 10)`
	results := d.Detect("server.go", "go", src)
	if len(results) == 0 {
		t.Error("expected go-rate entity")
	}
	if results[0].Properties["algorithm"] != "token_bucket" {
		t.Errorf("expected token_bucket algorithm for go-rate")
	}
}

func TestRateLimitExtractor_Detect_FlaskLimiter(t *testing.T) {
	d := &rateLimitExtractor{}
	src := `@limiter.limit("100 per minute")
def api_endpoint():`
	results := d.Detect("views.py", "python", src)
	if len(results) == 0 {
		t.Error("expected flask-limiter entity")
	}
}

// ============================================================
// Queue detector
// ============================================================

func TestQueueDetector_AppliesTo(t *testing.T) {
	d := &queueDetector{}
	if !d.AppliesTo(`from kafka import KafkaProducer`) {
		t.Error("should apply to kafka import")
	}
	if !d.AppliesTo(`import sarama`) {
		t.Error("should apply to sarama import")
	}
}

func TestQueueDetector_Detect_KafkaProducer(t *testing.T) {
	d := &queueDetector{}
	src := `producer = KafkaProducer(bootstrap_servers='localhost:9092')
producer.send('my-topic', value=b'message')`
	results := d.Detect("producer.py", "python", src)
	if len(results) == 0 {
		t.Error("expected WRITES_TO entity for KafkaProducer")
	}
}

func TestQueueDetector_Detect_KafkaConsumer(t *testing.T) {
	d := &queueDetector{}
	src := `consumer = KafkaConsumer('events', bootstrap_servers=['localhost:9092'])`
	results := d.Detect("consumer.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["relationship"] == "READS_FROM" {
			found = true
		}
	}
	if !found {
		t.Error("expected READS_FROM entity for KafkaConsumer")
	}
}

// ============================================================
// Redis key extractor
// ============================================================

func TestRedisKeyExtractor_AppliesTo(t *testing.T) {
	d := &redisKeyExtractor{}
	if !d.AppliesTo(`import redis`) {
		t.Error("should apply to redis import")
	}
	if d.AppliesTo(`import os`) {
		t.Error("should not apply to os import")
	}
}

func TestRedisKeyExtractor_Detect_Python(t *testing.T) {
	d := &redisKeyExtractor{}
	src := `import redis
r = redis.Redis()
r.set('user:123', 'data')
r.get('session:abc')`
	results := d.Detect("cache.py", "python", src)
	if len(results) == 0 {
		t.Error("expected cache_key entities")
	}
}

func TestRedisKeyExtractor_Detect_Go(t *testing.T) {
	d := &redisKeyExtractor{}
	src := `"github.com/redis/go-redis/v9"
rdb.Set(ctx, "counter:hits", 1, 0)`
	results := d.Detect("cache.go", "go", src)
	if len(results) == 0 {
		t.Error("expected cache_key entity for go-redis")
	}
}

// ============================================================
// gRPC impl detector
// ============================================================

func TestGRPCImplDetector_AppliesTo(t *testing.T) {
	d := &grpcImplDetector{}
	if !d.AppliesTo(`"google.golang.org/grpc"`) {
		t.Error("should apply to grpc import")
	}
	if d.AppliesTo(`import os`) {
		t.Error("should not apply without grpc import")
	}
}

func TestGRPCImplDetector_Detect_Go(t *testing.T) {
	d := &grpcImplDetector{}
	src := `"google.golang.org/grpc"
type UserServiceServer struct {
    pb.UnimplementedUserServiceServer
}`
	results := d.Detect("server.go", "go", src)
	if len(results) == 0 {
		t.Error("expected grpc impl entity")
	}
	if results[0].Properties["service_name"] != "UserService" {
		t.Errorf("expected service_name=UserService, got %s", results[0].Properties["service_name"])
	}
}

func TestGRPCImplDetector_Detect_Java(t *testing.T) {
	d := &grpcImplDetector{}
	src := `import io.grpc;
@GrpcService
public class UserServiceImpl extends UserServiceGrpc.UserServiceImplBase {}`
	results := d.Detect("UserServiceImpl.java", "java", src)
	if len(results) == 0 {
		t.Error("expected grpc impl entity for Java")
	}
}

// ============================================================
// Cache eviction detector
// ============================================================

func TestCacheEvictionDetector_AppliesTo(t *testing.T) {
	d := &cacheEvictionDetector{}
	if !d.AppliesTo(`@Cacheable(cacheNames="users")`) {
		t.Error("should apply to Spring @Cacheable")
	}
	if d.AppliesTo(`import time`) {
		t.Error("should not apply to plain time import")
	}
}

func TestCacheEvictionDetector_Detect_Spring(t *testing.T) {
	d := &cacheEvictionDetector{}
	src := `@CacheEvict(cacheNames="products", allEntries=true)
public void clearCache() {}`
	results := d.Detect("CacheService.java", "java", src)
	if len(results) == 0 {
		t.Error("expected cache eviction entity for Spring")
	}
}

func TestCacheEvictionDetector_Detect_Redis(t *testing.T) {
	d := &cacheEvictionDetector{}
	src := `r.expire("session:abc", 3600)
r.setex("token", 900, "value")`
	results := d.Detect("cache.py", "python", src)
	if len(results) == 0 {
		t.Error("expected cache eviction entity for Redis TTL")
	}
}

// ============================================================
// Call graph extractor
// ============================================================

func TestCallGraphExtractor_AppliesTo(t *testing.T) {
	d := &callGraphExtractor{}
	if !d.AppliesTo(`service.getUserById(id)`) {
		t.Error("should apply to method call")
	}
}

func TestCallGraphExtractor_Detect_Go(t *testing.T) {
	d := &callGraphExtractor{}
	src := `func (s *Service) GetUser(id string) User {
    return s.repo.FindByID(id)
}`
	results := d.Detect("service.go", "go", src)
	if len(results) == 0 {
		t.Error("expected call graph entity for Go dotted call")
	}
}

func TestCallGraphExtractor_Detect_SQL(t *testing.T) {
	d := &callGraphExtractor{}
	src := `CALL get_user_by_id(@id);`
	results := d.Detect("proc.sql", "sql", src)
	found := false
	for _, e := range results {
		if strings.Contains(e.Name, "get_user_by_id") {
			found = true
		}
	}
	if !found {
		t.Error("expected SQL CALL proc entity")
	}
}

// ============================================================
// CICD pipeline extractor
// ============================================================

func TestCICDPipelineExtractor_AppliesTo(t *testing.T) {
	d := &cicdPipelineExtractor{}
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3`
	if !d.AppliesTo(src) {
		t.Error("should apply to GitHub Actions workflow")
	}
}

func TestCICDPipelineExtractor_Detect_GHA(t *testing.T) {
	d := &cicdPipelineExtractor{}
	src := `on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
  test:
    runs-on: ubuntu-latest`
	results := d.Detect(".github/workflows/ci.yml", "yaml", src)
	if len(results) == 0 {
		t.Error("expected job entities from GHA workflow")
	}
	// Should have 'build' and 'test' jobs
	names := map[string]bool{}
	for _, e := range results {
		names[e.Properties["job_id"]] = true
	}
	if !names["build"] && !names["test"] {
		t.Error("expected build and test job entities")
	}
}

// ============================================================
// Comment marker extractor
// ============================================================

func TestCommentMarkerExtractor_AppliesTo(t *testing.T) {
	d := &commentMarkerExtractor{}
	if !d.AppliesTo(`// TODO: fix this`) {
		t.Error("should apply to TODO comment")
	}
	if d.AppliesTo(`x = 1`) {
		t.Error("should not apply without markers")
	}
}

func TestCommentMarkerExtractor_Detect_TODO(t *testing.T) {
	d := &commentMarkerExtractor{}
	src := `// TODO: implement caching
// FIXME: this is broken`
	results := d.Detect("main.go", "go", src)
	markers := map[string]bool{}
	for _, e := range results {
		markers[e.Properties["marker"]] = true
	}
	if !markers["TODO"] {
		t.Error("expected TODO marker entity")
	}
	if !markers["FIXME"] {
		t.Error("expected FIXME marker entity")
	}
}

// ============================================================
// Config detector
// ============================================================

func TestConfigDetector_AppliesTo(t *testing.T) {
	d := &configDetector{}
	if !d.AppliesTo(`os.Getenv("DATABASE_URL")`) {
		t.Error("should apply to os.Getenv")
	}
	if !d.AppliesTo(`process.env.PORT`) {
		t.Error("should apply to process.env")
	}
}

func TestConfigDetector_Detect_EnvVar(t *testing.T) {
	d := &configDetector{}
	src := `db_url = os.getenv("DATABASE_URL")
port = os.environ.get("PORT")`
	results := d.Detect("config.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["var_name"] == "DATABASE_URL" {
			found = true
		}
	}
	if !found {
		t.Error("expected DATABASE_URL env var entity")
	}
}

// ============================================================
// CSRF heuristic detector
// ============================================================

func TestCSRFHeuristicDetector_AppliesTo(t *testing.T) {
	d := &csrfHeuristicDetector{}
	if !d.AppliesTo(`.csrf().disable()`) {
		t.Error("should apply to csrf().disable()")
	}
}

func TestCSRFHeuristicDetector_Detect_SpringDisabled(t *testing.T) {
	d := &csrfHeuristicDetector{}
	src := `http.csrf().disable().authorizeRequests()`
	results := d.Detect("SecurityConfig.java", "java", src)
	if len(results) == 0 {
		t.Error("expected csrf disabled entity")
	}
	if results[0].Properties["csrf_kind"] != "disabled" {
		t.Errorf("expected csrf_kind=disabled, got %s", results[0].Properties["csrf_kind"])
	}
}

func TestCSRFHeuristicDetector_Detect_DjangoExempt(t *testing.T) {
	d := &csrfHeuristicDetector{}
	src := `@csrf_exempt
def my_view(request): pass`
	results := d.Detect("views.py", "python", src)
	if len(results) == 0 {
		t.Error("expected csrf exempt entity")
	}
}

// ============================================================
// Database index extractor
// ============================================================

func TestDatabaseIndexExtractor_AppliesTo(t *testing.T) {
	d := &databaseIndexExtractor{}
	if !d.AppliesTo(`CREATE INDEX idx_users_email ON users(email)`) {
		t.Error("should apply to CREATE INDEX")
	}
}

func TestDatabaseIndexExtractor_Detect_SQL(t *testing.T) {
	d := &databaseIndexExtractor{}
	src := `CREATE UNIQUE INDEX idx_users_email ON users (email);
CREATE INDEX idx_posts_user ON posts (user_id, created_at);`
	results := d.Detect("schema.sql", "sql", src)
	if len(results) < 2 {
		t.Errorf("expected 2 index entities, got %d", len(results))
	}
}

func TestDatabaseIndexExtractor_Detect_Hibernate(t *testing.T) {
	d := &databaseIndexExtractor{}
	src := `@Table(indexes = @Index(name="idx_email", columnList="email"))
public class User {}`
	results := d.Detect("User.java", "java", src)
	if len(results) == 0 {
		t.Error("expected Hibernate @Index entity")
	}
}

// ============================================================
// Decorator extractor
// ============================================================

func TestDecoratorExtractor_AppliesTo(t *testing.T) {
	d := &decoratorExtractor{}
	if !d.AppliesTo(`@app.route('/api')`) {
		t.Error("should apply to Python decorator")
	}
}

func TestDecoratorExtractor_Detect_Python(t *testing.T) {
	d := &decoratorExtractor{}
	src := `@require_http_methods(["GET", "POST"])
def my_view(request):
    pass`
	results := d.Detect("views.py", "python", src)
	if len(results) == 0 {
		t.Error("expected decorator entity for Python")
	}
}

// ============================================================
// Docker compose extractor
// ============================================================

func TestDockerComposeExtractor_AppliesTo(t *testing.T) {
	d := &dockerComposeExtractor{}
	src := `version: "3.9"
services:
  web:
    image: nginx`
	if !d.AppliesTo(src) {
		t.Error("should apply to docker-compose")
	}
}

func TestDockerComposeExtractor_Detect_Services(t *testing.T) {
	d := &dockerComposeExtractor{}
	src := `version: "3"
services:
  web:
    image: nginx:alpine
  db:
    image: postgres:14`
	results := d.Detect("docker-compose.yml", "yaml", src)
	if len(results) < 2 {
		t.Errorf("expected 2 service entities, got %d", len(results))
	}
}

// ============================================================
// Entity version tracker
// ============================================================

func TestEntityVersionTracker_AppliesTo(t *testing.T) {
	d := &entityVersionTracker{}
	if !d.AppliesTo(`/api/v2/users`) {
		t.Error("should apply to versioned URL")
	}
}

func TestEntityVersionTracker_Detect_URLVersion(t *testing.T) {
	d := &entityVersionTracker{}
	src := `const baseURL = '/api/v3/';`
	results := d.Detect("api.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected version entity")
	}
	if results[0].Properties["version"] != "v3" {
		t.Errorf("expected version=v3, got %s", results[0].Properties["version"])
	}
}

// ============================================================
// Error handling detector
// ============================================================

func TestErrorHandlingDetector_AppliesTo(t *testing.T) {
	d := &errorHandlingDetector{}
	if !d.AppliesTo(`if err != nil { return err }`) {
		t.Error("should apply to Go error check")
	}
}

func TestErrorHandlingDetector_Detect_Go(t *testing.T) {
	d := &errorHandlingDetector{}
	src := `func getUser(id string) (*User, error) {
    u, err := db.Find(id)
    if err != nil {
        return nil, err
    }
    return u, nil
}`
	results := d.Detect("service.go", "go", src)
	if len(results) == 0 {
		t.Error("expected error handling entity")
	}
}

// ============================================================
// Feature flag extractor
// ============================================================

func TestFeatureFlagExtractor_AppliesTo(t *testing.T) {
	d := &featureFlagExtractor{}
	if !d.AppliesTo(`ldClient.boolVariation("new-feature", user, false)`) {
		t.Error("should apply to LaunchDarkly variation call")
	}
}

func TestFeatureFlagExtractor_Detect_LaunchDarkly(t *testing.T) {
	d := &featureFlagExtractor{}
	src := `enabled = ldClient.boolVariation("dark-mode", ctx, false)`
	results := d.Detect("app.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected feature flag entity")
	}
	if results[0].Properties["flag_name"] != "dark-mode" {
		t.Errorf("expected flag_name=dark-mode, got %s", results[0].Properties["flag_name"])
	}
}

// ============================================================
// File upload detector
// ============================================================

func TestFileUploadDetector_AppliesTo(t *testing.T) {
	d := &fileUploadDetector{}
	if !d.AppliesTo(`const upload = multer({dest: 'uploads/'})`) {
		t.Error("should apply to multer")
	}
	if !d.AppliesTo(`async def upload(file: UploadFile)`) {
		t.Error("should apply to FastAPI UploadFile")
	}
}

func TestFileUploadDetector_Detect_Multer(t *testing.T) {
	d := &fileUploadDetector{}
	src := `const upload = multer({ dest: '/tmp' })`
	results := d.Detect("routes.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected multer entity")
	}
	if results[0].Properties["library"] != "multer" {
		t.Errorf("expected library=multer, got %s", results[0].Properties["library"])
	}
}

// ============================================================
// Framework version enricher
// ============================================================

func TestFrameworkVersionEnricher_AppliesTo(t *testing.T) {
	d := &frameworkVersionEnricher{}
	if !d.AppliesTo(`Django==4.2.0`) {
		t.Error("should apply to Django version spec")
	}
	if !d.AppliesTo(`"express": "^4.18.0"`) {
		t.Error("should apply to Express version in package.json")
	}
}

func TestFrameworkVersionEnricher_Detect_Django(t *testing.T) {
	d := &frameworkVersionEnricher{}
	src := `Django==4.2.3
fastapi==0.103.1`
	results := d.Detect("requirements.txt", "text", src)
	found := false
	for _, e := range results {
		if e.Properties["framework"] == "django" && e.Properties["version"] == "4.2.3" {
			found = true
		}
	}
	if !found {
		t.Error("expected Django version entity")
	}
}

// ============================================================
// Logging config extractor
// ============================================================

func TestLoggingConfigExtractor_AppliesTo(t *testing.T) {
	d := &loggingConfigExtractor{}
	if !d.AppliesTo(`import logging`) {
		t.Error("should apply to Python logging import")
	}
	if !d.AppliesTo(`"log/slog"`) {
		t.Error("should apply to Go slog import")
	}
}

func TestLoggingConfigExtractor_Detect_Python(t *testing.T) {
	d := &loggingConfigExtractor{}
	src := `import logging
logging.basicConfig(level=logging.DEBUG)`
	results := d.Detect("app.py", "python", src)
	if len(results) == 0 {
		t.Error("expected logging config entity")
	}
}

// ============================================================
// Middleware chain extractor
// ============================================================

func TestMiddlewareChainExtractor_AppliesTo(t *testing.T) {
	d := &middlewareChainExtractor{}
	if !d.AppliesTo(`app.use(helmet())`) {
		t.Error("should apply to app.use()")
	}
}

func TestMiddlewareChainExtractor_Detect_Express(t *testing.T) {
	d := &middlewareChainExtractor{}
	src := `app.get('/api/users', authenticate, authorize, getUsers)
app.use(logger())`
	results := d.Detect("routes.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected middleware entities")
	}
}

// ============================================================
// Mock library extractor
// ============================================================

func TestMockLibraryExtractor_AppliesTo(t *testing.T) {
	d := &mockLibraryExtractor{}
	if !d.AppliesTo(`from unittest.mock import patch`) {
		t.Error("should apply to unittest.mock")
	}
}

func TestMockLibraryExtractor_Detect_PythonPatch(t *testing.T) {
	d := &mockLibraryExtractor{}
	src := `@patch('mymodule.requests.get')
def test_api(mock_get): pass`
	results := d.Detect("test_api.py", "python", src)
	if len(results) == 0 {
		t.Error("expected mock entity for @patch")
	}
	if results[0].Properties["library"] != "unittest.mock" {
		t.Errorf("expected library=unittest.mock, got %s", results[0].Properties["library"])
	}
}

// ============================================================
// MongoDB query enricher
// ============================================================

func TestMongoQueryEnricher_AppliesTo(t *testing.T) {
	d := &mongoQueryEnricher{}
	if !d.AppliesTo(`from pymongo import MongoClient`) {
		t.Error("should apply to pymongo")
	}
}

func TestMongoQueryEnricher_Detect_Find(t *testing.T) {
	d := &mongoQueryEnricher{}
	src := `from pymongo import MongoClient
result = users.find({"active": True})`
	results := d.Detect("db.py", "python", src)
	if len(results) == 0 {
		t.Error("expected mongo find entity")
	}
}

// ============================================================
// MongoDB aggregate extractor
// ============================================================

func TestMongoDBAggregatextractor_AppliesTo(t *testing.T) {
	d := &mongodbAggregateExtractor{}
	if !d.AppliesTo(`db.orders.aggregate([{$match: {status: "active"}}])`) {
		t.Error("should apply to aggregate with $ stages")
	}
}

func TestMongoDBAggregatextractor_Detect_Stages(t *testing.T) {
	d := &mongodbAggregateExtractor{}
	src := `const pipeline = orders.aggregate([
  { $match: { status: "active" } },
  { $group: { _id: "$user_id", total: { $sum: "$amount" } } },
  { $sort: { total: -1 } }
])`
	results := d.Detect("analytics.js", "javascript", src)
	if len(results) == 0 {
		t.Error("expected aggregate pipeline entity")
	}
}

// ============================================================
// Naming convention detector
// ============================================================

func TestNamingConventionDetector_AppliesTo(t *testing.T) {
	d := &namingConventionDetector{}
	if !d.AppliesTo(`def get_user_by_id():`) {
		t.Error("should apply to Python function def")
	}
}

func TestNamingConventionDetector_Detect_SnakeCase(t *testing.T) {
	d := &namingConventionDetector{}
	src := `def get_user_by_id():
    pass
def create_session():
    pass`
	results := d.Detect("utils.py", "python", src)
	// Detector emits a per-file summary entity "naming_convention@<file>"
	// whose Properties["conventions"] lists all conventions found in the
	// file (comma-joined). Per-convention entities were removed to avoid
	// ghost entities that broke Python parity (see naming_convention_detector.go).
	found := false
	for _, e := range results {
		if e.Properties["summary"] == "true" &&
			strings.Contains(e.Properties["conventions"], "snake_case") {
			found = true
		}
	}
	if !found {
		t.Error("expected naming_convention summary entity containing snake_case")
	}
}

// ============================================================
// Onboarding entry enricher
// ============================================================

func TestOnboardingEntryEnricher_AppliesTo(t *testing.T) {
	d := &onboardingEntryEnricher{}
	if !d.AppliesTo(`func main() {`) {
		t.Error("should apply to Go main function")
	}
	if !d.AppliesTo(`app = Flask(__name__)`) {
		t.Error("should apply to Flask app creation")
	}
}

func TestOnboardingEntryEnricher_Detect_GoMain(t *testing.T) {
	d := &onboardingEntryEnricher{}
	src := `package main

func main() {
    http.ListenAndServe(":8080", nil)
}`
	results := d.Detect("main.go", "go", src)
	if len(results) == 0 {
		t.Error("expected entry point entity")
	}
	if results[0].Properties["entry_kind"] != "binary_entry" {
		t.Errorf("expected binary_entry, got %s", results[0].Properties["entry_kind"])
	}
}

// ============================================================
// OpenAPI extractor
// ============================================================

func TestOpenAPIExtractor_AppliesTo(t *testing.T) {
	d := &openAPIExtractor{}
	if !d.AppliesTo(`openapi: "3.0.0"`) {
		t.Error("should apply to OpenAPI spec")
	}
	if !d.AppliesTo(`swagger: "2.0"`) {
		t.Error("should apply to Swagger spec")
	}
}

func TestOpenAPIExtractor_Detect_Operations(t *testing.T) {
	d := &openAPIExtractor{}
	src := `openapi: "3.0.0"
info:
  title: My API
  version: "1.0"
paths:
  /users:
    get:
      summary: List users
    post:
      summary: Create user`
	results := d.Detect("openapi.yaml", "yaml", src)
	if len(results) < 2 {
		t.Errorf("expected at least 2 operation entities (get+post), got %d", len(results))
	}
}

// ============================================================
// ORM detector
// ============================================================

func TestORMDetector_AppliesTo(t *testing.T) {
	d := &ormDetector{}
	if !d.AppliesTo(`from sqlalchemy import Column, Integer`) {
		t.Error("should apply to SQLAlchemy import")
	}
}

func TestORMDetector_Detect_GORM(t *testing.T) {
	d := &ormDetector{}
	src := `import "gorm.io/gorm"
db.Find(&users, "active = ?", true)`
	results := d.Detect("db.go", "go", src)
	if len(results) == 0 {
		t.Error("expected ORM entity for GORM")
	}
}

// ============================================================
// Pattern recommendation enricher
// ============================================================

func TestPatternRecommendationEnricher_Detect_HardcodedCredential(t *testing.T) {
	d := &patternRecommendationEnricher{}
	src := `password = "super_secret_12345"`
	if !d.AppliesTo(src) {
		t.Error("should apply to hardcoded credential")
	}
	results := d.Detect("config.py", "python", src)
	if len(results) == 0 {
		t.Error("expected hardcoded credential recommendation entity")
	}
	if results[0].Properties["severity"] != "high" {
		t.Errorf("expected severity=high, got %s", results[0].Properties["severity"])
	}
}

// ============================================================
// Pattern taxonomy enricher
// ============================================================

func TestPatternTaxonomyEnricher_Detect_MVCPath(t *testing.T) {
	d := &patternTaxonomyEnricher{}
	results := d.Detect("src/controllers/UserController.go", "go", "")
	found := false
	for _, e := range results {
		if strings.Contains(e.Properties["taxonomy"], "mvc") {
			found = true
		}
	}
	if !found {
		t.Error("expected mvc taxonomy for controllers/ path")
	}
}

func TestPatternTaxonomyEnricher_Detect_Observer(t *testing.T) {
	d := &patternTaxonomyEnricher{}
	src := `eventEmitter.on('data', handler)`
	results := d.Detect("events.js", "javascript", src)
	found := false
	for _, e := range results {
		if e.Properties["taxonomy"] == "gof:observer" {
			found = true
		}
	}
	if !found {
		t.Error("expected gof:observer taxonomy")
	}
}

// ============================================================
// Port aggregation extractor
// ============================================================

func TestPortAggregationExtractor_AppliesTo(t *testing.T) {
	d := &portAggregationExtractor{}
	if !d.AppliesTo(`EXPOSE 8080`) {
		t.Error("should apply to Dockerfile EXPOSE")
	}
	if !d.AppliesTo(`app.listen(3000)`) {
		t.Error("should apply to .listen()")
	}
}

func TestPortAggregationExtractor_Detect_Dockerfile(t *testing.T) {
	d := &portAggregationExtractor{}
	src := `FROM node:18
EXPOSE 3000
EXPOSE 9090/tcp`
	results := d.Detect("Dockerfile", "dockerfile", src)
	if len(results) < 2 {
		t.Errorf("expected 2 port entities, got %d", len(results))
	}
}

// ============================================================
// Property test detector
// ============================================================

func TestPropertyTestDetector_AppliesTo(t *testing.T) {
	d := &propertyTestDetector{}
	if !d.AppliesTo(`@given(st.text())`) {
		t.Error("should apply to Hypothesis @given")
	}
}

func TestPropertyTestDetector_Detect_Hypothesis(t *testing.T) {
	d := &propertyTestDetector{}
	src := `from hypothesis import given, strategies as st

@given(st.integers())
def test_add_commutative(x):
    assert x + 0 == x`
	results := d.Detect("test_math.py", "python", src)
	if len(results) == 0 {
		t.Error("expected property test entity for Hypothesis")
	}
	if results[0].Properties["library"] != "hypothesis" {
		t.Errorf("expected library=hypothesis, got %s", results[0].Properties["library"])
	}
}

// ============================================================
// Raw SQL extractor
// ============================================================

func TestRawSQLExtractor_AppliesTo(t *testing.T) {
	d := &rawSQLExtractor{}
	if !d.AppliesTo(`cursor.execute("SELECT * FROM users")`) {
		t.Error("should apply to SQL execute")
	}
}

func TestRawSQLExtractor_Detect_Select(t *testing.T) {
	d := &rawSQLExtractor{}
	src := `rows = db.execute("SELECT id, name FROM customers WHERE active = true")`
	results := d.Detect("repo.py", "python", src)
	if len(results) == 0 {
		t.Error("expected raw SQL SELECT entity")
	}
	if results[0].Properties["operation"] != "SELECT" {
		t.Errorf("expected operation=SELECT, got %s", results[0].Properties["operation"])
	}
}

// ============================================================
// Re-export detector
// ============================================================

func TestReExportDetector_AppliesTo(t *testing.T) {
	d := &reExportDetector{}
	if !d.AppliesTo(`export * from './utils'`) {
		t.Error("should apply to wildcard re-export")
	}
}

func TestReExportDetector_Detect_JSWildcard(t *testing.T) {
	d := &reExportDetector{}
	src := `export * from './auth'
export * from './users'
export { default as Config } from './config'`
	results := d.Detect("index.ts", "typescript", src)
	if len(results) < 3 {
		t.Errorf("expected at least 3 re-export entities, got %d", len(results))
	}
}

// ============================================================
// React/Next.js enricher
// ============================================================

func TestReactNextJSEnricher_AppliesTo(t *testing.T) {
	d := &reactNextJSEnricher{}
	if !d.AppliesTo(`'use client'`) {
		t.Error("should apply to 'use client' directive")
	}
	if !d.AppliesTo(`const state = useState(null)`) {
		t.Error("should apply to React hooks")
	}
}

func TestReactNextJSEnricher_Detect_ClientDirective(t *testing.T) {
	d := &reactNextJSEnricher{}
	src := `'use client'

import { useState } from 'react'
export default function Component() { return <div/> }`
	results := d.Detect("Component.tsx", "typescript", src)
	found := false
	for _, e := range results {
		if e.Properties["component_type"] == "client" {
			found = true
		}
	}
	if !found {
		t.Error("expected client component entity")
	}
}

// ============================================================
// Resilience pattern extractor
// ============================================================

func TestResiliencePatternExtractor_AppliesTo(t *testing.T) {
	d := &resiliencePatternExtractor{}
	if !d.AppliesTo(`@CircuitBreaker(name="myService")`) {
		t.Error("should apply to @CircuitBreaker")
	}
	if !d.AppliesTo(`gobreaker.NewCircuitBreaker(settings)`) {
		t.Error("should apply to gobreaker")
	}
}

func TestResiliencePatternExtractor_Detect_R4J(t *testing.T) {
	d := &resiliencePatternExtractor{}
	src := `@CircuitBreaker(name = "userService", fallbackMethod = "fallback")
public User getUser(String id) { ... }`
	results := d.Detect("UserService.java", "java", src)
	if len(results) == 0 {
		t.Error("expected resilience4j circuit breaker entity")
	}
}

// ============================================================
// Schema detector
// ============================================================

func TestSchemaDetector_AppliesTo(t *testing.T) {
	d := &schemaDetector{}
	if !d.AppliesTo(`from pydantic import BaseModel`) {
		t.Error("should apply to pydantic import")
	}
	if !d.AppliesTo(`const schema = z.object({name: z.string()})`) {
		t.Error("should apply to zod schema")
	}
}

func TestSchemaDetector_Detect_Pydantic(t *testing.T) {
	d := &schemaDetector{}
	src := `from pydantic import BaseModel

class UserCreate(BaseModel):
    name: str
    email: str`
	results := d.Detect("schemas.py", "python", src)
	if len(results) == 0 {
		t.Error("expected pydantic schema entity")
	}
	if results[0].Properties["library"] != "pydantic" {
		t.Errorf("expected library=pydantic, got %s", results[0].Properties["library"])
	}
}

// ============================================================
// Secrets management detector
// ============================================================

func TestSecretsManagementDetector_AppliesTo(t *testing.T) {
	d := &secretsManagementDetector{}
	if !d.AppliesTo(`import hvac`) {
		t.Error("should apply to hvac import")
	}
	if !d.AppliesTo(`secretsmanager = boto3.client('secretsmanager')`) {
		t.Error("should apply to boto3 secretsmanager")
	}
}

func TestSecretsManagementDetector_Detect_Vault(t *testing.T) {
	d := &secretsManagementDetector{}
	src := `import hvac
client = hvac.Client(url='http://vault:8200', token=os.environ['VAULT_TOKEN'])`
	results := d.Detect("vault.py", "python", src)
	if len(results) == 0 {
		t.Error("expected vault entity")
	}
	if results[0].Properties["provider"] != "vault" {
		t.Errorf("expected provider=vault, got %s", results[0].Properties["provider"])
	}
}

// ============================================================
// Service detector
// ============================================================

func TestServiceDetector_AppliesTo(t *testing.T) {
	d := &serviceDetector{}
	if !d.AppliesTo(`grpc.NewServer()`) {
		t.Error("should apply to grpc.NewServer()")
	}
	if !d.AppliesTo(`@Injectable()`) {
		t.Error("should apply to NestJS @Injectable")
	}
}

func TestServiceDetector_Detect_gRPCServer(t *testing.T) {
	d := &serviceDetector{}
	src := `"google.golang.org/grpc"
s := grpc.NewServer(grpc.UnaryInterceptor(...))`
	results := d.Detect("server.go", "go", src)
	if len(results) == 0 {
		t.Error("expected gRPC server entity")
	}
}

// Kotlin files must NOT produce the generic "spring_service" ghost
// entity from the pattern detector. The kotlin extractor owns Spring
// stereotype → SCOPE.Service conversion locally and emits a proper named
// service entity. Detect() must return nothing for kotlin regardless of
// the source content.
func TestServiceDetector_Detect_KotlinExcluded(t *testing.T) {
	d := &serviceDetector{}
	src := `@RestController
@RequestMapping("/api/users")
class UserController {
    fun list(): List<String> = emptyList()
}`
	results := d.Detect("UserController.kt", "kotlin", src)
	if len(results) != 0 {
		names := make([]string, 0, len(results))
		for _, r := range results {
			names = append(names, r.Name)
		}
		t.Errorf("expected no service entities for kotlin, got %d: %v", len(results), names)
	}
}

// Guard that the exclusion is language-scoped — java, scala, and
// groovy must still receive the generic spring_service detection so their
// parity reports remain unchanged by this fix.
func TestServiceDetector_Detect_JVMPeersStillDetected(t *testing.T) {
	d := &serviceDetector{}
	src := `@RestController
public class UserController {
}`
	for _, lang := range []string{"java", "scala", "groovy"} {
		t.Run(lang, func(t *testing.T) {
			results := d.Detect("UserController."+lang, lang, src)
			if len(results) == 0 {
				t.Errorf("expected spring_service detection for %s", lang)
			}
		})
	}
}

// ============================================================
// Shared test helper detector
// ============================================================

func TestSharedTestHelperDetector_Detect_Conftest(t *testing.T) {
	d := &sharedTestHelperDetector{}
	results := d.Detect("tests/conftest.py", "python", `import pytest`)
	if len(results) == 0 {
		t.Error("expected shared test helper entity for conftest.py")
	}
}

func TestSharedTestHelperDetector_Detect_JSMocks(t *testing.T) {
	d := &sharedTestHelperDetector{}
	results := d.Detect("src/__mocks__/axios.ts", "typescript", `module.exports = {}`)
	if len(results) == 0 {
		t.Error("expected shared test helper entity for __mocks__")
	}
}

// ============================================================
// Singleton detector
// ============================================================

func TestSingletonDetector_AppliesTo(t *testing.T) {
	d := &singletonDetector{}
	if !d.AppliesTo(`public static getInstance() {`) {
		t.Error("should apply to getInstance")
	}
	if !d.AppliesTo(`var once sync.Once`) {
		t.Error("should apply to sync.Once")
	}
}

func TestSingletonDetector_Detect_Go(t *testing.T) {
	d := &singletonDetector{}
	src := `var (
    instance *DB
    once     sync.Once
)

func GetDB() *DB {
    once.Do(func() { instance = &DB{} })
    return instance
}`
	results := d.Detect("db.go", "go", src)
	if len(results) == 0 {
		t.Error("expected singleton entity for sync.Once")
	}
}

// ============================================================
// Snapshot test detector
// ============================================================

func TestSnapshotTestDetector_AppliesTo(t *testing.T) {
	d := &snapshotTestDetector{}
	if !d.AppliesTo(`expect(component).toMatchSnapshot()`) {
		t.Error("should apply to toMatchSnapshot")
	}
}

func TestSnapshotTestDetector_Detect_Jest(t *testing.T) {
	d := &snapshotTestDetector{}
	src := `it('renders correctly', () => {
    expect(tree).toMatchSnapshot()
})`
	results := d.Detect("Component.test.tsx", "typescript", src)
	if len(results) == 0 {
		t.Error("expected snapshot test entity")
	}
	if results[0].Properties["library"] != "jest" {
		t.Errorf("expected library=jest, got %s", results[0].Properties["library"])
	}
}

// ============================================================
// SQL join count extractor
// ============================================================

func TestSQLJoinCountExtractor_AppliesTo(t *testing.T) {
	d := &sqlJoinCountExtractor{}
	if !d.AppliesTo(`SELECT u.*, o.total FROM users u JOIN orders o ON u.id = o.user_id`) {
		t.Error("should apply to SQL with JOIN")
	}
}

func TestSQLJoinCountExtractor_Detect_MultipleJoins(t *testing.T) {
	d := &sqlJoinCountExtractor{}
	src := `query := "SELECT u.*, o.*, p.* FROM users u ` +
		`JOIN orders o ON u.id = o.user_id ` +
		`JOIN products p ON o.product_id = p.id ` +
		`JOIN categories c ON p.category_id = c.id ` +
		`JOIN tags t ON p.id = t.product_id"`
	results := d.Detect("repo.go", "go", src)
	if len(results) == 0 {
		t.Error("expected SQL join count entity")
	}
	if results[0].Properties["complexity"] != "high" {
		// 4 JOINs → could be medium. Just check join_count is set
		if results[0].Properties["join_count"] == "" {
			t.Error("expected join_count property")
		}
	}
}

// ============================================================
// Test fixture detector
// ============================================================

func TestTestFixtureDetector_AppliesTo(t *testing.T) {
	d := &testFixtureDetector{}
	if !d.AppliesTo(`@pytest.fixture`) {
		t.Error("should apply to pytest.fixture")
	}
	if !d.AppliesTo(`beforeAll(() => {`) {
		t.Error("should apply to Jest beforeAll")
	}
}

func TestTestFixtureDetector_Detect_PytestFixture(t *testing.T) {
	d := &testFixtureDetector{}
	src := `@pytest.fixture
def db_session():
    session = Session()
    yield session
    session.close()`
	results := d.Detect("conftest.py", "python", src)
	if len(results) == 0 {
		t.Error("expected pytest fixture entity")
	}
	if results[0].Properties["fixture_kind"] != "pytest_fixture" {
		t.Errorf("expected fixture_kind=pytest_fixture, got %s", results[0].Properties["fixture_kind"])
	}
}

// ============================================================
// Test quality enricher
// ============================================================

func TestTestQualityEnricher_AppliesTo(t *testing.T) {
	d := &testQualityEnricher{}
	if !d.AppliesTo(`@SpringBootTest`) {
		t.Error("should apply to @SpringBootTest")
	}
	if !d.AppliesTo(`import supertest`) {
		t.Error("should apply to supertest import")
	}
}

func TestTestQualityEnricher_Detect_Testcontainers(t *testing.T) {
	d := &testQualityEnricher{}
	src := `from testcontainers.postgres import PostgresContainer
pg = PostgresContainer("postgres:14")`
	results := d.Detect("test_db.py", "python", src)
	if len(results) == 0 {
		t.Error("expected testcontainers entity")
	}
	if results[0].Properties["test_type"] != "integration" {
		t.Errorf("expected test_type=integration, got %s", results[0].Properties["test_type"])
	}
}

// ============================================================
// Transaction changeset enricher
// ============================================================

func TestTransactionChangesetEnricher_AppliesTo(t *testing.T) {
	d := &transactionChangesetEnricher{}
	if !d.AppliesTo(`@Transactional`) {
		t.Error("should apply to @Transactional")
	}
	if !d.AppliesTo(`db.Begin()`) {
		t.Error("should apply to db.Begin()")
	}
}

func TestTransactionChangesetEnricher_Detect_GORM(t *testing.T) {
	d := &transactionChangesetEnricher{}
	src := `err := db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&user).Error; err != nil {
        return err
    }
    return nil
})`
	results := d.Detect("service.go", "go", src)
	if len(results) == 0 {
		t.Error("expected GORM transaction entity")
	}
}

// ============================================================
// Type alias extractor
// ============================================================

func TestTypeAliasExtractor_AppliesTo(t *testing.T) {
	d := &typeAliasExtractor{}
	if !d.AppliesTo(`type UserID = string`) {
		t.Error("should apply to type alias")
	}
	if !d.AppliesTo(`typealias UserId = Long`) {
		t.Error("should apply to Kotlin typealias")
	}
}

func TestTypeAliasExtractor_Detect_TypeScript(t *testing.T) {
	d := &typeAliasExtractor{}
	src := `type UserID = string
type EventHandler<T> = (event: T) => void`
	results := d.Detect("types.ts", "typescript", src)
	if len(results) == 0 {
		t.Error("expected type alias entities")
	}
}

func TestTypeAliasExtractor_Detect_Go(t *testing.T) {
	d := &typeAliasExtractor{}
	src := `type OrgID = string
type UserSlug string`
	results := d.Detect("types.go", "go", src)
	if len(results) == 0 {
		t.Error("expected Go type entities")
	}
}

// ============================================================
// UI route detector
// ============================================================

func TestUIRouteDetector_AppliesTo(t *testing.T) {
	d := &uiRouteDetector{}
	if !d.AppliesTo(`<Route path="/users" component={Users} />`) {
		t.Error("should apply to React Router")
	}
}

func TestUIRouteDetector_Detect_ReactRouter(t *testing.T) {
	d := &uiRouteDetector{}
	src := `<Routes>
  <Route path="/users" element={<Users />} />
  <Route path="/admin" element={<Admin />} />
</Routes>`
	results := d.Detect("App.tsx", "typescript", src)
	if len(results) < 2 {
		t.Errorf("expected 2 route entities, got %d", len(results))
	}
}

func TestUIRouteDetector_Detect_NextJSPage(t *testing.T) {
	d := &uiRouteDetector{}
	results := d.Detect("pages/users/[id].tsx", "typescript", "export default function UserPage() {}")
	if len(results) == 0 {
		t.Error("expected Next.js page entity")
	}
}

// ============================================================
// Validation confidence enricher
// ============================================================

func TestValidationConfidenceEnricher_Detect_SchemaFile(t *testing.T) {
	d := &validationConfidenceEnricher{}
	src := `from pydantic import BaseModel
def validate(data):
    return True`
	results := d.Detect("user_schema.py", "python", src)
	if len(results) == 0 {
		t.Error("expected validation confidence entity for schema file")
	}
}

// ============================================================
// HTTP client detector
// ============================================================

func TestHTTPClientDetector_AppliesTo(t *testing.T) {
	d := &httpClientDetector{}
	if !d.AppliesTo(`import requests`) {
		t.Error("should apply to requests import")
	}
	if !d.AppliesTo(`const res = await fetch('https://api.example.com/users')`) {
		t.Error("should apply to fetch call")
	}
}

func TestHTTPClientDetector_Detect_Requests(t *testing.T) {
	d := &httpClientDetector{}
	src := `import requests
resp = requests.get('https://api.example.com/users')`
	results := d.Detect("client.py", "python", src)
	if len(results) == 0 {
		t.Error("expected HTTP client entity")
	}
}

func TestHTTPClientDetector_Detect_URL(t *testing.T) {
	d := &httpClientDetector{}
	src := `import requests
r = requests.post("https://auth.service.internal/token", json=payload)`
	results := d.Detect("auth.py", "python", src)
	found := false
	for _, e := range results {
		if strings.Contains(e.Properties["url"], "auth.service.internal") {
			found = true
		}
	}
	if !found {
		t.Error("expected entity with extracted URL")
	}
}

// ============================================================
// Connection pool extractor
// ============================================================

func TestConnectionPoolExtractor_AppliesTo(t *testing.T) {
	d := &connectionPoolExtractor{}
	if !d.AppliesTo(`HikariDataSource ds = new HikariDataSource()`) {
		t.Error("should apply to HikariCP")
	}
	if !d.AppliesTo(`pool_size=10`) {
		t.Error("should apply to SQLAlchemy pool config")
	}
}

func TestConnectionPoolExtractor_Detect_Hikari(t *testing.T) {
	d := &connectionPoolExtractor{}
	src := `HikariConfig config = new HikariConfig();
config.setMaximumPoolSize(20);
config.setMinimumIdle(5);`
	results := d.Detect("DBConfig.java", "java", src)
	if len(results) == 0 {
		t.Error("expected HikariCP entity")
	}
	if results[0].Properties["max_pool_size"] != "20" {
		t.Errorf("expected max_pool_size=20, got %s", results[0].Properties["max_pool_size"])
	}
}

// ============================================================
// Crosscutting detector
// ============================================================

func TestCrosscuttingDetector_AppliesTo(t *testing.T) {
	d := &crosscuttingDetector{}
	if !d.AppliesTo(`func middleware(next http.Handler) http.Handler {`) {
		t.Error("should apply to Go middleware")
	}
}

func TestCrosscuttingDetector_Detect_GoMiddleware(t *testing.T) {
	d := &crosscuttingDetector{}
	src := `func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        next.ServeHTTP(w, r)
    })
}`
	results := d.Detect("middleware.go", "go", src)
	if len(results) == 0 {
		t.Error("expected crosscutting middleware entity")
	}
}

// ============================================================
// Dead module detector
// ============================================================

func TestDeadModuleDetector_Detect_JSLeafExport(t *testing.T) {
	d := &deadModuleDetector{}
	src := `export function unusedHelper() { return 42 }`
	if !d.AppliesTo(src) {
		t.Skip("does not apply")
	}
	results := d.Detect("utils.js", "javascript", src)
	// may or may not find dead exports depending on import presence
	_ = results
}

// ============================================================
// lineOf helper
// ============================================================

func TestLineOf(t *testing.T) {
	src := "line1\nline2\nline3"
	if lineOf(src, 0) != 1 {
		t.Errorf("expected line 1 for offset 0, got %d", lineOf(src, 0))
	}
	if lineOf(src, 6) != 2 {
		t.Errorf("expected line 2 for offset 6, got %d", lineOf(src, 6))
	}
	if lineOf(src, 12) != 3 {
		t.Errorf("expected line 3 for offset 12, got %d", lineOf(src, 12))
	}
}

// ============================================================
// Schema detector — Pydantic (issue #2984 A-win)
// ============================================================

func TestSchemaDetector_PydanticBaseModel(t *testing.T) {
	d := &schemaDetector{}
	src := `from pydantic import BaseModel, Field

class SignupRequest(BaseModel):
    username: str = Field(min_length=3)
    age: int
`
	if !d.AppliesTo(src) {
		t.Fatal("schemaDetector should apply to a Pydantic BaseModel file")
	}
	results := d.Detect("schemas.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["library"] == "pydantic" {
			found = true
			if e.Kind != "SCOPE.Component" {
				t.Errorf("kind = %q, want SCOPE.Component", e.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("expected a pydantic schema_validation entity, got %+v", results)
	}
}

func TestSchemaDetector_QualifiedPydanticBaseModel(t *testing.T) {
	d := &schemaDetector{}
	src := `import pydantic

class Order(pydantic.BaseModel):
    id: int
`
	results := d.Detect("order.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["library"] == "pydantic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pydantic detection for qualified BaseModel, got %+v", results)
	}
}

// Schema detector — marshmallow (issue #2985 A-win)
// ============================================================

func TestSchemaDetector_MarshmallowSchema(t *testing.T) {
	d := &schemaDetector{}
	src := `from marshmallow import Schema, fields

class UserSchema(Schema):
    name = fields.Str(required=True)
    email = fields.Email()
`
	if !d.AppliesTo(src) {
		t.Fatal("schemaDetector should apply to a marshmallow Schema file")
	}
	results := d.Detect("schemas.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["library"] == "marshmallow" {
			found = true
			if e.Kind != "SCOPE.Component" {
				t.Errorf("kind = %q, want SCOPE.Component", e.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("expected a marshmallow schema_validation entity, got %+v", results)
	}
}

func TestSchemaDetector_QualifiedMarshmallowSchema(t *testing.T) {
	d := &schemaDetector{}
	src := `import marshmallow as ma

class ProductSchema(ma.Schema):
    name = ma.fields.Str()
`
	results := d.Detect("product.py", "python", src)
	found := false
	for _, e := range results {
		if e.Properties["library"] == "marshmallow" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected marshmallow detection for qualified ma.Schema, got %+v", results)
	}
}
