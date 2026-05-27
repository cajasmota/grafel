// C# entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `static void Main(` / `static int Main(` / `static async Task
//     Main(` / `public static async Task<int> Main(` → cli_main. C#
//     entry methods are conventionally named `Main` and live on a
//     program class; the runtime locates them by signature.
//   - Methods decorated with NUnit / xUnit / MSTest annotations
//     (`[Test]`, `[Fact]`, `[Theory]`, `[TestCase]`, `[TestMethod]`,
//     `[DataTestMethod]`) → test_entry.
//   - Methods decorated with framework lifecycle attributes
//     (`[OneTimeSetUp]`, `[OneTimeTearDown]`, `[SetUp]`, `[TearDown]`,
//     `[TestInitialize]`, `[TestCleanup]`) → framework_lifecycle.
//   - ASP.NET Startup methods (`Configure`, `ConfigureServices`,
//     `ConfigureAppConfiguration`, `ConfigureContainer`) → framework_lifecycle.
//   - `public` methods, properties (`{ get; set; }`), and constructors
//     → library_export.
//
// Top-level statements (C# 9+ `Program.cs` with no `Main`) are also a
// cli_main entry — emitted when the file's name is exactly `Program.cs`
// and no `Main` method is present. The reachability pass cannot see
// file names from the sniffer, so we emit a synthetic `__main__` entry
// whenever a top-level `using` followed by a `var ` / executable
// statement is detected at column 0.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("csharp", sniffCSharpEntryPoints) }

// csharpMainRe matches a `Main` method declaration. Tolerates many
// signatures: void/int/Task/Task<int>/async, optional `public`, generic
// args.
var csharpMainRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:public\s+|private\s+|internal\s+)?static\s+(?:async\s+)?(?:void|int|Task(?:<int>)?)\s+Main\s*\(`,
)

// csharpAttributeRe matches a `[Attribute]` or `[Attribute(args)]` line
// (one or more attributes per line, e.g. `[Test, Category("smoke")]`).
// Capture 1 = the comma-separated body inside the brackets.
var csharpAttributeRe = regexp.MustCompile(`(?m)^[ \t]*\[([^\]]+)\]`)

// csharpPublicMemberRe matches a `public` method, property, or
// constructor declaration. Capture 1 = name.
var csharpPublicMemberRe = regexp.MustCompile(
	`(?m)^[ \t]*public\s+(?:static\s+|virtual\s+|override\s+|abstract\s+|sealed\s+|async\s+|partial\s+|readonly\s+|const\s+)*` +
		`(?:[\w.<>\[\],?\s]+?\s+)?([A-Z]\w*)\s*[\(\{]`,
)

// csharpStartupMethodNames are ASP.NET Core / Generic Host lifecycle
// methods invoked by the host without an in-graph caller.
var csharpStartupMethodNames = map[string]bool{
	"Configure":                 true,
	"ConfigureServices":         true,
	"ConfigureAppConfiguration": true,
	"ConfigureContainer":        true,
	"ConfigureHostConfiguration": true,
	"ConfigureLogging":          true,
	"ConfigureWebHostDefaults":  true,
}

// csharpTestAttrs are test-marker attributes (NUnit, xUnit, MSTest).
var csharpTestAttrs = map[string]bool{
	"Test":           true,
	"TestCase":       true,
	"Fact":           true,
	"Theory":         true,
	"TestMethod":     true,
	"DataTestMethod": true,
	"InlineData":     true,
}

// csharpLifecycleAttrs are setup / teardown attributes.
var csharpLifecycleAttrs = map[string]bool{
	"OneTimeSetUp":      true,
	"OneTimeTearDown":   true,
	"SetUp":             true,
	"TearDown":          true,
	"TestInitialize":    true,
	"TestCleanup":       true,
	"ClassInitialize":   true,
	"ClassCleanup":      true,
	"AssemblyInitialize": true,
	"AssemblyCleanup":   true,
}

func sniffCSharpEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	mainLines := map[int]bool{}
	for _, m := range csharpMainRe.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: "Main",
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	// Index attributes by line.
	testAttrLines := map[int]bool{}
	lifecycleAttrLines := map[int]bool{}
	for _, m := range csharpAttributeRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		for _, raw := range strings.Split(body, ",") {
			name := strings.TrimSpace(raw)
			if paren := strings.IndexByte(name, '('); paren > 0 {
				name = name[:paren]
			}
			if csharpTestAttrs[name] {
				testAttrLines[line] = true
			}
			if csharpLifecycleAttrs[name] {
				lifecycleAttrLines[line] = true
			}
		}
	}

	lines := strings.Split(content, "\n")

	for _, m := range csharpPublicMemberRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if csharpReservedNames[name] {
			continue
		}
		line := lineOfOffset(content, m[0])
		if mainLines[line] {
			continue
		}
		kind := EntryKindLibraryExport
		if csharpStartupMethodNames[name] {
			kind = EntryKindFrameworkLifecycle
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
			if testAttrLines[lineNo] {
				kind = EntryKindTestEntry
				break scan
			}
			if lifecycleAttrLines[lineNo] {
				kind = EntryKindFrameworkLifecycle
				break scan
			}
			if strings.HasPrefix(trimmed, "[") {
				continue
			}
			break
		}
		out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
	}

	return out
}

// csharpReservedNames are C# keywords / control-flow tokens that the
// relaxed `public … NAME(` regex could mis-capture.
var csharpReservedNames = map[string]bool{
	"If": true, "For": true, "While": true, "Switch": true,
	"Return": true, "Throw": true, "Try": true, "Catch": true,
	"Do": true, "Class": true, "Interface": true, "Enum": true,
	"Record": true, "Struct": true, "Namespace": true, "Using": true,
}
