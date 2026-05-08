package httpclient

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runExtract(t *testing.T, lang, source string) []types.EntityRecord {
	t.Helper()
	e := &Extractor{}
	records, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "testfile",
		Content:  []byte(source),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	return records
}

func apiEntities(records []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range records {
		if r.Kind == "SCOPE.ExternalAPI" {
			out = append(out, r)
		}
	}
	return out
}

func callRels(records []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range records {
		if r.Kind == "relationship" {
			out = append(out, r.Relationships...)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript: fetch
// ---------------------------------------------------------------------------

func TestJS_Fetch(t *testing.T) {
	src := `fetch('https://api.example.com/users')`
	records := runExtract(t, "javascript", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	if apis[0].Name != "https://api.example.com/users" {
		t.Errorf("url=%q", apis[0].Name)
	}
}

func TestJS_FetchDoubleQuote(t *testing.T) {
	src := `fetch("https://api.example.com/data")`
	apis := apiEntities(runExtract(t, "javascript", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1, got %d", len(apis))
	}
}

func TestTS_Fetch(t *testing.T) {
	src := `const data = await fetch('https://api.example.com/v2/items');`
	apis := apiEntities(runExtract(t, "typescript", src))
	if len(apis) != 1 {
		t.Fatalf("expected 1, got %d", len(apis))
	}
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript: axios
// ---------------------------------------------------------------------------

func TestJS_AxiosGet(t *testing.T) {
	src := `axios.get('https://api.example.com/items')`
	records := runExtract(t, "javascript", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected at least 1 relationship")
	}
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "GET" {
			found = true
		}
	}
	if !found {
		t.Error("expected GET method in relationship properties")
	}
}

func TestJS_AxiosPost(t *testing.T) {
	src := `axios.post('https://api.example.com/create', payload)`
	records := runExtract(t, "javascript", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected at least 1 relationship")
	}
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "POST" {
			found = true
		}
	}
	if !found {
		t.Error("expected POST method in relationship properties")
	}
}

func TestJS_NoHTTPCalls(t *testing.T) {
	src := `function add(a, b) { return a + b; }`
	records := runExtract(t, "javascript", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Python: requests / httpx
// ---------------------------------------------------------------------------

func TestPython_RequestsGet(t *testing.T) {
	src := `import requests
resp = requests.get('https://api.example.com/users')
`
	records := runExtract(t, "python", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	if apis[0].Name != "https://api.example.com/users" {
		t.Errorf("url=%q", apis[0].Name)
	}
}

func TestPython_HttpxPost(t *testing.T) {
	src := `import httpx
response = httpx.post("https://service.example.com/submit", json=payload)
`
	records := runExtract(t, "python", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	rels := callRels(records)
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "POST" {
			found = true
		}
	}
	if !found {
		t.Error("expected POST method")
	}
}

func TestPython_MultipleRequests(t *testing.T) {
	src := `
requests.get('https://a.example.com/one')
requests.post('https://b.example.com/two', data=x)
`
	records := runExtract(t, "python", src)
	apis := apiEntities(records)
	if len(apis) != 2 {
		t.Fatalf("expected 2 API entities, got %d", len(apis))
	}
}

func TestPython_NoHTTP(t *testing.T) {
	src := `def compute(x): return x * 2`
	records := runExtract(t, "python", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Go: http.Get / http.Post / http.NewRequest
// ---------------------------------------------------------------------------

func TestGo_HttpGet(t *testing.T) {
	src := `resp, err := http.Get("https://api.example.com/ping")`
	records := runExtract(t, "go", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	rels := callRels(records)
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "GET" {
			found = true
		}
	}
	if !found {
		t.Error("expected GET method")
	}
}

func TestGo_HttpPost(t *testing.T) {
	src := `resp, err := http.Post("https://api.example.com/upload", "application/json", body)`
	records := runExtract(t, "go", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	rels := callRels(records)
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "POST" {
			found = true
		}
	}
	if !found {
		t.Error("expected POST method")
	}
}

func TestGo_HttpNewRequest(t *testing.T) {
	src := `req, _ := http.NewRequest("PUT", "https://api.example.com/update", body)`
	records := runExtract(t, "go", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1, got %d", len(apis))
	}
	rels := callRels(records)
	found := false
	for _, r := range rels {
		if r.Properties["http_method"] == "PUT" {
			found = true
		}
	}
	if !found {
		t.Error("expected PUT method")
	}
}

func TestGo_NoHTTP(t *testing.T) {
	src := `func main() { fmt.Println("hello") }`
	records := runExtract(t, "go", src)
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Java: restTemplate / URI.create
// ---------------------------------------------------------------------------

func TestJava_RestTemplate(t *testing.T) {
	src := `restTemplate.exchange("https://api.example.com/data", HttpMethod.GET, null, String.class);`
	records := runExtract(t, "java", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
}

func TestJava_URICreate(t *testing.T) {
	src := `URI uri = URI.create("https://api.example.com/resource");`
	records := runExtract(t, "java", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
	if apis[0].Name != "https://api.example.com/resource" {
		t.Errorf("url=%q", apis[0].Name)
	}
}

func TestKotlin_UsesJavaPatterns(t *testing.T) {
	src := `val response = restTemplate.exchange("https://service.example.com/api", ...)`
	records := runExtract(t, "kotlin", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Fatalf("expected 1 API entity, got %d", len(apis))
	}
}

// ---------------------------------------------------------------------------
// Protocol detection
// ---------------------------------------------------------------------------

func TestProtocol_GRPC(t *testing.T) {
	src := `fetch('grpc://internal.service:9090/pkg.Service/Method')`
	records := runExtract(t, "javascript", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected relationships")
	}
	if rels[0].Properties["protocol"] != "grpc" {
		t.Errorf("protocol=%q want grpc", rels[0].Properties["protocol"])
	}
}

func TestProtocol_WebSocket(t *testing.T) {
	src := `fetch('wss://realtime.example.com/ws')`
	records := runExtract(t, "javascript", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected relationships")
	}
	if rels[0].Properties["protocol"] != "websocket" {
		t.Errorf("protocol=%q want websocket", rels[0].Properties["protocol"])
	}
}

func TestProtocol_RestDefault(t *testing.T) {
	src := `http.Get("https://api.example.com/health")`
	records := runExtract(t, "go", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected relationships")
	}
	if rels[0].Properties["protocol"] != "rest" {
		t.Errorf("protocol=%q want rest", rels[0].Properties["protocol"])
	}
}

// ---------------------------------------------------------------------------
// URL deduplication
// ---------------------------------------------------------------------------

func TestDeduplication(t *testing.T) {
	src := `
requests.get("https://api.example.com/same")
requests.get("https://api.example.com/same")
`
	records := runExtract(t, "python", src)
	apis := apiEntities(records)
	if len(apis) != 1 {
		t.Errorf("expected 1 unique API entity (dedup), got %d", len(apis))
	}
	// Two relationship entries for two call sites
	rels := callRels(records)
	if len(rels) != 2 {
		t.Errorf("expected 2 relationship records for 2 call sites, got %d", len(rels))
	}
}

// ---------------------------------------------------------------------------
// Relationship properties
// ---------------------------------------------------------------------------

func TestRelProperties(t *testing.T) {
	src := `http.Get("https://example.com/test")`
	records := runExtract(t, "go", src)
	rels := callRels(records)
	if len(rels) == 0 {
		t.Fatal("expected relationships")
	}
	r := rels[0]
	if r.Kind != "CALLS" {
		t.Errorf("rel kind=%q want CALLS", r.Kind)
	}
	if r.Properties["kind"] != "external_http_call" {
		t.Errorf("kind prop=%q want external_http_call", r.Properties["kind"])
	}
	if r.Properties["url"] == "" {
		t.Error("url property missing")
	}
}

// ---------------------------------------------------------------------------
// Empty language falls back to all-patterns scan
// ---------------------------------------------------------------------------

func TestEmptyLanguage_AllPatterns(t *testing.T) {
	src := `http.Get("https://go.example.com/api")
requests.get("https://py.example.com/api")
`
	records := runExtract(t, "", src)
	apis := apiEntities(records)
	if len(apis) < 2 {
		t.Errorf("expected at least 2 API entities with empty language, got %d", len(apis))
	}
}
