package relkinds_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/relkinds"
)

// writeTree materialises a synthetic source tree. Keys are slash-relative
// paths; parent directories are created as needed.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func kindsOf(sites []relkinds.Site) []string {
	var out []string
	for _, s := range sites {
		out = append(out, s.Kind)
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestScanGo_FindsEveryGoDeclarationMechanism covers the three Go shapes the
// #6757 measurement found: a bare string literal in a keyed composite literal,
// an untyped element inside a slice-of-relationship literal, and a
// string(<package-local const>) conversion.
func TestScanGo_FindsEveryGoDeclarationMechanism(t *testing.T) {
	root := writeTree(t, map[string]string{
		// Shape 1: java-style Relationship{RelationshipType: "OWNS"}.
		"custom/java/spring.go": `package java
type Relationship struct{ SourceRef, TargetRef, RelationshipType string }
func f() any { return Relationship{SourceRef: "a", TargetRef: "b", RelationshipType: "OWNS"} }
`,
		// Shape 2: untyped elements inside []types.RelationshipRecord{...}.
		"extractors/sql/sql.go": `package sql
import "github.com/cajasmota/grafel/internal/types"
func f() []types.RelationshipRecord {
	return []types.RelationshipRecord{
		{FromID: "i", ToID: "t", Kind: "INDEXES"},
		{FromID: "x", ToID: "y", Kind: "FIRES"},
	}
}
`,
		// Shape 3: package-local const behind a string() conversion.
		"engine/process_flow_kinds.go": `package engine
const RelationshipKindStepInProcess = "STEP_IN_PROCESS"
`,
		"engine/process_flow.go": `package engine
import "github.com/cajasmota/grafel/internal/graph"
func f() graph.Relationship {
	return graph.Relationship{FromID: "a", ToID: "b", Kind: string(RelationshipKindStepInProcess)}
}
`,
		// _test.go files are out of reach: fixtures emit arbitrary kinds.
		"engine/flow_test.go": `package engine
import "github.com/cajasmota/grafel/internal/graph"
func g() graph.Relationship { return graph.Relationship{Kind: "ONLY_IN_A_TEST"} }
`,
		// A `Kind:` on something that is not a relationship must not be picked up.
		"extractors/elm/tea.go": `package elm
import "github.com/cajasmota/grafel/internal/graph"
func f() graph.Entity { return graph.Entity{Kind: "SCOPE.Model"} }
`,
	})

	res, err := relkinds.ScanGo(root)
	if err != nil {
		t.Fatalf("ScanGo: %v", err)
	}
	eq(t, kindsOf(res.Sites), []string{"FIRES", "INDEXES", "OWNS", "STEP_IN_PROCESS"})
	if res.FilesParsed != 5 {
		t.Fatalf("FilesParsed = %d, want 5 (every non-test .go file)", res.FilesParsed)
	}
	for _, s := range res.Sites {
		if s.Origin != relkinds.OriginGo {
			t.Fatalf("site %+v has origin %q, want %q", s, s.Origin, relkinds.OriginGo)
		}
		if s.Line == 0 || s.File == "" {
			t.Fatalf("site %+v is missing a location", s)
		}
	}
}

// TestScanRuleYAML_FindsRelationshipRuleKinds is the half a Go-only detector is
// structurally blind to (#6757): engine/schema.go binds `relationship_rules`
// and detector.go writes rr.Relationship into the graph unvalidated.
func TestScanRuleYAML_FindsRelationshipRuleKinds(t *testing.T) {
	root := writeTree(t, map[string]string{
		"engine/rules/python/frameworks/flask.yaml": `language: python
relationship_rules:
  - pattern: "x"
    source_type: "a"
    target_type: "b"
    relationship: "REGISTERED_ON"
  - pattern: "y"
    relationship: "IMPORTS"
`,
		"engine/rules/kotlin/frameworks/ktor.yaml": `language: kotlin
relationship_rules:
  - pattern: "z"
    relationship: INSTALLED_IN
`,
		// source_patterns carry entity types, not relationship kinds.
		"engine/rules/go/frameworks/gin.yaml": `language: go
source_patterns:
  - pattern: "p"
    entity_type: "http_endpoint"
`,
	})

	res, err := relkinds.ScanRuleYAML(root)
	if err != nil {
		t.Fatalf("ScanRuleYAML: %v", err)
	}
	eq(t, kindsOf(res.Sites), []string{"IMPORTS", "INSTALLED_IN", "REGISTERED_ON"})
	if res.FilesParsed != 3 {
		t.Fatalf("FilesParsed = %d, want 3", res.FilesParsed)
	}
	for _, s := range res.Sites {
		if s.Origin != relkinds.OriginRuleYAML {
			t.Fatalf("site %+v has origin %q, want %q", s, s.Origin, relkinds.OriginRuleYAML)
		}
	}
}

// TestScan_UnionsBothMechanisms — a caller that asks for the whole population
// must get both halves from one call, so that "I ran the scan" cannot mean
// "I ran the Go half".
func TestScan_UnionsBothMechanisms(t *testing.T) {
	root := writeTree(t, map[string]string{
		"extractors/vhdl/extractor.go": `package vhdl
import "github.com/cajasmota/grafel/internal/types"
func f() types.RelationshipRecord { return types.RelationshipRecord{ToID: "e", Kind: "PORT_OF"} }
`,
		"engine/rules/php/frameworks/symfony.yaml": `language: php
relationship_rules:
  - pattern: "p"
    relationship: "DEFINED_BY"
`,
	})

	res, err := relkinds.Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	eq(t, kindsOf(res.Sites), []string{"DEFINED_BY", "PORT_OF"})
	if res.GoFilesParsed != 1 || res.YAMLFilesParsed != 1 {
		t.Fatalf("GoFilesParsed=%d YAMLFilesParsed=%d, want 1 and 1", res.GoFilesParsed, res.YAMLFilesParsed)
	}
}

// TestScanGo_SkipsExcludedDirs — .claude holds full worktree checkouts of this
// same repository; walking it would scan other branches' source.
func TestScanGo_SkipsExcludedDirs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a/keep.go": `package a
import "github.com/cajasmota/grafel/internal/types"
func f() types.RelationshipRecord { return types.RelationshipRecord{Kind: "KEPT"} }
`,
		".claude/worktrees/x/b/skip.go": `package b
import "github.com/cajasmota/grafel/internal/types"
func f() types.RelationshipRecord { return types.RelationshipRecord{Kind: "FROM_A_WORKTREE"} }
`,
		"a/testdata/skip.go": `package b
import "github.com/cajasmota/grafel/internal/types"
func f() types.RelationshipRecord { return types.RelationshipRecord{Kind: "FROM_TESTDATA"} }
`,
	})

	res, err := relkinds.ScanGo(root)
	if err != nil {
		t.Fatalf("ScanGo: %v", err)
	}
	eq(t, kindsOf(res.Sites), []string{"KEPT"})
}
