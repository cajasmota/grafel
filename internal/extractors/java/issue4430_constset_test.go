package java_test

// issue4430_constset_test.go — in-pipeline (REAL extract pipeline) tests for
// the Java constant-COLLECTION value-set indexing (#4430, extends #4420/#4429).
//
// Each test runs the registered Java extractor over a byte-copy of a
// representative fixture and asserts the SCOPE.Enum value-set is (a) emitted,
// (b) searchable by name, and (c) enumerates the real {key,value} pairs via the
// shared structured members_json property — the same RED→GREEN contract the
// Python/TS arms use.

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// constMemberJSON mirrors the shared members_json shape emitted by EnumEntity.
type constMemberJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line,omitempty"`
}

func parseMembersJSON(t *testing.T, en *types.EntityRecord) map[string]string {
	t.Helper()
	raw, ok := en.Properties["members_json"]
	if !ok {
		t.Fatalf("members_json absent on %s", en.Name)
	}
	var arr []constMemberJSON
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		t.Fatalf("members_json not valid JSON: %v", err)
	}
	m := make(map[string]string, len(arr))
	for _, e := range arr {
		m[e.Key] = e.Value
	}
	return m
}

// TestJavaConstMap_StaticFinalMapOf is the primary live-validation: a
// `static final Map<String,String> = Map.of(...)` constant map is emitted as a
// searchable value-set whose members enumerate the real role→slug pairs (the
// hyphen-vs-underscore parity case from the cross-language oracle).
func TestJavaConstMap_StaticFinalMapOf(t *testing.T) {
	src := `
package com.upvate.auth;

import java.util.Map;

public final class RoleSlugs {
    public static final Map<String, String> ROLE_SLUGS = Map.of(
        "CoreAdmin", "core-admin",
        "BuildingManager", "building_manager",
        "Tenant", "tenant"
    );
}
`
	recs := extractJavaForEnum(t, "RoleSlugs.java", src)

	// (a) emitted + (b) searchable by name.
	en := findJavaEnum(recs, "RoleSlugs.ROLE_SLUGS")
	if en == nil {
		t.Fatal("SCOPE.Enum:RoleSlugs.ROLE_SLUGS value-set not found")
	}
	if got := en.Properties["kind_hint"]; got != "java_const_map" {
		t.Fatalf("kind_hint = %q, want java_const_map", got)
	}
	if got := en.Properties["member_count"]; got != "3" {
		t.Fatalf("member_count = %q, want 3", got)
	}

	// (c) members enumerate the real key→value pairs (the hyphen vs underscore
	// distinction is preserved as structured data).
	m := parseMembersJSON(t, en)
	if m["CoreAdmin"] != "core-admin" {
		t.Fatalf("CoreAdmin -> %q, want core-admin", m["CoreAdmin"])
	}
	if m["BuildingManager"] != "building_manager" {
		t.Fatalf("BuildingManager -> %q, want building_manager", m["BuildingManager"])
	}
	if m["Tenant"] != "tenant" {
		t.Fatalf("Tenant -> %q, want tenant", m["Tenant"])
	}
}

// TestJavaConstGroup_InterfaceConstants validates the constants-interface
// shape: a group of `String FOO = "foo";` constants in an interface is emitted
// as one value-set named after the interface.
func TestJavaConstGroup_InterfaceConstants(t *testing.T) {
	src := `
package com.upvate.auth;

public interface Permissions {
    String READ = "read";
    String WRITE = "write";
    String DELETE = "delete";
    int MAX_PAGES = 50;
}
`
	recs := extractJavaForEnum(t, "Permissions.java", src)

	en := findJavaEnum(recs, "Permissions")
	if en == nil {
		t.Fatal("SCOPE.Enum:Permissions constant-group value-set not found")
	}
	if got := en.Properties["kind_hint"]; got != "java_const_group" {
		t.Fatalf("kind_hint = %q, want java_const_group", got)
	}
	m := parseMembersJSON(t, en)
	if m["READ"] != "read" || m["WRITE"] != "write" || m["DELETE"] != "delete" {
		t.Fatalf("string constants not enumerated: %#v", m)
	}
	if m["MAX_PAGES"] != "50" {
		t.Fatalf("MAX_PAGES -> %q, want 50", m["MAX_PAGES"])
	}
}

// TestJavaConstMap_MapOfEntries covers the Map.ofEntries(entry(...)) shape.
func TestJavaConstMap_MapOfEntries(t *testing.T) {
	src := `
package com.upvate.auth;

import java.util.Map;
import static java.util.Map.entry;

public final class Codes {
    static final Map<String, String> CODES = Map.ofEntries(
        entry("ok", "200"),
        entry("notFound", "404")
    );
}
`
	recs := extractJavaForEnum(t, "Codes.java", src)
	en := findJavaEnum(recs, "Codes.CODES")
	if en == nil {
		t.Fatal("SCOPE.Enum:Codes.CODES (ofEntries) value-set not found")
	}
	m := parseMembersJSON(t, en)
	if m["ok"] != "200" || m["notFound"] != "404" {
		t.Fatalf("ofEntries members not enumerated: %#v", m)
	}
}

// TestJavaConstMap_GuavaImmutableMap covers Guava ImmutableMap.of(...) and the
// builder().put(...).build() chain (declaration order preserved).
func TestJavaConstMap_GuavaImmutableMap(t *testing.T) {
	src := `
package com.upvate.auth;

import com.google.common.collect.ImmutableMap;

public final class GuavaMaps {
    static final ImmutableMap<String, String> FLAT =
        ImmutableMap.of("a", "1", "b", "2");
    static final ImmutableMap<String, String> BUILT =
        ImmutableMap.<String, String>builder()
            .put("x", "10")
            .put("y", "20")
            .build();
}
`
	recs := extractJavaForEnum(t, "GuavaMaps.java", src)

	flat := findJavaEnum(recs, "GuavaMaps.FLAT")
	if flat == nil {
		t.Fatal("SCOPE.Enum:GuavaMaps.FLAT (ImmutableMap.of) not found")
	}
	fm := parseMembersJSON(t, flat)
	if fm["a"] != "1" || fm["b"] != "2" {
		t.Fatalf("ImmutableMap.of members: %#v", fm)
	}

	built := findJavaEnum(recs, "GuavaMaps.BUILT")
	if built == nil {
		t.Fatal("SCOPE.Enum:GuavaMaps.BUILT (builder chain) not found")
	}
	bm := parseMembersJSON(t, built)
	if bm["x"] != "10" || bm["y"] != "20" {
		t.Fatalf("ImmutableMap builder members: %#v", bm)
	}
	// Declaration order: x before y.
	if built.Properties["members"] != "x, y" {
		t.Fatalf("builder order = %q, want \"x, y\"", built.Properties["members"])
	}
}

// TestJavaConstArray covers a static-final array of constant string literals.
func TestJavaConstArray(t *testing.T) {
	src := `
package com.upvate.auth;

public final class Names {
    static final String[] NAMES = {"alpha", "beta", "gamma"};
}
`
	recs := extractJavaForEnum(t, "Names.java", src)
	en := findJavaEnum(recs, "Names.NAMES")
	if en == nil {
		t.Fatal("SCOPE.Enum:Names.NAMES (const array) not found")
	}
	if got := en.Properties["kind_hint"]; got != "java_const_array" {
		t.Fatalf("kind_hint = %q, want java_const_array", got)
	}
	if got := en.Properties["members"]; got != "alpha, beta, gamma" {
		t.Fatalf("array members = %q", got)
	}
}

// TestJavaConstGroup_SingleConstantNotEmitted asserts honest-partial: a lone
// constant is NOT promoted to a value-set (needs ≥2 to be a comparable set).
func TestJavaConstGroup_SingleConstantNotEmitted(t *testing.T) {
	src := `
package com.upvate.auth;

public final class Lonely {
    public static final String ONLY = "one";
}
`
	recs := extractJavaForEnum(t, "Lonely.java", src)
	if en := findJavaEnum(recs, "Lonely"); en != nil {
		t.Fatalf("single constant should not form a value-set, got %s", en.Name)
	}
}
