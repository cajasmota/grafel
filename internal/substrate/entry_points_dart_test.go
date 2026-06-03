// Entry-points sniffer proving tests for Dart (Phase 1B T3 — #4035).
//
// Cells proven to partial:
//   - reachability_analysis  (entry_points_dart sniffer fires on main /
//     runApp / dart_frog onRequest / Isolate.spawn)
//   - dead_code_detection    (reachability BFS seeds from these entries,
//     so Flutter/server entities reached only via main stop reading as
//     dead)
//   - pure_function_tagging  (effect-free non-entry functions tag pure;
//     entries anchor the reachable set the pure pass runs over)
package substrate

import "testing"

func TestDartEntryPoints_MainAndRunApp(t *testing.T) {
	src := `
import 'package:flutter/material.dart';

void main() => runApp(const MyApp());

class MyApp extends StatelessWidget {}
`
	eps := sniffDartEntryPoints(src)
	var hasMain, hasRunApp bool
	for _, ep := range eps {
		if ep.Kind == EntryKindCLIMain && ep.Ident == "main" {
			hasMain = true
		}
		if ep.Kind == EntryKindFrameworkLifecycle && ep.Ident == "runApp" {
			hasRunApp = true
		}
	}
	if !hasMain {
		t.Error("expected cli_main entry for top-level void main()")
	}
	if !hasRunApp {
		t.Error("expected framework_lifecycle entry for runApp()")
	}
}

func TestDartEntryPoints_AsyncMain(t *testing.T) {
	src := `
Future<void> main(List<String> args) async {
  await bootstrap();
}
`
	eps := sniffDartEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindCLIMain && ep.Ident == "main" {
			found = true
		}
	}
	if !found {
		t.Error("expected cli_main entry for Future<void> main()")
	}
}

func TestDartEntryPoints_DartFrogHandler(t *testing.T) {
	src := `
import 'package:dart_frog/dart_frog.dart';

Response onRequest(RequestContext context) {
  return Response(body: 'ok');
}
`
	eps := sniffDartEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle && ep.Ident == "onRequest" {
			found = true
		}
	}
	if !found {
		t.Error("expected framework_lifecycle entry for dart_frog onRequest")
	}
}

func TestDartEntryPoints_ShelfHandler(t *testing.T) {
	src := `
Future<Response> echoHandler(Request request) async {
  return Response.ok('echo');
}
`
	eps := sniffDartEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle && ep.Ident == "echoHandler" {
			found = true
		}
	}
	if !found {
		t.Error("expected framework_lifecycle entry for shelf Request handler")
	}
}

func TestDartEntryPoints_IsolateSpawn(t *testing.T) {
	src := `
void startWorker() {
  Isolate.spawn(heavyTask, receivePort.sendPort);
}
`
	eps := sniffDartEntryPoints(src)
	found := false
	for _, ep := range eps {
		if ep.Kind == EntryKindFrameworkLifecycle && ep.Ident == "heavyTask" {
			found = true
		}
	}
	if !found {
		t.Error("expected framework_lifecycle entry for Isolate.spawn(heavyTask)")
	}
}

func TestDartEntryPoints_Tests(t *testing.T) {
	src := `
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('adds two numbers', () {
    expect(add(2, 3), 5);
  });
  testWidgets('renders title', (tester) async {
    await tester.pumpWidget(const MyApp());
  });
}
`
	eps := sniffDartEntryPoints(src)
	descs := map[string]bool{}
	for _, ep := range eps {
		if ep.Kind == EntryKindTestEntry {
			descs[ep.Ident] = true
		}
	}
	if !descs["adds two numbers"] {
		t.Error("expected test_entry for test('adds two numbers')")
	}
	if !descs["renders title"] {
		t.Error("expected test_entry for testWidgets('renders title')")
	}
}

// Negative: a `main` METHOD nested inside a class body is not the
// top-level program entry and must NOT be reported as cli_main.
func TestDartEntryPoints_Negative_NestedMainMethod(t *testing.T) {
	src := `
class Runner {
  void main() {
    doWork();
  }
}
`
	eps := sniffDartEntryPoints(src)
	for _, ep := range eps {
		if ep.Kind == EntryKindCLIMain {
			t.Errorf("nested class method main() must not be a cli_main entry, got %+v", ep)
		}
	}
}

// Negative: a plain function with no Request/runApp/main/spawn role is
// not an entry point.
func TestDartEntryPoints_Negative_PlainFunction(t *testing.T) {
	src := `
int add(int a, int b) {
  return a + b;
}
`
	eps := sniffDartEntryPoints(src)
	if len(eps) != 0 {
		t.Errorf("plain function must yield no entry points, got %d: %+v", len(eps), eps)
	}
}

func TestDartEntryPoints_Empty(t *testing.T) {
	if eps := sniffDartEntryPoints(""); len(eps) != 0 {
		t.Errorf("expected no entries for empty input, got %d", len(eps))
	}
}
