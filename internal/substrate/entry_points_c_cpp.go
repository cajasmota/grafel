// C / C++ entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `int main(`, `int main(void)`, `int main(int argc, char *argv[])`
//     → cli_main. Also `int wmain(`, `int WinMain(`, `int _tmain(`.
//   - Google Test `TEST(Suite, Name)`, `TEST_F(Fixture, Name)`,
//     `TEST_P(Fixture, Name)`, `TYPED_TEST(Fixture, Name)` → test_entry.
//   - Catch2 `TEST_CASE("name", "[tag]")`, `SCENARIO("name")`,
//     `SECTION("name")` → test_entry.
//   - Boost.Test `BOOST_AUTO_TEST_CASE(name)` and
//     `BOOST_FIXTURE_TEST_CASE(name, fixture)` → test_entry.
//   - Functions marked `__attribute__((constructor))` or
//     `__attribute__((destructor))` (GCC/Clang lifecycle hooks),
//     `DLLMAIN` / `DllMain` (Windows DLL load) → framework_lifecycle.
//   - Exported symbols — `extern "C"` blocks, declarations marked
//     `__declspec(dllexport)` or `__attribute__((visibility("default")))`,
//     and any non-static top-level function definition → library_export.
//     (C/C++ has no default visibility marker; the convention is that
//     `static` means internal-linkage and anything else is exported.)
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("c-cpp", sniffCCPPEntryPoints) }

// ccppMainRe matches `int main(` (and the Windows variants). Tolerates
// `void` / `int argc, char *argv[]` / `int argc, char **argv` arg lists.
var ccppMainRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:int|void)\s+(main|wmain|WinMain|_tmain|_tWinMain)\s*\(`,
)

// ccppGoogleTestRe matches Google Test macro invocations. Capture 1 =
// macro name (for kind classification); 2 = suite; 3 = test name.
var ccppGoogleTestRe = regexp.MustCompile(
	`(?m)^[ \t]*(TEST|TEST_F|TEST_P|TYPED_TEST|TYPED_TEST_P)\s*\(\s*([A-Za-z_]\w*)\s*,\s*([A-Za-z_]\w*)\s*\)`,
)

// ccppCatch2Re matches Catch2 / Boost.Test macros. Capture 1 = macro;
// 2 = label / name.
var ccppCatch2Re = regexp.MustCompile(
	`(?m)^[ \t]*(TEST_CASE|SCENARIO|SECTION|BOOST_AUTO_TEST_CASE|BOOST_FIXTURE_TEST_CASE)\s*\(\s*(?:"([^"]{1,200})"|([A-Za-z_]\w*))`,
)

// ccppCtorAttrRe matches a GCC/Clang lifecycle attribute on a function
// declaration.
var ccppCtorAttrRe = regexp.MustCompile(
	`__attribute__\s*\(\s*\(\s*(constructor|destructor)\b`,
)

// ccppExportedFnRe matches a non-static function definition at column 0.
// Capture 1 = function name. This regex is intentionally loose; the
// real parser cost belongs in a separate pass. We reject lines that
// start with `static`, `inline static`, or `#`. We accept declarations
// that span lines by anchoring on the `(` immediately after the name.
var ccppExportedFnRe = regexp.MustCompile(
	`(?m)^(?:extern\s+"C"\s+)?(?:[\w*&:<>,\[\] \t]+?\s+)?([A-Za-z_]\w*)\s*\([^;]*?\)\s*(?:const\s*)?(?:noexcept\s*)?(?:override\s*)?\s*\{`,
)

// ccppStaticPrefixRe rejects lines whose declaration starts with a
// linkage modifier that makes the function internal-only.
var ccppStaticPrefixRe = regexp.MustCompile(`(?m)^[ \t]*(?:static|extern\s+template)\b`)

// ccppDllMainRe matches Windows `DllMain` / `DLLMAIN` entry.
var ccppDllMainRe = regexp.MustCompile(`(?m)^[ \t]*(?:BOOL|int)\s+(?:WINAPI\s+)?(DllMain|DLLMAIN)\s*\(`)

func sniffCCPPEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	mainLines := map[int]bool{}

	for _, m := range ccppMainRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	for _, m := range ccppDllMainRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	for _, m := range ccppGoogleTestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 8 {
			continue
		}
		name := content[m[4]:m[5]] + "_" + content[m[6]:m[7]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	for _, m := range ccppCatch2Re.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		ident := ""
		if m[4] >= 0 && m[5] > m[4] {
			ident = content[m[4]:m[5]]
		} else if len(m) >= 8 && m[6] >= 0 && m[7] > m[6] {
			ident = content[m[6]:m[7]]
		}
		if ident == "" {
			ident = "test"
		}
		out = append(out, EntryPoint{
			Ident: ident,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	// Lifecycle attribute scan — pair the attribute with the next
	// function definition on the same logical declaration.
	for _, m := range ccppCtorAttrRe.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "__" + content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	lines := strings.Split(content, "\n")

	for _, m := range ccppExportedFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if ccppReservedNames[name] {
			continue
		}
		line := lineOfOffset(content, m[0])
		if mainLines[line] {
			continue
		}
		// Reject declarations whose first non-blank token on the
		// declaration's first line is `static` / `extern template`.
		ltext := ""
		if line >= 1 && line <= len(lines) {
			ltext = lines[line-1]
		}
		if ccppStaticPrefixRe.MatchString(ltext) {
			continue
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  line,
			Kind:  EntryKindLibraryExport,
		})
	}

	return out
}

// ccppReservedNames are control-flow / keyword tokens that the loose
// function-definition regex could otherwise mis-classify.
var ccppReservedNames = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"return": true, "throw": true, "try": true, "catch": true,
	"do": true, "else": true, "case": true, "default": true,
	"sizeof": true, "typeid": true, "new": true, "delete": true,
	"static_cast": true, "dynamic_cast": true, "const_cast": true,
	"reinterpret_cast": true,
}
