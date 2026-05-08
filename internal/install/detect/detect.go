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
	KindNone  MonorepoKind = ""
	KindPNPM  MonorepoKind = "pnpm"
	KindNPM   MonorepoKind = "npm"
	KindNx    MonorepoKind = "nx"
	KindTurbo MonorepoKind = "turbo"
	KindLerna MonorepoKind = "lerna"
	KindMulti MonorepoKind = "multi"
)

// Monorepo describes a detected monorepo layout.
type Monorepo struct {
	Kind     MonorepoKind
	Packages []string // repo-relative paths to package roots
}

// DetectMonorepo inspects a repo root and returns its kind plus the
// list of package roots (repo-relative). Returns Monorepo{Kind:KindNone}
// if no monorepo signal is present.
func DetectMonorepo(repo string) (Monorepo, error) {
	switch {
	case exists(repo, "pnpm-workspace.yaml"):
		pkgs, err := scanWorkspaces(repo, parseYAMLPackages(filepath.Join(repo, "pnpm-workspace.yaml")))
		return Monorepo{Kind: KindPNPM, Packages: pkgs}, err
	case exists(repo, "nx.json"):
		pkgs, err := scanWorkspaces(repo, parsePackageJSONWorkspaces(repo))
		return Monorepo{Kind: KindNx, Packages: pkgs}, err
	case exists(repo, "turbo.json"):
		pkgs, err := scanWorkspaces(repo, parsePackageJSONWorkspaces(repo))
		return Monorepo{Kind: KindTurbo, Packages: pkgs}, err
	case exists(repo, "lerna.json"):
		pkgs, err := scanWorkspaces(repo, parseLernaPackages(filepath.Join(repo, "lerna.json")))
		return Monorepo{Kind: KindLerna, Packages: pkgs}, err
	case exists(repo, "package.json"):
		ws := parsePackageJSONWorkspaces(repo)
		if len(ws) > 0 {
			pkgs, err := scanWorkspaces(repo, ws)
			return Monorepo{Kind: KindNPM, Packages: pkgs}, err
		}
	}
	// Heuristic fallback: presence of a top-level packages/ or apps/ dir
	// each containing >1 manifest.
	for _, sub := range []string{"packages", "apps", "services"} {
		if isDir(filepath.Join(repo, sub)) {
			pkgs, err := scanGlob(repo, []string{sub + "/*"})
			if err == nil && len(pkgs) > 1 {
				return Monorepo{Kind: KindMulti, Packages: pkgs}, nil
			}
		}
	}
	return Monorepo{Kind: KindNone}, nil
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
