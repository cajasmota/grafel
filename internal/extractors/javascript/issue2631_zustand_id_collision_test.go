// Package javascript_test — issue #2631: Zustand action entity ID collision.
//
// PR #2630 shipped emitStoreActionEntities which created entities for each
// Zustand store action using the action's bare name as the entity ID. When
// two stores share an action name (e.g. useAuthStore.logout + useAdminStore.logout)
// they collided into a single entity.
//
// Fix: qualify entity IDs with the store variable name using "::" separator.
//
//	Before: "logout"
//	After:  "useAuthStore::logout"
//
// Two tests:
//   - TestZustandStore_DistinctIDsAcrossStores — 2 stores both with a "logout"
//     action produce 2 distinct entities with distinct IDs.
//   - TestZustandStore_CallEdgeReferencesQualifiedID — a getState().logout()
//     call site emits a CALLS edge to "useAuthStore::logout" (not bare "logout").
package javascript_test

import (
	"testing"
)

// TestZustandStore_DistinctIDsAcrossStores verifies that two Zustand stores
// sharing an action name produce distinct entities with distinct qualified names.
//
// Fixture: useAuthStore.logout + useAdminStore.logout
// Expected: two entities named "useAuthStore::logout" and "useAdminStore::logout"
// (not a single colliding "logout" entity). Issue #2631.
func TestZustandStore_DistinctIDsAcrossStores(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  token: null,
  logout: () => set({ token: null }),
}));

export const useAdminStore = create((set) => ({
  adminToken: null,
  logout: () => set({ adminToken: null }),
}));
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	authLogout := findByNameRel(ents, "useAuthStore::logout")
	if authLogout == nil {
		t.Errorf("expected entity 'useAuthStore::logout' to be emitted; got names: %v", func() []string {
			var ns []string
			for _, e := range ents {
				ns = append(ns, e.Name)
			}
			return ns
		}())
	}

	adminLogout := findByNameRel(ents, "useAdminStore::logout")
	if adminLogout == nil {
		t.Errorf("expected entity 'useAdminStore::logout' to be emitted; got names: %v", func() []string {
			var ns []string
			for _, e := range ents {
				ns = append(ns, e.Name)
			}
			return ns
		}())
	}

	// Verify no bare "logout" entity was emitted (old collision shape).
	bareLogout := findByNameRel(ents, "logout")
	if bareLogout != nil {
		t.Errorf("unexpected bare entity 'logout': qualified names should be used (issue #2631)")
	}

	// Verify the two entities have distinct IDs.
	if authLogout != nil && adminLogout != nil {
		if authLogout.ID == adminLogout.ID {
			t.Errorf("useAuthStore::logout and useAdminStore::logout share the same entity ID %q (issue #2631 — ID collision not fixed)", authLogout.ID)
		}
	}
}

// TestZustandStore_CallEdgeReferencesQualifiedID verifies that a call site
// using useAuthStore.getState().logout() emits a CALLS edge to the qualified
// entity ID "useAuthStore::logout" (not the bare "logout"). Issue #2631.
func TestZustandStore_CallEdgeReferencesQualifiedID(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  token: null,
  logout: () => set({ token: null }),
}));

export function handleExpiry() {
  useAuthStore.getState().logout();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	caller := findByNameRel(ents, "handleExpiry")
	if caller == nil {
		t.Fatal("expected entity 'handleExpiry' to be emitted")
	}

	// Must have CALLS edge to the qualified ID.
	foundQualified := false
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::logout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_store" {
			foundQualified = true
			break
		}
	}
	if !foundQualified {
		t.Logf("handleExpiry relationships:")
		for _, r := range caller.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleExpiry→useAuthStore::logout (via=zustand_store); not found (issue #2631)")
	}

	// Must NOT have a CALLS edge to the bare "logout".
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "logout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_store" {
			t.Errorf("unexpected CALLS handleExpiry→logout (bare name): edge should use qualified ID useAuthStore::logout (issue #2631)")
		}
	}
}
