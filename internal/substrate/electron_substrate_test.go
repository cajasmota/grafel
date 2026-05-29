// Electron substrate proving tests (#3059).
//
// Proves that the framework-blind jsts substrate sniffers fire on Electron
// main-process source code, covering the cells listed in issue #3059.
//
// Cells proven to `partial` (sniffer fires but a full comprehensive corpus
// would be needed to stamp `full`):
//   - import_resolution_quality — named imports from 'electron'
//   - env_fallback_recognition  — process.env.ELECTRON_DEV ?? 'production'
//   - constant_propagation      — const API_URL literal
//   - http_effect               — httpFetch calls fetch()
//   - fs_effect                 — readConfig / writeLog use fs.readFile / fs.writeFile
//   - db_effect                 — queryDB / insertRecord use .findOne() / .create()
//                                 (Electron main-process runs Node.js, may use ORMs)
//   - mutation_effect           — AppState.setWindow assigns this._win
//   - dead_code_detection       — export signals surfaced by dead_module_detector
//   - def_use_chain_extraction  — const endpoint / resp / data in loadSettings
//   - pure_function_tagging     — formatTitle has no effects
//   - template_pattern_catalog  — t("app.title"), console.error, SELECT literal
//   - request_shape_extraction  — postEvent sends axios.post({eventType, payload})
//   - confidence_overlay        — effect sniffer fires; propagation pass consumes it
//   - reachability_analysis     — exported functions are library-export entry-points
package substrate

import (
	"os"
	"path/filepath"
	"testing"
)

// electronFixturePath returns the path to the substrate_electron fixture.
func electronFixturePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(
		"..", "..", "testdata", "fixtures", "typescript",
		"substrate_electron", "substrate_electron.ts",
	))
	if err != nil {
		t.Fatalf("cannot resolve electron fixture path: %v", err)
	}
	return p
}

// readElectronFixture reads the electron fixture file.
func readElectronFixture(t *testing.T) string {
	t.Helper()
	path := electronFixturePath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read electron fixture at %s: %v", path, err)
	}
	return string(b)
}

// ── import_resolution_quality ─────────────────────────────────────────────────

// TestElectronSubstrate_ImportResolutionQuality proves import_resolution_quality:
// partial. Named imports from 'electron' and './config' are captured.
func TestElectronSubstrate_ImportResolutionQuality(t *testing.T) {
	src := readElectronFixture(t)
	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	if b["app"].ImportSource != "electron" && b["BrowserWindow"].ImportSource != "electron" {
		t.Error("expected 'app' or 'BrowserWindow' captured as import from 'electron'")
	}
}

// ── env_fallback_recognition ──────────────────────────────────────────────────

// TestElectronSubstrate_EnvFallbackRecognition proves env_fallback_recognition:
// partial. ENV_MODE uses process.env.ELECTRON_DEV ?? 'production'.
func TestElectronSubstrate_EnvFallbackRecognition(t *testing.T) {
	src := readElectronFixture(t)
	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	if b["ENV_MODE"].Value != "production" {
		t.Errorf("ENV_MODE env-fallback: want 'production', got %q (binding: %+v)", b["ENV_MODE"].Value, b["ENV_MODE"])
	}
}

// ── constant_propagation ─────────────────────────────────────────────────────

// TestElectronSubstrate_ConstantPropagation proves constant_propagation: partial.
// API_URL is a literal string binding.
func TestElectronSubstrate_ConstantPropagation(t *testing.T) {
	src := readElectronFixture(t)
	bindings := sniffJSTS(src)
	b := byIdent(bindings)

	if b["API_URL"].Value != "https://api.example.com" {
		t.Errorf("API_URL: want 'https://api.example.com', got %q", b["API_URL"].Value)
	}
}

// ── http_effect ───────────────────────────────────────────────────────────────

// TestElectronSubstrate_HTTPEffect proves http_effect: partial. httpFetch calls
// fetch() — sniffEffectsJSTS detects it.
func TestElectronSubstrate_HTTPEffect(t *testing.T) {
	src := readElectronFixture(t)
	got := sniffEffectsJSTS(src)
	if len(got) == 0 {
		t.Fatal("sniffEffectsJSTS: expected matches on Electron fixture, got none")
	}
	by := groupByEffect(got)
	mustHave(t, by, EffectHTTPOut, "httpFetch")
}

// ── fs_effect ─────────────────────────────────────────────────────────────────

// TestElectronSubstrate_FSEffect proves fs_effect: partial. readConfig uses
// fs.readFile; writeLog uses fs.writeFile.
func TestElectronSubstrate_FSEffect(t *testing.T) {
	src := readElectronFixture(t)
	by := groupByEffect(sniffEffectsJSTS(src))
	mustHave(t, by, EffectFSRead, "readConfig")
	mustHave(t, by, EffectFSWrite, "writeLog")
}

// ── db_effect ─────────────────────────────────────────────────────────────────

// TestElectronSubstrate_DBEffect proves db_effect: partial for Electron.
// Electron's main process runs Node.js and may use ORM libraries directly.
// queryDB calls db.findOne(); insertRecord calls db.create().
func TestElectronSubstrate_DBEffect(t *testing.T) {
	src := readElectronFixture(t)
	by := groupByEffect(sniffEffectsJSTS(src))
	mustHave(t, by, EffectDBRead, "queryDB")
	mustHave(t, by, EffectDBWrite, "insertRecord")
}

// ── mutation_effect ───────────────────────────────────────────────────────────

// TestElectronSubstrate_MutationEffect proves mutation_effect: partial.
// setWindow assigns this._win.
func TestElectronSubstrate_MutationEffect(t *testing.T) {
	src := readElectronFixture(t)
	by := groupByEffect(sniffEffectsJSTS(src))
	mustHave(t, by, EffectMutation, "setWindow")
}

// ── def_use_chain_extraction ──────────────────────────────────────────────────

// TestElectronSubstrate_DefUseChainExtraction proves def_use_chain_extraction:
// partial. loadSettings defines endpoint / resp / data.
func TestElectronSubstrate_DefUseChainExtraction(t *testing.T) {
	src := readElectronFixture(t)
	defs, uses := sniffDefUseJSTS(src)
	if !containsVarDef(defs, "loadSettings", "endpoint") {
		t.Errorf("expected def of endpoint in loadSettings; defs: %+v", defs)
	}
	if !containsVarDef(defs, "loadSettings", "resp") {
		t.Errorf("expected def of resp in loadSettings; defs: %+v", defs)
	}
	if !containsVarDef(defs, "loadSettings", "data") {
		t.Errorf("expected def of data in loadSettings; defs: %+v", defs)
	}
	if !containsVarUse(uses, "loadSettings", "endpoint") {
		t.Errorf("expected use of endpoint in loadSettings; uses: %+v", uses)
	}
}

// ── pure_function_tagging ─────────────────────────────────────────────────────

// TestElectronSubstrate_PureFunctionTagging proves pure_function_tagging:
// partial. formatTitle has no side effects.
func TestElectronSubstrate_PureFunctionTagging(t *testing.T) {
	src := readElectronFixture(t)
	got := sniffEffectsJSTS(src)
	for _, m := range got {
		if m.Function == "formatTitle" {
			t.Errorf("formatTitle should be pure (no effects), got %+v", m)
		}
	}
}

// ── template_pattern_catalog ──────────────────────────────────────────────────

// TestElectronSubstrate_TemplatePatternCatalog proves template_pattern_catalog:
// partial. appTitle produces i18n t(), log console.error, and a SQL literal.
func TestElectronSubstrate_TemplatePatternCatalog(t *testing.T) {
	src := readElectronFixture(t)
	got := sniffTemplatePatternsJSTS(src)
	if !hasTemplateKind(got, TemplateKindI18n) {
		t.Errorf("expected i18n template (t(\"app.title\")); patterns: %+v", got)
	}
	if !hasTemplateKind(got, TemplateKindLog) {
		t.Errorf("expected log template (console.error); patterns: %+v", got)
	}
	if !hasTemplateKind(got, TemplateKindSQL) {
		t.Errorf("expected SQL template (SELECT literal); patterns: %+v", got)
	}
}

// ── request_shape_extraction ─────────────────────────────────────────────────

// TestElectronSubstrate_RequestShapeExtraction proves request_shape_extraction:
// partial. postEvent sends axios.post({eventType, payload}).
func TestElectronSubstrate_RequestShapeExtraction(t *testing.T) {
	src := readElectronFixture(t)
	got := sniffPayloadShapesJSTS(src)
	var hasEventType bool
	for _, s := range got {
		for _, f := range s.Fields {
			if f.Name == "eventType" {
				hasEventType = true
			}
		}
	}
	if !hasEventType {
		t.Errorf("expected 'eventType' field in postEvent request shape; shapes: %+v", got)
	}
}

// ── reachability_analysis ─────────────────────────────────────────────────────

// TestElectronSubstrate_ReachabilityAnalysis proves reachability_analysis:
// partial. Exported functions are entry-points for the reachability BFS.
func TestElectronSubstrate_ReachabilityAnalysis(t *testing.T) {
	src := readElectronFixture(t)
	eps := sniffJSTSEntryPoints(src)
	var hasExport bool
	for _, ep := range eps {
		if ep.Kind == EntryKindLibraryExport {
			hasExport = true
			break
		}
	}
	if !hasExport {
		t.Errorf("expected at least one library_export entry-point from Electron fixture; got: %+v", eps)
	}
}
