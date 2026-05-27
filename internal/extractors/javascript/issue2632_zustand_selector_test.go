// Package javascript_test — issue #2632: CALLS edges via single-property Zustand selector.
//
// PR #2629 handled `const { softLogout } = useAuthStore()` (object-pattern destructuring).
// This file covers the more common arrow-selector form:
//
//	const softLogout = useAuthStore(s => s.softLogout);
//	softLogout();  ← must produce CALLS edge
//
// Five test cases:
//   - Basic:        s => s.action
//   - Parenthesised: (s) => s.action
//   - Any param name: state => state.action
//   - Optional chain: s => s?.action
//   - Negative: derived value (s => Boolean(s.user)) — no spurious edge
package javascript_test

import (
	"testing"
)

// TestTSExtractor_ZustandSingleSelector_EmitsCallsEdge verifies the basic form:
//
//	const softLogout = useAuthStore(s => s.softLogout);
//	softLogout();
//
// Must emit CALLS → softLogout with Properties["via"] = "zustand_selector".
func TestTSExtractor_ZustandSingleSelector_EmitsCallsEdge(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  softLogout: () => set({ token: null }),
}));

export const handleLogout = () => {
  const softLogout = useAuthStore(s => s.softLogout);
  softLogout();
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleLogout")
	if e == nil {
		t.Fatal("expected entity 'handleLogout' to be emitted")
	}

	// Issue #2631 — ToID is now qualified: <storeVar>::<actionName>.
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::softLogout" {
			if r.Properties != nil && r.Properties["via"] == "zustand_selector" {
				found = true
				break
			}
			t.Logf("CALLS handleLogout→useAuthStore::softLogout found but via=%q (want zustand_selector); props=%v",
				r.Properties["via"], r.Properties)
		}
	}
	if !found {
		t.Logf("handleLogout relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleLogout→useAuthStore::softLogout with via=zustand_selector; not found")
	}
}

// TestTSExtractor_ZustandSelectorParenthesized verifies the parenthesised-param form:
//
//	const softLogout = useAuthStore((s) => s.softLogout);
//	softLogout();
func TestTSExtractor_ZustandSelectorParenthesized(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  softLogout: () => set({ token: null }),
}));

export const handleLogout = () => {
  const softLogout = useAuthStore((s) => s.softLogout);
  softLogout();
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleLogout")
	if e == nil {
		t.Fatal("expected entity 'handleLogout' to be emitted")
	}

	// Issue #2631 — ToID is now qualified: <storeVar>::<actionName>.
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::softLogout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_selector" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("handleLogout relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleLogout→useAuthStore::softLogout with via=zustand_selector (parenthesised param); not found")
	}
}

// TestTSExtractor_ZustandSelectorStateName verifies that any parameter name works:
//
//	const softLogout = useAuthStore(state => state.softLogout);
//	softLogout();
func TestTSExtractor_ZustandSelectorStateName(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  softLogout: () => set({ token: null }),
}));

export const handleLogout = () => {
  const softLogout = useAuthStore(state => state.softLogout);
  softLogout();
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleLogout")
	if e == nil {
		t.Fatal("expected entity 'handleLogout' to be emitted")
	}

	// Issue #2631 — ToID is now qualified: <storeVar>::<actionName>.
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::softLogout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_selector" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("handleLogout relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleLogout→useAuthStore::softLogout with via=zustand_selector (state param name); not found")
	}
}

// TestTSExtractor_ZustandSelectorOptionalChain verifies optional-chain selector:
//
//	const softLogout = useAuthStore(s => s?.softLogout);
//	softLogout();
func TestTSExtractor_ZustandSelectorOptionalChain(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  softLogout: () => set({ token: null }),
}));

export const handleLogout = () => {
  const softLogout = useAuthStore(s => s?.softLogout);
  softLogout();
};
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "handleLogout")
	if e == nil {
		t.Fatal("expected entity 'handleLogout' to be emitted")
	}

	// Issue #2631 — ToID is now qualified: <storeVar>::<actionName>.
	found := false
	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "useAuthStore::softLogout" &&
			r.Properties != nil && r.Properties["via"] == "zustand_selector" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("handleLogout relationships:")
		for _, r := range e.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Errorf("expected CALLS handleLogout→useAuthStore::softLogout with via=zustand_selector (optional chain); not found")
	}
}

// TestTSExtractor_ZustandSelectorDerivedValue_NoSpuriousEdge verifies the negative case:
// a derived-value selector must NOT produce a CALLS edge for the local variable.
//
//	const isAuthed = useAuthStore(s => Boolean(s.user));
//	isAuthed && doX();   ← isAuthed() is never called
func TestTSExtractor_ZustandSelectorDerivedValue_NoSpuriousEdge(t *testing.T) {
	src := `
import { create } from 'zustand';

export const useAuthStore = create((set) => ({
  user: null,
}));

function doX() {}

export function Guard() {
  const isAuthed = useAuthStore(s => Boolean(s.user));
  isAuthed && doX();
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	e := findByNameRel(ents, "Guard")
	if e == nil {
		t.Fatal("expected entity 'Guard' to be emitted")
	}

	for _, r := range e.Relationships {
		if r.Kind == "CALLS" && r.ToID == "isAuthed" {
			t.Errorf("unexpected CALLS Guard→isAuthed: derived-value selector should not produce action CALLS edge; props=%v", r.Properties)
		}
	}
}
