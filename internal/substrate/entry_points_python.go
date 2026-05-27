// Python entry-point sniffer (#2766 Phase 1B T1).
//
// Recognises:
//   - `if __name__ == "__main__":` block → cli_main (with the
//     synthetic ident "__main__").
//   - `def main(` at module level → cli_main.
//   - `def test_<name>(` / `class Test<Name>(` → test_entry (pytest).
//   - `def setUp` / `def tearDown` / `def setup` / `def teardown` /
//     framework-lifecycle names → framework_lifecycle.
//   - Module-level `def <name>(` where the name does not start with `_`
//     → library_export (Python's PEP 8 visibility convention).
//   - Module-level `__all__ = [...]` membership → library_export
//     entries for every quoted name.
//
// Class-method visibility is NOT enumerated — methods inherit their
// class's reachability. The reachability pass handles that via the
// CONTAINS edge once the class is reached.
package substrate

import "regexp"

func init() { RegisterEntryPoints("python", sniffPythonEntryPoints) }

// pyTopLevelDefRe matches `def <name>(` only at column 0 — class
// methods (indented) are skipped.
//
// Capture group 1 = function name.
var pyTopLevelDefRe = regexp.MustCompile(`(?m)^def\s+([A-Za-z_][\w]*)\s*\(`)

// pyTopLevelClassRe matches `class <Name>` at column 0. Capture 1=name.
var pyTopLevelClassRe = regexp.MustCompile(`(?m)^class\s+([A-Za-z_][\w]*)\s*[(:]`)

// pyDunderMainRe matches the canonical `if __name__ == "__main__":`
// guard in either quote style and with optional whitespace.
var pyDunderMainRe = regexp.MustCompile(
	`(?m)^if\s+__name__\s*==\s*['"]__main__['"]\s*:`,
)

// pyAllAssignRe matches `__all__ = [ ... ]` (single-line or multi-line).
// Capture 1 = the list body so a follow-up scan can lift the names.
var pyAllAssignRe = regexp.MustCompile(
	`(?s)^__all__\s*=\s*\[(.*?)\]`,
)

// pyQuotedNameRe matches a quoted identifier inside an `__all__` list.
// Capture 1 = the bare identifier.
var pyQuotedNameRe = regexp.MustCompile(`['"]([A-Za-z_][\w]*)['"]`)

// pyLifecycleNames are pytest / unittest / framework method names that
// the runner invokes without an explicit caller. Module-level only;
// class-method versions are reached via the class's reachability.
var pyLifecycleNames = map[string]bool{
	"setup_module":    true,
	"teardown_module": true,
	"setup_function":  true,
	"teardown_function": true,
	"pytest_configure":  true,
	"conftest":          true,
}

func sniffPythonEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	// `if __name__ == "__main__":` block.
	for _, m := range pyDunderMainRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "__main__",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	// Top-level `def` declarations.
	for _, m := range pyTopLevelDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		switch {
		case name == "main":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindCLIMain})
		case isPythonTestName(name):
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindTestEntry})
		case pyLifecycleNames[name]:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindFrameworkLifecycle})
		case name[0] != '_':
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
		}
	}

	// Top-level `class` declarations — public unless underscore-prefixed.
	// A class named `Test*` is also a pytest test entry.
	for _, m := range pyTopLevelClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		if len(name) >= 4 && name[:4] == "Test" {
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindTestEntry})
			continue
		}
		if name[0] != '_' {
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
		}
	}

	// `__all__ = [ ... ]` — every quoted name becomes a library_export.
	for _, m := range pyAllAssignRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		for _, nm := range pyQuotedNameRe.FindAllStringSubmatch(body, -1) {
			if len(nm) < 2 {
				continue
			}
			out = append(out, EntryPoint{
				Ident: nm[1],
				Line:  line,
				Kind:  EntryKindLibraryExport,
			})
		}
	}

	return out
}

// isPythonTestName reports whether name matches the pytest function
// convention (`test_*` / `test`). Class-style tests are handled
// separately above.
func isPythonTestName(name string) bool {
	if name == "test" {
		return true
	}
	return len(name) > 5 && name[:5] == "test_"
}
