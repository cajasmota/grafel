package ruby_test

// issue4427_constvalueset_test.go — value-asserting, IN-PIPELINE tests for Ruby
// CONSTANT-COLLECTION value-sets (#4427, extends #4429 / epic #4419, ref #4334).
//
// #4429 indexed Rails `enum` declarations as SCOPE.Enum value-sets. #4427
// generalises that to the plain Ruby constant-collection shapes that act as
// source-of-truth maps but were invisible to search_entities and could not be
// diffed by a downstream cross-graph parity-audit:
//
//   - frozen / plain constant HASH  (symbol or string keys)
//   - constant ARRAY of literals    (%w / %i / [...] / .freeze)
//   - module-level constant GROUP   (module Roles; ADMIN='admin'; end)
//   - Rails ActiveRecord enum       (already covered by #4429; re-asserted here)
//
// The first test runs the REAL Ruby extract pipeline on a byte-copy fixture
// (testdata/issue4427/permission_pages.rb) — a frozen constant hash plus a
// Rails enum — asserting the value-set is emitted, name-searchable, and that
// members_json enumerates the real {key,value} pairs. RED before the fix
// (only the Rails enum surfaced); GREEN after.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

type constMemberEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

func constMembersJSON(t *testing.T, e *types.EntityRecord) []constMemberEntry {
	t.Helper()
	raw := e.Properties["members_json"]
	if raw == "" {
		t.Fatalf("entity %q has no members_json property; props=%v", e.Name, e.Properties)
	}
	var got []constMemberEntry
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("members_json not valid JSON: %v (raw=%s)", err, raw)
	}
	return got
}

func constMemberValue(members []constMemberEntry, key string) (string, bool) {
	for _, m := range members {
		if m.Key == key {
			return m.Value, true
		}
	}
	return "", false
}

// searchableEnum mirrors how search_entities locates a value-set: by Kind +
// name. nil means the node would not be findable.
func searchableEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

// TestIssue4427_RealFrozenConstHash_InPipeline runs the live-shaped fixture
// through the REAL Ruby extractor and asserts the frozen PERMISSION_PAGES hash
// surfaces as a name-searchable value-set whose members_json carries the real
// hyphenated string values with source lines.
func TestIssue4427_RealFrozenConstHash_InPipeline(t *testing.T) {
	src, err := os.ReadFile("testdata/issue4427/permission_pages.rb")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	recs := extractRubyForEnum(t, "app/permission_pages.rb", string(src))

	en := searchableEnum(recs, "PERMISSION_PAGES")
	if en == nil {
		t.Fatal("SCOPE.Enum:PERMISSION_PAGES value-set node not found (RED before #4427)")
	}
	if en.Kind != "SCOPE.Enum" {
		t.Fatalf("Kind = %q, want SCOPE.Enum", en.Kind)
	}
	if got := en.Properties["kind_hint"]; got != "ruby_const_hash" {
		t.Fatalf("kind_hint = %q, want ruby_const_hash", got)
	}

	members := constMembersJSON(t, en)
	want := map[string]string{
		"core_admin":         "core-admin",
		"contract_proposals": "contract-proposal",
		"users":              "users",
		"sync":               "sync",
	}
	for k, v := range want {
		got, ok := constMemberValue(members, k)
		if !ok {
			t.Fatalf("member %q missing from members_json (got %+v)", k, members)
		}
		if got != v {
			t.Fatalf("member %q value = %q, want %q", k, got, v)
		}
	}
	// Each member must carry a real source line so a diff tool can locate it.
	for _, m := range members {
		if m.Line <= 0 {
			t.Fatalf("member %q line = %d, want > 0", m.Key, m.Line)
		}
	}
}

// TestIssue4427_RailsEnumStillSurfaces is the cross-check that the same fixture's
// Rails `enum status: {...}` (the #4429 shape) still surfaces alongside the new
// const-collection nodes — generalisation did not regress the original.
func TestIssue4427_RailsEnumStillSurfaces(t *testing.T) {
	src, err := os.ReadFile("testdata/issue4427/permission_pages.rb")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	recs := extractRubyForEnum(t, "app/permission_pages.rb", string(src))

	en := searchableEnum(recs, "status")
	if en == nil {
		t.Fatal("Rails enum SCOPE.Enum:status not found")
	}
	if got := en.Properties["values"]; got != "active=0, archived=1" {
		t.Fatalf("status values = %q, want active=0, archived=1", got)
	}
	// And the class-body frozen hash PRIORITY_LABELS also surfaces.
	pl := searchableEnum(recs, "PRIORITY_LABELS")
	if pl == nil {
		t.Fatal("class-body frozen hash SCOPE.Enum:PRIORITY_LABELS not found")
	}
	if v, _ := func() (string, bool) {
		return constMemberValue(constMembersJSON(t, pl), "low")
	}(); v != "Low" {
		t.Fatalf("PRIORITY_LABELS[low] = %q, want Low", v)
	}
}

// TestIssue4427_ConstArrayLiterals covers `%w[..]` / `%i[..]` / `[..]` arrays.
func TestIssue4427_ConstArrayLiterals(t *testing.T) {
	recs := extractRubyForEnum(t, "app/statuses.rb",
		`STATUSES = %w[active inactive archived].freeze`)
	en := searchableEnum(recs, "STATUSES")
	if en == nil {
		t.Fatal("SCOPE.Enum:STATUSES value-set (%w array) not found")
	}
	if got := en.Properties["kind_hint"]; got != "ruby_const_array" {
		t.Fatalf("kind_hint = %q, want ruby_const_array", got)
	}
	if got := en.Properties["members"]; got != "active, inactive, archived" {
		t.Fatalf("members = %q, want active, inactive, archived", got)
	}
	members := constMembersJSON(t, en)
	if v, _ := constMemberValue(members, "active"); v != "active" {
		t.Fatalf("array literal active value = %q, want active", v)
	}
}

func TestIssue4427_SymbolArray(t *testing.T) {
	recs := extractRubyForEnum(t, "app/ids.rb", `IDS = %i[admin user guest].freeze`)
	en := searchableEnum(recs, "IDS")
	if en == nil {
		t.Fatal("SCOPE.Enum:IDS value-set (%i array) not found")
	}
	if got := en.Properties["members"]; got != "admin, user, guest" {
		t.Fatalf("members = %q, want admin, user, guest", got)
	}
}

// TestIssue4427_ModuleConstGroup covers `module Roles; ADMIN='admin'; end`.
func TestIssue4427_ModuleConstGroup(t *testing.T) {
	recs := extractRubyForEnum(t, "app/roles.rb",
		`module Roles
  ADMIN   = 'admin'
  MANAGER = 'manager'
  MEMBER  = 'member'
end`)
	en := searchableEnum(recs, "Roles")
	if en == nil {
		t.Fatal("SCOPE.Enum:Roles module-constant-group value-set not found")
	}
	if got := en.Properties["kind_hint"]; got != "ruby_const_module" {
		t.Fatalf("kind_hint = %q, want ruby_const_module", got)
	}
	members := constMembersJSON(t, en)
	want := map[string]string{"ADMIN": "admin", "MANAGER": "manager", "MEMBER": "member"}
	for k, v := range want {
		got, ok := constMemberValue(members, k)
		if !ok {
			t.Fatalf("module const %q missing", k)
		}
		if got != v {
			t.Fatalf("module const %q = %q, want %q", k, got, v)
		}
	}
}

// TestIssue4427_StringRocketKeys covers `{ 'free' => 10, 'pro' => 100 }`.
func TestIssue4427_StringRocketKeys(t *testing.T) {
	recs := extractRubyForEnum(t, "app/limits.rb",
		`LIMITS = { 'free' => 10, 'pro' => 100 }.freeze`)
	en := searchableEnum(recs, "LIMITS")
	if en == nil {
		t.Fatal("SCOPE.Enum:LIMITS value-set (rocket keys) not found")
	}
	members := constMembersJSON(t, en)
	if v, _ := constMemberValue(members, "free"); v != "10" {
		t.Fatalf("LIMITS[free] = %q, want 10", v)
	}
	if v, _ := constMemberValue(members, "pro"); v != "100" {
		t.Fatalf("LIMITS[pro] = %q, want 100", v)
	}
}

// TestIssue4427_NonLiteralValueAsExpressionText is the #4427 honest-partial
// contract: a non-literal value records its EXPRESSION TEXT (not dropped), so
// the key set stays complete and a parity-audit sees the value is dynamic.
func TestIssue4427_NonLiteralValueAsExpressionText(t *testing.T) {
	recs := extractRubyForEnum(t, "app/mixed.rb",
		`MIXED = { a: 1, b: compute_default() }.freeze`)
	en := searchableEnum(recs, "MIXED")
	if en == nil {
		t.Fatal("SCOPE.Enum:MIXED value-set not found")
	}
	members := constMembersJSON(t, en)
	if v, _ := constMemberValue(members, "a"); v != "1" {
		t.Fatalf("MIXED[a] = %q, want 1", v)
	}
	v, ok := constMemberValue(members, "b")
	if !ok {
		t.Fatal("non-literal member b dropped; #4427 requires expression-text capture")
	}
	if v != "compute_default()" {
		t.Fatalf("MIXED[b] = %q, want expression text compute_default()", v)
	}
}

// TestIssue4427_NonCollectionConst_NoNode is the negative: a constant bound to
// a single scalar at file scope is NOT a collection and emits no value-set.
func TestIssue4427_NonCollectionConst_NoNode(t *testing.T) {
	recs := extractRubyForEnum(t, "app/scalar.rb", `MAX_RETRIES = 5`)
	if en := searchableEnum(recs, "MAX_RETRIES"); en != nil {
		t.Fatalf("scalar constant should not emit a value-set, got %+v", en.Properties)
	}
}
