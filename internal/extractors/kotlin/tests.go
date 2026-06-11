package kotlin

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// kotestSpecBaseTypes is the set of Kotest spec-style base classes whose
// constructor-lambda body hosts the test DSL (`"name" { ... }`, `describe/it`,
// `given/when/then`, `feature/scenario`, …). A `class FooSpec : <base>({ … })`
// declaration carries ALL of its example logic inside that anonymous
// constructor lambda — there is NO `@Test fun`, so the generic walk() (which
// mines `function_declaration` bodies for CALLS) produces ZERO CALLS edges for
// the whole spec. The receiver-typed `c.getCounts()` call inside the lambda is
// therefore orphaned and ComputeCoverage sees every controller reached only by
// a Kotest spec as untested.
//
// #4687 (the Kotlin slice of epic #4615): emit ONE SCOPE.Operation
// (subtype=test_scope) per Kotest spec class owning the receiver-typed CALLS
// edges mined from the spec lambda — the Kotlin mirror of the TS/JS
// (javascript/tests.go::emitTestScopeOwner, #4680), Ruby (#4719) and PHP (#4721)
// test-scope owners. JUnit5 `@Test fun` are NAMED operations already mined by
// walk(), so ONLY the Kotest anonymous-lambda case needs this pass.
var kotestSpecBaseTypes = map[string]bool{
	"StringSpec": true, "FunSpec": true, "DescribeSpec": true,
	"BehaviorSpec": true, "ShouldSpec": true, "WordSpec": true,
	"FeatureSpec": true, "ExpectSpec": true, "FreeSpec": true,
	"AnnotationSpec": true,
}

// emitKotestTestScopeOwner inspects a class_declaration. When the class extends a
// Kotest spec base type, it mines the constructor-lambda body for CALLS (with
// the #4687 local/field receiver typing applied, since extractCallRelationships
// finds every call_expression descendant) and appends a single test_scope
// SCOPE.Operation that owns them. No-op for non-spec classes and for spec
// classes whose lambda resolves no CALLS (shape-only specs → no owner, honest
// exclusion).
func emitKotestTestScopeOwner(
	node *sitter.Node,
	file extractor.FileInput,
	className string,
	ctx *kotlinCrossCtx,
	out *[]types.EntityRecord,
) {
	if node == nil || className == "" {
		return
	}
	lambda, base := kotestSpecLambdaBody(node, file.Content)
	if lambda == nil {
		return
	}
	// The spec lambda body owns the scope. The receiver-typing map combines any
	// class-level fields (rare in Kotest, but supported) with the lambda's own
	// locals (handled inside extractCallRelationships).
	rels := extractCallRelationships(lambda, file.Content, className, ctx)
	// Drop the Kotest DSL scaffolding calls (the `"name" { … }`, describe/it/…
	// receivers) — they are not calls into production code and would resolve to
	// nothing. They are bare leaf targets matching the DSL method names.
	filtered := rels[:0]
	for _, r := range rels {
		if r.ToID != "" && kotestDSLScaffolding[r.ToID] {
			continue
		}
		filtered = append(filtered, r)
	}
	if len(filtered) == 0 {
		return // shape-only spec — no production calls, no scope owner.
	}
	name := kotlinTestScopeName(file.Path, className)
	rec := types.EntityRecord{
		Name:          name,
		Kind:          "SCOPE.Operation",
		Subtype:       "test_scope",
		SourceFile:    file.Path,
		Language:      "kotlin",
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		Relationships: filtered,
		Properties: map[string]string{
			"framework":   "kotest",
			"provenance":  "INFERRED_FROM_KOTEST_TEST_SCOPE",
			"test_scope":  "true",
			"spec_style":  base,
			"test_class":  className,
			"description": "test scope " + name,
		},
	}
	*out = append(*out, rec)
}

// kotestDSLScaffolding lists the Kotest DSL block functions that structure a spec
// (and would otherwise surface as bare CALLS targets from the lambda body). They
// are filtered from the test_scope owner's edges.
var kotestDSLScaffolding = map[string]bool{
	"describe": true, "context": true, "it": true, "should": true,
	"given": true, "when": true, "then": true, "and": true,
	"feature": true, "scenario": true, "expect": true, "xdescribe": true,
	"xit": true, "test": true, "Given": true, "When": true, "Then": true,
	"And": true,
}

// kotestSpecLambdaBody returns the body of the constructor-lambda passed to a
// Kotest spec base type in a class declaration's delegation, plus the base type
// name. Returns (nil, "") when the class does not extend a known Kotest spec or
// the delegation has no constructor lambda. Shape:
//
//	class FooSpec : StringSpec({ … })
//	  └ delegation_specifier → constructor_invocation
//	       ├ user_type → type_identifier  (the base spec type)
//	       └ value_arguments → value_argument → lambda_literal → <body>
func kotestSpecLambdaBody(node *sitter.Node, src []byte) (*sitter.Node, string) {
	for i := 0; i < int(node.ChildCount()); i++ {
		ds := node.Child(i)
		if ds.Type() != "delegation_specifier" {
			continue
		}
		ci := firstChildOfTypeNode(ds, "constructor_invocation")
		if ci == nil {
			continue
		}
		ut := firstChildOfTypeNode(ci, "user_type")
		if ut == nil {
			continue
		}
		baseIDs := findAllNodes(ut, "type_identifier")
		if len(baseIDs) == 0 {
			continue
		}
		base := string(src[baseIDs[0].StartByte():baseIDs[0].EndByte()])
		if !kotestSpecBaseTypes[base] {
			continue
		}
		va := firstChildOfTypeNode(ci, "value_arguments")
		if va == nil {
			continue
		}
		// value_arguments → value_argument → lambda_literal.
		if lams := findAllNodes(va, "lambda_literal"); len(lams) > 0 {
			return lams[0], base
		}
	}
	return nil, ""
}

// firstChildOfTypeNode returns the first direct child of n whose Type() == kind,
// or nil.
func firstChildOfTypeNode(n *sitter.Node, kind string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if ch := n.Child(i); ch.Type() == kind {
			return ch
		}
	}
	return nil
}

// kotlinTestScopeName derives a stable scope-owner name from the spec file path
// and class name, e.g. `CountSpec@CountSpec.kt`. Keeps it unique per
// (file, class) so two specs in one file get distinct owners.
func kotlinTestScopeName(path, className string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == className {
		return className
	}
	return className + "@" + base
}
