// Package javascript_test — issue #2590: Zustand store action CALLS edges.
//
// Verifies that the extractor detects `create(...)` from "zustand", maps the
// store variable to its action set, and emits CALLS edges with
// Properties["via"]="zustand_store" when call sites access actions via
// getState().<action>() or immediately-invoked selectors.
package javascript_test

import (
	"testing"
)

// TestTSExtractor_ZustandStore_GetStateActionCall verifies that:
//
//	const useSyncQueueStore = create<State>((set, get) => ({ process: async () => {} }))
//	useSyncQueueStore.getState().process()
//
// emits a CALLS edge from the caller function to "process" with
// Properties["via"] = "zustand_store". Issue #2590.
func TestTSExtractor_ZustandStore_GetStateActionCall(t *testing.T) {
	src := `
import { create } from 'zustand';

type State = {
  queue: string[];
  process: () => Promise<void>;
};

export const useSyncQueueStore = create<State>((set, get) => ({
  queue: [],
  process: async () => {
    // process the queue
  },
}));

export async function syncEngine() {
  useSyncQueueStore.getState().process();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// syncEngine entity must exist.
	if findByNameRel(ents, "syncEngine") == nil {
		t.Fatal("expected entity 'syncEngine' to be emitted")
	}

	// Must emit CALLS from syncEngine to process via zustand_store.
	e := findByNameRel(ents, "syncEngine")
	if e == nil {
		t.Fatal("syncEngine entity missing")
	}

	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "process" {
			if r.Properties != nil && r.Properties["via"] == "zustand_store" {
				found = true
				break
			}
			t.Logf("CALLS syncEngine→process found but via=%q (want zustand_store); props=%v",
				r.Properties["via"], r.Properties)
		}
	}
	if !found {
		t.Logf("syncEngine relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS syncEngine→process with via=zustand_store; not found")
	}
}

// TestTSExtractor_ZustandStore_MultiActionGetState verifies that chained
// getState() calls in the same caller each produce a CALLS edge.
// Issue #2590 — real-world syncEngine.ts calls markFailed, markSyncing,
// markCompleted, storeResolvedId, etc. on the same store.
func TestTSExtractor_ZustandStore_MultiActionGetState(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useSyncQueueStore = create((set, get) => ({
  queue: [],
  markFailed: (id, msg) => set(state => state),
  markCompleted: (id) => set(state => state),
  markSyncing: (id) => set(state => state),
}));

async function processSyncQueue(id) {
  useSyncQueueStore.getState().markSyncing(id);
  try {
    useSyncQueueStore.getState().markCompleted(id);
  } catch (e) {
    useSyncQueueStore.getState().markFailed(id, e.message);
  }
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if findByNameRel(ents, "processSyncQueue") == nil {
		t.Fatal("expected entity 'processSyncQueue' to be emitted")
	}

	for _, action := range []string{"markSyncing", "markCompleted", "markFailed"} {
		e := findByNameRel(ents, "processSyncQueue")
		found := false
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == action &&
				r.Properties != nil && r.Properties["via"] == "zustand_store" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CALLS processSyncQueue→%s (via=zustand_store); not found", action)
		}
	}
}

// TestTSExtractor_ZustandStore_SelectorActionCall verifies that the
// immediately-invoked selector pattern emits a CALLS edge:
//
//	useSyncQueueStore(s => s.process)()
//
// Issue #2590.
func TestTSExtractor_ZustandStore_SelectorActionCall(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set, get) => ({
  token: null,
  logout: () => set({ token: null }),
  refresh: async () => { /* ... */ },
}));

function onSessionExpired() {
  useAuthStore(s => s.logout)();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if findByNameRel(ents, "onSessionExpired") == nil {
		t.Fatal("expected entity 'onSessionExpired' to be emitted")
	}

	e := findByNameRel(ents, "onSessionExpired")
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "logout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_store" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("onSessionExpired relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS onSessionExpired→logout (via=zustand_store); not found")
	}
}

// TestTSExtractor_ZustandStore_NonAction_NoEdge verifies that accessing a
// non-function property via getState() does NOT emit a spurious CALLS edge.
//
//	useSyncQueueStore.getState().queue  // queue is a plain array, not a function
//
// Issue #2590 — must not over-emit.
func TestTSExtractor_ZustandStore_NonAction_NoEdge(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useSyncQueueStore = create((set, get) => ({
  queue: [],
  enqueue: (item) => set(state => ({ queue: [...state.queue, item] })),
}));

function readQueue() {
  const q = useSyncQueueStore.getState().queue;
  return q.length;
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if findByNameRel(ents, "readQueue") == nil {
		t.Fatal("expected entity 'readQueue' to be emitted")
	}

	e := findByNameRel(ents, "readQueue")
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "queue" &&
			r.Properties != nil && r.Properties["via"] == "zustand_store" {
			t.Errorf("unexpected CALLS readQueue→queue (via=zustand_store); 'queue' is a plain array, not an action")
		}
	}
}

// TestZustandStore_ClosureMethod_EmittedAsEntity verifies that store action
// methods are emitted as standalone SCOPE.Operation entities with kind=Method
// and a CALLS edge is wired to an inner callee. Issue #2626.
//
// Fixture: create((set, get) => ({ foo: () => bar() }))
// Expected: entity for "foo" exists with Kind=SCOPE.Operation, subtype=method,
// and via=zustand_store in Properties.
func TestZustandStore_ClosureMethod_EmittedAsEntity(t *testing.T) {
	src := `
import { create } from 'zustand';

function bar() {}

export const useMyStore = create((set, get) => ({
  foo: () => bar(),
}));
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	fooEnt := findByNameRel(ents, "foo")
	if fooEnt == nil {
		t.Fatalf("expected entity 'foo' to be emitted as a standalone entity (issue #2626); got names: %v", func() []string {
			var ns []string
			for _, e := range ents {
				ns = append(ns, e.Name)
			}
			return ns
		}())
	}
	if fooEnt.Kind != "SCOPE.Operation" {
		t.Errorf("foo.Kind = %q, want SCOPE.Operation", fooEnt.Kind)
	}
	if fooEnt.Subtype != "method" {
		t.Errorf("foo.Subtype = %q, want method", fooEnt.Subtype)
	}
	if fooEnt.Properties == nil || fooEnt.Properties["via"] != "zustand_store" {
		t.Errorf("foo.Properties[via] = %q, want zustand_store; props=%v", fooEnt.Properties["via"], fooEnt.Properties)
	}
	if fooEnt.StartLine == 0 {
		t.Errorf("foo.StartLine = 0, expected a valid source line (issue #2626 — line range required)")
	}
}

// TestTraces_FollowsIntoZustandClosure exercises the 4-step chain:
//
//	caller → useAuthStore (via CALLS edge) → unlockWithBiometrics → biometricLib.auth
//
// Without the #2626 fix, the trace terminated at useAuthStore with
// result="no_outgoing_calls" because unlockWithBiometrics was not a graph
// entity and the CALLS adjacency had no outgoing edges for it.
//
// This test verifies that the emitted action entity ("unlockWithBiometrics")
// exists and carries the expected metadata.
func TestTraces_FollowsIntoZustandClosure(t *testing.T) {
	src := `
import { create } from 'zustand';

async function biometricAuth() { return true; }

export const useAuthStore = create((set, get) => ({
  isAuthenticated: false,
  unlockWithBiometrics: async () => {
    try {
      const ok = await biometricAuth();
      set({ isAuthenticated: ok });
    } catch (_) {}
  },
}));

export async function loginWithBiometrics() {
  useAuthStore.getState().unlockWithBiometrics();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// 1. unlockWithBiometrics must be emitted as a standalone entity.
	actionEnt := findByNameRel(ents, "unlockWithBiometrics")
	if actionEnt == nil {
		t.Fatalf("expected 'unlockWithBiometrics' as a standalone entity (issue #2626); not found")
	}
	if actionEnt.Kind != "SCOPE.Operation" {
		t.Errorf("unlockWithBiometrics.Kind = %q, want SCOPE.Operation", actionEnt.Kind)
	}
	if actionEnt.StartLine == 0 {
		t.Errorf("unlockWithBiometrics.StartLine = 0, want non-zero (body line range)")
	}

	// 2. loginWithBiometrics must have a CALLS edge to unlockWithBiometrics.
	caller := findByNameRel(ents, "loginWithBiometrics")
	if caller == nil {
		t.Fatal("expected entity 'loginWithBiometrics'")
	}
	found := false
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "unlockWithBiometrics" &&
			r.Properties != nil && r.Properties["via"] == "zustand_store" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("loginWithBiometrics relationships:")
		for _, r := range caller.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS loginWithBiometrics→unlockWithBiometrics (via=zustand_store); not found")
	}
}

// TestZustandStore_PartializeConfig_TrackedAsProperty verifies that when
// create() receives a second-argument config with a partialize function, the
// field names returned by partialize are recorded in the action entities'
// Properties["partialize_fields"]. Issue #2626.
//
// Fixture: create((...) => ({...}), { name: 'auth', partialize: (s) => ({ user: s.user }) })
// Expected: action entity has Properties["partialize_fields"] = "user"
func TestZustandStore_PartializeConfig_TrackedAsProperty(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create(
  (set, get) => ({
    user: null,
    token: null,
    login: (u) => set({ user: u }),
    logout: () => set({ user: null, token: null }),
  }),
  {
    name: 'auth',
    partialize: (s) => ({ user: s.user }),
  }
);
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// At least one of the action entities should carry partialize_fields.
	found := false
	for _, e := range ents {
		if e.Properties == nil {
			continue
		}
		if e.Properties["via"] == "zustand_store" {
			pf := e.Properties["partialize_fields"]
			if pf != "" {
				found = true
				if pf != "user" {
					t.Errorf("partialize_fields = %q, want \"user\"", pf)
				}
				break
			}
		}
	}
	if !found {
		t.Errorf("expected at least one zustand_store action entity with partialize_fields set; none found")
	}
}

// TestTSExtractor_ZustandStore_NonZustandCreate_NoEdge verifies that a
// `create()` call from a non-zustand package does NOT trigger action tracking.
// Issue #2590 — must not match react's createContext or other create helpers.
func TestTSExtractor_ZustandStore_NonZustandCreate_NoEdge(t *testing.T) {
	src := `
import { createContext } from 'react';

const MyContext = createContext((set, get) => ({
  doSomething: () => {},
}));

function useCtx() {
  MyContext.getState().doSomething();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	if e := findByNameRel(ents, "useCtx"); e != nil {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.Properties != nil && r.Properties["via"] == "zustand_store" {
				t.Errorf("unexpected CALLS via=zustand_store for non-zustand create(); got %s→%s",
					"useCtx", r.ToID)
			}
		}
	}
}
