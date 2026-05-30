// Swift entry-point sniffer (Phase 1B T3).
//
// Recognises:
//   - `@main` attribute on a type → cli_main (Swift 5.3+ @main convention)
//   - `static func main(` at type scope → cli_main
//   - Vapor `app.run()` / `try app.run()` → framework_lifecycle
//   - Vapor route-handler registrations (in `func boot(routes:)`) → framework_lifecycle
//   - `func test<Name>(` in an XCTestCase subclass → test_entry
//   - `func setUp(` / `func tearDown(` in an XCTestCase subclass → test_entry
//   - `public func <Name>(` at top level → library_export
//   - `open func <Name>(` at top level → library_export
//
// The reachability pass in internal/links/reachability.go consumes these
// entry points and seeds a BFS across CALLS edges.
package substrate

import "regexp"

func init() { RegisterEntryPoints("swift", sniffSwiftEntryPoints) }

// swiftAtMainRe matches the `@main` attribute on a type declaration.
// It fires on the line that contains `@main` followed (optionally on the
// same line) by a struct/class/enum keyword.
var swiftAtMainRe = regexp.MustCompile(
	`(?m)^\s*@main\b`,
)

// swiftStaticMainRe matches `static func main(` inside a type body.
// Capture group 1 = "main".
var swiftStaticMainRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+|open\s+)?static\s+func\s+(main)\s*\(`,
)

// swiftVaporRunRe matches `app.run()` / `try app.run()` / `application.run()`.
var swiftVaporRunRe = regexp.MustCompile(
	`(?m)^\s*(?:try\s+)?(?:app|application)\s*\.\s*run\s*\(`,
)

// swiftVaporBootRe matches `func boot(routes:` and `func configure(` — Vapor
// Application lifecycle hooks.
var swiftVaporBootRe = regexp.MustCompile(
	`(?m)^\s*(?:public\s+|internal\s+)?func\s+(boot|configure|didBoot|shutdown)\s*\(`,
)

// swiftXCTestRe matches XCTest test methods: `func test<Name>(`
// and `func setUp(` / `func tearDown(`.
// Capture group 1 = the method name.
var swiftXCTestRe = regexp.MustCompile(
	`(?m)^\s*(?:override\s+)?func\s+(test[A-Z][\w]*|setUp|tearDown|tearDownWithError|setUpWithError)\s*\(`,
)

// swiftPublicFuncRe matches top-level or type-scope `public func <name>(`
// and `open func <name>(`. Capture group 1 = the function name.
var swiftPublicFuncRe = regexp.MustCompile(
	`(?m)^\s*(?:public|open)\s+(?:static\s+|class\s+|final\s+)?func\s+([A-Za-z_][\w]*)\s*[(<]`,
)

func sniffSwiftEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	seen := map[string]bool{}

	// @main → cli_main (synthetic ident "__swift_main__")
	for _, m := range swiftAtMainRe.FindAllStringIndex(content, -1) {
		key := "cli:__swift_main__"
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: "__swift_main__",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	// static func main( → cli_main
	for _, m := range swiftStaticMainRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		key := "cli:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	// app.run() → framework_lifecycle
	for _, m := range swiftVaporRunRe.FindAllStringIndex(content, -1) {
		key := "lifecycle:app.run"
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: "app.run",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	// boot/configure/didBoot/shutdown → framework_lifecycle
	for _, m := range swiftVaporBootRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		key := "lifecycle:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindFrameworkLifecycle,
		})
	}

	// XCTest methods → test_entry
	for _, m := range swiftXCTestRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		key := "test:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	// public/open func → library_export
	for _, m := range swiftPublicFuncRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		key := "export:" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	return out
}
