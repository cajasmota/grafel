// Package manifest implements the cross-language package manifest extractor.
//
// Parses package manifest files and emits external dependency entities with
// DEPENDS_ON(kind=external_dependency) relationships.
//
// Supported manifest formats:
//   - package.json         (npm)
//   - go.mod               (go_modules)
//   - Cargo.toml           (cargo)
//   - pyproject.toml       (pip/poetry)
//   - uv.lock              (uv lockfile)
//   - pdm.lock             (pdm lockfile)
//   - poetry.lock          (poetry lockfile)
//   - Pipfile              (pipenv manifest)
//   - Pipfile.lock         (pipenv lockfile)
//   - pom.xml              (maven)
//   - requirements.txt     (pip)
//   - pubspec.yaml         (pub/dart)
//   - Gemfile              (bundler/ruby)
//
// Entity kind: "SCOPE.Component"
// Relationships emitted: DEPENDS_ON(kind=external_dependency)
//
// OTel span: indexer.manifest_extractor.extract
// Attributes: file_path, package_manager, dependencies_found,
//
//	dev_dependencies_found, total_found
//
// Registration key: "_cross_manifest"
package manifest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("_cross_manifest", &Extractor{})
}

// Extractor parses package manifest files.
type Extractor struct{}

// Language returns the registration key.
func (e *Extractor) Language() string { return "_cross_manifest" }

// ---------------------------------------------------------------------------
// dep is a single parsed dependency entry.
// ---------------------------------------------------------------------------

type dep struct {
	name    string
	version string
	isDev   bool
	// kind is "runtime", "dev", "peer", "locked", or (go.mod) "indirect"
	kind string
	// indirect marks a transitive dependency. For go.mod this is the
	// `// indirect` marker on a require line; surfaced via the
	// "indirect" entity/edge property so direct and transitive deps are
	// distinguishable (go.mod lockfile_parsing, #3217).
	indirect bool
}

// ---------------------------------------------------------------------------
// Ref builders
// ---------------------------------------------------------------------------

func pkgRef(packageManager, packageName string) string {
	return fmt.Sprintf("scope:component:external_dep:%s:%s", packageManager, packageName)
}

func projectRef(filePath string) string {
	return "scope:component:project:" + filePath
}

// ---------------------------------------------------------------------------
// Manifest detection
// ---------------------------------------------------------------------------

// exactManifestNames lists supported manifest basenames (case-sensitive).
//
// #2865 — npm/yarn/pnpm lockfiles are recognised here too: lockfiles record
// the FULLY RESOLVED dependency tree (exact versions, including transitive
// deps the manifest never names), which manifest_parsing alone cannot
// recover. Lockfile-sourced deps are flagged dependency_kind=locked and
// carry resolved=true so downstream queries can distinguish the resolved
// tree from the declared (range-versioned) manifest deps.
var exactManifestNames = map[string]bool{
	"package.json":        true,
	"package-lock.json":   true,
	"npm-shrinkwrap.json": true,
	"yarn.lock":           true,
	"pnpm-lock.yaml":      true,
	"go.mod":              true,
	"Cargo.toml":          true,
	"pyproject.toml":      true,
	"uv.lock":             true,
	"pdm.lock":            true,
	"poetry.lock":         true,
	"Pipfile":             true,
	"Pipfile.lock":        true,
	"pom.xml":             true,
	"requirements.txt":    true,
	"pubspec.yaml":        true,
	"Gemfile":             true,
}

// IsManifest returns true when filePath names a supported manifest file.
func IsManifest(filePath string) bool {
	basename := filepath.Base(filePath)
	return exactManifestNames[basename]
}

// detectPackageManager returns the package manager for a manifest path.
func detectPackageManager(filePath string) string {
	pm := map[string]string{
		"package.json":        "npm",
		"package-lock.json":   "npm",
		"npm-shrinkwrap.json": "npm",
		"yarn.lock":           "yarn",
		"pnpm-lock.yaml":      "pnpm",
		"go.mod":              "go_modules",
		"Cargo.toml":          "cargo",
		"pyproject.toml":      "pip",
		"uv.lock":             "uv",
		"pdm.lock":            "pdm",
		"poetry.lock":         "poetry",
		"Pipfile":             "pipenv",
		"Pipfile.lock":        "pipenv",
		"pom.xml":             "maven",
		"requirements.txt":    "pip",
		"pubspec.yaml":        "pub",
		"Gemfile":             "bundler",
	}
	if v, ok := pm[filepath.Base(filePath)]; ok {
		return v
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// Parser: package.json
// ---------------------------------------------------------------------------

func parsePackageJSON(source string) []dep {
	var data struct {
		Dependencies     map[string]string `json:"dependencies"`
		DevDependencies  map[string]string `json:"devDependencies"`
		PeerDependencies map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}
	var out []dep
	for pkg, ver := range data.Dependencies {
		out = append(out, dep{name: pkg, version: ver, isDev: false, kind: "runtime"})
	}
	for pkg, ver := range data.DevDependencies {
		out = append(out, dep{name: pkg, version: ver, isDev: true, kind: "dev"})
	}
	for pkg, ver := range data.PeerDependencies {
		out = append(out, dep{name: pkg, version: ver, isDev: false, kind: "peer"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: package-lock.json / npm-shrinkwrap.json (npm lockfile)
// ---------------------------------------------------------------------------

// parsePackageLockJSON parses an npm lockfile (lockfileVersion 1, 2, or 3).
//
//	v2/v3: every resolved package lives under "packages" keyed by its
//	       install path ("" = root project, "node_modules/<name>" =
//	       dependency, possibly nested). We strip the leading
//	       node_modules/ segments to recover the package name.
//	v1:    resolved packages live under "dependencies" keyed by name, with
//	       a recursive nested "dependencies" map.
//
// Lockfile deps record the EXACT resolved version and include the transitive
// closure the manifest never names — that is the whole point of
// lockfile_parsing. Each emitted dep is flagged kind="locked" with isDev
// carried through when the lockfile marks it.
func parsePackageLockJSON(source string) []dep {
	var data struct {
		Packages map[string]struct {
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}

	var out []dep
	seen := map[string]bool{}
	add := func(name, version string, isDev bool) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: "locked"})
	}

	// v2/v3 "packages" map. Keys are install paths; the package name is the
	// segment after the LAST "node_modules/" (handles nested deps like
	// "node_modules/a/node_modules/b" -> "b"). The "" key is the root project.
	for path, p := range data.Packages {
		if path == "" {
			continue
		}
		name := path
		if idx := strings.LastIndex(name, "node_modules/"); idx >= 0 {
			name = name[idx+len("node_modules/"):]
		}
		add(name, p.Version, p.Dev)
	}

	// v1 "dependencies" map. Keys are names directly.
	for name, d := range data.Dependencies {
		add(name, d.Version, d.Dev)
	}

	return out
}

// ---------------------------------------------------------------------------
// Parser: yarn.lock (Yarn classic v1 + Yarn berry v2+)
// ---------------------------------------------------------------------------

// yarnEntryHeaderRE matches a yarn.lock entry header line, e.g.
//
//	"lodash@^4.17.21":
//	lodash@^4.17.21, lodash@^4.17.0:
//	"@babel/core@npm:^7.0.0":
//
// Group 1 is the first descriptor (the part before the first range/comma).
// We derive the package name from it; the resolved version comes from the
// `version` line in the entry body.
var yarnEntryHeaderRE = regexp.MustCompile(`(?m)^ {0,2}"?([^"\s,][^",]*?)"?(?:,.*)?:\s*$`)

// yarnVersionRE matches the `version "1.2.3"` (v1) or `version: 1.2.3` (berry)
// line inside an entry body.
var yarnVersionRE = regexp.MustCompile(`^\s+version:?\s+"?([^"\n\r]+?)"?\s*$`)

// yarnDescriptorName extracts the bare package name from a yarn descriptor
// such as `lodash@^4.17.21`, `@babel/core@npm:^7.0.0`, or `@scope/pkg@1.0.0`.
// The version range is everything after the LAST `@` that is not the leading
// scope `@`.
func yarnDescriptorName(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	at := strings.LastIndex(desc, "@")
	// A leading "@" denotes a scope, not a version separator.
	if at <= 0 {
		return desc
	}
	return desc[:at]
}

func parseYarnLock(source string) []dep {
	lines := strings.Split(source, "\n")
	var out []dep
	seen := map[string]bool{}

	for i := 0; i < len(lines); i++ {
		hm := yarnEntryHeaderRE.FindStringSubmatch(lines[i])
		if hm == nil {
			continue
		}
		// The header may list several descriptors separated by ", "; the name
		// is the same for all of them, so the first descriptor suffices.
		firstDesc := hm[1]
		if c := strings.IndexByte(firstDesc, ','); c >= 0 {
			firstDesc = firstDesc[:c]
		}
		name := yarnDescriptorName(firstDesc)
		if name == "" || seen[name] {
			continue
		}
		// Scan the entry body (subsequent indented lines) for the version.
		version := ""
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			// A non-indented line starts the next entry.
			if lines[j][0] != ' ' && lines[j][0] != '\t' {
				break
			}
			if vm := yarnVersionRE.FindStringSubmatch(lines[j]); vm != nil {
				version = strings.TrimSpace(vm[1])
				break
			}
		}
		seen[name] = true
		out = append(out, dep{name: name, version: version, kind: "locked"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: pnpm-lock.yaml
// ---------------------------------------------------------------------------

// pnpmPackageKeyRE matches a package key under the top-level `packages:` map.
// pnpm uses two key shapes across lockfile versions:
//
//	v5/v6:  /lodash@4.17.21:  or  /@babel/core@7.0.0:
//	v9:     lodash@4.17.21:        @babel/core@7.0.0:
//
// Group 1 is the package path (with optional leading slash stripped by the
// caller). Peer-dependency suffixes (e.g. `@7.0.0(react@18.0.0)`) are trimmed.
var pnpmPackageKeyRE = regexp.MustCompile(`(?m)^ {2}(/?(?:@[^/@\s]+/)?[^@/\s][^@\s]*@[^:\s]+):`)

func parsePnpmLock(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	// Restrict scanning to the top-level `packages:` block so we don't match
	// the `dependencies:`/`importers:` descriptor sections (those use ranges,
	// not resolved versions).
	body := source
	if idx := strings.Index(source, "\npackages:"); idx >= 0 {
		body = source[idx:]
	}

	for _, m := range pnpmPackageKeyRE.FindAllStringSubmatch(body, -1) {
		key := strings.TrimPrefix(m[1], "/")
		// Strip a peer-dependency suffix: "@scope/pkg@1.0.0(react@18.0.0)".
		if p := strings.IndexByte(key, '('); p >= 0 {
			key = key[:p]
		}
		// Split on the LAST "@" that is not the leading scope marker.
		at := strings.LastIndex(key, "@")
		if at <= 0 {
			continue
		}
		name := key[:at]
		version := key[at+1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dep{name: name, version: version, kind: "locked"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: go.mod
// ---------------------------------------------------------------------------

var goRequireBlockRE = regexp.MustCompile(`(?s)require\s*\(([^)]+)\)`)

// goRequireSingleRE matches `require module version [// indirect]` on its own
// line, ensuring the token after require is not `(` (which starts a block).
// Group 3 captures the trailing `// indirect` marker when present.
var goRequireSingleRE = regexp.MustCompile(`(?m)^require\s+([^(\s]\S*)\s+(v\S+)[ \t]*(//\s*indirect)?`)

// goPkgLineRE matches a single dependency line inside a require(...) block.
// Group 3 captures the trailing `// indirect` marker when present, so direct
// and transitive (indirect) dependencies can be distinguished — the core of
// go.mod lockfile-style dependency tracking (#3217).
var goPkgLineRE = regexp.MustCompile(`(?m)^[ \t]+(\S+)\s+(v\S+)[ \t]*(//\s*indirect)?`)

func parseGoMod(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	emit := func(name, version string, indirect bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		kind := "runtime"
		if indirect {
			kind = "indirect"
		}
		out = append(out, dep{name: name, version: version, isDev: false, kind: kind, indirect: indirect})
	}

	for _, bm := range goRequireBlockRE.FindAllStringSubmatch(source, -1) {
		block := bm[1]
		for _, lm := range goPkgLineRE.FindAllStringSubmatch(block, -1) {
			emit(lm[1], lm[2], lm[3] != "")
		}
	}
	for _, m := range goRequireSingleRE.FindAllStringSubmatch(source, -1) {
		emit(m[1], m[2], m[3] != "")
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: Cargo.toml
// ---------------------------------------------------------------------------

var cargoSectionRE = regexp.MustCompile(`(?m)^\[([^\]]+)\]`)
var cargoDepLineRE = regexp.MustCompile(
	`(?m)^\s*([A-Za-z0-9_-]+)\s*=\s*(?:"([^"]*)"|\{[^}]*version\s*=\s*"([^"]*)"[^}]*\})`,
)

func parseCargoToml(source string) []dep {
	// Find section boundaries.
	sectionMatches := cargoSectionRE.FindAllStringSubmatchIndex(source, -1)
	type sectionEntry struct {
		start int
		name  string
	}
	sections := make([]sectionEntry, 0, len(sectionMatches))
	for _, m := range sectionMatches {
		sections = append(sections, sectionEntry{
			start: m[0],
			name:  strings.TrimSpace(source[m[2]:m[3]]),
		})
	}
	sections = append(sections, sectionEntry{start: len(source), name: "__end__"})

	bodyFor := func(name string) string {
		for i, s := range sections[:len(sections)-1] {
			if s.name == name {
				return source[s.start:sections[i+1].start]
			}
		}
		return ""
	}

	parseBody := func(body string, isDev bool, depKind string) []dep {
		var out []dep
		for _, m := range cargoDepLineRE.FindAllStringSubmatch(body, -1) {
			name := m[1]
			version := m[2]
			if version == "" {
				version = m[3]
			}
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
		}
		return out
	}

	var out []dep
	out = append(out, parseBody(bodyFor("dependencies"), false, "runtime")...)
	out = append(out, parseBody(bodyFor("dev-dependencies"), true, "dev")...)
	out = append(out, parseBody(bodyFor("build-dependencies"), false, "runtime")...)
	return out
}

// ---------------------------------------------------------------------------
// Parser: pyproject.toml
// ---------------------------------------------------------------------------

var tomlSectionRE = regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]`)
var pyProjectDepListRE = regexp.MustCompile(`(?s)dependencies\s*=\s*\[([^\]]*)\]`)
var pyDepNameRE = regexp.MustCompile(
	`^([A-Za-z0-9](?:[A-Za-z0-9._-]*)?)(?:\[[^\]]*\])?(?:\s*([^#\n]*))?`,
)
var tomlDepLineRE = regexp.MustCompile(
	`(?m)^\s*"?([A-Za-z0-9](?:[A-Za-z0-9._-]*)?)"?\s*(?:=\s*["{\[](?:([^"}\n]*))?)?`,
)

func parsePyprojectToml(source string) []dep {
	// Build section index.
	type sectionEntry struct {
		start int
		name  string
	}
	sectionMatches := tomlSectionRE.FindAllStringSubmatchIndex(source, -1)
	sections := make([]sectionEntry, 0, len(sectionMatches))
	for _, m := range sectionMatches {
		sections = append(sections, sectionEntry{
			start: m[0],
			name:  strings.TrimSpace(source[m[2]:m[3]]),
		})
	}
	sections = append(sections, sectionEntry{start: len(source), name: "__end__"})

	bodyFor := func(name string) string {
		for i, s := range sections[:len(sections)-1] {
			if s.name == name {
				return source[s.start:sections[i+1].start]
			}
		}
		return ""
	}

	extractDeps := func(body string, isDev bool, depKind string) []dep {
		var out []dep
		listMatch := pyProjectDepListRE.FindStringSubmatch(body)
		if listMatch != nil {
			for _, item := range strings.Split(listMatch[1], ",") {
				// Strip leading/trailing whitespace and quote characters.
				item = strings.TrimSpace(item)
				item = strings.Trim(item, `"'`)
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				m := pyDepNameRE.FindStringSubmatch(item)
				if m == nil {
					continue
				}
				name := m[1]
				version := strings.TrimSpace(m[2])
				if name != "" && name != "python" {
					out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
				}
			}
			return out
		}
		// Poetry-style key = "version"
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
				continue
			}
			m := tomlDepLineRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := m[1]
			version := strings.TrimSpace(m[2])
			skip := map[string]bool{
				"python": true, "true": true, "false": true,
				"path": true, "extras": true,
			}
			if skip[name] {
				continue
			}
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
		}
		return out
	}

	var out []dep
	seen := map[string]bool{}
	addDeps := func(deps []dep) {
		for _, d := range deps {
			if !seen[d.name] {
				seen[d.name] = true
				out = append(out, d)
			}
		}
	}

	addDeps(extractDeps(bodyFor("project"), false, "runtime"))
	addDeps(extractDeps(bodyFor("tool.poetry.dependencies"), false, "runtime"))
	addDeps(extractDeps(bodyFor("tool.poetry.dev-dependencies"), true, "dev"))
	for _, s := range sections[:len(sections)-1] {
		if strings.HasPrefix(s.name, "tool.poetry.group.") && strings.HasSuffix(s.name, ".dependencies") {
			parts := strings.Split(s.name, ".")
			groupName := ""
			if len(parts) >= 4 {
				groupName = parts[3]
			}
			isDev := groupName == "dev" || groupName == "test" || groupName == "docs" || groupName == "lint"
			depKind := "runtime"
			if isDev {
				depKind = "dev"
			}
			addDeps(extractDeps(bodyFor(s.name), isDev, depKind))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: pom.xml
// ---------------------------------------------------------------------------

// pomProject is a minimal Maven POM structure for XML unmarshalling.
type pomProject struct {
	XMLName      xml.Name        `xml:"project"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

func parsePomXML(source string) []dep {
	// Strip XML namespace to simplify unmarshalling.
	cleaned := regexp.MustCompile(`<project[^>]*>`).
		ReplaceAllString(source, "<project>")

	var pom pomProject
	if err := xml.Unmarshal([]byte(cleaned), &pom); err != nil {
		return nil
	}

	var out []dep
	for _, d := range pom.Dependencies {
		if d.ArtifactID == "" {
			continue
		}
		name := d.ArtifactID
		if d.GroupID != "" {
			name = d.GroupID + ":" + d.ArtifactID
		}
		scope := d.Scope
		if scope == "" {
			scope = "compile"
		}
		isDev := scope == "test" || scope == "provided"
		depKind := "runtime"
		if isDev {
			depKind = "dev"
		}
		out = append(out, dep{name: name, version: d.Version, isDev: isDev, kind: depKind})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: requirements.txt
// ---------------------------------------------------------------------------

// reqLineRE matches "package[extras]>=version" in requirements.txt.
var reqLineRE = regexp.MustCompile(`(?m)^([A-Za-z0-9](?:[A-Za-z0-9._-]*)?)(?:\[[^\]]*\])?(?:\s*([^#\n]*))?`)

func parseRequirementsTxt(source string) []dep {
	var out []dep
	seen := map[string]bool{}
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		m := reqLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		version := strings.TrimSpace(m[2])
		out = append(out, dep{name: name, version: version, isDev: false, kind: "runtime"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: pubspec.yaml (Dart/Flutter)
// ---------------------------------------------------------------------------

var pubspecSectionRE = regexp.MustCompile(`(?m)^([a-z_]+):\s*$`)
var pubspecDepLineRE = regexp.MustCompile(`(?m)^\s{2}([A-Za-z0-9_-]+)\s*:\s*(.*)$`)

func parsePubspecYaml(source string) []dep {
	// Find section positions (top-level keys followed by newline).
	sectionMatches := pubspecSectionRE.FindAllStringSubmatchIndex(source, -1)
	type sectionEntry struct {
		start int
		name  string
	}
	sections := make([]sectionEntry, 0, len(sectionMatches))
	for _, m := range sectionMatches {
		sections = append(sections, sectionEntry{
			start: m[0],
			name:  source[m[2]:m[3]],
		})
	}
	sections = append(sections, sectionEntry{start: len(source), name: "__end__"})

	bodyFor := func(name string) string {
		for i, s := range sections[:len(sections)-1] {
			if s.name == name {
				return source[s.start:sections[i+1].start]
			}
		}
		return ""
	}

	extractDeps := func(body string, isDev bool, depKind string) []dep {
		var out []dep
		for _, m := range pubspecDepLineRE.FindAllStringSubmatch(body, -1) {
			name := strings.TrimSpace(m[1])
			if name == "" {
				continue
			}
			version := strings.TrimSpace(m[2])
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
		}
		return out
	}

	var out []dep
	out = append(out, extractDeps(bodyFor("dependencies"), false, "runtime")...)
	out = append(out, extractDeps(bodyFor("dev_dependencies"), true, "dev")...)
	return out
}

// ---------------------------------------------------------------------------
// Parser: Gemfile (Ruby/Bundler)
// ---------------------------------------------------------------------------

// gemLineRE matches `gem 'name'` or `gem "name", '~> 1.0'`.
var gemLineRE = regexp.MustCompile(`(?m)^\s*gem\s+['"]([A-Za-z0-9_-]+)['"](?:\s*,\s*['"]([^'"]+)['"])?`)
var gemGroupRE = regexp.MustCompile(`(?m)^\s*group\s+(.*?)\s+do\s*$`)

func parseGemfile(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	// Find group blocks so we can flag dev/test gems.
	type groupBlock struct {
		start int
		end   int
		isDev bool
	}
	var groups []groupBlock
	groupMatches := gemGroupRE.FindAllStringSubmatchIndex(source, -1)
	for _, gm := range groupMatches {
		groupLabel := strings.ToLower(source[gm[2]:gm[3]])
		isDev := strings.Contains(groupLabel, "dev") || strings.Contains(groupLabel, "test")
		// Find matching "end" after this block start.
		start := gm[0]
		end := strings.Index(source[start:], "\nend")
		blockEnd := len(source)
		if end >= 0 {
			blockEnd = start + end + 4
		}
		groups = append(groups, groupBlock{start: start, end: blockEnd, isDev: isDev})
	}

	inDevGroup := func(pos int) bool {
		for _, g := range groups {
			if pos >= g.start && pos <= g.end {
				return g.isDev
			}
		}
		return false
	}

	for _, m := range gemLineRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if seen[name] {
			continue
		}
		seen[name] = true
		version := ""
		if m[4] >= 0 {
			version = source[m[4]:m[5]]
		}
		isDev := inDevGroup(m[0])
		depKind := "runtime"
		if isDev {
			depKind = "dev"
		}
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: Pipfile (Pipenv manifest)
// ---------------------------------------------------------------------------

// pipfileDepLineRE matches a pipenv dependency line in [packages] or
// [dev-packages] sections.  Both forms are supported:
//
//	requests = "*"
//	Django = ">=3.2,<4"
//	flask = {version = "^2.0", extras = ["async"]}
var pipfileDepLineRE = regexp.MustCompile(
	`(?m)^\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*)?)\s*=\s*(?:"([^"]*)"|\{[^}]*version\s*=\s*"([^"]*)"[^}]*\}|'([^']*)')`,
)

func parsePipfile(source string) []dep {
	// Build a minimal section index (same pattern as parseCargoToml).
	type sectionEntry struct {
		start int
		name  string
	}
	sectionMatches := tomlSectionRE.FindAllStringSubmatchIndex(source, -1)
	sections := make([]sectionEntry, 0, len(sectionMatches))
	for _, m := range sectionMatches {
		sections = append(sections, sectionEntry{
			start: m[0],
			name:  strings.TrimSpace(source[m[2]:m[3]]),
		})
	}
	sections = append(sections, sectionEntry{start: len(source), name: "__end__"})

	bodyFor := func(name string) string {
		for i, s := range sections[:len(sections)-1] {
			if s.name == name {
				return source[s.start:sections[i+1].start]
			}
		}
		return ""
	}

	extractDeps := func(body string, isDev bool, depKind string) []dep {
		var out []dep
		for _, m := range pipfileDepLineRE.FindAllStringSubmatch(body, -1) {
			name := m[1]
			if name == "python_version" || name == "python_full_version" {
				continue
			}
			// version is in group 2 (plain string), 3 (table version=), or 4 (single-quoted string)
			version := m[2]
			if version == "" {
				version = m[3]
			}
			if version == "" {
				version = m[4]
			}
			if version == "*" {
				version = ""
			}
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: depKind})
		}
		return out
	}

	var out []dep
	seen := map[string]bool{}
	addDeps := func(deps []dep) {
		for _, d := range deps {
			if !seen[d.name] {
				seen[d.name] = true
				out = append(out, d)
			}
		}
	}
	addDeps(extractDeps(bodyFor("packages"), false, "runtime"))
	addDeps(extractDeps(bodyFor("dev-packages"), true, "dev"))
	return out
}

// ---------------------------------------------------------------------------
// Parser: Pipfile.lock (Pipenv lockfile)
// ---------------------------------------------------------------------------

// parsePipfileLock parses a Pipfile.lock JSON document.
//
// Structure:
//
//	{
//	  "default": { "<name>": { "version": "==1.2.3" }, ... },
//	  "develop": { "<name>": { "version": "==1.2.3" }, ... }
//	}
//
// Versions are stored as PEP 440 constraints (e.g. "==1.2.3"); we strip
// the leading "==" for a cleaner display.
func parsePipfileLock(source string) []dep {
	var data struct {
		Default map[string]struct {
			Version string `json:"version"`
		} `json:"default"`
		Develop map[string]struct {
			Version string `json:"version"`
		} `json:"develop"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}

	var out []dep
	seen := map[string]bool{}
	add := func(name, version string, isDev bool) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		// Strip leading "==" from locked version specifier.
		version = strings.TrimPrefix(version, "==")
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: "locked"})
	}
	for name, pkg := range data.Default {
		add(name, pkg.Version, false)
	}
	for name, pkg := range data.Develop {
		add(name, pkg.Version, true)
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: uv.lock (uv lockfile, TOML-based)
// ---------------------------------------------------------------------------

// uvLockPackageNameRE matches a [[package]] entry name line in uv.lock.
//
//	[[package]]
//	name = "requests"
//	version = "2.31.0"
var uvLockNameRE = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
var uvLockVersionRE = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)

// parseUvLock parses a uv.lock file (TOML format, [[package]] array).
//
// Each [[package]] block lists one resolved package with name + version.
// We scan all blocks and emit them as kind=locked deps.
func parseUvLock(source string) []dep {
	// Split on [[package]] boundaries.
	blocks := strings.Split(source, "[[package]]")
	var out []dep
	seen := map[string]bool{}

	for _, block := range blocks[1:] { // skip preamble before first [[package]]
		nm := uvLockNameRE.FindStringSubmatch(block)
		if nm == nil {
			continue
		}
		name := nm[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		version := ""
		if vm := uvLockVersionRE.FindStringSubmatch(block); vm != nil {
			version = vm[1]
		}
		out = append(out, dep{name: name, version: version, kind: "locked"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: pdm.lock (PDM lockfile, TOML-based)
// ---------------------------------------------------------------------------

// parsePdmLock parses a pdm.lock file.
//
// pdm.lock uses the same [[package]] array structure as uv.lock, though
// it also includes a [metadata] block at the top.  We reuse the same
// name/version extraction approach.
func parsePdmLock(source string) []dep {
	return parseUvLock(source) // identical structure
}

// ---------------------------------------------------------------------------
// Parser: poetry.lock (Poetry lockfile, TOML-based)
// ---------------------------------------------------------------------------

// poetryLockNameRE / poetryLockVersionRE match the name and version fields
// inside a [[package]] block in poetry.lock.
var poetryLockNameRE = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
var poetryLockVersionRE = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)

// parsePoetryLock parses a poetry.lock file.
//
// poetry.lock is TOML-based with [[package]] blocks, each carrying name,
// version, and optional category ("main" vs "dev").  We emit kind=locked
// deps and flag dev when category == "dev".
func parsePoetryLock(source string) []dep {
	blocks := strings.Split(source, "[[package]]")
	var out []dep
	seen := map[string]bool{}

	categoryRE := regexp.MustCompile(`(?m)^category\s*=\s*"([^"]+)"`)

	for _, block := range blocks[1:] {
		nm := poetryLockNameRE.FindStringSubmatch(block)
		if nm == nil {
			continue
		}
		name := nm[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		version := ""
		if vm := poetryLockVersionRE.FindStringSubmatch(block); vm != nil {
			version = vm[1]
		}
		isDev := false
		if cm := categoryRE.FindStringSubmatch(block); cm != nil {
			isDev = cm[1] == "dev"
		}
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: "locked"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Dispatch table
// ---------------------------------------------------------------------------

type parserFn func(source string) []dep

var parsers = map[string]parserFn{
	"package.json":        parsePackageJSON,
	"package-lock.json":   parsePackageLockJSON,
	"npm-shrinkwrap.json": parsePackageLockJSON,
	"yarn.lock":           parseYarnLock,
	"pnpm-lock.yaml":      parsePnpmLock,
	"go.mod":              parseGoMod,
	"Cargo.toml":          parseCargoToml,
	"pyproject.toml":      parsePyprojectToml,
	"uv.lock":             parseUvLock,
	"pdm.lock":            parsePdmLock,
	"poetry.lock":         parsePoetryLock,
	"Pipfile":             parsePipfile,
	"Pipfile.lock":        parsePipfileLock,
	"pom.xml":             parsePomXML,
	"requirements.txt":    parseRequirementsTxt,
	"pubspec.yaml":        parsePubspecYaml,
	"Gemfile":             parseGemfile,
}

func dispatchParser(filePath, source string) (string, []dep) {
	basename := filepath.Base(filePath)
	if fn, ok := parsers[basename]; ok {
		pm := detectPackageManager(filePath)
		return pm, fn(source)
	}
	return "unknown", nil
}

// ---------------------------------------------------------------------------
// Entity / relationship builders
// ---------------------------------------------------------------------------

func buildEntitiesAndRels(filePath, packageManager string, deps []dep) []types.EntityRecord {
	var out []types.EntityRecord
	projRef := projectRef(filePath)
	seen := map[string]bool{}

	// Rust wave-2 (S20+) — emit a project anchor entity for the manifest
	// file so the DEPENDS_ON edges' FromID (= projRef) resolves to a real
	// in-tree entity via byQualifiedName. Without this anchor, every
	// dependency edge contributes one bug-extractor count for its FromID
	// endpoint regardless of whether the dependency itself classifies
	// (single-manifest repos drown the signal; actix-examples has 73 Cargo
	// .toml files in a workspace and these dangling FromIDs dominated its
	// bug-extractor bucket — issue tracked alongside #596 chain-fixes).
	//
	// Provenance INFERRED_FROM_PACKAGE_MANIFEST mirrors the dep records;
	// QualityScore is lower because this is a structural anchor, not a
	// language-level entity. The `ref` property is what byQualifiedName
	// keys off (resolve/refs.go line ~1662); we set QualifiedName too for
	// belt-and-braces resolution.
	out = append(out, types.EntityRecord{
		Name:          filepath.Base(filePath),
		Kind:          "SCOPE.Component",
		Subtype:       "project",
		SourceFile:    filePath,
		Language:      "",
		QualifiedName: projRef,
		Properties: map[string]string{
			"package_manager": packageManager,
			"ref":             projRef,
			"provenance":      "INFERRED_FROM_PACKAGE_MANIFEST",
		},
		QualityScore: 0.5,
	})

	for _, d := range deps {
		if seen[d.name] {
			continue
		}
		seen[d.name] = true

		pRef := pkgRef(packageManager, d.name)
		isDev := "false"
		if d.isDev {
			isDev = "true"
		}
		depKind := d.kind
		if depKind == "" {
			depKind = "runtime"
			if d.isDev {
				depKind = "dev"
			}
		}
		indirect := "false"
		if d.indirect {
			indirect = "true"
		}

		// #560: emit a single SCOPE.Component carrying the DEPENDS_ON edge
		// embedded in its Relationships, rather than a real entity plus a
		// synthetic "relationship"-kind container entity that the downstream
		// pipeline would otherwise count as a phantom entity.
		out = append(out, types.EntityRecord{
			Name:       d.name,
			Kind:       "SCOPE.Component",
			Subtype:    "external_dependency",
			SourceFile: filePath,
			Language:   "",
			Properties: map[string]string{
				"external_dependency": "true",
				"package_manager":     packageManager,
				"version":             d.version,
				"is_dev":              isDev,
				"dependency_kind":     depKind,
				"indirect":            indirect,
				"ref":                 pRef,
				"provenance":          "INFERRED_FROM_PACKAGE_MANIFEST",
			},
			Relationships: []types.RelationshipRecord{
				{
					FromID: projRef,
					ToID:   pRef,
					Kind:   "DEPENDS_ON",
					Properties: map[string]string{
						"kind":            "external_dependency",
						"package_manager": packageManager,
						"version":         d.version,
						"is_dev":          isDev,
						"dependency_kind": depKind,
						"indirect":        indirect,
					},
				},
			},
			QualityScore: 0.8,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Extract implements extractor.Extractor
// ---------------------------------------------------------------------------

// Extract parses a package manifest file and emits dependency entities.
// Returns empty result when the file is not a recognised manifest.
func (e *Extractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("extractor._cross_manifest")
	ctx, span := tracer.Start(ctx, "indexer.manifest_extractor.extract")
	defer span.End()
	_ = ctx

	span.SetAttributes(attribute.String("file_path", file.Path))

	if !IsManifest(file.Path) {
		span.SetAttributes(
			attribute.String("package_manager", "none"),
			attribute.Int("dependencies_found", 0),
			attribute.Int("dev_dependencies_found", 0),
			attribute.Int("total_found", 0),
		)
		return nil, nil
	}

	pm, deps := dispatchParser(file.Path, string(file.Content))

	depCount := 0
	devCount := 0
	for _, d := range deps {
		if d.isDev {
			devCount++
		} else {
			depCount++
		}
	}

	span.SetAttributes(
		attribute.String("package_manager", pm),
		attribute.Int("dependencies_found", depCount),
		attribute.Int("dev_dependencies_found", devCount),
		attribute.Int("total_found", depCount+devCount),
	)

	return buildEntitiesAndRels(file.Path, pm, deps), nil
}
