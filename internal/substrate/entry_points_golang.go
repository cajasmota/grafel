// Go entry-point sniffer (#2766 Phase 1B T1).
//
// Recognises:
//   - `func main()` at top level → cli_main.
//   - `func init()` at top level → framework_lifecycle (Go's runtime init).
//   - `func Test<Name>(t *testing.T)` / `func Benchmark<Name>` /
//     `func Example<Name>` / `func Fuzz<Name>` → test_entry.
//   - `func <Capitalised>(…)` and `func (r *T) <Capitalised>(…)` at top
//     level → library_export (Go's public-visibility rule: any
//     capitalised top-level identifier is exported).
package substrate

import "regexp"

func init() { RegisterEntryPoints("go", sniffGoEntryPoints) }

// goEntryFuncRe matches a top-level `func` declaration. Capture groups:
//
//	1 = optional receiver block (e.g. "(r *T) ") — empty for plain funcs.
//	2 = function name.
var goEntryFuncRe = regexp.MustCompile(
	`(?m)^func\s+(\([^)]+\)\s+)?([A-Za-z_][\w]*)\s*\(`,
)

// goTestNameRe matches the canonical Go test-runner entry-point name
// prefixes. Anchored so e.g. "Testify" (not a real test) does not match
// — the next char must be a capital, underscore, or end-of-name.
var goTestNameRe = regexp.MustCompile(`^(Test|Benchmark|Example|Fuzz)([A-Z_]|$)`)

func sniffGoEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	for _, m := range goEntryFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[4]:m[5]]
		line := lineOfOffset(content, m[0])
		switch {
		case name == "main":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindCLIMain})
		case name == "init":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindFrameworkLifecycle})
		case goTestNameRe.MatchString(name):
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindTestEntry})
		default:
			// Capitalised → exported. Lowercased → package-private,
			// not an entry point.
			if c := name[0]; c >= 'A' && c <= 'Z' {
				out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
			}
		}
	}
	return out
}
