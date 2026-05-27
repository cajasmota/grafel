// Elixir entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `def main(` at top level → cli_main. The escript / Mix.Task
//     contract names this function.
//   - ExUnit test macros — `test "name" do …` at module scope →
//     test_entry. Capture the literal so multiple tests in one module
//     each get their own entry.
//   - GenServer / Supervisor / Phoenix lifecycle callbacks
//     (`init/1`, `start_link/1`, `handle_call`, `handle_cast`,
//     `handle_info`, `handle_continue`, `terminate/2`, `code_change/3`,
//     `child_spec/1`) → framework_lifecycle.
//   - Phoenix callbacks (`mount/3`, `render/1`, `handle_event/3`,
//     `handle_params/3`) → framework_lifecycle.
//   - `def <name>(` declarations (not `defp`) → library_export. Elixir's
//     visibility model is explicit: `def` is public, `defp` is private.
package substrate

import "regexp"

func init() { RegisterEntryPoints("elixir", sniffElixirEntryPoints) }

// elixirDefRe matches a `def <name>(` declaration at any indentation
// level. Capture 1 = name. We deliberately do NOT match `defp` so
// private functions are skipped.
var elixirDefRe = regexp.MustCompile(`(?m)^[ \t]*def\s+([a-z_][\w]*[!?]?)\s*[(\s]`)

// elixirTestRe matches an ExUnit `test "label" do` macro. Capture 1 =
// the label literal.
var elixirTestRe = regexp.MustCompile(
	`(?m)^[ \t]*test\s+["']([^"']{1,200})["']\s*(?:,\s*\w+\s*)?do\b`,
)

// elixirDescribeRe matches an ExUnit `describe "label" do` macro.
// Capture 1 = label.
var elixirDescribeRe = regexp.MustCompile(
	`(?m)^[ \t]*describe\s+["']([^"']{1,200})["']\s*do\b`,
)

// elixirLifecycleNames are GenServer / Supervisor / Phoenix callbacks
// the runtime invokes without a static caller.
var elixirLifecycleNames = map[string]bool{
	"init":            true,
	"start":           true,
	"start_link":      true,
	"start_child":     true,
	"stop":            true,
	"terminate":       true,
	"handle_call":     true,
	"handle_cast":     true,
	"handle_info":     true,
	"handle_continue": true,
	"handle_event":    true,
	"handle_params":   true,
	"code_change":     true,
	"child_spec":      true,
	"mount":           true,
	"render":          true,
	"format_status":   true,
	"setup":           true,
	"setup_all":       true,
}

func sniffElixirEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	for _, m := range elixirDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		switch {
		case name == "main":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindCLIMain})
		case elixirLifecycleNames[name]:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindFrameworkLifecycle})
		default:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
		}
	}

	for _, m := range elixirTestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	for _, m := range elixirDescribeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	return out
}
