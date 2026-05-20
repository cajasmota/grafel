package resolve

import "testing"

// TestDynamicPatterns_Catalog_Dart verifies that Dart/Flutter dynamic-dispatch
// patterns classify correctly (Refs #44 slice 9).
func TestDynamicPatterns_Catalog_Dart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// 1. Package-URI import ToIDs — unresolvable without pub-resolution.
		{"dart_pkg_flutter_material", "dart", `package:flutter/material.dart`, true},
		{"dart_pkg_provider", "dart", `package:provider/provider.dart`, true},
		{"dart_pkg_http", "dart", `package:http/http.dart`, true},
		{"dart_pkg_riverpod", "dart", `package:flutter_riverpod/flutter_riverpod.dart`, true},
		{"dart_dart_convert", "dart", `dart:convert`, true},
		{"dart_dart_async", "dart", `dart:async`, true},
		{"dart_dart_core", "dart", `dart:core`, true},
		{"dart_relative_import_dotslash", "dart", `./widgets/product_card.dart`, true},
		{"dart_relative_import_dotdot", "dart", `../providers/cart_provider.dart`, true},
		{"dart_lib_prefix", "dart", `lib/src/screens/home_screen.dart`, true},
		{"dart_src_prefix", "dart", `src/screens/login_screen.dart`, true},
		// 2. Flutter/Material widget constructors.
		{"dart_widget_Text", "dart", `Text`, true},
		{"dart_widget_Column", "dart", `Column`, true},
		{"dart_widget_Row", "dart", `Row`, true},
		{"dart_widget_Scaffold", "dart", `Scaffold`, true},
		{"dart_widget_AppBar", "dart", `AppBar`, true},
		{"dart_widget_Container", "dart", `Container`, true},
		{"dart_widget_SizedBox", "dart", `SizedBox`, true},
		{"dart_widget_Padding", "dart", `Padding`, true},
		{"dart_widget_Expanded", "dart", `Expanded`, true},
		{"dart_widget_Center", "dart", `Center`, true},
		{"dart_widget_Stack", "dart", `Stack`, true},
		{"dart_widget_Positioned", "dart", `Positioned`, true},
		{"dart_widget_ListView", "dart", `ListView`, true},
		{"dart_widget_GridView", "dart", `GridView`, true},
		{"dart_widget_MaterialApp", "dart", `MaterialApp`, true},
		{"dart_widget_SnackBar", "dart", `SnackBar`, true},
		{"dart_widget_IconButton", "dart", `IconButton`, true},
		{"dart_widget_ElevatedButton", "dart", `ElevatedButton`, true},
		{"dart_widget_TextFormField", "dart", `TextFormField`, true},
		{"dart_widget_CircularProgressIndicator", "dart", `CircularProgressIndicator`, true},
		{"dart_widget_BoxDecoration", "dart", `BoxDecoration`, true},
		{"dart_widget_InputDecoration", "dart", `InputDecoration`, true},
		{"dart_widget_TextStyle", "dart", `TextStyle`, true},
		{"dart_widget_ThemeData", "dart", `ThemeData`, true},
		{"dart_widget_SafeArea", "dart", `SafeArea`, true},
		{"dart_widget_RefreshIndicator", "dart", `RefreshIndicator`, true},
		{"dart_widget_Card", "dart", `Card`, true},
		{"dart_widget_Icon", "dart", `Icon`, true},
		{"dart_widget_MultiProvider", "dart", `MultiProvider`, true},
		{"dart_widget_ChangeNotifierProvider", "dart", `ChangeNotifierProvider`, true},
		{"dart_widget_SliverGridDelegate", "dart", `SliverGridDelegateWithFixedCrossAxisCount`, true},
		{"dart_widget_SliverGridDelegateMax", "dart", `SliverGridDelegateWithMaxCrossAxisCount`, true},
		{"dart_widget_FutureBuilder", "dart", `FutureBuilder`, true},
		{"dart_widget_StreamBuilder", "dart", `StreamBuilder`, true},
		{"dart_widget_AlertDialog", "dart", `AlertDialog`, true},
		{"dart_widget_GestureDetector", "dart", `GestureDetector`, true},
		{"dart_widget_InkWell", "dart", `InkWell`, true},
		// 3. ChangeNotifier / StatefulWidget lifecycle.
		{"dart_lifecycle_notifyListeners", "dart", `notifyListeners`, true},
		{"dart_lifecycle_setState", "dart", `setState`, true},
		{"dart_lifecycle_addListener", "dart", `addListener`, true},
		{"dart_lifecycle_removeListener", "dart", `removeListener`, true},
		{"dart_lifecycle_initState", "dart", `initState`, true},
		{"dart_lifecycle_didUpdateWidget", "dart", `didUpdateWidget`, true},
		{"dart_lifecycle_didChangeDependencies", "dart", `didChangeDependencies`, true},
		// 4. Dart async/Future/Stream chain methods.
		{"dart_async_catchError", "dart", `catchError`, true},
		{"dart_async_whenComplete", "dart", `whenComplete`, true},
		{"dart_async_listen", "dart", `listen`, true},
		// 5. Flutter static factories and lifecycle helpers.
		{"dart_nav_pushNamed", "dart", `pushNamed`, true},
		{"dart_nav_pushReplacementNamed", "dart", `pushReplacementNamed`, true},
		{"dart_nav_pop", "dart", `pop`, true},
		{"dart_nav_push", "dart", `push`, true},
		{"dart_static_of", "dart", `of`, true},
		{"dart_static_maybeOf", "dart", `maybeOf`, true},
		{"dart_overlay_showSnackBar", "dart", `showSnackBar`, true},
		{"dart_overlay_showDialog", "dart", `showDialog`, true},
		{"dart_overlay_showModalBottomSheet", "dart", `showModalBottomSheet`, true},
		// 6. Dart core type constructors and methods.
		{"dart_core_Exception", "dart", `Exception`, true},
		{"dart_core_FormatException", "dart", `FormatException`, true},
		{"dart_core_toString", "dart", `toString`, true},
		{"dart_core_parse", "dart", `parse`, true},
		{"dart_core_tryParse", "dart", `tryParse`, true},
		{"dart_core_decode", "dart", `decode`, true},
		{"dart_core_encode", "dart", `encode`, true},
		{"dart_core_now", "dart", `now`, true},
		{"dart_core_fromSeed", "dart", `fromSeed`, true},
		{"dart_core_circular", "dart", `circular`, true},
		{"dart_core_network", "dart", `network`, true},
		{"dart_core_runApp", "dart", `runApp`, true},
		{"dart_core_dispose", "dart", `dispose`, true},
		// 7. dart:core collection methods.
		{"dart_map_containsKey", "dart", `containsKey`, true},
		{"dart_map_putIfAbsent", "dart", `putIfAbsent`, true},
		{"dart_map_remove", "dart", `remove`, true},
		{"dart_map_update", "dart", `update`, true},
		{"dart_list_toList", "dart", `toList`, true},
		{"dart_list_where", "dart", `where`, true},
		{"dart_list_firstWhere", "dart", `firstWhere`, true},
		{"dart_list_map", "dart", `map`, true},
		// 8. dart:core String methods.
		{"dart_str_trim", "dart", `trim`, true},
		{"dart_str_toStringAsFixed", "dart", `toStringAsFixed`, true},
		// Cross-language gate: Dart patterns MUST NOT fire for other languages.
		{"dart_of_python_neg", "python", `of`, false},
		{"dart_of_go_neg", "go", `of`, false},
		{"dart_of_ruby_neg", "ruby", `of`, false},
		{"dart_of_java_neg", "java", `of`, false},
		{"dart_notifyListeners_python_neg", "python", `notifyListeners`, false},
		{"dart_notifyListeners_go_neg", "go", `notifyListeners`, false},
		{"dart_notifyListeners_java_neg", "java", `notifyListeners`, false},
		{"dart_setState_python_neg", "python", `setState`, false},
		// Note: `setState` for JavaScript is already dynamic via the `^set[A-Z]...` React
		// useState-setter pattern (jsDynamicPatterns wave-7), so we do NOT test it as a
		// Dart-only negative for JS. Use a language that has no matching JS pattern instead.
		{"dart_setState_ruby_neg", "ruby", `setState`, false},
		{"dart_catchError_python_neg", "python", `catchError`, false},
		{"dart_catchError_ruby_neg", "ruby", `catchError`, false},
		{"dart_parse_python_neg", "python", `parse`, false},
		{"dart_parse_go_neg", "go", `parse`, false},
		{"dart_parse_java_neg", "java", `parse`, false},
		{"dart_toString_go_neg", "go", `toString`, false},
		{"dart_toString_python_neg", "python", `toString`, false},
		{"dart_toString_java_neg", "java", `toString`, false},
		{"dart_containsKey_python_neg", "python", `containsKey`, false},
		{"dart_containsKey_go_neg", "go", `containsKey`, false},
		{"dart_containsKey_java_neg", "java", `containsKey`, false},
		{"dart_remove_python_neg", "python", `remove`, false},
		{"dart_remove_go_neg", "go", `remove`, false},
		{"dart_remove_ruby_neg", "ruby", `remove`, false},
		{"dart_trim_python_neg", "python", `trim`, false},
		{"dart_trim_go_neg", "go", `trim`, false},
		{"dart_dispose_python_neg", "python", `dispose`, false},
		{"dart_dispose_go_neg", "go", `dispose`, false},
		{"dart_dispose_ruby_neg", "ruby", `dispose`, false},
		// Note: `Text` and `Column` are also in pythonDynamicPatterns (Flask-SQLAlchemy /
		// Marshmallow), so those names correctly fire as Dynamic for Python.
		// Use widget names that are unique to the Dart catalog for the cross-language gate.
		{"dart_Scaffold_python_neg", "python", `Scaffold`, false},
		{"dart_Scaffold_go_neg", "go", `Scaffold`, false},
		{"dart_Scaffold_java_neg", "java", `Scaffold`, false},
		{"dart_setState_kotlin_neg", "kotlin", `setState`, false},
		{"dart_pushNamed_python_neg", "python", `pushNamed`, false},
		{"dart_pushNamed_go_neg", "go", `pushNamed`, false},
		{"dart_pushNamed_java_neg", "java", `pushNamed`, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDynamicPatternLang(tc.stub, tc.lang)
			if got != tc.want {
				t.Fatalf("isDynamicPatternLang(%q, lang=%q) = %v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}
