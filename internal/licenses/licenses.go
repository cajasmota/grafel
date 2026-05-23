// Package licenses detects dependency licenses and flags incompatible combinations.
//
// Detection sources (in priority order):
//  1. node_modules/<pkg>/package.json    — npm/yarn local metadata
//  2. GOPATH module cache LICENSE files  — Go modules (local only)
//  3. pyproject.toml dist-info METADATA  — Python packages (venv local)
//  4. Gemfile.lock + gemspec cache       — Ruby gems
//  5. Cargo.toml [package].license       — Rust (own package; deps from cargo metadata)
//
// Project license is inferred from (first match):
//
//	LICENSE / LICENSE.txt / LICENSE.md, then package.json#license,
//	then pyproject.toml#project.license, then Cargo.toml#[package.license].
//
// Compatibility matrix (SPDX-based, simplified):
//
//	GPL-2.0, GPL-3.0, AGPL-3.0 are copyleft-incompatible with
//	MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, 0BSD, Unlicense.
//	LGPL-2.1, LGPL-3.0 are weak-copyleft (warn).
//	EUPL-1.2, CDDL-1.0, MPL-2.0 are weak-copyleft (warn).
//
// Transitive analysis uses package-lock.json (npm) and local node_modules.
// Drop-in alternatives are suggested for common incompatible packages.
package licenses

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// SPDX normalization
// ---------------------------------------------------------------------------

// normalizeSPDX converts common non-standard license strings to their SPDX
// identifiers. Unknown strings are returned as-is (trimmed).
func normalizeSPDX(raw string) string {
	s := strings.TrimSpace(raw)
	// Strip surrounding parens used by some package.json authors.
	s = strings.Trim(s, "()")
	s = strings.TrimSpace(s)

	// Normalisation table — extend as needed.
	table := map[string]string{
		// MIT variants
		"mit":             "MIT",
		"mit license":     "MIT",
		"the mit license": "MIT",
		"expat":           "MIT",
		// Apache variants
		"apache 2":           "Apache-2.0",
		"apache 2.0":         "Apache-2.0",
		"apache-2":           "Apache-2.0",
		"apache license 2.0": "Apache-2.0",
		"apache-2.0":         "Apache-2.0",
		// BSD variants
		"bsd":          "BSD-2-Clause",
		"bsd 2-clause": "BSD-2-Clause",
		"bsd 3-clause": "BSD-3-Clause",
		"new bsd":      "BSD-3-Clause",
		"revised bsd":  "BSD-3-Clause",
		// GPL variants
		"gpl":                           "GPL-2.0-only",
		"gpl-2":                         "GPL-2.0-only",
		"gpl-2.0":                       "GPL-2.0-only",
		"gpl2":                          "GPL-2.0-only",
		"gnu general public license v2": "GPL-2.0-only",
		"gpl-3":                         "GPL-3.0-only",
		"gpl-3.0":                       "GPL-3.0-only",
		"gpl3":                          "GPL-3.0-only",
		"gnu general public license v3": "GPL-3.0-only",
		// AGPL
		"agpl-3":   "AGPL-3.0-only",
		"agpl-3.0": "AGPL-3.0-only",
		"agpl3":    "AGPL-3.0-only",
		// LGPL
		"lgpl-2":   "LGPL-2.1-only",
		"lgpl-2.1": "LGPL-2.1-only",
		"lgpl2":    "LGPL-2.1-only",
		"lgpl-3":   "LGPL-3.0-only",
		"lgpl-3.0": "LGPL-3.0-only",
		"lgpl3":    "LGPL-3.0-only",
		// ISC / public domain
		"isc":           "ISC",
		"0bsd":          "0BSD",
		"unlicense":     "Unlicense",
		"public domain": "Unlicense",
		// Other weak-copyleft
		"mpl-2.0":  "MPL-2.0",
		"mpl2":     "MPL-2.0",
		"eupl-1.2": "EUPL-1.2",
		"cddl-1.0": "CDDL-1.0",
		// Commercial
		"commercial":  "Proprietary",
		"proprietary": "Proprietary",
		"see license": "Proprietary",
	}
	if v, ok := table[strings.ToLower(s)]; ok {
		return v
	}
	// Already-canonical SPDX: return as-is.
	return s
}

// ---------------------------------------------------------------------------
// Compatibility
// ---------------------------------------------------------------------------

// CompatibilityLevel is the result of a compatibility check.
type CompatibilityLevel string

const (
	// CompatOK means no known conflict.
	CompatOK CompatibilityLevel = "ok"
	// CompatWarn means a weak-copyleft license that may impose obligations.
	CompatWarn CompatibilityLevel = "warn"
	// CompatError means a strong-copyleft license incompatible with the project
	// license (e.g. GPL dep in MIT project).
	CompatError CompatibilityLevel = "error"
	// CompatUnknown means we could not determine compatibility.
	CompatUnknown CompatibilityLevel = "unknown"
)

// permissiveLicenses are the set of licenses considered permissive (allow
// linking from any license without propagating obligations).
var permissiveLicenses = map[string]bool{
	"MIT":          true,
	"Apache-2.0":   true,
	"BSD-2-Clause": true,
	"BSD-3-Clause": true,
	"ISC":          true,
	"0BSD":         true,
	"Unlicense":    true,
	"WTFPL":        true,
	"CC0-1.0":      true,
	"Zlib":         true,
	"Boost-1.0":    true,
}

// strongCopyleft are GPL/AGPL style: incompatible when used in a project
// distributed under a permissive license.
var strongCopyleft = map[string]bool{
	"GPL-2.0-only":      true,
	"GPL-2.0-or-later":  true,
	"GPL-3.0-only":      true,
	"GPL-3.0-or-later":  true,
	"AGPL-3.0-only":     true,
	"AGPL-3.0-or-later": true,
}

// weakCopyleft requires attribution/disclosure but is generally usable as a
// dependency without forcing the whole project to relicense.
var weakCopyleft = map[string]bool{
	"LGPL-2.1-only":     true,
	"LGPL-2.1-or-later": true,
	"LGPL-3.0-only":     true,
	"LGPL-3.0-or-later": true,
	"MPL-2.0":           true,
	"EUPL-1.2":          true,
	"CDDL-1.0":          true,
}

// CheckCompatibility returns the compatibility level of depLicense when used
// inside a project with projectLicense.
func CheckCompatibility(projectLicense, depLicense string) CompatibilityLevel {
	proj := normalizeSPDX(projectLicense)
	dep := normalizeSPDX(depLicense)

	if dep == "" || dep == "NOASSERTION" || dep == "NONE" {
		return CompatUnknown
	}
	if strongCopyleft[dep] {
		// Dep is GPL/AGPL — only OK if project is also GPL/AGPL-compatible.
		if strongCopyleft[proj] {
			return CompatOK
		}
		// Permissive or unknown project license: flagging as error.
		return CompatError
	}
	if weakCopyleft[dep] {
		return CompatWarn
	}
	if dep == "Proprietary" {
		// Proprietary dep used in open-source project: warn.
		if permissiveLicenses[proj] || weakCopyleft[proj] {
			return CompatWarn
		}
		return CompatOK
	}
	return CompatOK
}

// ---------------------------------------------------------------------------
// Drop-in alternatives
// ---------------------------------------------------------------------------

// dropInAlternatives returns permissive-licensed alternatives for a known
// package with an incompatible license.
var dropInAlternatives = map[string][]string{
	// npm
	"node-forge": {"@peculiar/webcrypto", "node:crypto (built-in)"},
	"pdfkit":     {"jspdf"},
	"pdf-lib":    {"jspdf"},
	"cron":       {"node-cron (MIT)"},
	// Python
	"mysql-connector-python": {"PyMySQL (MIT)", "aiomysql (MIT)"},
	"psycopg2":               {"asyncpg (Apache-2.0)"},
	// Go
	"github.com/gofrs/uuid": {"github.com/google/uuid (BSD-3-Clause)"},
	// General
	"readline": {"libedit (BSD-3-Clause)"},
}

// SuggestAlternatives returns known drop-in alternatives for the given
// package name, or an empty slice if none are known.
func SuggestAlternatives(packageName string) []string {
	if alts, ok := dropInAlternatives[packageName]; ok {
		return alts
	}
	return nil
}

// ---------------------------------------------------------------------------
// License detection — local metadata
// ---------------------------------------------------------------------------

// PackageLicense holds the detected license for a single dependency.
type PackageLicense struct {
	// PackageName is the dependency name as declared in the manifest.
	PackageName string `json:"package_name"`
	// PackageManager is npm / go_modules / cargo / pip / bundler.
	PackageManager string `json:"package_manager"`
	// Version is the pinned or declared version string.
	Version string `json:"version"`
	// License is the normalized SPDX identifier.
	License string `json:"license"`
	// LicenseSource describes where the license was detected from.
	LicenseSource string `json:"license_source"`
	// IsTransitive is true when the package is an indirect/transitive dependency.
	IsTransitive bool `json:"is_transitive"`
	// Compatibility is the result of the project vs dep license check.
	Compatibility CompatibilityLevel `json:"compatibility"`
	// Alternatives are suggested drop-in replacements when Compatibility != CompatOK.
	Alternatives []string `json:"alternatives,omitempty"`
}

// DetectResult holds all findings for a single repo.
type DetectResult struct {
	// ProjectLicense is the inferred SPDX license of the project itself.
	ProjectLicense string `json:"project_license"`
	// ProjectLicenseSource is where the project license was detected.
	ProjectLicenseSource string `json:"project_license_source"`
	// Dependencies holds one entry per detected dependency.
	Dependencies []PackageLicense `json:"dependencies"`
	// Incompatible is a filtered subset — deps with Compatibility == error or warn.
	Incompatible []PackageLicense `json:"incompatible"`
	// LicenseDensity maps SPDX id → estimated fraction (0-1) of total deps.
	LicenseDensity map[string]float64 `json:"license_density,omitempty"`
}

// ---------------------------------------------------------------------------
// Project license detection
// ---------------------------------------------------------------------------

// DetectProjectLicense reads the project root and returns (spdxID, source).
func DetectProjectLicense(repoPath string) (string, string) {
	// 1. Look for a LICENSE file.
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "COPYING"} {
		p := filepath.Join(repoPath, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lic := inferFromLicenseText(string(data))
		if lic != "" {
			return normalizeSPDX(lic), name
		}
	}
	// 2. package.json
	if data, err := os.ReadFile(filepath.Join(repoPath, "package.json")); err == nil {
		var pkg struct {
			License interface{} `json:"license"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if s, ok := pkg.License.(string); ok && s != "" {
				return normalizeSPDX(s), "package.json"
			}
			// Old format: {"type":"MIT"}
			if m, ok := pkg.License.(map[string]interface{}); ok {
				if t, ok := m["type"].(string); ok {
					return normalizeSPDX(t), "package.json"
				}
			}
		}
	}
	// 3. pyproject.toml
	if data, err := os.ReadFile(filepath.Join(repoPath, "pyproject.toml")); err == nil {
		if lic := extractTOMLField(string(data), "license"); lic != "" {
			return normalizeSPDX(lic), "pyproject.toml"
		}
	}
	// 4. Cargo.toml
	if data, err := os.ReadFile(filepath.Join(repoPath, "Cargo.toml")); err == nil {
		if lic := extractTOMLField(string(data), "license"); lic != "" {
			return normalizeSPDX(lic), "Cargo.toml"
		}
	}
	return "Unknown", ""
}

// inferFromLicenseText returns a rough SPDX identifier from the text of a
// LICENSE file. This is intentionally simple — we look for canonical phrases.
func inferFromLicenseText(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "gnu affero general public license") || strings.Contains(t, "agpl"):
		return "AGPL-3.0-only"
	case strings.Contains(t, "gnu general public license") && strings.Contains(t, "version 3"):
		return "GPL-3.0-only"
	case strings.Contains(t, "gnu general public license") && strings.Contains(t, "version 2"):
		return "GPL-2.0-only"
	case strings.Contains(t, "gnu general public license"):
		return "GPL-2.0-only" // default to v2 when version not explicit
	case strings.Contains(t, "gnu lesser general public license") && strings.Contains(t, "version 3"):
		return "LGPL-3.0-only"
	case strings.Contains(t, "gnu lesser general public license"):
		return "LGPL-2.1-only"
	case strings.Contains(t, "apache license") || strings.Contains(t, "apache software license"):
		return "Apache-2.0"
	case strings.Contains(t, "mit license") || strings.Contains(t, "permission is hereby granted"):
		return "MIT"
	case strings.Contains(t, "bsd 2-clause"):
		return "BSD-2-Clause"
	case strings.Contains(t, "bsd 3-clause") || strings.Contains(t, "redistributions of source code"):
		return "BSD-3-Clause"
	case strings.Contains(t, "isc license"):
		return "ISC"
	case strings.Contains(t, "mozilla public license"):
		return "MPL-2.0"
	case strings.Contains(t, "unlicense") || strings.Contains(t, "public domain"):
		return "Unlicense"
	}
	return ""
}

// extractTOMLField returns the value of a TOML scalar field by key, e.g.
// `license = "MIT"` → `MIT`. Handles both bare string and {text="MIT"} form.
func extractTOMLField(src, key string) string {
	re := regexp.MustCompile(`(?im)^` + key + `\s*=\s*(?:"([^"]*)"|\{\s*text\s*=\s*"([^"]*)")`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// ---------------------------------------------------------------------------
// npm — node_modules/*/package.json
// ---------------------------------------------------------------------------

// DetectNPMLicenses reads node_modules/<pkg>/package.json for each package
// in the provided name list. Packages not found on disk return "Unknown".
func DetectNPMLicenses(repoPath string, packages []string) map[string]string {
	out := make(map[string]string, len(packages))
	for _, pkg := range packages {
		lic := readNPMPackageLicense(repoPath, pkg)
		if lic == "" {
			lic = "Unknown"
		}
		out[pkg] = normalizeSPDX(lic)
	}
	return out
}

func readNPMPackageLicense(repoPath, pkgName string) string {
	p := filepath.Join(repoPath, "node_modules", pkgName, "package.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var pkg struct {
		License  interface{} `json:"license"`
		Licenses []struct {
			Type string `json:"type"`
		} `json:"licenses"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	if s, ok := pkg.License.(string); ok && s != "" {
		return s
	}
	if m, ok := pkg.License.(map[string]interface{}); ok {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	if len(pkg.Licenses) > 0 {
		return pkg.Licenses[0].Type
	}
	return ""
}

// ---------------------------------------------------------------------------
// npm — package-lock.json transitive deps
// ---------------------------------------------------------------------------

// ResolveNPMTransitiveDeps parses package-lock.json v2/v3 and returns
// a map of package name → version for all transitive dependencies.
func ResolveNPMTransitiveDeps(repoPath string) map[string]string {
	p := filepath.Join(repoPath, "package-lock.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal(data, &lock) != nil {
		return nil
	}
	out := make(map[string]string)
	// v2/v3 format uses "packages" with "node_modules/X" keys.
	for key, entry := range lock.Packages {
		name := strings.TrimPrefix(key, "node_modules/")
		if name == "" {
			continue
		}
		out[name] = entry.Version
	}
	// v1 format uses "dependencies".
	for name, entry := range lock.Dependencies {
		if _, exists := out[name]; !exists {
			out[name] = entry.Version
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — dist-info METADATA detection
// ---------------------------------------------------------------------------

// DetectPyPILicensesLocal reads *.dist-info/METADATA files in common venv
// paths to extract license metadata without a network call.
func DetectPyPILicensesLocal(repoPath string, packages []string) map[string]string {
	out := make(map[string]string, len(packages))
	// Candidate site-packages directories.
	sitePaths := []string{
		filepath.Join(repoPath, ".venv", "lib"),
		filepath.Join(repoPath, "venv", "lib"),
		filepath.Join(repoPath, "env", "lib"),
	}
	for _, pkg := range packages {
		lic := detectPyPILocal(pkg, sitePaths)
		if lic == "" {
			lic = "Unknown"
		}
		out[pkg] = lic
	}
	return out
}

func detectPyPILocal(pkg string, sitePaths []string) string {
	// dist-info dirs are named like <name>-<version>.dist-info
	normName := strings.ReplaceAll(strings.ToLower(pkg), "-", "_")
	for _, siteLib := range sitePaths {
		entries, err := os.ReadDir(siteLib)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// Descend into pythonX.Y subdirs.
			if e.IsDir() && strings.HasPrefix(e.Name(), "python") {
				subs, err := os.ReadDir(filepath.Join(siteLib, e.Name(), "site-packages"))
				if err != nil {
					continue
				}
				for _, sub := range subs {
					if lic := tryDistInfo(sub.Name(), normName, filepath.Join(siteLib, e.Name(), "site-packages")); lic != "" {
						return lic
					}
				}
			}
			if lic := tryDistInfo(e.Name(), normName, siteLib); lic != "" {
				return lic
			}
		}
	}
	return ""
}

func tryDistInfo(dirName, normPkgName string, base string) string {
	if !strings.HasSuffix(dirName, ".dist-info") {
		return ""
	}
	dirNorm := strings.ToLower(strings.ReplaceAll(dirName, "-", "_"))
	if !strings.HasPrefix(dirNorm, normPkgName+"-") && !strings.HasPrefix(dirNorm, normPkgName+"_") {
		return ""
	}
	metaPath := filepath.Join(base, dirName, "METADATA")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	// Parse RFC 822-style headers.
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "License:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return normalizeSPDX(strings.TrimSpace(parts[1]))
			}
		}
		if strings.HasPrefix(line, "Classifier: License ::") {
			parts := strings.Split(line, " :: ")
			if len(parts) >= 2 {
				return normalizeSPDX(parts[len(parts)-1])
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Go — module cache LICENSE detection
// ---------------------------------------------------------------------------

// DetectGoModLicenses scans the module cache (GOPATH/pkg/mod) for LICENSE
// files belonging to the listed modules.
func DetectGoModLicenses(modules []string) map[string]string {
	out := make(map[string]string, len(modules))
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	modCache := filepath.Join(gopath, "pkg", "mod")
	for _, mod := range modules {
		lic := detectGoModLicense(modCache, mod)
		if lic == "" {
			lic = "Unknown"
		}
		out[mod] = lic
	}
	return out
}

func detectGoModLicense(modCache, modPath string) string {
	// module path: github.com/foo/bar → modCache/github.com/foo/bar@vX.Y.Z/LICENSE
	// We don't know the version, so we look for any matching prefix.
	parts := strings.SplitN(modPath, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	hostDir := filepath.Join(modCache, parts[0])
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return ""
	}
	restLower := strings.ToLower(parts[1])
	for _, e := range entries {
		if !strings.HasPrefix(strings.ToLower(e.Name()), strings.ReplaceAll(restLower, "/", "+")+`@`) {
			continue
		}
		// Found the versioned directory.
		pkgDir := filepath.Join(hostDir, e.Name())
		for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
			data, err := os.ReadFile(filepath.Join(pkgDir, name))
			if err == nil {
				if lic := inferFromLicenseText(string(data)); lic != "" {
					return normalizeSPDX(lic)
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Ruby — Gemfile.lock gem license detection
// ---------------------------------------------------------------------------

// DetectGemLicenses parses Gemfile.lock and reads the gemspec from the local
// gem cache (~/.gem/specs or local .bundle).
func DetectGemLicenses(repoPath string) map[string]string {
	out := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(repoPath, "Gemfile.lock"))
	if err != nil {
		return out
	}
	// Parse gem name+version pairs from Gemfile.lock.
	gemRe := regexp.MustCompile(`(?m)^    ([A-Za-z0-9_-]+) \(([^)]+)\)$`)
	for _, m := range gemRe.FindAllStringSubmatch(string(data), -1) {
		name := m[1]
		if _, exists := out[name]; !exists {
			lic := readGemspecLicense(name, m[2])
			if lic == "" {
				lic = "Unknown"
			}
			out[name] = lic
		}
	}
	return out
}

func readGemspecLicense(name, version string) string {
	home, _ := os.UserHomeDir()
	// Try bundler cache first.
	gemspecPaths := []string{
		filepath.Join(home, ".bundle", "cache", fmt.Sprintf("%s-%s", name, version)),
	}
	// Ruby gem install directory.
	gemsDir := filepath.Join(home, ".gem", "ruby")
	if entries, err := os.ReadDir(gemsDir); err == nil {
		for _, e := range entries {
			gemspecPaths = append(gemspecPaths,
				filepath.Join(gemsDir, e.Name(), "specifications",
					fmt.Sprintf("%s-%s.gemspec", name, version)))
		}
	}
	for _, p := range gemspecPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// Gemspec licence field: s.license = "MIT"
		re := regexp.MustCompile(`s\.licens\w+\s*=\s*["']([^"']+)["']`)
		m := re.FindStringSubmatch(string(data))
		if m != nil {
			return normalizeSPDX(m[1])
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// High-level: scan a repo and return a DetectResult
// ---------------------------------------------------------------------------

// ScanRepoLicenses scans repoPath and returns detected licenses for all
// ExternalPackage entities provided in packageList.
// packageList entries are maps with keys: name, package_manager, version.
// totalEntityCount is used to compute license density as a rough LOC proxy.
func ScanRepoLicenses(repoPath string, packageList []map[string]string, totalEntityCount int) (*DetectResult, error) {
	projLic, projSrc := DetectProjectLicense(repoPath)

	// Group packages by package manager.
	type pkgEntry struct {
		name, version string
	}
	npmPkgs := []pkgEntry{}
	goPkgs := []pkgEntry{}
	pipPkgs := []pkgEntry{}

	for _, p := range packageList {
		name := p["name"]
		pm := p["package_manager"]
		ver := p["version"]
		switch pm {
		case "npm":
			npmPkgs = append(npmPkgs, pkgEntry{name, ver})
		case "go_modules":
			goPkgs = append(goPkgs, pkgEntry{name, ver})
		case "pip":
			pipPkgs = append(pipPkgs, pkgEntry{name, ver})
		}
	}

	// Detect npm licenses.
	npmNames := make([]string, len(npmPkgs))
	for i, p := range npmPkgs {
		npmNames[i] = p.name
	}
	npmLicenses := DetectNPMLicenses(repoPath, npmNames)

	// Detect transitive npm deps.
	transitiveNPM := ResolveNPMTransitiveDeps(repoPath)
	directNPM := make(map[string]bool, len(npmNames))
	for _, n := range npmNames {
		directNPM[n] = true
	}

	// Detect Go licenses.
	goNames := make([]string, len(goPkgs))
	for i, p := range goPkgs {
		goNames[i] = p.name
	}
	goLicenses := DetectGoModLicenses(goNames)

	// Detect pip licenses.
	pipNames := make([]string, len(pipPkgs))
	for i, p := range pipPkgs {
		pipNames[i] = p.name
	}
	pipLicenses := DetectPyPILicensesLocal(repoPath, pipNames)

	// Detect gem licenses (bundler).
	gemLicenses := DetectGemLicenses(repoPath)

	var deps []PackageLicense

	// Helper: resolve license for a package.
	resolveLicense := func(pm, name string) (lic, src string) {
		switch pm {
		case "npm":
			lic = npmLicenses[name]
			if lic != "" && lic != "Unknown" {
				src = "node_modules/" + name + "/package.json"
			}
		case "go_modules":
			lic = goLicenses[name]
			if lic != "" && lic != "Unknown" {
				src = "GOPATH module cache"
			}
		case "pip":
			lic = pipLicenses[name]
			if lic != "" && lic != "Unknown" {
				src = ".dist-info/METADATA"
			}
		case "bundler":
			lic = gemLicenses[name]
			if lic != "" && lic != "Unknown" {
				src = "gemspec"
			}
		}
		if lic == "" || lic == "Unknown" {
			lic = "Unknown"
			src = "not-detected"
		}
		return lic, src
	}

	// Build direct dependency records.
	for _, p := range packageList {
		pm := p["package_manager"]
		name := p["name"]
		ver := p["version"]
		lic, src := resolveLicense(pm, name)
		compat := CheckCompatibility(projLic, lic)
		alts := SuggestAlternatives(name)
		deps = append(deps, PackageLicense{
			PackageName:    name,
			PackageManager: pm,
			Version:        ver,
			License:        lic,
			LicenseSource:  src,
			IsTransitive:   false,
			Compatibility:  compat,
			Alternatives:   alts,
		})
	}

	// Build transitive npm records (those not already in direct list).
	for name, ver := range transitiveNPM {
		if directNPM[name] {
			continue
		}
		lic := normalizeSPDX(readNPMPackageLicense(repoPath, name))
		if lic == "" {
			lic = "Unknown"
		}
		src := "node_modules/" + name + "/package.json (transitive)"
		compat := CheckCompatibility(projLic, lic)
		alts := SuggestAlternatives(name)
		deps = append(deps, PackageLicense{
			PackageName:    name,
			PackageManager: "npm",
			Version:        ver,
			License:        lic,
			LicenseSource:  src,
			IsTransitive:   true,
			Compatibility:  compat,
			Alternatives:   alts,
		})
	}

	// Collect incompatible.
	var incompatible []PackageLicense
	for _, d := range deps {
		if d.Compatibility == CompatError || d.Compatibility == CompatWarn {
			incompatible = append(incompatible, d)
		}
	}

	// License density: fraction of deps under each license identifier.
	var density map[string]float64
	if len(deps) > 0 {
		licCount := make(map[string]int)
		for _, d := range deps {
			licCount[d.License]++
		}
		density = make(map[string]float64, len(licCount))
		totalDeps := float64(len(deps))
		for lic, cnt := range licCount {
			density[lic] = math.Round(float64(cnt)/totalDeps*10000) / 10000
		}
	}
	_ = totalEntityCount // reserved for future LOC-weighted density

	return &DetectResult{
		ProjectLicense:       projLic,
		ProjectLicenseSource: projSrc,
		Dependencies:         deps,
		Incompatible:         incompatible,
		LicenseDensity:       density,
	}, nil
}
