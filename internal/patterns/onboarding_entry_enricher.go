package patterns

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// onboardingEntryEnricher detects application entry points.
// Matches Python onboarding_entry_enricher.py.
type onboardingEntryEnricher struct{}

var (
	oePyMainFuncRE    = regexp.MustCompile(`(?m)^\s*def\s+main\s*\(`)
	oePyDunderMainRE  = regexp.MustCompile(`(?m)if\s+__name__\s*==\s*["']__main__["']`)
	oePyFlaskRE       = regexp.MustCompile(`\bFlask\s*\(\s*__name__\s*\)`)
	oePyFastAPIRE     = regexp.MustCompile(`\bFastAPI\s*\(\s*\)`)
	oeJSAppListenRE   = regexp.MustCompile(`\b(?:app|server|http)\s*\.\s*listen\s*\(`)
	oeJSIndexFileRE   = regexp.MustCompile(`(?:^|/)index\.[jt]sx?$`)
	oeJSExportRE      = regexp.MustCompile(`(?m)^\s*export\b`)
	oeGoMainRE        = regexp.MustCompile(`(?m)^func\s+main\s*\(\s*\)`)
	oeGoMainPackageRE = regexp.MustCompile(`(?m)^package\s+main\b`)
	oeJavaMainRE      = regexp.MustCompile(`public\s+static\s+void\s+main\s*\(\s*String`)
	oeSpringBootRE    = regexp.MustCompile(`@SpringBootApplication\b`)
	oeRustMainRE      = regexp.MustCompile(`(?m)^fn\s+main\s*\(\s*\)`)
	// Shell: matches POSIX "name() {" form and "function name {" form.
	oeShellFuncRE = regexp.MustCompile(`(?m)^(?:function\s+)?(\w+)\s*\(\s*\)\s*\{`)
	// Shell trigger: any shell function definition present.
	oeShellTriggerRE = regexp.MustCompile(`(?m)^\w+\s*\(\s*\)\s*\{`)
	// Clojure: matches (defn name [...])
	oeClojureDefnRE = regexp.MustCompile(`(?m)^\s*\(defn\s+([\w?!<>*+-]+)`)
	// Dart: matches class and method definitions
	oeDartClassRE  = regexp.MustCompile(`(?m)^\s*(?:abstract\s+)?class\s+(\w+)`)
	oeDartMethodRE = regexp.MustCompile(`(?m)^\s+(?:[\w<>\[\]?]+\s+)?(\w+)\s*\([^)]*\)\s*(?:async\s*)?\{`)
	// Zig: matches top-level const struct definitions and fn declarations
	oeZigStructRE = regexp.MustCompile(`(?m)^(?:pub\s+)?const\s+(\w+)\s*=\s*struct`)
	oeZigFnRE     = regexp.MustCompile(`(?m)^(?:pub\s+)?fn\s+(\w+)\s*\(`)
	// Proto: matches service, message, and rpc definitions
	oeProtoServiceRE = regexp.MustCompile(`(?m)^\s*service\s+(\w+)`)
	oeProtoMessageRE = regexp.MustCompile(`(?m)^\s*message\s+(\w+)`)
	oeProtoRPCRE     = regexp.MustCompile(`(?m)^\s*rpc\s+(\w+)`)
)

func (o *onboardingEntryEnricher) Category() string { return "onboarding_entry" }

func (o *onboardingEntryEnricher) AppliesTo(src string) bool {
	return oePyMainFuncRE.MatchString(src) ||
		oePyDunderMainRE.MatchString(src) ||
		oePyFlaskRE.MatchString(src) ||
		oeJSAppListenRE.MatchString(src) ||
		oeGoMainRE.MatchString(src) ||
		oeJavaMainRE.MatchString(src) ||
		oeSpringBootRE.MatchString(src) ||
		oeRustMainRE.MatchString(src) ||
		oeShellTriggerRE.MatchString(src) ||
		oeClojureDefnRE.MatchString(src) ||
		oeDartClassRE.MatchString(src) ||
		oeZigFnRE.MatchString(src) ||
		oeProtoServiceRE.MatchString(src)
}

func (o *onboardingEntryEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, entryKind string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Operation", "onboarding_entry", language, line,
			map[string]string{"kind": "onboarding_entry", "entry_kind": entryKind}))
	}

	switch language {
	case "python":
		if m := oePyMainFuncRE.FindStringIndex(src); m != nil {
			emit("py:main_func", "main_function", "main_function", lineOf(src, m[0]))
		}
		if m := oePyDunderMainRE.FindStringIndex(src); m != nil {
			emit("py:dunder_main", "dunder_main", "script_entry", lineOf(src, m[0]))
		}
		if m := oePyFlaskRE.FindStringIndex(src); m != nil {
			emit("py:flask_app", "flask_application", "web_app_entry", lineOf(src, m[0]))
		}
		if m := oePyFastAPIRE.FindStringIndex(src); m != nil {
			emit("py:fastapi_app", "fastapi_application", "web_app_entry", lineOf(src, m[0]))
		}
	case "javascript", "typescript":
		if m := oeJSAppListenRE.FindStringIndex(src); m != nil {
			emit("js:app_listen", "app_listen", "http_server_entry", lineOf(src, m[0]))
		}
		if oeJSIndexFileRE.MatchString(filePath) && oeJSExportRE.MatchString(src) {
			emit("js:index_exports", "index_exports", "module_entry", 1)
		}
	case "go":
		if oeGoMainPackageRE.MatchString(src) {
			if m := oeGoMainRE.FindStringIndex(src); m != nil {
				emit("go:main", "main", "binary_entry", lineOf(src, m[0]))
			}
		}
	case "java", "kotlin":
		if m := oeJavaMainRE.FindStringIndex(src); m != nil {
			emit("java:main", "main_method", "java_main_entry", lineOf(src, m[0]))
		}
		if m := oeSpringBootRE.FindStringIndex(src); m != nil {
			emit("java:spring_boot", "spring_boot_application", "spring_boot_entry", lineOf(src, m[0]))
		}
	case "rust":
		if m := oeRustMainRE.FindStringIndex(src); m != nil {
			emit("rust:main", "main", "binary_entry", lineOf(src, m[0]))
		}
	case "shell":
		// Shell: emit all functions as onboarding entries in reverse-definition order.
		// Python's onboarding_entry_enricher emits each function with reading_order
		// starting at 1 for the last-defined function (lowest line number).
		type shellFunc struct {
			name string
			line int
		}
		var funcs []shellFunc
		for _, m := range oeShellFuncRE.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			line := lineOf(src, m[0])
			funcs = append(funcs, shellFunc{name: name, line: line})
		}
		// Sort descending by line (last defined = order 1).
		sort.Slice(funcs, func(i, j int) bool {
			return funcs[i].line > funcs[j].line
		})
		for i, fn := range funcs {
			order := i + 1
			key := fmt.Sprintf("shell:func:%s", fn.name)
			entName := fmt.Sprintf("onboarding_entry:%s:order=%d", fn.name, order)
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, makeEntity(filePath,
				entName, "SCOPE.Pattern", "onboarding_entry", language, fn.line,
				map[string]string{
					"kind":       "onboarding_entry",
					"entry_kind": "shell_entrypoint",
				}))
		}
	case "clojure":
		results = append(results, emitOnboardingByDependency(filePath, language, src,
			oeClojureDefnRE, "high_dependency", 5)...)
	case "dart":
		results = append(results, emitOnboardingDartCombined(filePath, language, src)...)
	case "zig":
		results = append(results, emitOnboardingAllEntities(filePath, language, src,
			oeZigStructRE, oeZigFnRE, "main_func", "high_dependency")...)
	case "proto", "protobuf":
		// Normalize language to "protobuf" for proto files (matches Python golden).
		protoLang := "protobuf"
		results = append(results, emitOnboardingAllEntities(filePath, protoLang, src,
			oeProtoServiceRE, oeProtoMessageRE, "grpc_service", "grpc_service")...)
		results = append(results, emitOnboardingProtoRPCs(filePath, protoLang, src,
			len(results))...)
	default:
		// Try generic patterns
		if strings.Contains(src, "func main()") {
			emit("generic:main", "main", "binary_entry", 1)
		}
	}

	return results
}

// oeDefItem represents a definition found in source for onboarding ranking.
type oeDefItem struct {
	name string
	line int
}

// emitOnboardingByDependency emits onboarding entries ranked by inbound reference count.
// For each function name extracted by re, counts how many times it appears as a callee
// elsewhere in the source, sorts by count descending, then source order, caps at maxEntries.
func emitOnboardingByDependency(filePath, language, src string, re *regexp.Regexp, entryKind string, maxEntries int) []types.EntityRecord {
	var defs []oeDefItem
	for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		defs = append(defs, oeDefItem{name: name, line: line})
	}
	if len(defs) == 0 {
		return nil
	}

	// Count inbound references using word-boundary matching
	// (avoids substring false positives like "User" matching "UserService").
	type ranked struct {
		def     oeDefItem
		inbound int
	}
	var items []ranked
	for _, d := range defs {
		count := countWordBoundaryRefs(d.name, src)
		items = append(items, ranked{def: d, inbound: count})
	}

	// Sort by inbound descending, then source line ascending.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].inbound != items[j].inbound {
			return items[i].inbound > items[j].inbound
		}
		return items[i].def.line < items[j].def.line
	})

	if len(items) > maxEntries {
		items = items[:maxEntries]
	}

	var results []types.EntityRecord
	for i, item := range items {
		order := i + 1
		entName := fmt.Sprintf("onboarding_entry:%s:order=%d", item.def.name, order)
		results = append(results, makeEntity(filePath,
			entName, "SCOPE.Pattern", "onboarding_entry", language, item.def.line,
			map[string]string{
				"kind":       "onboarding_entry",
				"entry_kind": entryKind,
			}))
	}
	return results
}

// countWordBoundaryRefs counts how many times name appears as a whole word in src,
// excluding the definition itself. Uses word-boundary matching to avoid substring false positives.
func countWordBoundaryRefs(name, src string) int {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	count := len(re.FindAllStringIndex(src, -1))
	// Subtract 1 for the definition.
	if count > 0 {
		count--
	}
	return count
}

// emitOnboardingDartCombined emits onboarding entries for Dart classes and methods,
// ranked by inbound reference count, capped at 5 (matching Python's _MAX_HIGH_DEPENDENCY_FALLBACK).
// Excludes abstract classes since Python doesn't extract them.
func emitOnboardingDartCombined(filePath, language, src string) []types.EntityRecord {
	skipKeywords := map[string]bool{
		"if": true, "else": true, "for": true, "while": true,
		"do": true, "switch": true, "try": true, "catch": true,
		"return": true, "class": true, "abstract": true, "const": true,
	}

	var defs []oeDefItem

	// Collect non-abstract classes (tracked separately for ranking).
	abstractRE := regexp.MustCompile(`(?m)^\s*abstract\s+class\s+`)
	classNames := map[string]bool{}
	for _, m := range oeDartClassRE.FindAllStringSubmatchIndex(src, -1) {
		// Check if this is an abstract class.
		lineStart := strings.LastIndex(src[:m[0]], "\n") + 1
		lineText := src[lineStart:m[1]]
		if abstractRE.MatchString(lineText) {
			continue
		}
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		defs = append(defs, oeDefItem{name: name, line: line})
		classNames[name] = true
	}

	// Collect methods (not from abstract classes).
	for _, m := range oeDartMethodRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		if skipKeywords[name] {
			continue
		}
		line := lineOf(src, m[0])
		defs = append(defs, oeDefItem{name: name, line: line})
	}

	if len(defs) == 0 {
		return nil
	}

	// Ranking strategy: classes ranked by word-boundary reference count (type usage),
	// methods ranked by source line order. Classes with references sort first,
	// then methods in source order, then unreferenced classes.
	// This approximates Python's call-graph-based ranking.
	type ranked struct {
		def     oeDefItem
		inbound int
		isClass bool
	}
	var items []ranked
	for _, d := range defs {
		refs := 0
		if classNames[d.name] {
			refs = countWordBoundaryRefs(d.name, src)
		}
		items = append(items, ranked{def: d, inbound: refs, isClass: classNames[d.name]})
	}

	sort.SliceStable(items, func(i, j int) bool {
		// Classes with refs come first (data types).
		iHasRefs := items[i].isClass && items[i].inbound > 0
		jHasRefs := items[j].isClass && items[j].inbound > 0
		if iHasRefs != jHasRefs {
			return iHasRefs
		}
		// Among classes with refs, sort by ref count descending.
		if iHasRefs && jHasRefs {
			if items[i].inbound != items[j].inbound {
				return items[i].inbound > items[j].inbound
			}
		}
		// Methods come before unreferenced classes.
		if !items[i].isClass && items[j].isClass && !jHasRefs {
			return true
		}
		if items[i].isClass && !iHasRefs && !items[j].isClass {
			return false
		}
		// Within same category, sort by source line.
		return items[i].def.line < items[j].def.line
	})

	// Cap at 5 (Python's _MAX_HIGH_DEPENDENCY_FALLBACK).
	if len(items) > 5 {
		items = items[:5]
	}

	var results []types.EntityRecord
	for i, item := range items {
		order := i + 1
		entName := fmt.Sprintf("onboarding_entry:%s:order=%d", item.def.name, order)
		results = append(results, makeEntity(filePath,
			entName, "SCOPE.Pattern", "onboarding_entry", language, item.def.line,
			map[string]string{
				"kind":       "onboarding_entry",
				"entry_kind": "high_dependency",
			}))
	}
	return results
}

// emitOnboardingAllEntities emits onboarding entries for all struct/class + fn definitions.
// Used for Zig and Proto where the Python enricher emits entries for every entity.
// For Zig: main function gets "main_func" entry_kind; others get the fallback kind.
// For Proto: service gets listed first, then messages.
func emitOnboardingAllEntities(filePath, language, src string, primaryRE, secondaryRE *regexp.Regexp, mainKind, fallbackKind string) []types.EntityRecord {
	var defs []oeDefItem
	isMain := map[string]bool{}

	// Collect primary entities (structs/services).
	for _, m := range primaryRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		defs = append(defs, oeDefItem{name: name, line: line})
	}

	// Collect secondary entities (functions/messages).
	for _, m := range secondaryRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		defs = append(defs, oeDefItem{name: name, line: line})
	}

	// For zig: detect main function.
	if language == "zig" {
		for i := range defs {
			if defs[i].name == "main" {
				isMain["main"] = true
			}
		}
	}

	// Sort by line ascending (source order).
	sort.SliceStable(defs, func(i, j int) bool {
		return defs[i].line < defs[j].line
	})

	// For zig, main goes first.
	if isMain["main"] {
		var mainDefs []oeDefItem
		var otherDefs []oeDefItem
		for _, d := range defs {
			if d.name == "main" {
				mainDefs = append(mainDefs, d)
			} else {
				otherDefs = append(otherDefs, d)
			}
		}
		defs = append(mainDefs, otherDefs...)
	}

	// Deduplicate.
	seen := map[string]bool{}
	var results []types.EntityRecord
	order := 1
	for _, d := range defs {
		if seen[d.name] {
			continue
		}
		seen[d.name] = true

		kind := fallbackKind
		if isMain[d.name] {
			kind = mainKind
		}

		entName := fmt.Sprintf("onboarding_entry:%s:order=%d", d.name, order)
		results = append(results, makeEntity(filePath,
			entName, "SCOPE.Pattern", "onboarding_entry", language, d.line,
			map[string]string{
				"kind":       "onboarding_entry",
				"entry_kind": kind,
			}))
		order++
	}
	return results
}

// emitOnboardingProtoRPCs emits onboarding entries for rpc definitions, continuing from offset.
func emitOnboardingProtoRPCs(filePath, language, src string, offset int) []types.EntityRecord {
	var results []types.EntityRecord
	order := offset + 1
	for _, m := range oeProtoRPCRE.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		line := lineOf(src, m[0])
		entName := fmt.Sprintf("onboarding_entry:%s:order=%d", name, order)
		results = append(results, makeEntity(filePath,
			entName, "SCOPE.Pattern", "onboarding_entry", language, line,
			map[string]string{
				"kind":       "onboarding_entry",
				"entry_kind": "grpc_service",
			}))
		order++
	}
	return results
}

func init() {
	Register(&onboardingEntryEnricher{})
}
