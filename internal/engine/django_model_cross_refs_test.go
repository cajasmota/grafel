// Tests for the #2578 Serializer/Signal/FilterSet → Model cross-ref passes.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
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
			// #2589: FromID must be "SCOPE.Operation:" (not "Function:") so the
			// resolver rewrites it to the handler's hex entity ID.
			if r.FromID != "SCOPE.Operation:replicate_signal" {
				t.Errorf("GroupDeviceSettings listener: want FromID SCOPE.Operation:replicate_signal, got %q", r.FromID)
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
		if r.ToID == "Class:Contract" && r.FromID == "SCOPE.Operation:on_contract_deleted" {
			foundContract = true
		}
	}
	if !foundContract {
		t.Error("expected HANDLES_SIGNAL from SCOPE.Operation:on_contract_deleted to Class:Contract, not found")
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

// ---------------------------------------------------------------------------
// #2592 — string-literal and apps.get_model sender forms
// ---------------------------------------------------------------------------

func TestReceiverDecorator_StringSender_Matches(t *testing.T) {
	// sender='core.Building' — full dotted string form.
	files := map[string]string{
		"core/signals/replicate_to_datalake.py": `from django.db.models.signals import post_save
from django.dispatch import receiver

@receiver(post_save, sender='core.Building')
def replicate_building(sender, instance, created, **kwargs):
    pass
`,
	}
	paths := []string{"core/signals/replicate_to_datalake.py"}
	rels := ApplyReceiverSenderEdges(paths, fileMapReader2578(files))

	handlesSignal := relsOfKind(rels, "HANDLES_SIGNAL")
	if len(handlesSignal) != 1 {
		t.Fatalf("expected 1 HANDLES_SIGNAL edge, got %d: %v", len(handlesSignal), handlesSignal)
	}
	r := handlesSignal[0]
	if r.FromID != "SCOPE.Operation:replicate_building" {
		t.Errorf("FromID: want SCOPE.Operation:replicate_building, got %q", r.FromID)
	}
	if r.ToID != "Class:Building" {
		t.Errorf("ToID: want Class:Building, got %q", r.ToID)
	}
	if r.Properties["pattern_type"] != "receiver_sender_model" {
		t.Errorf("pattern_type: want receiver_sender_model, got %q", r.Properties["pattern_type"])
	}
}

func TestReceiverDecorator_BareStringSender_Matches(t *testing.T) {
	// sender='Building' — bare string (no app label).
	files := map[string]string{
		"core/signals/building_signals.py": `from django.db.models.signals import post_delete
from django.dispatch import receiver

@receiver(post_delete, sender='Building')
def on_building_deleted(sender, instance, **kwargs):
    pass
`,
	}
	paths := []string{"core/signals/building_signals.py"}
	rels := ApplyReceiverSenderEdges(paths, fileMapReader2578(files))

	handlesSignal := relsOfKind(rels, "HANDLES_SIGNAL")
	if len(handlesSignal) != 1 {
		t.Fatalf("expected 1 HANDLES_SIGNAL edge, got %d: %v", len(handlesSignal), handlesSignal)
	}
	r := handlesSignal[0]
	if r.FromID != "SCOPE.Operation:on_building_deleted" {
		t.Errorf("FromID: want SCOPE.Operation:on_building_deleted, got %q", r.FromID)
	}
	if r.ToID != "Class:Building" {
		t.Errorf("ToID: want Class:Building, got %q", r.ToID)
	}
}

func TestReceiverDecorator_AppsGetModel_Matches(t *testing.T) {
	// sender=apps.get_model('core', 'Building') — runtime lookup pattern.
	files := map[string]string{
		"core/signals/dynamic_signals.py": `from django.db.models.signals import post_save
from django.dispatch import receiver
from django.apps import apps

@receiver(post_save, sender=apps.get_model('core', 'Building'))
def on_building_saved(sender, instance, created, **kwargs):
    pass
`,
	}
	paths := []string{"core/signals/dynamic_signals.py"}
	rels := ApplyReceiverSenderEdges(paths, fileMapReader2578(files))

	handlesSignal := relsOfKind(rels, "HANDLES_SIGNAL")
	if len(handlesSignal) != 1 {
		t.Fatalf("expected 1 HANDLES_SIGNAL edge, got %d: %v", len(handlesSignal), handlesSignal)
	}
	r := handlesSignal[0]
	if r.FromID != "SCOPE.Operation:on_building_saved" {
		t.Errorf("FromID: want SCOPE.Operation:on_building_saved, got %q", r.FromID)
	}
	if r.ToID != "Class:Building" {
		t.Errorf("ToID: want Class:Building, got %q", r.ToID)
	}
	if r.Properties["via"] != "@receiver(sender=apps.get_model())" {
		t.Errorf("via: want @receiver(sender=apps.get_model()), got %q", r.Properties["via"])
	}
}

// ---------------------------------------------------------------------------
// #2589 — integration test: exact upvate_core replicate_to_datalake.py pattern
// ---------------------------------------------------------------------------

// TestReceiverDecorator_UpvateRealPattern exercises the EXACT content of
// upvate_core/core/signals/replicate_to_datalake.py — 11 stacked @receiver
// decorators all pointing at the same def replicate_signal.
//
// This test was added because unit tests for #2584 passed on simple 4-sender
// fixtures but real upvate data produced zero HANDLES_SIGNAL edges: the
// FromID was emitted as "Function:replicate_signal" (DispositionDynamic in
// the resolver — not rewritten to hex), while the actual entity lives under
// "SCOPE.Operation:replicate_signal". The fix changes all FromIDs to
// "SCOPE.Operation:..." which the resolver correctly rewrites to hex IDs.
// Closes #2589.
func TestReceiverDecorator_UpvateRealPattern(t *testing.T) {
	// Verbatim copy of the real upvate file (imports trimmed to one line for brevity).
	files := map[string]string{
		"core/signals/replicate_to_datalake.py": `import logging
from django.dispatch import receiver
from django.db.models.signals import post_save
from core.models.device import DeviceEquipmentType
from core.models import Client, Device, Building, Group, Jurisdiction, Contract, ContractDevice, BuildingAlternateAddress, GroupDeviceSettings, GroupBuildingSettings
from core.tasks.replicate_to_datalake import replicate_instance_data
from core.helper.replicate_to_datalake_helper import COLLECTIONS_MAPPING


logger = logging.getLogger(__name__)


@receiver(post_save, sender=Group)
@receiver(post_save, sender=Client)
@receiver(post_save, sender=Device)
@receiver(post_save, sender=Building)
@receiver(post_save, sender=Jurisdiction)
@receiver(post_save, sender=DeviceEquipmentType)
@receiver(post_save, sender=Contract)
@receiver(post_save, sender=ContractDevice)
@receiver(post_save, sender=BuildingAlternateAddress)
@receiver(post_save, sender=GroupDeviceSettings)
@receiver(post_save, sender=GroupBuildingSettings)
def replicate_signal(sender, instance, created, **kwargs):
    instance_name = sender.__name__.lower()
    try:
        model_path = f"core.{instance_name}"
        collection_name = COLLECTIONS_MAPPING.get(instance_name, None)
        instance_id = instance.id
        if not collection_name:
            logger.error(f"No collection name found for {instance_name}")
            return
        replicate_instance_data.delay(model_path=model_path, instance_id=instance_id, collection_name=collection_name)
    except Exception as e:
        logger.error(f"Error replicating {instance_name} to MongoDB: {e}")
`,
	}
	paths := []string{"core/signals/replicate_to_datalake.py"}
	rels := ApplyReceiverSenderEdges(paths, fileMapReader2578(files))

	handlesSignal := relsOfKind(rels, "HANDLES_SIGNAL")

	wantSenders := []string{
		"Group", "Client", "Device", "Building", "Jurisdiction",
		"DeviceEquipmentType", "Contract", "ContractDevice",
		"BuildingAlternateAddress", "GroupDeviceSettings", "GroupBuildingSettings",
	}
	if len(handlesSignal) != len(wantSenders) {
		t.Fatalf("expected %d HANDLES_SIGNAL edges (one per stacked @receiver), got %d: %v",
			len(wantSenders), len(handlesSignal), handlesSignal)
	}

	// Build a lookup: ToID → edge for easy assertion.
	byTarget := make(map[string]types.RelationshipRecord, len(handlesSignal))
	for _, r := range handlesSignal {
		byTarget[r.ToID] = r
	}

	for _, sender := range wantSenders {
		wantToID := "Class:" + sender
		r, ok := byTarget[wantToID]
		if !ok {
			t.Errorf("missing HANDLES_SIGNAL edge to %q", wantToID)
			continue
		}
		// #2589: FromID MUST be SCOPE.Operation so the resolver can rewrite it to
		// the handler's hex entity ID. "Function:" prefix stays as an unresolved
		// stub and makes the edge invisible to graph queries.
		wantFromID := "SCOPE.Operation:replicate_signal"
		if r.FromID != wantFromID {
			t.Errorf("edge to %q: want FromID %q, got %q", wantToID, wantFromID, r.FromID)
		}
		if r.Properties["framework"] != "django_signals" {
			t.Errorf("edge to %q: want framework django_signals, got %q", wantToID, r.Properties["framework"])
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
