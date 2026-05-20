package resolve

import "testing"

// TestDynamicPatterns_Catalog_Swift verifies that Swift dynamic-dispatch
// patterns classify correctly (Refs #44).
func TestDynamicPatterns_Catalog_Swift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- Swift — Combine publisher operator leaf names (issue #44) ---
		{"swift_combine_sink", "swift", `sink`, true},
		{"swift_combine_store", "swift", `store`, true},
		{"swift_combine_cancel", "swift", `cancel`, true},
		{"swift_combine_eraseToAnyPublisher", "swift", `eraseToAnyPublisher`, true},
		{"swift_combine_receive", "swift", `receive`, true},
		{"swift_combine_subscribe", "swift", `subscribe`, true},
		{"swift_combine_mapError", "swift", `mapError`, true},
		{"swift_combine_flatMap", "swift", `flatMap`, true},
		{"swift_combine_compactMap", "swift", `compactMap`, true},
		{"swift_combine_tryMap", "swift", `tryMap`, true},
		{"swift_combine_decode", "swift", `decode`, true},
		{"swift_combine_removeDuplicates", "swift", `removeDuplicates`, true},
		{"swift_combine_debounce", "swift", `debounce`, true},
		{"swift_combine_throttle", "swift", `throttle`, true},
		{"swift_combine_timeout", "swift", `timeout`, true},
		{"swift_combine_retry", "swift", `retry`, true},
		{"swift_combine_assign", "swift", `assign`, true},
		{"swift_combine_share", "swift", `share`, true},
		{"swift_combine_combineLatest", "swift", `combineLatest`, true},
		{"swift_combine_zip", "swift", `zip`, true},
		{"swift_combine_send", "swift", `send`, true},
		// Swift Foundation/URLSession bare leaf names.
		{"swift_foundation_dataTaskPublisher", "swift", `dataTaskPublisher`, true},
		{"swift_foundation_dataTask", "swift", `dataTask`, true},
		{"swift_foundation_appendingPathComponent", "swift", `appendingPathComponent`, true},
		{"swift_foundation_appendingPathExtension", "swift", `appendingPathExtension`, true},
		{"swift_foundation_setValue", "swift", `setValue`, true},
		{"swift_foundation_addValue", "swift", `addValue`, true},
		// SwiftUI view modifier leaf names.
		{"swift_swiftui_padding", "swift", `padding`, true},
		{"swift_swiftui_frame", "swift", `frame`, true},
		{"swift_swiftui_background", "swift", `background`, true},
		{"swift_swiftui_foregroundColor", "swift", `foregroundColor`, true},
		{"swift_swiftui_foregroundStyle", "swift", `foregroundStyle`, true},
		{"swift_swiftui_font", "swift", `font`, true},
		{"swift_swiftui_cornerRadius", "swift", `cornerRadius`, true},
		{"swift_swiftui_overlay", "swift", `overlay`, true},
		{"swift_swiftui_shadow", "swift", `shadow`, true},
		{"swift_swiftui_opacity", "swift", `opacity`, true},
		{"swift_swiftui_scaleEffect", "swift", `scaleEffect`, true},
		{"swift_swiftui_rotationEffect", "swift", `rotationEffect`, true},
		{"swift_swiftui_onAppear", "swift", `onAppear`, true},
		{"swift_swiftui_onDisappear", "swift", `onDisappear`, true},
		{"swift_swiftui_onTapGesture", "swift", `onTapGesture`, true},
		{"swift_swiftui_onChange", "swift", `onChange`, true},
		{"swift_swiftui_sheet", "swift", `sheet`, true},
		{"swift_swiftui_alert", "swift", `alert`, true},
		{"swift_swiftui_navigationTitle", "swift", `navigationTitle`, true},
		{"swift_swiftui_toolbar", "swift", `toolbar`, true},
		{"swift_swiftui_listStyle", "swift", `listStyle`, true},
		{"swift_swiftui_searchable", "swift", `searchable`, true},
		{"swift_swiftui_disabled", "swift", `disabled`, true},
		{"swift_swiftui_hidden", "swift", `hidden`, true},
		{"swift_swiftui_environmentObject", "swift", `environmentObject`, true},
		{"swift_swiftui_environment", "swift", `environment`, true},
		{"swift_swiftui_task", "swift", `task`, true},
		{"swift_swiftui_refreshable", "swift", `refreshable`, true},
		{"swift_swiftui_swipeActions", "swift", `swipeActions`, true},
		{"swift_swiftui_contextMenu", "swift", `contextMenu`, true},
		{"swift_swiftui_ignoresSafeArea", "swift", `ignoresSafeArea`, true},
		{"swift_swiftui_clipShape", "swift", `clipShape`, true},
		{"swift_swiftui_resizable", "swift", `resizable`, true},
		{"swift_swiftui_navigationDestination", "swift", `navigationDestination`, true},
		// UIKit bare leaf names.
		{"swift_uikit_addTarget", "swift", `addTarget`, true},
		{"swift_uikit_addSubview", "swift", `addSubview`, true},
		{"swift_uikit_removeFromSuperview", "swift", `removeFromSuperview`, true},
		{"swift_uikit_present", "swift", `present`, true},
		{"swift_uikit_dismiss", "swift", `dismiss`, true},
		{"swift_uikit_reloadData", "swift", `reloadData`, true},
		{"swift_uikit_dequeueReusableCell", "swift", `dequeueReusableCell`, true},
		{"swift_uikit_becomeFirstResponder", "swift", `becomeFirstResponder`, true},
		{"swift_uikit_resignFirstResponder", "swift", `resignFirstResponder`, true},
		{"swift_uikit_pushViewController", "swift", `pushViewController`, true},
		// Cross-language gate: Swift framework names MUST NOT fire for non-Swift.
		{"swift_sink_go_neg", "go", `sink`, false},
		{"swift_sink_python_neg", "python", `sink`, false},
		{"swift_sink_ruby_neg", "ruby", `sink`, false},
		{"swift_sink_js_neg", "javascript", `sink`, false},
		{"swift_store_go_neg", "go", `store`, false},
		{"swift_store_python_neg", "python", `store`, false},
		{"swift_store_java_neg", "java", `store`, false},
		{"swift_frame_python_neg", "python", `frame`, false},
		{"swift_frame_ruby_neg", "ruby", `frame`, false},
		{"swift_padding_go_neg", "go", `padding`, false},
		{"swift_padding_js_neg", "javascript", `padding`, false},
		{"swift_font_python_neg", "python", `font`, false},
		{"swift_send_java_neg", "java", `send`, false},
		{"swift_send_go_neg", "go", `send`, false},
		{"swift_zip_python_neg", "python", `zip`, false},
		{"swift_zip_ruby_neg", "ruby", `zip`, false},
		{"swift_present_python_neg", "python", `present`, false},
		{"swift_dismiss_js_neg", "javascript", `dismiss`, false},
		{"swift_alert_go_neg", "go", `alert`, false},
		{"swift_assign_python_neg", "python", `assign`, false},
		{"swift_assign_js_neg", "javascript", `assign`, false},
		{"swift_environment_python_neg", "python", `environment`, false},
		{"swift_environment_ruby_neg", "ruby", `environment`, false},
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
