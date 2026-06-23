// Package javascript_test — issue #2860 mobile navigation / native-bridge /
// platform coverage greening.
//
// Proves the Navigation (navigation_extraction, screen_detection,
// deep_link_extraction), Native Bridge (native_module_imports) and Platform
// (platform_branching) capabilities for the four mobile framework families
// (Expo, React Native, Ionic, NativeScript) against small hand-written
// fixtures under testdata/mobile_*. Each assertion is the proving artifact for
// a coverage cell flipped to `full` in docs/coverage/registry.json.
//
// The mobile signals decorate the file-level entity (subtype "file") with
// summary Properties (navigators / screens / deep_link_prefixes /
// deep_link_screens / native_modules / platform_branches) and emit
// NAVIGATES_TO edges (via=screen_config | deep_link) for declared screens and
// deep links. All edges reuse existing Kinds (NAVIGATES_TO, IMPORTS, BRANCHES_ON).
package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extractFixtureTSX parses a .tsx/.jsx testdata fixture with the JSX-enabled TSX
// grammar and extracts it at its real relative path (so .ios.tsx platform-
// variant detection fires).
func extractFixtureTSX(t *testing.T, relPath string) []types.EntityRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", relPath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relPath, err)
	}
	tree := parseTSX(t, content)
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
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

// fileEntity returns the file-level (subtype "file") entity from an extraction.
func fileEntity(t *testing.T, ents []types.EntityRecord) *types.EntityRecord {
	t.Helper()
	for i := range ents {
		if ents[i].Subtype == "file" {
			return &ents[i]
		}
	}
	t.Fatalf("no file entity found; names: %v", entityNames(ents))
	return nil
}

// propContains asserts the file entity's Properties[key] contains want.
func propContains(t *testing.T, fe *types.EntityRecord, key, want string) {
	t.Helper()
	got := fe.Properties[key]
	if !strings.Contains(got, want) {
		t.Errorf("file entity Properties[%q]=%q, want to contain %q", key, got, want)
	}
}

// hasNavVia asserts the file entity has a NAVIGATES_TO edge to route:<route>
// with Properties[via]=via.
func hasNavVia(fe *types.EntityRecord, route, via string) bool {
	for _, r := range fe.Relationships {
		if r.Kind == "NAVIGATES_TO" && r.ToID == "route:"+route && r.Properties["via"] == via {
			return true
		}
	}
	return false
}

// hasNativeImportEdge asserts an IMPORTS edge carries native_module=1.
func hasNativeImportEdge(fe *types.EntityRecord, module string) bool {
	for _, r := range fe.Relationships {
		if r.Kind != "IMPORTS" || r.Properties["native_module"] != "1" {
			continue
		}
		if r.ToID == module || r.Properties["source_module"] == module {
			return true
		}
	}
	return false
}

// ── React Native (FLAGSHIP) ──────────────────────────────────────────────────

func TestMobileNav_ReactNative_NavigatorScreensNativePlatform(t *testing.T) {
	ents := extractFixtureTSX(t, "mobile_react_native/AppNavigator.tsx")
	fe := fileEntity(t, ents)

	// navigation_extraction — navigator factory recognised.
	propContains(t, fe, "navigators", "createNativeStackNavigator")
	// screen_detection — <Stack.Screen name=…> declarations.
	for _, scr := range []string{"Home", "Profile", "Settings"} {
		propContains(t, fe, "screens", scr)
		if !hasNavVia(fe, scr, "screen_config") {
			t.Errorf("expected NAVIGATES_TO route:%s via=screen_config", scr)
		}
	}
	// native_module_imports — NativeModules import from react-native +
	// react-native-keychain native package import.
	propContains(t, fe, "native_modules", "react-native:NativeModules")
	if !hasNativeImportEdge(fe, "react-native-keychain") {
		t.Errorf("expected native_module IMPORTS edge for react-native-keychain; rels=%v", fe.Relationships)
	}
	// platform_branching — Platform.OS comparison + Platform.select.
	propContains(t, fe, "platform_branches", "Platform.OS")
	propContains(t, fe, "platform_branches", "Platform.select")
}

// ── Expo ─────────────────────────────────────────────────────────────────────

func TestMobileNav_Expo_DeepLinksNativeModules(t *testing.T) {
	ents := extractFixture(t, "mobile_expo/linking.ts")
	fe := fileEntity(t, ents)

	// deep_link_extraction — Linking.createURL prefix + linking config screens.
	propContains(t, fe, "deep_link_prefixes", "/redirect")
	propContains(t, fe, "deep_link_prefixes", "myapp://")
	for _, scr := range []string{"Home", "Profile", "Settings"} {
		propContains(t, fe, "deep_link_screens", scr)
		if !hasNavVia(fe, scr, "deep_link") {
			t.Errorf("expected NAVIGATES_TO route:%s via=deep_link", scr)
		}
	}
	if !hasNavVia(fe, "/redirect", "deep_link") {
		t.Error("expected NAVIGATES_TO route:/redirect via=deep_link")
	}
	// native_module_imports — expo-secure-store import + requireNativeModule.
	if !hasNativeImportEdge(fe, "expo-secure-store") {
		t.Errorf("expected native_module IMPORTS edge for expo-secure-store; rels=%v", fe.Relationships)
	}
	propContains(t, fe, "native_modules", "requireNativeModule('ExpoDevice')")
}

func TestMobileNav_Expo_PlatformVariantBranch(t *testing.T) {
	ents := extractFixtureTSX(t, "mobile_expo/StatusBar.ios.tsx")
	fe := fileEntity(t, ents)

	// platform_branching — the .ios.tsx filename is itself a platform branch,
	// AND the in-body Platform.OS comparison.
	propContains(t, fe, "platform_branches", "file:ios")
	propContains(t, fe, "platform_branches", "Platform.OS")
}

// ── Ionic ────────────────────────────────────────────────────────────────────

func TestMobileNav_Ionic_NavigationScreensPlatform(t *testing.T) {
	ents := extractFixtureTSX(t, "mobile_ionic/AppRouter.tsx")
	fe := fileEntity(t, ents)

	// navigation_extraction — Ionic router outlet containers.
	propContains(t, fe, "navigators", "IonRouterOutlet")
	propContains(t, fe, "navigators", "IonReactRouter")
	// screen_detection — <Route path=…> declarations.
	for _, scr := range []string{"/home", "/profile", "/settings"} {
		propContains(t, fe, "screens", scr)
		if !hasNavVia(fe, scr, "screen_config") {
			t.Errorf("expected NAVIGATES_TO route:%s via=screen_config", scr)
		}
	}
	// native_module_imports — @capacitor/* import.
	if !hasNativeImportEdge(fe, "@capacitor/geolocation") {
		t.Errorf("expected native_module IMPORTS edge for @capacitor/geolocation; rels=%v", fe.Relationships)
	}
	// platform_branching — isPlatform('ios') + Capacitor.getPlatform().
	propContains(t, fe, "platform_branches", "isPlatform('ios')")
	propContains(t, fe, "platform_branches", "Capacitor.getPlatform()")
}

func TestMobileNav_Ionic_DeepLinks(t *testing.T) {
	ents := extractFixture(t, "mobile_ionic/deepLinks.ts")
	fe := fileEntity(t, ents)

	// deep_link_extraction — Capacitor App.addListener('appUrlOpen').
	propContains(t, fe, "deep_link_prefixes", "appUrlOpen")
	if !hasNavVia(fe, "appUrlOpen", "deep_link") {
		t.Error("expected NAVIGATES_TO route:appUrlOpen via=deep_link")
	}
	if !hasNativeImportEdge(fe, "@capacitor/app") {
		t.Errorf("expected native_module IMPORTS edge for @capacitor/app; rels=%v", fe.Relationships)
	}
}

// ── NativeScript ─────────────────────────────────────────────────────────────

func TestMobileNav_NativeScript_NavigationDeepLinkNativePlatform(t *testing.T) {
	ents := extractFixture(t, "mobile_nativescript/nav-service.ts")
	fe := fileEntity(t, ents)

	// navigation_extraction — registerElement + frame.navigate.
	propContains(t, fe, "navigators", "registerElement:CardView")
	// screen_detection — frame.navigate({ moduleName }) destinations.
	for _, scr := range []string{"home-page", "profile-page"} {
		propContains(t, fe, "screens", scr)
		if !hasNavVia(fe, scr, "screen_config") {
			t.Errorf("expected NAVIGATES_TO route:%s via=screen_config", scr)
		}
	}
	// deep_link_extraction — handleOpenURL.
	propContains(t, fe, "deep_link_prefixes", "handleOpenURL")
	if !hasNavVia(fe, "handleOpenURL", "deep_link") {
		t.Error("expected NAVIGATES_TO route:handleOpenURL via=deep_link")
	}
	// native_module_imports — @nativescript/core import.
	if !hasNativeImportEdge(fe, "@nativescript/core") {
		t.Errorf("expected native_module IMPORTS edge for @nativescript/core; rels=%v", fe.Relationships)
	}
	// platform_branching — isIOS flag + Device.os.
	propContains(t, fe, "platform_branches", "isIOS")
	propContains(t, fe, "platform_branches", "Device.os")
}
