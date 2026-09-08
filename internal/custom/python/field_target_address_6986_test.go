package python_test

import (
	"context"
	"sort"
	"strings"
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

// edgeKey6986 identifies a field_target_type edge in a way that SURVIVES
// resolution. `resolve.ReferencesEmbedded` rewrites FromID in place once the
// source endpoint binds, so matching a post-resolution edge on
// `FromID == "Order.buyer"` silently matches NOTHING and every assertion under
// it passes vacuously — which is exactly what the first draft of this file did.
// The `field_name` property and the owning entity's Name are both untouched by
// resolution, so the pair is a stable key.
func edgeKey6986(owner types.EntityRecord, r types.RelationshipRecord) string {
	return owner.Name + "." + propOf6986(r.Properties, "field_name")
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
	checked := 0
	for _, e := range fieldTargetEdges6986(ents) {
		if edgeKey6986(e.Owner, e.Rel) != "Order.buyer" {
			continue
		}
		checked++
		target, ok := byID[e.Rel.ToID]
		if !ok {
			t.Fatalf("Order.buyer -> %q did NOT bind (dangling stub)", e.Rel.ToID)
		}
		if target.Name != "Customer" || target.SourceFile != modelsPath6986 {
			t.Fatalf("Order.buyer bound to %s@%s, want Customer@%s — #6369's wrong-node hazard",
				target.Name, target.SourceFile, modelsPath6986)
		}
	}
	if checked == 0 {
		t.Fatal("no Order.buyer edge survived to the post-resolution check: this half is vacuous")
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
		if edgeKey6986(e.Owner, e.Rel) == "Order.buyer" && target.SourceFile == rivalPath6986 {
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
		key := edgeKey6986(e.Owner, e.Rel)
		w, ok := want[key]
		if !ok {
			continue
		}
		seen[key] = true
		if got := propOf6986(e.Rel.Properties, "django_rel"); got != w.rel {
			t.Errorf("%s django_rel = %q, want %q", key, got, w.rel)
		}
		if got := propOf6986(e.Rel.Properties, "self_ref"); got != w.self {
			t.Errorf("%s self_ref = %q, want %q", key, got, w.self)
		}
		if got := propOf6986(e.Rel.Properties, "django_fk_string"); got != w.fkString {
			t.Errorf("%s django_fk_string = %q, want %q", key, got, w.fkString)
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
		got[edgeKey6986(e.Owner, e.Rel)] = propOf6986(e.Rel.Properties, "self_ref")
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

// ---------------------------------------------------------------------------
// The CROSS-FILE tier — the entire difference between this arm and #6984/#6991.
// ---------------------------------------------------------------------------

// TestPythonFieldTargetType_CrossFileTier_BindsUniqueRefusesAmbiguous grades the
// tier this pass newly depends on and that nothing in this package graded
// before: `lookupUniqueRealComponentByName`, reached when the CONSUMER's file
// declares nothing by that name.
//
// Why the existing negative does not cover it: in
// TestPythonFieldTargetType_NeverBindsToANonDeclaringFile the consumer file
// declares `Customer` itself, so the same-file `lookupLocationKind` tier
// resolves FIRST and the cross-file tier never fires. The two claims — "never
// binds to a non-declaring file" and "never binds a cross-file AMBIGUOUS name"
// — are different, and only the first was asserted. A mutant that makes
// lookupUniqueRealComponentByName accept an ambiguous name leaves this package
// GREEN without this case (scored on review as M3).
//
// Here `orders/models.py` declares NEITHER target, so every edge must travel
// the cross-file tier. All three outcomes are asserted together, because
// asserting only the bind would leave the refusal — the direction that matters
// for #6369 — ungraded, and asserting only the refusal could pass vacuously on
// a tier that binds nothing at all.
func TestPythonFieldTargetType_CrossFileTier_BindsUniqueRefusesAmbiguous(t *testing.T) {
	const consumerPath = "orders/models.py"
	files := map[string]string{
		// Consumer: declares Order and nothing else. Ambiguous target,
		// unique target and absent target, side by side.
		consumerPath: `from django.db import models


class Order(models.Model):
    buyer = models.ForeignKey(Customer, on_delete=models.CASCADE)
    carrier = models.ForeignKey(Vendor, on_delete=models.CASCADE)
    coupon = models.ForeignKey(Ghost, on_delete=models.CASCADE)
`,
		// AMBIGUOUS: Customer declared in two other files.
		"shop/models.py": `from django.db import models


class Customer(models.Model):
    name = models.CharField(max_length=100)
`,
		"billing/models.py": `from django.db import models


class Customer(models.Model):
    vat_id = models.CharField(max_length=32)
`,
		// UNIQUE: Vendor declared in exactly one other file.
		"vendors/models.py": `from django.db import models


class Vendor(models.Model):
    code = models.CharField(max_length=8)
`,
		// Ghost is declared nowhere at all.
	}
	ents := mergedFor6986(t, files)
	byID := map[string]types.EntityRecord{}
	for i := range ents {
		byID[ents[i].ID] = ents[i]
	}

	// Preconditions, or the three outcomes below are not the outcomes named.
	declCount := map[string]int{}
	for i := range ents {
		if ents[i].SourceFile == consumerPath {
			continue
		}
		switch ents[i].Name {
		case "Customer", "Vendor":
			if ents[i].Kind == "SCOPE.Component" || ents[i].Kind == "SCOPE.Schema" {
				declCount[ents[i].Name+"@"+ents[i].SourceFile] = 1
			}
		}
	}
	custFiles, vendFiles := 0, 0
	for k := range declCount {
		if strings.HasPrefix(k, "Customer@") {
			custFiles++
		}
		if strings.HasPrefix(k, "Vendor@") {
			vendFiles++
		}
	}
	if custFiles < 2 {
		t.Fatalf("fixture precondition: Customer must be declared outside %s in >=2 files, got %d — "+
			"the ambiguity this test grades is not present", consumerPath, custFiles)
	}
	if vendFiles != 1 {
		t.Fatalf("fixture precondition: Vendor must be declared outside %s in exactly 1 file, got %d",
			consumerPath, vendFiles)
	}

	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)

	got := map[string]types.RelationshipRecord{}
	for _, e := range fieldTargetEdges6986(ents) {
		got[edgeKey6986(e.Owner, e.Rel)] = e.Rel
	}
	for _, from := range []string{"Order.buyer", "Order.carrier", "Order.coupon"} {
		if _, ok := got[from]; !ok {
			t.Fatalf("%s emitted no field_target_type edge — every assertion below would be vacuous", from)
		}
	}

	// AMBIGUOUS -> must DANGLE. Refusing to guess is the #6369 direction.
	if target, bound := byID[got["Order.buyer"].ToID]; bound {
		t.Errorf("Order.buyer bound to %s@%s, want DANGLING: `Customer` is declared in two files "+
			"and %s declares neither, so the cross-file tier must refuse rather than pick one",
			target.Name, target.SourceFile, consumerPath)
	}

	// UNIQUE -> must BIND, and to the one declaring file.
	target, bound := byID[got["Order.carrier"].ToID]
	if !bound {
		t.Errorf("Order.carrier did NOT bind (ToID %q): `Vendor` has exactly one declaration, which is "+
			"the case the python cross-file tier exists to serve — this is the recall #6984 could not have",
			got["Order.carrier"].ToID)
	} else if target.Name != "Vendor" || target.SourceFile != "vendors/models.py" {
		t.Errorf("Order.carrier bound to %s@%s, want Vendor@vendors/models.py",
			target.Name, target.SourceFile)
	}

	// ABSENT -> must DANGLE.
	if target, bound := byID[got["Order.coupon"].ToID]; bound {
		t.Errorf("Order.coupon bound to %s@%s, want DANGLING: `Ghost` is declared nowhere in the fixture",
			target.Name, target.SourceFile)
	}
}

// ---------------------------------------------------------------------------
// #6990 — FIXED. This was a known-wrong pin; it is now a positive assertion.
// ---------------------------------------------------------------------------

// TestPythonFieldTargetType_6990_NoOverReadIntoTheNextFieldsTarget was, until
// #6990 shipped, a KNOWN-WRONG pin: it asserted that `LogEntry.user` came out
// bound to `ContentType`, and carried a t.Skip telling the fixer to delete it.
// It is converted rather than deleted, because deleting it would leave the
// defect's own named shape unobserved — and the correct behaviour is a
// NEGATIVE, which nothing else in this file grades.
//
// The mechanism that was fixed: `djangoModelRelTargetRe` has no left word
// boundary and the producer handed it a raw 400-BYTE forward window, unbounded
// by the declaration it started in. For
// `user = models.ForeignKey(settings.AUTH_USER_MODEL, …)` the dotted,
// lowercase-initial target is correctly REJECTED by the `[A-Z]` symbol
// alternative — and the window then ran on into the NEXT field's
// `ForeignKey(ContentType`, capturing that. The field's target_type became a
// class it does not reference: django/contrib/admin/models.py, verbatim.
//
// That made it strictly worse than #6988's case. `ContentType` is CamelCase and
// a genuine model, so the edge BOUND, to a legitimate node, in the declaring
// file. Neither `NeverBindsToANonDeclaringFile` nor a dangling count could see
// it: the FILE was right, the NAME was wrong, and no control here observes that
// dimension.
//
// The window is now bounded by the field's own call parentheses
// (`djangoDeclEnd`), so the correct target for `LogEntry.user` is NONE: the
// declaration names `settings.AUTH_USER_MODEL`, a settings indirection this
// extractor does not resolve, and emitting nothing is what "does not resolve"
// looks like. Asserted as "no field_target_type edge AT ALL" rather than "not
// ContentType", so a re-broadened window cannot pass by capturing a different
// wrong target.
//
// GRAPH-LEVEL INCIDENCE IS NOT ESTABLISHED. An earlier comment here claimed
// that on the real django corpus this edge stays dangling anyway because the
// `ext:django:` external-synthesis pass intercepts first. Nobody verified that,
// and it is not asserted. What this test grades is EXTRACTOR-LEVEL behaviour,
// end to end through the merge and resolve path, which it does prove.
func TestPythonFieldTargetType_6990_NoOverReadIntoTheNextFieldsTarget(t *testing.T) {
	files := map[string]string{
		"admin/models.py": `from django.conf import settings
from django.db import models


class LogEntry(models.Model):
    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)
    content_type = models.ForeignKey(ContentType, on_delete=models.CASCADE)
`,
		"contenttypes/models.py": `from django.db import models


class ContentType(models.Model):
    app_label = models.CharField(max_length=100)
`,
	}
	ents := mergedFor6986(t, files)
	byID := map[string]types.EntityRecord{}
	for i := range ents {
		byID[ents[i].ID] = ents[i]
	}
	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)

	var sawContentType bool
	for _, e := range fieldTargetEdges6986(ents) {
		switch edgeKey6986(e.Owner, e.Rel) {
		case "LogEntry.user":
			// THE FORBIDDEN ROW.
			t.Errorf("LogEntry.user emitted a field_target_type edge (target_type=%q, ToID=%q); want "+
				"NONE. The declaration names settings.AUTH_USER_MODEL — any target here came from "+
				"reading past the end of this field's own declaration (#6990)",
				propOf6986(e.Rel.Properties, "target_type"), e.Rel.ToID)
		case "LogEntry.content_type":
			// POSITIVE CONTROL, and the specific sibling whose target was
			// stolen. Without it the negative above could pass vacuously —
			// "this fixture produced nothing" reads identically.
			sawContentType = true
			if got := propOf6986(e.Rel.Properties, "target_type"); got != "ContentType" {
				t.Errorf("LogEntry.content_type target_type = %q, want \"ContentType\" — the #6990 "+
					"window bound dropped a target that was always correct", got)
			}
			target, bound := byID[e.Rel.ToID]
			if !bound {
				t.Errorf("LogEntry.content_type did NOT bind (ToID %q): the narrowing cost a real edge",
					e.Rel.ToID)
			} else if target.Name != "ContentType" || target.SourceFile != "contenttypes/models.py" {
				t.Errorf("LogEntry.content_type bound to %s@%s, want ContentType@contenttypes/models.py",
					target.Name, target.SourceFile)
			}
		}
	}
	if !sawContentType {
		t.Fatal("LogEntry.content_type emitted no field_target_type edge at all — the positive control " +
			"is gone, so the LogEntry.user negative above proves nothing (#6990)")
	}
	// The field under the NEGATIVE must still exist. A negative row that passes
	// because the field vanished entirely is passing for the wrong reason.
	var sawUserField bool
	for i := range ents {
		if ents[i].Name == "LogEntry.user" {
			sawUserField = true
		}
	}
	if !sawUserField {
		t.Error("no LogEntry.user entity — the negative above passed because the FIELD is gone, not " +
			"because its target scan was bounded (#6990)")
	}
}
