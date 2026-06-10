package golang_test

// issue4426_constantset_test.go — in-pipeline tests for Go constant-COLLECTION
// value-sets (#4426, extends #4420/#4429).
//
// These run the REAL Go extract pipeline (extractor.Get("go").Extract via
// extractFromPath) on a byte-copy of a representative fixture — not a synthetic
// AST — so a fixture that drifts cannot lie at merge time. They assert that the
// three additional Go constant-collection shapes become name-searchable
// SCOPE.Enum value-sets whose members_json enumerates the real key→value pairs.

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

func membersJSON(t *testing.T, e *types.EntityRecord) []constMemberEntry {
	t.Helper()
	raw := e.Properties["members_json"]
	if raw == "" {
		t.Fatalf("entity %q has no members_json; props=%v", e.Name, e.Properties)
	}
	var got []constMemberEntry
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("members_json not valid JSON: %v (raw=%s)", err, raw)
	}
	return got
}

func memberVal(members []constMemberEntry, key string) (string, bool) {
	for _, m := range members {
		if m.Key == key {
			return m.Value, true
		}
	}
	return "", false
}

// findSearchableEnum simulates the search_entities name lookup: an entity is
// "searchable by name" when a SCOPE.Enum record carries that exact Name.
func findSearchableEnum(recs []types.EntityRecord, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

func extract4426(t *testing.T) []types.EntityRecord {
	t.Helper()
	src, err := os.ReadFile("testdata/issue4426/constants.go")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out, err := extractFromPath(string(src), "auth/constants.go")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	recs := make([]types.EntityRecord, 0, len(out))
	for _, r := range out {
		recs = append(recs, r.(types.EntityRecord))
	}
	return recs
}

// TestIssue4426_ConstMapValueSet — `var PermissionPages = map[string]string{...}`
// becomes a name-searchable value-set enumerating the real {key,value} pairs,
// including the hyphenated `core-admin` literal captured as structured data.
func TestIssue4426_ConstMapValueSet(t *testing.T) {
	recs := extract4426(t)

	en := findSearchableEnum(recs, "PermissionPages")
	if en == nil {
		t.Fatal("SCOPE.Enum:PermissionPages value-set not found (RED before fix)")
	}
	if got := en.Properties["kind_hint"]; got != "go_const_map" {
		t.Fatalf("kind_hint = %q, want go_const_map", got)
	}
	members := membersJSON(t, en)
	want := map[string]string{
		"CoreAdmin":         "core-admin",
		"ContractProposals": "contract-proposal",
		"Users":             "users",
		"Sync":              "sync",
	}
	for k, v := range want {
		got, ok := memberVal(members, k)
		if !ok {
			t.Fatalf("member %q missing from members_json", k)
		}
		if got != v {
			t.Fatalf("member %q value = %q, want %q", k, got, v)
		}
	}
	// Every member must carry a real (non-zero) source line so a diff tool can
	// locate the key→value pair.
	for _, m := range members {
		if m.Line <= 0 {
			t.Fatalf("member %q has no source line (line=%d)", m.Key, m.Line)
		}
	}
}

// TestIssue4426_UntypedPrefixedConstGroup — an idiomatic untyped const block
// whose members share a `Role` prefix becomes a value-set named `Role`.
func TestIssue4426_UntypedPrefixedConstGroup(t *testing.T) {
	recs := extract4426(t)

	en := findSearchableEnum(recs, "Role")
	if en == nil {
		t.Fatal("SCOPE.Enum:Role value-set not found for untyped prefixed const group")
	}
	if got := en.Properties["kind_hint"]; got != "go_const_group" {
		t.Fatalf("kind_hint = %q, want go_const_group", got)
	}
	members := membersJSON(t, en)
	want := map[string]string{
		"RoleAdmin":  "admin",
		"RoleMember": "member",
		"RoleGuest":  "guest",
	}
	for k, v := range want {
		got, ok := memberVal(members, k)
		if !ok {
			t.Fatalf("member %q missing from members_json", k)
		}
		if got != v {
			t.Fatalf("member %q value = %q, want %q", k, got, v)
		}
	}
}

// TestIssue4426_TypedSameFileGroupNotDoubleEmitted — the same-file named-type
// group (Status) is owned by the existing go_iota path, and the #4426 path must
// NOT also emit a node for it (no double-emit). Exactly one Status value-set.
func TestIssue4426_TypedSameFileGroupNotDoubleEmitted(t *testing.T) {
	recs := extract4426(t)

	count := 0
	var st *types.EntityRecord
	for i := range recs {
		if recs[i].Kind == "SCOPE.Enum" && recs[i].Name == "Status" {
			count++
			st = &recs[i]
		}
	}
	if count != 1 {
		t.Fatalf("Status value-set emitted %d times, want exactly 1 (no double-emit)", count)
	}
	if got := st.Properties["kind_hint"]; got != "go_iota" {
		t.Fatalf("Status kind_hint = %q, want go_iota (owned by enum_valueset.go)", got)
	}
	// The typed group's members now also carry a source line (#4426 enrichment).
	members := membersJSON(t, st)
	if v, ok := memberVal(members, "StatusActive"); !ok || v != "active" {
		t.Fatalf("StatusActive value = %q (ok=%v), want active", v, ok)
	}
	for _, m := range members {
		if m.Line <= 0 {
			t.Fatalf("typed-group member %q has no source line", m.Key)
		}
	}
}

// TestIssue4426_IncidentalConstGroupNotEmitted — an untyped const group with no
// shared identifier prefix (maxRetries / timeoutSec) is an incidental grouping,
// NOT a value-set: no SCOPE.Enum node (precision-first).
func TestIssue4426_IncidentalConstGroupNotEmitted(t *testing.T) {
	recs := extract4426(t)
	for i := range recs {
		if recs[i].Kind != "SCOPE.Enum" {
			continue
		}
		mj := recs[i].Properties["members_json"]
		// The incidental group's members would appear under some derived name;
		// assert neither member key leaks into any emitted value-set.
		for _, bad := range []string{"maxRetries", "timeoutSec"} {
			if containsKey(mj, bad) {
				t.Fatalf("incidental const %q must not appear in a value-set (entity %q)", bad, recs[i].Name)
			}
		}
	}
}

func containsKey(membersJSON, key string) bool {
	var got []constMemberEntry
	if json.Unmarshal([]byte(membersJSON), &got) != nil {
		return false
	}
	for _, m := range got {
		if m.Key == key {
			return true
		}
	}
	return false
}
