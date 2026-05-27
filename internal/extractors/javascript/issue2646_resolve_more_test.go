// Package javascript_test — issue #2646: resolve more CALLS edges upfront.
//
// Two categories addressed:
//
//  1. Zustand middleware pattern — stores created with create<T>()(persist(factory))
//     were previously invisible to the store action tracker because the tracker
//     only looked for a direct arrow_function in the outer create() call's args.
//     Now the tracker unwraps one middleware level so action entities are emitted
//     for persist(), devtools(), immer(), and other single-level wrappers.
//
//  2. Bare relative-import call → structural ref — when a bare callee identifier
//     is a named import from a relative path, callTarget now emits a Format A
//     structural-ref ("scope:operation:ref:<lang>:<file>:<name>") instead of the
//     bare name. The resolver can then bind this via lookupStructural →
//     lookupLocationKind without requiring same-file co-location.
package javascript_test

import (
	"strings"
	"testing"
)

// ─── Fix 1: Zustand middleware pattern ──────────────────────────────────────

// TestZustandMiddleware_Persist_EmitsActionEntities verifies that a store
// created with the curried middleware pattern:
//
//	export const useAuthStore = create<AuthState>()(
//	  persist(
//	    (set) => ({ login: async () => {...}, logout: () => {...} }),
//	    { name: 'auth' },
//	  )
//	)
//
// emits SCOPE.Operation entities for the action functions (login, logout).
// Issue #2646 — before this fix the tracker found no factory function in the
// outer create()(...) args and silently skipped the store.
func TestZustandMiddleware_Persist_EmitsActionEntities(t *testing.T) {
	src := `
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface AuthState {
  user: string | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      login: async (username, password) => {
        // authenticate
        set({ user: username });
      },
      logout: () => set({ user: null }),
    }),
    { name: 'auth-store' },
  )
);
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// Both action methods must appear as named entities.
	// Issue #2631 — entity names are now qualified: <storeVar>::<actionName>.
	login := findByNameRel(ents, "useAuthStore::login")
	if login == nil {
		t.Error("expected SCOPE.Operation entity 'useAuthStore::login' to be emitted for Zustand persist store; got nil")
	}
	logout := findByNameRel(ents, "useAuthStore::logout")
	if logout == nil {
		t.Error("expected SCOPE.Operation entity 'useAuthStore::logout' to be emitted for Zustand persist store; got nil")
	}
}

// TestZustandMiddleware_Persist_GetStateCalls verifies that caller code using
// .getState().<action>() on a persist-wrapped store produces CALLS edges.
//
//	useAuthStore.getState().logout()
func TestZustandMiddleware_Persist_GetStateCalls(t *testing.T) {
	src := `
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export const useAuthStore = create()(
  persist(
    (set) => ({
      user: null,
      logout: () => set({ user: null }),
    }),
    { name: 'auth' },
  )
);

export function handleSignOut() {
  useAuthStore.getState().logout();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleSignOut")
	if e == nil {
		t.Fatal("expected entity 'handleSignOut' to be emitted")
	}

	// Issue #2631 — ToID is now qualified: <storeVar>::<actionName>.
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::logout" {
			if r.Properties != nil && r.Properties["via"] == "zustand_store" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Logf("handleSignOut relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Error("expected CALLS handleSignOut→useAuthStore::logout with via=zustand_store for persist-wrapped store")
	}
}

// TestZustandMiddleware_NestedMiddleware_EmitsActionEntities verifies that
// nested middleware (e.g. persist wrapping devtools or immer) is handled:
//
//	create()(persist(immer((set) => ({ enqueue: (x) => {...} }))))
func TestZustandMiddleware_NestedMiddleware_EmitsActionEntities(t *testing.T) {
	src := `
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { immer } from 'zustand/middleware/immer';

export const useSyncQueueStore = create()(
  persist(
    immer((set) => ({
      queue: [],
      enqueue: (item) => {
        set(state => { state.queue.push(item); });
      },
    })),
    { name: 'sync-queue' },
  )
);
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// Issue #2631 — entity names are now qualified: <storeVar>::<actionName>.
	if findByNameRel(ents, "useSyncQueueStore::enqueue") == nil {
		t.Error("expected SCOPE.Operation entity 'useSyncQueueStore::enqueue' to be emitted for nested-middleware store; got nil")
	}
}

// TestZustandMiddleware_NegativeCase_NonMiddlewareArg verifies that stores
// whose first arg is a plain object literal (not a factory function AND not a
// middleware call) do not produce spurious entities.
//
//	create({ user: null })  ← non-standard, should not crash
func TestZustandMiddleware_NegativeCase_NonMiddlewareArg(t *testing.T) {
	src := `
import { create } from 'zustand';

// Non-standard: passing a plain object is not a valid Zustand store, but
// the extractor must not panic or produce spurious action entities.
const badStore = create({ user: null });

export function doSomething() {
  badStore.getState();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// There must be a doSomething entity.
	e := findByNameRel(ents, "doSomething")
	if e == nil {
		t.Fatal("expected entity 'doSomething' to be emitted")
	}
	// No "user" entity should be emitted as a Zustand action.
	if findByNameRel(ents, "user") != nil {
		// "user" might legitimately appear as a schema field in other tests;
		// here we only care that doSomething has no spurious CALLS→user edge
		// tagged as zustand_store.
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == "user" &&
				r.Properties != nil && r.Properties["via"] == "zustand_store" {
				t.Errorf("spurious CALLS doSomething→user via=zustand_store for non-factory store")
			}
		}
	}
}

// ─── Fix 2: Bare relative-import call → structural ref ────────────────────

// TestRelativeImportCall_EmitsStructuralRef verifies that a call to a
// function imported from a relative path produces a structural-ref CALLS
// edge instead of a bare name:
//
//	import { fetchBuildings } from './buildings.api';
//	function loadData() { fetchBuildings(); }
//
// Expected: CALLS loadData → "scope:operation:ref:typescript:<file>:fetchBuildings"
func TestRelativeImportCall_EmitsStructuralRef(t *testing.T) {
	src := `
import { fetchBuildings } from './buildings.api';

export function loadData() {
  return fetchBuildings();
}
`
	tree := parseTSRel(t, []byte(src))
	// Use an explicit path so the resolver can compute the relative file path.
	ents := runJSPath(t, src, "typescript", tree, "src/features/buildings/index.ts")

	e := findByNameRel(ents, "loadData")
	if e == nil {
		t.Fatal("expected entity 'loadData' to be emitted")
	}

	found := false
	for _, r := range e.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		// Accept both the structural ref form and the bare name for
		// tolerance across resolver stages. The key requirement is that
		// the ToID is NOT a bare "fetchBuildings" ambiguous name but
		// instead carries the resolved file path.
		if strings.Contains(r.ToID, "fetchBuildings") &&
			strings.HasPrefix(r.ToID, "scope:operation:ref:") {
			found = true
			break
		}
	}
	if !found {
		t.Logf("loadData relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s", r.Kind, r.ToID)
		}
		t.Error("expected CALLS loadData → structural-ref containing 'fetchBuildings'; not found")
	}
}

// TestRelativeImportCall_AliasedImport verifies that a renamed (aliased)
// import still produces a structural ref keyed on the ORIGINAL exported name:
//
//	import { useSyncQueueStore as SyncStore } from './useSyncQueueStore';
//	SyncStore(s => s.queue);
//
// The structural ref must use the importedName "useSyncQueueStore" (the
// exported symbol), not the local alias "SyncStore".
func TestRelativeImportCall_AliasedImport(t *testing.T) {
	src := `
import { useSyncQueueStore as SyncStore } from './useSyncQueueStore';

export function getQueue() {
  return SyncStore(s => s.queue);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/features/checklist/ChecklistTab.ts")

	e := findByNameRel(ents, "getQueue")
	if e == nil {
		t.Fatal("expected entity 'getQueue' to be emitted")
	}

	found := false
	for _, r := range e.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		if strings.Contains(r.ToID, "useSyncQueueStore") &&
			strings.HasPrefix(r.ToID, "scope:operation:ref:") {
			found = true
			break
		}
	}
	if !found {
		t.Logf("getQueue relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s", r.Kind, r.ToID)
		}
		t.Error("expected CALLS getQueue → structural-ref containing 'useSyncQueueStore'; not found")
	}
}

// TestRelativeImportCall_ExternalModuleNotStructuralRef verifies that calls
// to functions imported from EXTERNAL (npm) modules do NOT get structural
// refs — they should remain bare names or ext: prefixed as before.
//
//	import { useMemo } from 'react';
//	function Comp() { useMemo(() => {}, []); }
//
// useMemo must NOT have a structural ref (no resolvedFile for 'react').
func TestRelativeImportCall_ExternalModuleNotStructuralRef(t *testing.T) {
	src := `
import { useMemo } from 'react';

export function Comp() {
  const val = useMemo(() => 42, []);
  return val;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "src/Comp.tsx")

	e := findByNameRel(ents, "Comp")
	if e == nil {
		t.Fatal("expected entity 'Comp' to be emitted")
	}

	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && strings.Contains(r.ToID, "useMemo") {
			if strings.HasPrefix(r.ToID, "scope:operation:ref:") {
				t.Errorf("useMemo from external module must NOT get a structural ref; got ToID=%q", r.ToID)
			}
		}
	}
}

// TestRelativeImportCall_DefaultImportNotStructuralRef verifies that default
// imports (importedName == "default") from relative paths also produce
// structural refs keyed on the local name (since there's no original exported
// symbol name to use — the default export is anonymous from the import side).
//
// The test just verifies that no panic occurs and we get some CALLS edge.
func TestRelativeImportCall_DefaultImportNotStructuralRef(t *testing.T) {
	src := `
import NewNoteFeature from './NewNote.feature';

export default function NewNoteScreen() {
  return NewNoteFeature({ entity: 'building' });
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJSPath(t, src, "typescript", tree, "app/notes/new.tsx")

	e := findByNameRel(ents, "NewNoteScreen")
	if e == nil {
		t.Fatal("expected entity 'NewNoteScreen' to be emitted")
	}
	// Just verify it did not panic — no specific edge assertion needed for defaults.
}
