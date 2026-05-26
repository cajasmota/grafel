// Package javascript — unit tests for issue #2338: gate const_destructure /
// const_destructure_call subtype emission behind ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL.
//
// Three invariants are tested:
//
//  1. Default-off: 0 entities with subtype const_destructure or const_destructure_call.
//  2. Opt-in (ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL=1): entities ARE emitted with
//     those subtypes.
//  3. Bindings-always: individual bindings (data, isLoading, createFoo, …) are
//     emitted regardless of the flag — the non-negotiable correctness invariant
//     that keeps the resolver working.
//
// Fixture summary for multiDestructureSrc (default-off probe results):
//
//	useFooQuery()  object-pattern  → opLift=false  → SCOPE.Component/const
//	useCreateFoo() object-pattern  → opLift=true   → SCOPE.Operation/const
//	useState(0)    array-pattern   → opLift=true (state hook) → SCOPE.Operation/const
//	                               setCount stays state_setter regardless of flag
package javascript_test

import (
	"testing"
)

// multiDestructureSrc exercises three destructure shapes.
// useState is treated as a mutation-style hook (opLift=true) by the extractor,
// so count / setCount are both SCOPE.Operation.
const multiDestructureSrc = `
const { data, isLoading, error } = useFooQuery();
const { mutate: createFoo, isError } = useCreateFoo();
const [count, setCount] = useState(0);
`

// ---------------------------------------------------------------------------
// 1. Default-off: no const_destructure* subtypes
// ---------------------------------------------------------------------------

// TestDestructureGate_DefaultOff verifies that with ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL
// unset, no entity has subtype "const_destructure" or "const_destructure_call".
func TestDestructureGate_DefaultOff(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "")

	src := []byte(multiDestructureSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	for _, e := range entities {
		if e.Subtype == "const_destructure" || e.Subtype == "const_destructure_call" {
			t.Errorf("default-off: entity %q has forbidden subtype %q; ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL must be set to emit these",
				e.Name, e.Subtype)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Opt-in: const_destructure* subtypes ARE emitted
// ---------------------------------------------------------------------------

// TestDestructureGate_OptIn verifies that with ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL=1
// at least one entity with subtype const_destructure or const_destructure_call
// is emitted.
func TestDestructureGate_OptIn(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "1")

	src := []byte(multiDestructureSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	found := false
	for _, e := range entities {
		if e.Subtype == "const_destructure" || e.Subtype == "const_destructure_call" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("opt-in: expected at least one const_destructure* entity; none found. entities: %v", entityNames(entities))
	}
}

// TestDestructureGate_OptIn_TrueValue verifies "true" is also accepted as
// a truthy flag value (mirrors the ARCHIGRAPH_MARKDOWN_EMIT_HEADINGS pattern).
func TestDestructureGate_OptIn_TrueValue(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "true")

	src := []byte(multiDestructureSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	found := false
	for _, e := range entities {
		if e.Subtype == "const_destructure" || e.Subtype == "const_destructure_call" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("opt-in (true): expected const_destructure* entities; none found. entities: %v", entityNames(entities))
	}
}

// ---------------------------------------------------------------------------
// 3. Bindings always preserved (correctness invariant)
// ---------------------------------------------------------------------------

// TestDestructureGate_BindingsAlways_DefaultOff asserts that the individual
// binding entities (data, isLoading, error, createFoo, count, setCount) are
// still emitted in default-off mode. The subtype changes (to "const" or
// "state_setter") but the entities themselves must exist so the resolver can
// bind same-file REFERENCES / CALLS edges.
func TestDestructureGate_BindingsAlways_DefaultOff(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "")

	src := []byte(multiDestructureSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// data, isLoading, error — from useFooQuery() (opLift=false → Component).
	for _, name := range []string{"data", "isLoading", "error"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("bindings-always: entity %q not found in default-off mode; names: %v",
				name, entityNames(entities))
			continue
		}
		if e.Kind != "SCOPE.Component" {
			t.Errorf("bindings-always: %q Kind=%q, want SCOPE.Component", name, e.Kind)
		}
	}

	// createFoo, isError — from useCreateFoo() (opLift=true → Operation).
	for _, name := range []string{"createFoo", "isError"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("bindings-always: entity %q not found in default-off mode; names: %v",
				name, entityNames(entities))
			continue
		}
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("bindings-always: %q Kind=%q, want SCOPE.Operation (mutation-hook kind must not degrade)", name, e.Kind)
		}
	}

	// count, setCount — from useState(0) which is also opLift=true (state hooks
	// are mutation-style), so both become SCOPE.Operation.
	for _, name := range []string{"count", "setCount"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("bindings-always: entity %q not found in default-off mode; names: %v",
				name, entityNames(entities))
		}
	}
}

// TestDestructureGate_BindingsAlways_OptIn asserts that with the flag on the
// same bindings are present and carry the expected const_destructure* subtypes.
func TestDestructureGate_BindingsAlways_OptIn(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "1")

	src := []byte(multiDestructureSrc)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Plain-hook bindings → const_destructure.
	for _, name := range []string{"data", "isLoading", "error"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("opt-in: entity %q not found; names: %v", name, entityNames(entities))
			continue
		}
		if e.Subtype != "const_destructure" {
			t.Errorf("opt-in: %q Subtype=%q, want const_destructure", name, e.Subtype)
		}
	}

	// Mutation-hook bindings → const_destructure_call.
	for _, name := range []string{"createFoo", "isError"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("opt-in: entity %q not found; names: %v", name, entityNames(entities))
			continue
		}
		if e.Subtype != "const_destructure_call" {
			t.Errorf("opt-in: %q Subtype=%q, want const_destructure_call", name, e.Subtype)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. Kind preservation for mutation-hook bindings (correctness guard)
// ---------------------------------------------------------------------------

// TestDestructureGate_MutationHookOp_DefaultOff verifies that a mutation-hook
// binding stays SCOPE.Operation in default-off mode. The kind must not degrade
// to SCOPE.Component; only the subtype label is suppressed.
func TestDestructureGate_MutationHookOp_DefaultOff(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "")

	src := []byte(`const { mutate: createFoo } = useCreateFoo();`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	e := findByName(entities, "createFoo")
	if e == nil {
		t.Fatalf("createFoo not found; names: %v", entityNames(entities))
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("createFoo default-off: Kind=%q, want SCOPE.Operation (mutation-hook kind must not degrade)", e.Kind)
	}
	if e.Subtype == "const_destructure_call" {
		t.Errorf("createFoo default-off: Subtype=%q, must not be const_destructure_call when flag is off", e.Subtype)
	}
}

// TestDestructureGate_MutationHookOp_OptIn verifies the same binding gets
// the const_destructure_call subtype when the flag is on.
func TestDestructureGate_MutationHookOp_OptIn(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "1")

	src := []byte(`const { mutate: createFoo } = useCreateFoo();`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	e := findByName(entities, "createFoo")
	if e == nil {
		t.Fatalf("createFoo not found; names: %v", entityNames(entities))
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("createFoo opt-in: Kind=%q, want SCOPE.Operation", e.Kind)
	}
	if e.Subtype != "const_destructure_call" {
		t.Errorf("createFoo opt-in: Subtype=%q, want const_destructure_call", e.Subtype)
	}
}

// ---------------------------------------------------------------------------
// 5. TypeScript variant — gate applies equally to TS files
// ---------------------------------------------------------------------------

// TestDestructureGate_TypeScript_DefaultOff verifies default-off behavior
// when the TypeScript grammar is used.
func TestDestructureGate_TypeScript_DefaultOff(t *testing.T) {
	t.Setenv("ARCHIGRAPH_EMIT_DESTRUCTURE_DETAIL", "")

	src := []byte(`const { data, isLoading }: QueryResult = useQuery<Data>("key");`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	for _, e := range entities {
		if e.Subtype == "const_destructure" || e.Subtype == "const_destructure_call" {
			t.Errorf("typescript default-off: entity %q has forbidden subtype %q", e.Name, e.Subtype)
		}
	}
}
