// Package javascript — unit tests for issue #1748: non-addressable local
// const bindings inside function/component bodies are tagged with
// Properties["local_scope"]="true" so the serving layer (denoise.go) can
// hide them from archigraph_find results while keeping them emitted for
// resolver use (REFERENCES/CALLS binding still works).
package javascript_test

import (
	"testing"
)

// --------------------------------------------------------------------------
// Tests — local_scope tagging
// --------------------------------------------------------------------------

// TestLocalScope_PlainDestructureInFunction — `const { counts } = someData`
// inside a component body must be tagged local_scope=true.
func TestLocalScope_PlainDestructureInFunction(t *testing.T) {
	src := []byte(`
function ContractProposals() {
  const { counts } = someData;
  return counts;
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// counts IS emitted (resolver may need it) but flagged as local_scope.
	e := findByName(entities, "counts")
	if e == nil {
		t.Fatalf("counts entity not found; names: %v", entityNames(entities))
	}
	if e.Properties["local_scope"] != "true" {
		t.Errorf("counts should have local_scope=true; got Properties=%v", e.Properties)
	}

	// The enclosing component must NOT be local_scope.
	comp := findByName(entities, "ContractProposals")
	if comp != nil && comp.Properties["local_scope"] == "true" {
		t.Errorf("ContractProposals should NOT have local_scope=true")
	}
}

// TestLocalScope_ArrayDestructureNonHook — `const [a, b] = arr` inside a
// function (non-hook RHS) must be tagged local_scope=true.
func TestLocalScope_ArrayDestructureNonHook(t *testing.T) {
	src := []byte(`
function Cmp() {
  const [a, b] = arr;
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	for _, name := range []string{"a", "b"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("entity %q not found; names: %v", name, entityNames(entities))
			continue
		}
		if e.Properties["local_scope"] != "true" {
			t.Errorf("%q: want local_scope=true; got Properties=%v", name, e.Properties)
		}
	}
}

// TestLocalScope_HookDestructureKept — hook-result destructures (mutation
// hooks, state hooks) inside a function body must NOT be tagged local_scope.
// The resolver depends on these entities for CALLS binding.
func TestLocalScope_HookDestructureKept(t *testing.T) {
	src := []byte(`
function Cmp() {
  const [isOpen, setIsOpen] = useState(false);
  const { mutate: createItem } = useCreateItem();
  const { data, isLoading } = useQuery({ queryKey: ["x"] });
}
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// State hook results — kept without local_scope.
	for _, name := range []string{"isOpen", "setIsOpen"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("hook entity %q not found", name)
			continue
		}
		if e.Properties["local_scope"] == "true" {
			t.Errorf("%q (state hook result): should NOT have local_scope=true", name)
		}
	}

	// Mutation hook — kept.
	e := findByName(entities, "createItem")
	if e == nil {
		t.Errorf("createItem (mutation hook) not found")
	} else if e.Properties["local_scope"] == "true" {
		t.Errorf("createItem (mutation hook): should NOT have local_scope=true")
	}

	// useQuery is in the mutation-style list — kept.
	for _, name := range []string{"data", "isLoading"} {
		e := findByName(entities, name)
		if e == nil {
			t.Errorf("query hook entity %q not found", name)
			continue
		}
		if e.Properties["local_scope"] == "true" {
			t.Errorf("%q (query hook): should NOT have local_scope=true", name)
		}
	}
}

// TestLocalScope_ModuleScopeNotTagged — destructures at module scope
// (top-level) must NOT be tagged local_scope, regardless of whether the RHS
// is a hook call.
func TestLocalScope_ModuleScopeNotTagged(t *testing.T) {
	src := []byte(`
const { foo, bar } = somethingArbitrary();
const [x, y] = arr;
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	for _, name := range []string{"foo", "bar", "x", "y"} {
		e := findByName(entities, name)
		if e == nil {
			// Not emitted at all — fine (may be filtered by existing logic).
			continue
		}
		if e.Properties["local_scope"] == "true" {
			t.Errorf("%q at module scope: should NOT have local_scope=true", name)
		}
	}
}

// TestLocalScope_NestedArrowNotTagged — a nested arrow const inside a
// component body (`const handleClick = () => {...}`) is a real callable
// entity and must NOT be tagged local_scope.
func TestLocalScope_NestedArrowNotTagged(t *testing.T) {
	src := []byte(`
function ContractProposals() {
  const handleClick = () => {
    doSomething();
  };
  return handleClick;
}
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	e := findByName(entities, "handleClick")
	if e == nil {
		t.Fatalf("handleClick not found; names: %v", entityNames(entities))
	}
	if e.Properties["local_scope"] == "true" {
		t.Errorf("handleClick (nested arrow const): should NOT have local_scope=true (it IS callable)")
	}
}

// TestLocalScope_ArrowComponentTopLevel — a top-level arrow component must
// NOT be tagged local_scope; but non-hook destructures inside its body must be.
func TestLocalScope_ArrowComponentTopLevel(t *testing.T) {
	src := []byte(`
const MyComponent = () => {
  const { counts } = someData;
  return counts;
};
`)
	tree := parseJS(t, src)
	entities := extract(t, src, "javascript", tree)

	// Top-level component — not local.
	comp := findByName(entities, "MyComponent")
	if comp == nil {
		t.Fatalf("MyComponent not found; names: %v", entityNames(entities))
	}
	if comp.Properties["local_scope"] == "true" {
		t.Errorf("MyComponent top-level arrow: should NOT have local_scope=true")
	}

	// counts INSIDE the arrow body — must be tagged.
	e := findByName(entities, "counts")
	if e == nil {
		t.Fatalf("counts not found inside arrow body; names: %v", entityNames(entities))
	}
	if e.Properties["local_scope"] != "true" {
		t.Errorf("counts inside arrow body: want local_scope=true; got Properties=%v", e.Properties)
	}
}
