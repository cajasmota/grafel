// Ruby entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `#!/usr/bin/env ruby` shebang at line 1 → cli_main (synthetic
//     ident "__main__"). Any shebang containing the word "ruby" counts.
//   - `def main(` at top level → cli_main.
//   - RSpec `describe`/`it`/`context` block calls at module scope and
//     Minitest `def test_<name>(` declarations → test_entry.
//   - Cucumber step definitions (`Given`/`When`/`Then`/`And`/`But`)
//     at module scope → test_entry.
//   - Rails-style lifecycle hooks (`initializer`, `setup`, `before_action`,
//     `after_action`, `around_action`, `before_save`, `after_save`,
//     `Rails.application.configure`, `Rails.application.routes.draw`,
//     `ActiveSupport.on_load`) → framework_lifecycle.
//   - Module-level `def <name>(` declarations whose name is not `_`-
//     prefixed → library_export. Ruby methods default to public unless
//     a `private`/`protected` visibility modifier flips them; the
//     reachability pass treats public methods as exports.
//
// Class-method visibility is NOT tracked statically — methods inherit
// their class's reachability via the CONTAINS edge.
package substrate

import "regexp"

func init() { RegisterEntryPoints("ruby", sniffRubyEntryPoints) }

// rubyShebangRe matches a `#!` line on line 1 whose interpreter mentions
// ruby. Anchored to the start of file.
var rubyShebangRe = regexp.MustCompile(`\A#![^\n]*\bruby\b`)

// rubyTopLevelDefRe matches `def <name>` at column 0. Capture 1 = name.
// The receiver form `def self.<name>` is also caught — capture group 2
// holds the bare name in that case.
var rubyTopLevelDefRe = regexp.MustCompile(`(?m)^def\s+(?:self\.)?([A-Za-z_][\w]*[!?=]?)`)

// rubyRSpecBlockRe matches an RSpec / Minitest spec block: `describe`,
// `it`, `context`, `specify`, `feature`, `scenario` at module scope.
var rubyRSpecBlockRe = regexp.MustCompile(
	`(?m)^[ \t]*(describe|context|it|specify|feature|scenario|example)\s*[("']`,
)

// rubyCucumberStepRe matches a Cucumber step definition at module scope.
// Capture 1 = step keyword.
var rubyCucumberStepRe = regexp.MustCompile(
	`(?m)^[ \t]*(Given|When|Then|And|But)\s+[/("']`,
)

// rubyLifecycleNames are method names invoked by Rails / Rake / Sinatra
// / DSL-style frameworks without a static caller.
var rubyLifecycleNames = map[string]bool{
	"initializer":     true,
	"setup":           true,
	"teardown":        true,
	"before_action":   true,
	"after_action":    true,
	"around_action":   true,
	"before_save":     true,
	"after_save":      true,
	"before_create":   true,
	"after_create":    true,
	"before_filter":   true,
	"after_filter":    true,
	"configure":       true,
	"register":        true,
	"boot":            true,
}

// rubyLifecycleCallRe matches a module-scope DSL call whose first token
// is a known lifecycle method (`Rails.application.configure do …`,
// `ActiveSupport.on_load(:active_record) do …`, etc.). Capture 1 = the
// final method name on the call chain.
var rubyLifecycleCallRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:[A-Z][\w:]*\.)*([a-z_][\w]*)\b`,
)

func sniffRubyEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	if rubyShebangRe.MatchString(content) {
		out = append(out, EntryPoint{
			Ident: "__main__",
			Line:  1,
			Kind:  EntryKindCLIMain,
		})
	}

	for _, m := range rubyTopLevelDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		switch {
		case name == "main":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindCLIMain})
		case isRubyTestMethodName(name):
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindTestEntry})
		case rubyLifecycleNames[name]:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindFrameworkLifecycle})
		case name[0] != '_':
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
		}
	}

	for _, m := range rubyRSpecBlockRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	for _, m := range rubyCucumberStepRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, EntryPoint{
			Ident: content[m[2]:m[3]],
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	// Lifecycle calls — gated on the dictionary so generic method calls
	// like `puts "hi"` do not all become lifecycle entries.
	for _, m := range rubyLifecycleCallRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if !rubyLifecycleNames[name] {
			continue
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	return out
}

// isRubyTestMethodName reports whether name matches the Minitest
// convention (`test_*`).
func isRubyTestMethodName(name string) bool {
	return len(name) > 5 && name[:5] == "test_"
}
