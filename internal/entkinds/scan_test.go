package entkinds_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestScanRuleYAMLReadsBoundSourcePatterns is the core case: an entity_type
// under source_patterns is a live producer — detector.go copies it into
// EntityRecord.Kind — and the scan must name it with file and line.
func TestScanRuleYAMLReadsBoundSourcePatterns(t *testing.T) {
	root := t.TempDir()
	write(t, root, "python/frameworks/x.yaml", "source_patterns:\n"+
		"  - pattern: \"a\"\n"+
		"    entity_type: Route\n")

	res, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesParsed != 1 {
		t.Fatalf("FilesParsed = %d, want 1", res.FilesParsed)
	}
	if len(res.Sites) != 1 {
		t.Fatalf("sites = %v, want exactly one", res.Sites)
	}
	got := res.Sites[0]
	if got.Kind != "Route" || got.File != "python/frameworks/x.yaml" || got.Line != 3 {
		t.Errorf("site = %+v; want Route at python/frameworks/x.yaml:3", got)
	}
	if !got.Bound {
		t.Errorf("source_patterns[].entity_type must be reported as Bound: %+v", got)
	}
	if got.Path != "source_patterns[].entity_type" {
		t.Errorf("Path = %q, want source_patterns[].entity_type", got.Path)
	}
}

// TestScanRuleYAMLReadsUnboundDeclarations pins the half a schema-driven decode
// would miss. kubernetes/extras.yaml declares kinds under `entity_extraction:`
// and `k8s_resource_types:`, keys internal/engine/schema.go does not bind at
// all. They are still declarations — the file says these strings are the kinds
// — and a scan that only decoded FrameworkRule would report the file clean.
func TestScanRuleYAMLReadsUnboundDeclarations(t *testing.T) {
	root := t.TempDir()
	write(t, root, "kubernetes/extras.yaml", "entity_extraction:\n"+
		"  mappings:\n"+
		"    - k8s_kinds: [Ingress]\n"+
		"      entity_type: IngressHost\n"+
		"k8s_resource_types:\n"+
		"  networking:\n"+
		"    - kind: Service\n"+
		"      entity_mapping: IngressHost\n")

	res, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sites) != 2 {
		t.Fatalf("sites = %+v, want 2", res.Sites)
	}
	for _, s := range res.Sites {
		if s.Kind != "IngressHost" {
			t.Errorf("kind = %q, want IngressHost", s.Kind)
		}
		if s.Bound {
			t.Errorf("%s sits under a key internal/engine/schema.go does not bind; Bound must be false", s)
		}
		if s.Line == 0 {
			t.Errorf("%s has no line", s)
		}
	}
	if res.Sites[0].Path != "entity_extraction.mappings[].entity_type" {
		t.Errorf("Path = %q", res.Sites[0].Path)
	}
	if res.Sites[1].Path != "k8s_resource_types.networking[].entity_mapping" {
		t.Errorf("Path = %q", res.Sites[1].Path)
	}
}

// TestScanRuleYAMLReportsNonScalarAsUnresolved: a declaration the scan cannot
// read must be reported, not dropped. A dropped one is indistinguishable from
// a clean file.
func TestScanRuleYAMLReportsNonScalarAsUnresolved(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go/a.yaml", "source_patterns:\n  - entity_type: [A, B]\n  - entity_type: \"\"\n")

	res, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sites) != 0 {
		t.Errorf("sites = %+v, want none", res.Sites)
	}
	if res.Unresolved() != 2 {
		t.Fatalf("unresolved = %d, want 2 (a sequence value and an empty scalar)", res.Unresolved())
	}
}

// TestScanRuleYAMLCountsUnparseableFilesAsRead separates "the walk is broken"
// from "one file is malformed".
func TestScanRuleYAMLCountsUnparseableFilesAsRead(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.yaml", "\tnot: [valid")
	write(t, root, "b.yaml", "source_patterns:\n  - entity_type: Route\n")

	res, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesParsed != 2 {
		t.Errorf("FilesParsed = %d, want 2 — a file that failed to parse was still reached", res.FilesParsed)
	}
	if len(res.Sites) != 1 {
		t.Errorf("sites = %+v, want 1", res.Sites)
	}
}

// TestScanGoReadsEntityLiterals covers the other producer mechanism, and pins
// that a Relationship literal's Kind is NOT mistaken for an entity kind.
func TestScanGoReadsEntityLiterals(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.go", `package p

const KindLocal = "SCOPE.Local"

type Entity struct{ Kind string }
type Relationship struct{ Kind string }

func f() []Entity {
	_ = Relationship{Kind: "CALLS"}
	return []Entity{{Kind: "SCOPE.Function"}, {Kind: KindLocal}, {Kind: mystery()}}
}

func mystery() string { return "" }
`)
	write(t, root, "p/a_test.go", `package p

func g() Entity { return Entity{Kind: "INVENTED"} }
`)

	res, err := entkinds.ScanGo(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesParsed != 1 {
		t.Fatalf("FilesParsed = %d, want 1 (_test.go is out of scope)", res.FilesParsed)
	}
	got := map[string]bool{}
	for _, s := range res.Sites {
		got[s.Kind] = true
	}
	for _, want := range []string{"SCOPE.Function", "SCOPE.Local"} {
		if !got[want] {
			t.Errorf("ScanGo missed %q; sites = %+v", want, res.Sites)
		}
	}
	if got["CALLS"] {
		t.Error("ScanGo read a Relationship literal's Kind as an entity kind")
	}
	if got["INVENTED"] {
		t.Error("ScanGo read a _test.go fixture")
	}
	if res.Unresolved() != 1 {
		t.Errorf("unresolved = %d, want 1 (the mystery() call)", res.Unresolved())
	}
}

// TestScanUnionCarriesBothHalves: Scan must not silently drop a half, and its
// per-mechanism counts must stay separable so a caller can tell "no YAML in
// this tree" from "the YAML half read nothing".
func TestScanUnionCarriesBothHalves(t *testing.T) {
	root := t.TempDir()
	write(t, root, "p/a.go", "package p\n\ntype Entity struct{ Kind string }\n\nvar _ = Entity{Kind: \"SCOPE.Function\"}\n")
	write(t, root, "r/x.yaml", "source_patterns:\n  - entity_type: Route\n")

	res, err := entkinds.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.GoFilesParsed != 1 || res.YAMLFilesParsed != 1 || res.FilesParsed != 2 {
		t.Fatalf("counts = go %d, yaml %d, total %d; want 1/1/2",
			res.GoFilesParsed, res.YAMLFilesParsed, res.FilesParsed)
	}
	byOrigin := map[string]string{}
	for _, s := range res.Sites {
		byOrigin[s.Origin] = s.Kind
	}
	if byOrigin[entkinds.OriginGo] != "SCOPE.Function" {
		t.Errorf("go half missing from the union: %+v", res.Sites)
	}
	if byOrigin[entkinds.OriginRuleYAML] != "Route" {
		t.Errorf("yaml half missing from the union: %+v", res.Sites)
	}
}

// TestScanSkipsWorktreeCheckouts: .claude holds full checkouts of this same
// repository. Descending into it would scan other branches' rule files.
func TestScanSkipsWorktreeCheckouts(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".claude/worktrees/w/r/x.yaml", "source_patterns:\n  - entity_type: Ghost\n")
	write(t, root, "testdata/x.yaml", "source_patterns:\n  - entity_type: Fixture\n")
	write(t, root, "r/x.yaml", "source_patterns:\n  - entity_type: Route\n")

	res, err := entkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesParsed != 1 {
		t.Fatalf("FilesParsed = %d, want 1; sites = %+v", res.FilesParsed, res.Sites)
	}
}
