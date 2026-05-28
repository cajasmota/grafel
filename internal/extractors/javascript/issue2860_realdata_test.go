// Package javascript — issue #2860 real-data verification (mobile navigation /
// native-bridge / platform). Runs the registered TSX extractor over the in-repo
// real-world React Native root navigator fixture and asserts the mobile signals
// (navigators / screens / deep_link / native_modules / platform_branches) fire
// on real-shaped source — not just the minimal per-capability fixtures.
package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIssue2860_RealData_ReactNativeNavigator(t *testing.T) {
	ents := extractRealWorld(t, "react_native_navigator.tsx")

	var fe *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "file" {
			fe = &ents[i]
			break
		}
	}
	if fe == nil {
		t.Fatalf("no file entity found; names: %v", entityNames(ents))
	}

	mustContain := func(key, want string) {
		got := fe.Properties[key]
		if !strings.Contains(got, want) {
			t.Errorf("real-data file Properties[%q]=%q, want to contain %q", key, got, want)
		}
	}

	// navigation_extraction — factories + container.
	mustContain("navigators", "createNativeStackNavigator")
	mustContain("navigators", "createBottomTabNavigator")
	mustContain("navigators", "NavigationContainer")

	// screen_detection — <Stack.Screen> / <Tabs.Screen> declarations.
	for _, scr := range []string{"Home", "Feed", "Profile", "Main", "Settings"} {
		mustContain("screens", scr)
	}

	// deep_link_extraction — Linking.createURL prefix + linking config screens.
	mustContain("deep_link_prefixes", "/")
	for _, scr := range []string{"Home", "Feed", "Profile", "Settings"} {
		mustContain("deep_link_screens", scr)
	}

	// native_module_imports — NativeModules + NativeEventEmitter from react-native
	// plus the @react-native-firebase/* native package.
	mustContain("native_modules", "react-native:NativeModules")
	mustContain("native_modules", "react-native:NativeEventEmitter")

	// platform_branching — Platform.OS comparison + Platform.select.
	mustContain("platform_branches", "Platform.OS")
	mustContain("platform_branches", "Platform.select")

	// Edge sanity: at least one NAVIGATES_TO via=screen_config and one via=deep_link.
	var screenEdges, deepEdges int
	for _, r := range fe.Relationships {
		if r.Kind != "NAVIGATES_TO" {
			continue
		}
		switch r.Properties["via"] {
		case "screen_config":
			screenEdges++
		case "deep_link":
			deepEdges++
		}
	}
	if screenEdges == 0 {
		t.Error("expected >=1 NAVIGATES_TO via=screen_config on real-data fixture")
	}
	if deepEdges == 0 {
		t.Error("expected >=1 NAVIGATES_TO via=deep_link on real-data fixture")
	}
	t.Logf("real-data #2860: screen_config edges=%d deep_link edges=%d", screenEdges, deepEdges)
}
