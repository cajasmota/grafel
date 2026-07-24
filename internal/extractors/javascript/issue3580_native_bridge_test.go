// Package javascript_test — issue #3580 React Native native-bridge deepening
// (epic #3571).
//
// The pre-existing #2860 pass recorded native-bridge access as a summary
// `native_modules` property string on the file entity. This pass makes the
// JS↔native boundary first-class: each distinct native module / native
// component reached across the bridge becomes a SCOPE.External entity (subtype
// native_module | native_component) with a DEPENDS_ON edge from the file
// entity. New-architecture (TurboModuleRegistry, codegenNativeComponent) and
// legacy (NativeModules destructuring, requireNativeComponent) surfaces, plus
// Expo modules-core (requireNativeModule), are all covered.
//
// Every assertion names a SPECIFIC module/component (value-asserting), proving
// the Native Bridge coverage cell for react-native / expo.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// nativeBridgeEntity returns the SCOPE.External entity with the given name and
// subtype, or nil.
func nativeBridgeEntity(ents []types.EntityRecord, name, subtype string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == "SCOPE.External" && e.Name == name && e.Subtype == subtype &&
			e.Properties["native_bridge"] == "1" {
			return e
		}
	}
	return nil
}

// hasDependsOnNative asserts the file entity has a DEPENDS_ON edge to the named
// native module/component carrying the expected subtype.
func hasDependsOnNative(fe *types.EntityRecord, name, subtype string) bool {
	for _, r := range fe.Relationships {
		if r.Kind == "DEPENDS_ON" && r.ToID == "native_module:"+name &&
			r.Properties.Get("subtype") == subtype && r.Properties.Get("native_bridge") == "1" {
			return true
		}
	}
	return false
}

func TestNativeBridge_ReactNative_ModulesAndComponents(t *testing.T) {
	ents := extractFixtureTSX(t, "mobile_react_native/NativeBridge.tsx")
	fe := fileEntity(t, ents)

	cases := []struct {
		name    string
		subtype string
	}{
		{"BiometricAuth", "native_module"}, // const { BiometricAuth } = NativeModules
		{"RNDeviceInfo", "native_module"},  // TurboModuleRegistry.getEnforcing('RNDeviceInfo')
		{"ExpoBattery", "native_module"},   // requireNativeModule('ExpoBattery')
		{"RCTMapView", "native_component"}, // requireNativeComponent('RCTMapView')
		{"RCTWebView", "native_component"}, // codegenNativeComponent('RCTWebView')
	}
	for _, tc := range cases {
		if nativeBridgeEntity(ents, tc.name, tc.subtype) == nil {
			t.Errorf("expected SCOPE.External native-bridge entity %q subtype %q", tc.name, tc.subtype)
		}
		if !hasDependsOnNative(fe, tc.name, tc.subtype) {
			t.Errorf("expected DEPENDS_ON native_module:%s subtype=%s on file entity", tc.name, tc.subtype)
		}
	}

	// The discovery `via` is recorded on the entity so the bridge mechanism is
	// queryable (legacy NativeModules vs new-arch TurboModuleRegistry vs codegen).
	if e := nativeBridgeEntity(ents, "RNDeviceInfo", "native_module"); e != nil {
		if e.Properties["via"] != "TurboModuleRegistry.getEnforcing" {
			t.Errorf("RNDeviceInfo via=%q, want TurboModuleRegistry.getEnforcing", e.Properties["via"])
		}
	}
	if e := nativeBridgeEntity(ents, "RCTWebView", "native_component"); e != nil {
		if e.Properties["via"] != "codegenNativeComponent" {
			t.Errorf("RCTWebView via=%q, want codegenNativeComponent", e.Properties["via"])
		}
	}
}

// The original #2860 RN fixture uses the destructured NativeModules form; prove
// the named module is now surfaced as a bridge entity there too (regression-
// proofs the destructuring path against real fixture shape).
func TestNativeBridge_ReactNative_DestructuredFromAppNavigator(t *testing.T) {
	ents := extractFixtureTSX(t, "mobile_react_native/AppNavigator.tsx")
	fe := fileEntity(t, ents)

	if nativeBridgeEntity(ents, "BiometricAuth", "native_module") == nil {
		t.Error("expected SCOPE.External native_module entity BiometricAuth from AppNavigator destructuring")
	}
	if !hasDependsOnNative(fe, "BiometricAuth", "native_module") {
		t.Error("expected DEPENDS_ON native_module:BiometricAuth on AppNavigator file entity")
	}
}
