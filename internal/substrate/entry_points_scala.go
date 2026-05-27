// Scala entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `def main(args: Array[String])` → cli_main. Also `def main()`
//     with optional `def main(): Unit =`.
//   - `object <Name> extends App` declarations → cli_main (the App
//     trait synthesises a `main` method).
//   - ScalaTest / MUnit annotations and test idioms — methods preceded
//     by `@Test`, `@org.junit.Test`, calls to `test("name")` at module
//     scope, and `it should "name" in { … }` blocks → test_entry.
//   - Lifecycle annotations / framework hooks (`@PostConstruct`,
//     `@PreDestroy`, `@BeforeAll`, `@AfterAll`, `@BeforeEach`,
//     `@AfterEach`) → framework_lifecycle.
//   - `def <name>(` at module scope without a `private` / `protected`
//     modifier → library_export.
//   - `class|object|trait <Name>` declarations without a `private`
//     modifier → library_export.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("scala", sniffScalaEntryPoints) }

// scalaMainFnRe matches a `def main(` declaration.
var scalaMainFnRe = regexp.MustCompile(`(?m)^[ \t]*def\s+main\s*\(`)

// scalaAppObjectRe matches `object <Name> extends App` (or `extends
// IOApp` / `extends ZIOAppDefault` — common cats-effect / ZIO patterns).
// Capture 1 = object name.
var scalaAppObjectRe = regexp.MustCompile(
	`(?m)^[ \t]*object\s+([A-Z]\w*)\s+extends\s+(?:App|IOApp(?:\.\w+)?|ZIOAppDefault|ZIOApp)\b`,
)

// scalaDefRe matches any `def <name>(` declaration. Capture 1 = optional
// visibility, capture 2 = name.
var scalaDefRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(private|protected)(?:\[[^\]]+\])?\s+)?(?:override\s+|final\s+|implicit\s+|inline\s+|sealed\s+)*def\s+([A-Za-z_]\w*)\s*[\[(]`,
)

// scalaClassRe matches `class|object|trait <Name>` declarations.
// Capture 1 = optional visibility; capture 2 = name.
var scalaClassRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(private|protected)(?:\[[^\]]+\])?\s+)?(?:abstract\s+|sealed\s+|final\s+|case\s+|implicit\s+)*(?:class|object|trait)\s+([A-Z]\w*)`,
)

// scalaAnnotationRe matches `@Annotation` lines. Capture 1 = annotation
// short-name (last segment after dots).
var scalaAnnotationRe = regexp.MustCompile(`(?m)^[ \t]*@(?:[\w.]+\.)?([A-Za-z_]\w*)`)

// scalaTestCallRe matches a module-scope test invocation:
// `test("name") { … }` (MUnit / utest), and `it should "name" in {`
// (ScalaTest FlatSpec).
var scalaTestCallRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:test|it\s+should|"[^"]+"\s+in)\s*[("]`,
)

var scalaTestAnnotations = map[string]bool{
	"Test":              true,
	"ParameterizedTest": true,
	"Fact":              true,
	"Theory":            true,
}

var scalaLifecycleAnnotations = map[string]bool{
	"PostConstruct":     true,
	"PreDestroy":        true,
	"BeforeAll":         true,
	"AfterAll":          true,
	"BeforeEach":        true,
	"AfterEach":         true,
	"Before":            true,
	"After":             true,
	"EventListener":     true,
	"Scheduled":         true,
}

func sniffScalaEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	mainLines := map[int]bool{}

	for _, m := range scalaMainFnRe.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	appObjectNames := map[string]bool{}
	for _, m := range scalaAppObjectRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		appObjectNames[name] = true
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindCLIMain,
		})
	}

	testAnnLines := map[int]bool{}
	lifecycleAnnLines := map[int]bool{}
	for _, m := range scalaAnnotationRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		if scalaTestAnnotations[name] {
			testAnnLines[line] = true
		}
		if scalaLifecycleAnnotations[name] {
			lifecycleAnnLines[line] = true
		}
	}

	lines := strings.Split(content, "\n")

	for _, m := range scalaDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		vis := ""
		if m[2] >= 0 {
			vis = content[m[2]:m[3]]
		}
		name := content[m[4]:m[5]]
		line := lineOfOffset(content, m[0])
		if mainLines[line] || name == "main" {
			continue
		}
		kind := EntryKindLibraryExport
		if vis == "private" || vis == "protected" {
			kind = EntryKind("")
		}
		// Annotation lookback.
	scan:
		for back := 1; back <= 5; back++ {
			lineNo := line - back
			if lineNo < 1 || lineNo > len(lines) {
				break
			}
			trimmed := strings.TrimSpace(lines[lineNo-1])
			if trimmed == "" {
				continue
			}
			if testAnnLines[lineNo] {
				kind = EntryKindTestEntry
				break scan
			}
			if lifecycleAnnLines[lineNo] {
				kind = EntryKindFrameworkLifecycle
				break scan
			}
			if strings.HasPrefix(trimmed, "@") {
				continue
			}
			break
		}
		if kind != "" {
			out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
		}
	}

	for _, m := range scalaClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		vis := ""
		if m[2] >= 0 {
			vis = content[m[2]:m[3]]
		}
		if vis == "private" || vis == "protected" {
			continue
		}
		name := content[m[4]:m[5]]
		if appObjectNames[name] {
			continue // already emitted as cli_main above
		}
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	for _, m := range scalaTestCallRe.FindAllStringIndex(content, -1) {
		out = append(out, EntryPoint{
			Ident: "test",
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindTestEntry,
		})
	}

	return out
}
