// PHP entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `<?php` opening tag at offset 0 → cli_main (synthetic ident
//     "__main__"). Every PHP script with a top-level executable body
//     is a process entry from the PHP-CLI / FPM dispatcher's view.
//   - `function main(` at module scope → cli_main.
//   - PHPUnit test methods: `function test<Name>(` and methods preceded
//     by `@test` docblock annotation → test_entry.
//   - Laravel / Symfony framework lifecycle methods on Kernel /
//     ServiceProvider classes (`register`, `boot`, `bootstrap`,
//     `configure`, `terminate`, `handle`) → framework_lifecycle.
//   - `public function <name>(` declarations → library_export. PHP
//     defaults to public visibility when no modifier is present, so we
//     also catch bare `function <name>(` at top-of-class level.
//   - `Route::get|post|put|patch|delete|options|any|match` chained
//     calls bind a closure / controller method as a route entry —
//     handled by the existing http_endpoint extractor; not duplicated
//     here.
//
// Class-scoping is not tracked statically; methods inherit their
// class's reachability via the CONTAINS edge.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("php", sniffPHPEntryPoints) }

// phpOpenTagRe matches the canonical `<?php` opening tag anywhere near
// the start of file. We accept whitespace before it for templated
// scripts that start with a BOM or blank line.
var phpOpenTagRe = regexp.MustCompile(`\A\s*<\?php\b`)

// phpFunctionRe matches a `function <name>(` declaration. Capture 1 =
// optional visibility modifier (`public`/`protected`/`private`/empty);
// capture 2 = function name.
var phpFunctionRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(public|protected|private)\s+)?(?:static\s+)?(?:final\s+)?(?:abstract\s+)?function\s+([A-Za-z_]\w*)\s*\(`,
)

// phpTestAnnotationRe matches a `@test` docblock annotation. The
// annotation typically sits on its own line inside a `/** … */` block.
// Capture: none — we just need the line number.
var phpTestAnnotationRe = regexp.MustCompile(`(?m)^[ \t]*\*\s*@test\b`)

// phpLifecycleMethodNames are Laravel / Symfony framework hooks.
var phpLifecycleMethodNames = map[string]bool{
	"register":   true,
	"boot":       true,
	"bootstrap":  true,
	"configure":  true,
	"terminate":  true,
	"handle":     true,
	"setUp":      true,
	"tearDown":   true,
	"setUpBeforeClass": true,
	"tearDownAfterClass": true,
}

func sniffPHPEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	if phpOpenTagRe.MatchString(content) {
		out = append(out, EntryPoint{
			Ident: "__main__",
			Line:  1,
			Kind:  EntryKindCLIMain,
		})
	}

	// Pre-index @test annotation lines so the next function declaration
	// can be classified as a test entry.
	testAnnLines := map[int]bool{}
	for _, m := range phpTestAnnotationRe.FindAllStringIndex(content, -1) {
		testAnnLines[lineOfOffset(content, m[0])] = true
	}

	lines := strings.Split(content, "\n")

	for _, m := range phpFunctionRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		vis := ""
		if m[2] >= 0 {
			vis = content[m[2]:m[3]]
		}
		name := content[m[4]:m[5]]
		line := lineOfOffset(content, m[0])

		// Hidden by explicit private/protected visibility — skip.
		if vis == "private" || vis == "protected" {
			continue
		}

		// Walk up to 8 lines of docblock to detect @test annotation.
		isTest := false
	scan:
		for back := 1; back <= 8; back++ {
			lineNo := line - back
			if lineNo < 1 || lineNo > len(lines) {
				break
			}
			if testAnnLines[lineNo] {
				isTest = true
				break scan
			}
			trimmed := strings.TrimSpace(lines[lineNo-1])
			if trimmed == "" || strings.HasPrefix(trimmed, "*") ||
				strings.HasPrefix(trimmed, "/**") || strings.HasPrefix(trimmed, "*/") {
				continue
			}
			break
		}

		switch {
		case name == "main":
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindCLIMain})
		case isPHPTestName(name) || isTest:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindTestEntry})
		case phpLifecycleMethodNames[name]:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindFrameworkLifecycle})
		default:
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: EntryKindLibraryExport})
		}
	}

	return out
}

// isPHPTestName reports whether name follows the PHPUnit `test<Name>`
// convention.
func isPHPTestName(name string) bool {
	if len(name) < 5 || name[:4] != "test" {
		return false
	}
	c := name[4]
	return c >= 'A' && c <= 'Z'
}
