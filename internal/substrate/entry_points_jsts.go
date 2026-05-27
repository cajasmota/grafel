// JS/TS entry-point sniffer (#2766 Phase 1B T1).
//
// Recognises:
//   - `export function <name>` / `export const <name>` / `export class
//     <Name>` / `export default …` → library_export.
//   - `function main(` at module scope → cli_main.
//   - `it(` / `test(` / `describe(` at module scope → test_entry (one
//     entry per call site; the test runner invokes each).
//   - Common framework lifecycle names at module scope (`setup`,
//     `bootstrap`, `register`, `configure`) → framework_lifecycle.
//
// React/Vue component reachability is handled by RENDERS / NAVIGATES_TO
// edges in the graph, not here — the reachability pass picks those up
// directly.
package substrate

import "regexp"

func init() { RegisterEntryPoints("jsts", sniffJSTSEntryPoints) }

// jstsExportNamedRe matches `export function|class|const|let|var <name>`.
// Capture 1 = name.
var jstsExportNamedRe = regexp.MustCompile(
	`(?m)^export\s+(?:async\s+)?(?:function\*?|class|const|let|var|interface|type|enum)\s+([A-Za-z_$][\w$]*)`,
)

// jstsExportDefaultFnRe matches `export default function <name>(` and
// `export default class <Name>`. The default-export sniff applies
// whether or not a name is present. Capture 1 = optional name.
var jstsExportDefaultFnRe = regexp.MustCompile(
	`(?m)^export\s+default\s+(?:async\s+)?(?:function\*?|class)(?:\s+([A-Za-z_$][\w$]*))?`,
)

// jstsExportDefaultIdentRe matches `export default Identifier;` —
// captures the referenced identifier so the reachability pass can seed
// it directly (real-world React/Vue files commonly declare a `const
// Component = () => …;` then `export default Component;`).
var jstsExportDefaultIdentRe = regexp.MustCompile(
	`(?m)^export\s+default\s+([A-Za-z_$][\w$]*)\s*;?\s*$`,
)

// jstsExportDefaultAnyRe matches a bare `export default <expr>` line
// (catches `export default someValue;` and arrow-function defaults).
// We emit a synthetic "default" entry-point so the reachability pass
// can treat the file's default export as a starting point.
var jstsExportDefaultAnyRe = regexp.MustCompile(
	`(?m)^export\s+default\s+`,
)

// jstsReexportRe matches `export { name1, name2 as alias } from "…"`.
// We emit one entry-point per name on the LHS of the `as` (or the bare
// ident when there is no `as`).
var jstsReexportRe = regexp.MustCompile(
	`(?m)^export\s*\{([^}]*)\}`,
)

// jstsReexportIdentRe matches a single re-export specifier inside the
// braces — captures the source name (before optional ` as `).
var jstsReexportIdentRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)(?:\s+as\s+[A-Za-z_$][\w$]*)?`,
)

// jstsMainFnRe matches `function main(` at module scope.
var jstsMainFnRe = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+main\s*\(`)

// jstsTestCallRe matches a module-scope `it("…", …)` / `test("…", …)` /
// `describe("…", …)` invocation. Capture 1 = runner name.
var jstsTestCallRe = regexp.MustCompile(
	`(?m)^\s*(it|test|describe)\s*\(\s*['"\x60]([^'"\x60]{1,200})['"\x60]`,
)

// jstsLifecycleNames are framework-managed bootstrap names recognised
// at module scope.
var jstsLifecycleNames = map[string]bool{
	"setup":     true,
	"bootstrap": true,
	"register":  true,
	"configure": true,
	"start":     true,
	"init":      true,
	"plugin":    true,
}

// jstsTopLevelFnRe matches a module-scope `function <name>(` (no
// export keyword). Used to spot framework-lifecycle names declared
// without `export` in plugin files.
var jstsTopLevelFnRe = regexp.MustCompile(
	`(?m)^(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(`,
)

// jstsTopLevelConstFnRe matches a module-scope `const|let|var <Name> =`
// where the value is a function — covers React component declarations
// like `const ForgotPassword = () => { … }`. We only emit when the
// identifier is PascalCase, the conventional marker that the binding
// is a component/class/exported entry; lowercase locals are common
// helpers and would explode the seed set.
var jstsTopLevelConstFnRe = regexp.MustCompile(
	`(?m)^(?:const|let|var)\s+([A-Z][\w$]*)\s*=\s*(?:async\s+)?(?:\([^)]*\)\s*=>|function|React\.memo|forwardRef|memo)`,
)

func sniffJSTSEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	seenDefault := false

	for _, m := range jstsExportNamedRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	for _, m := range jstsExportDefaultFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		seenDefault = true
		name := "default"
		if m[2] >= 0 {
			name = content[m[2]:m[3]]
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	if !seenDefault {
		// First try the `export default Identifier;` form so the
		// reachability pass can seed the actual component / function
		// declared elsewhere in the file. Falls back to the synthetic
		// "default" ident only when the export is a non-trivial
		// expression (object literal, arrow function, etc.).
		identDefaults := jstsExportDefaultIdentRe.FindAllStringSubmatchIndex(content, -1)
		for _, m := range identDefaults {
			if len(m) < 4 {
				continue
			}
			out = append(out, EntryPoint{
				Ident: content[m[2]:m[3]],
				Line:  lineOfOffset(content, m[0]),
				Kind:  EntryKindLibraryExport,
			})
			seenDefault = true
		}
		if !seenDefault {
			for _, m := range jstsExportDefaultAnyRe.FindAllStringIndex(content, -1) {
				out = append(out, EntryPoint{
					Ident: "default",
					Line:  lineOfOffset(content, m[0]),
					Kind:  EntryKindLibraryExport,
				})
			}
		}
	}

	for _, m := range jstsReexportRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		for _, nm := range jstsReexportIdentRe.FindAllStringSubmatch(body, -1) {
			if len(nm) < 2 {
				continue
			}
			if nm[1] == "from" || nm[1] == "default" || nm[1] == "type" {
				continue
			}
			out = append(out, EntryPoint{
				Ident: nm[1],
				Line:  line,
				Kind:  EntryKindLibraryExport,
			})
		}
	}

	for _, m := range jstsMainFnRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	for _, m := range jstsTestCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		runner := content[m[2]:m[3]]
		out = append(out, EntryPoint{
			Ident: runner,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	for _, m := range jstsTopLevelConstFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	for _, m := range jstsTopLevelFnRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if jstsLifecycleNames[name] {
			out = append(out, EntryPoint{
				Ident: name,
				Line:  lineOfOffset(content, m[0]),
				Kind:  EntryKindFrameworkLifecycle,
			})
		}
	}

	return out
}
