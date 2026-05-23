package dashboard

// handlers_repo_manifest.go — Repo Manifest viewer (#1351)
//
// Surfaces per-repo metadata: languages, AGENTS.md status, .archigraph/
// state files, and dependency manifest files. Also provides a lightweight
// re-scan endpoint that refreshes manifest data without a full rebuild.
//
// Routes registered in server.go:
//
//	GET  /api/groups/{group}/repos/{repo}/manifest   — full manifest
//	POST /api/groups/{group}/repos/{repo}/manifest/refresh — re-scan (async)

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/install/detect"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// RepoManifestReply is the wire shape for GET /api/groups/{group}/repos/{repo}/manifest.
type RepoManifestReply struct {
	// Group and Repo echo back the path params.
	Group string `json:"group"`
	Repo  string `json:"repo"`

	// AbsPath is the absolute on-disk path of the repo.
	AbsPath string `json:"abs_path"`

	// Stack is the dominant language stack ("go", "node", "python", …).
	Stack string `json:"stack"`

	// Languages is the ordered list of all detected language stacks.
	Languages []string `json:"languages"`

	// AgentsMD describes the AGENTS.md (or CLAUDE.md / GEMINI.md) file found.
	AgentsMD AgentsMDInfo `json:"agents_md"`

	// ArchigraphState lists the files present under <repo>/.archigraph/.
	ArchigraphState []string `json:"archigraph_state"`

	// DependencyManifests lists the dependency manifest files found at the root.
	DependencyManifests []string `json:"dependency_manifests"`

	// QualityScore is a 0-100 heuristic derived from manifest completeness signals.
	QualityScore int `json:"quality_score"`

	// QualitySignals enumerates the individual signals used for scoring.
	QualitySignals []QualitySignal `json:"quality_signals"`
}

// AgentsMDInfo describes the AGENTS.md (or equivalent) file for a repo.
type AgentsMDInfo struct {
	// Exists is true when any agents-context file was found.
	Exists bool `json:"exists"`
	// Filename is the base name of the found file ("AGENTS.md", "CLAUDE.md", …).
	Filename string `json:"filename,omitempty"`
	// InjectedByArchigraph is true when the file contains the archigraph marker block.
	InjectedByArchigraph bool `json:"injected_by_archigraph"`
	// PreviewLines holds up to 50 lines from the beginning of the file.
	PreviewLines []string `json:"preview_lines,omitempty"`
	// EditorURI is a "file://…" URI that desktop editors can open directly.
	EditorURI string `json:"editor_uri,omitempty"`
}

// QualitySignal is a single boolean signal that contributes to the quality score.
type QualitySignal struct {
	// Name is a human-readable label for the signal.
	Name string `json:"name"`
	// OK is true when the signal is present.
	OK bool `json:"ok"`
	// Points is the number of points this signal contributes when OK.
	Points int `json:"points"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/groups/{group}/repos/{repo}/manifest
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleRepoManifest(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	repoPath, err := resolveRepoPath(group, repo)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	reply := buildManifest(group, repo, repoPath)
	writeJSON(w, http.StatusOK, reply)
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/groups/{group}/repos/{repo}/manifest/refresh
// ─────────────────────────────────────────────────────────────────────────────

// ManifestRefreshReply is the wire shape for POST …/manifest/refresh.
type ManifestRefreshReply struct {
	// Manifest is the freshly-scanned manifest (synchronous — no rebuild needed).
	Manifest RepoManifestReply `json:"manifest"`
}

func (s *Server) handleRepoManifestRefresh(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	repo := r.PathValue("repo")
	if group == "" || repo == "" {
		writeErr(w, http.StatusBadRequest, "group and repo required")
		return
	}

	repoPath, err := resolveRepoPath(group, repo)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	manifest := buildManifest(group, repo, repoPath)
	writeJSON(w, http.StatusOK, ManifestRefreshReply{Manifest: manifest})
}

// ─────────────────────────────────────────────────────────────────────────────
// Core scanning logic
// ─────────────────────────────────────────────────────────────────────────────

// buildManifest scans repoPath and returns a fully-populated RepoManifestReply.
func buildManifest(group, repo, repoPath string) RepoManifestReply {
	reply := RepoManifestReply{
		Group:   group,
		Repo:    repo,
		AbsPath: repoPath,
		Stack:   detect.Stack(repoPath),
	}

	reply.Languages = detectLanguages(repoPath)
	reply.AgentsMD = scanAgentsMD(repoPath)
	reply.ArchigraphState = scanArchigraphState(repoPath)
	reply.DependencyManifests = scanDependencyManifests(repoPath)
	reply.QualitySignals, reply.QualityScore = computeQuality(reply)

	return reply
}

// detectLanguages returns all language stacks detected in the repo, not just
// the dominant one returned by detect.Stack. Multiple stacks can coexist in
// monorepo roots or polyglot services.
func detectLanguages(repoPath string) []string {
	candidates := []struct {
		marker string
		lang   string
	}{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"Cargo.toml", "rust"},
		{"pom.xml", "jvm"},
		{"build.gradle", "jvm"},
		{"build.gradle.kts", "jvm"},
		{"Gemfile", "ruby"},
		{"composer.json", "php"},
		{"*.csproj", "dotnet"},
		{"*.fsproj", "dotnet"},
	}
	seen := map[string]struct{}{}
	var langs []string
	for _, c := range candidates {
		if strings.Contains(c.marker, "*") {
			// glob match — scan root-level files
			ext := strings.TrimPrefix(c.marker, "*")
			entries, err := os.ReadDir(repoPath)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ext) {
					if _, ok := seen[c.lang]; !ok {
						seen[c.lang] = struct{}{}
						langs = append(langs, c.lang)
					}
					break
				}
			}
		} else {
			if fileExists(repoPath, c.marker) {
				if _, ok := seen[c.lang]; !ok {
					seen[c.lang] = struct{}{}
					langs = append(langs, c.lang)
				}
			}
		}
	}
	return langs
}

// scanAgentsMD probes for AGENTS.md, CLAUDE.md, and GEMINI.md in the repo root.
// It reads up to 50 lines for preview and checks for the archigraph marker.
func scanAgentsMD(repoPath string) AgentsMDInfo {
	const markerSubstring = "archigraph"
	candidates := []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"}
	for _, name := range candidates {
		full := filepath.Join(repoPath, name)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		info := AgentsMDInfo{
			Exists:    true,
			Filename:  name,
			EditorURI: "file://" + full,
		}
		// Read up to 50 lines.
		f, err := os.Open(full)
		if err != nil {
			return info
		}
		defer f.Close()

		scanner := bufio.NewScanner(io.LimitReader(f, 64*1024)) // 64 KiB max
		var lines []string
		injected := false
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			if strings.Contains(strings.ToLower(line), markerSubstring) {
				injected = true
			}
			if len(lines) >= 50 {
				break
			}
		}
		info.PreviewLines = lines
		info.InjectedByArchigraph = injected
		return info
	}
	return AgentsMDInfo{}
}

// scanArchigraphState lists file names present under <repo>/.archigraph/.
// Returns an empty slice (never nil) when the directory is absent.
func scanArchigraphState(repoPath string) []string {
	stateDir := daemon.StateDirForRepo(repoPath)
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		// Also probe the in-repo .archigraph directory for committed manifests.
		inRepoDir := filepath.Join(repoPath, ".archigraph")
		entries2, err2 := os.ReadDir(inRepoDir)
		if err2 != nil {
			return []string{}
		}
		var names []string
		for _, e := range entries2 {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		return names
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// Merge in-repo .archigraph committed files (e.g. group.json).
	inRepoDir := filepath.Join(repoPath, ".archigraph")
	if entries2, err2 := os.ReadDir(inRepoDir); err2 == nil {
		seen := map[string]struct{}{}
		for _, n := range names {
			seen[n] = struct{}{}
		}
		for _, e := range entries2 {
			if !e.IsDir() {
				if _, ok := seen[e.Name()]; !ok {
					names = append(names, e.Name()+" (committed)")
				}
			}
		}
	}
	return names
}

// scanDependencyManifests returns the known dependency manifest files present
// at the repo root.
func scanDependencyManifests(repoPath string) []string {
	candidates := []string{
		"go.mod",
		"go.sum",
		"package.json",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"requirements.txt",
		"pyproject.toml",
		"Pipfile",
		"Pipfile.lock",
		"Cargo.toml",
		"Cargo.lock",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"Gemfile",
		"Gemfile.lock",
		"composer.json",
		"composer.lock",
	}
	var found []string
	for _, c := range candidates {
		if fileExists(repoPath, c) {
			found = append(found, c)
		}
	}
	return found
}

// computeQuality derives a 0-100 quality score from the manifest signals and
// returns both the individual signals and the total score.
func computeQuality(r RepoManifestReply) ([]QualitySignal, int) {
	signals := []QualitySignal{
		{Name: "stack detected", OK: r.Stack != "" && r.Stack != "unknown", Points: 10},
		{Name: "AGENTS.md / CLAUDE.md present", OK: r.AgentsMD.Exists, Points: 20},
		{Name: "archigraph marker in agents file", OK: r.AgentsMD.InjectedByArchigraph, Points: 20},
		{Name: ".archigraph state files present", OK: len(r.ArchigraphState) > 0, Points: 20},
		{Name: "dependency manifests present", OK: len(r.DependencyManifests) > 0, Points: 15},
		{Name: "multiple languages detected", OK: len(r.Languages) >= 1, Points: 10},
		{Name: "lock file present", OK: hasLockFile(r.DependencyManifests), Points: 5},
	}
	total := 0
	for _, s := range signals {
		if s.OK {
			total += s.Points
		}
	}
	if total > 100 {
		total = 100
	}
	return signals, total
}

// hasLockFile returns true when any of the manifests is a known lock file.
func hasLockFile(manifests []string) bool {
	lockFiles := map[string]struct{}{
		"go.sum":            {},
		"package-lock.json": {},
		"yarn.lock":         {},
		"pnpm-lock.yaml":    {},
		"Cargo.lock":        {},
		"Gemfile.lock":      {},
		"Pipfile.lock":      {},
		"composer.lock":     {},
	}
	for _, m := range manifests {
		if _, ok := lockFiles[m]; ok {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// resolveRepoPath finds the on-disk path of a repo by slug within a group.
func resolveRepoPath(group, repoSlug string) (string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == group {
			cfg, err := registry.LoadGroupConfig(g.ConfigPath)
			if err != nil {
				return "", err
			}
			for _, r := range cfg.Repos {
				if r.Slug == repoSlug {
					return r.Path, nil
				}
			}
			return "", &repoNotFoundError{group: group, repo: repoSlug}
		}
	}
	return "", &groupNotFoundError{group: group}
}

type groupNotFoundError struct{ group string }

func (e *groupNotFoundError) Error() string {
	return "group \"" + e.group + "\" not registered"
}

type repoNotFoundError struct{ group, repo string }

func (e *repoNotFoundError) Error() string {
	return "repo \"" + e.repo + "\" not registered in group \"" + e.group + "\""
}
