// Plugin / extension-system registration topology (issue #3628 area #25).
//
// Build tools, linters, transpilers, test runners, and JVM build systems are
// composed from plugins/extensions. Where feature flags (#3628 area #17,
// GATED_BY) answer "what code is gated by flag X", plugin registration answers
// "which plugins does this build/app register" — the composition of a
// project's build/runtime behavior.
//
// This pass keys off the FILE (config / build manifest) rather than call
// sites: a plugin is declared in a config, not invoked at a code offset. It
// scans recognised config files and emits:
//
//   - one synthetic SCOPE.Plugin entity per distinct (ecosystem, plugin name)
//     pair (synthetic ID `plugin:<ecosystem>:<name>`, Subtype = the system),
//     and
//   - one REGISTERS_PLUGIN edge from the declaring config file
//     (`File:<path>`) to the plugin entity.
//
// The plugin entity ID embeds the ecosystem so two configs in the same repo
// that register the same plugin name converge on one node (same cross-file
// identity strategy used by MessageTopic / FeatureFlag), while a webpack
// "html" and a babel "html" stay distinct.
//
// Supported systems (the dominant, clear idioms):
//
//   - Webpack / Vite / Rollup (JS bundler configs): a `plugins: [ ... ]`
//     array whose elements are `new HtmlWebpackPlugin()` (constructor) or
//     `terser()` / `vue()` (factory call). The constructed/called identifier
//     is the plugin name.
//   - Babel / ESLint (.babelrc / .eslintrc / package.json blocks): a
//     `plugins: ["@babel/plugin-x", "react"]` / `extends: [...]` string array.
//   - pytest: `pytest_plugins = ["a", "b"]` in conftest.py / a test module.
//   - setuptools: `entry_points={"<group>": ["name = module:obj"]}` —
//     each entry is a registered plugin under its group.
//   - Maven: `<plugin>...<artifactId>X</artifactId>...</plugin>` in pom.xml.
//   - Gradle: `plugins { id 'java'; id "org.x" version "1" }` in build.gradle.
//
// Honest-partial: only LITERAL plugin names are emitted. A dynamically
// computed plugin (spread `...basePlugins`, a bare variable element, a
// non-literal artifactId) yields NO entity and NO edge — the same disposition
// the feature-flag and config-read passes use for dynamic values.
//
// Append-only — never modifies existing entities or edges, so this pass cannot
// regress the surrounding pipeline on files that register no plugins.
package engine

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// pluginEntityKind / pluginEdgeKind are aliased through the typed enum so
// kinds.go stays canonical (producer_kinds_test.go guardrail).
var (
	pluginEntityKind = string(types.EntityKindPlugin)
	pluginEdgeKind   = string(types.RelationshipKindRegistersPlugin)
)

// pluginPatternType tags every emitted entity/edge so downstream consumers can
// filter the pass output, matching the pattern_type convention used by the
// feature-flag and config passes.
const pluginPatternType = "plugin_registration"

// pluginHit is one detected plugin registration.
type pluginHit struct {
	name   string // literal plugin name
	system string // webpack | vite | rollup | babel | eslint | pytest | setuptools | maven | gradle
	group  string // setuptools entry-point group ("" otherwise)
	line   int    // 1-indexed source line
}

// applyPluginSystemEdges scans a config/build file for plugin registrations
// and appends a SCOPE.Plugin entity (deduped per ecosystem:name) plus a
// REGISTERS_PLUGIN edge from the declaring file. No-op for files that are not
// a recognised config or that register no plugins. Runs append-only.
func applyPluginSystemEdges(args DetectorPassArgs) DetectorPassResult {
	filePath := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships

	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)
	hits := scanPlugins(filePath, src)
	if len(hits) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	fromID := "File:" + filePath

	// Dedup the plugin entities by ecosystem:name. The first hit wins the
	// metadata (system / group).
	emittedPlugin := map[string]bool{}
	// Dedup edges by plugin ID so a plugin declared twice yields one edge.
	emittedEdge := map[string]bool{}

	for _, h := range hits {
		pluginID := buildPluginID(h.system, h.name)

		if !emittedPlugin[pluginID] {
			emittedPlugin[pluginID] = true
			props := map[string]string{
				"plugin":       h.name,
				"system":       h.system,
				"pattern_type": pluginPatternType,
			}
			if h.group != "" {
				props["group"] = h.group
			}
			entities = append(entities, types.EntityRecord{
				ID:                 pluginID,
				Name:               h.name,
				Kind:               pluginEntityKind,
				Subtype:            h.system,
				SourceFile:         filePath,
				StartLine:          h.line,
				EndLine:            h.line,
				Language:           args.Lang,
				Properties:         props,
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}

		if emittedEdge[pluginID] {
			continue
		}
		emittedEdge[pluginID] = true

		edgeProps := map[string]string{
			"plugin":       h.name,
			"system":       h.system,
			"line":         strconv.Itoa(h.line),
			"pattern_type": pluginPatternType,
		}
		if h.group != "" {
			edgeProps["group"] = h.group
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fromID,
			ToID:       pluginID,
			Kind:       pluginEdgeKind,
			Properties: edgeProps,
		})
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// scanPlugins dispatches on the file's basename to the right system scanner.
// Returns nil for files that are not a recognised plugin-declaring config.
func scanPlugins(filePath, src string) []pluginHit {
	base := strings.ToLower(path.Base(filePath))

	switch {
	case base == "pom.xml":
		return scanMavenPlugins(src)
	case base == "build.gradle" || base == "build.gradle.kts":
		return scanGradlePlugins(src)
	case base == ".babelrc" || base == ".babelrc.json" || base == "babel.config.js" || base == "babel.config.json":
		return scanStringArrayPlugins(src, "babel", "plugins")
	case isESLintConfig(base):
		// ESLint registers via both `plugins` and `extends`.
		hits := scanStringArrayPlugins(src, "eslint", "plugins")
		hits = append(hits, scanStringArrayPlugins(src, "eslint", "extends")...)
		return hits
	case isBundlerConfig(base):
		return scanBundlerPlugins(filePath, src)
	case isPythonFile(base):
		hits := scanPytestPlugins(src)
		hits = append(hits, scanSetuptoolsEntryPoints(src)...)
		return hits
	case base == "setup.cfg":
		return scanSetuptoolsEntryPoints(src)
	}
	return nil
}

func isESLintConfig(base string) bool {
	return strings.HasPrefix(base, ".eslintrc") ||
		base == "eslint.config.js" || base == "eslint.config.mjs" ||
		base == "eslint.config.cjs"
}

// isBundlerConfig matches the webpack / vite / rollup bundler config files
// whose dominant idiom is a `plugins: [ ... ]` array of constructors/factories.
var bundlerConfigRe = regexp.MustCompile(
	`^(webpack|vite|rollup|vitest)\.config\.(js|ts|mjs|cjs)$`)

func isBundlerConfig(base string) bool {
	return bundlerConfigRe.MatchString(base)
}

func isPythonFile(base string) bool {
	return strings.HasSuffix(base, ".py")
}

// bundlerSystemFor returns the bundler system tag from the config basename.
func bundlerSystemFor(base string) string {
	if i := strings.IndexByte(base, '.'); i > 0 {
		switch base[:i] {
		case "webpack":
			return "webpack"
		case "vite", "vitest":
			return "vite"
		case "rollup":
			return "rollup"
		}
	}
	return "webpack"
}

// --- JS bundler (webpack / vite / rollup) ----------------------------------

// pluginsArrayRe captures the bracketed body of a `plugins: [ ... ]` literal.
// Non-greedy to the first closing bracket; nested arrays inside plugin args
// are uncommon in the dominant idiom and are honest-partial out of scope.
var pluginsArrayRe = regexp.MustCompile(`(?s)plugins\s*:\s*\[(.*?)\]`)

// bundlerElementRe captures a plugin element as either `new Name(` (constructor)
// or `name(` (factory call). Group 1 = constructor name, group 2 = factory name.
var bundlerElementRe = regexp.MustCompile(
	`\bnew\s+([A-Za-z_$][\w$]*)\s*\(|(?:^|[\[,{]\s*)([A-Za-z_$][\w$]*)\s*\(`)

// scanBundlerPlugins extracts plugin entities from a `plugins: [ ... ]` array
// in a webpack/vite/rollup config. Only constructor (`new X()`) and
// factory-call (`x()`) elements with a literal identifier are emitted; spreads
// and bare variable references are skipped (honest-partial).
func scanBundlerPlugins(filePath, src string) []pluginHit {
	system := bundlerSystemFor(strings.ToLower(path.Base(filePath)))

	var hits []pluginHit
	for _, arr := range pluginsArrayRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := arr[2], arr[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, m := range bundlerElementRe.FindAllStringSubmatch(body, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if name == "" || isJSKeyword(name) {
				continue
			}
			hits = append(hits, pluginHit{
				name:   name,
				system: system,
				line:   lineAtOffset(src, bodyStart),
			})
		}
	}
	return hits
}

// isJSKeyword filters out JS keywords that bundlerElementRe's factory branch
// could otherwise capture (e.g. `require(...)`, `if(...)`), so they don't
// become fabricated plugin entities.
func isJSKeyword(s string) bool {
	switch s {
	case "require", "if", "for", "while", "switch", "return", "function",
		"import", "export", "await", "typeof", "new", "module", "process":
		return true
	}
	return false
}

// --- Babel / ESLint (string-array plugins) ---------------------------------

// stringArrayKeyRe is built per key to capture the `<key>: [ ... ]` body in a
// JSON/JS config object.
func stringArrayKeyRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)["']?` + regexp.QuoteMeta(key) + `["']?\s*:\s*\[(.*?)\]`)
}

// pluginStringLiteralRe captures a single/double-quoted string literal.
var pluginStringLiteralRe = regexp.MustCompile(`"([^"\\]+)"|'([^'\\]+)'`)

// scanStringArrayPlugins extracts plugin names from a `<key>: ["a", "b"]`
// string array (Babel `plugins`, ESLint `plugins` / `extends`). Each string
// literal is a plugin name; non-literal elements are skipped.
func scanStringArrayPlugins(src, system, key string) []pluginHit {
	re := stringArrayKeyRe(key)
	var hits []pluginHit
	for _, arr := range re.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := arr[2], arr[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, m := range pluginStringLiteralRe.FindAllStringSubmatch(body, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if name == "" {
				continue
			}
			hits = append(hits, pluginHit{
				name:   name,
				system: system,
				line:   lineAtOffset(src, bodyStart),
			})
		}
	}
	return hits
}

// --- pytest -----------------------------------------------------------------

// pytestPluginsRe captures the `pytest_plugins = [ ... ]` (or tuple) body.
var pytestPluginsRe = regexp.MustCompile(`(?s)pytest_plugins\s*=\s*[\[\(](.*?)[\]\)]`)

// scanPytestPlugins extracts plugin module names from a `pytest_plugins`
// list/tuple of string literals (conftest.py or a test module).
func scanPytestPlugins(src string) []pluginHit {
	var hits []pluginHit
	for _, arr := range pytestPluginsRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := arr[2], arr[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, m := range pluginStringLiteralRe.FindAllStringSubmatch(body, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if name == "" {
				continue
			}
			hits = append(hits, pluginHit{
				name:   name,
				system: "pytest",
				line:   lineAtOffset(src, bodyStart),
			})
		}
	}
	return hits
}

// --- setuptools entry points ------------------------------------------------

// entryPointsBlockRe captures the `entry_points={ ... }` dict body (setup.py)
// or the `[options.entry_points]` ... block is handled by setup.cfg scanning
// below via the same per-group regex; here we cover the dict form.
var entryPointsDictRe = regexp.MustCompile(`(?s)entry_points\s*=\s*\{(.*?)\}`)

// entryPointsGroupRe captures one `"group": [ "name = mod:obj", ... ]` pair
// inside the entry_points dict. Group 1 = group, group 2 = the list body.
var entryPointsGroupRe = regexp.MustCompile(`(?s)["']([^"'\\]+)["']\s*:\s*\[(.*?)\]`)

// entryPointSpecRe captures the `name` of a `name = module:object` entry-point
// string literal (the left-hand side before `=`).
var entryPointSpecRe = regexp.MustCompile(`^\s*([\w\.\-]+)\s*=`)

// scanSetuptoolsEntryPoints extracts entry-point plugins from a setuptools
// `entry_points={"<group>": ["name = mod:obj"]}` dict (setup.py). Each entry's
// left-hand name is a registered plugin under its group.
func scanSetuptoolsEntryPoints(src string) []pluginHit {
	var hits []pluginHit
	for _, blk := range entryPointsDictRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := blk[2], blk[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, g := range entryPointsGroupRe.FindAllStringSubmatchIndex(body, -1) {
			groupStart, groupEnd := g[2], g[3]
			listStart, listEnd := g[4], g[5]
			if groupStart < 0 || listStart < 0 {
				continue
			}
			group := body[groupStart:groupEnd]
			list := body[listStart:listEnd]
			lineBase := lineAtOffset(src, bodyStart+listStart)
			for _, m := range pluginStringLiteralRe.FindAllStringSubmatch(list, -1) {
				entry := m[1]
				if entry == "" {
					entry = m[2]
				}
				name := entryPointName(entry)
				if name == "" {
					continue
				}
				hits = append(hits, pluginHit{
					name:   name,
					system: "setuptools",
					group:  group,
					line:   lineBase,
				})
			}
		}
	}
	return hits
}

// entryPointName returns the registered name (left of `=`) from a
// `name = module:object` entry-point string. Returns "" if the form is not
// recognised.
func entryPointName(entry string) string {
	m := entryPointSpecRe.FindStringSubmatch(entry)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// --- Maven ------------------------------------------------------------------

// mavenPluginBlockRe captures each `<plugin> ... </plugin>` body in pom.xml.
var mavenPluginBlockRe = regexp.MustCompile(`(?s)<plugin>(.*?)</plugin>`)

// mavenArtifactRe captures the `<artifactId>X</artifactId>` value.
var mavenArtifactRe = regexp.MustCompile(`<artifactId>\s*([^<\s][^<]*?)\s*</artifactId>`)

// scanMavenPlugins extracts the artifactId of each `<plugin>` element in
// pom.xml. Plugins under `<dependencies>` are NOT matched (those are
// `<dependency>` elements), so only true build plugins are emitted.
func scanMavenPlugins(src string) []pluginHit {
	var hits []pluginHit
	for _, blk := range mavenPluginBlockRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := blk[2], blk[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		am := mavenArtifactRe.FindStringSubmatch(body)
		if am == nil {
			continue
		}
		name := strings.TrimSpace(am[1])
		if name == "" {
			continue
		}
		hits = append(hits, pluginHit{
			name:   name,
			system: "maven",
			line:   lineAtOffset(src, bodyStart),
		})
	}
	return hits
}

// --- Gradle -----------------------------------------------------------------

// gradlePluginsBlockRe captures the body of a `plugins { ... }` block in
// build.gradle / build.gradle.kts.
var gradlePluginsBlockRe = regexp.MustCompile(`(?s)plugins\s*\{(.*?)\}`)

// gradleIdRe captures the plugin id from `id 'x'`, `id "x"`, or
// `id("x")` (Kotlin DSL).
var gradleIdRe = regexp.MustCompile(`\bid\s*[\(\s]\s*["']([^"'\\]+)["']`)

// scanGradlePlugins extracts plugin ids declared in a `plugins { ... }` block.
// Covers both the Groovy (`id 'java'`) and Kotlin (`id("java")`) DSLs.
func scanGradlePlugins(src string) []pluginHit {
	var hits []pluginHit
	for _, blk := range gradlePluginsBlockRe.FindAllStringSubmatchIndex(src, -1) {
		bodyStart, bodyEnd := blk[2], blk[3]
		if bodyStart < 0 {
			continue
		}
		body := src[bodyStart:bodyEnd]
		for _, m := range gradleIdRe.FindAllStringSubmatch(body, -1) {
			name := strings.TrimSpace(m[1])
			if name == "" {
				continue
			}
			hits = append(hits, pluginHit{
				name:   name,
				system: "gradle",
				line:   lineAtOffset(src, bodyStart),
			})
		}
	}
	return hits
}

// buildPluginID returns the synthetic entity ID for a plugin:
// `plugin:<ecosystem>:<name>`. Same ecosystem+name converge on one node.
func buildPluginID(system, name string) string {
	return "plugin:" + system + ":" + name
}
