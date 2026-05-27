// Tests for the #2578 Serializer/Signal/FilterSet → Model cross-ref passes.
package engine

import (
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func fileMapReader2578(files map[string]string) NestedURLConfFileReader {
	return func(relPath string) []byte {
		if s, ok := files[relPath]; ok {
			return []byte(s)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// TestPyExtractor_SerializerMetaModel_EmitsReferences
// ---------------------------------------------------------------------------

func TestPyExtractor_SerializerMetaModel_EmitsReferences(t *testing.T) {
	files := map[string]string{
		"core/serializers/group_device_settings_serializer.py": `from rest_framework import serializers
from core.models import GroupDeviceSettings

class ReadGroupDeviceSettingsSerializer(serializers.ModelSerializer):
    class Meta:
        model = GroupDeviceSettings
        fields = "__all__"

class WriteGroupDeviceSettingsSerializer(serializers.ModelSerializer):
    class Meta:
        model = GroupDeviceSettings
        fields = ["id", "name"]
`,
		"core/serializers/device_serializer.py": `from rest_framework import serializers
from core.models import Device

class DeviceSerializer(serializers.ModelSerializer):
    class Meta:
        model = Device
        fields = ["id", "serial_number"]
`,
		// Non-serializer file — should produce zero edges.
		"core/views/dashboard.py": `def dashboard(request):
    pass
`,
	}

	paths := []string{
		"core/serializers/group_device_settings_serializer.py",
		"core/serializers/device_serializer.py",
		"core/views/dashboard.py",
	}
	reader := fileMapReader2578(files)

	rels := ApplySerializerMetaModelEdges(paths, reader)

	// Expect 3 REFERENCES edges: 2 from the first file + 1 from the second.
	refs := relsOfKind(rels, "REFERENCES")
	if len(refs) != 3 {
		t.Fatalf("expected 3 REFERENCES edges, got %d: %v", len(refs), refs)
	}

	// Verify each edge targets the correct model.
	wantEdges := map[string]string{
		"Class:ReadGroupDeviceSettingsSerializer":  "Class:GroupDeviceSettings",
		"Class:WriteGroupDeviceSettingsSerializer": "Class:GroupDeviceSettings",
		"Class:DeviceSerializer":                   "Class:Device",
	}
	for _, r := range refs {
		want, ok := wantEdges[r.FromID]
		if !ok {
			t.Errorf("unexpected FromID %q in REFERENCES edge %v", r.FromID, r)
			continue
		}
		if r.ToID != want {
			t.Errorf("edge from %q: want ToID %q got %q", r.FromID, want, r.ToID)
		}
		if r.Properties["pattern_type"] != "serializer_meta_model" {
			t.Errorf("edge from %q: want pattern_type serializer_meta_model, got %q",
				r.FromID, r.Properties["pattern_type"])
		}
		if r.Properties["framework"] != "drf" {
			t.Errorf("edge from %q: want framework drf, got %q",
				r.FromID, r.Properties["framework"])
		}
	}
}

func TestPyExtractor_SerializerMetaModel_NilReader(t *testing.T) {
	rels := ApplySerializerMetaModelEdges([]string{"a.py"}, nil)
	if len(rels) != 0 {
		t.Fatalf("expected nil reader to return zero edges, got %d", len(rels))
	}
}

func TestPyExtractor_SerializerMetaModel_NoSerializerFiles(t *testing.T) {
	files := map[string]string{
		"core/models/group.py": `from django.db import models

class Group(models.Model):
    name = models.CharField(max_length=255)
    class Meta:
        db_table = "groups"
`,
	}
	rels := ApplySerializerMetaModelEdges([]string{"core/models/group.py"}, fileMapReader2578(files))
	if len(rels) != 0 {
		t.Fatalf("expected 0 edges for non-serializer file, got %d: %v", len(rels), rels)
	}
}

// ---------------------------------------------------------------------------
// TestPyExtractor_ReceiverDecorator_EmitsListensFor
// ---------------------------------------------------------------------------

func TestPyExtractor_ReceiverDecorator_EmitsListensFor(t *testing.T) {
	// upvate_core pattern: multiple stacked @receiver decorators on one def.
	files := map[string]string{
		"core/signals/replicate_to_datalake.py": `from django.db.models.signals import post_save
from django.dispatch import receiver
from core.models import Group, Client, Device, GroupDeviceSettings

@receiver(post_save, sender=Group)
@receiver(post_save, sender=Client)
@receiver(post_save, sender=Device)
@receiver(post_save, sender=GroupDeviceSettings)
def replicate_signal(sender, instance, created, **kwargs):
    pass
`,
		// Single-sender receiver.
		"core/signals/notify.py": `from django.db.models.signals import post_delete
from django.dispatch import receiver
from core.models import Contract

@receiver(post_delete, sender=Contract)
def on_contract_deleted(sender, instance, **kwargs):
    pass
`,
	}

	paths := []string{
		"core/signals/replicate_to_datalake.py",
		"core/signals/notify.py",
	}
	reader := fileMapReader2578(files)

	rels := ApplyReceiverSenderEdges(paths, reader)

	// Expect 4 edges from the first file + 1 from the second = 5 total.
	handlesSignal := relsOfKind(rels, "HANDLES_SIGNAL")
	if len(handlesSignal) != 5 {
		t.Fatalf("expected 5 HANDLES_SIGNAL edges, got %d: %v", len(handlesSignal), rels)
	}

	// Confirm GroupDeviceSettings is among the targets.
	found := false
	for _, r := range handlesSignal {
		if r.ToID == "Class:GroupDeviceSettings" {
			found = true
			if r.FromID != "Function:replicate_signal" {
				t.Errorf("GroupDeviceSettings listener: want FromID Function:replicate_signal, got %q", r.FromID)
			}
			if r.Properties["pattern_type"] != "receiver_sender_model" {
				t.Errorf("GroupDeviceSettings listener: want pattern_type receiver_sender_model, got %q",
					r.Properties["pattern_type"])
			}
		}
	}
	if !found {
		t.Error("expected a HANDLES_SIGNAL edge targeting Class:GroupDeviceSettings, not found")
	}

	// Confirm Contract delete handler.
	foundContract := false
	for _, r := range handlesSignal {
		if r.ToID == "Class:Contract" && r.FromID == "Function:on_contract_deleted" {
			foundContract = true
		}
	}
	if !foundContract {
		t.Error("expected HANDLES_SIGNAL from on_contract_deleted to Class:Contract, not found")
	}
}

func TestPyExtractor_ReceiverDecorator_BareReceiverSkipped(t *testing.T) {
	// A bare @receiver(post_save) without sender= must NOT produce an edge.
	files := map[string]string{
		"core/signals/generic.py": `from django.db.models.signals import post_save
from django.dispatch import receiver

@receiver(post_save)
def catch_all(sender, instance, **kwargs):
    pass
`,
	}
	rels := ApplyReceiverSenderEdges([]string{"core/signals/generic.py"}, fileMapReader2578(files))
	if len(rels) != 0 {
		t.Fatalf("expected 0 edges for bare @receiver without sender=, got %d: %v", len(rels), rels)
	}
}

func TestPyExtractor_ReceiverDecorator_NilReader(t *testing.T) {
	rels := ApplyReceiverSenderEdges([]string{"a.py"}, nil)
	if len(rels) != 0 {
		t.Fatalf("expected nil reader to return zero edges, got %d", len(rels))
	}
}

// ---------------------------------------------------------------------------
// TestPyExtractor_FilterSetMetaModel_EmitsReferences
// ---------------------------------------------------------------------------

func TestPyExtractor_FilterSetMetaModel_EmitsReferences(t *testing.T) {
	files := map[string]string{
		"core/filters/device_filters.py": `import django_filters
from core.models import Device

class DeviceFilter(django_filters.FilterSet):
    class Meta:
        model = Device
        fields = []
`,
		"core/filters/building_filters.py": `import django_filters
from core.models import Building

class BuildingFilter(django_filters.FilterSet):
    class Meta:
        model = Building
        fields = ["id", "name"]

class BuildingActiveFilter(django_filters.FilterSet):
    class Meta:
        model = Building
        fields = ["is_active"]
`,
		// A non-filter file — must produce zero edges.
		"core/models/device.py": `from django.db import models

class Device(models.Model):
    serial_number = models.CharField(max_length=100)
    class Meta:
        db_table = "devices"
`,
	}

	paths := []string{
		"core/filters/device_filters.py",
		"core/filters/building_filters.py",
		"core/models/device.py",
	}
	reader := fileMapReader2578(files)

	rels := ApplyFilterSetMetaModelEdges(paths, reader)

	refs := relsOfKind(rels, "REFERENCES")
	if len(refs) != 3 {
		t.Fatalf("expected 3 REFERENCES edges, got %d: %v", len(refs), refs)
	}

	wantEdges := map[string]string{
		"Class:DeviceFilter":         "Class:Device",
		"Class:BuildingFilter":       "Class:Building",
		"Class:BuildingActiveFilter": "Class:Building",
	}
	for _, r := range refs {
		want, ok := wantEdges[r.FromID]
		if !ok {
			t.Errorf("unexpected FromID %q in REFERENCES edge %v", r.FromID, r)
			continue
		}
		if r.ToID != want {
			t.Errorf("edge from %q: want ToID %q got %q", r.FromID, want, r.ToID)
		}
		if r.Properties["pattern_type"] != "filterset_meta_model" {
			t.Errorf("edge from %q: want pattern_type filterset_meta_model, got %q",
				r.FromID, r.Properties["pattern_type"])
		}
		if r.Properties["framework"] != "django_filter" {
			t.Errorf("edge from %q: want framework django_filter, got %q",
				r.FromID, r.Properties["framework"])
		}
	}
}

func TestPyExtractor_FilterSetMetaModel_NilReader(t *testing.T) {
	rels := ApplyFilterSetMetaModelEdges([]string{"a.py"}, nil)
	if len(rels) != 0 {
		t.Fatalf("expected nil reader to return zero edges, got %d", len(rels))
	}
}

func TestPyExtractor_FilterSetMetaModel_NonFilterFile(t *testing.T) {
	files := map[string]string{
		"core/models/group.py": `from django.db import models

class Group(models.Model):
    name = models.CharField(max_length=255)
    class Meta:
        db_table = "core_group"
`,
	}
	rels := ApplyFilterSetMetaModelEdges([]string{"core/models/group.py"}, fileMapReader2578(files))
	if len(rels) != 0 {
		t.Fatalf("expected 0 edges for non-filter model file, got %d: %v", len(rels), rels)
	}
}
