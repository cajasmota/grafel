package csharp_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/csharp"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// #6912 — the field→declared-type edge for C#. See field_type_refs.go for the
// kind / address / same-file decisions; this file grades them.
//
// Every assertion below names FIELDS AND TARGETS. A count table cannot see a
// substitution (#6973), and recall cannot see over-firing at all — so the
// forbidden set (primitives, a BCL type, the generic CONSTRUCTORS `List` and
// `Dictionary`, a qualified name) is asserted by name beside the expected set.

const fieldTypeRefsOwnerPath = "Models/Order.cs"

const fieldTypeRefsSrc = `namespace App.Models;

public enum OrderStatus { New, Shipped }

public interface IShipper { void Ship(); }

public struct Money { public int Cents; }

public record Address(string City, Money Total);

public class Customer { public string Name { get; set; } }

public class Order
{
    public Customer Buyer { get; set; }
    public OrderStatus Status { get; set; }
    public IShipper Shipper { get; set; }
    public Address? ShipTo { get; set; }
    public List<Customer> Watchers { get; set; }
    public Dictionary<string, Customer> ByName { get; set; }
    public Customer[] History { get; set; }
    public Order Parent { get; set; }
    public (Customer, Customer) Pair { get; set; }
    public global::Customer Aliased { get; set; }
    public int Quantity { get; set; }
    public string Label { get; set; }
    public System.Net.Http.HttpClient Client { get; set; }
}
`

// A SECOND file declaring types of the SAME bare names. Its only job is to make
// the bare-name resolver tier ambiguous, which is why the structural address is
// used; the edges from Models/Order.cs must be identical with and without it.
const fieldTypeRefsRivalPath = "Other/Rival.cs"

const fieldTypeRefsRivalSrc = `namespace App.Other;

public class Customer { public string Other { get; set; } }
public class Order { public Customer Buyer { get; set; } }
public enum OrderStatus { A, B }
`

func extractCSFiles(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("csharp")
	if !ok {
		t.Fatal("csharp extractor not registered")
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var recs []types.EntityRecord
	for _, p := range paths {
		got, err := ext.Extract(context.Background(), extractor.FileInput{
			Path:     p,
			Content:  []byte(files[p]),
			Language: "csharp",
			TSTree:   parseForTest(t, files[p]),
		})
		if err != nil {
			t.Fatalf("Extract %s: %v", p, err)
		}
		recs = append(recs, got...)
	}
	return recs
}

// fieldTypeRefEdges returns "<field name> -> <ToID>" for every REFERENCES edge
// carrying ref_kind=field_target_type, across ALL records, so an edge that
// escaped onto some other record is still seen. It fails on a non-empty FromID:
// the Django precedent leaves it empty so assembly anchors the edge on the
// field that carries it.
func fieldTypeRefEdges(t *testing.T, recs []types.EntityRecord) []string {
	t.Helper()
	var out []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || propOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s -[REFERENCES]-> %s has FromID=%q, want empty",
					recs[i].Name, r.ToID, r.FromID)
			}
			out = append(out, recs[i].Name+" -> "+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

func propOf(p types.Props, key string) string {
	for _, kv := range p {
		if kv.K == key {
			return kv.V
		}
	}
	return ""
}

// TestCsharpFieldTypeRefs_EmittedEdges is the positive direction, written as an
// INDEPENDENT LITERAL set (#6975) rather than derived from the production
// target index. Each ToID is spelled out so a change of address dialect, of
// enum target node, or of owning field fails here instead of passing silently.
func TestCsharpFieldTypeRefs_EmittedEdges(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{fieldTypeRefsOwnerPath: fieldTypeRefsSrc})
	want := []string{
		"Address.Total -> scope:component:class:csharp:Models/Order.cs:Money",
		"Order.ByName -> scope:component:class:csharp:Models/Order.cs:Customer",
		"Order.Buyer -> scope:component:class:csharp:Models/Order.cs:Customer",
		"Order.History -> scope:component:class:csharp:Models/Order.cs:Customer",
		// ONE row, not two: `(Customer, Customer)` names the same target twice
		// and the per-field dedup collapses it. A second identical row here is
		// what a dropped dedup looks like.
		"Order.Pair -> scope:component:class:csharp:Models/Order.cs:Customer",
		"Order.Parent -> scope:component:class:csharp:Models/Order.cs:Order",
		"Order.ShipTo -> scope:component:class:csharp:Models/Order.cs:Address",
		"Order.Shipper -> scope:component:class:csharp:Models/Order.cs:IShipper",
		"Order.Status -> scope:enum:Models/Order.cs:OrderStatus",
		"Order.Watchers -> scope:component:class:csharp:Models/Order.cs:Customer",
	}
	sort.Strings(want)
	got := fieldTypeRefEdges(t, recs)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("field-type edges mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestCsharpFieldTypeRefs_ForbiddenTargets is the negative direction, by name.
// Recall is structurally blind to over-firing and over-firing is the failure
// mode that hurts here: every non-binding edge is one more `bug-extractor`
// endpoint (#6906), so a too-broad predicate makes the graph worse, not better.
func TestCsharpFieldTypeRefs_ForbiddenTargets(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{fieldTypeRefsOwnerPath: fieldTypeRefsSrc})
	edges := fieldTypeRefEdges(t, recs)

	// Fields whose declared type is a primitive, a BCL type, or a qualified
	// name must carry NO field-type edge at all.
	for _, field := range []string{
		"Order.Quantity", // int
		"Order.Label",    // string
		"Order.Client",   // System.Net.Http.HttpClient — qualified AND external
		"Order.Aliased",  // global::Customer — alias-qualified, NOT descended into
		"Customer.Name",  // string
		"Money.Cents",    // int
		"Address.City",   // string
	} {
		for _, e := range edges {
			if strings.HasPrefix(e, field+" -> ") {
				t.Errorf("%s must have no field-type edge, got %q", field, e)
			}
		}
	}

	// The generic CONSTRUCTORS are targets in their own right if anything
	// emits to them. `List<Customer>` must reach Customer and never List.
	for _, banned := range []string{"List", "Dictionary", "HttpClient", "int", "string"} {
		for _, e := range edges {
			if strings.HasSuffix(e, ":"+banned) {
				t.Errorf("no field-type edge may target %q, got %q", banned, e)
			}
		}
	}
}

// TestCsharpFieldTypeRefs_QualifiedTypeIsNotResolved pins the one place the
// rule is stricter than "is it declared in this file": a qualified name is not
// descended into, so `App.Other.Customer` does NOT bind to the file's own
// `Customer` even though the rightmost segment matches it.
func TestCsharpFieldTypeRefs_QualifiedTypeIsNotResolved(t *testing.T) {
	const src = `namespace App.Models;

public class Customer { public string Name { get; set; } }

public class Order
{
    public App.Other.Customer Foreign { get; set; }
    public Customer Local { get; set; }
}
`
	recs := extractCSFiles(t, map[string]string{"Models/Order.cs": src})
	want := []string{"Order.Local -> scope:component:class:csharp:Models/Order.cs:Customer"}
	if got := fieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("qualified-type edges\n got: %v\nwant: %v", got, want)
	}
}

// TestCsharpFieldTypeRefs_CrossFileTypeGetsNoEdge states the deliberate cost of
// the same-file rule as an assertion rather than as prose: a field whose type
// is declared in ANOTHER file gets nothing, because pass 1 has no cross-file
// view and an edge it cannot verify would dangle.
func TestCsharpFieldTypeRefs_CrossFileTypeGetsNoEdge(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{
		"Models/Order.cs":   "namespace App.Models;\n\npublic class Order { public Shipper Via { get; set; } }\n",
		"Models/Shipper.cs": "namespace App.Models;\n\npublic class Shipper { public string Name { get; set; } }\n",
	})
	// Positive control: the field whose type lives in the other file must exist,
	// otherwise "no edges" is trivially true for the wrong reason.
	found := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == "Order.Via" {
			found = true
		}
	}
	if !found {
		t.Fatal("fixture produced no Order.Via field; the cross-file case is not exercised")
	}
	if got := fieldTypeRefEdges(t, recs); len(got) != 0 {
		t.Fatalf("cross-file declared type must produce no edge, got %v", got)
	}
}

// TestCsharpFieldTypeRefs_SameNameInTwoFiles is the reason the address is
// structural. The rival file declares Customer / Order / OrderStatus under the
// same bare names; the edges emitted for Models/Order.cs must be byte-identical
// to the single-file run.
func TestCsharpFieldTypeRefs_SameNameInTwoFiles(t *testing.T) {
	alone := fieldTypeRefEdges(t, extractCSFiles(t, map[string]string{
		fieldTypeRefsOwnerPath: fieldTypeRefsSrc,
	}))
	withRival := fieldTypeRefEdges(t, extractCSFiles(t, map[string]string{
		fieldTypeRefsOwnerPath: fieldTypeRefsSrc,
		fieldTypeRefsRivalPath: fieldTypeRefsRivalSrc,
	}))
	// The rival file has its own Order.Buyer -> its own Customer.
	wantExtra := "Order.Buyer -> scope:component:class:csharp:Other/Rival.cs:Customer"
	var got []string
	for _, e := range withRival {
		if e != wantExtra {
			got = append(got, e)
		}
	}
	if strings.Join(got, "\n") != strings.Join(alone, "\n") {
		t.Fatalf("a same-named type in another file changed this file's edges\n got:\n%s\nwant:\n%s",
			strings.Join(got, "\n"), strings.Join(alone, "\n"))
	}
	if len(withRival) != len(alone)+1 {
		t.Fatalf("rival file's own edge missing: %v", withRival)
	}
}

// TestCsharpFieldTypeRefs_SameNameDeclaredTwiceInOneFile — a name declared twice
// in the SAME file is removed from the target index rather than resolved to
// either declaration, so the pass never guesses.
//
// The fixture uses PARTIAL CLASSES on purpose. An earlier cut wrote the two
// declarations into two different namespaces in one file, which made the
// fixture's SHAPE imply that namespace scope is part of what the collide branch
// considers. It is not — see
// TestCsharpFieldTypeRefs_KnownOverFire_NamespaceScopeIsNotConsulted, which
// pins the opposite. A fixture that implies coverage it does not assert is how
// two defects got past review this cycle; partial classes exercise the same
// branch and imply nothing about namespaces.
func TestCsharpFieldTypeRefs_SameNameDeclaredTwiceInOneFile(t *testing.T) {
	const src = `namespace App.Models;

public partial class Widget { public string A { get; set; } }
public partial class Widget { public string B { get; set; } }

public class Holder { public Widget W { get; set; } }
`
	recs := extractCSFiles(t, map[string]string{"Models/Dup.cs": src})
	// Positive control: the fixture must actually contain TWO Widget
	// declarations and the Holder.W field, or the assertion below is vacuous.
	widgets := 0
	holderField := false
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Name == "Widget" {
			widgets++
		}
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Subtype == "field" && recs[i].Name == "Holder.W" {
			holderField = true
		}
	}
	if widgets != 2 || !holderField {
		t.Fatalf("fixture did not produce two Widget declarations and a Holder.W field "+
			"(widgets=%d holderField=%v); the duplicate-name case is not exercised",
			widgets, holderField)
	}
	for _, e := range fieldTypeRefEdges(t, recs) {
		if strings.HasPrefix(e, "Holder.W -> ") {
			t.Fatalf("Widget is declared twice in this file; Holder.W must get no edge, got %q", e)
		}
	}
}

// TestCsharpFieldTypeRefs_KnownOverFire_NamespaceScopeIsNotConsulted and
// TestCsharpFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType PIN A
// KNOWN DEFECT, and they are the only assertions in this file that describe
// behaviour that is WRONG.
//
// "Declared in this same file" is a FILE-scoped check with no namespace and no
// type-parameter scope behind it. Two consequences, both found in review of
// #6912 and both reproduced here:
//
//  1. `namespace A { class Customer }` + `namespace B { class Order { Customer
//     Buyer } }` in one file emits an edge, though App.B.Order cannot see
//     App.A.Customer without a `using`.
//  2. A type PARAMETER named after a same-file class (`class Box<Customer>`)
//     binds the parameter to the class.
//
// Both are WORSE than a dangling edge by the same logic that chose this pass's
// address: the edge BINDS, so it never reaches `bug-extractor` and no
// disposition figure will ever surface it. Measured incidence on the corpora is
// ZERO — aspnetcore-mvc has 12 multi-namespace .cs files and emits no field-type
// edge inside any of them; aspnetcore-realworld and WakeOnLAN have none at all
// — which is why the behaviour is recorded here rather than fixed in this arm.
//
// These two tests are expected to FAIL when the follow-up fixes them. That is
// the point: they make the limitation observable at the code, and a fix has to
// come here and say so rather than changing behaviour silently.
func TestCsharpFieldTypeRefs_KnownOverFire_NamespaceScopeIsNotConsulted(t *testing.T) {
	const src = `namespace App.A { public class Customer { public string N { get; set; } } }
namespace App.B { public class Order { public Customer Buyer { get; set; } } }
`
	recs := extractCSFiles(t, map[string]string{"F.cs": src})
	want := []string{"Order.Buyer -> scope:component:class:csharp:F.cs:Customer"}
	if got := fieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("KNOWN over-fire changed shape — if namespace scope is now consulted, "+
			"delete this test and say so\n got: %v\nwant: %v", got, want)
	}
}

func TestCsharpFieldTypeRefs_KnownOverFire_TypeParameterShadowsSameFileType(t *testing.T) {
	const src = `namespace App.Models;

public class Customer { public string N { get; set; } }

public class Box<Customer> { public Customer Item { get; set; } }
`
	recs := extractCSFiles(t, map[string]string{"F.cs": src})
	want := []string{"Box.Item -> scope:component:class:csharp:F.cs:Customer"}
	if got := fieldTypeRefEdges(t, recs); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("KNOWN over-fire changed shape — if type-parameter scope is now "+
			"consulted, delete this test and say so\n got: %v\nwant: %v", got, want)
	}
}

// TestCsharpFieldTypeRefs_ResolverBindsEveryEdge is the gate this whole issue
// class turns on: #6906 exists because a comment described a binding nobody
// built. Every emitted edge is driven through the PRODUCTION resolver
// (BuildIndex → ReferencesEmbedded, exactly as graph assembly does) and must
// come back REWRITTEN to a real entity ID. A dangling edge is worse than no
// edge — it is one more `bug-extractor` endpoint — so zero non-resolved
// dispositions is the assertion, not a ratio.
func TestCsharpFieldTypeRefs_ResolverBindsEveryEdge(t *testing.T) {
	recs := extractCSFiles(t, map[string]string{
		fieldTypeRefsOwnerPath: fieldTypeRefsSrc,
		fieldTypeRefsRivalPath: fieldTypeRefsRivalSrc,
	})
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6912", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	idx := resolve.BuildIndex(recs)

	// Positive control on the address dialect itself: the BARE-name form of the
	// same target is AMBIGUOUS here, which is why it is not what we emit.
	if _, st := idx.LookupStatusHint("Customer", "REFERENCES"); st == 1 {
		t.Fatalf("the bare name %q binds in this fixture, so the ambiguity this "+
			"test controls for is not present and the structural address is ungraded", "Customer")
	}

	// Snapshot the (field, target-entity) pairs the structural stubs point at
	// BEFORE resolution rewrites the ToIDs in place.
	idToName := map[string]string{}
	for i := range recs {
		idToName[recs[i].ID] = recs[i].SourceFile + ":" + recs[i].Kind + ":" + recs[i].Name
	}

	before := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "REFERENCES" && propOf(r.Properties, "ref_kind") == "field_target_type" {
				before++
			}
		}
	}
	if before == 0 {
		t.Fatal("no field-type edges to drive through the resolver")
	}

	resolve.ReferencesEmbedded(recs, idx)

	var bound []string
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind != "REFERENCES" || propOf(r.Properties, "ref_kind") != "field_target_type" {
				continue
			}
			target, ok := idToName[r.ToID]
			if !ok {
				t.Errorf("%s -[REFERENCES]-> %q did NOT bind to an entity ID (dangling stub)",
					recs[i].Name, r.ToID)
				continue
			}
			bound = append(bound, recs[i].SourceFile+":"+recs[i].Name+" => "+target)
		}
	}
	sort.Strings(bound)
	want := []string{
		"Models/Order.cs:Address.Total => Models/Order.cs:SCOPE.Component:Money",
		"Models/Order.cs:Order.ByName => Models/Order.cs:SCOPE.Component:Customer",
		"Models/Order.cs:Order.Buyer => Models/Order.cs:SCOPE.Component:Customer",
		"Models/Order.cs:Order.History => Models/Order.cs:SCOPE.Component:Customer",
		"Models/Order.cs:Order.Pair => Models/Order.cs:SCOPE.Component:Customer",
		"Models/Order.cs:Order.Parent => Models/Order.cs:SCOPE.Component:Order",
		"Models/Order.cs:Order.ShipTo => Models/Order.cs:SCOPE.Component:Address",
		"Models/Order.cs:Order.Shipper => Models/Order.cs:SCOPE.Component:IShipper",
		"Models/Order.cs:Order.Status => Models/Order.cs:SCOPE.Enum:OrderStatus",
		"Models/Order.cs:Order.Watchers => Models/Order.cs:SCOPE.Component:Customer",
		"Other/Rival.cs:Order.Buyer => Other/Rival.cs:SCOPE.Component:Customer",
	}
	sort.Strings(want)
	if strings.Join(bound, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resolved endpoints mismatch\n got:\n%s\nwant:\n%s",
			strings.Join(bound, "\n"), strings.Join(want, "\n"))
	}
}
