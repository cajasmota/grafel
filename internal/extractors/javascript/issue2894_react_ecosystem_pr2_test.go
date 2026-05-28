// issue2894_react_ecosystem_pr2_test.go — issue #2894 PR2 proving tests.
//
// Proves the two PR2 framework_specific["React Ecosystem"] cells against the
// hand-written fixtures testdata/react_ecosystem/Atoms.tsx (Recoil/Jotai/Valtio/
// MobX) and testdata/react_ecosystem/Swr.tsx (SWR). Each assertion is the
// proving artifact for a coverage cell:
//   - atom_store_extraction → recoil_atom / recoil_selector / jotai_atom /
//                             valtio_proxy / mobx_store entities (atom_library,
//                             atom_key stamped) + useRecoilState/useAtom hooks.
//   - swr_extraction        → useSWR/useSWRMutation/useSWRInfinite consumers
//                             decorated swr=true + swr_keys.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2894PR2_AtomStoreExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Atoms.tsx")

	// Recoil atom + selector — atom_library=recoil, atom_key stamped.
	count := ecoEntity(t, ents, "countState", "recoil_atom")
	if count.Properties["atom_library"] != "recoil" {
		t.Errorf("countState atom_library=%q, want recoil", count.Properties["atom_library"])
	}
	if count.Properties["atom_key"] != "countState" {
		t.Errorf("countState atom_key=%q, want countState", count.Properties["atom_key"])
	}
	sel := ecoEntity(t, ents, "doubledState", "recoil_selector")
	if sel.Properties["atom_key"] != "doubledState" {
		t.Errorf("doubledState atom_key=%q, want doubledState", sel.Properties["atom_key"])
	}

	// Jotai atoms — aliased `atom as jotaiAtom` + atomWithStorage.
	tok := ecoEntity(t, ents, "tokenAtom", "jotai_atom")
	if tok.Properties["atom_library"] != "jotai" {
		t.Errorf("tokenAtom atom_library=%q, want jotai", tok.Properties["atom_library"])
	}
	prefs := ecoEntity(t, ents, "prefsAtom", "jotai_atom")
	if prefs.Properties["atom_factory"] != "atomWithStorage" {
		t.Errorf("prefsAtom atom_factory=%q, want atomWithStorage", prefs.Properties["atom_factory"])
	}

	// Valtio proxy + MobX observable.
	cart := ecoEntity(t, ents, "cartStore", "valtio_proxy")
	if cart.Properties["via"] != "atom_store" {
		t.Errorf("cartStore via=%q, want atom_store", cart.Properties["via"])
	}
	ecoEntity(t, ents, "counterStore", "mobx_store")

	// Read/write hooks surface as USES_HOOK on the consumer (generic pass).
	comp := findByName(ents, "Counter")
	if comp == nil {
		t.Fatalf("Counter component not extracted")
	}
	wantUses := func(hook string) {
		for _, r := range comp.Relationships {
			if r.Kind == "USES_HOOK" && r.ToID == hook {
				return
			}
		}
		t.Errorf("Counter missing USES_HOOK->%s; rels=%v", hook, comp.Relationships)
	}
	wantUses("useRecoilState")
	wantUses("useAtom")
	wantUses("useSnapshot")
}

func TestIssue2894PR2_SWRExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Swr.tsx")

	swrDecorated := func(name, wantKey, wantHook string) *types.EntityRecord {
		e := findByName(ents, name)
		if e == nil {
			t.Fatalf("%s not extracted; names: %v", name, entityNames(ents))
		}
		if e.Properties["swr"] != "true" {
			t.Errorf("%s swr=%q, want true; props=%v", name, e.Properties["swr"], e.Properties)
		}
		if wantKey != "" && e.Properties["swr_keys"] != wantKey {
			t.Errorf("%s swr_keys=%q, want %q", name, e.Properties["swr_keys"], wantKey)
		}
		if wantHook != "" {
			got := e.Properties["swr_hooks"]
			if got != wantHook {
				t.Errorf("%s swr_hooks=%q, want %q", name, got, wantHook)
			}
		}
		// The hook call also surfaces as USES_HOOK (generic pass).
		found := false
		for _, r := range e.Relationships {
			if r.Kind == "USES_HOOK" && r.ToID == wantHook {
				found = true
			}
		}
		if wantHook != "" && !found {
			t.Errorf("%s missing USES_HOOK->%s; rels=%v", name, wantHook, e.Relationships)
		}
		return e
	}

	swrDecorated("useUsers", "/api/users", "useSWR")
	swrDecorated("useUpdateUser", "/api/users", "useSWRMutation")
	// useUser uses a template-literal key (no literal) — decorated but no key.
	uu := findByName(ents, "useUser")
	if uu == nil || uu.Properties["swr"] != "true" {
		t.Errorf("useUser not decorated swr=true; props=%v", uu)
	}
	// Component using useSWRInfinite.
	swrDecorated("Feed", "", "useSWRInfinite")
}
