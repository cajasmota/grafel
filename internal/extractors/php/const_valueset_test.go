package php_test

// const_valueset_test.go — value-asserting tests for the PHP SCOPE.Enum
// value-set nodes (data-model, epic #3628 / #4419; extends #4429, sibling
// Ruby #4427). Each test drives the REAL registered PHP extractor pipeline on
// byte-copies of representative fixtures and asserts that a queryable value-set
// is emitted with the member roster and literal backing values captured.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractPHPForConst runs the real registered php extractor on src.
func extractPHPForConst(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("php")
	if !ok {
		t.Fatal("php extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "php", TSTree: tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return got
}

// findPHPValueSet returns the SCOPE.Enum value-set node with the given name,
// optionally filtered by kind_hint (pass "" to ignore the hint).
func findPHPValueSet(recs []types.EntityRecord, name, kindHint string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind != string(types.EntityKindEnum) || recs[i].Name != name {
			continue
		}
		if kindHint != "" && recs[i].Properties["kind_hint"] != kindHint {
			continue
		}
		return &recs[i]
	}
	return nil
}

func TestPHPConstValueSet_ArrayMap(t *testing.T) {
	src := `<?php
const PERMISSION_PAGES = [
    'core_admin' => 'core-admin',
    'user'       => 'user-page',
];`
	recs := extractPHPForConst(t, "permissions.php", src)
	vs := findPHPValueSet(recs, "PERMISSION_PAGES", "php_const_map")
	if vs == nil {
		t.Fatal("SCOPE.Enum value-set PERMISSION_PAGES not emitted")
	}
	if got := vs.Properties["members"]; got != "core_admin, user" {
		t.Fatalf("members = %q, want %q", got, "core_admin, user")
	}
	if got := vs.Properties["values"]; got != "core_admin=core-admin, user=user-page" {
		t.Fatalf("values = %q, want %q", got, "core_admin=core-admin, user=user-page")
	}
	// Searchable via the file-scoped QualifiedName.
	if want := extractor.EnumQualifiedName("permissions.php", "PERMISSION_PAGES"); vs.QualifiedName != want {
		t.Fatalf("QualifiedName = %q, want %q", vs.QualifiedName, want)
	}
}

func TestPHPConstValueSet_ClassConstGroup(t *testing.T) {
	src := `<?php
class Pages {
    const CORE_ADMIN = 'core-admin';
    const USER       = 'user-page';
    const LIMIT      = 50;
}`
	recs := extractPHPForConst(t, "pages.php", src)
	vs := findPHPValueSet(recs, "Pages", "php_class_const")
	if vs == nil {
		t.Fatal("SCOPE.Enum value-set Pages (class-const group) not emitted")
	}
	if got := vs.Properties["members"]; got != "CORE_ADMIN, USER, LIMIT" {
		t.Fatalf("members = %q, want %q", got, "CORE_ADMIN, USER, LIMIT")
	}
	if got := vs.Properties["values"]; got != "CORE_ADMIN=core-admin, USER=user-page, LIMIT=50" {
		t.Fatalf("values = %q, want %q", got, "CORE_ADMIN=core-admin, USER=user-page, LIMIT=50")
	}
}

func TestPHPConstValueSet_BackedEnum(t *testing.T) {
	src := `<?php
enum Status: string {
    case Active = 'active';
    case Done   = 'done';
}`
	recs := extractPHPForConst(t, "status.php", src)
	vs := findPHPValueSet(recs, "Status", "php_enum")
	if vs == nil {
		t.Fatal("SCOPE.Enum value-set Status (backed enum) not emitted")
	}
	if got := vs.Properties["members"]; got != "Active, Done" {
		t.Fatalf("members = %q, want %q", got, "Active, Done")
	}
	if got := vs.Properties["values"]; got != "Active=active, Done=done" {
		t.Fatalf("values = %q, want %q (backing values must be captured)", got, "Active=active, Done=done")
	}
	// The existing SCOPE.Schema/enum node must still be emitted (additive).
	var schema *types.EntityRecord
	for i := range recs {
		if recs[i].Kind == "SCOPE.Schema" && recs[i].Name == "Status" {
			schema = &recs[i]
		}
	}
	if schema == nil {
		t.Fatal("existing SCOPE.Schema/enum Status node was lost (must be additive)")
	}
}

func TestPHPConstValueSet_DefineMap(t *testing.T) {
	src := `<?php
define('FEATURE_FLAGS', [
    'beta'  => true,
    'legacy' => false,
]);`
	recs := extractPHPForConst(t, "flags.php", src)
	vs := findPHPValueSet(recs, "FEATURE_FLAGS", "php_define_map")
	if vs == nil {
		t.Fatal("SCOPE.Enum value-set FEATURE_FLAGS (define map) not emitted")
	}
	if got := vs.Properties["members"]; got != "beta, legacy" {
		t.Fatalf("members = %q, want %q", got, "beta, legacy")
	}
	if got := vs.Properties["values"]; got != "beta=true, legacy=false" {
		t.Fatalf("values = %q, want %q", got, "beta=true, legacy=false")
	}
}

func TestPHPConstValueSet_ListArrayMembers(t *testing.T) {
	// A list (no `=>`) records each literal as a value-less member so the
	// roster stays complete.
	src := `<?php const STATUSES = ['active', 'inactive', 'pending'];`
	recs := extractPHPForConst(t, "statuses.php", src)
	vs := findPHPValueSet(recs, "STATUSES", "php_const_map")
	if vs == nil {
		t.Fatal("SCOPE.Enum value-set STATUSES (list array) not emitted")
	}
	if got := vs.Properties["members"]; got != "active, inactive, pending" {
		t.Fatalf("members = %q, want %q", got, "active, inactive, pending")
	}
}

func TestPHPConstValueSet_HonestPartial(t *testing.T) {
	// A bare scalar file-scope const is NOT a collection → no value-set node.
	recs := extractPHPForConst(t, "v.php", `<?php const VERSION = '1.0.0';`)
	if vs := findPHPValueSet(recs, "VERSION", ""); vs != nil {
		t.Fatalf("bare scalar const must not emit a value-set node, got %+v", vs.Properties)
	}
	// An empty array const emits no node.
	recs2 := extractPHPForConst(t, "e.php", `<?php const EMPTY = [];`)
	if vs := findPHPValueSet(recs2, "EMPTY", ""); vs != nil {
		t.Fatal("empty array const must not emit a value-set node")
	}
}

func TestPHPConstValueSet_NonLiteralRecorded(t *testing.T) {
	// A non-literal value records its expression text (not dropped) so the
	// key set is complete and a parity-audit can see the value is dynamic.
	src := `<?php const CONF = ['timeout' => SOME_CONST, 'name' => 'svc'];`
	recs := extractPHPForConst(t, "conf.php", src)
	vs := findPHPValueSet(recs, "CONF", "php_const_map")
	if vs == nil {
		t.Fatal("value-set CONF not emitted")
	}
	if got := vs.Properties["members"]; got != "timeout, name" {
		t.Fatalf("members = %q, want %q", got, "timeout, name")
	}
	if got := vs.Properties["values"]; got != "timeout=SOME_CONST, name=svc" {
		t.Fatalf("values = %q, want %q (dynamic expr text must be recorded)", got, "timeout=SOME_CONST, name=svc")
	}
}
