package links

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// findLink returns the first link matching the predicate, or nil.
func findLink(links []Link, pred func(Link) bool) *Link {
	for i := range links {
		if pred(links[i]) {
			return &links[i]
		}
	}
	return nil
}

// runDataFlowForTest runs the pass over a single repo fixture and returns
// the emitted links (via the in-memory build, no sidecar write).
func runDataFlowForTest(t *testing.T, file, content string, entities []entityNode) []Link {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, file, content)
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: entities,
	}}
	// paths.Links == "" → no sidecar write, but we still need the links;
	// re-run the internal builder by invoking the pass and reading the
	// sidecar. Simpler: give a temp links path and read it back.
	linksPath := root + "/.archigraph/links.json"
	res, err := runDataFlowPass(graphs, Paths{Links: linksPath}, nil)
	if err != nil {
		t.Fatalf("pass error: %v", err)
	}
	_ = res
	doc := readDataFlowSidecar(t, linksPath)
	return doc
}

func readDataFlowSidecar(t *testing.T, linksPath string) []Link {
	t.Helper()
	sidecar := linksPath[:len(linksPath)-len(".json")] + "-data-flow.json"
	buf, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var doc dataFlowDocument
	if err := json.Unmarshal(buf, &doc); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	return doc.Links
}

func TestDataFlowPass_JSTS_DBWrite_EmitsEdge(t *testing.T) {
	content := `
function createUser(req, res) {
  const name = req.body.name;
  await User.create({ name });
}
`
	ents := []entityNode{
		{ID: "h1", Name: "createUser", Kind: "function", SourceFile: "src/users.ts"},
	}
	links := runDataFlowForTest(t, "src/users.ts", content, ents)
	l := findLink(links, func(l Link) bool {
		return l.Relation == string(types.RelationshipKindDataFlowsTo) &&
			l.Properties["sink_kind"] == "db_write"
	})
	if l == nil {
		t.Fatalf("expected a DATA_FLOWS_TO db_write link, got %+v", links)
	}
	if l.Source != "repo-a::h1" {
		t.Errorf("source = %q, want repo-a::h1", l.Source)
	}
	if l.Properties["field"] != "name" {
		t.Errorf("field = %q, want name", l.Properties["field"])
	}
	if l.Properties["sink"] != "User.create" {
		t.Errorf("sink = %q, want User.create", l.Properties["sink"])
	}
}

func TestDataFlowPass_Python_OneHop_ResolvesToCalleeEntity(t *testing.T) {
	content := "" +
		"def handler(request):\n" +
		"    x = request.data['x']\n" +
		"    persist(x)\n" +
		"\n" +
		"def persist(v):\n" +
		"    repo.insert(v)\n"
	ents := []entityNode{
		{ID: "h1", Name: "handler", Kind: "function", SourceFile: "app/views.py"},
		{ID: "h2", Name: "persist", Kind: "function", SourceFile: "app/views.py"},
	}
	links := runDataFlowForTest(t, "app/views.py", content, ents)
	l := findLink(links, func(l Link) bool {
		return l.Relation == string(types.RelationshipKindDataFlowsTo) &&
			l.Properties["hop_via"] == "persist"
	})
	if l == nil {
		t.Fatalf("expected a one-hop DATA_FLOWS_TO link, got %+v", links)
	}
	if l.Source != "repo-a::h1" {
		t.Errorf("source = %q, want repo-a::h1 (handler)", l.Source)
	}
	if l.Properties["field"] != "x" {
		t.Errorf("field = %q, want x", l.Properties["field"])
	}
	if l.Properties["sink_kind"] != "db_write" {
		t.Errorf("sink_kind = %q, want db_write", l.Properties["sink_kind"])
	}
}

func TestDataFlowPass_Negative_StaticValue_NoEdge(t *testing.T) {
	content := `
function createUser(req, res) {
  const name = 'static';
  await User.create({ name });
}
`
	ents := []entityNode{
		{ID: "h1", Name: "createUser", Kind: "function", SourceFile: "src/users.ts"},
	}
	links := runDataFlowForTest(t, "src/users.ts", content, ents)
	if len(links) != 0 {
		t.Fatalf("expected NO links for static value, got %+v", links)
	}
}

// runDataFlowMultiFile runs the pass over a multi-file fixture with explicit
// CALLS edges, returning the emitted links.
func runDataFlowMultiFile(t *testing.T, files map[string]string, entities []entityNode, edges []edgeRef) []Link {
	t.Helper()
	root := t.TempDir()
	for f, c := range files {
		writeFile(t, root, f, c)
	}
	graphs := []repoGraph{{
		Repo:     "repo-a",
		FileRoot: root,
		Entities: entities,
		Edges:    edges,
	}}
	linksPath := root + "/.archigraph/links.json"
	if _, err := runDataFlowPass(graphs, Paths{Links: linksPath}, nil); err != nil {
		t.Fatalf("pass error: %v", err)
	}
	return readDataFlowSidecar(t, linksPath)
}

func TestDataFlowPass_JSTS_CrossFile_DBWrite(t *testing.T) {
	// handler in handlers.ts calls imported save() defined in svc.ts.
	handler := `
import { save } from './svc';
function h(req, res) {
  save(req.body.x);
}
`
	svc := `
export function save(v) {
  Model.create({ v });
}
`
	files := map[string]string{
		"src/handlers.ts": handler,
		"src/svc.ts":      svc,
	}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/handlers.ts"},
		{ID: "save", Name: "save", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "create", Name: "create", Kind: "function", SourceFile: "src/svc.ts"},
	}
	// CALLS edge: handler h -> save (cross-file).
	edges := []edgeRef{{FromID: "h", ToID: "save", Kind: "calls"}}
	links := runDataFlowMultiFile(t, files, ents, edges)

	l := findLink(links, func(l Link) bool {
		return l.Relation == string(types.RelationshipKindDataFlowsTo) &&
			l.Properties["sink"] == "Model.create"
	})
	if l == nil {
		t.Fatalf("expected cross-file DATA_FLOWS_TO to Model.create, got %+v", links)
	}
	if l.Source != "repo-a::h" {
		t.Errorf("source = %q, want repo-a::h (origin handler)", l.Source)
	}
	if l.Properties["field"] != "x" {
		t.Errorf("field = %q, want x", l.Properties["field"])
	}
	if l.Properties["sink_kind"] != "db_write" {
		t.Errorf("sink_kind = %q, want db_write", l.Properties["sink_kind"])
	}
	if l.Properties["hop_path"] != "save" {
		t.Errorf("hop_path = %q, want save", l.Properties["hop_path"])
	}
	if l.Properties["hop_via"] != "save" {
		t.Errorf("hop_via = %q, want save", l.Properties["hop_via"])
	}
	// The sink must resolve to the real callee-file entity, not a residue.
	if l.Target != "repo-a::create" {
		t.Errorf("target = %q, want repo-a::create (sink resolved in svc.ts)", l.Target)
	}
}

func TestDataFlowPass_Python_CrossFile_DBWrite(t *testing.T) {
	views := "" +
		"from .svc import save\n" +
		"def handler(request):\n" +
		"    save(request.data['name'])\n"
	svc := "" +
		"def save(v):\n" +
		"    Account.objects.create(name=v)\n"
	files := map[string]string{
		"app/views.py": views,
		"app/svc.py":   svc,
	}
	ents := []entityNode{
		{ID: "handler", Name: "handler", Kind: "function", SourceFile: "app/views.py"},
		{ID: "save", Name: "save", Kind: "function", SourceFile: "app/svc.py"},
	}
	edges := []edgeRef{{FromID: "handler", ToID: "save", Kind: "calls"}}
	links := runDataFlowMultiFile(t, files, ents, edges)

	l := findLink(links, func(l Link) bool {
		return l.Relation == string(types.RelationshipKindDataFlowsTo) &&
			l.Properties["sink"] == "Account.objects.create"
	})
	if l == nil {
		t.Fatalf("expected cross-file DATA_FLOWS_TO to Account.objects.create, got %+v", links)
	}
	if l.Source != "repo-a::handler" {
		t.Errorf("source = %q, want repo-a::handler", l.Source)
	}
	if l.Properties["field"] != "name" {
		t.Errorf("field = %q, want name", l.Properties["field"])
	}
	if l.Properties["hop_path"] != "save" {
		t.Errorf("hop_path = %q, want save", l.Properties["hop_path"])
	}
}

func TestDataFlowPass_JSTS_CrossFile_TwoHop(t *testing.T) {
	// h -> (cross-file) a -> (in-file) b -> sink. hop_path = a>b.
	handler := `
import { a } from './svc';
function h(req, res) {
  a(req.body.x);
}
`
	svc := `
export function a(v) {
  b(v);
}
function b(w) {
  repo.insert(w);
}
`
	files := map[string]string{
		"src/h.ts":   handler,
		"src/svc.ts": svc,
	}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/h.ts"},
		{ID: "a", Name: "a", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "b", Name: "b", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "insert", Name: "insert", Kind: "function", SourceFile: "src/svc.ts"},
	}
	edges := []edgeRef{{FromID: "h", ToID: "a", Kind: "calls"}}
	links := runDataFlowMultiFile(t, files, ents, edges)
	l := findLink(links, func(l Link) bool {
		return l.Properties["sink"] == "repo.insert"
	})
	if l == nil {
		t.Fatalf("expected cross-file 2-hop flow to repo.insert, got %+v", links)
	}
	if l.Properties["hop_path"] != "a>b" {
		t.Errorf("hop_path = %q, want a>b", l.Properties["hop_path"])
	}
	if l.Source != "repo-a::h" {
		t.Errorf("source = %q, want repo-a::h", l.Source)
	}
}

func TestDataFlowPass_Negative_CrossFile_Unresolved_NoEdge(t *testing.T) {
	// No CALLS edge to an in-repo entity → import is external/unresolved → drop.
	handler := `
import { save } from 'external-lib';
function h(req, res) {
  save(req.body.x);
}
`
	files := map[string]string{"src/h.ts": handler}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/h.ts"},
	}
	links := runDataFlowMultiFile(t, files, ents, nil)
	if l := findLink(links, func(l Link) bool { return l.Properties["sink"] == "Model.create" }); l != nil {
		t.Fatalf("expected NO edge for unresolved import, got %+v", *l)
	}
	if len(links) != 0 {
		t.Fatalf("expected zero links, got %+v", links)
	}
}

func TestDataFlowPass_Negative_CrossFile_AmbiguousCallee_NoEdge(t *testing.T) {
	// Two distinct cross-file entities named save → ambiguous → drop.
	handler := `
import { save } from './svc';
function h(req, res) {
  save(req.body.x);
}
`
	svc := `
export function save(v) { Model.create({ v }); }
`
	other := `
export function save(v) { Other.create({ v }); }
`
	files := map[string]string{
		"src/h.ts":     handler,
		"src/svc.ts":   svc,
		"src/other.ts": other,
	}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/h.ts"},
		{ID: "save1", Name: "save", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "save2", Name: "save", Kind: "function", SourceFile: "src/other.ts"},
	}
	edges := []edgeRef{
		{FromID: "h", ToID: "save1", Kind: "calls"},
		{FromID: "h", ToID: "save2", Kind: "calls"},
	}
	links := runDataFlowMultiFile(t, files, ents, edges)
	if len(links) != 0 {
		t.Fatalf("expected NO link for ambiguous cross-file callee, got %+v", links)
	}
}

func TestDataFlowPass_Negative_CrossFile_FourthHopDropped(t *testing.T) {
	// h -(xfile)-> a -> b -> c -> sink. That is 4 hops (a,b,c are 3 in-file
	// after the cross-file entry... a is hop1, b hop2, c hop3, sink in c is
	// reached at 3 hops -> allowed). To exceed, chain one more: sink in d.
	handler := `
import { a } from './svc';
function h(req, res) {
  a(req.body.x);
}
`
	svc := `
export function a(v) { b(v); }
function b(w) { c(w); }
function c(z) { d(z); }
function d(q) { repo.insert(q); }
`
	files := map[string]string{"src/h.ts": handler, "src/svc.ts": svc}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/h.ts"},
		{ID: "a", Name: "a", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "b", Name: "b", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "c", Name: "c", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "d", Name: "d", Kind: "function", SourceFile: "src/svc.ts"},
	}
	edges := []edgeRef{{FromID: "h", ToID: "a", Kind: "calls"}}
	links := runDataFlowMultiFile(t, files, ents, edges)
	if l := findLink(links, func(l Link) bool { return l.Properties["sink"] == "repo.insert" }); l != nil {
		t.Fatalf("expected NO flow beyond 3 hops, got %+v", *l)
	}
}

func TestDataFlowPass_CrossFile_ThreeHop_Reaches(t *testing.T) {
	// h -(xfile)-> a -> b -> sink-in-b is 2 hops; sink directly in c via
	// a->b->c chain = 3 hops (the inclusive bound) must still reach.
	handler := `
import { a } from './svc';
function h(req, res) {
  a(req.body.x);
}
`
	svc := `
export function a(v) { b(v); }
function b(w) { c(w); }
function c(z) { repo.insert(z); }
`
	files := map[string]string{"src/h.ts": handler, "src/svc.ts": svc}
	ents := []entityNode{
		{ID: "h", Name: "h", Kind: "function", SourceFile: "src/h.ts"},
		{ID: "a", Name: "a", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "b", Name: "b", Kind: "function", SourceFile: "src/svc.ts"},
		{ID: "c", Name: "c", Kind: "function", SourceFile: "src/svc.ts"},
	}
	edges := []edgeRef{{FromID: "h", ToID: "a", Kind: "calls"}}
	links := runDataFlowMultiFile(t, files, ents, edges)
	l := findLink(links, func(l Link) bool { return l.Properties["sink"] == "repo.insert" })
	if l == nil {
		t.Fatalf("expected 3-hop cross-file flow to reach repo.insert, got %+v", links)
	}
	if l.Properties["hop_path"] != "a>b>c" {
		t.Errorf("hop_path = %q, want a>b>c", l.Properties["hop_path"])
	}
}
