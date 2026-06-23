// python_relational_bundle_test.go — coverage for the Python relational
// edges bundle (#1977 retest + #2007 / #2008 / #2009 / #2010 / #2011).
//
// All fixtures use the `client-fixture-a` naming convention per the
// standing rule — no real client names appear.

package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/extractors/python"
)

// runPy is a small helper that runs the Python extractor on a snippet
// at the supplied file path and returns the entities slice.
func runPy(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	fi := extractor.FileInput{
		Path:     path,
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

// findEnt returns the first entity matching kind+subtype+name. Fails the
// test when no match is found.
func findEnt(t *testing.T, entities []types.EntityRecord, kind, subtype, name string) types.EntityRecord {
	t.Helper()
	for _, e := range entities {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return e
		}
	}
	t.Fatalf("entity %s/%s %q not found", kind, subtype, name)
	return types.EntityRecord{}
}

// hasRelKind reports whether e has a relationship of kind k whose ToID
// contains substr.
func hasRelKind(e types.EntityRecord, k, substr string) bool {
	for _, r := range e.Relationships {
		if r.Kind == k && strings.Contains(r.ToID, substr) {
			return true
		}
	}
	return false
}

// ============================================================================
// #1977 retest — ForeignKey REFERENCES is emitted post-bundle-A. W6R2 had
// flagged the surface as still broken, but Bundle A's enrich pass already
// stamps the edge; this test pins the behaviour so a future regression
// shows up here, and documents that any downstream surface gap (e.g. the
// NeighbourBrief Properties surface in #2025) is not an extractor bug.
// ============================================================================

func TestIssue1977_ForeignKey_References_StillEmitted(t *testing.T) {
	src := `from django.db import models

class Jurisdiction(models.Model):
    name = models.CharField(max_length=100)

class Building(models.Model):
    jurisdiction = models.ForeignKey(Jurisdiction, on_delete=models.CASCADE)
`
	entities := runPy(t, "client_fixture_a/buildings/models.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "Building.jurisdiction")
	if !hasRelKind(field, "REFERENCES", ":Jurisdiction") {
		t.Fatalf("expected Building.jurisdiction REFERENCES → Jurisdiction; rels=%+v", field.Relationships)
	}
}

// ============================================================================
// #2007 — Nested constructor call inside a method body emits REFERENCES.
// ============================================================================

func TestIssue2007_NestedConstructorInMethodEmitsReferences(t *testing.T) {
	src := `class WidgetSerializer:
    pass

class ParentSerializer:
    def get_widget(self, obj):
        return WidgetSerializer().data

    def get_thing(self, obj):
        helper = WidgetSerializer()
        return helper
`
	entities := runPy(t, "client_fixture_a/api/serializers.py", src)
	method := findEnt(t, entities, "SCOPE.Operation", "method", "ParentSerializer.get_widget")
	if !hasRelKind(method, "REFERENCES", ":WidgetSerializer") {
		t.Fatalf("expected ParentSerializer.get_widget REFERENCES → WidgetSerializer; rels=%+v", method.Relationships)
	}
	// nested_ctor provenance property is stamped.
	found := false
	for _, r := range method.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["nested_ctor"] == "true" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one REFERENCES edge with nested_ctor=true; rels=%+v", method.Relationships)
	}

	// Dedup: get_thing also constructs WidgetSerializer — should be one edge.
	thing := findEnt(t, entities, "SCOPE.Operation", "method", "ParentSerializer.get_thing")
	count := 0
	for _, r := range thing.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":WidgetSerializer") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 nested_ctor REFERENCES to WidgetSerializer from get_thing, got %d", count)
	}
}

// ============================================================================
// #2008 — SerializerMethodField → get_<field> RESOLVED_BY edge.
// ============================================================================

func TestIssue2008_SerializerMethodFieldImplicitName(t *testing.T) {
	src := `from rest_framework import serializers

class FooSerializer(serializers.ModelSerializer):
    full_name = serializers.SerializerMethodField()

    def get_full_name(self, obj):
        return obj.name
`
	entities := runPy(t, "client_fixture_a/api/serializers.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "FooSerializer.full_name")
	if !hasRelKind(field, "RESOLVED_BY", "FooSerializer.get_full_name") {
		t.Fatalf("expected FooSerializer.full_name RESOLVED_BY → get_full_name; rels=%+v", field.Relationships)
	}
}

func TestIssue2008_SerializerMethodFieldExplicitName(t *testing.T) {
	src := `from rest_framework import serializers

class FooSerializer(serializers.ModelSerializer):
    full_name = serializers.SerializerMethodField(method_name="compute_full")

    def compute_full(self, obj):
        return obj.name
`
	entities := runPy(t, "client_fixture_a/api/serializers.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "FooSerializer.full_name")
	if !hasRelKind(field, "RESOLVED_BY", "FooSerializer.compute_full") {
		t.Fatalf("expected explicit method_name link; rels=%+v", field.Relationships)
	}
}

func TestIssue2008_SerializerMethodField_MissingMethodNoEdge(t *testing.T) {
	// The implementing method doesn't exist — the pass must NOT
	// fabricate the edge.
	src := `from rest_framework import serializers

class FooSerializer(serializers.ModelSerializer):
    full_name = serializers.SerializerMethodField()
`
	entities := runPy(t, "client_fixture_a/api/serializers.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "FooSerializer.full_name")
	for _, r := range field.Relationships {
		if r.Kind == "RESOLVED_BY" {
			t.Fatalf("did not expect RESOLVED_BY edge when implementor is absent; got %+v", r)
		}
	}
}

// ============================================================================
// #2009 — Indirect Model refs (`choices=User.TYPE_CHOICES`) emit REFERENCES.
// ============================================================================

func TestIssue2009_ChoicesAttributeEmitsReferences(t *testing.T) {
	src := `from django.db import models

class User(models.Model):
    TYPE_CHOICES = [("a", "A"), ("b", "B")]

class Profile(models.Model):
    user_type = models.IntegerField(choices=User.TYPE_CHOICES)
`
	entities := runPy(t, "client_fixture_a/accounts/models.py", src)
	profile := findEnt(t, entities, "SCOPE.Component", "class", "Profile")
	if !hasRelKind(profile, "REFERENCES", ":User") {
		t.Fatalf("expected Profile class REFERENCES → User from choices attribute; rels=%+v", profile.Relationships)
	}
	found := false
	for _, r := range profile.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["nested_ctor"] == "true" && strings.Contains(r.ToID, ":User") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected REFERENCES edge with nested_ctor=true and target_class=User; rels=%+v", profile.Relationships)
	}
}

// ============================================================================
// #2010 — router.register(prefix, ViewSet, ...) REFERENCES from urls.py.
// ============================================================================

func TestIssue2010_RouterRegisterEmitsEdge(t *testing.T) {
	src := `from rest_framework.routers import DefaultRouter
from .views import PermitViewSet, JurisdictionViewSet

router = DefaultRouter()
router.register(r"permits", PermitViewSet, basename="permit")
router.register(r"jurisdictions", JurisdictionViewSet)
`
	entities := runPy(t, "client_fixture_a/api/urls.py", src)
	// File entity carries the REFERENCES edges.
	fileEnt := findEnt(t, entities, "SCOPE.Component", "file", "client_fixture_a/api/urls.py")
	if !hasRelKind(fileEnt, "REFERENCES", ":PermitViewSet") {
		t.Fatalf("expected REFERENCES → PermitViewSet on urls.py; rels=%+v", fileEnt.Relationships)
	}
	if !hasRelKind(fileEnt, "REFERENCES", ":JurisdictionViewSet") {
		t.Fatalf("expected REFERENCES → JurisdictionViewSet on urls.py; rels=%+v", fileEnt.Relationships)
	}
	// Property check: url_prefix + basename are stamped on the
	// PermitViewSet edge.
	var permitEdge types.RelationshipRecord
	for _, r := range fileEnt.Relationships {
		if r.Kind == "REFERENCES" && strings.Contains(r.ToID, ":PermitViewSet") {
			permitEdge = r
			break
		}
	}
	if permitEdge.Properties["url_prefix"] != "permits" {
		t.Errorf("url_prefix = %q, want permits", permitEdge.Properties["url_prefix"])
	}
	if permitEdge.Properties["basename"] != "permit" {
		t.Errorf("basename = %q, want permit", permitEdge.Properties["basename"])
	}
	if permitEdge.Properties["router_register"] != "true" {
		t.Errorf("router_register property missing")
	}
}

func TestIssue2010_NonUrlsFileIsNoOp(t *testing.T) {
	// Same call shape in a non-urls module must NOT emit the edge.
	src := `from rest_framework.routers import DefaultRouter
router = DefaultRouter()
router.register(r"permits", PermitViewSet)
`
	entities := runPy(t, "client_fixture_a/api/handlers.py", src)
	fileEnt := findEnt(t, entities, "SCOPE.Component", "file", "client_fixture_a/api/handlers.py")
	for _, r := range fileEnt.Relationships {
		if r.Kind == "REFERENCES" && r.Properties["router_register"] == "true" {
			t.Fatalf("router_register edge emitted from non-urls file: %+v", r)
		}
	}
}

// ============================================================================
// #2011 — CBV inherited methods annotation.
// ============================================================================

func TestIssue2011_ModelViewSetInheritedMethods(t *testing.T) {
	src := `from rest_framework import viewsets

class PermitViewSet(viewsets.ModelViewSet):
    pass
`
	entities := runPy(t, "client_fixture_a/api/views.py", src)
	cls := findEnt(t, entities, "SCOPE.Component", "class", "PermitViewSet")
	got := cls.Properties["inherited_methods"]
	want := "create,destroy,list,partial_update,retrieve,update"
	if got != want {
		t.Errorf("inherited_methods = %q, want %q", got, want)
	}
	if cls.Properties["cbv_bases"] != "ModelViewSet" {
		t.Errorf("cbv_bases = %q, want ModelViewSet", cls.Properties["cbv_bases"])
	}
}

func TestIssue2011_ListViewInheritedMethods(t *testing.T) {
	src := `from django.views.generic import ListView

class PermitListView(ListView):
    pass
`
	entities := runPy(t, "client_fixture_a/web/views.py", src)
	cls := findEnt(t, entities, "SCOPE.Component", "class", "PermitListView")
	if cls.Properties["inherited_methods"] != "get" {
		t.Errorf("ListView subclass inherited_methods = %q, want get", cls.Properties["inherited_methods"])
	}
}

func TestIssue2011_UnknownBaseIsNoOp(t *testing.T) {
	// User-defined base class that we don't recognise: no annotation.
	src := `class MyBase:
    pass

class MySub(MyBase):
    pass
`
	entities := runPy(t, "client_fixture_a/random.py", src)
	cls := findEnt(t, entities, "SCOPE.Component", "class", "MySub")
	if _, exists := cls.Properties["inherited_methods"]; exists {
		t.Errorf("unknown-base subclass should not carry inherited_methods; got %q", cls.Properties["inherited_methods"])
	}
}

func TestIssue2011_MultipleBasesUnion(t *testing.T) {
	src := `from rest_framework import mixins
from rest_framework.viewsets import GenericViewSet

class CustomViewSet(mixins.ListModelMixin, mixins.RetrieveModelMixin, GenericViewSet):
    pass
`
	entities := runPy(t, "client_fixture_a/api/views.py", src)
	cls := findEnt(t, entities, "SCOPE.Component", "class", "CustomViewSet")
	got := cls.Properties["inherited_methods"]
	want := "list,retrieve"
	if got != want {
		t.Errorf("multi-mixin inherited_methods = %q, want %q", got, want)
	}
}
