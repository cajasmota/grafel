// Package config implements the supplemental "config-discovery" pass that
// promotes project-level configuration files to first-class graph entities.
//
// Issue #1885 (Wave 1 tail of #1890): Module-aggregate pages cannot ground
// reference-* sections without filesystem-level entities for the files that
// actually carry deployment, build, and runtime configuration:
//
//   Python:   pyproject.toml, setup.cfg, requirements*.txt, Pipfile,
//             .flake8, mypy.ini, pytest.ini, .env, .env.*
//   Java:     pom.xml, build.gradle, build.gradle.kts,
//             application.properties, application.yml/.yaml,
//             quarkus.properties
//   JS/TS:    package.json, tsconfig.json, vite.config.*, next.config.*,
//             .eslintrc.*, .prettierrc.*, .env, .env.*
//   Go:       go.mod, Makefile
//   General:  Dockerfile, docker-compose.yml/.yaml
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
	formatTOML       configKind = "toml"
	formatJSON       configKind = "json"
	formatYAML       configKind = "yaml"
	formatXML        configKind = "xml"
	formatProperties configKind = "properties"
	formatINI        configKind = "ini"
	formatEnv        configKind = "env"
	formatDockerfile configKind = "dockerfile"
	formatMakefile   configKind = "makefile"
	formatRequirements configKind = "requirements"
	formatGradle     configKind = "gradle"
	formatGoMod      configKind = "go_mod"
	formatJSConfig   configKind = "javascript"
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
	"pyproject.toml":         {"python_project", formatTOML},
	"setup.cfg":              {"python_project_legacy", formatINI},
	"setup.py":               {"python_project_legacy", formatJSConfig}, // parsed as text
	"requirements.txt":       {"python_requirements", formatRequirements},
	"Pipfile":                {"python_pipenv", formatTOML},
	".flake8":                {"python_flake8", formatINI},
	"mypy.ini":               {"python_mypy", formatINI},
	"pytest.ini":             {"python_pytest", formatINI},
	"tox.ini":                {"python_tox", formatINI},

	"pom.xml":                {"maven_project", formatXML},
	"build.gradle":           {"gradle_project", formatGradle},
	"build.gradle.kts":       {"gradle_project", formatGradle},
	"application.properties": {"spring_properties", formatProperties},
	"application.yml":        {"spring_yaml", formatYAML},
	"application.yaml":       {"spring_yaml", formatYAML},
	"quarkus.properties":     {"quarkus_properties", formatProperties},

	"package.json":           {"node_project", formatJSON},
	"tsconfig.json":          {"typescript_project", formatJSON},

	"go.mod":                 {"go_module", formatGoMod},
	"go.sum":                 {"go_sum", formatGoMod}, // existence only

	"Makefile":               {"makefile", formatMakefile},
	"makefile":               {"makefile", formatMakefile},
	"GNUmakefile":            {"makefile", formatMakefile},

	"Dockerfile":             {"docker_image", formatDockerfile},
	"Containerfile":          {"docker_image", formatDockerfile},
	"docker-compose.yml":     {"docker_compose", formatYAML},
	"docker-compose.yaml":    {"docker_compose", formatYAML},
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
		return configSpec{"vite_config", formatJSConfig}, true
	case nextConfigRe.MatchString(base):
		return configSpec{"next_config", formatJSConfig}, true
	case eslintConfigRe.MatchString(base):
		return configSpec{"eslint_config", formatJSConfig}, true
	case prettierConfigRe.MatchString(base):
		return configSpec{"prettier_config", formatJSConfig}, true
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
	case formatJSConfig:
		return "javascript"
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
	}
}

// ---------------------------------------------------------------------------
// Per-format parsers (intentionally permissive — we only need stable signal).
// ---------------------------------------------------------------------------

func parseJSON(props map[string]string, spec configSpec, content []byte) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(content, &generic); err != nil {
		return
	}
	props["keys_top_level"] = joinSortedKeys(generic)

	// package.json is the only JSON file we deeply mine.
	if spec.subtype == "node_project" {
		var pkg struct {
			Name             string            `json:"name"`
			Version          string            `json:"version"`
			Scripts          map[string]string `json:"scripts"`
			Dependencies     map[string]string `json:"dependencies"`
			DevDependencies  map[string]string `json:"devDependencies"`
			PeerDependencies map[string]string `json:"peerDependencies"`
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
	}
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
		XMLName      xml.Name   `xml:"project"`
		GroupID      string     `xml:"groupId"`
		ArtifactID   string     `xml:"artifactId"`
		Version      string     `xml:"version"`
		Name         string     `xml:"name"`
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
