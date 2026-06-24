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
//   - *.csproj             (nuget — manifest_parsing via <PackageReference>)
//   - packages.lock.json   (nuget — lockfile_parsing via NuGet v3 lock format)
//   - CMakeLists.txt       (cmake) — find_package / target_link_libraries deps
//   - conanfile.txt        (conan) — [requires] section
//   - conanfile.py         (conan) — requires list in ConanFile class
//   - vcpkg.json           (vcpkg) — dependencies array
//   - composer.json        (composer — PHP manifest_parsing, require/require-dev)
//   - composer.lock        (composer — PHP lockfile_parsing, packages/packages-dev)
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

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	// kind is "runtime", "dev", "peer", "optional", "locked", or
	// (go.mod) "indirect"
	kind string
	// section is the package.json manifest section a declared dependency came
	// from, one of "prod", "dev", "peer", "optional" (#5526). Empty for
	// non-package.json manifests and for lockfile-derived (kind="locked") deps.
	// It feeds the External-node `dep_section` attribute and the declared/
	// dead-dependency cross-reference pass.
	section string
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
// [sbom] Converged package node (SCOPE.Package) — file/repo-agnostic
// ---------------------------------------------------------------------------
//
// Distinct from the per-manifest SCOPE.Component(external_dependency) record:
// the SCOPE.Package node is the SBOM convergence point. Its identity is
// (ecosystem, name) ONLY — version is intentionally excluded so the same
// package declared across many manifests/repos collapses to one node (the
// per-edge DEPENDS_ON_PACKAGE carries version + dev scope). This mirrors the
// SCOPE.ExternalService synthetic-SourceFile model.

// PackageSourceFile is the synthetic, constant SourceFile assigned to every
// SCOPE.Package entity so identical ecosystem:name pairs converge to a single
// graph node under EntityRecord.ComputeID (SourceFile+Kind+Name).
const PackageSourceFile = "<package>"

// packageName returns the canonical entity Name for a converged package node,
// namespaced so it never collides with a same-named code symbol, e.g.
// "package:npm:react", "package:maven:org.springframework:spring-core".
func packageName(packageManager, name string) string {
	return "package:" + packageManager + ":" + name
}

// packageTargetID returns the structural-ref ToID for a DEPENDS_ON_PACKAGE edge
// pointing at a package node. This value is ALSO the package entity's
// QualifiedName, so the resolver's byQualifiedName exact-match tier binds the
// edge without any new linker code. Constant across files and repos so the same
// dependency converges everywhere.
func packageTargetID(packageManager, name string) string {
	return "scope:package:" + packageManager + ":" + name
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
	"pubspec.lock":        true,
	"Gemfile":             true,
	"packages.lock.json":  true,
	// C++ build/package manifests
	"CMakeLists.txt": true,
	"conanfile.txt":  true,
	"conanfile.py":   true,
	"vcpkg.json":     true,
	// PHP / Composer
	"composer.json": true,
	"composer.lock": true,
	// Java / Kotlin — Gradle build scripts (Groovy DSL + Kotlin DSL)
	"build.gradle":     true,
	"build.gradle.kts": true,
	// Erlang — rebar3 (hex.pm) + erlang.mk
	"rebar.config": true,
	"rebar.lock":   true,
	"erlang.mk":    true,
	"Makefile":     true,
	// Lua — LuaRocks lockfile (the *.rockspec manifest is suffix-matched in
	// IsManifest/dispatchParser since rock names vary per package).
	"luarocks.lock": true,
	// Crystal — shards (shard.yml manifest + shard.lock lockfile)
	"shard.yml":  true,
	"shard.lock": true,
}

// IsManifest returns true when filePath names a supported manifest file.
// In addition to the exact-name set, *.csproj files (NuGet project manifests)
// are recognised by extension.
func IsManifest(filePath string) bool {
	basename := filepath.Base(filePath)
	if exactManifestNames[basename] {
		return true
	}
	// *.csproj — NuGet <PackageReference> manifest
	if strings.HasSuffix(basename, ".csproj") {
		return true
	}
	// *.app.src — Erlang/OTP application resource manifest (rebar3/hex deps
	// surface via the {applications, [...]} list).
	if strings.HasSuffix(basename, ".app.src") {
		return true
	}
	// *.rockspec — LuaRocks package manifest (rock name + version are encoded
	// in the filename, e.g. luafilesystem-1.8.0-1.rockspec, so it is matched by
	// extension rather than an exact name).
	if strings.HasSuffix(basename, ".rockspec") {
		return true
	}
	return false
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
		"pubspec.lock":        "pub",
		"Gemfile":             "bundler",
		"packages.lock.json":  "nuget",
		// C++ build/package manifests
		"CMakeLists.txt": "cmake",
		"conanfile.txt":  "conan",
		"conanfile.py":   "conan",
		"vcpkg.json":     "vcpkg",
		// PHP / Composer
		"composer.json": "composer",
		"composer.lock": "composer",
		// Java / Kotlin — Gradle
		"build.gradle":     "gradle",
		"build.gradle.kts": "gradle",
		// Erlang — rebar3 deps resolve from hex.pm
		"rebar.config": "rebar3",
		"rebar.lock":   "rebar3",
		"erlang.mk":    "erlang_mk",
		"Makefile":     "erlang_mk",
		// Lua — LuaRocks
		"luarocks.lock": "luarocks",
		// Crystal — shards
		"shard.yml":  "shards",
		"shard.lock": "shards",
	}
	basename := filepath.Base(filePath)
	if v, ok := pm[basename]; ok {
		return v
	}
	// *.csproj → nuget
	if strings.HasSuffix(basename, ".csproj") {
		return "nuget"
	}
	// *.app.src → rebar3 (Erlang/OTP application resource)
	if strings.HasSuffix(basename, ".app.src") {
		return "rebar3"
	}
	// *.rockspec → luarocks (LuaRocks package manifest)
	if strings.HasSuffix(basename, ".rockspec") {
		return "luarocks"
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// Parser: package.json
// ---------------------------------------------------------------------------

// parsePackageJSON parses all four declared-dependency sections of an npm
// package.json: dependencies (prod), devDependencies, peerDependencies, and
// optionalDependencies (#5526). Each dep carries its manifest version RANGE
// in `version` plus the originating `section` so the cross-reference pass can
// distinguish declared (range-versioned) deps from the lockfile's resolved
// tree and flag declared-but-unused (dead) dependencies.
func parsePackageJSON(source string) []dep {
	var data struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}
	var out []dep
	seen := map[string]bool{}
	add := func(pkg, ver string, isDev bool, kind, section string) {
		// First section wins on duplicate names (prod > dev > peer > optional),
		// matching npm's own precedence; keeps one declared record per package.
		if pkg == "" || seen[pkg] {
			return
		}
		seen[pkg] = true
		out = append(out, dep{name: pkg, version: ver, isDev: isDev, kind: kind, section: section})
	}
	for pkg, ver := range data.Dependencies {
		add(pkg, ver, false, "runtime", "prod")
	}
	for pkg, ver := range data.DevDependencies {
		add(pkg, ver, true, "dev", "dev")
	}
	for pkg, ver := range data.PeerDependencies {
		add(pkg, ver, false, "peer", "peer")
	}
	for pkg, ver := range data.OptionalDependencies {
		add(pkg, ver, false, "optional", "optional")
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
// Parser: pubspec.lock (Dart/Flutter lockfile)
// ---------------------------------------------------------------------------
//
// pubspec.lock is the fully-resolved dependency tree for a Dart/Flutter
// project. It is YAML with a top-level `packages:` map; each package entry
// carries a `dependency:` classifier ("direct main" / "direct dev" /
// "transitive") and a `version:` field:
//
//	packages:
//	  drift:
//	    dependency: "direct main"
//	    description: { name: drift, url: "https://pub.dev" }
//	    source: hosted
//	    version: "2.14.1"
//	  build_runner:
//	    dependency: "direct dev"
//	    source: hosted
//	    version: "2.4.6"
//	  meta:
//	    dependency: transitive
//	    source: hosted
//	    version: "1.10.0"
//
// Lockfile deps record the EXACT resolved version and include the transitive
// closure pubspec.yaml never names — that is the point of lockfile_parsing.
// Each emitted dep is flagged kind="locked"; dev classification follows the
// `dependency:` line, and transitive entries are marked indirect=true.
var pubspecLockPkgRE = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)
var pubspecLockDepKindRE = regexp.MustCompile(`(?m)^\s+dependency:\s*"?([^"\n]+?)"?\s*$`)
var pubspecLockVersionRE = regexp.MustCompile(`(?m)^\s+version:\s*"?([^"\n]+?)"?\s*$`)

func parsePubspecLock(source string) []dep {
	// Restrict scanning to the `packages:` block so the trailing `sdks:` map
	// (which lists the Dart/Flutter SDK constraint, not a package) is excluded.
	body := source
	if idx := strings.Index(source, "\npackages:"); idx >= 0 {
		body = source[idx+len("\npackages:"):]
	} else if strings.HasPrefix(source, "packages:") {
		body = source[len("packages:"):]
	}
	// Stop at the next top-level key (a line beginning in column 0).
	if end := regexp.MustCompile(`(?m)^[A-Za-z]`).FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}

	pkgIdx := pubspecLockPkgRE.FindAllStringSubmatchIndex(body, -1)
	var out []dep
	seen := map[string]bool{}
	for i, m := range pkgIdx {
		name := body[m[2]:m[3]]
		if seen[name] {
			continue
		}
		seen[name] = true
		// The entry body runs from this match to the next package header.
		blockEnd := len(body)
		if i+1 < len(pkgIdx) {
			blockEnd = pkgIdx[i+1][0]
		}
		block := body[m[1]:blockEnd]

		isDev := false
		indirect := false
		if dm := pubspecLockDepKindRE.FindStringSubmatch(block); dm != nil {
			kind := strings.TrimSpace(dm[1])
			isDev = strings.Contains(kind, "dev")
			indirect = strings.HasPrefix(kind, "transitive")
		}
		version := ""
		if vm := pubspecLockVersionRE.FindStringSubmatch(block); vm != nil {
			version = strings.TrimSpace(vm[1])
		}
		out = append(out, dep{
			name:     name,
			version:  version,
			isDev:    isDev,
			kind:     "locked",
			indirect: indirect,
		})
	}
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
// Parser: CMakeLists.txt
// ---------------------------------------------------------------------------

// cmakeFindPackageRE matches find_package(PkgName [REQUIRED] [...]) calls.
// Captures: (1) package name.
var cmakeFindPackageRE = regexp.MustCompile(
	`(?im)\bfind_package\s*\(\s*([A-Za-z0-9_][A-Za-z0-9_.-]*)`)

// cmakeTargetLinkRE matches target_link_libraries(target [scope] dep ...) calls.
// We capture each dep token following the target and optional scope keyword.
// Captures: (1) target name, (2) remainder of the argument list.
var cmakeTargetLinkRE = regexp.MustCompile(
	`(?im)\btarget_link_libraries\s*\(\s*([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*((?:[^)]|\n)*)\)`)

// cmakeScopeTokenRE matches PUBLIC/PRIVATE/INTERFACE scope keywords (stripped).
var cmakeScopeTokenRE = regexp.MustCompile(`(?i)^(?:PUBLIC|PRIVATE|INTERFACE)$`)

func parseCMakeLists(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	add := func(name, kind string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, dep{name: name, kind: kind})
	}

	// find_package() → runtime dep
	for _, m := range cmakeFindPackageRE.FindAllStringSubmatch(source, -1) {
		add(m[1], "runtime")
	}

	// target_link_libraries() → runtime deps (skip scope keywords and generator expressions)
	for _, m := range cmakeTargetLinkRE.FindAllStringSubmatch(source, -1) {
		argBlock := m[2]
		for _, tok := range strings.Fields(argBlock) {
			tok = strings.TrimSpace(tok)
			// Skip scope keywords
			if cmakeScopeTokenRE.MatchString(tok) {
				continue
			}
			// Skip CMake generator expressions $<...>
			if strings.HasPrefix(tok, "$<") {
				continue
			}
			// Skip cmake variables ${...}
			if strings.HasPrefix(tok, "${") {
				continue
			}
			// Skip empty or purely numeric tokens
			if tok == "" {
				continue
			}
			add(tok, "runtime")
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Parser: conanfile.txt
// ---------------------------------------------------------------------------

// conanTxtRequiresRE matches the [requires] section in a conanfile.txt.
// Lines under [requires] look like:  boost/1.79.0   or  zlib/1.2.13
var conanTxtSectionRE = regexp.MustCompile(`(?m)^\[([a-z_]+)\]`)
var conanTxtDepLineRE = regexp.MustCompile(`(?m)^([A-Za-z0-9_][A-Za-z0-9_.-]*)(?:/([^\s#\n]*))?`)

func parseConanfileTxt(source string) []dep {
	// Build a section index.
	sectionMatches := conanTxtSectionRE.FindAllStringSubmatchIndex(source, -1)
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

	var out []dep
	seen := map[string]bool{}

	parseDeps := func(body string, isDev bool, kind string) {
		for _, m := range conanTxtDepLineRE.FindAllStringSubmatch(body, -1) {
			name := m[1]
			// Skip lines that are section headers themselves
			if name == "" || strings.HasPrefix(name, "[") {
				continue
			}
			version := ""
			if len(m) > 2 {
				version = m[2]
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
		}
	}

	parseDeps(bodyFor("requires"), false, "runtime")
	parseDeps(bodyFor("build_requires"), false, "runtime")
	parseDeps(bodyFor("test_requires"), true, "dev")
	return out
}

// ---------------------------------------------------------------------------
// Parser: conanfile.py
// ---------------------------------------------------------------------------

// conanPyRequiresRE matches Python list/tuple/string requires in a ConanFile:
//
//	requires = "boost/1.79.0", "zlib/1.2.13"
//	requires = ("boost/1.79.0", "zlib/1.2.13")
//	requires = ["boost/1.79.0"]
var conanPyDepRE = regexp.MustCompile(`"([A-Za-z0-9_][A-Za-z0-9_.-]*)(?:/([^"]*))?"`)

// conanPyRequiresBlockRE captures the content assigned to requires / build_requires.
var conanPyRequiresBlockRE = regexp.MustCompile(
	`(?m)^\s*(requires|build_requires|test_requires)\s*=\s*([^\n]+(?:\n\s+[^\n]+)*)`)

func parseConanfilePy(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	for _, bm := range conanPyRequiresBlockRE.FindAllStringSubmatch(source, -1) {
		attrName := bm[1]
		block := bm[2]
		isDev := attrName == "test_requires"
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		for _, dm := range conanPyDepRE.FindAllStringSubmatch(block, -1) {
			name := dm[1]
			version := ""
			if len(dm) > 2 {
				version = dm[2]
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: vcpkg.json
// ---------------------------------------------------------------------------

// vcpkgManifest is a minimal vcpkg.json structure for JSON unmarshalling.
type vcpkgManifest struct {
	Dependencies    []vcpkgDep `json:"dependencies"`
	DevDependencies []vcpkgDep `json:"dev-dependencies"`
}

// vcpkgDep can be either a plain string or an object {"name": "...", "version-gte": "..."}.
// JSON unmarshalling for mixed types requires custom handling.
type vcpkgDep struct {
	Name    string
	Version string
	IsDev   bool
}

func parseVcpkgJSON(source string) []dep {
	// Custom parse: vcpkg dependencies can be either a string or an object.
	// We use a two-pass approach: unmarshal to raw messages, then inspect each element.
	var raw struct {
		Dependencies    []json.RawMessage `json:"dependencies"`
		DevDependencies []json.RawMessage `json:"dev-dependencies"`
	}
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		return nil
	}

	var out []dep
	seen := map[string]bool{}

	parseDep := func(msg json.RawMessage, isDev bool) {
		var s string
		if err := json.Unmarshal(msg, &s); err == nil {
			// plain string
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, dep{name: s, isDev: isDev, kind: depKindStr(isDev)})
			}
			return
		}
		// object with "name" key
		var obj struct {
			Name       string `json:"name"`
			VersionGte string `json:"version-gte"`
		}
		if err := json.Unmarshal(msg, &obj); err == nil && obj.Name != "" && !seen[obj.Name] {
			seen[obj.Name] = true
			out = append(out, dep{name: obj.Name, version: obj.VersionGte, isDev: isDev, kind: depKindStr(isDev)})
		}
	}

	for _, msg := range raw.Dependencies {
		parseDep(msg, false)
	}
	for _, msg := range raw.DevDependencies {
		parseDep(msg, true)
	}
	return out
}

func depKindStr(isDev bool) string {
	if isDev {
		return "dev"
	}
	return "runtime"
}

// ---------------------------------------------------------------------------
// Dispatch table
// ---------------------------------------------------------------------------
// Parser: *.csproj  (NuGet / MSBuild PackageReference manifest)
// ---------------------------------------------------------------------------

// csprojPackageRefRE matches <PackageReference Include="PkgName" Version="x.y.z" />
// or the two-line block form (<PackageReference Include="…"><Version>x</Version>).
var csprojPackageRefRE = regexp.MustCompile(
	`<PackageReference\s+Include\s*=\s*"([^"]+)"(?:[^/]*Version\s*=\s*"([^"]*)")?`,
)
var csprojVersionTagRE = regexp.MustCompile(
	`<Version>\s*([^<]+)\s*</Version>`,
)

func parseCsproj(source string) []dep {
	var out []dep
	// Find all <PackageReference> occurrences.
	for _, m := range csprojPackageRefRE.FindAllStringSubmatch(source, -1) {
		name := m[1]
		version := m[2]
		if name == "" {
			continue
		}
		out = append(out, dep{name: name, version: version, isDev: false, kind: "runtime"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: packages.lock.json  (NuGet v3 lock file)
// ---------------------------------------------------------------------------

// nugetLockFile is a minimal representation of the NuGet packages.lock.json format.
// The top-level key is the target framework (e.g. "net8.0"); each framework maps
// package names to version objects.
type nugetLockFile struct {
	Dependencies map[string]map[string]struct {
		Resolved string `json:"resolved"`
		Type     string `json:"type"`
	} `json:"dependencies"`
}

func parseNugetLockJSON(source string) []dep {
	var lock nugetLockFile
	if err := json.Unmarshal([]byte(source), &lock); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []dep
	for _, pkgs := range lock.Dependencies {
		for name, info := range pkgs {
			if seen[name] {
				continue
			}
			seen[name] = true
			isDev := info.Type == "Dev" || info.Type == "dev"
			out = append(out, dep{
				name:    name,
				version: info.Resolved,
				isDev:   isDev,
				kind:    "locked",
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: composer.json (PHP/Composer manifest)
// ---------------------------------------------------------------------------
//
// Parses the top-level "require" and "require-dev" maps.
// Version constraints are kept verbatim (e.g. "^1.0", "~2.3", "1.2.3").
func parseComposerJSON(source string) []dep {
	var raw struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		return nil
	}
	var out []dep
	for name, ver := range raw.Require {
		// Skip the PHP runtime version constraint itself.
		if name == "php" || strings.HasPrefix(name, "ext-") {
			continue
		}
		out = append(out, dep{name: name, version: ver, isDev: false, kind: "runtime"})
	}
	for name, ver := range raw.RequireDev {
		if name == "php" || strings.HasPrefix(name, "ext-") {
			continue
		}
		out = append(out, dep{name: name, version: ver, isDev: true, kind: "dev"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: composer.lock (PHP/Composer lockfile)
// ---------------------------------------------------------------------------
//
// Parses the top-level "packages" (runtime) and "packages-dev" arrays.
// Each entry has at least "name" and "version".
func parseComposerLock(source string) []dep {
	var raw struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
		PackagesDev []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages-dev"`
	}
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		return nil
	}
	var out []dep
	for _, p := range raw.Packages {
		if p.Name == "" {
			continue
		}
		out = append(out, dep{name: p.Name, version: p.Version, isDev: false, kind: "locked"})
	}
	for _, p := range raw.PackagesDev {
		if p.Name == "" {
			continue
		}
		out = append(out, dep{name: p.Name, version: p.Version, isDev: true, kind: "locked"})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: build.gradle / build.gradle.kts (Gradle, Groovy + Kotlin DSL)
// ---------------------------------------------------------------------------
//
// Parses dependency declarations inside the `dependencies { ... }` block.
// Both the short "group:artifact:version" string notation and the Kotlin-DSL
// quote style are handled:
//
//	implementation 'org.springframework:spring-core:5.3.0'
//	implementation("com.google.guava:guava:31.0-jre")
//	testImplementation 'junit:junit:4.13.2'
//	api "io.reactivex:rxjava:2.2.21"
//	compileOnly 'org.projectlombok:lombok:1.18.24'
//
// The configuration keyword (implementation/api/runtimeOnly/... vs
// testImplementation/testRuntimeOnly/...) determines runtime-vs-dev scope.
// Map-style notation (group: '...', name: '...', version: '...') and dependency
// constraints are out of scope — honest-partial: those lines are skipped rather
// than emitting a malformed package.
var gradleDepLineRE = regexp.MustCompile(
	`(?m)^\s*([A-Za-z][A-Za-z0-9]*)\s*[ (]\s*['"]([^'"\s]+:[^'"\s]+)['"]`,
)

// gradleConfigs is the set of recognised Gradle dependency configurations.
// Anything outside this set (e.g. a custom function call that happens to take a
// quoted "a:b:c" string) is ignored — precision over recall.
var gradleConfigs = map[string]bool{
	"implementation":            true,
	"api":                       true,
	"compileOnly":               true,
	"runtimeOnly":               true,
	"compile":                   true, // legacy
	"runtime":                   true, // legacy
	"annotationProcessor":       true,
	"kapt":                      true,
	"testImplementation":        true,
	"testCompileOnly":           true,
	"testRuntimeOnly":           true,
	"testCompile":               true, // legacy
	"androidTestImplementation": true,
	"developmentOnly":           true,
}

// gradleConfigIsDev reports whether a configuration keyword denotes a
// test/dev-only dependency.
func gradleConfigIsDev(cfg string) bool {
	return strings.HasPrefix(cfg, "test") || strings.HasPrefix(cfg, "androidTest")
}

// gradlePluginsBlockRE captures the body of a top-level `plugins { … }` block.
// Group 1 is the brace body, scanned by gradlePluginIDRE for plugin entries.
var gradlePluginsBlockRE = regexp.MustCompile(`(?s)\bplugins\s*\{(.*?)\}`)

// gradlePluginIDRE matches a modern plugin declaration inside a `plugins { … }`
// block — `id 'org.springframework.boot'` / `id("io.spring.dependency-management")`
// with an optional ` version '3.1.0'` / ` version("3.1.0")` suffix. Group 1 is
// the plugin id, group 2 the (optional) version. The Kotlin-DSL accessor form
// (`kotlin("jvm") version "1.9"`) and `apply false` modifiers are out of scope —
// honest-partial.
var gradlePluginIDRE = regexp.MustCompile(
	`\bid\s*[ (]\s*['"]([A-Za-z0-9_.\-]+)['"]\s*\)?(?:\s*version\s*[ (]\s*['"]([^'"\s]+)['"])?`)

// parseGradlePlugins extracts `plugins { id '…' version '…' }` declarations as
// plugin dependencies (kind="plugin"). Modern Gradle declares plugins in this
// block rather than the legacy `apply plugin:` form (handled by the base groovy
// extractor as a SCOPE.Component/plugin_id). Surfacing them here gives the
// plugin graph a version-carrying external-dependency node, completing the
// Gradle dep/plugin/task triad (#5364).
func parseGradlePlugins(source string) []dep {
	var out []dep
	seen := map[string]bool{}
	for _, blk := range gradlePluginsBlockRE.FindAllStringSubmatch(source, -1) {
		for _, m := range gradlePluginIDRE.FindAllStringSubmatch(blk[1], -1) {
			id := m[1]
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, dep{
				name:    id,
				version: m[2],
				kind:    "plugin",
			})
		}
	}
	return out
}

func parseBuildGradle(source string) []dep {
	var out []dep
	seen := map[string]bool{}
	// Modern `plugins { id '…' }` block declarations (kind="plugin").
	for _, p := range parseGradlePlugins(source) {
		if seen[p.name] {
			continue
		}
		seen[p.name] = true
		out = append(out, p)
	}
	for _, m := range gradleDepLineRE.FindAllStringSubmatch(source, -1) {
		cfg := m[1]
		if !gradleConfigs[cfg] {
			continue
		}
		coord := m[2] // group:artifact[:version]
		parts := strings.Split(coord, ":")
		if len(parts) < 2 {
			continue
		}
		name := parts[0] + ":" + parts[1]
		version := ""
		if len(parts) >= 3 {
			version = parts[2]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		isDev := gradleConfigIsDev(cfg)
		kind := "runtime"
		if isDev {
			kind = "dev"
		}
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: shard.yml (Crystal shards manifest)
// ---------------------------------------------------------------------------
//
// shard.yml is the Crystal `shards` package-manager manifest (YAML). A
// dependency is a sub-key under the top-level `dependencies:` (runtime) or
// `development_dependencies:` (dev) maps; the shard name is the sub-key and the
// version is an INDENTED `version:` line in the dependency's nested body (the
// source — github:/git:/path: — is the resolution coordinate, not the version):
//
//	name: myapp
//	version: 0.1.0
//	dependencies:
//	  kemal:
//	    github: kemalcr/kemal
//	    version: ~> 1.1.0
//	  jennifer:
//	    github: imdrasil/jennifer.cr
//	development_dependencies:
//	  spectator:
//	    github: crystal-loot/spectator
//	    version: ~> 0.10.0
//
// Honest-partial: a shard with no nested `version:` (pinned only by branch/commit
// /path) still emits the dependency with an empty version. The top-level
// `name:`/`version:`/`crystal:`/`license:` scalar keys are project metadata, not
// dependencies, and are excluded by the section-scoping below.
var shardDepKeyRE = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)
var shardVersionRE = regexp.MustCompile(`(?m)^\s+version:\s*"?([^"\n]+?)"?\s*$`)

// shardSectionBody returns the indented body of a top-level `<section>:` map in a
// shard.yml / shard.lock document, i.e. every line until the next column-0 key.
func shardSectionBody(source, section string) string {
	marker := "\n" + section + ":"
	idx := strings.Index(source, marker)
	if idx < 0 {
		if strings.HasPrefix(source, section+":") {
			idx = -1 // body starts at offset 0's section line
		} else {
			return ""
		}
	}
	body := source[idx+len(marker):]
	if idx < 0 {
		body = source[len(section+":"):]
	}
	// Stop at the next top-level key (a line beginning in column 0).
	if end := regexp.MustCompile(`(?m)^[A-Za-z]`).FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}
	return body
}

// parseShardSection extracts the shards declared under one section body, reading
// each shard's nested `version:` line. Used for both shard.yml's
// dependencies/development_dependencies and shard.lock's shards map.
func parseShardSection(body string, isDev bool, kind string) []dep {
	idx := shardDepKeyRE.FindAllStringSubmatchIndex(body, -1)
	var out []dep
	for i, m := range idx {
		name := body[m[2]:m[3]]
		blockEnd := len(body)
		if i+1 < len(idx) {
			blockEnd = idx[i+1][0]
		}
		block := body[m[1]:blockEnd]
		version := ""
		if vm := shardVersionRE.FindStringSubmatch(block); vm != nil {
			version = strings.TrimSpace(vm[1])
		}
		out = append(out, dep{name: name, version: version, isDev: isDev, kind: kind})
	}
	return out
}

func parseShardYml(source string) []dep {
	var out []dep
	seen := map[string]bool{}
	add := func(deps []dep) {
		for _, d := range deps {
			if seen[d.name] {
				continue
			}
			seen[d.name] = true
			out = append(out, d)
		}
	}
	add(parseShardSection(shardSectionBody(source, "dependencies"), false, "runtime"))
	add(parseShardSection(shardSectionBody(source, "development_dependencies"), true, "dev"))
	return out
}

// ---------------------------------------------------------------------------
// Parser: shard.lock (Crystal shards lockfile)
// ---------------------------------------------------------------------------
//
// shard.lock is the resolved-tree lockfile `shards` writes after `shards
// install`. It is YAML with a top-level `shards:` map (every dependency in the
// resolved closure — including transitive shards the manifest never names) each
// carrying a `git:`/`github:` source and the exact resolved `version:`:
//
//	version: 2.0
//	shards:
//	  kemal:
//	    git: https://github.com/kemalcr/kemal.git
//	    version: 1.1.2
//	  radix:
//	    git: https://github.com/luislavena/radix.git
//	    version: 0.4.1
//
// Lockfile deps record the EXACT resolved version (kind="locked") — the point of
// lockfile_parsing. The top-level `version:` scalar is the lockfile-format
// version, not a shard, and is excluded by scoping to the `shards:` section.
func parseShardLock(source string) []dep {
	return parseShardSection(shardSectionBody(source, "shards"), false, "locked")
}

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
	"pubspec.lock":        parsePubspecLock,
	"Gemfile":             parseGemfile,
	"packages.lock.json":  parseNugetLockJSON,
	// C++ build/package manifests
	"CMakeLists.txt": parseCMakeLists,
	"conanfile.txt":  parseConanfileTxt,
	"conanfile.py":   parseConanfilePy,
	"vcpkg.json":     parseVcpkgJSON,
	// PHP / Composer
	"composer.json": parseComposerJSON,
	"composer.lock": parseComposerLock,
	// Java / Kotlin — Gradle
	"build.gradle":     parseBuildGradle,
	"build.gradle.kts": parseBuildGradle,
	// Erlang — rebar3 + erlang.mk
	"rebar.config": parseRebarConfig,
	"rebar.lock":   parseRebarLock,
	"erlang.mk":    func(s string) []dep { return parseErlangMk(s, false) },
	// Lua — LuaRocks lockfile (*.rockspec manifest is suffix-dispatched below).
	"luarocks.lock": parseLuarocksLock,
	// Crystal — shards
	"shard.yml":  parseShardYml,
	"shard.lock": parseShardLock,
}

func dispatchParser(filePath, source string) (string, []dep) {
	basename := filepath.Base(filePath)
	if fn, ok := parsers[basename]; ok {
		pm := detectPackageManager(filePath)
		return pm, fn(source)
	}
	// *.csproj — NuGet project manifest
	if strings.HasSuffix(basename, ".csproj") {
		return "nuget", parseCsproj(source)
	}
	// *.app.src — Erlang/OTP application resource (rebar3 runtime apps).
	if strings.HasSuffix(basename, ".app.src") {
		return "rebar3", parseAppSrc(source)
	}
	// *.rockspec — LuaRocks package manifest.
	if strings.HasSuffix(basename, ".rockspec") {
		return "luarocks", parseRockspec(source)
	}
	// Makefile — only an erlang.mk build when it includes erlang.mk / declares
	// PROJECT (requireSignal=true); a plain Makefile is a no-op.
	if basename == "Makefile" {
		return "erlang_mk", parseErlangMk(source, true)
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

		// #5526 — package.json declared-dependency provenance. `dep_section`
		// (prod/dev/peer/optional) and `declared=true` mark a dep that the
		// manifest explicitly lists, distinct from a lockfile-only resolved
		// dep. Empty section ⇒ omit the attributes (lockfiles, other
		// ecosystems) so non-package.json records are byte-identical.
		extraProps := func() map[string]string {
			if d.section == "" {
				return nil
			}
			return map[string]string{
				"dep_section": d.section,
				"declared":    "true",
			}
		}()

		// #560: emit a single SCOPE.Component carrying the DEPENDS_ON edge
		// embedded in its Relationships, rather than a real entity plus a
		// synthetic "relationship"-kind container entity that the downstream
		// pipeline would otherwise count as a phantom entity.
		depProps := map[string]string{
			"external_dependency": "true",
			"package_manager":     packageManager,
			"version":             d.version,
			"is_dev":              isDev,
			"dependency_kind":     depKind,
			"indirect":            indirect,
			"ref":                 pRef,
			"provenance":          "INFERRED_FROM_PACKAGE_MANIFEST",
		}
		for k, v := range extraProps {
			depProps[k] = v
		}
		out = append(out, types.EntityRecord{
			Name:       d.name,
			Kind:       "SCOPE.Component",
			Subtype:    "external_dependency",
			SourceFile: filePath,
			Language:   "",
			Properties: depProps,
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

		// [sbom] Converged package node + DEPENDS_ON_PACKAGE edge. The node is
		// file/repo-agnostic (synthetic SourceFile) so the SAME ecosystem:name
		// across every manifest in the group collapses to ONE node — the
		// software-bill-of-materials convergence point. The DEPENDS_ON_PACKAGE
		// edge from the project anchor carries the per-declaration version +
		// dev scope (which are NOT part of the node identity). Mirrors the
		// SCOPE.ExternalService synthetic-node model.
		pkgID := packageTargetID(packageManager, d.name)
		pkgEnt := types.EntityRecord{
			Name:          packageName(packageManager, d.name),
			QualifiedName: pkgID,
			Kind:          string(types.EntityKindPackage),
			Subtype:       "package",
			SourceFile:    PackageSourceFile,
			Language:      "",
			StartLine:     1,
			EndLine:       1,
			Properties: map[string]string{
				"package":         d.name,
				"package_manager": packageManager,
				"ref":             pkgID,
			},
			Relationships: []types.RelationshipRecord{
				{
					FromID: projRef,
					ToID:   pkgID,
					Kind:   string(types.RelationshipKindDependsOnPackage),
					Properties: map[string]string{
						"package_manager": packageManager,
						"version":         d.version,
						"dev":             isDev,
						"dependency_kind": depKind,
						"indirect":        indirect,
					},
				},
			},
			QualityScore: 0.8,
		}
		pkgEnt.ID = pkgEnt.ComputeID()
		out = append(out, pkgEnt)
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
