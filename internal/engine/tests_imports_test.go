// Tests for ApplyTestsViaImports — #2812.
//
// Verifies that the import-driven TESTS pass links pytest/Django test functions
// to the production entities imported into the test module: direct-import
// helpers, package re-exported models, alias imports, and TestCase fixture
// methods. Also verifies that unimported / unreferenced symbols never produce
// spurious edges.
package engine

import (
	"sort"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// prodRecords is a minimal set of entity records standing in for the production
// graph: two helper functions in one module and one model re-exported through a
// package __init__.
func prodRecords() []types.EntityRecord {
	return []types.EntityRecord{
		{
			Name:          "parse_csv_file",
			QualifiedName: "core.helper.schedule_import_helper.parse_csv_file",
			Kind:          "SCOPE.Operation",
			SourceFile:    "core/helper/schedule_import_helper.py",
		},
		{
			Name:          "resolve_device",
			QualifiedName: "core.helper.schedule_import_helper.resolve_device",
			Kind:          "SCOPE.Operation",
			SourceFile:    "core/helper/schedule_import_helper.py",
		},
		{
			// Re-exported from core.models (defined in core/models/device.py).
			Name:          "Device",
			QualifiedName: "core.models.device.Device",
			Kind:          "Model",
			SourceFile:    "core/models/device.py",
		},
	}
}

const importTestSrc = `import io
from core.helper.schedule_import_helper import parse_csv_file, resolve_device
from core.models import Device
from core.helper.schedule_import_helper import resolve_device as rd

class ResolveDeviceTest(TestCase):
    def setUp(self):
        self.device = Device.objects.create(name="ELV-100")

    def test_parse(self):
        rows, errors = parse_csv_file(b"")
        self.assertEqual(rows, [])

    def test_resolve(self):
        device, errors = resolve_device("ELV-100", 1)
        self.assertIsNotNone(device)

    def test_alias(self):
        device, errors = rd("ELV-100", 1)
        self.assertIsNotNone(device)

    def test_unrelated(self):
        # references no imported production symbol
        self.assertTrue(True)
`

func importEdgeKey(r types.RelationshipRecord) string {
	return r.Properties["test_function"] + "->" + r.Properties["tested"]
}

func TestApplyTestsViaImports_LinksImportedSymbols(t *testing.T) {
	paths := []string{"core/tests/test_schedule_import.py", "core/helper/schedule_import_helper.py"}
	reader := func(p string) []byte {
		if p == "core/tests/test_schedule_import.py" {
			return []byte(importTestSrc)
		}
		return nil
	}

	edges := ApplyTestsViaImports(paths, reader, prodRecords())

	got := map[string]types.RelationshipRecord{}
	for _, e := range edges {
		if e.Kind != "TESTS" {
			t.Fatalf("expected TESTS edge, got %q", e.Kind)
		}
		got[importEdgeKey(e)] = e
	}

	want := []struct {
		fn    string
		qname string
	}{
		{"setUp", "core.models.device.Device"},
		{"test_parse", "core.helper.schedule_import_helper.parse_csv_file"},
		{"test_resolve", "core.helper.schedule_import_helper.resolve_device"},
		// alias `rd` resolves to resolve_device.
		{"test_alias", "core.helper.schedule_import_helper.resolve_device"},
	}
	for _, w := range want {
		key := w.fn + "->" + w.qname
		e, ok := got[key]
		if !ok {
			t.Errorf("missing TESTS edge %s", key)
			continue
		}
		if e.FromID != "scope:operation:core/tests/test_schedule_import.py#"+w.fn {
			t.Errorf("edge %s: unexpected FromID %q", key, e.FromID)
		}
		if e.ToID != "scope:operation:?#"+w.qname {
			t.Errorf("edge %s: unexpected ToID %q", key, e.ToID)
		}
		if e.Properties["via"] != "import" {
			t.Errorf("edge %s: expected via=import, got %q", key, e.Properties["via"])
		}
	}

	// test_unrelated references no imported symbol → no edge.
	for k := range got {
		if k[:len("test_unrelated")] == "test_unrelated" {
			t.Errorf("unexpected edge from test_unrelated: %s", k)
		}
	}
}

func TestApplyTestsViaImports_NoSpuriousEdges(t *testing.T) {
	// A test file importing a symbol that does NOT exist in the graph must
	// produce no edges.
	src := `from core.helper.unknown import does_not_exist

def test_x():
    does_not_exist()
`
	paths := []string{"app/tests/test_x.py"}
	reader := func(p string) []byte { return []byte(src) }
	edges := ApplyTestsViaImports(paths, reader, prodRecords())
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges for unresolvable import, got %d", len(edges))
	}
}

func TestApplyTestsViaImports_FlattensParenImports(t *testing.T) {
	src := `from core.helper.schedule_import_helper import (
    parse_csv_file,
    resolve_device,
)

def test_multi():
    parse_csv_file(b"")
    resolve_device("x", 1)
`
	paths := []string{"tests/test_multi.py"}
	reader := func(p string) []byte { return []byte(src) }
	edges := ApplyTestsViaImports(paths, reader, prodRecords())

	var qns []string
	for _, e := range edges {
		qns = append(qns, e.Properties["tested"])
	}
	sort.Strings(qns)
	want := []string{
		"core.helper.schedule_import_helper.parse_csv_file",
		"core.helper.schedule_import_helper.resolve_device",
	}
	if len(qns) != len(want) {
		t.Fatalf("expected %d edges, got %d (%v)", len(want), len(qns), qns)
	}
	for i := range want {
		if qns[i] != want[i] {
			t.Errorf("edge %d: want %q, got %q", i, want[i], qns[i])
		}
	}
}

func TestApplyTestsViaImports_EmptyInputs(t *testing.T) {
	if got := ApplyTestsViaImports(nil, nil, prodRecords()); got != nil {
		t.Errorf("nil reader must return nil, got %v", got)
	}
	reader := func(string) []byte { return nil }
	if got := ApplyTestsViaImports([]string{"a.py"}, reader, nil); got != nil {
		t.Errorf("nil records must return nil, got %v", got)
	}
}
