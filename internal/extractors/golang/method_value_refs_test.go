// method_value_refs_test.go — fixture tests for issue #1789.
//
// Covers CALLS via_value=true emission for method-value references: the
// `s.wrap("name", s.handler)` registration pattern and similar shapes where
// a method is used as a VALUE (passed as an argument, stored in a variable,
// etc.) rather than being directly invoked.
//
// These tests verify:
//  1. s.wrap("name", s.Method) argument → CALLS via_value=true, receiver_type set.
//  2. var h = obj.Method (var decl) → CALLS via_value=true.
//  3. register("foo", obj.Method) generic arg → CALLS via_value=true.
//  4. obj.Method() direct call → existing CALLS behaviour UNCHANGED (no via_value).
//  5. Self-receiver method value does NOT produce a self-loop when target == caller.
package golang

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runMethodValueExtract runs the Go extractor on src with the given path and
// returns the entity slice.
func runMethodValueExtract(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ex := &GoExtractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findCallsEdgeWithViaValue looks in e.Relationships for a CALLS edge whose
// ToID contains toSubstr and whose Properties["via_value"] == "true".
func findCallsEdgeWithViaValue(e *types.EntityRecord, toSubstr string) *types.RelationshipRecord {
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind != "CALLS" {
			continue
		}
		if !strings.Contains(r.ToID, toSubstr) {
			continue
		}
		if r.Properties["via_value"] == "true" {
			return r
		}
	}
	return nil
}

// findCallsEdge looks in e.Relationships for any CALLS edge whose ToID
// contains toSubstr (ignoring via_value).
func findCallsEdge(e *types.EntityRecord, toSubstr string) *types.RelationshipRecord {
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == "CALLS" && strings.Contains(r.ToID, toSubstr) {
			return r
		}
	}
	return nil
}

// methodValueSummary returns a compact string of all CALLS edges on ents,
// for use in test failure messages.
func methodValueSummary(ents []types.EntityRecord) string {
	var b strings.Builder
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				b.WriteString(e.Name)
				b.WriteString(" -[CALLS")
				if r.Properties["via_value"] == "true" {
					b.WriteString(",via_value")
				}
				if rt := r.Properties["receiver_type"]; rt != "" {
					b.WriteString(",recv=")
					b.WriteString(rt)
				}
				b.WriteString("]-> ")
				b.WriteString(r.ToID)
				b.WriteString("; ")
			}
		}
	}
	if b.Len() == 0 {
		return "(no CALLS)"
	}
	return b.String()
}

// findEntityByName returns a pointer to the first entity matching name.
func findEntityByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// ---- Test 1: s.wrap("name", s.Method) registration pattern -----------------
//
// This is the concrete dogfood case from issue #1789:
//   s.wrap("archigraph_find", s.handleQueryGraph)
// The inner s.handleQueryGraph is a method-value reference; it should emit
// a CALLS edge with via_value=true and receiver_type=Server so the resolver
// can bind the bare name "handleQueryGraph" to the entity Server.handleQueryGraph.

func TestMethodValueRef_RegistrationPattern(t *testing.T) {
	src := `package demo

type Server struct{}

func (s *Server) handleQueryGraph() {}

func (s *Server) wrap(name string, fn func()) {}

func (s *Server) registerTools() {
	s.wrap("archigraph_find", s.handleQueryGraph)
}
`
	ents := runMethodValueExtract(t, src, "server.go")
	caller := findEntityByName(ents, "Server.registerTools")
	if caller == nil {
		t.Fatal("Server.registerTools entity not found")
	}
	hit := findCallsEdgeWithViaValue(caller, "handleQueryGraph")
	if hit == nil {
		t.Fatalf("expected CALLS via_value=true from Server.registerTools -> handleQueryGraph; all edges: %s",
			methodValueSummary(ents))
	}
	if hit.Properties["receiver_type"] != "Server" {
		t.Errorf("expected receiver_type=Server on via_value CALLS edge, got %+v", hit.Properties)
	}
}

// ---- Test 2: var handler = obj.Method (variable-bound method value) ---------
//
// `var h = obj.Method` in a function body. The method is stored as a
// function value; emit CALLS via_value=true from the enclosing function.

func TestMethodValueRef_VarDecl(t *testing.T) {
	src := `package demo

type Widget struct{}

func (w *Widget) Process() {}

func setup(obj *Widget) {
	var h = obj.Process
	_ = h
}
`
	ents := runMethodValueExtract(t, src, "widget.go")
	caller := findEntityByName(ents, "setup")
	if caller == nil {
		t.Fatal("setup function not found")
	}
	hit := findCallsEdgeWithViaValue(caller, "Process")
	if hit == nil {
		t.Fatalf("expected CALLS via_value=true from setup -> Process; all edges: %s",
			methodValueSummary(ents))
	}
}

// ---- Test 3: register("foo", obj.Method) generic argument ------------------
//
// `register("foo", obj.Method)` — the method is the second argument to a
// plain function call (not a method call on the same receiver). Should emit
// CALLS via_value=true from the enclosing function.

func TestMethodValueRef_GenericFuncArg(t *testing.T) {
	src := `package demo

type Handler struct{}

func (h *Handler) ServeHTTP() {}

func register(name string, fn func()) {}

func setupRoutes(h *Handler) {
	register("foo", h.ServeHTTP)
}
`
	ents := runMethodValueExtract(t, src, "routes.go")
	caller := findEntityByName(ents, "setupRoutes")
	if caller == nil {
		t.Fatal("setupRoutes function not found")
	}
	hit := findCallsEdgeWithViaValue(caller, "ServeHTTP")
	if hit == nil {
		t.Fatalf("expected CALLS via_value=true from setupRoutes -> ServeHTTP; all edges: %s",
			methodValueSummary(ents))
	}
}

// ---- Test 4: obj.Method() direct invocation — NO via_value -----------------
//
// Existing CALLS behaviour must be UNCHANGED: `obj.Method()` emits a CALLS
// edge WITHOUT via_value=true. This test confirms no regression.

func TestMethodValueRef_DirectCallNoViaValue(t *testing.T) {
	src := `package demo

type Foo struct{}

func (f *Foo) Bar() {}

func driver(x *Foo) {
	x.Bar()
}
`
	ents := runMethodValueExtract(t, src, "foo.go")
	caller := findEntityByName(ents, "driver")
	if caller == nil {
		t.Fatal("driver function not found")
	}
	// A normal CALLS edge must exist.
	direct := findCallsEdge(caller, "Bar")
	if direct == nil {
		t.Fatalf("expected CALLS edge from driver -> Bar; all edges: %s",
			methodValueSummary(ents))
	}
	// It must NOT carry via_value=true (the direct-invocation case).
	if direct.Properties["via_value"] == "true" {
		t.Errorf("direct invocation x.Bar() should NOT carry via_value=true, got %+v", direct.Properties)
	}
}

// ---- Test 5: method value passed via self-receiver (same struct) -----------
//
// `s.wrap("name", s.handleQueryGraph)` — multiple method-value arguments in
// the same call. All must produce separate CALLS via_value=true edges.

func TestMethodValueRef_MultipleMethodValues(t *testing.T) {
	src := `package demo

type Server struct{}

func (s *Server) handleA() {}
func (s *Server) handleB() {}
func (s *Server) wrap(name string, fn func()) {}

func (s *Server) registerAll() {
	s.wrap("a", s.handleA)
	s.wrap("b", s.handleB)
}
`
	ents := runMethodValueExtract(t, src, "srv.go")
	caller := findEntityByName(ents, "Server.registerAll")
	if caller == nil {
		t.Fatal("Server.registerAll entity not found")
	}
	for _, want := range []string{"handleA", "handleB"} {
		hit := findCallsEdgeWithViaValue(caller, want)
		if hit == nil {
			t.Errorf("expected CALLS via_value=true from Server.registerAll -> %s; all edges: %s",
				want, methodValueSummary(ents))
		}
	}
}

// ---- Test 6: no self-loop when method value references enclosing method -----
//
// `func (s *Server) setup() { s.wrap("x", s.setup) }` — the method value
// IS the enclosing method itself. Must NOT produce a via_value self-edge.

func TestMethodValueRef_NoSelfLoop(t *testing.T) {
	src := `package demo

type Server struct{}

func (s *Server) wrap(name string, fn func()) {}

func (s *Server) setup() {
	// Pathological: passing self as handler. Should not produce a self-loop.
	s.wrap("self", s.setup)
}
`
	ents := runMethodValueExtract(t, src, "srv.go")
	caller := findEntityByName(ents, "Server.setup")
	if caller == nil {
		t.Fatal("Server.setup entity not found")
	}
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && strings.Contains(r.ToID, "setup") && r.Properties["via_value"] == "true" {
			t.Errorf("self-loop via_value edge leaked: %+v", r)
		}
	}
}
