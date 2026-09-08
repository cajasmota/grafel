package python_test

import (
	"context"
	"sort"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/python"
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// field_target_address_6986_test.go — the Python custom lane's field→declared-
// type address (issue #6986).
//
// MEASURED PREMISE, gate-ON (`WithCustomExtractors(true)`; the custom lane is
// OFF by default, so user-facing incidence today is zero — #6966). On the
// `django` corpus repo at 64ffa3d22: 863 `ref_kind: field_target_type` edges,
// 39 bound (4.5%). Re-addressed from `Class:<Target>` to
// `scope:component:class:python:<consumer file>:<Target>`: 949 edges, 886 bound
// (93.4%), zero previously-bound key lost, zero bound to a file that does not
// declare the name.
//
// The address is built from the CONSUMER's file, not the target's — which a
// per-file pass-1 extractor cannot know. That is the dialect the resolver
// already speaks for Python: internal/resolve/refs.go lookupStructural resolves
// this shape same-file via lookupLocationKind and then, for `component` scope
// with lang=="python" only, cross-file via lookupUniqueRealComponentByName.

const modelsPath6986 = "shop/models.py"

// twoFilesSrc6986 is the shape the whole issue turns on: a target model name
// declared in TWO files. `Class:Customer` goes AMBIGUOUS here and dangles; the
// file-scoped structural address binds to the RIGHT one.
const ownerSrc6986 = `from django.db import models


class Customer(models.Model):
    name = models.CharField(max_length=100)


class Order(models.Model):
    buyer = models.ForeignKey(Customer, on_delete=models.CASCADE)
    watchers = models.ManyToManyField('self', blank=True)
`

const rivalPath6986 = "billing/models.py"

const rivalSrc6986 = `from django.db import models


class Customer(models.Model):
    vat_id = models.CharField(max_length=32)
`

func mergedFor6986(t *testing.T, files map[string]string) []types.EntityRecord {
	t.Helper()
	var all []types.EntityRecord
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		file := extreg.FileInput{Path: p, Language: "python", Content: []byte(files[p])}
		base, ok := extreg.Get("python")
		if !ok {
			t.Fatal("base python extractor not registered")
		}
		baseEnts, err := base.Extract(context.Background(), file)
		if err != nil {
			t.Fatalf("base extract %s: %v", p, err)
		}
		customEnts, errs := extractors.RunCustomExtractors(context.Background(), file)
		for _, e := range errs {
			t.Fatalf("custom extract %s: %v", p, e)
		}
		all = append(all, extractors.MergeWithCustom(baseEnts, customEnts)...)
	}
	for i := range all {
		if all[i].Name == "" {
			continue
		}
		all[i].ID = graph.EntityID("issue6986", all[i].Kind, all[i].Name, all[i].SourceFile)
	}
	return all
}

func fieldTargetEdges6986(ents []types.EntityRecord) []struct {
	Owner types.EntityRecord
	Rel   types.RelationshipRecord
} {
	var out []struct {
		Owner types.EntityRecord
		Rel   types.RelationshipRecord
	}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind != string(types.RelationshipKindReferences) {
				continue
			}
			for _, p := range r.Properties {
				if p.K == "ref_kind" && p.V == "field_target_type" {
					out = append(out, struct {
						Owner types.EntityRecord
						Rel   types.RelationshipRecord
					}{ents[i], r})
				}
			}
		}
	}
	return out
}

func propOf6986(props types.Props, k string) string {
	for _, p := range props {
		if p.K == k {
			return p.V
		}
	}
	return ""
}

// TestPythonFieldTargetType_BindsWhenTheNameIsDeclaredInTwoFiles is the
// mechanism. `Customer` is declared in shop/models.py AND billing/models.py, so
// the bare-name tier is ambiguous — the positive control asserts that first, or
// the structural address would be ungraded. The edge must then bind, and bind
// to the SHOP declaration.
func TestPythonFieldTargetType_BindsWhenTheNameIsDeclaredInTwoFiles(t *testing.T) {
	ents := mergedFor6986(t, map[string]string{
		modelsPath6986: ownerSrc6986,
		rivalPath6986:  rivalSrc6986,
	})
	idx := resolve.BuildIndex(ents)

	// Positive control on the dialect itself: the OLD address is ambiguous here.
	if id, ok := idx.Lookup("Class:Customer"); ok && id != "" {
		t.Fatalf("Class:Customer binds in this fixture (%q), so the ambiguity this test "+
			"controls for is absent and the structural address is ungraded", id)
	}

	byID := map[string]types.EntityRecord{}
	for i := range ents {
		byID[ents[i].ID] = ents[i]
	}
	edges := fieldTargetEdges6986(ents)
	if len(edges) == 0 {
		t.Fatal("no field_target_type edges emitted")
	}
	var buyer *types.RelationshipRecord
	for i := range edges {
		if edges[i].Rel.FromID == "Order.buyer" {
			buyer = &edges[i].Rel
		}
	}
	if buyer == nil {
		t.Fatal("Order.buyer emitted no field_target_type edge")
	}
	want := extreg.BuildComponentStructuralRef("python", modelsPath6986, "Customer")
	if buyer.ToID != want {
		t.Fatalf("Order.buyer ToID = %q, want %q", buyer.ToID, want)
	}

	resolve.ReferencesEmbedded(ents, idx)
	for _, e := range fieldTargetEdges6986(ents) {
		if e.Rel.FromID != "Order.buyer" {
			continue
		}
		target, ok := byID[e.Rel.ToID]
		if !ok {
			t.Fatalf("Order.buyer -> %q did NOT bind (dangling stub)", e.Rel.ToID)
		}
		if target.Name != "Customer" || target.SourceFile != modelsPath6986 {
			t.Fatalf("Order.buyer bound to %s@%s, want Customer@%s — #6369's wrong-node hazard",
				target.Name, target.SourceFile, modelsPath6986)
		}
	}
}

// TestPythonFieldTargetType_NeverBindsToANonDeclaringFile is the OTHER
// direction, asserted BY NAME. Recall cannot detect over-firing. `Customer` is
// declared in shop/models.py and billing/models.py and NOWHERE ELSE, so no
// field_target_type edge in this fixture may bind to an entity in any other
// file — and specifically not to billing/models.py's `Customer` from a
// shop/models.py field.
func TestPythonFieldTargetType_NeverBindsToANonDeclaringFile(t *testing.T) {
	ents := mergedFor6986(t, map[string]string{
		modelsPath6986: ownerSrc6986,
		rivalPath6986:  rivalSrc6986,
	})
	declares := map[string]map[string]bool{
		"Customer": {modelsPath6986: true, rivalPath6986: true},
		"Order":    {modelsPath6986: true},
	}
	byID := map[string]types.EntityRecord{}
	for i := range ents {
		byID[ents[i].ID] = ents[i]
	}
	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)

	checked := 0
	for _, e := range fieldTargetEdges6986(ents) {
		target, ok := byID[e.Rel.ToID]
		if !ok {
			continue // dangling is not the over-fire direction
		}
		files, known := declares[target.Name]
		if !known {
			t.Errorf("%s bound to %q@%s — a name nothing in this fixture declares",
				e.Rel.FromID, target.Name, target.SourceFile)
			continue
		}
		if !files[target.SourceFile] {
			t.Errorf("%s bound to %q@%s, a file that does NOT declare that name",
				e.Rel.FromID, target.Name, target.SourceFile)
		}
		// Nothing in shop/models.py may reach billing's Customer.
		if e.Rel.FromID == "Order.buyer" && target.SourceFile == rivalPath6986 {
			t.Errorf("Order.buyer (shop/models.py) bound to the billing/models.py Customer")
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no bound field_target_type edge to check: this negative is vacuous")
	}
}

// TestPythonFieldTargetType_CarriesTheRelationShapeProperties is the control on
// A′'s reason for existing. Once the structural ToID resolves to the SAME
// entity ID the core extractor's parallel edge points at, graph assembly
// dedupes the pair and keeps ONE property set — measured on django gate-ON,
// REFERENCES edges carrying `django_rel` fell 1018 → 258 when the custom edge
// did not carry them. So the custom edge must carry `django_rel` / `self_ref`
// (and `django_fk_string` for the string form) itself.
//
// Asserted per NAMED edge, not as a count: a count cannot see a substitution
// (#6973).
func TestPythonFieldTargetType_CarriesTheRelationShapeProperties(t *testing.T) {
	ents := mergedFor6986(t, map[string]string{modelsPath6986: ownerSrc6986})
	want := map[string]struct{ rel, self, fkString string }{
		"Order.buyer":    {"ForeignKey", "false", ""},
		"Order.watchers": {"ManyToManyField", "true", "self"},
	}
	seen := map[string]bool{}
	for _, e := range fieldTargetEdges6986(ents) {
		w, ok := want[e.Rel.FromID]
		if !ok {
			continue
		}
		seen[e.Rel.FromID] = true
		if got := propOf6986(e.Rel.Properties, "django_rel"); got != w.rel {
			t.Errorf("%s django_rel = %q, want %q", e.Rel.FromID, got, w.rel)
		}
		if got := propOf6986(e.Rel.Properties, "self_ref"); got != w.self {
			t.Errorf("%s self_ref = %q, want %q", e.Rel.FromID, got, w.self)
		}
		if got := propOf6986(e.Rel.Properties, "django_fk_string"); got != w.fkString {
			t.Errorf("%s django_fk_string = %q, want %q", e.Rel.FromID, got, w.fkString)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("%s emitted no field_target_type edge — the property assertion above is vacuous", k)
		}
	}
}

// TestPythonFieldTargetType_SelfRefIsTheStringFormOnly pins the ONE place the
// two producers could silently disagree. `self_ref` is true for the literal
// string target `'self'` and FALSE for a symbol target that happens to name the
// enclosing class — parseTargetExpr's rule in
// internal/extractors/python/django_relational.go. A permissive reading ("the
// target resolves to the owner") would flip `Node.parent` to true here.
func TestPythonFieldTargetType_SelfRefIsTheStringFormOnly(t *testing.T) {
	const src = `from django.db import models


class Node(models.Model):
    parent = models.ForeignKey(Node, null=True, on_delete=models.CASCADE)
    sibling = models.ForeignKey('self', null=True, on_delete=models.CASCADE)
`
	ents := mergedFor6986(t, map[string]string{modelsPath6986: src})
	got := map[string]string{}
	for _, e := range fieldTargetEdges6986(ents) {
		got[e.Rel.FromID] = propOf6986(e.Rel.Properties, "self_ref")
	}
	if got["Node.parent"] != "false" {
		t.Errorf("Node.parent self_ref = %q, want \"false\" — a SYMBOL target naming the "+
			"enclosing class is not a self-reference (parseTargetExpr's rule)", got["Node.parent"])
	}
	if got["Node.sibling"] != "true" {
		t.Errorf("Node.sibling self_ref = %q, want \"true\" — the literal string 'self' IS one",
			got["Node.sibling"])
	}
}
