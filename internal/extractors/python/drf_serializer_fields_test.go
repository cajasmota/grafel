// drf_serializer_fields_test.go — Issue #2061 regression coverage.
//
// Each test pins a single DRF serializer field shape and asserts the
// expected REFERENCES edge is emitted on the SCOPE.Schema/field entity.

package python_test

import (
	"testing"
)

// (1) PrimaryKeyRelatedField(queryset=Foo.objects.all()) → REFERENCES → Foo.
func TestIssue2061_PrimaryKeyRelatedField_QuerysetEmitsReferences(t *testing.T) {
	src := `from rest_framework import serializers

class Building:
    pass

class ContractSerializer(serializers.ModelSerializer):
    building = serializers.PrimaryKeyRelatedField(queryset=Building.objects.all())

    class Meta:
        model = Contract
        fields = ['building']
`
	entities := runPy(t, "client_fixture_a/serializers/contract_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "ContractSerializer.building")
	if !hasRelKind(field, "REFERENCES", ":Building") {
		t.Fatalf("expected ContractSerializer.building REFERENCES → Building; rels=%+v", field.Relationships)
	}
}

// (1b) SlugRelatedField with queryset.
func TestIssue2061_SlugRelatedField_QuerysetEmitsReferences(t *testing.T) {
	src := `from rest_framework import serializers

class Client:
    pass

class ContractSerializer(serializers.ModelSerializer):
    client = serializers.SlugRelatedField(queryset=Client.objects.all(), slug_field='name')

    class Meta:
        model = Contract
        fields = ['client']
`
	entities := runPy(t, "client_fixture_a/serializers/contract_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "ContractSerializer.client")
	if !hasRelKind(field, "REFERENCES", ":Client") {
		t.Fatalf("expected ContractSerializer.client REFERENCES → Client; rels=%+v", field.Relationships)
	}
}

// (2) Nested serializer reference: `device = DeviceLiteSerializer(read_only=True)`.
func TestIssue2061_NestedSerializerReference_EmitsReferences(t *testing.T) {
	src := `from rest_framework import serializers

class DeviceLiteSerializer(serializers.Serializer):
    pass

class ContractSerializer(serializers.ModelSerializer):
    device = DeviceLiteSerializer(read_only=True)

    class Meta:
        model = Contract
        fields = ['device']
`
	entities := runPy(t, "client_fixture_a/serializers/contract_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "ContractSerializer.device")
	if !hasRelKind(field, "REFERENCES", ":DeviceLiteSerializer") {
		t.Fatalf("expected ContractSerializer.device REFERENCES → DeviceLiteSerializer; rels=%+v", field.Relationships)
	}
}

// (3) source="contract.id" → REFERENCES Meta.model (root of the source path).
func TestIssue2061_SourcePathField_EmitsReferencesToMetaModel(t *testing.T) {
	src := `from rest_framework import serializers

class ContractDeviceSerializer(serializers.ModelSerializer):
    contract_id = serializers.IntegerField(source="contract.id")

    class Meta:
        model = ContractDevice
        fields = ['contract_id']
`
	entities := runPy(t, "client_fixture_a/serializers/contract_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "ContractDeviceSerializer.contract_id")
	if !hasRelKind(field, "REFERENCES", ":ContractDevice") {
		t.Fatalf("expected ContractDeviceSerializer.contract_id REFERENCES → ContractDevice (meta_model); rels=%+v", field.Relationships)
	}
}

// (4) Plain scalar field inside a ModelSerializer → REFERENCES Meta.model.
func TestIssue2061_ScalarField_ImplicitMetaModelBinding(t *testing.T) {
	src := `from rest_framework import serializers

class UserSerializer(serializers.ModelSerializer):
    email = serializers.EmailField()

    class Meta:
        model = User
        fields = ['email']
`
	entities := runPy(t, "client_fixture_a/serializers/user_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "UserSerializer.email")
	if !hasRelKind(field, "REFERENCES", ":User") {
		t.Fatalf("expected UserSerializer.email REFERENCES → User; rels=%+v", field.Relationships)
	}
}

// (5) NEGATIVE — Django model scalar field (no Meta.model) must NOT get an
// implicit REFERENCES. enrichDjangoModelFieldsAndManagers handles model fields
// and only emits REFERENCES for FK/O2O/M2M.
func TestIssue2061_DjangoModelScalar_NoSpuriousReferences(t *testing.T) {
	src := `from django.db import models

class Contract(models.Model):
    title = models.CharField(max_length=255)
`
	entities := runPy(t, "client_fixture_a/models/contract.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "Contract.title")
	for _, r := range field.Relationships {
		if r.Kind == "REFERENCES" {
			t.Fatalf("Django model scalar should NOT emit REFERENCES from #2061 pass; got %+v", r)
		}
	}
}

// (6) Dedup — running multiple matching rules on the same field emits only
// one REFERENCES edge per target. We exercise this by giving a nested
// serializer field a `source=` kwarg that points to the same target shape.
func TestIssue2061_RuleDedup_NestedSerializerWithSource(t *testing.T) {
	src := `from rest_framework import serializers

class DeviceSerializer(serializers.Serializer):
    pass

class ContractSerializer(serializers.ModelSerializer):
    device = DeviceSerializer(source='related_device', read_only=True)

    class Meta:
        model = Contract
        fields = ['device']
`
	entities := runPy(t, "client_fixture_a/serializers/contract_serializer.py", src)
	field := findEnt(t, entities, "SCOPE.Schema", "field", "ContractSerializer.device")
	count := 0
	for _, r := range field.Relationships {
		if r.Kind == "REFERENCES" {
			count++
		}
	}
	if count == 0 {
		t.Fatalf("expected at least one REFERENCES edge; got none. rels=%+v", field.Relationships)
	}
	// Nested serializer rule fires first and `continue`s, so we expect exactly 1.
	if count != 1 {
		t.Fatalf("expected exactly one REFERENCES edge (nested rule wins); got %d. rels=%+v", count, field.Relationships)
	}
}
