// Flutter/Dart substrate proving tests (#4034, epic #3872, audit #3888).
//
// CREDIT-WAVE: these tests prove that the framework-blind Dart substrate
// sniffers (gated on .dart via LanguageForPath) fire on canonical Flutter
// idioms, so the corresponding registry cells on lang.dart.framework.flutter
// can be credited honestly. No new extractor code — these are value-asserting
// fixtures for cells whose sniffers shipped without a Flutter-shaped test.
//
// Sibling tests already prove the effect/taint/sanitizer primitives:
//   - effect_sinks_t3_test.go  TestSniffEffectsDart_PrimitiveCoverage
//     (db_read/db_write/mutation/fs_read/fs_write/http_out)
//   - taint_sites_test.go      TestTaintSniffer_Dart_RawQueryIsSink
//     (SQL sink + whereArgs sanitizer)
//
// This file adds the three that lacked a Dart-shaped fixture:
//   - def_use_chain_extraction  (sniffDefUseDart)
//   - template_pattern_catalog  (sniffTemplatePatternsdart)
//   - taint_source_detection    (sniffTaintDart — source side)
package substrate

import "testing"

// TestFlutterSubstrate_DefUseChainExtraction proves def_use_chain_extraction:
// local `final` bindings inside a widget method become defs, and the bare
// identifiers that read them become uses, both attributed to the enclosing
// function. Mirrors the canonical Flutter `build`/handler body.
func TestFlutterSubstrate_DefUseChainExtraction(t *testing.T) {
	const src = `
class ProfileScreen extends StatelessWidget {
  Future<void> loadUser(String userId) async {
    final name = userId;
    final greeting = name;
  }
}
`
	defs, uses := sniffDefUseDart(src)
	if len(defs) == 0 {
		t.Fatal("expected def-use defs on Flutter widget method, got none")
	}
	var sawNameDef, sawGreetingDef bool
	for _, d := range defs {
		if d.Function != "loadUser" {
			t.Errorf("def %q attributed to %q, want loadUser", d.Var, d.Function)
		}
		if d.Var == "name" {
			sawNameDef = true
		}
		if d.Var == "greeting" {
			sawGreetingDef = true
		}
	}
	if !sawNameDef || !sawGreetingDef {
		t.Errorf("expected defs for name and greeting; got %+v", defs)
	}
	// `name` is read by the `greeting` binding — a genuine def->use chain.
	var sawNameUse bool
	for _, u := range uses {
		if u.Var == "name" && u.Function == "loadUser" {
			sawNameUse = true
		}
	}
	if !sawNameUse {
		t.Errorf("expected a use of name (read by greeting); got %+v", uses)
	}
}

// TestFlutterSubstrate_TemplatePatternCatalog proves template_pattern_catalog:
// a Flutter build body using debugPrint (log_format), gen-l10n
// AppLocalizations (i18n), and a sqflite raw SQL literal (sql) yields one
// template pattern per kind.
func TestFlutterSubstrate_TemplatePatternCatalog(t *testing.T) {
	const src = `
class HomeScreen extends StatelessWidget {
  Widget build(BuildContext context) {
    debugPrint("home build");
    final title = AppLocalizations.of(context).translate("home.title");
    db.rawQuery("SELECT id FROM users WHERE active = 1");
    return Container();
  }
}
`
	got := sniffTemplatePatternsdart(src)
	if len(got) == 0 {
		t.Fatal("expected template patterns on Flutter build body, got none")
	}
	kinds := map[TemplateKind]string{}
	for _, p := range got {
		kinds[p.Kind] = p.Literal
	}
	if kinds[TemplateKindLog] != "home build" {
		t.Errorf("log_format: got %q, want %q", kinds[TemplateKindLog], "home build")
	}
	if kinds[TemplateKindI18n] != "home.title" {
		t.Errorf("i18n: got %q, want %q", kinds[TemplateKindI18n], "home.title")
	}
	if kinds[TemplateKindSQL] == "" {
		t.Errorf("expected an sql template literal; got %+v", got)
	}
}

// TestFlutterSubstrate_TaintSourceDetection proves taint_source_detection:
// a Dart server-side / shelf-style HttpRequest query-parameter read is
// flagged as a taint source. (The sink + sanitizer sides are proven by
// TestTaintSniffer_Dart_RawQueryIsSink in taint_sites_test.go.)
func TestFlutterSubstrate_TaintSourceDetection(t *testing.T) {
	const src = `
Future<Response> handle(HttpRequest request) async {
  final id = request.uri.queryParameters['id'];
  return Response.ok(id);
}
`
	var hasSrc bool
	for _, m := range sniffTaintDart(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Error("expected request.uri.queryParameters to be flagged as a taint source")
	}
}
