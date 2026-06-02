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
