package java

import (
	"testing"
)

// ============================================================================
// Issue #3188: Android platform_branching + native_module_imports extractor
// ============================================================================
//
// These dedicated tests prove the two new capabilities added to android.go:
//
//   platform_branching   -> Build.VERSION.SDK_INT API-level comparisons
//   native_module_imports -> <uses-permission>/<uses-feature> android.hardware.*
//                            in AndroidManifest.xml + `import android.hardware.*`
//
// Registry targets (both flipped to partial):
//   lang.java.framework.android-sdk     Platform/platform_branching
//   lang.java.framework.android-sdk     Native Bridge/native_module_imports
//   lang.java.framework.android-jetpack Platform/platform_branching
//   lang.java.framework.android-jetpack Native Bridge/native_module_imports
// Cite: internal/custom/java/android.go

// ----------------------------------------------------------------------------
// platform_branching: Build.VERSION.SDK_INT comparisons
// ----------------------------------------------------------------------------

// androidSdkIntFixture is the golden source proving SDK_INT branch detection.
const androidSdkIntFixture = `package com.example.app;

import android.os.Build;
import android.app.Activity;

public class FeatureGate extends Activity {
    public void apply() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundChannel();
        } else if (Build.VERSION.SDK_INT < 21) {
            legacyPath();
        }
        if (Build.VERSION.SDK_INT == Build.VERSION_CODES.TIRAMISU) {
            notifyPermission();
        }
    }
}
`

func TestAndroid_PlatformBranching_SdkInt_Issue3188(t *testing.T) {
	r := ExtractAndroid(PatternContext{
		Source:    androidSdkIntFixture,
		Language:  "java",
		Framework: "android",
		FilePath:  "FeatureGate.java",
	})

	type branch struct{ op, level string }
	got := make(map[branch]bool)
	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_ANDROID_SDK_INT_BRANCH" {
			continue
		}
		if e.Kind != "SCOPE.Operation" || e.Subtype != "branch" {
			t.Errorf("[#3188 platform_branching] unexpected kind/subtype %s/%s for %s",
				e.Kind, e.Subtype, e.Name)
		}
		if e.Properties["framework"] != "android" {
			t.Errorf("[#3188 platform_branching] missing framework=android on %s", e.Name)
		}
		got[branch{
			e.Properties["operator"].(string),
			e.Properties["api_level"].(string),
		}] = true
	}

	want := []branch{
		{">=", "Build.VERSION_CODES.O"},
		{"<", "21"},
		{"==", "Build.VERSION_CODES.TIRAMISU"},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("[#3188 platform_branching] expected SDK_INT branch %q %q, got %v",
				w.op, w.level, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("[#3188 platform_branching] expected exactly 3 branches, got %d: %v", len(got), got)
	}

	// Each branch site should be OWNED by the enclosing class.
	owns := 0
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "OWNS" &&
			rel.Properties["branch_kind"] == "platform_sdk_int" {
			owns++
		}
	}
	if owns != 3 {
		t.Errorf("[#3188 platform_branching] expected 3 OWNS branch edges, got %d", owns)
	}
}

// ----------------------------------------------------------------------------
// native_module_imports: AndroidManifest hardware permissions/features
// ----------------------------------------------------------------------------

// androidManifestNativeFixture is the golden manifest proving hardware
// permission + feature detection (and that non-hardware permissions are skipped).
const androidManifestNativeFixture = `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="com.example.app">
    <uses-permission android:name="android.hardware.camera" />
    <uses-permission android:name="android.permission.INTERNET" />
    <uses-feature android:name="android.hardware.camera.autofocus" android:required="false" />
    <uses-feature android:name="android.software.leanback" />
    <application android:label="App">
        <activity android:name=".MainActivity" />
    </application>
</manifest>
`

func TestAndroid_NativeModuleImports_Manifest_Issue3188(t *testing.T) {
	r := ExtractAndroid(PatternContext{
		Source:    androidManifestNativeFixture,
		Language:  "java",
		Framework: "android",
		FilePath:  "app/src/main/AndroidManifest.xml",
	})

	type nm struct{ name, decl string }
	got := make(map[nm]bool)
	for _, e := range r.Entities {
		switch e.Provenance {
		case "INFERRED_FROM_ANDROID_USES_PERMISSION",
			"INFERRED_FROM_ANDROID_USES_FEATURE":
			if e.Kind != "SCOPE.Reference" || e.Subtype != "native_module" {
				t.Errorf("[#3188 native_module_imports] unexpected kind/subtype %s/%s for %s",
					e.Kind, e.Subtype, e.Name)
			}
			got[nm{e.Name, e.Properties["declaration_kind"].(string)}] = true
		}
	}

	want := []nm{
		{"android.hardware.camera", "uses-permission"},
		{"android.hardware.camera.autofocus", "uses-feature"},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("[#3188 native_module_imports] expected %q (%s), got %v", w.name, w.decl, got)
		}
	}
	// android.permission.INTERNET and android.software.leanback must NOT match.
	for k := range got {
		if k.name == "android.permission.INTERNET" || k.name == "android.software.leanback" {
			t.Errorf("[#3188 native_module_imports] non-hardware decl leaked: %v", k)
		}
	}
	if len(got) != 2 {
		t.Errorf("[#3188 native_module_imports] expected exactly 2 hardware decls, got %d: %v", len(got), got)
	}
}

// ----------------------------------------------------------------------------
// native_module_imports: android.hardware.* import statements
// ----------------------------------------------------------------------------

const androidHardwareImportFixture = `package com.example.app;

import android.hardware.Camera;
import android.hardware.SensorManager;
import android.hardware.fingerprint.FingerprintManager;
import android.os.Build;
import java.util.List;

public class DeviceProbe {
}
`

func TestAndroid_NativeModuleImports_JavaImports_Issue3188(t *testing.T) {
	r := ExtractAndroid(PatternContext{
		Source:    androidHardwareImportFixture,
		Language:  "java",
		Framework: "android_jetpack",
		FilePath:  "DeviceProbe.java",
	})

	got := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_ANDROID_HARDWARE_IMPORT" {
			continue
		}
		if e.Kind != "SCOPE.Reference" || e.Subtype != "native_module" {
			t.Errorf("[#3188 native_module_imports] unexpected kind/subtype %s/%s for %s",
				e.Kind, e.Subtype, e.Name)
		}
		if e.Properties["declaration_kind"] != "import" {
			t.Errorf("[#3188 native_module_imports] expected declaration_kind=import for %s", e.Name)
		}
		got[e.Name] = true
	}

	want := []string{
		"android.hardware.Camera",
		"android.hardware.SensorManager",
		"android.hardware.fingerprint.FingerprintManager",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("[#3188 native_module_imports] expected import %q, got %v", w, got)
		}
	}
	// android.os.Build and java.util.List must NOT be treated as native modules.
	if got["android.os.Build"] || got["java.util.List"] {
		t.Errorf("[#3188 native_module_imports] non-hardware import leaked: %v", got)
	}
	if len(got) != 3 {
		t.Errorf("[#3188 native_module_imports] expected exactly 3 hardware imports, got %d: %v", len(got), got)
	}
}

// TestAndroid_PlatformNative_GateMiss_Issue3188 proves the new scanners stay
// silent for non-android frameworks (gate respected, no false positives).
func TestAndroid_PlatformNative_GateMiss_Issue3188(t *testing.T) {
	r := ExtractAndroid(PatternContext{
		Source:    androidSdkIntFixture,
		Language:  "java",
		Framework: "spring",
		FilePath:  "FeatureGate.java",
	})
	if len(r.Entities) != 0 || len(r.Relationships) != 0 {
		t.Errorf("[#3188] non-android framework should yield nothing, got %d entities", len(r.Entities))
	}
}
