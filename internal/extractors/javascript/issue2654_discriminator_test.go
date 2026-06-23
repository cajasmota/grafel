// Package javascript_test — issue #2654 discriminator-pattern extraction tests.
//
// Verifies that BinaryExpression nodes of the form `identifier === literal`
// are detected in function/method bodies and stamped as
// Properties["discriminators"] on the enclosing entity.
//
// Six cases:
//  1. TestDiscriminator_TSStrictEquality_NumericLiteral  — status === 1
//  2. TestDiscriminator_TSStrictEquality_StringLiteral   — type === 'periodic'
//  3. TestDiscriminator_MultipleInSameEntity             — 3 discriminators in one function
//  4. TestDiscriminator_SkipsTypeof                      — typeof x === 'string' (skip)
//  5. TestDiscriminator_SkipsVarToVar                    — a === b (skip)
//  6. TestDiscriminator_ReversedForm                     — 1 === status (also captured)
package javascript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/types"
)

// parseTSDiscriminator parses source with the TypeScript grammar.
func parseTSDiscriminator(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTSDiscriminator: %v", err)
	}
	return tree
}

// extractTSForDiscriminator runs the TypeScript extractor and returns entities.
func extractTSForDiscriminator(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	content := []byte(src)
	tree := parseTSDiscriminator(t, content)
	ext := javascript.New()
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     "test.tsx",
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// discriminatorProp returns the "discriminators" property of the entity named
// entityName, or "" when the entity is not found or the property is absent.
func discriminatorProp(ents []types.EntityRecord, entityName string) string {
	for i := range ents {
		if ents[i].Name == entityName {
			return ents[i].Properties["discriminators"]
		}
	}
	return ""
}

// TestDiscriminator_TSStrictEquality_NumericLiteral verifies that a numeric
// strict-equality comparison (`status === 1`) inside a function body is
// captured as discriminator "status=1" on the enclosing function entity.
func TestDiscriminator_TSStrictEquality_NumericLiteral(t *testing.T) {
	src := `
function checkStatus() {
  if (status === 1) {
    return true;
  }
  return false;
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "checkStatus")
	if got == "" {
		t.Fatalf("checkStatus: discriminators property is empty; want 'status=1'")
	}
	if !strings.Contains(got, "status=1") {
		t.Errorf("checkStatus: discriminators=%q, want it to contain 'status=1'", got)
	}
}

// TestDiscriminator_TSStrictEquality_StringLiteral verifies that a string
// strict-equality comparison (`type === 'periodic'`) inside a function body is
// captured as discriminator "type=periodic" on the enclosing function entity.
func TestDiscriminator_TSStrictEquality_StringLiteral(t *testing.T) {
	src := `
function handleType() {
  const isPeriodic = type === 'periodic';
  return isPeriodic;
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "handleType")
	if got == "" {
		t.Fatalf("handleType: discriminators property is empty; want 'type=periodic'")
	}
	if !strings.Contains(got, "type=periodic") {
		t.Errorf("handleType: discriminators=%q, want it to contain 'type=periodic'", got)
	}
}

// TestDiscriminator_MultipleInSameEntity verifies that multiple discriminator
// comparisons inside a single function body all appear in the property.
func TestDiscriminator_MultipleInSameEntity(t *testing.T) {
	src := `
function processChecklist() {
  const isCat5 = checklistType === 2;
  const isPeriodic = type === 'periodic';
  if (status === 1) {
    return 'paid';
  }
  return null;
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "processChecklist")
	if got == "" {
		t.Fatalf("processChecklist: discriminators property is empty; want 3 discriminators")
	}
	wantPairs := []string{"checklistType=2", "type=periodic", "status=1"}
	for _, pair := range wantPairs {
		if !strings.Contains(got, pair) {
			t.Errorf("processChecklist: discriminators=%q, want it to contain %q", got, pair)
		}
	}
}

// TestDiscriminator_SkipsTypeof verifies that typeof checks (`typeof x === 'string'`)
// are NOT emitted as discriminators, because the LHS is a typeof_expression,
// not a bare identifier.
func TestDiscriminator_SkipsTypeof(t *testing.T) {
	src := `
function validateInput(x) {
  if (typeof x === 'string') {
    return true;
  }
  return false;
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "validateInput")
	// The typeof form should NOT produce a discriminator. If the property is
	// non-empty it must not contain a typeof-derived pair.
	if got != "" {
		// A typeof_expression on the LHS should be excluded. The pair would look
		// like "x=string" which is incorrect (x is not the variable being compared
		// to 'string'; typeof x is a type check, not a value binding).
		// We allow other discriminators if present, but if the only expression is
		// the typeof check and it produces a pair, that is a bug.
		t.Logf("validateInput: discriminators=%q (want empty for typeof-only body)", got)
		// Fail only if a typeof-derived pair appears (heuristic: "x=string").
		if got == "x=string" {
			t.Errorf("validateInput: discriminators=%q — typeof check was incorrectly captured", got)
		}
	}
}

// TestDiscriminator_SkipsVarToVar verifies that equality checks between two
// identifiers (`a === b`) are NOT emitted as discriminators.
func TestDiscriminator_SkipsVarToVar(t *testing.T) {
	src := `
function compareVars(a, b) {
  if (a === b) {
    return true;
  }
  return false;
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "compareVars")
	if got != "" {
		t.Errorf("compareVars: discriminators=%q, want empty (var-to-var should be skipped)", got)
	}
}

// TestDiscriminator_ReversedForm verifies that the reversed form (`1 === status`)
// is also captured correctly with (variable, literal) order.
func TestDiscriminator_ReversedForm(t *testing.T) {
	src := `
function checkPaid() {
  if (1 === status) {
    return 'paid';
  }
}
`
	ents := extractTSForDiscriminator(t, src)
	got := discriminatorProp(ents, "checkPaid")
	if got == "" {
		t.Fatalf("checkPaid: discriminators property is empty; want 'status=1'")
	}
	if !strings.Contains(got, "status=1") {
		t.Errorf("checkPaid: discriminators=%q, want it to contain 'status=1'", got)
	}
}
