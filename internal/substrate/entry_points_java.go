// Java entry-point sniffer (#2766 Phase 1B T1).
//
// Recognises:
//   - `public static void main(String[]` → cli_main.
//   - `@Test` / `@ParameterizedTest` / `@RepeatedTest` annotations on
//     the line immediately preceding a method declaration → test_entry.
//   - `public` method or constructor declarations → library_export.
//   - Common Spring/Quarkus framework lifecycle annotations
//     (`@PostConstruct`, `@PreDestroy`, `@EventListener`, `@Scheduled`)
//     → framework_lifecycle.
//
// Class-level visibility is NOT enumerated separately; a class is
// reachable when any of its public methods is reachable, via the
// CONTAINS edge owned by the reachability pass.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterEntryPoints("java", sniffJavaEntryPoints) }

// javaMainRe matches the canonical Java `main` declaration. Tolerant
// of `String...` varargs and whitespace.
var javaMainRe = regexp.MustCompile(
	`(?m)public\s+static\s+(?:final\s+)?void\s+main\s*\(\s*(?:final\s+)?String\s*(?:\[\s*\]|\.{3})\s*[A-Za-z_]\w*\s*\)`,
)

// javaPublicMethodRe matches a public method or constructor
// declaration anchored on a line. Capture group 1 = method name.
// Tolerates common modifiers (`static`, `final`, generics, return
// type). Constructors are caught by the same pattern because Java
// constructors have no return-type token — we relax `(?:[\w<>\[\],?\s]+\s+)?`
// to match both.
var javaPublicMethodRe = regexp.MustCompile(
	`(?m)^[ \t]*public\s+(?:static\s+|final\s+|abstract\s+|synchronized\s+|<[^>]+>\s+)*` +
		`(?:[\w.<>\[\],?\s]+?\s+)?([A-Za-z_]\w*)\s*\(`,
)

// javaTestAnnotationRe matches lines that carry a recognised test
// annotation. The reachability marking applies to the method on the
// following declaration, so the sniffer pairs the annotation line with
// the next `javaPublicMethodRe` match (handled in sniffJavaEntryPoints).
var javaTestAnnotationRe = regexp.MustCompile(
	`(?m)^[ \t]*@(Test|ParameterizedTest|RepeatedTest|TestFactory|TestTemplate|BeforeEach|BeforeAll|AfterEach|AfterAll|Before|After|BeforeClass|AfterClass)\b`,
)

// javaLifecycleAnnotationRe matches Spring/Quarkus/CDI lifecycle hooks
// that a container invokes without an in-graph caller.
var javaLifecycleAnnotationRe = regexp.MustCompile(
	`(?m)^[ \t]*@(PostConstruct|PreDestroy|EventListener|Scheduled|Startup|Observes|OnEvent|Initialized|Destroyed|ApplicationScoped|Singleton|RequestScoped|SessionScoped)\b`,
)

func sniffJavaEntryPoints(content string) []EntryPoint {
	if content == "" {
		return nil
	}
	var out []EntryPoint

	// main(String[] …) — emit once per match, record its line so the
	// public-method scan below does not also classify it as
	// library_export.
	mainLines := map[int]bool{}
	for _, m := range javaMainRe.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		mainLines[line] = true
		out = append(out, EntryPoint{
			Ident: "main",
			Line:  line,
			Kind:  EntryKindCLIMain,
		})
	}

	// Index annotation positions by line so we can pair them with the
	// next method declaration.
	testAnnLines := map[int]bool{}
	lifecycleAnnLines := map[int]bool{}
	for _, m := range javaTestAnnotationRe.FindAllStringIndex(content, -1) {
		testAnnLines[lineOfOffset(content, m[0])] = true
	}
	for _, m := range javaLifecycleAnnotationRe.FindAllStringIndex(content, -1) {
		lifecycleAnnLines[lineOfOffset(content, m[0])] = true
	}

	// Pre-split into lines so the annotation lookback can tell apart
	// "annotation directly above this method" from "annotation
	// belongs to a different method earlier in the file".
	lines := strings.Split(content, "\n")
	for _, m := range javaPublicMethodRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		if javaReservedNames[name] {
			continue
		}
		line := lineOfOffset(content, m[0])
		if mainLines[line] {
			continue // already emitted as cli_main above
		}
		// Look backwards line-by-line: a contiguous run of annotation
		// or whitespace lines above this method is its annotation
		// stack. Stop at the first non-annotation, non-blank line.
		kind := EntryKindLibraryExport
	scan:
		for back := 1; back <= 5; back++ {
			lineNo := line - back
			if lineNo < 1 || lineNo > len(lines) {
				break
			}
			ltext := strings.TrimSpace(lines[lineNo-1])
			if ltext == "" {
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
			if strings.HasPrefix(ltext, "@") {
				// Some other annotation (e.g. @Override) — keep
				// scanning past it without claiming the method.
				continue
			}
			// Non-annotation source line: the annotation lookback ends.
			break
		}
		out = append(out, EntryPoint{Ident: name, Line: line, Kind: kind})
	}

	return out
}

// javaReservedNames are Java keywords / common control-flow tokens
// that the relaxed `public ... NAME(` regex could otherwise catch.
// Filtering them avoids junk entry-points.
var javaReservedNames = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"return": true, "throw": true, "try": true, "catch": true,
	"do": true, "class": true, "interface": true, "enum": true,
	"record": true, "synchronized": true, "new": true,
}
