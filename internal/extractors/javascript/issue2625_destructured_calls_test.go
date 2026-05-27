// Package javascript_test — issue #2625: CALLS edges via destructured bindings.
//
// Verifies that the extractor detects destructured hook/store/query calls and
// emits CALLS edges when the locally-bound name is subsequently called.
//
// Four test cases:
//   - Form 1: useXxxStore() selector destructuring (most common React/Zustand form)
//   - Form 2: useSyncQueueStore.getState() destructuring
//   - Form 3: useMutation / useQuery result destructuring (React Query)
//   - Negative: const { foo } = somethingRandom() → no spurious edge
package javascript_test

import (
	"testing"
)

// TestTSExtractor_DestructuredZustandSelector_EmitsCallsEdge verifies Form 1:
//
//	const { softLogout, login } = useAuthStore();
//	const handler = () => { softLogout(); };
//
// Must emit CALLS handler → softLogout with Properties["via"] = "zustand_store".
// Issue #2625: real failure — archigraph_neighbors(handleLogout) returned 0 callees.
func TestTSExtractor_DestructuredZustandSelector_EmitsCallsEdge(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set, get) => ({
  token: null,
  softLogout: () => set({ token: null }),
  login: async (credentials) => { /* ... */ },
}));

export const handleLogout = () => {
  const { softLogout } = useAuthStore();
  softLogout();
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleLogout")
	if e == nil {
		t.Fatal("expected entity 'handleLogout' to be emitted")
	}

	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "softLogout" {
			if r.Properties != nil && r.Properties["via"] == "zustand_store" {
				found = true
				break
			}
			t.Logf("CALLS handleLogout→softLogout found but via=%q (want zustand_store); props=%v",
				r.Properties["via"], r.Properties)
		}
	}
	if !found {
		t.Logf("handleLogout relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleLogout→softLogout with via=zustand_store; not found")
	}
}

// TestTSExtractor_DestructuredGetState_EmitsCallsEdge verifies Form 2:
//
//	const { markFailed } = useSyncQueueStore.getState();
//	markFailed();
//
// Must emit CALLS caller → markFailed with Properties["via"] = "zustand_store".
// Issue #2625.
func TestTSExtractor_DestructuredGetState_EmitsCallsEdge(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useSyncQueueStore = create((set, get) => ({
  queue: [],
  markFailed: (id, msg) => set(state => state),
  markCompleted: (id) => set(state => state),
}));

export async function processItem(id) {
  const { markFailed, markCompleted } = useSyncQueueStore.getState();
  try {
    markCompleted(id);
  } catch (e) {
    markFailed(id, e.message);
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "processItem")
	if e == nil {
		t.Fatal("expected entity 'processItem' to be emitted")
	}

	for _, action := range []string{"markFailed", "markCompleted"} {
		found := false
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == action &&
				r.Properties != nil && r.Properties["via"] == "zustand_store" {
				found = true
				break
			}
		}
		if !found {
			t.Logf("processItem relationships:")
			for _, r := range e.Relationships {
				t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
			}
			t.Errorf("expected CALLS processItem→%s with via=zustand_store; not found", action)
		}
	}
}

// TestTSExtractor_DestructuredReactQueryMutate_EmitsCallsEdge verifies Form 3:
//
//	const { mutate } = useMutation({ mutationFn: ... });
//	mutate(payload);
//
// Must emit CALLS caller → mutate with Properties["via"] = "react_query".
// Issue #2625.
func TestTSExtractor_DestructuredReactQueryMutate_EmitsCallsEdge(t *testing.T) {
	src := `
import { useMutation } from '@tanstack/react-query';

export function useCreateUser() {
  const { mutate, isLoading } = useMutation({
    mutationFn: async (data) => fetch('/api/users', { method: 'POST', body: JSON.stringify(data) }),
  });

  const handleSubmit = (formData) => {
    mutate(formData);
  };

  return { handleSubmit, isLoading };
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "useCreateUser")
	if e == nil {
		t.Fatal("expected entity 'useCreateUser' to be emitted")
	}

	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "mutate" {
			if r.Properties != nil && r.Properties["via"] == "react_query" {
				found = true
				break
			}
			t.Logf("CALLS useCreateUser→mutate found but via=%q (want react_query); props=%v",
				r.Properties["via"], r.Properties)
		}
	}
	if !found {
		t.Logf("useCreateUser relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS useCreateUser→mutate with via=react_query; not found")
	}
}

// TestTSExtractor_NoSpuriousEdgeWhenBindingUnresolvable verifies the negative case:
// a destructuring whose RHS is an unrecognised call must NOT produce a
// destructured_binding CALLS edge. It may produce a bare CALLS edge to the
// callee function itself, but not to the destructured property names.
// Issue #2625 — guard against over-emission.
func TestTSExtractor_NoSpuriousEdgeWhenBindingUnresolvable(t *testing.T) {
	src := `
function somethingRandom() {
  return { foo: 1, bar: 2 };
}

function caller() {
  const { foo } = somethingRandom();
  // foo is a plain number, not a function — no edge should be emitted
  return foo + 1;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "caller")
	if e == nil {
		t.Fatal("expected entity 'caller' to be emitted")
	}

	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "foo" &&
			r.Properties != nil && r.Properties["via"] == "destructured_binding" {
			t.Errorf("unexpected destructured_binding CALLS caller→foo; 'foo' is a plain value from an unrecognised RHS")
		}
	}
}
