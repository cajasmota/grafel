package resolve

import "regexp"

// dartDynamicPatterns covers three categories of statically-unresolvable
// stubs that the Dart/Flutter extractor emits (issue #44 slice 9):
//
//  1. Package-URI import ToIDs — `package:flutter/material.dart`,
//     `dart:convert`, relative paths like `../widgets/cart.dart`. The Dart
//     extractor records each import as a CALLS edge whose ToID is the
//     package URI; no in-tree entity is indexed for these, so static
//     binding is impossible without a full pub-resolution pass.
//
//  2. Flutter/Material/Cupertino widget constructors — bare class names
//     (`Scaffold`, `Column`, `Text`, …) extracted from widget-build
//     expressions. These are framework types imported from
//     `package:flutter/material.dart` (or cupertino / widgets packages)
//     and have no in-tree entity. The most frequent hits are enumerated
//     below; the per-language gate keeps these names from shadowing
//     user-defined classes in other languages.
//
//  3. ChangeNotifier / StatefulWidget lifecycle and async/Future chain
//     methods — `notifyListeners`, `setState`, `then`, `catchError`,
//     `whenComplete`, `listen` are called on framework-provided or
//     Dart-stdlib objects; the receiver is stripped by the extractor and
//     static binding is impossible without full type inference.
var dartDynamicPatterns = []*regexp.Regexp{
	// ── 1. Package-URI import ToIDs ──────────────────────────────────
	// `package:` imports: `package:flutter/material.dart`,
	// `package:provider/provider.dart`, `package:http/http.dart`, etc.
	regexp.MustCompile(`^package:`),
	// `dart:` standard-library imports: `dart:convert`, `dart:async`, etc.
	regexp.MustCompile(`^dart:`),
	// Relative path imports: `../widgets/cart.dart`, `./util.dart`,
	// `src/screens/home.dart`. Same shape as the JS/TS relative-import
	// rule — one SCOPE.Component placeholder per importing file, so
	// bare-name lookup is ambiguous.
	regexp.MustCompile(`^\.{1,2}/`),
	regexp.MustCompile(`^\.{1,3}$`),
	regexp.MustCompile(`^(lib|src|test|integration_test)/`),

	// ── 2. Flutter/Material/Cupertino widget constructors ────────────
	// Layout widgets
	regexp.MustCompile(`^Column$`),
	regexp.MustCompile(`^Row$`),
	regexp.MustCompile(`^Stack$`),
	regexp.MustCompile(`^Wrap$`),
	regexp.MustCompile(`^Flex$`),
	regexp.MustCompile(`^Expanded$`),
	regexp.MustCompile(`^Flexible$`),
	regexp.MustCompile(`^Spacer$`),
	regexp.MustCompile(`^SizedBox$`),
	regexp.MustCompile(`^ConstrainedBox$`),
	regexp.MustCompile(`^FittedBox$`),
	regexp.MustCompile(`^LimitedBox$`),
	regexp.MustCompile(`^OverflowBox$`),
	regexp.MustCompile(`^UnconstrainedBox$`),
	regexp.MustCompile(`^AspectRatio$`),
	regexp.MustCompile(`^Center$`),
	regexp.MustCompile(`^Align$`),
	regexp.MustCompile(`^Padding$`),
	regexp.MustCompile(`^Container$`),
	regexp.MustCompile(`^DecoratedBox$`),
	regexp.MustCompile(`^Positioned$`),
	// Scrolling
	regexp.MustCompile(`^ListView$`),
	regexp.MustCompile(`^GridView$`),
	regexp.MustCompile(`^SingleChildScrollView$`),
	regexp.MustCompile(`^CustomScrollView$`),
	regexp.MustCompile(`^PageView$`),
	regexp.MustCompile(`^NestedScrollView$`),
	regexp.MustCompile(`^RefreshIndicator$`),
	// Sliver delegates (common SliverGrid / SliverList)
	regexp.MustCompile(`^SliverList$`),
	regexp.MustCompile(`^SliverGrid$`),
	regexp.MustCompile(`^SliverGridDelegateWith(?:Fixed|Max)CrossAxisCount$`),
	regexp.MustCompile(`^SliverChildListDelegate$`),
	regexp.MustCompile(`^SliverChildBuilderDelegate$`),
	// Scaffold & navigation structure
	regexp.MustCompile(`^Scaffold$`),
	regexp.MustCompile(`^AppBar$`),
	regexp.MustCompile(`^BottomNavigationBar$`),
	regexp.MustCompile(`^BottomNavigationBarItem$`),
	regexp.MustCompile(`^NavigationBar$`),
	regexp.MustCompile(`^NavigationRail$`),
	regexp.MustCompile(`^Drawer$`),
	regexp.MustCompile(`^DrawerHeader$`),
	regexp.MustCompile(`^TabBar$`),
	regexp.MustCompile(`^TabBarView$`),
	regexp.MustCompile(`^TabController$`),
	regexp.MustCompile(`^DefaultTabController$`),
	// App-level
	regexp.MustCompile(`^MaterialApp$`),
	regexp.MustCompile(`^WidgetsApp$`),
	regexp.MustCompile(`^CupertinoApp$`),
	regexp.MustCompile(`^MultiProvider$`),
	regexp.MustCompile(`^ChangeNotifierProvider$`),
	regexp.MustCompile(`^Provider$`),
	regexp.MustCompile(`^Consumer$`),
	regexp.MustCompile(`^Selector$`),
	// Text & typography
	regexp.MustCompile(`^Text$`),
	regexp.MustCompile(`^RichText$`),
	regexp.MustCompile(`^SelectableText$`),
	regexp.MustCompile(`^TextSpan$`),
	regexp.MustCompile(`^DefaultTextStyle$`),
	// Buttons
	regexp.MustCompile(`^ElevatedButton$`),
	regexp.MustCompile(`^TextButton$`),
	regexp.MustCompile(`^OutlinedButton$`),
	regexp.MustCompile(`^FilledButton$`),
	regexp.MustCompile(`^IconButton$`),
	regexp.MustCompile(`^FloatingActionButton$`),
	regexp.MustCompile(`^DropdownButton$`),
	regexp.MustCompile(`^DropdownMenuItem$`),
	regexp.MustCompile(`^PopupMenuButton$`),
	regexp.MustCompile(`^PopupMenuItem$`),
	// Forms & input
	regexp.MustCompile(`^TextField$`),
	regexp.MustCompile(`^TextFormField$`),
	regexp.MustCompile(`^Form$`),
	regexp.MustCompile(`^FormField$`),
	regexp.MustCompile(`^Checkbox$`),
	regexp.MustCompile(`^CheckboxListTile$`),
	regexp.MustCompile(`^Radio$`),
	regexp.MustCompile(`^RadioListTile$`),
	regexp.MustCompile(`^Switch$`),
	regexp.MustCompile(`^SwitchListTile$`),
	regexp.MustCompile(`^Slider$`),
	regexp.MustCompile(`^RangeSlider$`),
	regexp.MustCompile(`^DatePicker$`),
	regexp.MustCompile(`^TimePicker$`),
	regexp.MustCompile(`^InputDecoration$`),
	// Decoration & theming
	regexp.MustCompile(`^BoxDecoration$`),
	regexp.MustCompile(`^ShapeDecoration$`),
	regexp.MustCompile(`^BoxConstraints$`),
	regexp.MustCompile(`^BorderRadius$`),
	regexp.MustCompile(`^RoundedRectangleBorder$`),
	regexp.MustCompile(`^Border$`),
	regexp.MustCompile(`^BorderSide$`),
	regexp.MustCompile(`^ThemeData$`),
	regexp.MustCompile(`^Theme$`),
	regexp.MustCompile(`^ColorScheme$`),
	regexp.MustCompile(`^TextStyle$`),
	regexp.MustCompile(`^TextTheme$`),
	regexp.MustCompile(`^EdgeInsets$`),
	regexp.MustCompile(`^EdgeInsetsDirectional$`),
	regexp.MustCompile(`^Gradient$`),
	regexp.MustCompile(`^LinearGradient$`),
	regexp.MustCompile(`^RadialGradient$`),
	regexp.MustCompile(`^SweepGradient$`),
	regexp.MustCompile(`^BoxShadow$`),
	// Images & icons
	regexp.MustCompile(`^Image$`),
	regexp.MustCompile(`^Icon$`),
	regexp.MustCompile(`^CircleAvatar$`),
	regexp.MustCompile(`^ClipRRect$`),
	regexp.MustCompile(`^ClipOval$`),
	regexp.MustCompile(`^ClipPath$`),
	// Lists & cards
	regexp.MustCompile(`^Card$`),
	regexp.MustCompile(`^ListTile$`),
	regexp.MustCompile(`^ExpansionTile$`),
	regexp.MustCompile(`^Divider$`),
	// Overlays & dialogs
	regexp.MustCompile(`^AlertDialog$`),
	regexp.MustCompile(`^SimpleDialog$`),
	regexp.MustCompile(`^BottomSheet$`),
	regexp.MustCompile(`^SnackBar$`),
	regexp.MustCompile(`^SnackBarAction$`),
	regexp.MustCompile(`^Tooltip$`),
	regexp.MustCompile(`^Banner$`),
	// Progress & loading
	regexp.MustCompile(`^CircularProgressIndicator$`),
	regexp.MustCompile(`^LinearProgressIndicator$`),
	// Gesture & interaction
	regexp.MustCompile(`^GestureDetector$`),
	regexp.MustCompile(`^InkWell$`),
	regexp.MustCompile(`^InkResponse$`),
	regexp.MustCompile(`^Draggable$`),
	regexp.MustCompile(`^DragTarget$`),
	// Safe area, media, focus
	regexp.MustCompile(`^SafeArea$`),
	regexp.MustCompile(`^MediaQuery$`),
	regexp.MustCompile(`^FocusScope$`),
	regexp.MustCompile(`^Focus$`),
	// Builder / async widgets
	regexp.MustCompile(`^Builder$`),
	regexp.MustCompile(`^StatefulBuilder$`),
	regexp.MustCompile(`^FutureBuilder$`),
	regexp.MustCompile(`^StreamBuilder$`),
	regexp.MustCompile(`^AnimatedBuilder$`),
	regexp.MustCompile(`^ValueListenableBuilder$`),
	regexp.MustCompile(`^AnimatedWidget$`),
	// Navigation
	regexp.MustCompile(`^MaterialPageRoute$`),
	regexp.MustCompile(`^CupertinoPageRoute$`),
	regexp.MustCompile(`^PageRouteBuilder$`),
	// Misc Material
	regexp.MustCompile(`^Chip$`),
	regexp.MustCompile(`^Badge$`),
	regexp.MustCompile(`^Stepper$`),
	regexp.MustCompile(`^DataTable$`),
	regexp.MustCompile(`^DataRow$`),
	regexp.MustCompile(`^DataCell$`),
	regexp.MustCompile(`^DataColumn$`),

	// ── 3. ChangeNotifier / StatefulWidget lifecycle ─────────────────
	// `notifyListeners()` — ChangeNotifier method; receiver is `this`
	// (stripped by extractor). Not user-defined, not resolvable.
	regexp.MustCompile(`^notifyListeners$`),
	// `setState(() { ... })` — StatefulWidget lifecycle; same pattern.
	regexp.MustCompile(`^setState$`),
	// `addListener` / `removeListener` — Listenable API.
	regexp.MustCompile(`^addListener$`),
	regexp.MustCompile(`^removeListener$`),
	// `markNeedsBuild` / `markNeedsPaint` — internal widget tree methods.
	regexp.MustCompile(`^markNeedsBuild$`),
	regexp.MustCompile(`^markNeedsPaint$`),
	// `initState` / `dispose` / `didUpdateWidget` / `didChangeDependencies`
	// appear on CONTAINS edges when a State override calls super.*; they
	// also appear as bare CALLS inside override bodies when calling
	// `super.initState()` (super receiver stripped to `initState`).
	regexp.MustCompile(`^initState$`),
	regexp.MustCompile(`^didUpdateWidget$`),
	regexp.MustCompile(`^didChangeDependencies$`),
	regexp.MustCompile(`^reassemble$`),
	regexp.MustCompile(`^deactivate$`),

	// ── 4. Dart async / Future / Stream chain methods ────────────────
	// These arrive as bare identifiers after the extractor strips the
	// receiver (e.g. `future.then(...)` → ToID="then"). The actual
	// implementation lives in dart:async; static binding is impossible.
	// Dart-only gate prevents collision with Ruby's `then` (real method)
	// and TS `then`/`catch`/`finally` (already in jsDynamicPatterns).
	regexp.MustCompile(`^catchError$`),
	regexp.MustCompile(`^whenComplete$`),
	regexp.MustCompile(`^listen$`),
	regexp.MustCompile(`^asBroadcastStream$`),
	regexp.MustCompile(`^asyncExpand$`),
	regexp.MustCompile(`^asyncMap$`),
	regexp.MustCompile(`^onError$`),

	// ── 5. Dart core / stdlib method names stripped from receivers ───
	// `of` — static factory ubiquitous in Flutter: `Theme.of(context)`,
	// `Navigator.of(context)`, `Provider.of<T>(context)`,
	// `ScaffoldMessenger.of(context)`, `MediaQuery.of(context)`.
	// The receiver is stripped by the extractor; `of` alone is
	// unresolvable. Dart-only gate: `of` in Python/Go/Ruby/Java is
	// a real user method name.
	regexp.MustCompile(`^of$`),
	// `maybeOf` — nullable variant of the static factory pattern.
	regexp.MustCompile(`^maybeOf$`),
	// `showSnackBar` / `showDialog` / `showModalBottomSheet` —
	// called on ScaffoldMessenger.of(context) / showDialog(context:...)
	// after receiver strip.
	regexp.MustCompile(`^showSnackBar$`),
	regexp.MustCompile(`^showDialog$`),
	regexp.MustCompile(`^showModalBottomSheet$`),
	regexp.MustCompile(`^showBottomSheet$`),
	regexp.MustCompile(`^showDatePicker$`),
	regexp.MustCompile(`^showTimePicker$`),
	regexp.MustCompile(`^showSearch$`),
	// Navigator push/pop — called on `Navigator.of(context)` after strip.
	regexp.MustCompile(`^pushNamed$`),
	regexp.MustCompile(`^pushReplacementNamed$`),
	regexp.MustCompile(`^push$`),
	regexp.MustCompile(`^pushReplacement$`),
	regexp.MustCompile(`^pop$`),
	regexp.MustCompile(`^popAndPushNamed$`),
	regexp.MustCompile(`^canPop$`),

	// ── 6. Dart core / dart:core type constructors and methods ───────
	// `Exception(...)` — Dart built-in exception constructor; the
	// extractor strips `throw Exception(...)` to bare `Exception`.
	regexp.MustCompile(`^Exception$`),
	regexp.MustCompile(`^FormatException$`),
	regexp.MustCompile(`^ArgumentError$`),
	regexp.MustCompile(`^StateError$`),
	regexp.MustCompile(`^RangeError$`),
	regexp.MustCompile(`^UnsupportedError$`),
	regexp.MustCompile(`^UnimplementedError$`),
	regexp.MustCompile(`^TypeError$`),
	regexp.MustCompile(`^AssertionError$`),
	regexp.MustCompile(`^Error$`),
	// `toString()` — dart:core Object method; every Dart object has it.
	// Dart-gate prevents collision with Go `.String()` helper / Python
	// `str()` / Java `toString()` (those are handled by their own
	// per-language logic or not present in the dynamic catalog).
	regexp.MustCompile(`^toString$`),
	// `parse` / `tryParse` — static factory on int/double/DateTime/Uri.
	// The receiver is stripped by the extractor; the bare `parse` callee
	// cannot be resolved without knowing the receiver type.
	regexp.MustCompile(`^parse$`),
	regexp.MustCompile(`^tryParse$`),
	// `decode` / `encode` — json.decode / json.encode / utf8.decode
	// stripped of the receiver; dart:convert stdlib, not user code.
	regexp.MustCompile(`^decode$`),
	regexp.MustCompile(`^encode$`),
	// `now` — DateTime.now() static factory.
	regexp.MustCompile(`^now$`),
	// `fromSeed` — ColorScheme.fromSeed() static factory (Material 3).
	regexp.MustCompile(`^fromSeed$`),
	// `circular` — BorderRadius.circular() static factory.
	regexp.MustCompile(`^circular$`),
	// `network` — Image.network() constructor.
	regexp.MustCompile(`^network$`),
	// `icon` — ElevatedButton.icon(), OutlinedButton.icon() named constructors.
	regexp.MustCompile(`^icon$`),
	// `builder` — GridView.builder(), ListView.builder() named constructors.
	regexp.MustCompile(`^builder$`),
	// `all` — EdgeInsets.all(), SizedBox.all(), BoxConstraints.expand() pattern.
	regexp.MustCompile(`^all$`),
	// `only` / `symmetric` — EdgeInsets.only() / EdgeInsets.symmetric().
	regexp.MustCompile(`^only$`),
	regexp.MustCompile(`^symmetric$`),

	// ── 7. dart:collection / Dart Map + Iterable stdlib methods ──────
	// Called on Map<K,V> / List<T> / Set<T> receivers that the
	// extractor strips to bare identifiers. These are dart:core stdlib
	// methods; no in-tree entity exists for them.
	regexp.MustCompile(`^containsKey$`),
	regexp.MustCompile(`^containsValue$`),
	regexp.MustCompile(`^putIfAbsent$`),
	regexp.MustCompile(`^remove$`),
	regexp.MustCompile(`^update$`),
	regexp.MustCompile(`^toList$`),
	regexp.MustCompile(`^toSet$`),
	regexp.MustCompile(`^toMap$`),
	regexp.MustCompile(`^forEach$`),
	regexp.MustCompile(`^map$`),
	regexp.MustCompile(`^where$`),
	regexp.MustCompile(`^reduce$`),
	regexp.MustCompile(`^fold$`),
	regexp.MustCompile(`^any$`),
	regexp.MustCompile(`^every$`),
	regexp.MustCompile(`^firstWhere$`),
	regexp.MustCompile(`^lastWhere$`),
	regexp.MustCompile(`^singleWhere$`),
	regexp.MustCompile(`^expand$`),
	regexp.MustCompile(`^skip$`),
	regexp.MustCompile(`^take$`),
	regexp.MustCompile(`^sort$`),
	regexp.MustCompile(`^reversed$`),
	regexp.MustCompile(`^isEmpty$`),
	regexp.MustCompile(`^isNotEmpty$`),

	// ── 8. dart:core String methods ───────────────────────────────────
	regexp.MustCompile(`^trim$`),
	regexp.MustCompile(`^trimLeft$`),
	regexp.MustCompile(`^trimRight$`),
	regexp.MustCompile(`^split$`),
	regexp.MustCompile(`^join$`),
	regexp.MustCompile(`^toLowerCase$`),
	regexp.MustCompile(`^toUpperCase$`),
	regexp.MustCompile(`^contains$`),
	regexp.MustCompile(`^startsWith$`),
	regexp.MustCompile(`^endsWith$`),
	regexp.MustCompile(`^replaceAll$`),
	regexp.MustCompile(`^replaceFirst$`),
	regexp.MustCompile(`^indexOf$`),
	regexp.MustCompile(`^lastIndexOf$`),
	regexp.MustCompile(`^substring$`),
	regexp.MustCompile(`^padLeft$`),
	regexp.MustCompile(`^padRight$`),
	// `toStringAsFixed` / `toStringAsPrecision` — double/num methods.
	regexp.MustCompile(`^toStringAsFixed$`),
	regexp.MustCompile(`^toStringAsPrecision$`),
	regexp.MustCompile(`^toStringAsExponential$`),
	regexp.MustCompile(`^toDouble$`),
	regexp.MustCompile(`^toInt$`),
	regexp.MustCompile(`^abs$`),
	regexp.MustCompile(`^ceil$`),
	regexp.MustCompile(`^floor$`),
	regexp.MustCompile(`^round$`),
	// `validate` — GlobalKey<FormState>.currentState!.validate().
	regexp.MustCompile(`^validate$`),
	regexp.MustCompile(`^reset$`),
	regexp.MustCompile(`^save$`),
	// `runApp` — Flutter framework entry point (dart:ui / package:flutter).
	regexp.MustCompile(`^runApp$`),
	// `dispose` — TextEditingController.dispose() / ScrollController.dispose()
	// etc. Called on framework-owned controllers; receiver stripped.
	// Also appears as a StatefulWidget lifecycle override body calling
	// `controller.dispose()`. Dart-gate prevents collision with user
	// `dispose()` methods in other ecosystems.
	regexp.MustCompile(`^dispose$`),
	// `clear` — TextEditingController.clear() / List.clear().
	regexp.MustCompile(`^clear$`),
}

func init() {
	dynamicPatternsByLang["dart"] = dartDynamicPatterns
}
