// Dart entry-point sniffer (Phase 1B T3 — #4035).
//
// Recognises:
//   - top-level `void main(` / `main(` / `Future<void> main(` → cli_main
//     (every Dart program — Flutter app, CLI, server bootstrap — has a
//     top-level main; it is the canonical reachability root)
//   - `runApp(` (Flutter app launch, usually inside main) →
//     framework_lifecycle
//   - dart_frog / shelf / serverpod server handlers:
//     `Response onRequest(` (dart_frog), `Response <name>(Request ` /
//     `Future<Response> <name>(Request ` (shelf-style handler) →
//     framework_lifecycle (the framework invokes these without an
//     in-graph caller)
//   - `Isolate.spawn(handler, ...)` → the spawned handler is an entry
//     point reachable only via the isolate runtime → framework_lifecycle
//   - test bodies: `test('...', () {` / `testWidgets('...', (` /
//     `group('...', () {` (package:test / flutter_test) → test_entry
//
// The reachability pass in internal/links/reachability.go consumes these
// entry points and seeds a BFS across CALLS edges, which lets the
// dead-code pass stop over-reporting Dart entities that are only reached
// through main / framework lifecycle / isolate spawn.
package substrate

import "regexp"

func init() { RegisterEntryPoints("dart", sniffDartEntryPoints) }

// dartMainRe matches a top-level `main` function declaration:
//
//	void main() { ... }
//	void main(List<String> args) { ... }
//	Future<void> main() async { ... }
//	main() => runApp(App());
//
// Anchored to column 0 (no leading whitespace) so it only fires on the
// top-level entry, never on a `main` method nested inside a class body.
// Capture group 1 = "main".
var dartMainRe = regexp.MustCompile(
	`(?m)^(?:Future\s*<\s*void\s*>\s+|void\s+|FutureOr\s*<\s*void\s*>\s+)?(main)\s*\(`,
)

// dartRunAppRe matches the Flutter `runApp(` launch call.
var dartRunAppRe = regexp.MustCompile(
	`\brunApp\s*\(`,
)

// dartFrogHandlerRe matches a dart_frog route handler:
//
//	Response onRequest(RequestContext context) { ... }
//	Future<Response> onRequest(RequestContext context) async { ... }
//
// Capture group 1 = "onRequest".
var dartFrogHandlerRe = regexp.MustCompile(
	`(?m)^\s*(?:Future\s*<\s*Response\s*>|Response)\s+(onRequest)\s*\(`,
)

// dartShelfHandlerRe matches a shelf-style handler — any function whose
// first parameter is a `Request`:
//
//	Response handler(Request request) { ... }
//	Future<Response> _router(Request req) async { ... }
//
// Capture group 1 = the handler name. Excludes `onRequest`, which is
// attributed to dart_frog above (dedup also guards this).
var dartShelfHandlerRe = regexp.MustCompile(
	`(?m)^\s*(?:Future\s*<\s*Response\s*>|Response)\s+([A-Za-z_$][\w$]*)\s*\(\s*Request\b`,
)

// dartIsolateSpawnRe matches `Isolate.spawn(handler, ...)`. Capture
// group 1 = the spawned entry-point function name.
var dartIsolateSpawnRe = regexp.MustCompile(
	`\bIsolate\s*\.\s*spawn\s*\(\s*([A-Za-z_$][\w$]*)\s*,`,
)

// dartTestRe matches package:test / flutter_test entry shapes:
//
//	test('description', () { ... });
//	testWidgets('description', (tester) async { ... });
//	group('description', () { ... });
//
// Capture group 1 = the keyword (test/testWidgets/group), group 2 = the
// test description string. The description becomes the entry Ident so
// distinct tests in one file stay distinct.
var dartTestRe = regexp.MustCompile(
	`\b(test|testWidgets|group)\s*\(\s*(?:'([^']*)'|"([^"]*)")\s*,`,
)

func sniffDartEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	seen := map[string]bool{}

	add := func(kindKey, ident string, kind EntryKind, line int) {
		key := kindKey + ":" + ident
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, EntryPoint{Ident: ident, Line: line, Kind: kind})
	}

	// top-level main → cli_main
	for _, m := range dartMainRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		add("cli", content[m[2]:m[3]], EntryKindCLIMain, lineOfOffset(content, m[0]))
	}

	// runApp( → framework_lifecycle (synthetic ident)
	for _, m := range dartRunAppRe.FindAllStringIndex(content, -1) {
		add("lifecycle", "runApp", EntryKindFrameworkLifecycle, lineOfOffset(content, m[0]))
	}

	// dart_frog onRequest → framework_lifecycle
	for _, m := range dartFrogHandlerRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		add("lifecycle", content[m[2]:m[3]], EntryKindFrameworkLifecycle, lineOfOffset(content, m[0]))
	}

	// shelf-style Request handlers → framework_lifecycle
	for _, m := range dartShelfHandlerRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		add("lifecycle", content[m[2]:m[3]], EntryKindFrameworkLifecycle, lineOfOffset(content, m[0]))
	}

	// Isolate.spawn(handler, ...) → framework_lifecycle
	for _, m := range dartIsolateSpawnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		add("lifecycle", content[m[2]:m[3]], EntryKindFrameworkLifecycle, lineOfOffset(content, m[0]))
	}

	// test / testWidgets / group → test_entry
	for _, m := range dartTestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		var desc string
		if m[4] >= 0 {
			desc = content[m[4]:m[5]]
		} else if m[6] >= 0 {
			desc = content[m[6]:m[7]]
		}
		if desc == "" {
			continue
		}
		add("test", desc, EntryKindTestEntry, lineOfOffset(content, m[0]))
	}

	return out
}
