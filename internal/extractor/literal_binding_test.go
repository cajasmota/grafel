package extractor

import (
	"strings"
	"testing"
)

func TestLiteralBindingResolver_HappyPath(t *testing.T) {
	r := NewLiteralBindingResolver(nil)
	r.Bind("name", "handle_order")
	if lit, ok := r.Resolve("name"); !ok || lit != "handle_order" {
		t.Fatalf("Resolve(name) = %q,%v; want handle_order,true", lit, ok)
	}
}

func TestLiteralBindingResolver_LastWriteWins(t *testing.T) {
	r := NewLiteralBindingResolver(nil)
	r.Bind("cmd", "first")
	r.Bind("cmd", "second")
	if lit, _ := r.Resolve("cmd"); lit != "second" {
		t.Fatalf("last-write-wins: got %q want second", lit)
	}
}

func TestLiteralBindingResolver_TaintClears(t *testing.T) {
	r := NewLiteralBindingResolver(nil)
	r.Bind("m", "process")
	r.Taint("m") // x = foo() — non-literal reassignment
	if lit, ok := r.Resolve("m"); ok {
		t.Fatalf("after taint Resolve(m) = %q,true; want unresolved", lit)
	}
}

func TestLiteralBindingResolver_NoMatch(t *testing.T) {
	r := NewLiteralBindingResolver(nil)
	if _, ok := r.Resolve("never_bound"); ok {
		t.Fatal("Resolve of unbound name should be false")
	}
	// empty-name guards
	r.Bind("", "x")
	r.Taint("")
	if _, ok := r.Resolve(""); ok {
		t.Fatal("empty name must never resolve")
	}
	if r.Len() != 0 {
		t.Fatalf("Len = %d; want 0 (empty-name binds ignored)", r.Len())
	}
}

func TestLiteralBindingResolver_ScopeReset(t *testing.T) {
	r := NewLiteralBindingResolver(nil)
	r.Bind("a", "x")
	r.Bind("b", "y")
	if r.Len() != 2 {
		t.Fatalf("Len = %d; want 2", r.Len())
	}
	r.Reset()
	if r.Len() != 0 {
		t.Fatalf("after Reset Len = %d; want 0", r.Len())
	}
	if _, ok := r.Resolve("a"); ok {
		t.Fatal("binding survived Reset")
	}
}

func TestLiteralBindingResolver_CaseFoldKeyFn(t *testing.T) {
	// COBOL-style case-insensitive names.
	r := NewLiteralBindingResolver(strings.ToUpper)
	r.Bind("ws-prog", "TAXCALC")
	if lit, ok := r.Resolve("WS-PROG"); !ok || lit != "TAXCALC" {
		t.Fatalf("case-fold Resolve = %q,%v; want TAXCALC,true", lit, ok)
	}
	r.Taint("Ws-Prog")
	if _, ok := r.Resolve("WS-PROG"); ok {
		t.Fatal("case-fold taint did not clear")
	}
}
