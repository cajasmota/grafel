// Package config implements the supplemental "config-discovery" pass that
// promotes project-level configuration files to first-class graph entities.
//
// Issue #1885 (Wave 1 tail of #1890): Module-aggregate pages cannot ground
// reference-* sections without filesystem-level entities for the files that
// actually carry deployment, build, and runtime configuration:
//
//	Python:   pyproject.toml, setup.cfg, requirements*.txt, Pipfile,
//	          .flake8, mypy.ini, pytest.ini, .env, .env.*
//	Java:     pom.xml, build.gradle, build.gradle.kts,
//	          application.properties, application.yml/.yaml,
//	          quarkus.properties
//	JS/TS:    package.json, tsconfig.json, vite.config.*, next.config.*,
//	          .eslintrc.*, .prettierrc.*, .env, .env.*
//	Go:       go.mod, Makefile
//	General:  Dockerfile, docker-compose.yml/.yaml
//
// The cross-language extractor framework (Pass 3) only runs on files that
// survive classification. Many of these basenames have no language token and
// therefore never reach Pass 3. This pass walks the *original* file list
// (pre-classification) and emits one SCOPE.Config entity per recognised
// config file plus DEPENDS_ON_CONFIG / CONFIGURES edges to nearby modules.
//
// SECURITY: For .env files we record env-variable NAMES ONLY. Values are
// dropped before they enter the graph. Test
// TestDiscover_EnvNeverLeaksValues asserts the boundary.
package config

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/types"
)

// maxKeysPerProperty bounds the size of any single Properties value so a
// pathological config file cannot inflate the graph.
const maxKeysPerProperty = 64

// maxFileBytes caps the bytes we read from any one config file. Larger
// files are still recognised as config (entity emitted) but only the first
// maxFileBytes are parsed for keys/dependencies.
const maxFileBytes = 256 * 1024 // 256 KiB

// configKind is the parsed file's format-family. It is used to select the
// parser and is recorded as the entity Property "format".
type configKind string

const (
	formatTOML         configKind = "toml"
	formatJSON         configKind = "json"
	formatYAML         configKind = "yaml"
	formatXML          configKind = "xml"
	formatProperties   configKind = "properties"
	formatINI          configKind = "ini"
	formatEnv          configKind = "env"
	formatDockerfile   configKind = "dockerfile"
	formatMakefile     configKind = "makefile"
	formatRequirements configKind = "requirements"
	formatGradle       configKind = "gradle"
	formatGoMod        configKind = "go_mod"
	formatJSConfig     configKind = "javascript"
	formatBundlerJSON  configKind = "bundler_json"
	formatBundlerJS    configKind = "bundler_js"

	// JS/TS test-runner config families (issue #2864). JSON/JSONC configs are
	// mined for test targets (spec globs / files) and config->spec dependency
	// edges; JS configs are mined with permissive regexes; YAML (.taprc) and the
	// package.json "ava" block are handled by dedicated parsers.
	formatTestRunnerJSON configKind = "test_runner_json"
	formatTestRunnerJS   configKind = "test_runner_js"
	formatTestRunnerYAML configKind = "test_runner_yaml"

	// Mobile (React Native CLI + Expo) config families (issue #2879). JSON
	// configs (eas.json, app.json) are JSONC-tolerant and mined for build
	// profiles / the Expo manifest; JS configs (metro.config.js,
	// react-native.config.js, app.config.{js,ts}) are mined with permissive
	// regexes — no JS evaluation, just stable structural signal.
	formatMobileJSON configKind = "mobile_json"
	formatMobileJS   configKind = "mobile_js"
)

// configSpec describes one recognised config file. Subtype is the value
// recorded on the emitted entity (Subtype field) — chosen to mirror the
// vocabulary in issue #1885.
type configSpec struct {
	subtype string
	format  configKind
}

// exactBasenames maps an exact basename (case-sensitive on Linux/macOS
// behaviour, but we lowercase Windows-style paths before lookup) to its
// spec. Patterns that need glob behaviour are handled by matchPattern.
var exactBasenames = map[string]configSpec{
	"pyproject.toml":   {"python_project", formatTOML},
	"setup.cfg":        {"python_project_legacy", formatINI},
	"setup.py":         {"python_project_legacy", formatJSConfig}, // parsed as text
	"requirements.txt": {"python_requirements", formatRequirements},
	"Pipfile":          {"python_pipenv", formatTOML},
	".flake8":          {"python_flake8", formatINI},
	"mypy.ini":         {"python_mypy", formatINI},
	"pytest.ini":       {"python_pytest", formatINI},
	"tox.ini":          {"python_tox", formatINI},

	"pom.xml":                {"maven_project", formatXML},
	"build.gradle":           {"gradle_project", formatGradle},
	"build.gradle.kts":       {"gradle_project", formatGradle},
	"application.properties": {"spring_properties", formatProperties},
	"application.yml":        {"spring_yaml", formatYAML},
	"application.yaml":       {"spring_yaml", formatYAML},
	"quarkus.properties":     {"quarkus_properties", formatProperties},

	"package.json":  {"node_project", formatJSON},
	"tsconfig.json": {"typescript_project", formatJSON},

	// JS/TS build & monorepo tools (issue #2863). JSON/JSONC configs are
	// deeply mined for build targets (target_extraction) and inter-target /
	// inter-package dependency edges (dependency_graph).
	"turbo.json":   {"turborepo_config", formatBundlerJSON},
	"nx.json":      {"nx_config", formatBundlerJSON},
	"project.json": {"nx_project", formatBundlerJSON},
	"lerna.json":   {"lerna_config", formatBundlerJSON},
	".parcelrc":    {"parcel_config", formatBundlerJSON},
	"bunfig.toml":  {"bun_config", formatTOML},

	// JS/TS test runners (issue #2864). Mined for test targets
	// (target_extraction) and config->spec dependency edges (dependency_graph).
	// .mocharc / jasmine.json / .taprc carry spec globs directly; AVA's config
	// lives in package.json (handled in parseJSON when an "ava" block exists).
	".mocharc.json":  {"mocha_config", formatTestRunnerJSON},
	".mocharc.jsonc": {"mocha_config", formatTestRunnerJSON},
	".mocharc.yml":   {"mocha_config", formatTestRunnerYAML},
	".mocharc.yaml":  {"mocha_config", formatTestRunnerYAML},
	"jasmine.json":   {"jasmine_config", formatTestRunnerJSON},
	".taprc":         {"tap_config", formatTestRunnerYAML},

	// Mobile — React Native CLI + Expo (issue #2879). eas.json carries the
	// EAS Build/Submit profiles; app.json is the Expo manifest (the "expo"
	// block). metro.config / react-native.config / app.config.{js,ts} are JS
	// and matched via regex below.
	"eas.json": {"eas_config", formatMobileJSON},
	"app.json": {"expo_config", formatMobileJSON},

	"go.mod": {"go_module", formatGoMod},
	"go.sum": {"go_sum", formatGoMod}, // existence only

	"Makefile":    {"makefile", formatMakefile},
	"makefile":    {"makefile", formatMakefile},
	"GNUmakefile": {"makefile", formatMakefile},

	"Dockerfile":          {"docker_image", formatDockerfile},
	"Containerfile":       {"docker_image", formatDockerfile},
	"docker-compose.yml":  {"docker_compose", formatYAML},
	"docker-compose.yaml": {"docker_compose", formatYAML},
}

// requirementsPrefix matches requirements-dev.txt, requirements-test.txt, …
var requirementsPrefix = regexp.MustCompile(`^requirements[-._].*\.txt$`)

// dockerfileVariant matches Dockerfile.dev, Dockerfile.prod, dockerfile-test, …
var dockerfileVariant = regexp.MustCompile(`(?i)^dockerfile([._\-].+)?$`)

// envFile matches .env and .env.<suffix>. Excludes .envrc (direnv).
var envFile = regexp.MustCompile(`^\.env(\..+)?$`)

// jsConfigVariant matches vite.config.{js,ts,mjs}, next.config.{js,ts,mjs},
// .eslintrc.{js,cjs,json,yml,yaml}, .prettierrc.{js,cjs,json,yml,yaml,toml}.
var (
	viteConfigRe     = regexp.MustCompile(`^vite\.config\.(js|ts|mjs|cjs)$`)
	nextConfigRe     = regexp.MustCompile(`^next\.config\.(js|ts|mjs|cjs)$`)
	eslintConfigRe   = regexp.MustCompile(`^\.eslintrc(\.(js|cjs|json|yml|yaml))?$`)
	prettierConfigRe = regexp.MustCompile(`^\.prettierrc(\.(js|cjs|json|yml|yaml|toml))?$`)

	// JS/TS bundler config variants (issue #2863). Matched as JS source so the
	// bundler parser can mine entry points, output dirs, and workspace hints.
	webpackConfigRe = regexp.MustCompile(`^webpack\.config\.(js|ts|mjs|cjs)$`)
	rollupConfigRe  = regexp.MustCompile(`^rollup\.config\.(js|ts|mjs|cjs)$`)
	esbuildConfigRe = regexp.MustCompile(`^esbuild\.config\.(js|ts|mjs|cjs)$`)

	// JS/TS test-runner config variants (issue #2864). Matched as JS source so
	// the test-runner parser can mine spec globs, test dirs, and project matrices.
	vitestConfigRe     = regexp.MustCompile(`^vitest\.config\.(js|ts|mjs|cjs|mts|cts)$`)
	cypressConfigRe    = regexp.MustCompile(`^cypress\.config\.(js|ts|mjs|cjs)$`)
	playwrightConfigRe = regexp.MustCompile(`^playwright\.config\.(js|ts|mjs|cjs)$`)
	mocharcJSRe        = regexp.MustCompile(`^\.mocharc\.(js|cjs)$`)
	avaConfigRe        = regexp.MustCompile(`^ava\.config\.(js|cjs|mjs)$`)

	// Mobile JS config variants (issue #2879). Matched as JS source so the
	// mobile parser can mine the metro resolver/transformer, react-native.config
	// native-module links, and the Expo app.config manifest.
	metroConfigRe   = regexp.MustCompile(`^metro\.config\.(js|ts|mjs|cjs)$`)
	rnConfigRe      = regexp.MustCompile(`^react-native\.config\.(js|ts|mjs|cjs)$`)
	expoAppConfigRe = regexp.MustCompile(`^app\.config\.(js|ts|mjs|cjs)$`)
)

// classify returns the configSpec for filePath, or false when the file is
// not a known project-level config file.
func classify(relPath string) (configSpec, bool) {
	base := filepath.Base(filepath.FromSlash(relPath))
	if spec, ok := exactBasenames[base]; ok {
		return spec, true
	}
	switch {
	case requirementsPrefix.MatchString(base):
		return configSpec{"python_requirements", formatRequirements}, true
	case envFile.MatchString(base):
		return configSpec{"env_vars", formatEnv}, true
	case dockerfileVariant.MatchString(base) && strings.Contains(strings.ToLower(base), "dockerfile"):
		return configSpec{"docker_image", formatDockerfile}, true
	case viteConfigRe.MatchString(base):
		return configSpec{"vite_config", formatBundlerJS}, true
	case webpackConfigRe.MatchString(base):
		return configSpec{"webpack_config", formatBundlerJS}, true
	case rollupConfigRe.MatchString(base):
		return configSpec{"rollup_config", formatBundlerJS}, true
	case esbuildConfigRe.MatchString(base):
		return configSpec{"esbuild_config", formatBundlerJS}, true
	case vitestConfigRe.MatchString(base):
		return configSpec{"vitest_config", formatTestRunnerJS}, true
	case cypressConfigRe.MatchString(base):
		return configSpec{"cypress_config", formatTestRunnerJS}, true
	case playwrightConfigRe.MatchString(base):
		return configSpec{"playwright_config", formatTestRunnerJS}, true
	case mocharcJSRe.MatchString(base):
		return configSpec{"mocha_config", formatTestRunnerJS}, true
	case avaConfigRe.MatchString(base):
		return configSpec{"ava_config", formatTestRunnerJS}, true
	case nextConfigRe.MatchString(base):
		return configSpec{"next_config", formatJSConfig}, true
	case eslintConfigRe.MatchString(base):
		return configSpec{"eslint_config", formatJSConfig}, true
	case prettierConfigRe.MatchString(base):
		return configSpec{"prettier_config", formatJSConfig}, true
	case metroConfigRe.MatchString(base):
		return configSpec{"metro_config", formatMobileJS}, true
	case rnConfigRe.MatchString(base):
		return configSpec{"react_native_config", formatMobileJS}, true
	case expoAppConfigRe.MatchString(base):
		return configSpec{"expo_config", formatMobileJS}, true
	}
	return configSpec{}, false
}

// Discover walks files (repo-relative paths, sourced from the same walker
// that feeds the per-language extractors) and emits SCOPE.Config entities
// plus DEPENDS_ON_CONFIG edges from the file's directory to the config.
//
// Returned slices are sorted deterministically (issue #481) before return.
func Discover(ctx context.Context, repoRoot string, files []string) ([]types.EntityRecord, []types.RelationshipRecord, error) {
	tracer := otel.Tracer("extractor.config_discover")
	ctx, span := tracer.Start(ctx, "indexer.config_discover.run")
	defer span.End()
	_ = ctx

	var entities []types.EntityRecord
	var rels []types.RelationshipRecord
	seenConfigID := map[string]bool{}

	for _, rel := range files {
		spec, ok := classify(rel)
		if !ok {
			continue
		}

		abs := filepath.Join(repoRoot, rel)
		content, err := readBounded(abs)
		if err != nil {
			// Best-effort: emit the entity even when read fails, but with
			// empty body. Skipping silently would lose graph signal for
			// files we know exist on disk.
			content = nil
		}

		ent := buildConfigEntity(repoRoot, rel, spec, content)
		if seenConfigID[ent.ID] {
			continue
		}
		seenConfigID[ent.ID] = true
		entities = append(entities, ent)

		// Emit a DEPENDS_ON_CONFIG edge from the file's containing
		// directory (treated as a Module structural reference) to the
		// config entity. The intra-repo resolver will rebind to the
		// real Module entity when one exists.
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." || dir == "" {
			dir = "_repo_root"
		}
		rels = append(rels, types.RelationshipRecord{
			FromID: "module:" + dir,
			ToID:   ent.ID,
			Kind:   string(types.RelationshipKindDependsOnConfig),
			Properties: map[string]string{
				"config_subtype": spec.subtype,
				"config_format":  string(spec.format),
			},
		})
		// CONFIGURES — directional inverse, lets docgen pull configs into
		// downstream Module pages even when no DEPENDS_ON_CONFIG resolves.
		rels = append(rels, types.RelationshipRecord{
			FromID: ent.ID,
			ToID:   "module:" + dir,
			Kind:   string(types.RelationshipKindConfigures),
			Properties: map[string]string{
				"config_subtype": spec.subtype,
			},
		})
	}

	sortEntities(entities)
	sortRels(rels)

	span.SetAttributes(
		attribute.Int("config_entities", len(entities)),
		attribute.Int("config_edges", len(rels)),
		attribute.Int("input_files", len(files)),
	)
	return entities, rels, nil
}

// readBounded reads at most maxFileBytes from path.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxFileBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// buildConfigEntity constructs the SCOPE.Config EntityRecord, including its
// per-format Properties bag.
func buildConfigEntity(repoRoot, relPath string, spec configSpec, content []byte) types.EntityRecord {
	base := filepath.Base(filepath.FromSlash(relPath))
	rel := filepath.ToSlash(relPath)
	repoTag := filepath.Base(repoRoot)
	if repoTag == "" || repoTag == "." {
		repoTag = "repo"
	}
	qual := repoTag + "::" + rel

	props := map[string]string{
		"format":  string(spec.format),
		"subtype": spec.subtype,
	}

	parseInto(props, spec, content)

	ent := types.EntityRecord{
		Name:          base,
		QualifiedName: qual,
		Kind:          string(types.EntityKindConfig),
		Subtype:       spec.subtype,
		Language:      languageForFormat(spec.format),
		SourceFile:    rel,
		StartLine:     1,
		EndLine:       1,
		Signature:     "# " + base,
		Properties:    props,
	}
	// Stable synthetic ID so cross-repo references and tests can locate the
	// entity without relying on org/project hashing.
	ent.ID = "scope:config:" + spec.subtype + ":" + rel
	return ent
}

// languageForFormat maps the parsed format to a sensible language tag for
// downstream language-aware filters. Falls back to "text" when no closer
// match is available — config files are not source code.
func languageForFormat(f configKind) string {
	switch f {
	case formatJSON:
		return "json"
	case formatYAML:
		return "yaml"
	case formatTOML:
		return "toml"
	case formatXML:
		return "xml"
	case formatDockerfile:
		return "dockerfile"
	case formatMakefile:
		return "makefile"
	case formatGoMod:
		return "go"
	case formatProperties, formatINI:
		return "properties"
	case formatEnv:
		return "env"
	case formatGradle:
		return "groovy"
	case formatJSConfig, formatBundlerJS, formatTestRunnerJS, formatMobileJS:
		return "javascript"
	case formatBundlerJSON, formatTestRunnerJSON, formatMobileJSON:
		return "json"
	case formatTestRunnerYAML:
		return "yaml"
	}
	return "text"
}

// parseInto fills props (in place) with format-specific information.
func parseInto(props map[string]string, spec configSpec, content []byte) {
	if len(content) == 0 {
		return
	}
	switch spec.format {
	case formatJSON:
		parseJSON(props, spec, content)
	case formatTOML:
		parseTOML(props, spec, content)
	case formatXML:
		parseXML(props, spec, content)
	case formatProperties:
		parseProperties(props, content)
	case formatINI:
		parseINI(props, content)
	case formatEnv:
		parseEnv(props, content)
	case formatYAML:
		parseYAML(props, content)
	case formatGradle:
		parseGradle(props, content)
	case formatRequirements:
		parseRequirements(props, content)
	case formatGoMod:
		parseGoMod(props, content)
	case formatMakefile:
		parseMakefile(props, content)
	case formatDockerfile:
		parseDockerfile(props, content)
	case formatJSConfig:
		// JS/TS config files: only record top-level export/identifier hints.
		parseJSConfig(props, content)
	case formatBundlerJSON:
		parseBundlerJSON(props, spec, content)
	case formatBundlerJS:
		parseBundlerJS(props, spec, content)
	case formatTestRunnerJSON:
		parseTestRunnerJSON(props, spec, content)
	case formatTestRunnerJS:
		parseTestRunnerJS(props, spec, content)
	case formatTestRunnerYAML:
		parseTestRunnerYAML(props, spec, content)
	case formatMobileJSON:
		parseMobileJSON(props, spec, content)
	case formatMobileJS:
		parseMobileJS(props, spec, content)
	}
}

// ---------------------------------------------------------------------------
// Per-format parsers (intentionally permissive — we only need stable signal).
// ---------------------------------------------------------------------------

func parseJSON(props map[string]string, spec configSpec, content []byte) {
	// tsconfig.json is JSONC: the TypeScript compiler accepts // and /* */
	// comments and trailing commas, which idiomatic configs use heavily.
	// Strict encoding/json rejects them, so a commented tsconfig previously
	// failed the decode below and yielded NO mined keys (the reason
	// file_parsing was only partial — #2865). Strip comments first for the
	// typescript_project subtype so the full compilerOptions / references /
	// extends surface is recoverable.
	if spec.subtype == "typescript_project" {
		content = stripTrailingCommas(stripJSONC(content))
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(content, &generic); err != nil {
		return
	}
	props["keys_top_level"] = joinSortedKeys(generic)

	// tsconfig.json deep parse (#2865): mine compilerOptions, project
	// references, extends chain, and include/exclude globs. These are the
	// fields that make a tsconfig meaningful — the module resolution mode,
	// path aliases (which downstream import resolution depends on), the
	// referenced sub-projects (which form the TS build graph), and the base
	// config it extends. file_parsing is "full" once these are surfaced.
	if spec.subtype == "typescript_project" {
		parseTSConfig(props, content)
	}

	// package.json is the only JSON file we deeply mine.
	if spec.subtype == "node_project" {
		var pkg struct {
			Name             string            `json:"name"`
			Version          string            `json:"version"`
			Scripts          map[string]string `json:"scripts"`
			Dependencies     map[string]string `json:"dependencies"`
			DevDependencies  map[string]string `json:"devDependencies"`
			PeerDependencies map[string]string `json:"peerDependencies"`
			// Workspaces drives the monorepo dependency graph for Bun / npm /
			// Yarn / pnpm. It is either a string array or an object with a
			// "packages" array; json.RawMessage lets us accept both forms.
			Workspaces json.RawMessage `json:"workspaces"`
			// AVA's config conventionally lives in package.json under "ava"
			// (issue #2864). Mining it here yields the test-target globs without
			// a separate config file.
			Ava json.RawMessage `json:"ava"`
		}
		if err := json.Unmarshal(content, &pkg); err != nil {
			return
		}
		if pkg.Name != "" {
			props["project_name"] = pkg.Name
		}
		if pkg.Version != "" {
			props["project_version"] = pkg.Version
		}
		if len(pkg.Scripts) > 0 {
			props["scripts"] = joinSortedKeys(pkg.Scripts)
		}
		var allDeps []string
		for k := range pkg.Dependencies {
			allDeps = append(allDeps, k)
		}
		for k := range pkg.DevDependencies {
			allDeps = append(allDeps, k+" (dev)")
		}
		for k := range pkg.PeerDependencies {
			allDeps = append(allDeps, k+" (peer)")
		}
		if len(allDeps) > 0 {
			sort.Strings(allDeps)
			props["dependencies"] = capJoin(allDeps)
		}
		if ws := parseWorkspaces(pkg.Workspaces); len(ws) > 0 {
			sort.Strings(ws)
			props["workspaces"] = capJoin(ws)
		}
		if len(pkg.Ava) > 0 {
			parseAvaBlock(props, pkg.Ava)
		}
	}
}

// parseTSConfig mines a tsconfig.json (#2865). It records the headline
// compilerOptions (target / module / moduleResolution / strict / baseUrl /
// outDir / rootDir / jsx), the path-alias keys (compilerOptions.paths), the
// extends base config, the project references, and include/exclude globs.
// All values are stable structural signal — no TypeScript evaluation. The
// content passed in has already had JSONC comments stripped.
func parseTSConfig(props map[string]string, content []byte) {
	var ts struct {
		Extends    json.RawMessage `json:"extends"`
		Include    []string        `json:"include"`
		Exclude    []string        `json:"exclude"`
		Files      []string        `json:"files"`
		References []struct {
			Path string `json:"path"`
		} `json:"references"`
		CompilerOptions struct {
			Target           string              `json:"target"`
			Module           string              `json:"module"`
			ModuleResolution string              `json:"moduleResolution"`
			JSX              string              `json:"jsx"`
			BaseURL          string              `json:"baseUrl"`
			OutDir           string              `json:"outDir"`
			RootDir          string              `json:"rootDir"`
			Strict           *bool               `json:"strict"`
			Paths            map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(content, &ts); err != nil {
		return
	}

	co := ts.CompilerOptions
	setIf := func(key, val string) {
		if val != "" {
			props[key] = val
		}
	}
	setIf("ts_target", co.Target)
	setIf("ts_module", co.Module)
	setIf("ts_module_resolution", co.ModuleResolution)
	setIf("ts_jsx", co.JSX)
	setIf("ts_base_url", co.BaseURL)
	setIf("ts_out_dir", co.OutDir)
	setIf("ts_root_dir", co.RootDir)
	if co.Strict != nil {
		if *co.Strict {
			props["ts_strict"] = "true"
		} else {
			props["ts_strict"] = "false"
		}
	}

	// Path aliases drive module resolution; surface the alias keys (the RHS
	// target arrays are noisy and resolution-specific, so we record the alias
	// names — the resolvable surface).
	if len(co.Paths) > 0 {
		aliases := make([]string, 0, len(co.Paths))
		for k := range co.Paths {
			aliases = append(aliases, k)
		}
		sort.Strings(aliases)
		props["ts_path_aliases"] = capJoin(aliases)
	}

	// extends is either a string or (TS 5.0+) an array of strings.
	if base := parseTSExtends(ts.Extends); len(base) > 0 {
		sort.Strings(base)
		props["ts_extends"] = capJoin(base)
	}

	// Project references form the TypeScript build graph.
	if len(ts.References) > 0 {
		refs := make([]string, 0, len(ts.References))
		for _, r := range ts.References {
			if r.Path != "" {
				refs = append(refs, r.Path)
			}
		}
		sort.Strings(refs)
		refs = dedup(refs)
		if len(refs) > 0 {
			props["ts_references"] = capJoin(refs)
		}
	}

	if len(ts.Include) > 0 {
		inc := append([]string(nil), ts.Include...)
		sort.Strings(inc)
		props["ts_include"] = capJoin(inc)
	}
	if len(ts.Exclude) > 0 {
		exc := append([]string(nil), ts.Exclude...)
		sort.Strings(exc)
		props["ts_exclude"] = capJoin(exc)
	}
	if len(ts.Files) > 0 {
		f := append([]string(nil), ts.Files...)
		sort.Strings(f)
		props["ts_files"] = capJoin(f)
	}
}

// parseTSExtends accepts the two tsconfig "extends" shapes:
//
//	"extends": "./tsconfig.base.json"
//	"extends": ["@tsconfig/node18/tsconfig.json", "./base.json"]   // TS 5.0+
func parseTSExtends(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return nil
}

// parseAvaBlock mines the package.json "ava" object (issue #2864). AVA test
// targets come from "files" (spec globs); "require" + "extensions" are setup
// dependencies the spec discovery depends on (config->spec dependency_graph).
func parseAvaBlock(props map[string]string, raw json.RawMessage) {
	var ava struct {
		Files      []string        `json:"files"`
		Match      []string        `json:"match"`
		Require    []string        `json:"require"`
		Extensions json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(raw, &ava); err != nil {
		return
	}
	specs := append([]string(nil), ava.Files...)
	specs = append(specs, ava.Match...)
	sort.Strings(specs)
	specs = dedup(specs)
	if len(specs) > 0 {
		props["test_targets"] = capJoin(specs)
	}
	if len(ava.Require) > 0 {
		reqs := append([]string(nil), ava.Require...)
		sort.Strings(reqs)
		props["spec_dependencies"] = capJoin(reqs)
	}
}

// parseWorkspaces accepts the two package.json "workspaces" shapes:
//
//	"workspaces": ["packages/*", "apps/*"]
//	"workspaces": { "packages": ["packages/*"], "nohoist": [...] }
func parseWorkspaces(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Packages
	}
	return nil
}

// tomlSectionRE matches a section header `[name]` or `[name.subname]`.
var tomlSectionRE = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]`)

// tomlKeyRE matches `key = …` lines (top-level form).
var tomlKeyRE = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)\s*=`)

// tomlDepListRE matches the canonical pyproject.toml `dependencies = [...]` body.
var tomlDepListRE = regexp.MustCompile(`(?s)dependencies\s*=\s*\[([^\]]*)\]`)

func parseTOML(props map[string]string, spec configSpec, content []byte) {
	src := string(content)
	// Sections become "keys_top_level" (each [section] is a structural key).
	secs := tomlSectionRE.FindAllStringSubmatch(src, -1)
	var sectionNames []string
	seen := map[string]bool{}
	for _, m := range secs {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sectionNames = append(sectionNames, name)
	}
	if len(sectionNames) > 0 {
		sort.Strings(sectionNames)
		props["keys_top_level"] = capJoin(sectionNames)
	}

	if spec.subtype == "python_project" {
		// Find dependency list (PEP 621 style).
		if m := tomlDepListRE.FindStringSubmatch(src); m != nil {
			deps := splitTomlDepList(m[1])
			if len(deps) > 0 {
				props["dependencies"] = capJoin(deps)
			}
		}
		// poetry-style: collect [tool.poetry.dependencies] keys.
		if poetry := extractTomlSectionBody(src, "tool.poetry.dependencies"); poetry != "" {
			var keys []string
			for _, m := range tomlKeyRE.FindAllStringSubmatch(poetry, -1) {
				if k := strings.TrimSpace(m[1]); k != "" && k != "python" {
					keys = append(keys, k)
				}
			}
			if len(keys) > 0 {
				sort.Strings(keys)
				prev := props["dependencies"]
				if prev == "" {
					props["dependencies"] = capJoin(keys)
				} else {
					props["dependencies"] = capJoin(append(strings.Split(prev, ","), keys...))
				}
			}
		}
	}
}

func extractTomlSectionBody(src, name string) string {
	// Locate `[name]` then read until the next `[…]` header or EOF.
	idx := strings.Index(src, "["+name+"]")
	if idx < 0 {
		return ""
	}
	tail := src[idx+len(name)+2:]
	if next := tomlSectionRE.FindStringIndex(tail); next != nil {
		return tail[:next[0]]
	}
	return tail
}

// splitTomlDepList splits the body of a `dependencies = [ … ]` block.
func splitTomlDepList(body string) []string {
	var out []string
	for _, item := range strings.Split(body, ",") {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, "\"'")
		if item == "" || strings.HasPrefix(item, "#") {
			continue
		}
		// Strip version specifiers (e.g. "requests>=2.0" → "requests").
		name := pickPackageName(item)
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

var pkgNameRE = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*)?)`)

func pickPackageName(s string) string {
	m := pkgNameRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

func parseXML(props map[string]string, spec configSpec, content []byte) {
	if spec.subtype != "maven_project" {
		return
	}
	// Permissive maven POM parsing — we only need groupId/artifactId for
	// dependencies and the project's own coordinates.
	type mavenDep struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
		Scope      string `xml:"scope"`
	}
	var pom struct {
		XMLName      xml.Name `xml:"project"`
		GroupID      string   `xml:"groupId"`
		ArtifactID   string   `xml:"artifactId"`
		Version      string   `xml:"version"`
		Name         string   `xml:"name"`
		Dependencies struct {
			Dep []mavenDep `xml:"dependency"`
		} `xml:"dependencies"`
		Properties struct {
			Inner []xml.Name `xml:",any"`
		} `xml:"properties"`
	}
	if err := xml.Unmarshal(content, &pom); err != nil {
		return
	}
	if pom.ArtifactID != "" {
		props["project_name"] = pom.ArtifactID
	}
	if pom.GroupID != "" {
		props["group_id"] = pom.GroupID
	}
	if pom.Version != "" {
		props["project_version"] = pom.Version
	}
	if len(pom.Dependencies.Dep) > 0 {
		var deps []string
		for _, d := range pom.Dependencies.Dep {
			if d.GroupID == "" || d.ArtifactID == "" {
				continue
			}
			deps = append(deps, d.GroupID+":"+d.ArtifactID)
		}
		sort.Strings(deps)
		if len(deps) > 0 {
			props["dependencies"] = capJoin(deps)
		}
	}
}

var propertiesLineRE = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_.\-]+)\s*[=:]`)

func parseProperties(props map[string]string, content []byte) {
	matches := propertiesLineRE.FindAllStringSubmatch(string(content), -1)
	var keys []string
	seen := map[string]bool{}
	for _, m := range matches {
		k := strings.TrimSpace(m[1])
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		props["keys_top_level"] = capJoin(keys)
	}
}

var iniSectionRE = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]`)

func parseINI(props map[string]string, content []byte) {
	src := string(content)
	var sections []string
	seen := map[string]bool{}
	for _, m := range iniSectionRE.FindAllStringSubmatch(src, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sections = append(sections, name)
	}
	if len(sections) > 0 {
		sort.Strings(sections)
		props["keys_top_level"] = capJoin(sections)
	}
}

// envVarRE matches a NAME=value line. SECURITY: capture group is only the NAME.
// The value side after `=` is intentionally discarded — never stored.
var envVarRE = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)\s*=`)

func parseEnv(props map[string]string, content []byte) {
	matches := envVarRE.FindAllStringSubmatch(string(content), -1)
	var names []string
	seen := map[string]bool{}
	for _, m := range matches {
		name := m[1]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		props["keys_top_level"] = capJoin(names)
	}
	// SECURITY GUARD: explicitly mark the parser tier so downstream auditors
	// can assert that no value strings ever land in Properties for env files.
	props["redaction"] = "names_only"
}

// yamlTopKeyRE matches a top-level `key:` line (no leading whitespace).
var yamlTopKeyRE = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_.\-]*)\s*:`)

func parseYAML(props map[string]string, content []byte) {
	matches := yamlTopKeyRE.FindAllStringSubmatch(string(content), -1)
	var keys []string
	seen := map[string]bool{}
	for _, m := range matches {
		k := m[1]
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		props["keys_top_level"] = capJoin(keys)
	}
}

// gradleDepLineRE captures `implementation "group:artifact:version"` and
// variants (api, testImplementation, runtimeOnly, …).
var gradleDepLineRE = regexp.MustCompile(`(?m)^\s*(?:implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly|annotationProcessor)\s*[\(\s]?["']([^"']+)["']`)

// gradlePluginRE captures `id 'plugin.name'` and `id("plugin.name")`.
var gradlePluginRE = regexp.MustCompile(`(?m)^\s*id\s*[\(\s]["']([^"']+)["']`)

func parseGradle(props map[string]string, content []byte) {
	src := string(content)
	var deps []string
	for _, m := range gradleDepLineRE.FindAllStringSubmatch(src, -1) {
		deps = append(deps, m[1])
	}
	sort.Strings(deps)
	if len(deps) > 0 {
		props["dependencies"] = capJoin(deps)
	}
	var plugins []string
	for _, m := range gradlePluginRE.FindAllStringSubmatch(src, -1) {
		plugins = append(plugins, m[1])
	}
	sort.Strings(plugins)
	if len(plugins) > 0 {
		props["plugins"] = capJoin(plugins)
	}
}

func parseRequirements(props map[string]string, content []byte) {
	var deps []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		name := pickPackageName(line)
		if name != "" {
			deps = append(deps, name)
		}
	}
	sort.Strings(deps)
	if len(deps) > 0 {
		props["dependencies"] = capJoin(deps)
	}
}

var goModModuleRE = regexp.MustCompile(`(?m)^module\s+(\S+)`)
var goModRequireBlockRE = regexp.MustCompile(`(?s)require\s*\(([^)]+)\)`)
var goModRequireLineRE = regexp.MustCompile(`(?m)^\s*(\S+)\s+(v\S+)`)
var goModSingleRequireRE = regexp.MustCompile(`(?m)^require\s+(\S+)\s+(v\S+)`)

func parseGoMod(props map[string]string, content []byte) {
	src := string(content)
	if m := goModModuleRE.FindStringSubmatch(src); m != nil {
		props["project_name"] = m[1]
	}
	var deps []string
	seen := map[string]bool{}
	for _, bm := range goModRequireBlockRE.FindAllStringSubmatch(src, -1) {
		for _, lm := range goModRequireLineRE.FindAllStringSubmatch(bm[1], -1) {
			if seen[lm[1]] {
				continue
			}
			seen[lm[1]] = true
			deps = append(deps, lm[1])
		}
	}
	for _, sm := range goModSingleRequireRE.FindAllStringSubmatch(src, -1) {
		if seen[sm[1]] {
			continue
		}
		seen[sm[1]] = true
		deps = append(deps, sm[1])
	}
	sort.Strings(deps)
	if len(deps) > 0 {
		props["dependencies"] = capJoin(deps)
	}
}

// makeTargetRE matches a top-level Makefile target line: `name:` or `name: deps`.
// Excludes pattern rules ("%.o:"), .PHONY directives, and indented recipe lines.
var makeTargetRE = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_.\-]*)\s*:(?:\s|$)`)

func parseMakefile(props map[string]string, content []byte) {
	matches := makeTargetRE.FindAllStringSubmatch(string(content), -1)
	var targets []string
	seen := map[string]bool{}
	for _, m := range matches {
		name := m[1]
		if name == "" || seen[name] {
			continue
		}
		// Skip the special directives.
		if strings.HasPrefix(name, ".") {
			continue
		}
		seen[name] = true
		targets = append(targets, name)
	}
	sort.Strings(targets)
	if len(targets) > 0 {
		props["scripts"] = capJoin(targets)
	}
}

// dockerInstructionRE matches the verb at the start of every effective
// Dockerfile line. Comments and blank lines are dropped.
var dockerInstructionRE = regexp.MustCompile(`(?m)^\s*([A-Z][A-Z]+)\s+`)

func parseDockerfile(props map[string]string, content []byte) {
	src := string(content)
	matches := dockerInstructionRE.FindAllStringSubmatch(src, -1)
	var verbs []string
	seen := map[string]bool{}
	for _, m := range matches {
		v := m[1]
		if seen[v] {
			continue
		}
		seen[v] = true
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)
	if len(verbs) > 0 {
		props["keys_top_level"] = capJoin(verbs)
	}
	// Capture FROM bases as "dependencies" — they are the build deps.
	fromRE := regexp.MustCompile(`(?m)^\s*FROM\s+(\S+)`)
	var froms []string
	for _, m := range fromRE.FindAllStringSubmatch(src, -1) {
		froms = append(froms, m[1])
	}
	sort.Strings(froms)
	if len(froms) > 0 {
		props["dependencies"] = capJoin(froms)
	}
}

// jsConfigExportRE matches top-level `export const NAME`, `export default`,
// `module.exports`, and `defineConfig`-style identifiers.
var jsConfigExportRE = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+|const\s+|let\s+|var\s+)|module\.exports\s*=|module\.exports\.)\s*([A-Za-z_$][A-Za-z0-9_$]*)?`)

func parseJSConfig(props map[string]string, content []byte) {
	matches := jsConfigExportRE.FindAllStringSubmatch(string(content), -1)
	var keys []string
	seen := map[string]bool{}
	for _, m := range matches {
		k := strings.TrimSpace(m[1])
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		props["keys_top_level"] = capJoin(keys)
	}
}

// ---------------------------------------------------------------------------
// JS/TS bundler & monorepo config parsers (issue #2863)
//
// These power the build_system capabilities for the JS bundler/monorepo tools:
//   - target_extraction: emit build targets / entry points
//       → Properties["scripts"]      (named build targets / tasks)
//       → Properties["entry_points"] (bundler entry/input modules)
//   - dependency_graph: inter-target / inter-package dependency edges
//       → Properties["target_dependencies"] (task graph: target -> dependsOn)
//       → Properties["workspaces"]           (monorepo package globs)
//
// JSON configs (turbo.json, nx.json, project.json, lerna.json, .parcelrc) are
// JSONC-tolerant. JS configs (webpack/vite/rollup/esbuild) are mined with
// permissive regexes — no JS evaluation, just stable structural signal.
// ---------------------------------------------------------------------------

// stripTrailingCommas removes a comma that is immediately followed (ignoring
// whitespace) by a closing `}` or `]`. tsconfig.json (and other JSONC files)
// permit trailing commas, which strict encoding/json rejects. Run AFTER
// stripJSONC so comments don't confuse the lookahead. String contents are
// preserved so a comma inside a string value is never touched (#2865).
func stripTrailingCommas(content []byte) []byte {
	out := make([]byte, 0, len(content))
	inStr := false
	for i := 0; i < len(content); i++ {
		c := content[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(content) {
				out = append(out, content[i+1])
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			// Look ahead past whitespace for a closing bracket/brace.
			j := i + 1
			for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n' || content[j] == '\r') {
				j++
			}
			if j < len(content) && (content[j] == '}' || content[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out = append(out, c)
	}
	return out
}

// stripJSONC removes // line comments and /* */ block comments so JSONC files
// (turbo.json, tsconfig-style) decode with encoding/json. String contents are
// preserved; this is a best-effort lexer good enough for config mining.
func stripJSONC(content []byte) []byte {
	var out []byte
	src := content
	inStr := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				out = append(out, src[i+1])
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++ // land on the closing '/'
		default:
			out = append(out, c)
		}
	}
	return out
}

func parseBundlerJSON(props map[string]string, spec configSpec, content []byte) {
	clean := stripJSONC(content)
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(clean, &generic); err != nil {
		return
	}
	props["keys_top_level"] = joinSortedKeys(generic)

	switch spec.subtype {
	case "turborepo_config":
		parseTurboConfig(props, clean)
	case "nx_config":
		parseNxConfig(props, clean)
	case "nx_project":
		parseNxProject(props, clean)
	case "lerna_config":
		parseLernaConfig(props, clean)
	case "parcel_config":
		parseParcelConfig(props, clean)
	}
}

// parseTurboConfig mines turbo.json. Build targets live under "tasks" (Turbo
// 2.x) or "pipeline" (Turbo 1.x); each task's "dependsOn" forms the task graph.
func parseTurboConfig(props map[string]string, content []byte) {
	var turbo struct {
		Tasks    map[string]turboTask `json:"tasks"`
		Pipeline map[string]turboTask `json:"pipeline"`
	}
	if err := json.Unmarshal(content, &turbo); err != nil {
		return
	}
	tasks := turbo.Tasks
	if len(tasks) == 0 {
		tasks = turbo.Pipeline
	}
	var names []string
	var edges []string
	for name, t := range tasks {
		names = append(names, name)
		for _, dep := range t.DependsOn {
			edges = append(edges, name+"->"+dep)
		}
	}
	sort.Strings(names)
	sort.Strings(edges)
	if len(names) > 0 {
		props["scripts"] = capJoin(names)
	}
	if len(edges) > 0 {
		props["target_dependencies"] = capJoin(edges)
	}
}

type turboTask struct {
	DependsOn []string `json:"dependsOn"`
}

// parseNxConfig mines nx.json. Reusable target defaults define the canonical
// build targets; targetDefaults[*].dependsOn forms the inter-target graph.
func parseNxConfig(props map[string]string, content []byte) {
	var nx struct {
		TargetDefaults map[string]struct {
			DependsOn []json.RawMessage `json:"dependsOn"`
		} `json:"targetDefaults"`
	}
	if err := json.Unmarshal(content, &nx); err != nil {
		return
	}
	var names, edges []string
	for name, def := range nx.TargetDefaults {
		names = append(names, name)
		for _, raw := range def.DependsOn {
			dep := strings.Trim(string(raw), `"`)
			edges = append(edges, name+"->"+dep)
		}
	}
	sort.Strings(names)
	sort.Strings(edges)
	if len(names) > 0 {
		props["scripts"] = capJoin(names)
	}
	if len(edges) > 0 {
		props["target_dependencies"] = capJoin(edges)
	}
}

// parseNxProject mines an Nx project.json. "targets" are the build targets;
// "implicitDependencies" are inter-package edges to other workspace projects.
func parseNxProject(props map[string]string, content []byte) {
	var proj struct {
		Name                 string                     `json:"name"`
		Targets              map[string]json.RawMessage `json:"targets"`
		ImplicitDependencies []string                   `json:"implicitDependencies"`
	}
	if err := json.Unmarshal(content, &proj); err != nil {
		return
	}
	if proj.Name != "" {
		props["project_name"] = proj.Name
	}
	if len(proj.Targets) > 0 {
		props["scripts"] = joinSortedKeys(proj.Targets)
	}
	if len(proj.ImplicitDependencies) > 0 {
		deps := append([]string(nil), proj.ImplicitDependencies...)
		sort.Strings(deps)
		props["workspaces"] = capJoin(deps)
	}
}

// parseLernaConfig mines lerna.json. "packages" (or top-level workspaces) are
// the monorepo package globs; "command" keys are configured lifecycle targets.
func parseLernaConfig(props map[string]string, content []byte) {
	var lerna struct {
		Packages []string                   `json:"packages"`
		Command  map[string]json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(content, &lerna); err != nil {
		return
	}
	if len(lerna.Packages) > 0 {
		ws := append([]string(nil), lerna.Packages...)
		sort.Strings(ws)
		props["workspaces"] = capJoin(ws)
	}
	if len(lerna.Command) > 0 {
		props["scripts"] = joinSortedKeys(lerna.Command)
	}
}

// parseParcelConfig mines a .parcelrc. The pipeline plugin maps (transformers,
// bundlers, packagers, …) are the configured build targets; "extends" is the
// upstream config dependency.
func parseParcelConfig(props map[string]string, content []byte) {
	var parcel struct {
		Extends      json.RawMessage            `json:"extends"`
		Transformers map[string]json.RawMessage `json:"transformers"`
		Bundlers     map[string]json.RawMessage `json:"bundlers"`
		Packagers    map[string]json.RawMessage `json:"packagers"`
		Resolvers    json.RawMessage            `json:"resolvers"`
		Optimizers   map[string]json.RawMessage `json:"optimizers"`
	}
	if err := json.Unmarshal(content, &parcel); err != nil {
		return
	}
	var targets []string
	for _, m := range []map[string]json.RawMessage{
		parcel.Transformers, parcel.Bundlers, parcel.Packagers, parcel.Optimizers,
	} {
		for k := range m {
			targets = append(targets, k)
		}
	}
	sort.Strings(targets)
	if len(targets) > 0 {
		props["scripts"] = capJoin(targets)
	}
	if len(parcel.Extends) > 0 {
		ext := strings.Trim(string(parcel.Extends), `"[] `)
		if ext != "" {
			props["dependencies"] = ext
		}
	}
}

// Regexes mining JS/TS bundler configs. We do not evaluate JS — these capture
// the canonical literal forms the docs recommend.
var (
	// entry / input: `entry: './src/main.ts'`, `input: 'src/index.js'`,
	// entryPoints array members `'src/app.tsx'`.
	bundlerEntryKeyRE = regexp.MustCompile(`(?m)\b(?:entry|input|entryPoints)\s*:\s*(['"][^'"]+['"]|\[[^\]]*\]|\{[^}]*\})`)
	bundlerStringRE   = regexp.MustCompile(`['"]([^'"]+)['"]`)
	// output dir/file: `outDir: 'dist'`, `outfile: 'out.js'`, `dir: 'build'`,
	// `output: { dir: 'dist' }`, `filename: 'bundle.js'`.
	bundlerOutRE = regexp.MustCompile(`(?m)\b(?:outDir|outfile|outdir|dir|filename|file)\s*:\s*['"]([^'"]+)['"]`)
	// webpack `path: path.resolve(__dirname, 'dist')` — capture the final
	// string literal of a path.resolve / path.join call assigned to `path:`.
	bundlerPathResolveRE = regexp.MustCompile(`(?m)\bpath\s*:\s*path\.(?:resolve|join)\([^)]*['"]([^'"]+)['"]\s*\)`)
)

// parseBundlerJS mines webpack/vite/rollup/esbuild JS config files for entry
// points (dependency_graph roots) and output targets (target_extraction).
func parseBundlerJS(props map[string]string, spec configSpec, content []byte) {
	parseJSConfig(props, content) // keep top-level export hints
	src := string(content)

	var entries []string
	for _, m := range bundlerEntryKeyRE.FindAllStringSubmatch(src, -1) {
		for _, sm := range bundlerStringRE.FindAllStringSubmatch(m[1], -1) {
			val := sm[1]
			if val != "" && !strings.Contains(val, "${") {
				entries = append(entries, val)
			}
		}
	}
	sort.Strings(entries)
	entries = dedup(entries)
	if len(entries) > 0 {
		props["entry_points"] = capJoin(entries)
	}

	var outs []string
	for _, m := range bundlerOutRE.FindAllStringSubmatch(src, -1) {
		outs = append(outs, m[1])
	}
	for _, m := range bundlerPathResolveRE.FindAllStringSubmatch(src, -1) {
		outs = append(outs, m[1])
	}
	sort.Strings(outs)
	outs = dedup(outs)
	if len(outs) > 0 {
		props["scripts"] = capJoin(outs)
	}
}

// ---------------------------------------------------------------------------
// JS/TS test-runner config parsers (issue #2864)
//
// These power the test-runner capabilities for AVA / Cypress / Jasmine /
// Mocha / Playwright / tap / Vitest (Jest is the reference, handled by the
// engine test rules):
//   - target_extraction: discover the runner's test targets / spec globs
//       → Properties["test_targets"]  (spec globs / files / dirs)
//       → Properties["test_projects"] (Cypress e2e/component, Playwright project
//                                       matrix — framework_specific idioms)
//   - dependency_graph: the config -> spec / setup dependency edges
//       → Properties["spec_dependencies"] (setup/require/helper modules the
//                                            spec discovery hangs off of)
//
// JSON configs (.mocharc.json, jasmine.json) are JSONC-tolerant. JS configs
// (vitest/cypress/playwright/.mocharc.js/ava.config.js) are mined with
// permissive regexes — no JS evaluation, just stable structural signal. YAML
// (.taprc, .mocharc.yml) is mined line-by-line.
// ---------------------------------------------------------------------------

func parseTestRunnerJSON(props map[string]string, spec configSpec, content []byte) {
	clean := stripJSONC(content)
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(clean, &generic); err != nil {
		return
	}
	props["keys_top_level"] = joinSortedKeys(generic)

	switch spec.subtype {
	case "mocha_config":
		parseMochaConfigJSON(props, clean)
	case "jasmine_config":
		parseJasmineConfig(props, clean)
	}
}

// parseMochaConfigJSON mines a .mocharc.json/.jsonc. "spec" holds the test
// target globs; "require" lists setup modules the specs depend on; "extension"
// constrains discovered file types.
func parseMochaConfigJSON(props map[string]string, content []byte) {
	var mocha struct {
		Spec      json.RawMessage `json:"spec"`
		Require   json.RawMessage `json:"require"`
		Extension json.RawMessage `json:"extension"`
		Recursive bool            `json:"recursive"`
	}
	if err := json.Unmarshal(content, &mocha); err != nil {
		return
	}
	if specs := jsonStringOrArray(mocha.Spec); len(specs) > 0 {
		sort.Strings(specs)
		props["test_targets"] = capJoin(dedup(specs))
	}
	if reqs := jsonStringOrArray(mocha.Require); len(reqs) > 0 {
		sort.Strings(reqs)
		props["spec_dependencies"] = capJoin(dedup(reqs))
	}
}

// parseJasmineConfig mines a jasmine.json. Test targets come from joining
// "spec_dir" with each "spec_files" glob; "helpers" are setup deps the specs
// hang off of (config->spec dependency_graph).
func parseJasmineConfig(props map[string]string, content []byte) {
	var jas struct {
		SpecDir   string   `json:"spec_dir"`
		SpecFiles []string `json:"spec_files"`
		Helpers   []string `json:"helpers"`
	}
	if err := json.Unmarshal(content, &jas); err != nil {
		return
	}
	var targets []string
	for _, f := range jas.SpecFiles {
		if jas.SpecDir != "" {
			targets = append(targets, jas.SpecDir+"/"+f)
		} else {
			targets = append(targets, f)
		}
	}
	sort.Strings(targets)
	if len(targets) > 0 {
		props["test_targets"] = capJoin(dedup(targets))
	}
	if len(jas.Helpers) > 0 {
		var helpers []string
		for _, h := range jas.Helpers {
			if jas.SpecDir != "" {
				helpers = append(helpers, jas.SpecDir+"/"+h)
			} else {
				helpers = append(helpers, h)
			}
		}
		sort.Strings(helpers)
		props["spec_dependencies"] = capJoin(dedup(helpers))
	}
}

// jsonStringOrArray accepts a JSON value that is either a single string or an
// array of strings (Mocha config keys accept both shapes).
func jsonStringOrArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

// Regexes mining JS/TS test-runner configs. We do not evaluate JS — these
// capture the canonical literal forms the runners' docs recommend.
var (
	// Vitest: `include: ['tests/**/*.test.ts']`, `exclude: [...]`.
	// Mocha .mocharc.js: `spec: 'test/**/*.spec.js'`.
	testIncludeRE = regexp.MustCompile(`(?m)\b(?:include|spec|files|match|testMatch|testRegex)\s*:\s*(['"][^'"]+['"]|\[[^\]]*\])`)
	// Cypress / Playwright: `specPattern: 'cypress/e2e/**/*.cy.ts'`,
	// `testDir: './tests'`, `testMatch: '**/*.spec.ts'`.
	testDirPatternRE = regexp.MustCompile(`(?m)\b(?:specPattern|testDir|testMatch)\s*:\s*(['"][^'"]+['"]|\[[^\]]*\])`)
	// Setup / support dependencies: `supportFile: '...'`, `setupFiles: [...]`,
	// `globalSetup: '...'`, `require: [...]`, `setupNodeEvents` (presence only).
	testSetupRE = regexp.MustCompile(`(?m)\b(?:supportFile|setupFiles|setupFilesAfterEach|globalSetup|globalTeardown|require)\s*:\s*(['"][^'"]+['"]|\[[^\]]*\])`)
	// Cypress e2e/component blocks + Playwright `projects: [{ name: '...' }]`.
	testProjectNameRE = regexp.MustCompile(`(?m)\bname\s*:\s*['"]([^'"]+)['"]`)
	cypressBlockRE    = regexp.MustCompile(`(?m)\b(e2e|component)\s*:\s*\{`)
)

// extractQuotedStrings pulls every quoted string literal out of a regex
// capture group (single string literal or a [...] array body).
func extractQuotedStrings(group string) []string {
	var out []string
	for _, sm := range bundlerStringRE.FindAllStringSubmatch(group, -1) {
		val := sm[1]
		if val != "" && !strings.Contains(val, "${") {
			out = append(out, val)
		}
	}
	return out
}

// parseTestRunnerJS mines vitest/cypress/playwright/.mocharc.js/ava.config.js
// for test targets (target_extraction) and config->spec setup dependencies
// (dependency_graph). Cypress e2e/component blocks and the Playwright project
// matrix are recorded under test_projects (framework_specific idioms).
func parseTestRunnerJS(props map[string]string, spec configSpec, content []byte) {
	parseJSConfig(props, content) // keep top-level export hints
	src := string(content)

	var targets []string
	for _, m := range testIncludeRE.FindAllStringSubmatch(src, -1) {
		targets = append(targets, extractQuotedStrings(m[1])...)
	}
	for _, m := range testDirPatternRE.FindAllStringSubmatch(src, -1) {
		targets = append(targets, extractQuotedStrings(m[1])...)
	}
	sort.Strings(targets)
	targets = dedup(targets)
	if len(targets) > 0 {
		props["test_targets"] = capJoin(targets)
	}

	var deps []string
	for _, m := range testSetupRE.FindAllStringSubmatch(src, -1) {
		deps = append(deps, extractQuotedStrings(m[1])...)
	}
	sort.Strings(deps)
	deps = dedup(deps)
	if len(deps) > 0 {
		props["spec_dependencies"] = capJoin(deps)
	}

	// framework_specific: Cypress runs an e2e/component testing-type matrix,
	// Playwright runs a named-project (browser/device) matrix. Neither maps to
	// a plain "test target" — record the project/block names separately.
	var projects []string
	switch spec.subtype {
	case "cypress_config":
		for _, m := range cypressBlockRE.FindAllStringSubmatch(src, -1) {
			projects = append(projects, m[1])
		}
	case "playwright_config":
		// Only mine project names when a "projects:" array is present, so a
		// stray `name:` field elsewhere is not misread as a project.
		if strings.Contains(src, "projects") {
			for _, m := range testProjectNameRE.FindAllStringSubmatch(src, -1) {
				projects = append(projects, m[1])
			}
		}
	}
	sort.Strings(projects)
	projects = dedup(projects)
	if len(projects) > 0 {
		props["test_projects"] = capJoin(projects)
	}
}

// Regexes mining YAML test-runner configs (.taprc, .mocharc.yml).
var (
	// Use [^\S\n] (horizontal whitespace only) around the value so the scalar
	// matcher cannot consume a following newline and misread an indented list
	// item as the scalar value.
	yamlListItemRE  = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*['"]?([^'"\n#]+?)['"]?[^\S\n]*$`)
	yamlScalarKeyRE = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_.\-]*)[^\S\n]*:[^\S\n]+['"]?([^'"\n#]+?)['"]?[^\S\n]*$`)
)

// parseTestRunnerYAML mines .taprc / .mocharc.yml. Test targets come from a
// "files"/"spec" key (scalar or YAML list); setup deps from "require"/"before".
func parseTestRunnerYAML(props map[string]string, spec configSpec, content []byte) {
	parseYAML(props, content) // keep top-level keys
	src := string(content)

	targetKeys := map[string]bool{"files": true, "spec": true, "test": true}
	depKeys := map[string]bool{"require": true, "before": true, "after": true}

	var targets, deps []string
	// Block-style list values: `files:` followed by `  - 'glob'` items.
	for _, block := range splitYAMLBlocks(src) {
		key := block.key
		for _, item := range block.items {
			switch {
			case targetKeys[key]:
				targets = append(targets, item)
			case depKeys[key]:
				deps = append(deps, item)
			}
		}
	}
	// Scalar values: `files: 'test/**/*.js'`.
	for _, m := range yamlScalarKeyRE.FindAllStringSubmatch(src, -1) {
		key, val := m[1], strings.TrimSpace(m[2])
		if val == "" {
			continue
		}
		switch {
		case targetKeys[key]:
			targets = append(targets, val)
		case depKeys[key]:
			deps = append(deps, val)
		}
	}
	sort.Strings(targets)
	targets = dedup(targets)
	if len(targets) > 0 {
		props["test_targets"] = capJoin(targets)
	}
	sort.Strings(deps)
	deps = dedup(deps)
	if len(deps) > 0 {
		props["spec_dependencies"] = capJoin(deps)
	}
}

// ---------------------------------------------------------------------------
// Mobile config parsers — React Native CLI + Expo (issue #2879)
//
// These power the framework_specific mobile idioms:
//   - metro_config_detection: metro.config.{js,ts} resolver/transformer block
//       → Properties["metro_keys"] (resolver/transformer/projectRoot/...)
//   - native_link_recognition: react-native.config.js native-module links
//       → Properties["native_modules"] (dependencies.* keys = autolinked deps)
//   - eas_build_detection: eas.json EAS Build/Submit profiles
//       → Properties["eas_build_profiles"] / Properties["eas_submit_profiles"]
//   - expo_config_extraction: app.json / app.config.{js,ts} Expo manifest
//       → Properties["expo_name"|"expo_slug"|"expo_scheme"|"expo_plugins"]
//
// JSON configs (eas.json, app.json) are JSONC-tolerant. JS configs
// (metro.config.js, react-native.config.js, app.config.{js,ts}) are mined with
// permissive regexes — no JS evaluation, just stable structural signal.
// ---------------------------------------------------------------------------

var (
	// metroSectionRE finds the top-level metro config sections we care about
	// (resolver/transformer/serializer/server/watchFolders/projectRoot).
	metroSectionRE = regexp.MustCompile(`(?m)\b(resolver|transformer|serializer|server|watchFolders|projectRoot|maxWorkers|cacheStores)\s*:`)
)

func parseMobileJSON(props map[string]string, spec configSpec, content []byte) {
	clean := stripJSONC(content)
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(clean, &generic); err != nil {
		return
	}
	props["keys_top_level"] = joinSortedKeys(generic)

	switch spec.subtype {
	case "eas_config":
		parseEASConfig(props, clean)
	case "expo_config":
		parseExpoManifest(props, clean)
	}
}

// parseEASConfig mines eas.json. EAS Build profiles live under "build"
// (development/preview/production/...), submit profiles under "submit"; the
// "cli" block carries the appVersionSource / requireCommit policy.
func parseEASConfig(props map[string]string, content []byte) {
	var eas struct {
		CLI    json.RawMessage            `json:"cli"`
		Build  map[string]json.RawMessage `json:"build"`
		Submit map[string]json.RawMessage `json:"submit"`
	}
	if err := json.Unmarshal(content, &eas); err != nil {
		return
	}
	if len(eas.Build) > 0 {
		props["eas_build_profiles"] = joinSortedKeys(eas.Build)
	}
	if len(eas.Submit) > 0 {
		props["eas_submit_profiles"] = joinSortedKeys(eas.Submit)
	}
	if len(eas.CLI) > 0 {
		var cli map[string]json.RawMessage
		if json.Unmarshal(eas.CLI, &cli) == nil && len(cli) > 0 {
			props["eas_cli_keys"] = joinSortedKeys(cli)
		}
	}
}

// parseExpoManifest mines app.json / the JSON-decodable head of an Expo
// manifest. The manifest is conventionally wrapped in an "expo" key, but a
// bare manifest (no wrapper) is also accepted.
func parseExpoManifest(props map[string]string, content []byte) {
	var wrapper struct {
		Expo json.RawMessage `json:"expo"`
	}
	manifest := content
	if json.Unmarshal(content, &wrapper) == nil && len(wrapper.Expo) > 0 {
		manifest = wrapper.Expo
	}
	var expo struct {
		Name    string          `json:"name"`
		Slug    string          `json:"slug"`
		Version string          `json:"version"`
		Scheme  json.RawMessage `json:"scheme"`
		Plugins json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(manifest, &expo); err != nil {
		return
	}
	if expo.Name != "" {
		props["expo_name"] = expo.Name
	}
	if expo.Slug != "" {
		props["expo_slug"] = expo.Slug
	}
	if expo.Version != "" {
		props["expo_version"] = expo.Version
	}
	if scheme := decodeStringOrArray(expo.Scheme); len(scheme) > 0 {
		sort.Strings(scheme)
		props["expo_scheme"] = capJoin(scheme)
	}
	if plugins := decodeExpoPlugins(expo.Plugins); len(plugins) > 0 {
		sort.Strings(plugins)
		props["expo_plugins"] = capJoin(plugins)
	}
}

// decodeStringOrArray accepts either "x" or ["x","y"] and returns the values.
func decodeStringOrArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	return nil
}

// decodeExpoPlugins accepts the Expo "plugins" array, whose members are either
// a bare plugin name ("expo-router") or a [name, config] tuple. The plugin
// name (first element / scalar) is the stable signal.
func decodeExpoPlugins(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var names []string
	for _, item := range items {
		var name string
		if json.Unmarshal(item, &name) == nil && name != "" {
			names = append(names, name)
			continue
		}
		var tuple []json.RawMessage
		if json.Unmarshal(item, &tuple) == nil && len(tuple) > 0 {
			if json.Unmarshal(tuple[0], &name) == nil && name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func parseMobileJS(props map[string]string, spec configSpec, content []byte) {
	parseJSConfig(props, content) // keep top-level export hints
	src := string(content)

	switch spec.subtype {
	case "metro_config":
		var sections []string
		for _, m := range metroSectionRE.FindAllStringSubmatch(src, -1) {
			sections = append(sections, m[1])
		}
		sort.Strings(sections)
		sections = dedup(sections)
		if len(sections) > 0 {
			props["metro_keys"] = capJoin(sections)
		}
	case "react_native_config":
		// react-native.config.js declares autolinked native modules under
		// `dependencies: { 'pkg': { ... } }`. We mine the dependency package
		// names (the native-module link signal) and note project platforms.
		var mods []string
		if block := sliceObjectValue(src, "dependencies"); block != "" {
			for _, k := range topLevelObjectKeys(block) {
				mods = append(mods, strings.Trim(k, `'"`))
			}
		}
		sort.Strings(mods)
		mods = dedup(mods)
		if len(mods) > 0 {
			props["native_modules"] = capJoin(mods)
		}
		var platforms []string
		for _, p := range []string{"ios", "android"} {
			if strings.Contains(src, p+":") || strings.Contains(src, `"`+p+`"`) || strings.Contains(src, "'"+p+"'") {
				platforms = append(platforms, p)
			}
		}
		if len(platforms) > 0 {
			props["rn_platforms"] = capJoin(platforms)
		}
	case "expo_config":
		// app.config.{js,ts} returns the manifest from JS. Mine the manifest
		// fields with permissive regexes (no JS evaluation).
		if name := jsStringField(src, "name"); name != "" {
			props["expo_name"] = name
		}
		if slug := jsStringField(src, "slug"); slug != "" {
			props["expo_slug"] = slug
		}
		if scheme := jsStringField(src, "scheme"); scheme != "" {
			props["expo_scheme"] = scheme
		}
		var plugins []string
		if block := sliceArrayValue(src, "plugins"); block != "" {
			for _, sm := range bundlerStringRE.FindAllStringSubmatch(block, -1) {
				plugins = append(plugins, sm[1])
			}
		}
		sort.Strings(plugins)
		plugins = dedup(plugins)
		if len(plugins) > 0 {
			props["expo_plugins"] = capJoin(plugins)
		}
	}
}

// objectKeyRE matches an object-literal key (quoted or bare) followed by a
// colon: `'react-native-foo':` or `myModule:`.
var objectKeyRE = regexp.MustCompile(`(['"][^'"]+['"]|[A-Za-z_$][\w$.\-]*)\s*:`)

// topLevelObjectKeys returns the depth-1 keys of the object literal `block`
// (which must start with `{`). Nested object/array keys are skipped so that a
// `dependencies: { 'pkg': { platforms: { ios: null } } }` block yields only
// "pkg", not the nested "platforms"/"ios" keys. Permissive; strings are
// skipped so a colon inside a string value is not mistaken for a key.
func topLevelObjectKeys(block string) []string {
	var keys []string
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(block); i++ {
		c := block[i]
		if inStr != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		// At depth 1 (immediately inside the outer object), a key is the token
		// preceding a colon. Match it before the quote-as-string-start branch
		// so quoted keys ('react-native-foo':) are captured, not treated as a
		// bare string value.
		if depth == 1 && (c == '"' || c == '\'' || isKeyStart(c)) {
			if m := objectKeyRE.FindStringSubmatchIndex(block[i:]); m != nil && m[0] == 0 {
				keys = append(keys, block[i+m[2]:i+m[3]])
				i += m[1] - 1
				continue
			}
		}
		switch c {
		case '"', '\'', '`':
			inStr = c
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return keys
}

func isKeyStart(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// jsStringField extracts the first `key: 'value'` / `key: "value"` scalar for
// the given key from a JS source string. Returns "" when absent or non-scalar.
func jsStringField(src, key string) string {
	re := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(key) + `\s*:\s*['"]([^'"]+)['"]`)
	if m := re.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return ""
}

// sliceObjectValue returns the brace-balanced object literal that follows
// `key:` in src, or "" if not found. Permissive — handles nested braces.
func sliceObjectValue(src, key string) string {
	return sliceBalanced(src, key, '{', '}')
}

// sliceArrayValue returns the bracket-balanced array literal that follows
// `key:` in src, or "" if not found.
func sliceArrayValue(src, key string) string {
	return sliceBalanced(src, key, '[', ']')
}

func sliceBalanced(src, key string, open, close byte) string {
	keyRE := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(key) + `\s*:\s*`)
	loc := keyRE.FindStringIndex(src)
	if loc == nil {
		return ""
	}
	i := loc[1]
	for i < len(src) && src[i] != open {
		if src[i] != ' ' && src[i] != '\t' && src[i] != '\n' && src[i] != '\r' {
			return "" // value is not the expected literal type
		}
		i++
	}
	if i >= len(src) {
		return ""
	}
	depth := 0
	start := i
	for ; i < len(src); i++ {
		switch src[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	return ""
}

type yamlBlock struct {
	key   string
	items []string
}

// splitYAMLBlocks groups top-level `key:` headers with their following
// indented `- item` list members. Permissive — good enough for config mining.
func splitYAMLBlocks(src string) []yamlBlock {
	var blocks []yamlBlock
	lines := strings.Split(src, "\n")
	headerRE := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.\-]*)\s*:\s*$`)
	for i := 0; i < len(lines); i++ {
		hm := headerRE.FindStringSubmatch(lines[i])
		if hm == nil {
			continue
		}
		blk := yamlBlock{key: hm[1]}
		for j := i + 1; j < len(lines); j++ {
			im := yamlListItemRE.FindStringSubmatch(lines[j])
			if im == nil {
				break
			}
			blk.items = append(blk.items, strings.TrimSpace(im[1]))
		}
		if len(blk.items) > 0 {
			blocks = append(blocks, blk)
		}
	}
	return blocks
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func joinSortedKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return capJoin(keys)
}

// capJoin truncates the slice to maxKeysPerProperty and joins with a comma.
// When truncation fires, a trailing "+N more" marker is appended so consumers
// can see that the bag was capped without having to count.
func capJoin(values []string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) <= maxKeysPerProperty {
		return strings.Join(dedup(values), ",")
	}
	head := values[:maxKeysPerProperty]
	more := len(values) - maxKeysPerProperty
	return fmt.Sprintf("%s,+%d more", strings.Join(head, ","), more)
}

// dedup removes duplicates while preserving order.
func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func sortEntities(es []types.EntityRecord) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].SourceFile != es[j].SourceFile {
			return es[i].SourceFile < es[j].SourceFile
		}
		return es[i].QualifiedName < es[j].QualifiedName
	})
}

func sortRels(rs []types.RelationshipRecord) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].FromID != rs[j].FromID {
			return rs[i].FromID < rs[j].FromID
		}
		if rs[i].ToID != rs[j].ToID {
			return rs[i].ToID < rs[j].ToID
		}
		return rs[i].Kind < rs[j].Kind
	})
}
