// Kotlin entry-point sniffer (#2767 Phase 1B T2).
//
// Recognises:
//   - `fun main(` at top level → cli_main. Both `fun main()` and
//     `fun main(args: Array<String>)` are recognised.
//   - Functions preceded by `@Test`, `@ParameterizedTest`,
//     `@RepeatedTest`, `@TestFactory` → test_entry.
//   - Functions preceded by Spring / Quarkus lifecycle annotations
//     (`@PostConstruct`, `@PreDestroy`, `@EventListener`, `@Scheduled`,
//     `@BeforeEach`, `@AfterEach`, `@BeforeAll`, `@AfterAll`,
//     `@BeforeClass`, `@AfterClass`) → framework_lifecycle.
//   - `fun <name>(` declarations at top level (no `private` / `internal`
//     visibility modifier) → library_export. Kotlin top-level functions
//     are public by default.
//   - `class <Name>` declarations at top level → library_export.
//   - Test class methods declared as `@Test fun <name>(` are handled
//     by the annotation lookback regardless of class-method nesting.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("kotlin", sniffKotlinEntryPoints) }

// kotlinMainFnRe matches `fun main(` at column 0 with optional `public`
// / `internal` modifier and `suspend` keyword.
var kotlinMainFnRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:public\s+|internal\s+)?(?:suspend\s+)?fun\s+main\s*\(`,
)

// kotlinFunRe matches any `fun <name>(` declaration. Capture 1 = optional
// visibility modifier (`private`/`internal`/`protected`/`public`); capture
// 2 = name.
var kotlinFunRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(public|private|internal|protected)\s+)?(?:suspend\s+|inline\s+|operator\s+|tailrec\s+|infix\s+|external\s+|override\s+|open\s+|final\s+|abstract\s+)*fun\s+(?:<[^>]+>\s+)?([A-Za-z_]\w*)\s*\(`,
)

// kotlinClassRe matches a `class|object|interface <Name>` declaration.
// Capture 1 = optional visibility modifier; capture 2 = name.
var kotlinClassRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(public|private|internal)\s+)?(?:abstract\s+|open\s+|final\s+|sealed\s+|data\s+|enum\s+|annotation\s+|inner\s+|inline\s+|value\s+)*(?:class|object|interface)\s+([A-Z]\w*)`,
)

// kotlinAnnotationRe matches a `@Annotation` or `@Annotation(args)` line.
// Capture 1 = annotation name (without args).
var kotlinAnnotationRe = regexp.MustCompile(`(?m)^[ \t]*@([A-Za-z_]\w*)`)

var kotlinTestAnnotations = map[string]bool{
	"Test":              true,
	"ParameterizedTest": true,
	"RepeatedTest":      true,
	"TestFactory":       true,
	"TestTemplate":      true,
	"Fact":              true,
	"Theory":            true,
}

var kotlinLifecycleAnnotations = map[string]bool{
	"PostConstruct":     true,
	"PreDestroy":        true,
	"EventListener":     true,
	"Scheduled":         true,
	"BeforeEach":        true,
	"AfterEach":         true,
	"BeforeAll":         true,
	"AfterAll":          true,
	"BeforeClass":       true,
	"AfterClass":        true,
	"Before":            true,
	"After":             true,
	"Startup":           true,
	"Initialized":       true,
	"ApplicationScoped": true,
	"Singleton":         true,
}

func sniffKotlinEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint
	mainLines := map[int]bool{}

	for _, m := range kotlinMainFnRe.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	testAnnLines := map[int]bool{}
	lifecycleAnnLines := map[int]bool{}
	for _, m := range kotlinAnnotationRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		if kotlinTestAnnotations[name] {
			testAnnLines[line] = true
		}
		if kotlinLifecycleAnnotations[name] {
			lifecycleAnnLines[line] = true
		}
	}

	lines := strings.Split(content, "\n")

	for _, m := range kotlinFunRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		vis := ""
		if m[2] >= 0 {
			vis = content[m[2]:m[3]]
		}
		name := content[m[4]:m[5]]
		line := lineOfOffset(content, m[0])
		if mainLines[line] {
			continue
		}
		if vis == "private" || vis == "internal" || vis == "protected" {
			// Annotation lookback can still elevate to test/lifecycle.
			kind := EntryKind("")
		scanPriv:
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
					break scanPriv
				}
				if lifecycleAnnLines[lineNo] {
					kind = EntryKindFrameworkLifecycle
					break scanPriv
				}
				if strings.HasPrefix(trimmed, "@") {
					continue
				}
				break
			}
			if kind != "" {
				out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
			}
			continue
		}
		kind := EntryKindLibraryExport
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
		out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
	}

	for _, m := range kotlinClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		vis := ""
		if m[2] >= 0 {
			vis = content[m[2]:m[3]]
		}
		if vis == "private" || vis == "internal" {
			continue
		}
		name := content[m[4]:m[5]]
		out = append(out, EntryPoint{
			Ident: name,
			Line:  lineOfOffset(content, m[0]),
			Kind:  EntryKindLibraryExport,
		})
	}

	return out
}
