// Package javascript_test — issue #2859 mobile coverage greening.
//
// Proves the Structure / Data Flow / Lifecycle capabilities for the mobile
// framework family (Ionic, NativeScript, Expo, React Native) against small
// hand-written fixtures under testdata/mobile_*. Each fixture is the proving
// artifact for a coverage cell flipped to `full` in docs/coverage/registry.json.
//
// The JS/TS extractor is framework-agnostic: Ionic (React under the hood),
// React-NativeScript, Expo and React Native all reuse React's
// createContext / HOC / useState primitives, so the existing extraction
// (#611 context, #513 state setters, HOC wrapper recognition, #2654
// discriminators, #2590 zustand stores) covers them. NativeScript core adds
// its own Observable state-setter idiom (this.set / notifyPropertyChange),
// recognised by isNativeScriptStateSetter (#2859).
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
	"github.com/cajasmota/grafel/internal/types"
)

// extractFixture parses and extracts a testdata file with the TypeScript grammar.
func extractFixture(t *testing.T, relPath string) []types.EntityRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", relPath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relPath, err)
	}
	tree := parseTS(t, content)
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     relPath,
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract %s: %v", relPath, err)
	}
	return ents
}

func entityWithSubtype(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func assertContext(t *testing.T, ents []types.EntityRecord, name string) {
	t.Helper()
	e := entityWithSubtype(ents, name)
	if e == nil {
		t.Fatalf("context entity %q not found; names: %v", name, entityNames(ents))
	}
	if e.Subtype != "context" {
		t.Errorf("context %q: subtype=%q, want \"context\"", name, e.Subtype)
	}
}

func assertHOC(t *testing.T, ents []types.EntityRecord, name string) {
	t.Helper()
	e := entityWithSubtype(ents, name)
	if e == nil {
		t.Fatalf("HOC entity %q not found; names: %v", name, entityNames(ents))
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("HOC %q: kind=%q, want SCOPE.Operation", name, e.Kind)
	}
}

func assertStateSetter(t *testing.T, ents []types.EntityRecord, name string) {
	t.Helper()
	e := entityWithSubtype(ents, name)
	if e == nil {
		t.Fatalf("state-setter entity %q not found; names: %v", name, entityNames(ents))
	}
	if e.Subtype != "state_setter" {
		t.Errorf("state setter %q: subtype=%q, want \"state_setter\"", name, e.Subtype)
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("state setter %q: kind=%q, want SCOPE.Operation", name, e.Kind)
	}
}

func assertDiscriminator(t *testing.T, ents []types.EntityRecord, entityName, wantContains string) {
	t.Helper()
	for i := range ents {
		if ents[i].Name == entityName {
			got := ents[i].Properties["discriminators"]
			if got == "" {
				t.Errorf("%q: no discriminators stamped", entityName)
			}
			if wantContains != "" && !contains(got, wantContains) {
				t.Errorf("%q: discriminators=%q, want to contain %q", entityName, got, wantContains)
			}
			return
		}
	}
	t.Errorf("entity %q not found for discriminator check; names: %v", entityName, entityNames(ents))
}

func assertZustandAction(t *testing.T, ents []types.EntityRecord, name string) {
	t.Helper()
	e := entityWithSubtype(ents, name)
	if e == nil {
		t.Fatalf("zustand action %q not found; names: %v", name, entityNames(ents))
	}
	if e.Kind != "SCOPE.Operation" {
		t.Errorf("zustand action %q: kind=%q, want SCOPE.Operation", name, e.Kind)
	}
	if e.Properties["via"] != "zustand_store" {
		t.Errorf("zustand action %q: via=%q, want \"zustand_store\"", name, e.Properties["via"])
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ── Ionic (React under the hood) ─────────────────────────────────────────────

func TestMobile_Ionic_StructureDataFlowLifecycle(t *testing.T) {
	ents := extractFixture(t, "mobile_ionic/SessionContext.tsx")

	// Structure/context_extraction
	assertContext(t, ents, "SessionContext")
	// Structure/hoc_wrapper_recognition — Ionic lifecycle HOC + React memo
	assertHOC(t, ents, "LifecyclePanel")
	assertHOC(t, ents, "MemoPanel")
	// Data Flow/state_management + Lifecycle/state_setter_emission
	assertStateSetter(t, ents, "setAuthState")
	assertStateSetter(t, ents, "setRetries")
	// Data Flow/branch_conditions
	assertDiscriminator(t, ents, "classify", "authState=authenticated")
}

// ── NativeScript (Observable core + React flavor) ────────────────────────────

func TestMobile_NativeScript_ObservableStateSetters(t *testing.T) {
	ents := extractFixture(t, "mobile_nativescript/main-view-model.ts")

	// Data Flow/state_management + Lifecycle/state_setter_emission via the
	// NativeScript Observable idiom (set accessor, this.set, notifyPropertyChange).
	assertStateSetter(t, ents, "counter")          // set accessor that notifies
	assertStateSetter(t, ents, "incrementCounter") // this.set("counter", ...)
	assertStateSetter(t, ents, "reset")            // notifyPropertyChange

	// Regression: a plain method that does not notify is NOT a state setter.
	if e := entityWithSubtype(ents, "classify"); e == nil {
		t.Fatal("classify method not found")
	} else if e.Subtype == "state_setter" {
		t.Errorf("classify: should NOT be state_setter (no observable notify)")
	}

	// Data Flow/branch_conditions
	assertDiscriminator(t, ents, "classify", "status=idle")
}

func TestMobile_NativeScript_ReactFlavorContextHOC(t *testing.T) {
	ents := extractFixture(t, "mobile_nativescript/AppShell.tsx")

	// Structure/context_extraction
	assertContext(t, ents, "DeviceContext")
	// Structure/hoc_wrapper_recognition — NativeScript orientation HOC + memo
	assertHOC(t, ents, "OrientationShell")
	assertHOC(t, ents, "MemoShell")
}

// ── React Native (FLAGSHIP) ──────────────────────────────────────────────────

func TestMobile_ReactNative_StateManagementFull(t *testing.T) {
	ents := extractFixture(t, "mobile_react_native/CartScreen.tsx")

	// Data Flow/state_management + Lifecycle/state_setter_emission
	assertStateSetter(t, ents, "setQuantity")
	assertStateSetter(t, ents, "setCoupon")
	assertStateSetter(t, ents, "dispatch") // useReducer dispatch

	// Structure/context_extraction + hoc_wrapper_recognition
	assertContext(t, ents, "CartContext")

	// zustand store actions are recognised store members (#2590) — emitted as
	// SCOPE.Operation with Properties["via"]="zustand_store".
	assertZustandAction(t, ents, "useCartStore::addItem")
	assertZustandAction(t, ents, "useCartStore::clear")

	// Data Flow/branch_conditions
	assertDiscriminator(t, ents, "checkout", "coupon=FREESHIP")
}

// ── Expo ─────────────────────────────────────────────────────────────────────

func TestMobile_Expo_StateManagementFull(t *testing.T) {
	ents := extractFixture(t, "mobile_expo/ProfileScreen.tsx")

	assertStateSetter(t, ents, "setName")
	assertStateSetter(t, ents, "setTheme")
	assertStateSetter(t, ents, "dispatchPrefs")

	assertContext(t, ents, "ThemeContext")

	assertZustandAction(t, ents, "useSessionStore::setToken")
	assertZustandAction(t, ents, "useSessionStore::logout")

	assertDiscriminator(t, ents, "save", "theme=dark")
}
