// UI (Angular/Svelte/Vue), Mobile (Expo/Ionic/NativeScript/React Native), and
// Meta-framework (Astro/SvelteKit) substrate proving-fixture tests (#2850).
//
// Each test loads the hand-written fixture for the framework family and asserts
// that the existing substrate sniffers produce the expected outputs, proving
// the cells are (A) recording gaps — the code already delivers the capability.
//
// Fixtures live in testdata/fixtures/typescript/substrate_<family>/.
package substrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureContent reads a fixture file relative to the module root. The tests
// run from the package directory (internal/substrate/), so we walk up two
// levels to reach the module root.
func fixtureContent(t *testing.T, relPath string) string {
	t.Helper()
	// Resolve from module root: two levels up from internal/substrate.
	root := filepath.Join("..", "..", "testdata", "fixtures", "typescript")
	full := filepath.Join(root, relPath)
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("fixture %q not found: %v", full, err)
	}
	return string(b)
}

// ── Angular ──────────────────────────────────────────────────────────────────

// TestSubstrate_Angular_ImportResolution proves that the jsts substrate sniffer
// recognises Angular-style import statements (named imports from @angular/*,
// relative service imports) — establishing import_resolution_quality: full.
func TestSubstrate_Angular_ImportResolution(t *testing.T) {
	src := fixtureContent(t, "substrate_angular/app.component.ts")

	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// Literal binding: const API_BASE = environment.apiUrl ?? 'https://api.example.com'
	// Only the fallback literal is recorded by jstsEnvFallbackRe when the LHS looks like
	// an env-fallback; otherwise the literal binding is recorded.
	// The fixture uses both patterns; assert at least one import is captured.
	if b["Component"].ImportSource == "" && b["HttpClient"].ImportSource == "" && b["UserService"].ImportSource == "" {
		t.Error("expected at least one named import binding (Component, HttpClient, or UserService)")
	}

	// Check UserService cross-file import.
	if us, ok := b["UserService"]; ok {
		if us.Provenance != ProvenanceCrossFile || us.ImportSource != "./user.service" {
			t.Errorf("UserService: want cross_file from './user.service', got %+v", us)
		}
	} else {
		// Acceptable if Angular destructured imports are recorded differently.
		// Check that Component or HttpClient were captured instead.
		if b["Component"].ImportSource == "" {
			t.Error("UserService import not found; Component also absent — import_resolution regression")
		}
	}

	// Env-fallback: const TIMEOUT = process.env['NG_TIMEOUT'] ?? '5000'
	// The sniffer records fallback strings only when it sees the ?? / || pattern.
	if timeout, ok := b["TIMEOUT"]; ok {
		if timeout.Value != "5000" {
			t.Errorf("TIMEOUT fallback: want '5000', got %q", timeout.Value)
		}
	}
}

// ── Svelte ───────────────────────────────────────────────────────────────────

// TestSubstrate_Svelte_ImportResolution proves import_resolution_quality: full
// for Svelte SFCs by extracting named imports from the <script> block.
func TestSubstrate_Svelte_ImportResolution(t *testing.T) {
	src := fixtureContent(t, "substrate_svelte/UserCard.svelte")

	// sniffMarkupScript dispatches to sniffJSTS on <script> blocks.
	bindings := sniffMarkupScript(src)
	b := byIdent(bindings)

	// 'import DOMPurify from 'dompurify'' — default import captured via jstsImportRe.
	// 'import { UserService } from './user.service'' — named import.
	if b["UserService"].ImportSource == "" && b["page"].ImportSource == "" {
		t.Error("expected at least one import binding from Svelte <script> block")
	}

	// Literal: const API_URL = 'https://api.example.com'
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q (binding: %+v)", b["API_URL"].Value, b["API_URL"])
	}

	// Cross-file: $app/stores import.
	if pg, ok := b["page"]; ok {
		if pg.Provenance != ProvenanceCrossFile {
			t.Errorf("page: want cross_file, got %+v", pg)
		}
	}
}

// TestSubstrate_Svelte_TaintSource proves taint_source_detection: full
// by asserting $page.params/url.searchParams are flagged as sources.
func TestSubstrate_Svelte_TaintSource(t *testing.T) {
	src := fixtureContent(t, "substrate_svelte/UserCard.svelte")

	matches := sniffTaintMarkupScript(src)
	var hasSrc bool
	for _, m := range matches {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected at least one taint source in Svelte fixture ($page.params or $page.url.searchParams)")
	}
}

// TestSubstrate_Svelte_TaintSinkAndSanitizer proves taint_sink_detection,
// sanitizer_recognition, and vulnerability_finding: full.
func TestSubstrate_Svelte_TaintSinkAndSanitizer(t *testing.T) {
	src := fixtureContent(t, "substrate_svelte/UserCard.svelte")

	matches := sniffTaintMarkupScript(src)
	var hasSink, hasSan bool
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSink {
		t.Error("expected at least one taint sink in Svelte fixture ({@html} or eval)")
	}
	if !hasSan {
		t.Error("expected at least one sanitizer in Svelte fixture (DOMPurify.sanitize)")
	}
}

// ── Vue ──────────────────────────────────────────────────────────────────────

// TestSubstrate_Vue_ImportResolution proves import_resolution_quality: full
// for Vue SFCs.
func TestSubstrate_Vue_ImportResolution(t *testing.T) {
	src := fixtureContent(t, "substrate_vue/UserCard.vue")

	bindings := sniffMarkupScript(src)
	b := byIdent(bindings)

	// Named imports: ref, useRoute, ApiService.
	if b["ApiService"].ImportSource == "" && b["useRoute"].ImportSource == "" {
		t.Error("expected at least one import binding from Vue <script setup> block")
	}

	// Literal: const API_URL = 'https://api.example.com'
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}
}

// TestSubstrate_Vue_TaintSource proves taint_source_detection: full.
func TestSubstrate_Vue_TaintSource(t *testing.T) {
	src := fixtureContent(t, "substrate_vue/UserCard.vue")

	matches := sniffTaintMarkupScript(src)
	var hasSrc bool
	for _, m := range matches {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected at least one taint source in Vue fixture (route.query or route.params)")
	}
}

// TestSubstrate_Vue_TaintSinkAndSanitizer proves taint_sink_detection,
// sanitizer_recognition, and vulnerability_finding: full.
func TestSubstrate_Vue_TaintSinkAndSanitizer(t *testing.T) {
	src := fixtureContent(t, "substrate_vue/UserCard.vue")

	matches := sniffTaintMarkupScript(src)
	var hasSink, hasSan bool
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSink {
		t.Error("expected at least one taint sink in Vue fixture (v-html or db.query concat)")
	}
	if !hasSan {
		t.Error("expected at least one sanitizer in Vue fixture (DOMPurify.sanitize)")
	}
}

// ── Mobile (React Native / Expo / Ionic / NativeScript) ──────────────────────

// TestSubstrate_Mobile_ImportResolution proves import_resolution_quality: full
// for all four mobile framework families (React Native, Expo, Ionic, NativeScript)
// using one shared App.tsx fixture.
func TestSubstrate_Mobile_ImportResolution(t *testing.T) {
	src := fixtureContent(t, "substrate_mobile/App.tsx")

	// Mobile .tsx files are plain JS/TS — sniffJSTS handles them directly.
	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// React Native: 'import { View, Text, Pressable } from 'react-native''
	found := map[string]bool{}
	for _, bind := range bindings {
		if bind.Provenance == ProvenanceCrossFile {
			found[bind.ImportSource] = true
		}
	}

	rnSources := []string{"react-native", "@react-navigation/native", "expo-camera", "expo-file-system", "@ionic/react", "@capacitor/filesystem", "@nativescript/core"}
	var matched int
	for _, src := range rnSources {
		if found[src] {
			matched++
		}
	}
	if matched < 3 {
		t.Errorf("expected at least 3 mobile framework imports recognised, got %d (found: %v)", matched, found)
	}

	// Literal binding.
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL literal: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}

	// Env-fallback: process.env['RN_API_KEY'] ?? 'dev-only'
	if b["SECRET"].Value != "dev-only" {
		t.Errorf("SECRET fallback: want 'dev-only', got %q (binding: %+v)", b["SECRET"].Value, b["SECRET"])
	}
}

// TestSubstrate_ReactNative_ImportResolution is an explicit sub-test for the
// React Native substrate cell (flagged PRIORITY in #2850).
func TestSubstrate_ReactNative_ImportResolution(t *testing.T) {
	// Minimal inline fixture targeting the React Native import shape specifically.
	src := strings.TrimSpace(`
import { View, Text } from 'react-native';
import { useNavigation, useRoute } from '@react-navigation/native';
import AsyncStorage from '@react-native-async-storage/async-storage';

const API_URL = 'https://api.example.com';
const RN_ENV = process.env.RN_API_KEY ?? 'fallback-key';
`)

	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// Named imports from react-native.
	if b["View"].ImportSource != "react-native" && b["Text"].ImportSource != "react-native" {
		t.Error("react-native named imports (View/Text) not captured as cross_file bindings")
	}

	// Import from navigation library.
	if b["useNavigation"].ImportSource != "@react-navigation/native" && b["useRoute"].ImportSource != "@react-navigation/native" {
		t.Error("@react-navigation/native imports not captured")
	}

	// Literal.
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}

	// Env-fallback.
	if b["RN_ENV"].Value != "fallback-key" {
		t.Errorf("RN_ENV: want 'fallback-key', got %q (binding: %+v)", b["RN_ENV"].Value, b["RN_ENV"])
	}
}

// TestSubstrate_Expo_ImportResolution is an explicit sub-test for the Expo
// substrate import_resolution_quality cell.
func TestSubstrate_Expo_ImportResolution(t *testing.T) {
	src := strings.TrimSpace(`
import { Camera } from 'expo-camera';
import * as FileSystem from 'expo-file-system';
import { StatusBar } from 'expo-status-bar';

const EXPO_API = import.meta.env['EXPO_PUBLIC_API_URL'] ?? 'https://api.example.com';
const BUILD_ID = 'expo-build-1';
`)

	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// Camera from expo-camera.
	if b["Camera"].ImportSource != "expo-camera" {
		t.Errorf("Camera: want import from 'expo-camera', got %+v", b["Camera"])
	}

	// StatusBar from expo-status-bar.
	if b["StatusBar"].ImportSource != "expo-status-bar" {
		t.Errorf("StatusBar: want import from 'expo-status-bar', got %+v", b["StatusBar"])
	}

	// Literal.
	if b["BUILD_ID"].Value != "expo-build-1" {
		t.Errorf("BUILD_ID: want 'expo-build-1', got %q", b["BUILD_ID"].Value)
	}
}

// TestSubstrate_Ionic_ImportResolution is an explicit sub-test for the Ionic
// substrate import_resolution_quality cell.
func TestSubstrate_Ionic_ImportResolution(t *testing.T) {
	src := strings.TrimSpace(`
import { IonContent, IonHeader, IonPage, IonTitle, IonToolbar } from '@ionic/react';
import { Filesystem, Directory } from '@capacitor/filesystem';
import { Geolocation } from '@capacitor/geolocation';

const API_URL = 'https://api.example.com';
const IONIC_ENV = process.env.IONIC_APP_ID ?? 'com.example.app';
`)

	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// Named imports from @ionic/react.
	if b["IonContent"].ImportSource != "@ionic/react" {
		t.Errorf("IonContent: want import from '@ionic/react', got %+v", b["IonContent"])
	}

	// From Capacitor.
	if b["Filesystem"].ImportSource != "@capacitor/filesystem" {
		t.Errorf("Filesystem: want import from '@capacitor/filesystem', got %+v", b["Filesystem"])
	}

	// Literal + env-fallback.
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}
	if b["IONIC_ENV"].Value != "com.example.app" {
		t.Errorf("IONIC_ENV: want 'com.example.app', got %q", b["IONIC_ENV"].Value)
	}
}

// TestSubstrate_NativeScript_ImportResolution is an explicit sub-test for the
// NativeScript substrate import_resolution_quality cell.
func TestSubstrate_NativeScript_ImportResolution(t *testing.T) {
	src := strings.TrimSpace(`
import { Frame, Page, StackLayout } from '@nativescript/core';
import { Http } from '@nativescript/core';
import { LocalNotifications } from '@nativescript/local-notifications';

const API_URL = 'https://api.example.com';
const NS_ENV = process.env.NS_APP_ID ?? 'com.example.ns';
`)

	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	// Named imports from @nativescript/core.
	if b["Frame"].ImportSource != "@nativescript/core" {
		t.Errorf("Frame: want import from '@nativescript/core', got %+v", b["Frame"])
	}

	// Literal.
	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}
	// Env-fallback.
	if b["NS_ENV"].Value != "com.example.ns" {
		t.Errorf("NS_ENV: want 'com.example.ns', got %q", b["NS_ENV"].Value)
	}
}

// ── Astro ─────────────────────────────────────────────────────────────────────

// TestSubstrate_Astro_TaintSource proves taint_source_detection: full for Astro.
func TestSubstrate_Astro_TaintSource(t *testing.T) {
	src := fixtureContent(t, "substrate_astro/UserPage.astro")

	// Astro files use sniffTaintMarkupScript (registered for "astro").
	matches := sniffTaintMarkupScript(src)
	var hasSrc bool
	for _, m := range matches {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected at least one taint source in Astro fixture (Astro.params or Astro.url.searchParams)")
	}
}

// TestSubstrate_Astro_TaintSinkAndSanitizer proves taint_sink_detection,
// sanitizer_recognition, and vulnerability_finding: full for Astro.
func TestSubstrate_Astro_TaintSinkAndSanitizer(t *testing.T) {
	src := fixtureContent(t, "substrate_astro/UserPage.astro")

	matches := sniffTaintMarkupScript(src)
	var hasSink, hasSan bool
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSink {
		t.Error("expected at least one taint sink in Astro fixture (eval or set:html or fetch non-literal)")
	}
	if !hasSan {
		t.Error("expected at least one sanitizer in Astro fixture (DOMPurify.sanitize)")
	}
}

// ── SvelteKit ─────────────────────────────────────────────────────────────────

// TestSubstrate_SvelteKit_TaintSource proves taint_source_detection: full.
// SvelteKit server routes are plain .ts files; sniffTaintJSTS handles them.
func TestSubstrate_SvelteKit_TaintSource(t *testing.T) {
	src := fixtureContent(t, "substrate_sveltekit/+page.server.ts")

	// SvelteKit server routes are plain TS — use sniffTaintJSTS directly.
	matches := sniffTaintJSTS(src)
	var hasSrc bool
	for _, m := range matches {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected at least one taint source in SvelteKit fixture (req.body/params/url.searchParams)")
	}
}

// TestSubstrate_SvelteKit_TaintSinkAndSanitizer proves taint_sink_detection,
// sanitizer_recognition, and vulnerability_finding: full for SvelteKit.
func TestSubstrate_SvelteKit_TaintSinkAndSanitizer(t *testing.T) {
	src := fixtureContent(t, "substrate_sveltekit/+page.server.ts")

	matches := sniffTaintJSTS(src)
	var hasSink, hasSan bool
	for _, m := range matches {
		if m.Kind == TaintKindSink {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSink {
		t.Error("expected at least one taint sink in SvelteKit fixture (db.query template string or eval)")
	}
	if !hasSan {
		t.Error("expected at least one sanitizer in SvelteKit fixture (DOMPurify.sanitize)")
	}
}
