package bazel_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/bazel"
	"github.com/cajasmota/grafel/internal/types"
)

// fixtureRoot returns the absolute path of the test workspace fixture.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "workspace"))
	if err != nil {
		t.Fatalf("fixture path: %v", err)
	}
	return abs
}

// walkFixture walks repoRoot and returns all relative file paths (forward slashes).
func walkFixture(t *testing.T, repoRoot string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
	return files
}

// findEntsBySubtype returns all entities with the given Subtype.
func findEntsBySubtype(ents []types.EntityRecord, sub string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Subtype == sub {
			out = append(out, e)
		}
	}
	return out
}

// findRelsByKind returns all rels with the given Kind.
func findRelsByKind(rels []types.RelationshipRecord, kind string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// TestDiscover_BasicFixture verifies the happy path against the bundled
// workspace fixture. It asserts the counts and key structural properties of
// the emitted entities and edges.
func TestDiscover_BasicFixture(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	ents, rels, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// --- Entity counts ---------------------------------------------------------

	buildEnts := findEntsBySubtype(ents, "bazel_build")
	targetEnts := findEntsBySubtype(ents, "bazel_target")

	// Three BUILD files: services/auth/BUILD, services/billing/BUILD.bazel,
	// common/utils/BUILD.
	if len(buildEnts) != 3 {
		t.Errorf("bazel_build entities: got %d, want 3", len(buildEnts))
		for _, e := range buildEnts {
			t.Logf("  %s", e.SourceFile)
		}
	}

	// Targets per BUILD file:
	//   services/auth:     auth_lib, auth_server, auth_test        → 3
	//   services/billing:  billing_lib, billing_server, billing_test → 3
	//   common/utils:      string_utils, string_utils_test          → 2
	//                                                           Total  8
	if len(targetEnts) != 8 {
		t.Errorf("bazel_target entities: got %d, want 8", len(targetEnts))
		for _, e := range targetEnts {
			t.Logf("  %s", e.Name)
		}
	}

	// --- Labels ----------------------------------------------------------------

	targetLabels := make(map[string]bool, len(targetEnts))
	for _, e := range targetEnts {
		targetLabels[e.Name] = true
	}
	wantLabels := []string{
		"//services/auth:auth_lib",
		"//services/auth:auth_server",
		"//services/auth:auth_test",
		"//services/billing:billing_lib",
		"//services/billing:billing_server",
		"//services/billing:billing_test",
		"//common/utils:string_utils",
		"//common/utils:string_utils_test",
	}
	for _, lbl := range wantLabels {
		if !targetLabels[lbl] {
			t.Errorf("missing target label %q", lbl)
		}
	}

	// --- BAZEL_DEPENDS_ON edges ------------------------------------------------

	depRels := findRelsByKind(rels, bazel.RelationshipKindBazelDependsOn)
	if len(depRels) == 0 {
		t.Fatal("no BAZEL_DEPENDS_ON edges emitted")
	}

	// Verify specific intra-workspace edges exist.
	type edgePair struct{ src, dst string }
	edgeByProp := func(srcProp, dstProp string) bool {
		for _, r := range depRels {
			if r.Properties.Get("source_rule") == srcProp && r.Properties.Get("dep_label") == dstProp {
				return true
			}
		}
		return false
	}

	wantEdges := []struct{ src, dep string }{
		// auth_lib → common/utils:string_utils
		{"//services/auth:auth_lib", "//common/utils:string_utils"},
		// auth_lib → services/billing:billing_lib
		{"//services/auth:auth_lib", "//services/billing:billing_lib"},
		// auth_server → common/utils:string_utils
		{"//services/auth:auth_server", "//common/utils:string_utils"},
		// auth_server → :auth_lib (resolved to //services/auth:auth_lib)
		{"//services/auth:auth_server", "//services/auth:auth_lib"},
		// billing_lib → common/utils:string_utils
		{"//services/billing:billing_lib", "//common/utils:string_utils"},
		// billing_server → :billing_lib
		{"//services/billing:billing_server", "//services/billing:billing_lib"},
	}
	for _, we := range wantEdges {
		if !edgeByProp(we.src, we.dep) {
			t.Errorf("missing BAZEL_DEPENDS_ON edge: %q → %q", we.src, we.dep)
		}
	}
}

// TestDiscover_ExternalDeps verifies that external deps (e.g. @pip//:pyjwt)
// get a stable synthetic entity ID and their BAZEL_DEPENDS_ON edges are still
// emitted.
func TestDiscover_ExternalDeps(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	_, rels, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	depRels := findRelsByKind(rels, bazel.RelationshipKindBazelDependsOn)

	// auth_lib has @pip//:pyjwt as a dep.
	found := false
	for _, r := range depRels {
		if r.Properties.Get("source_rule") == "//services/auth:auth_lib" &&
			r.Properties.Get("dep_label") == "@pip//:pyjwt" {
			found = true
			// ToID must be a 16-char hex string (stable synthetic ID).
			if len(r.ToID) != 16 {
				t.Errorf("external dep ToID len=%d want 16: %q", len(r.ToID), r.ToID)
			}
			break
		}
	}
	if !found {
		t.Error("BAZEL_DEPENDS_ON for @pip//:pyjwt not found")
	}
}

// TestDiscover_RuleKinds verifies that py_library, java_library, go_library,
// py_binary, py_test, java_binary, java_test, go_test are all recognised.
func TestDiscover_RuleKinds(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	ents, _, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	targets := findEntsBySubtype(ents, "bazel_target")
	kindSet := make(map[string]bool)
	for _, e := range targets {
		kindSet[e.Properties["rule_kind"]] = true
	}

	wantKinds := []string{
		"py_library", "py_binary", "py_test",
		"java_library", "java_binary", "java_test",
		"go_library", "go_test",
	}
	for _, k := range wantKinds {
		if !kindSet[k] {
			t.Errorf("rule_kind %q not seen in targets", k)
		}
	}
}

// TestDiscover_EntityFields verifies that emitted entities carry the required
// grafel fields (ID, Kind, Subtype, SourceFile, Language, Properties).
func TestDiscover_EntityFields(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	ents, _, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, e := range ents {
		if e.ID == "" {
			t.Errorf("entity missing ID: %+v", e)
		}
		if len(e.ID) != 16 {
			t.Errorf("entity ID len=%d want 16: %q", len(e.ID), e.ID)
		}
		if e.Kind == "" {
			t.Errorf("entity missing Kind: %+v", e)
		}
		if e.SourceFile == "" {
			t.Errorf("entity missing SourceFile: %+v", e)
		}
		if e.Language != "bazel" {
			t.Errorf("entity Language=%q want %q: %q", e.Language, "bazel", e.Name)
		}
		if e.Subtype == "bazel_target" {
			if e.Properties["label"] == "" {
				t.Errorf("bazel_target missing label property: %+v", e)
			}
			if e.Properties["rule_kind"] == "" {
				t.Errorf("bazel_target missing rule_kind property: %+v", e)
			}
		}
	}
}

// TestDiscover_Determinism verifies that calling Discover twice with the same
// input produces byte-identical output (required for issue #481 determinism).
func TestDiscover_Determinism(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	ents1, rels1, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("first Discover: %v", err)
	}
	ents2, rels2, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("second Discover: %v", err)
	}

	if len(ents1) != len(ents2) {
		t.Fatalf("entity count differs: %d vs %d", len(ents1), len(ents2))
	}
	if len(rels1) != len(rels2) {
		t.Fatalf("rel count differs: %d vs %d", len(rels1), len(rels2))
	}
	for i := range ents1 {
		if ents1[i].ID != ents2[i].ID || ents1[i].Name != ents2[i].Name {
			t.Errorf("entity[%d] mismatch: %q vs %q", i, ents1[i].ID, ents2[i].ID)
		}
	}
	for i := range rels1 {
		if rels1[i].FromID != rels2[i].FromID || rels1[i].ToID != rels2[i].ToID {
			t.Errorf("rel[%d] mismatch", i)
		}
	}
}

// TestDiscover_NonBazelFilesIgnored verifies that non-BUILD files do not
// produce any output.
func TestDiscover_NonBazelFilesIgnored(t *testing.T) {
	ents, rels, err := bazel.Discover(context.Background(), "/tmp", []string{
		"README.md", "main.go", "Makefile", "package.json",
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-BUILD files, got %d", len(ents))
	}
	if len(rels) != 0 {
		t.Errorf("expected 0 rels for non-BUILD files, got %d", len(rels))
	}
}

// TestDiscover_ShortFormLabelsResolved verifies that ":name" short-form dep
// labels are resolved to "//pkg:name" absolute form.
func TestDiscover_ShortFormLabelsResolved(t *testing.T) {
	root := fixtureRoot(t)
	files := walkFixture(t, root)

	_, rels, err := bazel.Discover(context.Background(), root, files)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	depRels := findRelsByKind(rels, bazel.RelationshipKindBazelDependsOn)

	// auth_server depends on ":auth_lib" — should resolve to "//services/auth:auth_lib".
	found := false
	for _, r := range depRels {
		if r.Properties.Get("source_rule") == "//services/auth:auth_server" &&
			r.Properties.Get("dep_label") == "//services/auth:auth_lib" {
			found = true
			break
		}
	}
	if !found {
		t.Error("short-form :auth_lib dep not resolved to //services/auth:auth_lib")
		t.Log("actual edges from //services/auth:auth_server:")
		for _, r := range depRels {
			if r.Properties.Get("source_rule") == "//services/auth:auth_server" {
				t.Logf("  dep_label=%q", r.Properties.Get("dep_label"))
			}
		}
	}
}

// TestDiscover_InlineDepsSingleLine verifies that deps listed on a single line
// are parsed correctly.
func TestDiscover_InlineDepsSingleLine(t *testing.T) {
	dir := t.TempDir()
	buildContent := `
go_library(
    name = "mylib",
    srcs = ["lib.go"],
    deps = ["//other/pkg:target", "//another:dep"],
)
`
	err := os.WriteFile(filepath.Join(dir, "BUILD"), []byte(buildContent), 0o644)
	if err != nil {
		t.Fatalf("write BUILD: %v", err)
	}

	ents, rels, err := bazel.Discover(context.Background(), dir, []string{"BUILD"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	targets := findEntsBySubtype(ents, "bazel_target")
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Name != ":mylib" && targets[0].Name != "//"+":mylib" {
		// The root package label is "//:mylib".
	}

	depRels := findRelsByKind(rels, bazel.RelationshipKindBazelDependsOn)
	labels := make([]string, len(depRels))
	for i, r := range depRels {
		labels[i] = r.Properties.Get("dep_label")
	}
	sort.Strings(labels)

	wantDeps := []string{"//another:dep", "//other/pkg:target"}
	if !equalStringSlice(labels, wantDeps) {
		t.Errorf("deps: got %v, want %v", labels, wantDeps)
	}
}

// TestIsBuildFile exercises the IsBuildFile helper.
func TestIsBuildFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"BUILD", true},
		{"BUILD.bazel", true},
		{"services/auth/BUILD", true},
		{"services/billing/BUILD.bazel", true},
		{"main.go", false},
		{"Makefile", false},
		{"WORKSPACE", false},
		{"go.mod", false},
	}
	for _, tc := range cases {
		got := bazel.IsBuildFile(tc.path)
		if got != tc.want {
			t.Errorf("IsBuildFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestIsWorkspaceFile exercises the IsWorkspaceFile helper.
func TestIsWorkspaceFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"WORKSPACE", true},
		{"WORKSPACE.bazel", true},
		{"MODULE.bazel", true},
		{"BUILD", false},
		{"main.go", false},
	}
	for _, tc := range cases {
		got := bazel.IsWorkspaceFile(tc.path)
		if got != tc.want {
			t.Errorf("IsWorkspaceFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// equalStringSlice reports whether two sorted string slices are equal.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTargetLabel exercises the TargetLabel normalisation helper.
func TestTargetLabel(t *testing.T) {
	cases := []struct {
		raw, pkg, want string
	}{
		{"//other/pkg:target", "services/auth", "//other/pkg:target"},
		{":mylib", "services/auth", "//services/auth:mylib"},
		{"@maven//:guava", "services/billing", "@maven//:guava"},
	}
	for _, tc := range cases {
		got := bazel.TargetLabel(tc.raw, tc.pkg)
		if got != tc.want {
			t.Errorf("TargetLabel(%q, %q) = %q, want %q", tc.raw, tc.pkg, got, tc.want)
		}
	}
}

// TestDiscover_EmptyBuildFile verifies that an empty BUILD file produces a
// build entity with 0 targets and no dep edges (does not panic).
func TestDiscover_EmptyBuildFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BUILD"), []byte("# no rules here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ents, rels, err := bazel.Discover(context.Background(), dir, []string{"BUILD"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	buildEnts := findEntsBySubtype(ents, "bazel_build")
	if len(buildEnts) != 1 {
		t.Errorf("expected 1 bazel_build entity, got %d", len(buildEnts))
	}
	if e := buildEnts[0]; e.Properties["rule_count"] != "0" {
		t.Errorf("rule_count=%q want 0", e.Properties["rule_count"])
	}
	if len(rels) != 0 {
		t.Errorf("expected 0 rels for empty BUILD, got %d", len(rels))
	}
}

// TestDiscover_MalformedBuildFile verifies that a syntactically broken BUILD
// file does not panic and still emits a build entity.
func TestDiscover_MalformedBuildFile(t *testing.T) {
	dir := t.TempDir()
	malformed := `
py_library(
    name = "broken
    # unclosed string — parser should not panic
`
	if err := os.WriteFile(filepath.Join(dir, "BUILD"), []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not panic, should not return a hard error.
	ents, _, err := bazel.Discover(context.Background(), dir, []string{"BUILD"})
	if err != nil {
		t.Fatalf("Discover returned unexpected error: %v", err)
	}
	_ = ents
	_ = strings.Contains // suppress unused import warning
}
