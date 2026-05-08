package golang

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsgo "github.com/smacker/go-tree-sitter/golang"
)

// parseGoInternal parses Go source inside the package test so we can
// access unexported helpers (isErrorIdent, isNilLiteral,
// binaryExprOperatorIs, firstAndLastValueChildren).
func parseGoInternal(t *testing.T, src string) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(tsgo.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

// TestIsErrorIdent_BareErr covers the exact-match branch.
func TestIsErrorIdent_BareErr(t *testing.T) {
	src := "package main\nfunc f() { var err int; _ = err }\n"
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "identifier") {
		if nodeText(n, []byte(src)) == "err" {
			if !isErrorIdent(n, []byte(src)) {
				t.Fatalf("isErrorIdent(err) = false, want true")
			}
			return
		}
	}
	t.Fatal("did not find err identifier")
}

// TestIsErrorIdent_Empty covers the text == "" branch (nil node and
// zero-length fallback).
func TestIsErrorIdent_Empty(t *testing.T) {
	if isErrorIdent(nil, nil) {
		t.Error("nil node must not match")
	}
	// A type_identifier should not match (wrong node type).
	src := "package main\nvar _ Foo\ntype Foo int\n"
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "type_identifier") {
		if isErrorIdent(n, []byte(src)) {
			t.Errorf("type_identifier %q must not match error ident rule", nodeText(n, []byte(src)))
		}
	}
}

// TestIsErrorIdent_ShortName covers the len < 4 branch (identifiers
// too short to carry a trailing "Err" suffix).
func TestIsErrorIdent_ShortName(t *testing.T) {
	src := "package main\nfunc f() { var e int; _ = e }\n"
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "identifier") {
		if nodeText(n, []byte(src)) == "e" {
			if isErrorIdent(n, []byte(src)) {
				t.Errorf("short ident 'e' must not match error rule")
			}
			return
		}
	}
}

// TestIsErrNotNilIf_NilGuards covers the early-return branches for
// nil ifNode and missing condition. Ensures no panics on degenerate
// input.
func TestIsErrNotNilIf_NilGuards(t *testing.T) {
	if isErrNotNilIf(nil, nil) {
		t.Error("nil if node must not match")
	}
	// An if statement whose condition is a function call — not a
	// binary_expression. Must return false rather than panic.
	src := `package main
func f() {
	if check() {
		return
	}
}
func check() bool { return true }
`
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "if_statement") {
		if isErrNotNilIf(n, []byte(src)) {
			t.Error("if check() must not match err != nil rule")
		}
	}
}

// TestIsErrNotNilIf_WrongOperator verifies that an `if err == nil`
// form does not match — only `!=` is the error-return pattern.
func TestIsErrNotNilIf_WrongOperator(t *testing.T) {
	src := `package main
func f() {
	var err error
	if err == nil {
		return
	}
	_ = err
}
`
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "if_statement") {
		if isErrNotNilIf(n, []byte(src)) {
			t.Error("if err == nil must not match")
		}
	}
}

// TestIsErrNotNilIf_NonNilRight verifies that `if err != someOther`
// does not match — the right operand must be the nil literal.
func TestIsErrNotNilIf_NonNilRight(t *testing.T) {
	src := `package main
func f() {
	var err, other error
	if err != other {
		return
	}
	_ = err
	_ = other
}
`
	tree := parseGoInternal(t, src)
	for _, n := range findAll(tree.RootNode(), "if_statement") {
		if isErrNotNilIf(n, []byte(src)) {
			t.Error("err != other must not match")
		}
	}
}

// TestExtractErrorHandlingPatterns_NilRoot covers the early-return
// branch in extractErrorHandlingPatterns.
func TestExtractErrorHandlingPatterns_NilRoot(t *testing.T) {
	if recs := extractErrorHandlingPatterns(nil, nil, "x.go"); recs != nil {
		t.Errorf("nil root must return nil, got %v", recs)
	}
}
