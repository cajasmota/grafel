package kotlin_test

// issue4428_constmap_test.go — value-asserting, REAL-pipeline tests for the
// Kotlin enum + constant-COLLECTION value-sets (#4428, extends #4429 / epic
// #4419). Kotlin had no value-set extraction before this; an `object` const-val
// group, a top-level `mapOf` const map, and an `enum class` with constructor
// values must all surface as name-searchable SCOPE.Enum nodes whose members
// enumerate the real {key,value} pairs as STRUCTURED members_json.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

type ktMember struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

func extractKotlinRecords(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatal("kotlin extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "kotlin",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func ktFindEnum(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Enum" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func ktMembers(t *testing.T, e *types.EntityRecord) []ktMember {
	t.Helper()
	raw := e.Properties["members_json"]
	if raw == "" {
		t.Fatalf("entity %q has no members_json; props=%v", e.Name, e.Properties)
	}
	var got []ktMember
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("members_json not valid JSON: %v (raw=%s)", err, raw)
	}
	return got
}

func ktMemberValue(ms []ktMember, key string) (string, bool) {
	for _, m := range ms {
		if m.Key == key {
			return m.Value, true
		}
	}
	return "", false
}

// TestIssue4428_RealKotlinConstCollections runs the live-shaped fixture through
// the REAL extract pipeline and asserts the object const-val group, the
// top-level mapOf const map, AND the enum class all surface as searchable
// value-sets enumerating their real key→value pairs.
func TestIssue4428_RealKotlinConstCollections(t *testing.T) {
	src, err := os.ReadFile("testdata/issue4428/PermissionPages.kt")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	ents := extractKotlinRecords(t, string(src), "PermissionPages.kt")

	// 1) `object Pages { const val ... }` → group named after the object.
	group := ktFindEnum(ents, "PermissionPages")
	if group == nil {
		t.Fatal("SCOPE.Enum:PermissionPages const-group value-set not found")
	}
	if got := group.Properties["kind_hint"]; got != "kotlin_const_group" {
		t.Fatalf("PermissionPages kind_hint = %q, want kotlin_const_group", got)
	}
	gm := ktMembers(t, group)
	if v, ok := ktMemberValue(gm, "CORE_ADMIN"); !ok || v != "core-admin" {
		t.Fatalf("PermissionPages.CORE_ADMIN = %q (ok=%v), want core-admin", v, ok)
	}
	if v, ok := ktMemberValue(gm, "BILLING"); !ok || v != "billing" {
		t.Fatalf("PermissionPages.BILLING = %q (ok=%v), want billing", v, ok)
	}
	if len(gm) != 3 {
		t.Fatalf("PermissionPages group member count = %d, want 3", len(gm))
	}

	// 2) top-level `val X = mapOf("a" to "b", ...)`.
	labels := ktFindEnum(ents, "PAGE_LABELS")
	if labels == nil {
		t.Fatal("SCOPE.Enum:PAGE_LABELS const-map value-set not found")
	}
	if got := labels.Properties["kind_hint"]; got != "kotlin_const_map" {
		t.Fatalf("PAGE_LABELS kind_hint = %q, want kotlin_const_map", got)
	}
	lm := ktMembers(t, labels)
	if v, ok := ktMemberValue(lm, "core-admin"); !ok || v != "Core Admin" {
		t.Fatalf("PAGE_LABELS[core-admin] = %q (ok=%v), want Core Admin", v, ok)
	}
	if len(lm) != 3 {
		t.Fatalf("PAGE_LABELS member count = %d, want 3", len(lm))
	}

	// 3) `enum class PageGroup(val slug: String) { ADMIN("core-admin"), ... }`.
	en := ktFindEnum(ents, "PageGroup")
	if en == nil {
		t.Fatal("SCOPE.Enum:PageGroup enum value-set not found")
	}
	if got := en.Properties["kind_hint"]; got != "kotlin_enum" {
		t.Fatalf("PageGroup kind_hint = %q, want kotlin_enum", got)
	}
	em := ktMembers(t, en)
	if v, ok := ktMemberValue(em, "ADMIN"); !ok || v != "core-admin" {
		t.Fatalf("PageGroup.ADMIN = %q (ok=%v), want core-admin", v, ok)
	}
	if v, ok := ktMemberValue(em, "FINANCE"); !ok || v != "billing" {
		t.Fatalf("PageGroup.FINANCE = %q (ok=%v), want billing", v, ok)
	}
}

// TestIssue4428_KotlinNonLiteralMapSkipped asserts honest-partial: a mapOf with
// a non-literal value (`"a" to compute()`) is NOT a closed value-set and emits
// no node.
func TestIssue4428_KotlinNonLiteralMapSkipped(t *testing.T) {
	src := `
package com.example
val M = mapOf("a" to compute(), "b" to "y")
`
	ents := extractKotlinRecords(t, src, "M.kt")
	if en := ktFindEnum(ents, "M"); en != nil {
		t.Fatalf("non-literal mapOf M should emit no value-set, got %+v", en.Properties)
	}
}

// TestIssue4428_KotlinObjectMapMember asserts a `mapOf` declared inside an
// object body (not just top-level) is also captured.
func TestIssue4428_KotlinObjectMapMember(t *testing.T) {
	src := `
package com.example
object Cfg {
    val ROUTES = mapOf("home" to "/", "admin" to "/admin")
}
`
	ents := extractKotlinRecords(t, src, "Cfg.kt")
	en := ktFindEnum(ents, "ROUTES")
	if en == nil {
		t.Fatal("object-level mapOf ROUTES not captured as value-set")
	}
	ms := ktMembers(t, en)
	if v, ok := ktMemberValue(ms, "admin"); !ok || v != "/admin" {
		t.Fatalf("ROUTES[admin] = %q (ok=%v), want /admin", v, ok)
	}
}
