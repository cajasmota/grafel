// Issue #6742 — the enum guard, graded at the MECHANISM rather than the
// outcome.
//
// WHY A SECOND ENUM TEST. csharp_test.TestCsharpHierarchy_EnumUnderlyingType
// IsNotABase asserts an ABSENCE: no hierarchy edge comes out of `enum E : byte`.
// An absence assertion passes for two very different reasons — the guard
// worked, or the code never ran — and the first cut of #6742 shipped in the
// second state: `enum_declaration` was a branch of walk() that never stashed a
// base list, so the enum never reached csBaseTypeNames and a reviewer's mutant
// (accept "predefined_type") survived every test and every fixture.
//
// This file calls csBaseTypeNames DIRECTLY on the enum's own tree-sitter node,
// so "the allow-list is what rejects the storage type" is observed rather than
// asserted in prose. It lives in package csharp (not csharp_test) because that
// is what reaching an unexported function costs; the alternative — an exported
// test-only hook on the production package — would be worse.
package csharp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tscsharp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/csharp"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	"github.com/cajasmota/grafel/internal/types"
)

// findDeclForTest returns the first node of the given type in the tree.
func findDeclForTest(root ts.Node, want string) ts.Node {
	if root == nil {
		return nil
	}
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == want {
			return n
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return nil
}

func TestCsBaseTypeNamesRejectsEnumStorageType6742(t *testing.T) {
	src := `
namespace App
{
    public enum Status : byte { Active, Closed }
    public class Holder : BaseHolder { }
}
`
	p, err := tsofficial.New().NewParser(tscsharp.Language())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer p.Close()
	tree, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root := tree.RootNode()

	enumNode := findDeclForTest(root, "enum_declaration")
	classNode := findDeclForTest(root, "class_declaration")
	if enumNode == nil || classNode == nil {
		t.Fatalf("positive control failed: enum_declaration found=%v, "+
			"class_declaration found=%v — the grammar did not produce the "+
			"nodes this test grades, so its assertions mean nothing",
			enumNode != nil, classNode != nil)
	}

	// Control 1: the enum DOES have a base_list. Without this the assertion
	// below could pass because there was nothing there to reject.
	if bl := findChildByType(enumNode, "base_list"); bl == nil {
		t.Fatalf("positive control failed: `enum Status : byte` produced no " +
			"base_list node, so csBaseTypeNames has nothing to filter and " +
			"the rejection below is vacuous")
	}

	// The assertion: the allow-list, and only the allow-list, drops `byte`.
	if got := csBaseTypeNames(enumNode, []byte(src)); len(got) != 0 {
		t.Fatalf("csBaseTypeNames must reject an enum's predefined_type "+
			"storage type; got %v. An enum-base is `byte`/`int`/…, never a "+
			"supertype — nothing in C# derives from a predefined type", got)
	}

	// Control 2: held constant against a real base list in the same file, so a
	// csBaseTypeNames that returned nil for EVERYTHING cannot pass this test.
	if got := csBaseTypeNames(classNode, []byte(src)); len(got) != 1 || got[0] != "BaseHolder" {
		t.Fatalf("control failed: csBaseTypeNames must still accept a real "+
			"base list; want [BaseHolder], got %v", got)
	}
}

// TestEnumOwnerIsRoutedIntoTheHierarchyPass6742 pins the WIRING the test above
// depends on. csBaseTypeNames rejecting `byte` is worth nothing if walk() never
// hands it an enum — which is precisely the hole that let the reviewer's mutant
// live. This grades the stash, not the edge: the enum record must carry the
// hierarchy metadata that routes it into attachCsharpHierarchy.
func TestEnumOwnerIsRoutedIntoTheHierarchyPass6742(t *testing.T) {
	if got := csDeclKeyword("enum_declaration"); got != "enum" {
		t.Fatalf("csDeclKeyword must map enum_declaration to a keyword the "+
			"kind ladder understands, got %q", got)
	}
	// attachCsharpHierarchy must be willing to OWN the enum record, which is a
	// SCOPE.Schema/enum (buildEnumEntity), NOT a SCOPE.Component.
	// A pass that skipped the kind outright would suppress the edge before
	// the allow-list ran, restoring the vacuity this file exists to remove.
	recs := []types.EntityRecord{{
		Name:    "Status",
		Kind:    "SCOPE.Schema",
		Subtype: "enum",
		Metadata: map[string]interface{}{
			"hierarchy_bases": []string{"SomeAlias"},
			"hierarchy_decl":  "enum",
		},
	}}
	out := attachCsharpHierarchy(recs)
	if len(out[0].Relationships) != 1 {
		t.Fatalf("a SCOPE.Enum owner carrying hierarchy metadata must be "+
			"processed by attachCsharpHierarchy — if a Kind filter skips it, "+
			"allow-list is unreachable for enums and every enum assertion in "+
			"this package is vacuous; got %d edges", len(out[0].Relationships))
	}
}
