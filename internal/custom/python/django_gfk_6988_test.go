package python

import (
	"context"
	"regexp"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #6988 — `GenericForeignKey(ct_field, fk_field)` names two SIBLING FIELDS
// on the same model. Neither argument is a model. The Django model pass used to
// treat argument 1 as a target model and emit a `field_target_type` REFERENCES
// edge to `Class:<ct_field>`.
//
// Two compounding admits let it through, and EITHER ALONE closes the bug:
//
//	(1) `isDjangoRelationalField` gated on `strings.HasSuffix(rhs, "ForeignKey")`
//	    and `GenericForeignKey` ends in `ForeignKey`.
//	(2) `djangoModelRelTargetRe` has no LEFT word boundary, so the alternation's
//	    `ForeignKey(` matches INSIDE `GenericForeignKey(`. (`GenericForeignKey`
//	    is not in the alternation — the missing boundary is the whole story.)
//
// ONLY (1) was fixed. (2) was rejected on measured merit: the missing boundary
// is also what lets third-party `*ForeignKey` SUBCLASSES (django-mptt's
// `TreeForeignKey`) reach their target, and a `\b` drops those with it —
// TestIssue6988_RegexBoundaryRejected_WouldDropSubclasses compiles the rejected
// variant and shows both halves of that trade. Shipping one guard also keeps it
// GRADED: two guards that only fire together grade neither.
//
// Scope note (#6966): these edges only ever existed with the custom Python lane
// gate ON. Gate-OFF — the default — they were never produced, so the
// user-facing incidence of the defect was zero.

// ---------------------------------------------------------------------------
// The shipped guard: the constructor gate.
// ---------------------------------------------------------------------------

// TestIssue6988_GateRejectsGenericForeignKey grades the `HasSuffix` gate
// on its own. It never reaches the regex.
func TestIssue6988_GateRejectsGenericForeignKey(t *testing.T) {
	rejected := []string{
		"GenericForeignKey",
		"fields.GenericForeignKey",
		"contenttypes.fields.GenericForeignKey",
		"django.contrib.contenttypes.fields.GenericForeignKey",
	}
	for _, rhs := range rejected {
		if isDjangoRelationalField(rhs) {
			t.Errorf("isDjangoRelationalField(%q) = true, want false: "+
				"GenericForeignKey's first argument is a sibling field name, not a model (#6988)", rhs)
		}
	}
}

// TestIssue6988_GateKeepsRelationalConstructors pins the ACCEPT side of
// the same gate, member by member — a set needs an assertion per member. The
// third-party `*ForeignKey` SUBCLASSES are deliberately still admitted: they do
// take a model as argument 1, so a whole-suffix ban would be a recall loss.
func TestIssue6988_GateKeepsRelationalConstructors(t *testing.T) {
	accepted := []string{
		"models.ForeignKey",
		"ForeignKey",
		"models.OneToOneField",
		"models.ManyToManyField",
		"mptt.models.TreeForeignKey", // django-mptt subclass: arg 1 IS a model
	}
	for _, rhs := range accepted {
		if !isDjangoRelationalField(rhs) {
			t.Errorf("isDjangoRelationalField(%q) = false, want true (#6988 must not narrow the legitimate set)", rhs)
		}
	}
}

// ---------------------------------------------------------------------------
// The REJECTED alternative: a left word boundary on djangoModelRelTargetRe.
// ---------------------------------------------------------------------------

// TestIssue6988_RegexBoundaryRejected_WouldDropSubclasses is the evidence behind
// closing #6988 at the GATE ONLY. The issue offered a second fix — add a left
// word boundary to `djangoModelRelTargetRe` so the alternation stops matching
// the `ForeignKey(` inside `GenericForeignKey(`. That does close the bug, but it
// is not free: the missing boundary is ALSO what lets a third-party `*ForeignKey`
// SUBCLASS reach its target argument, and `\b` drops those too. This test pins
// the recall that the boundary would have cost, so the trade-off is observed
// rather than argued.
//
// Shipping both guards was therefore rejected on merit, not on effort — and it
// also avoids a masking pair: two guards that only fire together grade neither.
// `isDjangoRelationalField` is the single graded guard.
func TestIssue6988_RegexBoundaryRejected_WouldDropSubclasses(t *testing.T) {
	const subclassRHS = `    node = TreeForeignKey(Category, on_delete=models.CASCADE)`

	// What ships: the boundary-free regex resolves the subclass target.
	if got := djangoRelTarget(subclassRHS, "Item"); got != "Category" {
		t.Fatalf("djangoRelTarget(%q) = %q, want \"Category\" — the subclass recall this "+
			"regex exists to preserve is gone (#6988)", subclassRHS, got)
	}

	// The rejected variant, compiled here so the claim is measured. It closes
	// #6988 (no GFK target) AND drops TreeForeignKey (no subclass target).
	withBoundary := regexp.MustCompile(
		`\b` + djangoModelRelTargetRe.String())
	if withBoundary.FindStringSubmatch(subclassRHS) != nil {
		t.Error("the \\b variant still matched TreeForeignKey — the stated cost of the " +
			"rejected fix is wrong and the decision in #6988 should be revisited")
	}
	const gfkRHS = `    content_object = GenericForeignKey("content_type", "object_id")`
	if withBoundary.FindStringSubmatch(gfkRHS) != nil {
		t.Error("the \\b variant did not close #6988 — the rejected fix was mischaracterised")
	}
}

// TestIssue6988_RegexKeepsEveryTargetForm pins the shipped regex's ACCEPT set,
// one assertion per form, so the target extractor cannot be narrowed silently.
// It deliberately does NOT assert anything about GenericForeignKey: the regex is
// not the guard, and asserting it here would grade the gate a second time
// instead of grading this.
func TestIssue6988_RegexKeepsEveryTargetForm(t *testing.T) {
	cases := []struct{ rhs, owner, want string }{
		{`    author = models.ForeignKey('catalog.Author', on_delete=models.CASCADE)`, "Book", "Author"},
		{`    parent = models.ForeignKey('self', null=True, on_delete=models.CASCADE)`, "Node", "Node"},
		{`    profile = models.OneToOneField(Profile, on_delete=models.CASCADE)`, "User", "Profile"},
		{`    tags = models.ManyToManyField(to=Tag, blank=True)`, "Post", "Tag"},
		{`    owner = models.ForeignKey(to='auth.User', on_delete=models.CASCADE)`, "Doc", "User"},
		{`    node = TreeForeignKey(Category, on_delete=models.CASCADE)`, "Item", "Category"},
	}
	for _, c := range cases {
		if got := djangoRelTarget(c.rhs, c.owner); got != c.want {
			t.Errorf("djangoRelTarget(%q, %q) = %q, want %q (#6988 must not break a legitimate form)",
				c.rhs, c.owner, got, c.want)
		}
	}
}

// TestIssue6988_RegexRejectsLowercaseInitialSymbol grades the symbol group's
// UPPERCASE-INITIAL anchor — `([A-Z][A-Za-z0-9_]*)`. Nothing else observes it,
// and widening it to `[A-Za-z]` is production-reachable: on the django corpus it
// mints `LogEntry.user -> Class:settings` (from
// `models.ForeignKey(settings.AUTH_USER_MODEL, ...)`, django/contrib/admin/models.py)
// and `User.friends -> Class:auth`. A lowercase-initial identifier is a module or
// package path, never a Django model name, so it must yield no target.
func TestIssue6988_RegexRejectsLowercaseInitialSymbol(t *testing.T) {
	cases := []string{
		`    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)`,
		`    friends = models.ManyToManyField(auth.User, blank=True)`,
	}
	for _, rhs := range cases {
		if got := djangoRelTarget(rhs, "LogEntry"); got != "" {
			t.Errorf("djangoRelTarget(%q) = %q, want \"\" — a lowercase-initial identifier is a "+
				"module path, not a model; the symbol group must stay anchored to [A-Z] (#6988)", rhs, got)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end, through the real Django extractor.
// ---------------------------------------------------------------------------

// djangoEdges6988 runs the registered python_django extractor over src and
// returns every emitted relationship.
func djangoEdges6988(t *testing.T, src string) []types.RelationshipRecord {
	t.Helper()
	ext, ok := extractor.Get("python_django")
	if !ok {
		t.Fatal("python_django extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "models.py", Content: []byte(src), Language: "python",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var rels []types.RelationshipRecord
	for _, e := range ents {
		rels = append(rels, e.Relationships...)
	}
	return rels
}

func fieldTargetEdgesFrom6988(rels []types.RelationshipRecord, from string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.FromID != from || r.Kind != string(types.RelationshipKindReferences) {
			continue
		}
		for _, p := range r.Properties {
			if p.K == "ref_kind" && p.V == "field_target_type" {
				out = append(out, r)
			}
		}
	}
	return out
}

func hasEdge6988(rels []types.RelationshipRecord, from, to, kind string) bool {
	for _, r := range rels {
		if r.FromID == from && r.ToID == to && r.Kind == kind {
			return true
		}
	}
	return false
}

// The named forbidden case from the issue: `tests/delete/models.py:279`,
// verbatim, plus the sibling declarations that make it a real GFK.
const gfkSrc6988 = `from django.contrib.contenttypes.fields import GenericForeignKey, GenericRelation
from django.db import models


class GenericB1(models.Model):
    content_type = models.ForeignKey(ContentType, on_delete=models.CASCADE)
    object_id = models.PositiveIntegerField()
    generic_delete_top = GenericForeignKey("content_type", "object_id")


class Book(models.Model):
    title = models.CharField(max_length=200)
    created_by_ct = models.ForeignKey(ContentType, on_delete=models.CASCADE)
    created_by_id = models.PositiveIntegerField()
    created_by = GenericForeignKey("created_by_ct", "created_by_id")
    tagged = GenericRelation(TaggedItem)
`

// TestIssue6988_NamedGFKFieldEmitsNoTargetEdge is the forbidden row. Recall
// cannot detect over-firing; only a named negative catches a too-broad
// predicate, and over-firing IS the defect. Asserted BY NAME on
// `GenericB1.generic_delete_top`, and asserted as "no field_target_type edge AT
// ALL" rather than "not to Class:content_type", so a re-broadened predicate
// cannot pass by picking a different wrong target.
func TestIssue6988_NamedGFKFieldEmitsNoTargetEdge(t *testing.T) {
	rels := djangoEdges6988(t, gfkSrc6988)

	if got := fieldTargetEdgesFrom6988(rels, "GenericB1.generic_delete_top"); len(got) != 0 {
		t.Errorf("GenericB1.generic_delete_top emitted %d field_target_type edge(s), want 0; first ToID=%q (#6988)",
			len(got), got[0].ToID)
	}
	// Positive control: the sibling REAL ForeignKey on the same class still
	// resolves, so the negative above is not a vacuous "this file produced
	// nothing" pass.
	if !hasEdge6988(rels, "GenericB1.content_type", pyClassRef("ContentType"), string(types.RelationshipKindReferences)) {
		t.Error("GenericB1.content_type -> Class:ContentType missing: the fix removed a LEGITIMATE edge (#6988)")
	}
	// The GFK field itself must still be a CONTAINS member of its model — the
	// fix removes the wrong REFERENCES edge, not the field node.
	if !hasEdge6988(rels, pyClassRef("GenericB1"), "GenericB1.generic_delete_top", string(types.RelationshipKindContains)) {
		t.Error("GenericB1 CONTAINS generic_delete_top missing: the fix must drop only the target edge (#6988)")
	}
}

// TestIssue6988_WrongButBoundEdgeShapeIsGone pins the FIVE bound edges measured
// on the `django` corpus (gate ON) as a SHAPE, not as a count. Those edges bound
// to the sibling ForeignKey field's Constraint node — `Book.created_by` ->
// `created_by_ct` — so they read as valid and a cardinality assertion is blind
// to their removal by construction (#6973). This asserts the exact substitution
// shape instead.
//
// Layer note: this is a PRE-RESOLUTION emission test, so what it pins is the
// emitted edge shape. The "5 of 21 BIND to a sibling Constraint node" figure is
// corpus-measured (gate ON), NOT observed here — this test does not run the
// resolver and makes no claim about binding.
func TestIssue6988_WrongButBoundEdgeShapeIsGone(t *testing.T) {
	rels := djangoEdges6988(t, gfkSrc6988)

	if hasEdge6988(rels, "Book.created_by", pyClassRef("created_by_ct"), string(types.RelationshipKindReferences)) {
		t.Error("Book.created_by -> Class:created_by_ct still emitted: the wrong-but-BOUND edge shape " +
			"(#6369's wrong-node hazard) survives (#6988)")
	}
	if got := fieldTargetEdgesFrom6988(rels, "Book.created_by"); len(got) != 0 {
		t.Errorf("Book.created_by emitted %d field_target_type edge(s), want 0; first ToID=%q (#6988)",
			len(got), got[0].ToID)
	}
	// The sibling that MINTS the `created_by_ct` node keeps its own real edge.
	if !hasEdge6988(rels, "Book.created_by_ct", pyClassRef("ContentType"), string(types.RelationshipKindReferences)) {
		t.Error("Book.created_by_ct -> Class:ContentType missing (#6988)")
	}
}

// TestIssue6988_GenericRelationEmitsNoTargetEdge pins the neighbour named in the
// issue. `GenericRelation` fails the constructor gate (it ends in neither
// suffix), so it emits no target edge — observed here rather than asserted in
// prose. Adding it is a RECALL question (its argument IS a model), tracked
// separately; this test pins today's behaviour so that change is a visible one.
func TestIssue6988_GenericRelationEmitsNoTargetEdge(t *testing.T) {
	rels := djangoEdges6988(t, gfkSrc6988)
	if got := fieldTargetEdgesFrom6988(rels, "Book.tagged"); len(got) != 0 {
		t.Errorf("Book.tagged (GenericRelation) emitted %d field_target_type edge(s), want 0; first ToID=%q (#6988)",
			len(got), got[0].ToID)
	}
}

// TestIssue6988_LegitimateRelationalFieldsSurvive walks the whole legitimate set
// END TO END, one assertion per member: ForeignKey / OneToOneField /
// ManyToManyField, in string form ('app.Model' and 'self'), symbol form and `to=`
// kwarg form.
func TestIssue6988_LegitimateRelationalFieldsSurvive(t *testing.T) {
	const src = `from django.db import models


class Book(models.Model):
    author = models.ForeignKey('catalog.Author', on_delete=models.CASCADE)
    sequel_to = models.ForeignKey('self', null=True, on_delete=models.CASCADE)
    publisher = models.ForeignKey(Publisher, on_delete=models.PROTECT)
    isbn_record = models.OneToOneField(ISBN, on_delete=models.CASCADE)
    editor = models.OneToOneField(to='staff.Editor', on_delete=models.CASCADE)
    tags = models.ManyToManyField(Tag, blank=True)
    shelves = models.ManyToManyField(to='library.Shelf', blank=True)
    category = TreeForeignKey(Category, on_delete=models.CASCADE)
`
	rels := djangoEdges6988(t, src)
	want := map[string]string{
		"Book.author":      "Author",    // ForeignKey, string 'app.Model'
		"Book.sequel_to":   "Book",      // ForeignKey, string 'self' -> owner
		"Book.publisher":   "Publisher", // ForeignKey, symbol
		"Book.isbn_record": "ISBN",      // OneToOneField, symbol
		"Book.editor":      "Editor",    // OneToOneField, to= kwarg string
		"Book.tags":        "Tag",       // ManyToManyField, symbol
		"Book.shelves":     "Shelf",     // ManyToManyField, to= kwarg string
		"Book.category":    "Category",  // third-party *ForeignKey subclass
	}
	for from, target := range want {
		if !hasEdge6988(rels, from, pyClassRef(target), string(types.RelationshipKindReferences)) {
			t.Errorf("%s -> Class:%s missing — #6988 broke a legitimate relational field", from, target)
		}
	}
}
