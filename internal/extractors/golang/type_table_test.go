// type_table_test.go — coverage for the local-var type-chain
// resolution path added in #1840.
//
// These tests assert that REFERENCES edges are emitted for selector
// expressions whose LHS is a local-scope name typed as a project
// struct, not just the receiver-method / PascalCase shapes the
// references pass already handled. Each test exercises exactly one
// of the three v1 lightweight scope cases enumerated in #1840:
//
//   - Parameter typed as a project struct.
//   - Short-var-decl with composite literal RHS.
//   - var-spec with explicit type.
//
// Cases NOT covered here (and intentionally so — they are v2):
//   - Cross-file struct types (the symbol table is per-file).
//   - Return-type chains across user-defined functions.
//   - Interface dispatch and embedded type promotion.

package golang

import (
	"testing"
)

// Parameter typed as a same-file struct. `entry.ToID` inside the
// function body should resolve to the struct's ToID field via the
// local-var type chain.
func TestGoRefsStructFieldViaParameter(t *testing.T) {
	src := `package demo

type EntityRecord struct {
	ToID string
	Name string
}

func handle(entry EntityRecord) string {
	return entry.ToID
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "handle", "EntityRecord") {
		t.Fatalf("expected handle to REFERENCES EntityRecord (parameter type-chain); got %s",
			goRelsSummary(ents))
	}
}

// Method receiver typed as a same-file struct. `s.Name` inside the
// method body should resolve via the receiver path (already worked
// pre-#1840), but the receiver is also entered into varTypes, so the
// local-var path should NOT double-emit. Verifies the dedup guard.
func TestGoRefsReceiverPathStillResolves(t *testing.T) {
	src := `package demo

type Server struct {
	Name string
}

func (s *Server) describe() string {
	return s.Name
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "Server.describe", "Server") {
		t.Fatalf("expected Server.describe to REFERENCES Server (receiver path); got %s",
			goRelsSummary(ents))
	}
	// Count Server-receiver edges — must be exactly one (no double-emit).
	count := 0
	for _, e := range ents {
		if e.Name != "Server.describe" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one REFERENCES edge on Server.describe, got %d (%s)",
			count, goRelsSummary(ents))
	}
}

// Short-var-decl with composite literal. `x := EntityRecord{}` types
// x, and `x.ToID` resolves through the var-type table.
func TestGoRefsStructFieldViaShortVarDecl(t *testing.T) {
	src := `package demo

type EntityRecord struct {
	ToID string
}

func handle() string {
	x := EntityRecord{ToID: "abc"}
	return x.ToID
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "handle", "EntityRecord") {
		t.Fatalf("expected handle to REFERENCES EntityRecord (short-var-decl chain); got %s",
			goRelsSummary(ents))
	}
}

// Short-var-decl with pointer composite literal. `x := &EntityRecord{}`
// canonicalises to type `EntityRecord` (pointer stripped); same lookup
// path works as the value-receiver case.
func TestGoRefsStructFieldViaPointerShortVarDecl(t *testing.T) {
	src := `package demo

type EntityRecord struct {
	ToID string
}

func handle() string {
	x := &EntityRecord{ToID: "abc"}
	return x.ToID
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "handle", "EntityRecord") {
		t.Fatalf("expected handle to REFERENCES EntityRecord (pointer short-var-decl); got %s",
			goRelsSummary(ents))
	}
}

// var-spec with explicit type. `var x EntityRecord` types x via the
// type field, and `x.ToID` resolves through the var-type table.
func TestGoRefsStructFieldViaVarSpec(t *testing.T) {
	src := `package demo

type EntityRecord struct {
	ToID string
}

func handle() string {
	var x EntityRecord
	return x.ToID
}
`
	ents := runGoExtract(t, src)
	if !hasGoRef(ents, "handle", "EntityRecord") {
		t.Fatalf("expected handle to REFERENCES EntityRecord (var-spec); got %s",
			goRelsSummary(ents))
	}
}

// The via_receiver_type Properties stamp is emitted on edges resolved
// through the local-var type chain. Lets audits attribute rescued
// edges to this path vs. the older receiver / PascalCase paths.
func TestGoRefsLocalVarChainStampsViaReceiverType(t *testing.T) {
	src := `package demo

type EntityRecord struct {
	ToID string
}

func handle(entry EntityRecord) string {
	return entry.ToID
}
`
	ents := runGoExtract(t, src)
	found := false
	for _, e := range ents {
		if e.Name != "handle" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.Properties != nil && r.Properties["via_receiver_type"] == "EntityRecord" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected handle REFERENCES edge with via_receiver_type=EntityRecord; got %s",
			goRelsSummary(ents))
	}
}

// Graceful fallback: an unknown LHS type produces no edge and no
// crash. `entry` here is typed as an unrecognised type — the lookup
// misses and emission is skipped silently.
func TestGoRefsUnknownLHSTypeIsGraceful(t *testing.T) {
	src := `package demo

func handle(entry interface{}) {
	_ = entry
}
`
	ents := runGoExtract(t, src)
	// No panic, no edge — just confirm we got entities back.
	if len(ents) == 0 {
		t.Fatalf("expected entities from runGoExtract even for unbindable type")
	}
}

// Conflicting types on the same name across scopes are dropped by
// mergeVarTypes, so neither lookup binds. Verifies the conservative
// "drop on conflict" rule survives the wiring.
func TestGoRefsAmbiguousNameDropsBinding(t *testing.T) {
	src := `package demo

type A struct {
	F string
}
type B struct {
	F string
}

func handle() {
	x := A{}
	_ = x
	{
		x := B{}
		_ = x.F
	}
}
`
	ents := runGoExtract(t, src)
	// We do not assert on absence of an A/B edge — collectBodyVarTypes
	// flattens block scopes and drops the conflicting binding. What we
	// assert is that extraction completes without crashing AND that we
	// don't emit a mis-bound A.F edge (the inner reference is to B.F).
	for _, e := range ents {
		if e.Name != "handle" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.Properties != nil && r.Properties["via_receiver_type"] == "A" {
				t.Fatalf("ambiguous binding produced A-typed edge (should have been dropped): %s",
					goRelsSummary(ents))
			}
		}
	}
}
