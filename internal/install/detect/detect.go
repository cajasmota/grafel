// Package detect provides cheap heuristic detection for repo stacks
// and monorepo layouts. The CLI uses these to suggest sensible defaults
// in the wizard and the `monorepo` subcommand.
package detect

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stack returns a short label for the dominant stack in a repo. It
// looks for canonical manifest files and returns the first match. The
// list is intentionally small — wizard suggestions, not classification.
func Stack(repo string) string {
	if exists(repo, "package.json") {
		// Disambiguate between Next/React/Node.
		if exists(repo, "next.config.js") || exists(repo, "next.config.mjs") || exists(repo, "next.config.ts") {
			return "next"
		}
		if exists(repo, "app.json") || exists(repo, "metro.config.js") {
			return "react-native"
		}
		return "node"
	}
	if exists(repo, "go.mod") {
		return "go"
	}
	if exists(repo, "pyproject.toml") || exists(repo, "requirements.txt") || exists(repo, "manage.py") {
		return "python"
	}
	if exists(repo, "Cargo.toml") {
		return "rust"
	}
	if exists(repo, "pom.xml") || exists(repo, "build.gradle") || exists(repo, "build.gradle.kts") {
		return "jvm"
	}
	if exists(repo, "Gemfile") {
		return "ruby"
	}
	return "unknown"
}

// MonorepoKind labels the detected monorepo layout.
type MonorepoKind string

const (
	KindNone     MonorepoKind = ""
	KindPNPM     MonorepoKind = "pnpm"
	KindNPM      MonorepoKind = "npm"
	KindNx       MonorepoKind = "nx"
	KindTurbo    MonorepoKind = "turbo"
	KindLerna    MonorepoKind = "lerna"
	KindMulti    MonorepoKind = "multi"
	KindPolyglot MonorepoKind = "polyglot"
)

// containerDirs are the conventional top-level directories that hold service
// or package modules in a (polyglot) monorepo. Each immediate child that
// contains source code is treated as a selectable module.
var containerDirs = []string{"services", "apps", "packages", "libs", "lib", "frontend", "backend", "modules", "cmd", "pkg"}

// ecosystemManifests are per-package manifest filenames across ecosystems. A
// directory containing any of these is a package root regardless of language.
var ecosystemManifests = []string{
	"package.json",                            // npm/pnpm/yarn (Node)
	"pyproject.toml", "setup.py", "setup.cfg", // Python
	"requirements.txt", "Pipfile", // Python
	"go.mod",                                      // Go
	"pom.xml", "build.gradle", "build.gradle.kts", // Maven/Gradle (JVM)
	"Cargo.toml",    // Rust
	"composer.json", // PHP
	"mix.exs",       // Elixir
	"Gemfile",       // Ruby
	"*.csproj",      // .NET (matched specially)
}

// sourceExts is the set of file extensions that count as "source code" for the
// code-dir fallback. Kept in sync (loosely) with the classifier's language map
// but inlined here to keep the detect package dependency-free and cheap.
var sourceExts = map[string]struct{}{
	".py": {}, ".pyi": {}, ".pyw": {},
	".go": {},
	".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {},
	".ts": {}, ".tsx": {}, ".mts": {}, ".cts": {},
	".java": {},
	".kt":   {}, ".kts": {},
	".rb": {}, ".rake": {},
	".php":   {},
	".rs":    {},
	".cs":    {},
	".swift": {},
	".scala": {}, ".sc": {},
	".ex": {}, ".exs": {},
	".c": {}, ".h": {}, ".cpp": {}, ".cc": {}, ".cxx": {}, ".hpp": {},
	".dart": {},
}

// Monorepo describes a detected monorepo layout.
type Monorepo struct {
	Kind     MonorepoKind
	Packages []string // repo-relative paths to package roots
}

// DetectMonorepo inspects a repo root and returns its kind plus the
// list of package roots (repo-relative). Returns Monorepo{Kind:KindNone}
// if no monorepo signal is present.
//
// Detection is a HYBRID of two strategies, unioned so a polyglot monorepo
// surfaces ALL its services (not just the pnpm/Node ones a workspace manifest
// happens to list):
//
//  1. Ecosystem workspace manifests (pnpm-workspace.yaml, package.json
//     `workspaces`, nx/turbo/lerna) — the JS/TS workspace packages.
//  2. A polyglot code-dir scan — every immediate child of the conventional
//     container dirs (services/, apps/, libs/, frontend/, …) plus top-level
//     module dirs that contain SOURCE CODE in any supported language or an
//     ecosystem manifest (go.mod, pyproject.toml, pom.xml, Cargo.toml,
//     composer.json, mix.exs, …).
//
// The union means a pure-Node workspace still reports exactly its workspace
// packages, while a polyglot platform reports its Python/Go/Java/Kotlin/Rust/
// PHP/Elixir services too. The reported Kind reflects the dominant signal: a
// workspace manifest's kind when one exists, else KindMulti/KindPolyglot.
func DetectMonorepo(repo string) (Monorepo, error) {
	kind := KindNone
	seen := map[string]struct{}{}

	add := func(pkgs []string) {
		for _, p := range pkgs {
			seen[filepath.ToSlash(p)] = struct{}{}
		}
	}

	// 1. Ecosystem workspace manifests (Node/JS/TS).
	switch {
	case exists(repo, "pnpm-workspace.yaml"):
		pkgs, err := scanWorkspaces(repo, parseYAMLPackages(filepath.Join(repo, "pnpm-workspace.yaml")))
		if err != nil {
			return Monorepo{}, err
		}
		add(pkgs)
		kind = KindPNPM
	case exists(repo, "nx.json"):
		pkgs, err := scanWorkspaces(repo, parsePackageJSONWorkspaces(repo))
		if err != nil {
			return Monorepo{}, err
		}
		add(pkgs)
		kind = KindNx
	case exists(repo, "turbo.json"):
		pkgs, err := scanWorkspaces(repo, parsePackageJSONWorkspaces(repo))
		if err != nil {
			return Monorepo{}, err
		}
		add(pkgs)
		kind = KindTurbo
	case exists(repo, "lerna.json"):
		pkgs, err := scanWorkspaces(repo, parseLernaPackages(filepath.Join(repo, "lerna.json")))
		if err != nil {
			return Monorepo{}, err
		}
		add(pkgs)
		kind = KindLerna
	case exists(repo, "package.json"):
		ws := parsePackageJSONWorkspaces(repo)
		if len(ws) > 0 {
			pkgs, err := scanWorkspaces(repo, ws)
			if err != nil {
				return Monorepo{}, err
			}
			add(pkgs)
			kind = KindNPM
		}
	}

	// A workspace manifest is, by itself, definitive monorepo evidence even
	// when it lists a single package — record that before the code-dir scan so
	// the plain-repo gate below does not discard a legitimately-empty manifest.
	hadWorkspaceManifest := kind != KindNone

	// 2. Polyglot code-dir scan — surfaces non-Node services the workspace
	//    manifest doesn't list (orders/Python, inventory/Go, notifications/
	//    Kotlin, pricing/Rust, billing/PHP, realtime-dashboard/Elixir, …).
	//
	// The scan distinguishes two classes of candidate (issue #1628):
	//
	//   - container-based modules: immediate children of conventional monorepo
	//     container dirs (services/, apps/, packages/, libs/, …). A repo laid
	//     out this way IS a monorepo, so source-only children count.
	//   - manifested top-level dirs: count as real package boundaries.
	//   - source-only top-level dirs (src/, components/, docs/, …): weak — only
	//     surfaced once the repo is independently established as a monorepo.
	//     A plain repo's bare top-level source dirs must NOT be promoted to
	//     "modules" — that is the mis-split this issue fixes.
	containerMods, manifestedTop, sourceOnlyTop := scanPolyglotModules(repo)
	add(containerMods)
	add(manifestedTop)

	// Plain-repo gate: a repo is a TRUE monorepo only when there is real
	// multi-package evidence — a workspace manifest, a conventional container
	// layout, or more than one independently-manifested top-level package.
	// A repo whose ONLY structure is bare source-only top-level dirs (src/,
	// components/, docs/) is a PLAIN repo: report KindNone so it indexes as a
	// SINGLE unit (no per-top-level-dir module breakdown).
	isMonorepo := hadWorkspaceManifest ||
		len(containerMods) > 0 ||
		len(manifestedTop) > 1
	if !isMonorepo {
		return Monorepo{Kind: KindNone}, nil
	}

	// Now that the repo is confirmed a monorepo, source-only top-level dirs are
	// legitimate sibling modules (e.g. data-pipeline/ alongside services/).
	add(sourceOnlyTop)

	// Decide the reported kind. If a workspace manifest set a kind, keep it.
	// Otherwise label the layout from the code-dir scan: KindPolyglot when the
	// detected modules span more than one language, else KindMulti.
	if kind == KindNone && len(seen) > 1 {
		if len(distinctLanguages(repo, seen)) > 1 {
			kind = KindPolyglot
		} else {
			kind = KindMulti
		}
	}
	if len(seen) == 0 {
		return Monorepo{Kind: KindNone}, nil
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return Monorepo{Kind: kind, Packages: out}, nil
}

// scanPolyglotModules returns repo-relative paths to directories that look like
// service/package modules in ANY language, split into two classes so the caller
// can apply the plain-repo gate (issue #1628):
//
//   - containerMods: immediate children of conventional container dirs
//     (services/, apps/, packages/, libs/, …). A directory laid out this way is
//     a recognized monorepo convention, so a child counts when it has an
//     ecosystem manifest OR merely contains source files.
//   - manifestedTop: top-level dirs that are NOT container dirs and carry their
//     OWN ecosystem manifest (go.mod, pyproject.toml, package.json, Cargo.toml,
//     pom.xml, composer.json, mix.exs, …) — a real, self-declared package
//     boundary (e.g. libs/py-shared at the root).
//   - sourceOnlyTop: top-level dirs that are NOT container dirs and contain
//     only source files (no manifest) — e.g. src/, components/, docs/,
//     data-pipeline/. These are WEAK evidence: they are reported as modules
//     ONLY when the repo is independently established as a monorepo (container
//     layout or workspace manifest). In a plain repo they are part of the
//     single unit, NOT separate modules — this is the mis-split #1628 fixes.
func scanPolyglotModules(repo string) (containerMods, manifestedTop, sourceOnlyTop []string) {
	seen := map[string]struct{}{}

	considerContainer := func(rel string) {
		rel = filepath.ToSlash(rel)
		if _, ok := seen[rel]; ok {
			return
		}
		abs := filepath.Join(repo, rel)
		if isDir(abs) && isModuleDir(abs) {
			seen[rel] = struct{}{}
			containerMods = append(containerMods, rel)
		}
	}

	// Immediate children of conventional container dirs (source-only counts).
	for _, container := range containerDirs {
		cdir := filepath.Join(repo, container)
		entries, err := os.ReadDir(cdir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || isIgnoredDir(e.Name()) {
				continue
			}
			considerContainer(filepath.Join(container, e.Name()))
		}
	}

	// Top-level service/package dirs that aren't themselves containers.
	topEntries, err := os.ReadDir(repo)
	if err == nil {
		for _, e := range topEntries {
			if !e.IsDir() || isIgnoredDir(e.Name()) {
				continue
			}
			name := e.Name()
			// Skip the container dirs themselves; their children were scanned.
			if isContainer(name) {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			abs := filepath.Join(repo, name)
			if !isDir(abs) {
				continue
			}
			switch {
			case hasEcosystemManifest(abs):
				seen[name] = struct{}{}
				manifestedTop = append(manifestedTop, name)
			case hasSourceFiles(abs, 2):
				seen[name] = struct{}{}
				sourceOnlyTop = append(sourceOnlyTop, name)
			}
		}
	}

	sort.Strings(containerMods)
	sort.Strings(manifestedTop)
	sort.Strings(sourceOnlyTop)
	return containerMods, manifestedTop, sourceOnlyTop
}

// distinctLanguages returns the set of source languages across the given
// repo-relative module paths. Used to decide KindPolyglot vs KindMulti. It
// probes by source-file extension (shallow) rather than the root-manifest-only
// Stack(), so a Python service whose only signal is nested *.py files still
// counts.
func distinctLanguages(repo string, modules map[string]struct{}) map[string]struct{} {
	langs := map[string]struct{}{}
	for rel := range modules {
		moduleLanguages(filepath.Join(repo, rel), 2, langs)
	}
	return langs
}

// extLanguage maps a (lower-cased) extension to a coarse language label for
// polyglot classification.
var extLanguage = map[string]string{
	".py": "python", ".pyi": "python", ".pyw": "python",
	".go": "go",
	".js": "node", ".jsx": "node", ".mjs": "node", ".cjs": "node",
	".ts": "node", ".tsx": "node", ".mts": "node", ".cts": "node",
	".java": "jvm", ".kt": "jvm", ".kts": "jvm", ".scala": "jvm",
	".rb":    "ruby",
	".php":   "php",
	".rs":    "rust",
	".cs":    "dotnet",
	".swift": "swift",
	".ex":    "elixir", ".exs": "elixir",
	".dart": "dart",
	".c":    "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp",
}

// moduleLanguages adds the languages found in dir (up to maxDepth) into langs.
func moduleLanguages(dir string, maxDepth int, langs map[string]struct{}) {
	if maxDepth < 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			if !isIgnoredDir(e.Name()) {
				moduleLanguages(filepath.Join(dir, e.Name()), maxDepth-1, langs)
			}
			continue
		}
		if l, ok := extLanguage[strings.ToLower(filepath.Ext(e.Name()))]; ok {
			langs[l] = struct{}{}
		}
	}
}

func isContainer(name string) bool {
	for _, c := range containerDirs {
		if c == name {
			return true
		}
	}
	return false
}

// isIgnoredDir skips dot-dirs and well-known non-source directories so the
// scan never surfaces node_modules/vendor/.git as "modules".
func isIgnoredDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "target", "build", "dist", "out",
		"__pycache__", ".venv", "venv", "bin", "obj", "tmp",
		"contracts", "infra", "docs", "deploy", "scripts", "test", "tests",
		"testdata", "fixtures", "examples", "third_party":
		return true
	}
	return false
}

// isModuleDir reports whether dir is a source module: it has an ecosystem
// manifest OR contains source files in a supported language (scanned shallowly,
// up to ~2 levels deep — enough to catch services/orders/app/db.py).
func isModuleDir(dir string) bool {
	if hasEcosystemManifest(dir) {
		return true
	}
	return hasSourceFiles(dir, 2)
}

// hasEcosystemManifest reports whether dir contains any per-package manifest.
func hasEcosystemManifest(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for _, m := range ecosystemManifests {
			if m == "*.csproj" {
				if strings.HasSuffix(name, ".csproj") {
					return true
				}
				continue
			}
			if name == m {
				return true
			}
		}
	}
	return false
}

// hasSourceFiles reports whether dir (or a subdir within maxDepth) contains at
// least one supported source file. It stops at the first hit and skips ignored
// directories so it stays cheap on large trees.
func hasSourceFiles(dir string, maxDepth int) bool {
	if maxDepth < 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir() {
			if !isIgnoredDir(e.Name()) {
				subdirs = append(subdirs, e.Name())
			}
			continue
		}
		if _, ok := sourceExts[strings.ToLower(filepath.Ext(e.Name()))]; ok {
			return true
		}
	}
	for _, sd := range subdirs {
		if hasSourceFiles(filepath.Join(dir, sd), maxDepth-1) {
			return true
		}
	}
	return false
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func parsePackageJSONWorkspaces(repo string) []string {
	b, err := os.ReadFile(filepath.Join(repo, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Workspaces any `json:"workspaces"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	switch v := pkg.Workspaces.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case map[string]any:
		if pkgs, ok := v["packages"].([]any); ok {
			out := make([]string, 0, len(pkgs))
			for _, x := range pkgs {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

func parseLernaPackages(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lerna struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(b, &lerna); err != nil {
		return nil
	}
	return lerna.Packages
}

// parseYAMLPackages reads a tiny pnpm-workspace.yaml without pulling in
// a real YAML parser — only the `packages:` key is honored.
func parseYAMLPackages(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	in := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		trim := strings.TrimSpace(line)
		if !in {
			if strings.HasPrefix(trim, "packages:") {
				in = true
				rest := strings.TrimSpace(strings.TrimPrefix(trim, "packages:"))
				if strings.HasPrefix(rest, "[") {
					rest = strings.Trim(rest, "[] \t")
					for _, p := range strings.Split(rest, ",") {
						p = strings.Trim(strings.TrimSpace(p), `"' `)
						if p != "" {
							out = append(out, p)
						}
					}
					return out
				}
			}
			continue
		}
		if strings.HasPrefix(line, "  - ") || strings.HasPrefix(line, "- ") {
			val := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "  "), "- "))
			val = strings.Trim(val, `"' `)
			if val != "" {
				out = append(out, val)
			}
			continue
		}
		// Any non-list line at the top level ends the packages: block.
		if !strings.HasPrefix(line, " ") {
			break
		}
	}
	return out
}

// scanWorkspaces expands a list of glob-ish workspace patterns into the
// concrete package roots that exist on disk. Each pattern may end in
// "/*" or be a literal path.
func scanWorkspaces(repo string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	for _, pat := range patterns {
		matches, err := scanGlob(repo, []string{pat})
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func scanGlob(repo string, patterns []string) ([]string, error) {
	var out []string
	for _, pat := range patterns {
		// Trim trailing /*; we'll list directories ourselves.
		base := pat
		recurse := false
		if strings.HasSuffix(base, "/*") {
			base = strings.TrimSuffix(base, "/*")
			recurse = true
		} else if strings.HasSuffix(base, "/**") {
			base = strings.TrimSuffix(base, "/**")
			recurse = true
		}
		root := filepath.Join(repo, base)
		fi, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !fi.IsDir() {
			continue
		}
		if !recurse {
			out = append(out, base)
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, e.Name(), "package.json")); err == nil {
				out = append(out, filepath.Join(base, e.Name()))
			}
		}
	}
	return out, nil
}
