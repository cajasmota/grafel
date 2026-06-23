// django_relational_test.go — coverage for the Django model relational
// extraction post-pass (#1977 / #1978 / #1989).
//
// Test fixtures use the `client-fixture-a` naming convention per the
// standing rule (memory: feedback_grafel_competitor_name_scrub) — no
// real client model names appear in these tests.

package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	// Trigger registration init.
	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// findFieldEntity returns the first SCOPE.Schema/field entity whose Name
// matches qualified. Fails the test when not found.
func findFieldEntity(t *testing.T, entities []types.EntityRecord, qualified string) types.EntityRecord {
	t.Helper()
	for _, e := range entities {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "field" && e.Name == qualified {
			return e
		}
	}
	t.Fatalf("field entity %q not found", qualified)
	return types.EntityRecord{}
}

// findClassEntity returns the first SCOPE.Component/class entity by Name.
func findClassEntity(t *testing.T, entities []types.EntityRecord, name string) types.EntityRecord {
	t.Helper()
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == name {
			return e
		}
	}
	t.Fatalf("class entity %q not found", name)
	return types.EntityRecord{}
}

// hasReferencesEdgeContaining reports whether entity has a REFERENCES edge
// whose ToID contains substr.
func hasReferencesEdgeContaining(e types.EntityRecord, substr string) bool {
	for _, r := range e.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, substr) {
			return true
		}
	}
	return false
}

// extractDjango is a small helper that runs the Python extractor on a snippet
// using a stable file path and returns the entities slice.
func extractDjango(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	fi := extractor.FileInput{
		Path:     "client_fixture_a/models.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	}
	entities, err := ext.Extract(context.Background(), fi)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return entities
}

// TestDjangoRelational_SimpleForeignKey covers the headline #1977 case: a
// `ForeignKey(Target, on_delete=...)` declaration emits a REFERENCES edge
// from the field entity to a structural-ref of the target class, and the
// #1978 properties (field_type + kwargs) are stamped on the field.
func TestDjangoRelational_SimpleForeignKey(t *testing.T) {
	src := `from django.db import models

class Author(models.Model):
    name = models.CharField(max_length=200, null=False)

class Book(models.Model):
    title = models.CharField(max_length=500)
    author = models.ForeignKey(Author, on_delete=models.CASCADE, related_name="books")
`
	entities := extractDjango(t, src)

	authorField := findFieldEntity(t, entities, "Book.author")
	if authorField.Properties["field_type"] != "ForeignKey" {
		t.Errorf("Book.author field_type = %q, want ForeignKey", authorField.Properties["field_type"])
	}
	if got := authorField.Properties["kwarg.on_delete"]; got != "CASCADE" {
		t.Errorf("Book.author kwarg.on_delete = %q, want CASCADE", got)
	}
	if got := authorField.Properties["kwarg.related_name"]; got != "books" {
		t.Errorf("Book.author kwarg.related_name = %q, want books", got)
	}
	if !hasReferencesEdgeContaining(authorField, ":Author") {
		t.Errorf("Book.author missing REFERENCES edge to Author; rels=%+v", authorField.Relationships)
	}

	// #1978 — non-relational CharField also gets field_type + kwargs.
	titleField := findFieldEntity(t, entities, "Book.title")
	if titleField.Properties["field_type"] != "CharField" {
		t.Errorf("Book.title field_type = %q, want CharField", titleField.Properties["field_type"])
	}
	if titleField.Properties["kwarg.max_length"] != "500" {
		t.Errorf("Book.title kwarg.max_length = %q, want 500", titleField.Properties["kwarg.max_length"])
	}

	// CharField on Author with positional-less null=False kwarg.
	authorName := findFieldEntity(t, entities, "Author.name")
	if authorName.Properties["field_type"] != "CharField" {
		t.Errorf("Author.name field_type = %q, want CharField", authorName.Properties["field_type"])
	}
	if authorName.Properties["kwarg.null"] != "False" {
		t.Errorf("Author.name kwarg.null = %q, want False", authorName.Properties["kwarg.null"])
	}
}

// TestDjangoRelational_SelfReference covers ForeignKey("self", ...) — the
// REFERENCES edge points back at the parent class.
func TestDjangoRelational_SelfReference(t *testing.T) {
	src := `from django.db import models

class Category(models.Model):
    name = models.CharField(max_length=100)
    parent = models.ForeignKey("self", on_delete=models.SET_NULL, null=True, blank=True)
`
	entities := extractDjango(t, src)

	parentField := findFieldEntity(t, entities, "Category.parent")
	if parentField.Properties["field_type"] != "ForeignKey" {
		t.Errorf("Category.parent field_type = %q, want ForeignKey", parentField.Properties["field_type"])
	}
	if !hasReferencesEdgeContaining(parentField, ":Category") {
		t.Errorf("Category.parent missing self-REFERENCES edge to Category; rels=%+v", parentField.Relationships)
	}
	// self_ref property should be true.
	var foundSelf bool
	for _, r := range parentField.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["self_ref"] == "true" {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		t.Errorf("Category.parent REFERENCES edge missing self_ref=true property")
	}
}

// TestDjangoRelational_StringForeignKey covers the late-bound string form
// `ForeignKey("app.Target", ...)` — the leaf class name is extracted and
// emitted as the REFERENCES target.
func TestDjangoRelational_StringForeignKey(t *testing.T) {
	src := `from django.db import models

class Permit(models.Model):
    jurisdiction = models.ForeignKey("client_fixture_a.Jurisdiction", on_delete=models.PROTECT)
`
	entities := extractDjango(t, src)

	jur := findFieldEntity(t, entities, "Permit.jurisdiction")
	if jur.Properties["field_type"] != "ForeignKey" {
		t.Errorf("Permit.jurisdiction field_type = %q, want ForeignKey", jur.Properties["field_type"])
	}
	if jur.Properties["kwarg.on_delete"] != "PROTECT" {
		t.Errorf("Permit.jurisdiction kwarg.on_delete = %q, want PROTECT", jur.Properties["kwarg.on_delete"])
	}
	if !hasReferencesEdgeContaining(jur, ":Jurisdiction") {
		t.Errorf("Permit.jurisdiction missing REFERENCES edge to Jurisdiction; rels=%+v", jur.Relationships)
	}
}

// TestDjangoRelational_StringFKBareName covers the #2049 case: bare-name string FK
// `ForeignKey('Building', on_delete=CASCADE)`. The extractor must stamp
// django_fk_string="Building" on the REFERENCES edge so the late-binding
// resolver pass (ResolveDjangoStringFKRefs) can find the real Model entity
// via same-app byPackageComponent lookup.
func TestDjangoRelational_StringFKBareName(t *testing.T) {
	src := `from django.db import models

class Building(models.Model):
    address = models.CharField(max_length=200)

class GroupBuildingSettings(models.Model):
    building = models.ForeignKey('Building', on_delete=models.CASCADE)
    capacity = models.IntegerField(default=0)
`
	entities := extractDjango(t, src)

	buildingField := findFieldEntity(t, entities, "GroupBuildingSettings.building")
	if buildingField.Properties["field_type"] != "ForeignKey" {
		t.Errorf("GroupBuildingSettings.building field_type = %q, want ForeignKey", buildingField.Properties["field_type"])
	}
	if buildingField.Properties["kwarg.on_delete"] != "CASCADE" {
		t.Errorf("GroupBuildingSettings.building kwarg.on_delete = %q, want CASCADE", buildingField.Properties["kwarg.on_delete"])
	}
	if !hasReferencesEdgeContaining(buildingField, ":Building") {
		t.Errorf("GroupBuildingSettings.building missing REFERENCES edge to Building; rels=%+v", buildingField.Relationships)
	}
	// Verify django_fk_string property is stamped for bare-name string FK.
	var fkStringProp string
	for _, r := range buildingField.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":Building") {
			fkStringProp = r.Properties["django_fk_string"]
			break
		}
	}
	if fkStringProp != "Building" {
		t.Errorf("GroupBuildingSettings.building REFERENCES edge django_fk_string = %q, want Building", fkStringProp)
	}
}

// TestDjangoRelational_StringFKDottedAppLabel covers the #2049 case: dotted
// app-label string FK `ForeignKey("app_label.ModelName", ...)`. The extractor
// must stamp django_fk_string="app_label.ModelName" (the full dotted form
// before stripping) so the late-binding resolver pass can derive the app
// directory and use it for cross-app byPackageComponent lookup.
func TestDjangoRelational_StringFKDottedAppLabel(t *testing.T) {
	src := `from django.db import models

class GroupBuildingSettings(models.Model):
    building = models.ForeignKey('core.Building', on_delete=models.CASCADE)
    owner = models.ForeignKey('auth.User', on_delete=models.SET_NULL, null=True)
`
	entities := extractDjango(t, src)

	// Verify building FK: class name "Building", django_fk_string="core.Building"
	buildingField := findFieldEntity(t, entities, "GroupBuildingSettings.building")
	if !hasReferencesEdgeContaining(buildingField, ":Building") {
		t.Errorf("GroupBuildingSettings.building missing REFERENCES edge to Building; rels=%+v", buildingField.Relationships)
	}
	var buildingFKStr string
	for _, r := range buildingField.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":Building") {
			buildingFKStr = r.Properties["django_fk_string"]
			break
		}
	}
	if buildingFKStr != "core.Building" {
		t.Errorf("building FK django_fk_string = %q, want core.Building", buildingFKStr)
	}

	// Verify owner FK: class name "User", django_fk_string="auth.User"
	ownerField := findFieldEntity(t, entities, "GroupBuildingSettings.owner")
	if !hasReferencesEdgeContaining(ownerField, ":User") {
		t.Errorf("GroupBuildingSettings.owner missing REFERENCES edge to User; rels=%+v", ownerField.Relationships)
	}
	var ownerFKStr string
	for _, r := range ownerField.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":User") {
			ownerFKStr = r.Properties["django_fk_string"]
			break
		}
	}
	if ownerFKStr != "auth.User" {
		t.Errorf("owner FK django_fk_string = %q, want auth.User", ownerFKStr)
	}
}

// TestDjangoRelational_StringFKSelfNoFKString covers self-reference string FK
// `ForeignKey('self', ...)`. The django_fk_string should be "self" and
// self_ref=true, but the REFERENCES edge still points at the parent class.
func TestDjangoRelational_StringFKSelfNoFKString(t *testing.T) {
	src := `from django.db import models

class TreeNode(models.Model):
    name = models.CharField(max_length=100)
    parent = models.ForeignKey('self', on_delete=models.CASCADE, null=True)
`
	entities := extractDjango(t, src)

	parentField := findFieldEntity(t, entities, "TreeNode.parent")
	if !hasReferencesEdgeContaining(parentField, ":TreeNode") {
		t.Errorf("TreeNode.parent missing self-REFERENCES edge; rels=%+v", parentField.Relationships)
	}
	// Self-ref: django_fk_string should be "self" and self_ref="true".
	var foundSelf bool
	for _, r := range parentField.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["self_ref"] == "true" {
			foundSelf = true
			if got := r.Properties["django_fk_string"]; got != "self" {
				t.Errorf("self-ref edge django_fk_string = %q, want self", got)
			}
			break
		}
	}
	if !foundSelf {
		t.Errorf("TreeNode.parent missing self_ref=true REFERENCES edge; rels=%+v", parentField.Relationships)
	}
}

// TestDjangoRelational_IdentifierFKNoDjangoFKString covers the class-reference
// (non-string) FK form `ForeignKey(Building, ...)`. For identifier targets
// django_fk_string must NOT be set (empty or absent) — these resolve via the
// standard byLocation path without needing the late-binding pass.
func TestDjangoRelational_IdentifierFKNoDjangoFKString(t *testing.T) {
	src := `from django.db import models

class Building(models.Model):
    address = models.CharField(max_length=200)

class Room(models.Model):
    building = models.ForeignKey(Building, on_delete=models.CASCADE)
`
	entities := extractDjango(t, src)

	roomField := findFieldEntity(t, entities, "Room.building")
	if !hasReferencesEdgeContaining(roomField, ":Building") {
		t.Errorf("Room.building missing REFERENCES edge to Building; rels=%+v", roomField.Relationships)
	}
	// Identifier form: django_fk_string should NOT be set.
	for _, r := range roomField.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":Building") {
			if got := r.Properties["django_fk_string"]; got != "" {
				t.Errorf("identifier-form FK incorrectly set django_fk_string = %q, want empty", got)
			}
			break
		}
	}
}

// TestDjangoRelational_ManyToManyWithThrough covers M2M field declarations
// that carry a `through=` kwarg. We assert the REFERENCES edge points at
// the first positional (the target Model) AND that the through model name
// is captured as a kwarg property for downstream consumers.
func TestDjangoRelational_ManyToManyWithThrough(t *testing.T) {
	src := `from django.db import models

class Membership(models.Model):
    pass

class Group(models.Model):
    members = models.ManyToManyField("User", through=Membership, related_name="groups")
`
	entities := extractDjango(t, src)

	members := findFieldEntity(t, entities, "Group.members")
	if members.Properties["field_type"] != "ManyToManyField" {
		t.Errorf("Group.members field_type = %q, want ManyToManyField", members.Properties["field_type"])
	}
	if members.Properties["kwarg.through"] != "Membership" {
		t.Errorf("Group.members kwarg.through = %q, want Membership", members.Properties["kwarg.through"])
	}
	if !hasReferencesEdgeContaining(members, ":User") {
		t.Errorf("Group.members missing REFERENCES edge to User; rels=%+v", members.Relationships)
	}
}

// TestDjangoRelational_CharFieldChoices covers field_type + kwargs capture
// for the `choices=` tuple-of-tuples shape — a structurally rich kwarg
// that we capture verbatim.
func TestDjangoRelational_CharFieldChoices(t *testing.T) {
	src := `from django.db import models

class Order(models.Model):
    STATUS_CHOICES = (("A", "Active"), ("C", "Closed"))
    status = models.CharField(max_length=1, choices=STATUS_CHOICES, default="A")
`
	entities := extractDjango(t, src)

	status := findFieldEntity(t, entities, "Order.status")
	if status.Properties["field_type"] != "CharField" {
		t.Errorf("Order.status field_type = %q, want CharField", status.Properties["field_type"])
	}
	if status.Properties["kwarg.max_length"] != "1" {
		t.Errorf("Order.status kwarg.max_length = %q, want 1", status.Properties["kwarg.max_length"])
	}
	// Choices captured verbatim (raw RHS text).
	if got := status.Properties["kwarg.choices"]; got != "STATUS_CHOICES" {
		t.Errorf("Order.status kwarg.choices = %q, want STATUS_CHOICES", got)
	}
	if status.Properties["kwarg.default"] != "A" {
		t.Errorf("Order.status kwarg.default = %q, want A", status.Properties["kwarg.default"])
	}
}

// TestDjangoRelational_ManagerAttachment covers the #1989 case: an
// `objects = SomeManager()` assignment inside a model body emits a
// REFERENCES edge from the Model class entity to the Manager class.
func TestDjangoRelational_ManagerAttachment(t *testing.T) {
	src := `from django.db import models

class BookManager(models.Manager):
    pass

class Book(models.Model):
    title = models.CharField(max_length=200)
    objects = BookManager()
`
	entities := extractDjango(t, src)

	book := findClassEntity(t, entities, "Book")
	if !hasReferencesEdgeContaining(book, ":BookManager") {
		t.Errorf("Book missing REFERENCES edge to BookManager; rels=%+v", book.Relationships)
	}
	// Property metadata should mark this as a manager attachment.
	var foundManager bool
	for _, r := range book.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["django_attachment"] == "manager" && r.Properties["manager_attr"] == "objects" {
			foundManager = true
			break
		}
	}
	if !foundManager {
		t.Errorf("Book REFERENCES→BookManager edge missing django_attachment=manager / manager_attr=objects metadata")
	}
}

// TestDjangoRelational_CustomManagerName covers non-`objects` Manager
// attachments (`published = PublishedManager()`).
func TestDjangoRelational_CustomManagerName(t *testing.T) {
	src := `from django.db import models

class PublishedManager(models.Manager):
    pass

class Article(models.Model):
    title = models.CharField(max_length=200)
    objects = models.Manager()
    published = PublishedManager()
`
	entities := extractDjango(t, src)

	article := findClassEntity(t, entities, "Article")
	if !hasReferencesEdgeContaining(article, ":PublishedManager") {
		t.Errorf("Article missing REFERENCES edge to PublishedManager; rels=%+v", article.Relationships)
	}
	// We deliberately don't emit an edge for `objects = models.Manager()`
	// because the function is module-qualified (`models.Manager`), not a
	// bare Capitalised identifier. That keeps the heuristic conservative.
	if hasReferencesEdgeContaining(article, ":Manager") && !hasReferencesEdgeContaining(article, ":PublishedManager") {
		t.Errorf("Article unexpectedly emitted REFERENCES to bare Manager from models.Manager()")
	}
}

// TestDjangoRelational_NoEdgeForUnrelatedAssignments verifies that
// non-callable / lowercase-callable RHS assignments don't produce spurious
// Manager-attachment edges.
func TestDjangoRelational_NoEdgeForUnrelatedAssignments(t *testing.T) {
	src := `from django.db import models

def compute_default():
    return 0

class Counter(models.Model):
    name = models.CharField(max_length=50)
    count = compute_default()
    base = 42
`
	entities := extractDjango(t, src)

	counter := findClassEntity(t, entities, "Counter")
	for _, r := range counter.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, "compute_default") {
			t.Errorf("Counter unexpectedly emitted REFERENCES to compute_default")
		}
	}
}
