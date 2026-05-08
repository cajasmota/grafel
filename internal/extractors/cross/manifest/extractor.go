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
//   - pom.xml              (maven)
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
var exactManifestNames = map[string]bool{
	"package.json":   true,
	"go.mod":         true,
	"Cargo.toml":     true,
	"pyproject.toml": true,
	"pom.xml":        true,
}

// IsManifest returns true when filePath names a supported manifest file.
func IsManifest(filePath string) bool {
	basename := filepath.Base(filePath)
	return exactManifestNames[basename]
}

// detectPackageManager returns the package manager for a manifest path.
func detectPackageManager(filePath string) string {
	pm := map[string]string{
		"package.json":   "npm",
		"go.mod":         "go_modules",
		"Cargo.toml":     "cargo",
		"pyproject.toml": "pip",
		"pom.xml":        "maven",
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
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		return nil
	}
	var out []dep
	for pkg, ver := range data.Dependencies {
		out = append(out, dep{name: pkg, version: ver, isDev: false})
	}
	for pkg, ver := range data.DevDependencies {
		out = append(out, dep{name: pkg, version: ver, isDev: true})
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: go.mod
// ---------------------------------------------------------------------------

var goRequireBlockRE = regexp.MustCompile(`(?s)require\s*\(([^)]+)\)`)

// goRequireSingleRE matches `require module version` on its own line,
// ensuring the token after require is not `(` (which starts a block).
var goRequireSingleRE = regexp.MustCompile(`(?m)^require\s+([^(\s]\S*)\s+(v\S+)`)
var goPkgLineRE = regexp.MustCompile(`(?m)^\s+(\S+)\s+(v\S+)(?:\s*//\s*indirect)?`)

func parseGoMod(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	for _, bm := range goRequireBlockRE.FindAllStringSubmatch(source, -1) {
		block := bm[1]
		for _, lm := range goPkgLineRE.FindAllStringSubmatch(block, -1) {
			name := lm[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, dep{name: name, version: lm[2], isDev: false})
		}
	}
	for _, m := range goRequireSingleRE.FindAllStringSubmatch(source, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dep{name: name, version: m[2], isDev: false})
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

	parseBody := func(body string, isDev bool) []dep {
		var out []dep
		for _, m := range cargoDepLineRE.FindAllStringSubmatch(body, -1) {
			name := m[1]
			version := m[2]
			if version == "" {
				version = m[3]
			}
			out = append(out, dep{name: name, version: version, isDev: isDev})
		}
		return out
	}

	var out []dep
	out = append(out, parseBody(bodyFor("dependencies"), false)...)
	out = append(out, parseBody(bodyFor("dev-dependencies"), true)...)
	out = append(out, parseBody(bodyFor("build-dependencies"), false)...)
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

	extractDeps := func(body string, isDev bool) []dep {
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
					out = append(out, dep{name: name, version: version, isDev: isDev})
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
			out = append(out, dep{name: name, version: version, isDev: isDev})
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

	addDeps(extractDeps(bodyFor("project"), false))
	addDeps(extractDeps(bodyFor("tool.poetry.dependencies"), false))
	addDeps(extractDeps(bodyFor("tool.poetry.dev-dependencies"), true))
	for _, s := range sections[:len(sections)-1] {
		if strings.HasPrefix(s.name, "tool.poetry.group.") && strings.HasSuffix(s.name, ".dependencies") {
			parts := strings.Split(s.name, ".")
			groupName := ""
			if len(parts) >= 4 {
				groupName = parts[3]
			}
			isDev := groupName == "dev" || groupName == "test" || groupName == "docs" || groupName == "lint"
			addDeps(extractDeps(bodyFor(s.name), isDev))
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
		out = append(out, dep{name: name, version: d.Version, isDev: isDev})
	}
	return out
}

// ---------------------------------------------------------------------------
// Dispatch table
// ---------------------------------------------------------------------------

type parserFn func(source string) []dep

var parsers = map[string]parserFn{
	"package.json":   parsePackageJSON,
	"go.mod":         parseGoMod,
	"Cargo.toml":     parseCargoToml,
	"pyproject.toml": parsePyprojectToml,
	"pom.xml":        parsePomXML,
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
				"ref":                 pRef,
				"provenance":          "INFERRED_FROM_PACKAGE_MANIFEST",
			},
			QualityScore: 0.8,
		})

		out = append(out, types.EntityRecord{
			Name:       "DEPENDS_ON:" + projRef + "->" + pRef,
			Kind:       "relationship",
			Subtype:    "depends_on",
			SourceFile: filePath,
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
