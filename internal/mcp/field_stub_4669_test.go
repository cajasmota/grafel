package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// field_stub_4669_test.go — #4669 field-level partial-stub signal surfaced on
// the stub_detector endpoint record. Exercises the real handleStubDetector
// end-to-end with an on-disk handler source so the partial_stub_fields facet is
// computed from actual source (fixtures lie at merge → drive the real handler).
//
// The marquee upvate cases the rewrite agent flagged:
//   - get_extras: reads data but hardcodes cat1/cat5 fields → those flagged.
//   - checklists: part_id:null → part_id flagged.

// stubFieldServer builds a two-group server where the v3 repo's LoadedRepo
// Path points at dir (so handler source reads resolve), with the given v3/oracle
// docs. Mirrors stubTwoGroupServer but wires the on-disk Path.
func stubFieldServer(t *testing.T, dir string, v3, oracle *graph.Document) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	reg := &Registry{Groups: map[string]RegistryGroup{
		"v3":     {Repos: map[string]RegistryRepo{"r": {Path: dir}}},
		"oracle": {Repos: map[string]RegistryRepo{"r": {Path: dir}}},
	}}
	st := NewState(reg)
	st.mu.Lock()
	st.groups["v3"] = &LoadedGroup{
		Name:  "v3",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: v3, Path: dir}},
	}
	st.groups["oracle"] = &LoadedGroup{
		Name:  "oracle",
		Repos: map[string]*LoadedRepo{"r": {Repo: "r", Doc: oracle, Path: dir}},
	}
	st.mu.Unlock()
	return &Server{State: st, Tel: NewTelemetry(0)}
}

func TestStubDetector_PartialStubFields_getExtras(t *testing.T) {
	dir := t.TempDir()
	// A DRF handler that reads (db_read) but hardcodes cat1/cat5 scheduling
	// fields — the #763 get_extras shape.
	src := `class ClientViewSet:
    def get_extras(self, request):
        client = Client.objects.get(pk=request.GET["id"])
        return Response({
            "client_name": client.name,
            "cat1": 0,
            "cat5": 0,
            "count": Extra.objects.filter(client=client).count(),
        })
`
	srcPath := filepath.Join(dir, "client_viewset.py")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := graph.Entity{
		ID: "h1", Name: "ClientViewSet.get_extras",
		Kind: "SCOPE.Operation", SourceFile: "client_viewset.py",
		StartLine: 2, EndLine: 9,
		Properties: map[string]string{"effects": "db_read"},
	}
	v3 := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("d1", "GET", "/clients/get_extras"), handler},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("od1", "GET", "/clients/get_extras"), handlerFn("oh1", "oracle.get_extras", "db_read")},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubFieldServer(t, dir, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": {"db_read"}})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_read"}})

	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})
	r := resultFor(t, out, "GET /clients/get_extras")

	if r["partial_stub_supported"] != true {
		t.Fatalf("partial_stub_supported = %v, want true", r["partial_stub_supported"])
	}
	fields, _ := r["partial_stub_fields"].([]any)
	got := map[string]string{}
	for _, f := range fields {
		m := f.(map[string]any)
		got[m["field"].(string)] = m["literal_value"].(string)
	}
	if _, ok := got["cat1"]; !ok {
		t.Errorf("expected cat1 flagged; got %v", got)
	}
	if _, ok := got["cat5"]; !ok {
		t.Errorf("expected cat5 flagged; got %v", got)
	}
	if _, ok := got["count"]; ok {
		t.Errorf("count is derived (db count) and must NOT be flagged; got %v", got)
	}
	if _, ok := got["client_name"]; ok {
		t.Errorf("client_name is derived (model attr) and must NOT be flagged; got %v", got)
	}
	// The endpoint-level verdict still classifies it implemented (it has
	// db_read on both sides) — the field signal COMPLEMENTS, doesn't replace.
	if r["verdict"] == "likely_stub" {
		t.Errorf("endpoint verdict should not be likely_stub here (has db_read); got %v", r["verdict"])
	}
}

func TestStubDetector_PartialStubFields_checklistsPartId(t *testing.T) {
	dir := t.TempDir()
	src := `class ChecklistViewSet:
    def list(self, request):
        items = Checklist.objects.all()
        return Response({"part_id": None, "name": items[0].name})
`
	if err := os.WriteFile(filepath.Join(dir, "checklist.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := graph.Entity{
		ID: "h1", Name: "ChecklistViewSet.list",
		Kind: "SCOPE.Operation", SourceFile: "checklist.py",
		StartLine: 2, EndLine: 5,
		Properties: map[string]string{"effects": "db_read"},
	}
	v3 := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("d1", "GET", "/checklists"), handler},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("od1", "GET", "/checklists"), handlerFn("oh1", "oracle.list", "db_read")},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubFieldServer(t, dir, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": {"db_read"}})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_read"}})

	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})
	r := resultFor(t, out, "GET /checklists")
	fields, _ := r["partial_stub_fields"].([]any)
	if len(fields) != 1 {
		t.Fatalf("expected exactly 1 flagged field (part_id), got %+v", fields)
	}
	m := fields[0].(map[string]any)
	if m["field"] != "part_id" || m["literal_value"] != "None" {
		t.Fatalf("expected part_id=None, got %+v", m)
	}
}

// A non-flagship language with no field analyzer reports supported=false rather
// than "no literal fields" (honest-partial).
func TestStubDetector_PartialStubFields_unsupportedLanguage(t *testing.T) {
	dir := t.TempDir()
	handler := graph.Entity{
		ID: "h1", Name: "h", Kind: "SCOPE.Operation",
		SourceFile: "handler.rb", StartLine: 1, EndLine: 3,
		Properties: map[string]string{"effects": "db_read"},
	}
	v3 := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("d1", "GET", "/x"), handler},
		Relationships: []graph.Relationship{implementsEdge("h1", "d1")},
	}
	oracle := &graph.Document{Repo: "r",
		Entities:      []graph.Entity{endpointDef("od1", "GET", "/x"), handlerFn("oh1", "o", "db_read")},
		Relationships: []graph.Relationship{implementsEdge("oh1", "od1")},
	}
	s := stubFieldServer(t, dir, v3, oracle)
	writeEffectsSidecar(t, "v3", map[string][]string{"r::h1": {"db_read"}})
	writeEffectsSidecar(t, "oracle", map[string][]string{"r::oh1": {"db_read"}})
	out := callStubDetector(t, s, map[string]any{"group_v3": "v3", "group_oracle": "oracle"})
	r := resultFor(t, out, "GET /x")
	if r["partial_stub_supported"] != false {
		t.Fatalf("ruby handler: partial_stub_supported = %v, want false", r["partial_stub_supported"])
	}
	if _, ok := r["partial_stub_fields"]; ok {
		t.Fatalf("unsupported language must not emit partial_stub_fields")
	}
}
