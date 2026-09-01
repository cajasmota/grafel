package types

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// kindsSourceFile is the single file that declares the RelationshipKind
// vocabulary. The guard below reads it rather than a hand-written list: a
// hand-maintained roster of constants would be the very defect it is guarding
// against, one level up.
const kindsSourceFile = "kinds.go"

// declaredRelationshipKind is one `Name RelationshipKind = "VALUE"` constant as
// it is actually written in kinds.go.
type declaredRelationshipKind struct {
	Name  string
	Value string
}

// declaredRelationshipKindsFromSource parses kinds.go and returns every
// constant declared with the explicit type RelationshipKind, in source order.
//
// Const-block type elision is handled: inside a `const (...)` group a spec with
// neither a type nor values repeats the previous spec's type and expression, so
// the last explicit type is carried forward for exactly that case.
func declaredRelationshipKindsFromSource(t *testing.T) []declaredRelationshipKind {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, kindsSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", kindsSourceFile, err)
	}

	var out []declaredRelationshipKind
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var carriedType ast.Expr
		var carriedValues []ast.Expr
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ, values := vs.Type, vs.Values
			if typ == nil && len(values) == 0 {
				// Elided spec: repeats the previous type and expression.
				typ, values = carriedType, carriedValues
			} else {
				carriedType, carriedValues = typ, values
			}
			ident, ok := typ.(*ast.Ident)
			if !ok || ident.Name != "RelationshipKind" {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				if i >= len(values) {
					continue
				}
				lit, ok := values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, declaredRelationshipKind{
					Name:  name.Name,
					Value: lit.Value[1 : len(lit.Value)-1], // strip the quotes
				})
			}
		}
	}
	return out
}

// TestDeclaredRelationshipKindsExtraction_NonVacuous is the floor under the
// completeness guard below. If the extraction ever breaks — a renamed file, a
// reworked const layout, a botched AST walk — it would find zero constants and
// the completeness guard would pass by examining nothing. This makes that
// failure loud instead of silent.
//
// It deliberately asserts two independent things: a population floor, and the
// presence of specific long-lived constants by name AND value.
func TestDeclaredRelationshipKindsExtraction_NonVacuous(t *testing.T) {
	declared := declaredRelationshipKindsFromSource(t)

	// The population floor is DERIVED, not a magic number: every kind
	// AllRelationshipKinds() returns comes from a constant declared in this
	// file, so the parse must find at least as many constants as the accessor
	// returns. A parse that narrows to a subset of the const blocks fails here,
	// not only a parse that finds nothing at all — and the floor rises on its
	// own as kinds are added.
	if minDeclared := len(AllRelationshipKinds()); len(declared) < minDeclared {
		t.Fatalf("extracted %d RelationshipKind constants from %s, but AllRelationshipKinds() "+
			"alone returns %d; the extraction is no longer reading the declarations and the "+
			"completeness guard would be vacuous", len(declared), kindsSourceFile, minDeclared)
	}

	byName := map[string]string{}
	for _, d := range declared {
		byName[d.Name] = d.Value
	}
	sentinels := map[string]string{
		"RelationshipKindCalls":      "CALLS",
		"RelationshipKindImports":    "IMPORTS",
		"RelationshipKindDeploys":    "DEPLOYS",
		"RelationshipKindDeliversTo": "DELIVERS_TO",
		"RelationshipKindEnqueues":   "ENQUEUES",
	}
	for name, want := range sentinels {
		got, ok := byName[name]
		if !ok {
			t.Errorf("extraction did not find %s in %s; the AST walk is not reading the declarations",
				name, kindsSourceFile)
			continue
		}
		if got != want {
			t.Errorf("extraction read %s = %q, want %q", name, got, want)
		}
	}
}

// TestAllRelationshipKinds_CoversEveryDeclaredConstant pins the general
// property that #6741 Arm 1 could not: EVERY RelationshipKind constant declared
// in kinds.go must appear in AllRelationshipKinds().
//
// Arm 1 pinned PRODUCES/CONSUMES by name, so it could not catch the two
// constants that had been declared, documented and actively emitted for months
// while missing from their own enum (#6757):
//
//	RelationshipKindDeliversTo  — emitted at internal/engine/async_trigger_edges.go:96
//	RelationshipKindEnqueues    — emitted at internal/engine/scheduled_jobs_edges.go
//	                              (four sites)
//
// A constant that is declared but unregistered makes IsValidRelationshipKind
// report false for a kind the graph really carries, which is exactly what
// #6757's enforcement would reject. There is no legitimate reason to declare a
// RelationshipKind and withhold it from the vocabulary: if a kind is to be
// retired, delete the constant. So this guard has no exemption list — adding
// one would reopen the hole.
func TestAllRelationshipKinds_CoversEveryDeclaredConstant(t *testing.T) {
	declared := declaredRelationshipKindsFromSource(t)
	if len(declared) == 0 {
		t.Fatalf("no RelationshipKind constants extracted from %s; guard would be vacuous", kindsSourceFile)
	}

	registered := map[RelationshipKind]bool{}
	for _, k := range AllRelationshipKinds() {
		registered[k] = true
	}

	var missing []string
	for _, d := range declared {
		if !registered[RelationshipKind(d.Value)] {
			missing = append(missing, d.Name+" ("+d.Value+")")
			continue
		}
		if !IsValidRelationshipKind(d.Value) {
			t.Errorf("IsValidRelationshipKind(%q) = false although %s is registered", d.Value, d.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d RelationshipKind constant(s) declared in %s but missing from AllRelationshipKinds(): %v\n"+
			"IsValidRelationshipKind rejects these, yet producers emit them. Add each to AllRelationshipKinds().",
			len(missing), kindsSourceFile, missing)
	}
}
