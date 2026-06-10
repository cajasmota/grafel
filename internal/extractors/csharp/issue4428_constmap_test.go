package csharp_test

// issue4428_constmap_test.go — value-asserting, REAL-pipeline tests for the C#
// constant-COLLECTION value-sets (#4428, extends #4429 / epic #4419). A C#
// source-of-truth uses a `static readonly Dictionary` const map or a grouped
// set of string consts instead of an enum; both must be emitted as
// name-searchable SCOPE.Enum value-set nodes whose members enumerate the real
// {key,value} pairs as STRUCTURED members_json so a downstream cross-graph
// parity-audit reads the literal set without re-parsing source.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

type csMember struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

func csFindEnum(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Enum" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func csMembers(t *testing.T, e *types.EntityRecord) []csMember {
	t.Helper()
	raw := e.Properties["members_json"]
	if raw == "" {
		t.Fatalf("entity %q has no members_json; props=%v", e.Name, e.Properties)
	}
	var got []csMember
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("members_json not valid JSON: %v (raw=%s)", err, raw)
	}
	return got
}

func csMemberValue(ms []csMember, key string) (string, bool) {
	for _, m := range ms {
		if m.Key == key {
			return m.Value, true
		}
	}
	return "", false
}

// TestIssue4428_RealCSharpConstCollections runs the live-shaped fixture through
// the REAL extract pipeline and asserts BOTH the Dictionary const map and the
// grouped string consts surface as searchable value-sets enumerating their real
// key→value pairs.
func TestIssue4428_RealCSharpConstCollections(t *testing.T) {
	src, err := os.ReadFile("testdata/issue4428/PermissionPages.cs")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	ents := extractCSharpRecords(t, string(src))

	// 1) The `static readonly Dictionary` const map (object-initialiser form).
	labels := csFindEnum(ents, "PageLabels")
	if labels == nil {
		t.Fatal("SCOPE.Enum:PageLabels const-map value-set not found")
	}
	if got := labels.Properties["kind_hint"]; got != "csharp_const_map" {
		t.Fatalf("PageLabels kind_hint = %q, want csharp_const_map", got)
	}
	lm := csMembers(t, labels)
	if v, ok := csMemberValue(lm, "core-admin"); !ok || v != "Core Admin" {
		t.Fatalf("PageLabels[core-admin] = %q (ok=%v), want Core Admin", v, ok)
	}
	if v, ok := csMemberValue(lm, "billing"); !ok || v != "Billing" {
		t.Fatalf("PageLabels[billing] = %q (ok=%v), want Billing", v, ok)
	}
	if len(lm) != 3 {
		t.Fatalf("PageLabels member count = %d, want 3", len(lm))
	}

	// 2) The indexer-init Dictionary const map (`["k"] = "v"` form).
	routes := csFindEnum(ents, "PageRoutes")
	if routes == nil {
		t.Fatal("SCOPE.Enum:PageRoutes const-map value-set not found")
	}
	if got := routes.Properties["kind_hint"]; got != "csharp_const_map" {
		t.Fatalf("PageRoutes kind_hint = %q, want csharp_const_map", got)
	}
	rm := csMembers(t, routes)
	if v, ok := csMemberValue(rm, "core-admin"); !ok || v != "/admin" {
		t.Fatalf("PageRoutes[core-admin] = %q (ok=%v), want /admin", v, ok)
	}

	// 3) The grouped string consts → one value-set named after the class.
	group := csFindEnum(ents, "PermissionPages")
	if group == nil {
		t.Fatal("SCOPE.Enum:PermissionPages const-group value-set not found")
	}
	if got := group.Properties["kind_hint"]; got != "csharp_const_group" {
		t.Fatalf("PermissionPages kind_hint = %q, want csharp_const_group", got)
	}
	gm := csMembers(t, group)
	if v, ok := csMemberValue(gm, "CoreAdmin"); !ok || v != "core-admin" {
		t.Fatalf("PermissionPages.CoreAdmin = %q (ok=%v), want core-admin", v, ok)
	}
	if v, ok := csMemberValue(gm, "Billing"); !ok || v != "billing" {
		t.Fatalf("PermissionPages.Billing = %q (ok=%v), want billing", v, ok)
	}
	if len(gm) != 3 {
		t.Fatalf("PermissionPages group member count = %d, want 3", len(gm))
	}
}

// TestIssue4428_CSharpEnumCarriesMembersJSON guards that a backed C# enum also
// carries the structured members_json the shared helper emits (the #4429
// enrichment), so all three C# value-set shapes are diff-ready.
func TestIssue4428_CSharpEnumCarriesMembersJSON(t *testing.T) {
	src := `
namespace App
{
    public enum Status { Active = 1, Inactive = 2 }
}
`
	ents := extractCSharpRecords(t, src)
	en := csFindEnum(ents, "Status")
	if en == nil {
		t.Fatal("SCOPE.Enum:Status not found")
	}
	ms := csMembers(t, en)
	if v, ok := csMemberValue(ms, "Active"); !ok || v != "1" {
		t.Fatalf("Status.Active = %q (ok=%v), want 1", v, ok)
	}
}

// TestIssue4428_CSharpNonLiteralMapSkipped asserts honest-partial: a Dictionary
// whose values are non-literal (a call / identifier) is NOT a closed value-set
// and emits no node.
func TestIssue4428_CSharpNonLiteralMapSkipped(t *testing.T) {
	src := `
namespace App
{
    public static class Cfg
    {
        public static readonly Dictionary<string, string> M = new()
        {
            { "a", Compute() },
        };
    }
}
`
	ents := extractCSharpRecords(t, src)
	if en := csFindEnum(ents, "M"); en != nil {
		t.Fatalf("non-literal-valued map M should emit no value-set, got %+v", en.Properties)
	}
}

// TestIssue4428_CSharpSingleConstNoGroup asserts a class with a single string
// const does not form a value-set group (a lone constant is not an enumeration).
func TestIssue4428_CSharpSingleConstNoGroup(t *testing.T) {
	src := `
namespace App
{
    public static class One { public const string Only = "x"; }
}
`
	ents := extractCSharpRecords(t, src)
	if en := csFindEnum(ents, "One"); en != nil {
		t.Fatalf("single-const class should emit no group value-set, got %+v", en.Properties)
	}
}
