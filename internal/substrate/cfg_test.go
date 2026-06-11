package substrate

import "testing"

func nodesByShape(g *ControlFlowGraph, shape CFGNodeShape) []CFGNode {
	var out []CFGNode
	for _, n := range g.Nodes {
		if n.Shape == shape {
			out = append(out, n)
		}
	}
	return out
}

func hasEdgeKind(g *ControlFlowGraph, kind CFGEdgeKind) bool {
	for _, e := range g.Edges {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestCFGPython proves the on-demand CFG of a small Django-shaped function has
// the right decision node + condition, the loop header + back-edge, the
// terminal return wired to the exit, and an effect annotation on a process node.
func TestCFGPython(t *testing.T) {
	src := `def sync(self, request):
    rows = Contact.objects.filter(active=True)
    if request.data.get("force"):
        Contact.objects.create(name="x")
    for row in rows:
        requests.post("https://api.example.com/notify", json={"id": row.id})
    return Response({"ok": True})
`
	g := BuildControlFlowGraph("python", src, 100)

	if !g.Supported {
		t.Fatalf("python CFG should be supported")
	}
	if g.Cyclomatic < 3 {
		t.Errorf("cyclomatic = %d; want >= 3", g.Cyclomatic)
	}

	// Decision node for the `if`, carrying its condition.
	decs := nodesByShape(g, ShapeDecision)
	if len(decs) == 0 {
		t.Fatalf("no decision node; nodes=%+v", g.Nodes)
	}
	foundCond := false
	for _, d := range decs {
		if d.Condition != "" {
			foundCond = true
		}
	}
	if !foundCond {
		t.Errorf("decision node missing condition text; decisions=%+v", decs)
	}

	// Loop node + back-edge.
	if len(nodesByShape(g, ShapeLoop)) == 0 {
		t.Errorf("no loop node; nodes=%+v", g.Nodes)
	}
	if !hasEdgeKind(g, EdgeBack) {
		t.Errorf("no loop back-edge; edges=%+v", g.Edges)
	}

	// Terminal return wired to exit.
	if len(nodesByShape(g, ShapeReturn)) == 0 {
		t.Errorf("no return terminal; nodes=%+v", g.Nodes)
	}
	if !hasEdgeKind(g, EdgeExit) {
		t.Errorf("no exit edge from terminal; edges=%+v", g.Edges)
	}

	// Effect annotation present on some process node (the db_write / http_out).
	gotEffect := false
	for _, n := range g.Nodes {
		if len(n.Effects) > 0 {
			gotEffect = true
		}
	}
	if !gotEffect {
		t.Errorf("expected an effect-annotated node; nodes=%+v", g.Nodes)
	}

	// Start/end bookends.
	if len(nodesByShape(g, ShapeStart)) != 1 || len(nodesByShape(g, ShapeEnd)) != 1 {
		t.Errorf("want exactly one start and one end node; nodes=%+v", g.Nodes)
	}
}

// TestCFGTypeScript proves the same on a small NestJS-shaped TS function with an
// if/else, a for loop, an effect, and an early return.
func TestCFGTypeScript(t *testing.T) {
	src := `async function handle(req: Request): Promise<Response> {
  const rows = await this.repo.find({ active: true });
  if (req.force) {
    await this.repo.save({ name: "x" });
  } else {
    return badRequest();
  }
  for (const row of rows) {
    await fetch("https://api.example.com/notify", { method: "POST" });
  }
  return ok(rows);
}
`
	g := BuildControlFlowGraph("jsts", src, 10)

	if !g.Supported {
		t.Fatalf("jsts CFG should be supported")
	}
	if len(nodesByShape(g, ShapeDecision)) == 0 {
		t.Fatalf("no decision node; nodes=%+v", g.Nodes)
	}
	if len(nodesByShape(g, ShapeLoop)) == 0 {
		t.Errorf("no loop node; nodes=%+v", g.Nodes)
	}
	if !hasEdgeKind(g, EdgeBack) {
		t.Errorf("no loop back-edge; edges=%+v", g.Edges)
	}
	if len(nodesByShape(g, ShapeReturn)) < 1 {
		t.Errorf("expected return terminal(s); nodes=%+v", g.Nodes)
	}
	if !hasEdgeKind(g, EdgeExit) {
		t.Errorf("no exit edge; edges=%+v", g.Edges)
	}
	// Condition text carried on a decision.
	hasCond := false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition != "" {
			hasCond = true
		}
	}
	if !hasCond {
		t.Errorf("no decision carried condition text; nodes=%+v", g.Nodes)
	}
}

// TestCFGCacheKeyedBySource proves the on-demand cache returns a stable graph
// for an unchanged source and rebuilds when the source changes.
func TestCFGCacheKeyedBySource(t *testing.T) {
	src := "def f():\n    return 1\n"
	a := BuildControlFlowGraphCached("ent1", "python", src, 1)
	b := BuildControlFlowGraphCached("ent1", "python", src, 1)
	if a != b {
		t.Errorf("cache should return the identical pointer for unchanged source")
	}
	c := BuildControlFlowGraphCached("ent1", "python", "def f():\n    return 2\n", 1)
	if c == a {
		t.Errorf("changed source must rebuild (different hash)")
	}
}

// TestCFGUnsupportedLanguageDegenerates proves an unknown language still returns
// a valid degenerate CFG (start → end) rather than erroring.
func TestCFGUnsupportedLanguageDegenerates(t *testing.T) {
	g := BuildControlFlowGraph("cobol", "PROCEDURE DIVISION.\n", 1)
	if g.Supported {
		t.Errorf("cobol has no brace/indent block detector; Supported should be false")
	}
	if len(nodesByShape(g, ShapeStart)) != 1 || len(nodesByShape(g, ShapeEnd)) != 1 {
		t.Errorf("degenerate CFG must still have start+end; nodes=%+v", g.Nodes)
	}
}
