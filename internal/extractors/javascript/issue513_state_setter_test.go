// Package javascript — unit tests for issue #513: useState/useReducer/etc.
// setters are lifted as SCOPE.Operation with subtype="state_setter".
package javascript_test

import (
	"testing"
)

// TestStateSetterLift_useState — the setter element of a useState call
// (index 1 in the array pattern) must be emitted as SCOPE.Operation with
// subtype="state_setter", not land in bug-extractor as an unresolvable name.
func TestStateSetterLift_useState(t *testing.T) {
	src := []byte(`
const [isOpen, setIsOpen] = useState(false);
const [active, setActive] = useState(0);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// Both setters must be present as SCOPE.Operation.
	for _, setterName := range []string{"setIsOpen", "setActive"} {
		e := findByName(entities, setterName)
		if e == nil {
			t.Errorf("entity %q not found; all names: %v", setterName, entityNames(entities))
			continue
		}
		if e.Kind != "SCOPE.Operation" {
			t.Errorf("%q: kind=%q, want SCOPE.Operation", setterName, e.Kind)
		}
		if e.Subtype != "state_setter" {
			t.Errorf("%q: subtype=%q, want state_setter", setterName, e.Subtype)
		}
	}

	// The value (index 0) keeps its regular subtype, not state_setter.
	for _, valName := range []string{"isOpen", "active"} {
		e := findByName(entities, valName)
		if e == nil {
			t.Errorf("value entity %q not found", valName)
			continue
		}
		if e.Subtype == "state_setter" {
			t.Errorf("%q (value element) should NOT have subtype state_setter", valName)
		}
	}
}

// TestStateSetterLift_useReducer — useReducer dispatch function (index 1)
// must be lifted as state_setter.
func TestStateSetterLift_useReducer(t *testing.T) {
	src := []byte(`
const [state, dispatch] = useReducer(reducer, initialState);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "dispatch")
	if e == nil {
		t.Fatal("dispatch entity not found")
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("dispatch: kind=%q, want SCOPE.Operation", e.Kind)
	}
	if e.Subtype != "state_setter" {
		t.Errorf("dispatch: subtype=%q, want state_setter", e.Subtype)
	}
}

// TestStateSetterLift_useTransition — useTransition returns [isPending, startTransition].
// startTransition (index 1) must be lifted as state_setter.
func TestStateSetterLift_useTransition(t *testing.T) {
	src := []byte(`
const [isPending, startTransition] = useTransition();
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "startTransition")
	if e == nil {
		t.Fatal("startTransition entity not found")
	}
	if e.Subtype != "state_setter" {
		t.Errorf("startTransition: subtype=%q, want state_setter", e.Subtype)
	}
}

// TestStateSetterLift_useOptimistic — useOptimistic returns [value, updateFn].
// The update function (index 1) must be lifted as state_setter.
func TestStateSetterLift_useOptimistic(t *testing.T) {
	src := []byte(`
const [optimisticCart, updateOptimisticCart] = useOptimistic(cart);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	e := findByName(entities, "updateOptimisticCart")
	if e == nil {
		t.Fatal("updateOptimisticCart entity not found")
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("updateOptimisticCart: kind=%q, want SCOPE.Operation", e.Kind)
	}
	if e.Subtype != "state_setter" {
		t.Errorf("updateOptimisticCart: subtype=%q, want state_setter", e.Subtype)
	}
}

// TestStateSetterLift_useActionState — new React 19 hook.
func TestStateSetterLift_useActionState(t *testing.T) {
	src := []byte(`
const [state, dispatch, isPending] = useActionState(action, initialState);
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	// dispatch (index 1) must be state_setter.
	e := findByName(entities, "dispatch")
	if e == nil {
		t.Fatal("dispatch entity not found")
	}
	if e.Subtype != "state_setter" {
		t.Errorf("dispatch: subtype=%q, want state_setter", e.Subtype)
	}
}

// TestStateSetterLift_nonStateHook — object-destructure from a non-state hook
// must NOT get state_setter subtype (regression guard).
func TestStateSetterLift_nonStateHook(t *testing.T) {
	src := []byte(`
const { data, isLoading } = useQuery({ queryKey: ['todos'] });
`)
	tree := parseTS(t, src)
	entities := extract(t, src, "typescript", tree)

	for _, name := range []string{"data", "isLoading"} {
		e := findByName(entities, name)
		if e != nil && e.Subtype == "state_setter" {
			t.Errorf("%q from useQuery should NOT have subtype state_setter", name)
		}
	}
}
