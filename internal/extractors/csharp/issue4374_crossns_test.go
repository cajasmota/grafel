// issue4374_crossns_test.go — end-to-end live-pipeline validation for #4374.
//
// Bug: C# cross-namespace calls reach a method through a qualifier on a
// member_access_expression — a fully-qualified
// `App.Services.Orders.OrderService.Place()`, an aliased
// `using Ord = App.Services.Orders; Ord.OrderService.Place()`, a
// `using static App.Services.Orders.OrderService; Place()`, a same-namespace
// static `OrderService.Create()`, or a `global::App.Services...Place()`. The
// extractor only typed a single-level receiver, so a multi-segment qualified
// call collapsed to the bare leaf method name (`Place`). The bare leaf resolves
// through the global byName index, which goes AMBIGUOUS the moment two
// namespaces define a same-named method/type (`OrderService.Place` in both
// App.Services.Orders and App.Services.Billing) — so the CALLS edge dropped and
// the callee namespace looked falsely uncalled. This is the C# analogue of the
// Go cross-package (#4332) and Rust cross-module (#4373) qualifier drops.
//
// These tests drive the REAL extraction + resolver passes — the same sequence
// cmd/grafel/index.go runs (extract per file → stamp deterministic IDs →
// BuildIndex → ResolveCSharpCrossNamespaceCalls → ReferencesEmbedded) — on a
// faithful multi-namespace project that reproduces a cross-namespace call WITH
// a same-named type/method collision in two namespaces. The collision is
// essential: a globally-unique name would false-pass through the bare-name
// fallback (the exact trap #4332 documents).
package csharp_test

import (
	"context"
	"testing"

	tscsharp "github.com/smacker/go-tree-sitter/csharp"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/csharp"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	"github.com/cajasmota/grafel/internal/types"
)

func extractCSharpProjectForTest(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	var merged []types.EntityRecord
	for rel, src := range files {
		parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tscsharp.GetLanguage()))
		if err != nil {
			t.Fatalf("parser init %s: %v", rel, err)
		}
		tree, err := parser.Parse([]byte(src))
		if err != nil {
			parser.Close()
			t.Fatalf("parse %s: %v", rel, err)
		}
		ents, err := ext.Extract(context.Background(), extractor.FileInput{
			Path: rel, Language: "csharp", Content: []byte(src), TSTree: tree,
		})
		parser.Close()
		if err != nil {
			t.Fatalf("extract %s: %v", rel, err)
		}
		merged = append(merged, ents...)
	}
	for k := range merged {
		if merged[k].Name == "" {
			continue
		}
		merged[k].ID = graph.EntityID("acme/shop", merged[k].Kind,
			merged[k].Name, merged[k].SourceFile)
	}
	return merged
}

func is16Hex(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func runCSharpResolve(merged []types.EntityRecord) int {
	idx := resolve.BuildIndex(merged)
	n := idx.ResolveCSharpCrossNamespaceCalls(merged)
	resolve.ReferencesEmbedded(merged, idx)
	return n
}

// callEdge returns the ToID of the CALLS edge whose call_leaf == leaf emitted
// from the operation named `caller` in `srcFile`.
func callEdge(merged []types.EntityRecord, srcFile, caller, leaf string) (string, map[string]string, bool) {
	for k := range merged {
		e := merged[k]
		if e.SourceFile != srcFile || e.Name != caller {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			if r.Properties["call_leaf"] == leaf || r.ToID == leaf {
				return r.ToID, r.Properties, true
			}
		}
	}
	return "", nil, false
}

func entID(merged []types.EntityRecord, srcFile, name string) string {
	for k := range merged {
		if merged[k].SourceFile == srcFile && merged[k].Name == name {
			return merged[k].ID
		}
	}
	return ""
}

// colliding callee namespaces: App.Services.Orders.OrderService.Place AND
// App.Services.Billing.OrderService.Place — same Type AND same method in two
// distinct namespaces. Only the namespace qualifier on the call can pick the
// right one.
func collidingCalleeFiles() map[string]string {
	return map[string]string{
		"src/Orders/OrderService.cs": "" +
			"namespace App.Services.Orders {\n" +
			"  public class OrderService {\n" +
			"    public void Place() {}\n" +
			"    public void Create() {}\n" +
			"  }\n" +
			"}\n",
		"src/Billing/OrderService.cs": "" +
			"namespace App.Services.Billing {\n" +
			"  public class OrderService {\n" +
			"    public void Place() {}\n" +
			"  }\n" +
			"}\n",
	}
}

func assertBindsTo(t *testing.T, merged []types.EntityRecord, srcFile, caller, leaf, wantID, wrongID, label string) {
	t.Helper()
	before, props, ok := callEdge(merged, srcFile, caller, leaf)
	if !ok {
		t.Fatalf("%s: no CALLS edge for leaf %q from %s", label, leaf, caller)
	}
	if props["csharp_call_ns"] == "" || props["csharp_call_type"] == "" {
		t.Fatalf("%s: extractor did not stamp csharp_call_ns/csharp_call_type (props=%v)", label, props)
	}
	t.Logf("%s: stamped ns=%q type=%q leaf=%q", label,
		props["csharp_call_ns"], props["csharp_call_type"], props["call_leaf"])
	if is16Hex(before) {
		t.Fatalf("%s precondition: edge already a hex id before resolve (%q)", label, before)
	}
	n := runCSharpResolve(merged)
	t.Logf("%s: ResolveCSharpCrossNamespaceCalls rewrote=%d", label, n)
	after, _, _ := callEdge(merged, srcFile, caller, leaf)
	t.Logf("%s: ToID before=%q after=%q (want=%q wrong=%q)", label, before, after, wantID, wrongID)
	switch after {
	case wantID:
		// correct
	case wrongID:
		t.Fatalf("%s: CALL bound to the WRONG namespace's entity (qualifier ignored)", label)
	default:
		t.Fatalf("%s: CALL unresolved/ambiguous after fix, ToID=%q", label, after)
	}
}

// TestIssue4374_FullyQualified_NameCollision — the core regression. The caller
// invokes `App.Services.Orders.OrderService.Place()`, and a second namespace
// (App.Services.Billing) ALSO defines OrderService.Place. Before the fix the
// qualifier was dropped and the bare name went ambiguous → the CALLS edge bound
// nowhere. After the fix it binds to exactly App.Services.Orders via the
// stamped namespace.
func TestIssue4374_FullyQualified_NameCollision(t *testing.T) {
	files := collidingCalleeFiles()
	files["src/Handlers/Handler.cs"] = "" +
		"namespace App.Handlers {\n" +
		"  public class Handler {\n" +
		"    public void Run() {\n" +
		"      App.Services.Orders.OrderService.Place();\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	merged := extractCSharpProjectForTest(t, files)
	want := entID(merged, "src/Orders/OrderService.cs", "OrderService.Place")
	wrong := entID(merged, "src/Billing/OrderService.cs", "OrderService.Place")
	if want == "" || wrong == "" {
		t.Fatalf("missing colliding Place entities: orders=%q billing=%q", want, wrong)
	}
	assertBindsTo(t, merged, "src/Handlers/Handler.cs", "Handler.Run", "Place", want, wrong, "fully-qualified")
}

// TestIssue4374_UsingAlias_NameCollision — `using Ord = App.Services.Orders;`
// then `Ord.OrderService.Place()`.
func TestIssue4374_UsingAlias_NameCollision(t *testing.T) {
	files := collidingCalleeFiles()
	files["src/Handlers/Handler.cs"] = "" +
		"using Ord = App.Services.Orders;\n" +
		"namespace App.Handlers {\n" +
		"  public class Handler {\n" +
		"    public void Run() {\n" +
		"      Ord.OrderService.Place();\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	merged := extractCSharpProjectForTest(t, files)
	want := entID(merged, "src/Orders/OrderService.cs", "OrderService.Place")
	wrong := entID(merged, "src/Billing/OrderService.cs", "OrderService.Place")
	assertBindsTo(t, merged, "src/Handlers/Handler.cs", "Handler.Run", "Place", want, wrong, "using-alias")
}

// TestIssue4374_UsingStatic_NameCollision — `using static
// App.Services.Orders.OrderService;` then a bare `Place()`. The colliding
// Billing.OrderService.Place must NOT win.
func TestIssue4374_UsingStatic_NameCollision(t *testing.T) {
	files := collidingCalleeFiles()
	files["src/Handlers/Handler.cs"] = "" +
		"using static App.Services.Orders.OrderService;\n" +
		"namespace App.Handlers {\n" +
		"  public class Handler {\n" +
		"    public void Run() {\n" +
		"      OrderService.Place();\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	merged := extractCSharpProjectForTest(t, files)
	want := entID(merged, "src/Orders/OrderService.cs", "OrderService.Place")
	wrong := entID(merged, "src/Billing/OrderService.cs", "OrderService.Place")
	assertBindsTo(t, merged, "src/Handlers/Handler.cs", "Handler.Run", "Place", want, wrong, "using-static")
}

// TestIssue4374_GlobalQualified_NameCollision — `global::App.Services...Place()`.
func TestIssue4374_GlobalQualified_NameCollision(t *testing.T) {
	files := collidingCalleeFiles()
	files["src/Handlers/Handler.cs"] = "" +
		"namespace App.Handlers {\n" +
		"  public class Handler {\n" +
		"    public void Run() {\n" +
		"      global::App.Services.Orders.OrderService.Place();\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	merged := extractCSharpProjectForTest(t, files)
	want := entID(merged, "src/Orders/OrderService.cs", "OrderService.Place")
	wrong := entID(merged, "src/Billing/OrderService.cs", "OrderService.Place")
	assertBindsTo(t, merged, "src/Handlers/Handler.cs", "Handler.Run", "Place", want, wrong, "global-qualified")
}

// TestIssue4374_SameNamespaceStatic_NameCollision — a same-namespace static
// call `OrderService.Create()` from a sibling type in App.Services.Orders. The
// caller's own namespace is the disambiguator against a colliding
// App.Services.Billing.OrderService (which has no Create, but binding must
// still pick the Orders entity, never go ambiguous on the Type leaf).
func TestIssue4374_SameNamespaceStatic_NameCollision(t *testing.T) {
	files := collidingCalleeFiles()
	// add a colliding Create in Billing too, so the (Type, method) leaf is
	// ambiguous across namespaces and ONLY the caller's namespace resolves it.
	files["src/Billing/OrderService.cs"] = "" +
		"namespace App.Services.Billing {\n" +
		"  public class OrderService {\n" +
		"    public void Place() {}\n" +
		"    public void Create() {}\n" +
		"  }\n" +
		"}\n"
	files["src/Orders/Caller.cs"] = "" +
		"namespace App.Services.Orders {\n" +
		"  public class Caller {\n" +
		"    public void Run() {\n" +
		"      OrderService.Create();\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	merged := extractCSharpProjectForTest(t, files)
	want := entID(merged, "src/Orders/OrderService.cs", "OrderService.Create")
	wrong := entID(merged, "src/Billing/OrderService.cs", "OrderService.Create")
	if want == "" || wrong == "" {
		t.Fatalf("missing colliding Create entities: orders=%q billing=%q", want, wrong)
	}
	assertBindsTo(t, merged, "src/Orders/Caller.cs", "Caller.Run", "Create", want, wrong, "same-namespace-static")
}

// TestIssue4374_NoFalseStamp_BareAndInstanceCalls — negative guard: a bare
// unqualified call and an instance-receiver call must NOT carry the
// cross-namespace stamp (they are not statically type-qualified cross-namespace
// calls).
func TestIssue4374_NoFalseStamp_BareAndInstanceCalls(t *testing.T) {
	files := map[string]string{
		"src/Svc.cs": "" +
			"namespace App.Svc {\n" +
			"  public class Svc {\n" +
			"    private Helper _h;\n" +
			"    public void Helper2() {}\n" +
			"    public void Run() {\n" +
			"      Helper2();\n" + // bare self call
			"      _h.Do();\n" + // instance-receiver call
			"    }\n" +
			"  }\n" +
			"  public class Helper { public void Do() {} }\n" +
			"}\n",
	}
	merged := extractCSharpProjectForTest(t, files)
	for k := range merged {
		if merged[k].SourceFile != "src/Svc.cs" || merged[k].Name != "Svc.Run" {
			continue
		}
		for _, r := range merged[k].Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			if r.Properties["csharp_call_ns"] != "" {
				t.Fatalf("false stamp on non-cross-namespace call: ToID=%q props=%v", r.ToID, r.Properties)
			}
		}
	}
}
