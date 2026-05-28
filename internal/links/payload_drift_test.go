// Tests for the Phase 2A payload-shape drift detector (#2770).
//
// Each test builds an in-memory two-repo fixture: a producer with a
// known request/response shape and a consumer that talks to it. The
// HTTP pass is bypassed — we manually write a MethodHTTP link record
// to disk so the drift pass has something to walk. This keeps the
// test focused on the drift detector itself rather than the cross-
// repo matching it depends on.
package links

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPayloadDrift_MissingFieldsOnBothSides exercises the canonical
// case from the issue: consumer sends a field the producer doesn't
// read, AND the producer reads a field the consumer doesn't send.
func TestPayloadDrift_MissingFieldsOnBothSides(t *testing.T) {
	root := t.TempDir()
	// Producer: a Python handler that reads {name, age} off request.data.
	writeFile(t, root, "server/handler.py", `
def create_user(request):
    name = request.data["name"]
    age = request.data["age"]
    return Response({"id": 1, "name": name})
`)
	// Consumer: a JS function that posts {name, email}. `email` is
	// missing on the producer's read set; `age` is missing on the
	// consumer's send set.
	writeFile(t, root, "client/submit.ts", `
function submit() {
  axios.post("/api/users", { name: "x", email: "y" });
}
`)

	graphs := []repoGraph{
		{
			Repo:     "server",
			FileRoot: root,
			Entities: []entityNode{
				{ID: "h1", Name: "create_user", Kind: "SCOPE.Function", SourceFile: "server/handler.py"},
				{ID: "ep1", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint",
					Properties: map[string]string{"pattern_type": "http_endpoint_synthesis", "verb": "POST", "path": "/api/users"}},
			},
			Edges: []edgeRef{
				{FromID: "h1", ToID: "ep1", Kind: "IMPLEMENTS"},
			},
		},
		{
			Repo:     "client",
			FileRoot: root,
			Entities: []entityNode{
				{ID: "c1", Name: "submit", Kind: "SCOPE.Function", SourceFile: "client/submit.ts"},
				{ID: "ep2", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint_call",
					Properties: map[string]string{"pattern_type": "http_endpoint_client_synthesis", "verb": "POST", "path": "/api/users"}},
			},
			Edges: []edgeRef{
				{FromID: "c1", ToID: "ep2", Kind: "CALLS"},
			},
		},
	}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	// Pre-write a MethodHTTP link record so the drift pass has
	// something to join against.
	mustWriteHTTPLink(t, paths.Links, Link{
		ID:       "L1",
		Source:   entityKey("client", "ep2"),
		Target:   entityKey("server", "ep1"),
		Relation: RelationCalls,
		Method:   MethodHTTP,
	})

	res, err := runPayloadDriftPass("test-group", graphs, paths)
	if err != nil {
		t.Fatal(err)
	}
	if res.LinksAdded == 0 {
		t.Fatal("expected at least one drift finding; got 0")
	}
	findings := mustLoadDrift(t, paths)
	var reqDrift *SchemaDrift
	for i := range findings {
		if findings[i].Direction == "request" && findings[i].Severity == DriftSeverityHigh {
			reqDrift = &findings[i]
			break
		}
	}
	if reqDrift == nil {
		t.Fatalf("expected high-severity request drift; got %+v", findings)
	}
	if !containsString(reqDrift.MissingInProducer, "email") {
		t.Errorf("expected `email` missing on producer; got %v", reqDrift.MissingInProducer)
	}
	if !containsString(reqDrift.MissingInConsumer, "age") {
		t.Errorf("expected `age` missing on consumer; got %v", reqDrift.MissingInConsumer)
	}
}

// TestPayloadDrift_CaseNormaliseBridgesCamelVsSnake verifies the
// #2703 case-normalisation rule the issue calls out: camelCase
// consumer field names should not be flagged as drift against
// snake_case producer field names.
func TestPayloadDrift_CaseNormaliseBridgesCamelVsSnake(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/handler.py", `
def create_user(request):
    first_name = request.data["first_name"]
    last_name = request.data["last_name"]
    return Response({"id": 1})
`)
	writeFile(t, root, "client/submit.ts", `
function submit() {
  axios.post("/api/users", { firstName: "x", lastName: "y" });
}
`)
	graphs := []repoGraph{
		{
			Repo: "server", FileRoot: root,
			Entities: []entityNode{
				{ID: "h1", Name: "create_user", Kind: "SCOPE.Function", SourceFile: "server/handler.py"},
				{ID: "ep1", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint",
					Properties: map[string]string{"pattern_type": "http_endpoint_synthesis", "verb": "POST", "path": "/api/users"}},
			},
			Edges: []edgeRef{{FromID: "h1", ToID: "ep1", Kind: "IMPLEMENTS"}},
		},
		{
			Repo: "client", FileRoot: root,
			Entities: []entityNode{
				{ID: "c1", Name: "submit", Kind: "SCOPE.Function", SourceFile: "client/submit.ts"},
				{ID: "ep2", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint_call",
					Properties: map[string]string{"pattern_type": "http_endpoint_client_synthesis"}},
			},
			Edges: []edgeRef{{FromID: "c1", ToID: "ep2", Kind: "CALLS"}},
		},
	}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	mustWriteHTTPLink(t, paths.Links, Link{
		ID: "L1", Source: entityKey("client", "ep2"), Target: entityKey("server", "ep1"),
		Relation: RelationCalls, Method: MethodHTTP,
	})

	_, err := runPayloadDriftPass("test-group", graphs, paths)
	if err != nil {
		t.Fatal(err)
	}
	findings := mustLoadDrift(t, paths)
	// Request-direction findings should NOT report firstName / lastName
	// as drift — the case-normalise rule should bridge them.
	for _, f := range findings {
		if f.Direction != "request" {
			continue
		}
		for _, m := range f.MissingInProducer {
			if strings.EqualFold(m, "firstname") || strings.EqualFold(m, "lastname") {
				t.Errorf("case-normalise failed: %q reported as drift; finding=%+v", m, f)
			}
		}
		for _, m := range f.MissingInConsumer {
			if strings.EqualFold(m, "first_name") || strings.EqualFold(m, "last_name") {
				t.Errorf("case-normalise failed: %q reported as drift; finding=%+v", m, f)
			}
		}
	}
}

// TestPayloadDrift_NoFindingsWhenShapesAgree verifies the pass emits
// nothing when producer and consumer agree on the field set.
func TestPayloadDrift_NoFindingsWhenShapesAgree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server/handler.py", `
def create_user(request):
    name = request.data["name"]
    return Response({"id": 1})
`)
	writeFile(t, root, "client/submit.ts", `
function submit() {
  axios.post("/api/users", { name: "x" });
}
`)
	graphs := []repoGraph{
		{
			Repo: "server", FileRoot: root,
			Entities: []entityNode{
				{ID: "h1", Name: "create_user", Kind: "SCOPE.Function", SourceFile: "server/handler.py"},
				{ID: "ep1", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint",
					Properties: map[string]string{"pattern_type": "http_endpoint_synthesis"}},
			},
			Edges: []edgeRef{{FromID: "h1", ToID: "ep1", Kind: "IMPLEMENTS"}},
		},
		{
			Repo: "client", FileRoot: root,
			Entities: []entityNode{
				{ID: "c1", Name: "submit", Kind: "SCOPE.Function", SourceFile: "client/submit.ts"},
				{ID: "ep2", Name: "http:POST:/api/users", Kind: "synthetic.http_endpoint_call"},
			},
			Edges: []edgeRef{{FromID: "c1", ToID: "ep2", Kind: "CALLS"}},
		},
	}
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	mustWriteHTTPLink(t, paths.Links, Link{
		ID: "L1", Source: entityKey("client", "ep2"), Target: entityKey("server", "ep1"),
		Relation: RelationCalls, Method: MethodHTTP,
	})

	_, err := runPayloadDriftPass("test-group", graphs, paths)
	if err != nil {
		t.Fatal(err)
	}
	findings := mustLoadDrift(t, paths)
	// Only the response direction should be findings-worthy (consumer
	// has no observable shape); request must be drift-free.
	for _, f := range findings {
		if f.Direction == "request" && f.Severity == DriftSeverityHigh {
			t.Errorf("unexpected high-severity request drift: %+v", f)
		}
	}
}

// TestPayloadDrift_NoSourceFilesIsNoOp verifies the pass cleans up
// stale sidecars when no T1 source is on disk.
func TestPayloadDrift_NoSourceFilesIsNoOp(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Links: filepath.Join(root, "out", "links.json")}
	// Pre-stamp a sidecar so we can verify it gets cleared.
	if err := os.MkdirAll(filepath.Dir(paths.Links), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := DriftSidecarPath(paths)
	if err := os.WriteFile(stale, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	graphs := []repoGraph{{Repo: "x", FileRoot: root}}
	_, err := runPayloadDriftPass("test-group", graphs, paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("expected stale sidecar to be removed; err=%v", err)
	}
}

// mustWriteHTTPLink seeds the links.json file with one Link record so
// the drift pass has something to walk.
func mustWriteHTTPLink(t *testing.T, path string, l Link) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := Document{Version: SchemaVersion, Links: []Link{l}}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustLoadDrift(t *testing.T, paths Paths) []SchemaDrift {
	t.Helper()
	fs, err := LoadDriftFindings(paths)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
