package dashboard

// handlers_skills.go — Skills surface (#1354)
//
// Routes registered in server.go:
//
//	GET  /api/skills/installed       — list skills present in the skills/ directory
//	GET  /api/skills/available       — static marketplace catalog
//	POST /api/skills/install         — "install" a marketplace skill (writes stub SKILL.md)
//	POST /api/skills/uninstall       — remove an installed skill directory

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// SkillMeta is the parsed frontmatter from a SKILL.md file.
type SkillMeta struct {
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
	Type        string `yaml:"type"        json:"type"`
	WhenToUse   string `yaml:"when-to-use" json:"when_to_use"`
	Version     string `yaml:"version"     json:"version"`
}

// InstalledSkill combines the parsed metadata with runtime info.
type InstalledSkill struct {
	SkillMeta
	// Slug is the directory name under skills/
	Slug string `json:"slug"`
	// LastInvokedAt is the mtime of the SKILL.md file as a proxy for last use
	// (v1: use file mtime; future: real invocation log)
	LastInvokedAt string `json:"last_invoked_at,omitempty"`
	// TotalInvocations is a stub in v1 (always 0 until metrics log lands)
	TotalInvocations int `json:"total_invocations"`
	// UpdateAvailable is true when the catalog version differs from installed
	UpdateAvailable bool `json:"update_available"`
}

// CatalogSkill describes a skill available in the marketplace.
type CatalogSkill struct {
	SkillMeta
	// Slug is the unique identifier used for install/uninstall
	Slug string `json:"slug"`
	// Source is a human-readable origin label
	Source string `json:"source"`
	// InstallURL is where the skill can be fetched in a future online version
	InstallURL string `json:"install_url,omitempty"`
	// Installed is true when a matching slug exists in skills/
	Installed bool `json:"installed"`
}

// SkillsInstalledReply is returned by GET /api/skills/installed.
type SkillsInstalledReply struct {
	Skills    []InstalledSkill `json:"skills"`
	SkillsDir string           `json:"skills_dir"`
}

// SkillsAvailableReply is returned by GET /api/skills/available.
type SkillsAvailableReply struct {
	Skills []CatalogSkill `json:"skills"`
}

// SkillInstallRequest is the body for POST /api/skills/install.
type SkillInstallRequest struct {
	Slug string `json:"slug"`
}

// SkillUninstallRequest is the body for POST /api/skills/uninstall.
type SkillUninstallRequest struct {
	Slug string `json:"slug"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Static marketplace catalog
// ─────────────────────────────────────────────────────────────────────────────

// marketplaceCatalog is the v1 static skill catalog.
// In a future version this would be fetched from a remote registry.
var marketplaceCatalog = []CatalogSkill{
	{
		Slug:   "generate-docs",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "generate-docs",
			Description: "Generate comprehensive, navigable documentation for a codebase. Produces module-organised markdown docs with cross-repo links.",
			Type:        "action",
			WhenToUse:   "When you want to create or refresh /docs for a repository or group.",
			Version:     "bundled",
		},
	},
	{
		Slug:   "using-archigraph",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "using-archigraph",
			Description: "Teaches an AI agent how to use archigraph MCP tools effectively when working on a registered codebase.",
			Type:        "behavior",
			WhenToUse:   "Invoke when opening a codebase that has archigraph indexed.",
			Version:     "bundled",
		},
	},
	{
		Slug:   "archigraph-aware-review",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "archigraph-aware-review",
			Description: "Reviews a pull request with structural awareness: callers, flows, topology impact.",
			Type:        "action",
			WhenToUse:   "Before merging a PR that touches core business logic.",
			Version:     "bundled",
		},
	},
	{
		Slug:   "archigraph-graph-enrich",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "archigraph-graph-enrich",
			Description: "Annotates the graph with structured YAML frontmatter for http_endpoint, process_flow, and message_topic entities so the dashboard panels (Paths, Flows, Topology) display rich data.",
			Type:        "action",
			WhenToUse:   "When dashboard panels are blank or after indexing new entities to populate Paths, Flows, and Topology with summaries, ranks, and gap analysis.",
		},
	},
	{
		Slug:   "archigraph-graph-quality",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "archigraph-graph-quality",
			Description: "Runs a quality audit against a group: orphan detection, N+1 patterns, dead-ends.",
			Type:        "action",
			WhenToUse:   "Periodically or before a release.",
			Version:     "bundled",
		},
	},
	{
		Slug:   "archigraph-resolve",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "archigraph-resolve",
			Description: "Applies pending repair candidates from the graph — renames, merges, deletes.",
			Type:        "action",
			WhenToUse:   "When the pending queue is non-empty and repairs have been reviewed.",
			Version:     "bundled",
		},
	},
	{
		Slug:   "archigraph-tech-docs",
		Source: "archigraph-bundled",
		SkillMeta: SkillMeta{
			Name:        "archigraph-tech-docs",
			Description: "Generates the complete technical documentation set for a registered group: per-module READMEs, API reference, cross-cutting concerns, group synthesis, cross-repo links, and pattern library.",
			Type:        "action",
			WhenToUse:   "When documenting a repo or group for engineers. Run after /archigraph-resolve and optionally /archigraph-graph-quality to confirm graph health before spending tokens.",
		},
	},
	// Third-party stub entries — demo marketplace entries
	{
		Slug:   "openapi-diff",
		Source: "community",
		SkillMeta: SkillMeta{
			Name:        "openapi-diff",
			Description: "Compares two OpenAPI specs and highlights breaking changes using archigraph path data.",
			Type:        "action",
			WhenToUse:   "When upgrading a downstream API version.",
			Version:     "0.2.1",
		},
		InstallURL: "https://skills.archigraph.dev/community/openapi-diff",
	},
	{
		Slug:   "changelog-generator",
		Source: "community",
		SkillMeta: SkillMeta{
			Name:        "changelog-generator",
			Description: "Produces a human-readable CHANGELOG from git history cross-referenced with archigraph flow data.",
			Type:        "action",
			WhenToUse:   "Before a release tag.",
			Version:     "1.0.0",
		},
		InstallURL: "https://skills.archigraph.dev/community/changelog-generator",
	},
	{
		Slug:   "security-audit",
		Source: "community",
		SkillMeta: SkillMeta{
			Name:        "security-audit",
			Description: "Deep security audit combining archigraph auth-coverage data with OWASP checklist.",
			Type:        "action",
			WhenToUse:   "Before a public API launch or compliance review.",
			Version:     "0.1.3",
		},
		InstallURL: "https://skills.archigraph.dev/community/security-audit",
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// skillsDir returns the path to the skills/ directory relative to the binary's
// working directory.  The env var ARCHIGRAPH_SKILLS_DIR overrides the default.
func skillsDir() string {
	if v := os.Getenv("ARCHIGRAPH_SKILLS_DIR"); v != "" {
		return v
	}
	return "skills"
}

// parseSkillMeta reads SKILL.md from dir and extracts YAML frontmatter.
// Returns zero-value SkillMeta when the file is absent or has no frontmatter.
func parseSkillMeta(dir string) (SkillMeta, time.Time) {
	path := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillMeta{}, time.Time{}
	}

	info, _ := os.Stat(path)
	mtime := time.Time{}
	if info != nil {
		mtime = info.ModTime()
	}

	s := string(data)
	// Frontmatter is delimited by --- lines
	if !strings.HasPrefix(s, "---") {
		return SkillMeta{}, mtime
	}
	rest := s[3:]
	end := strings.Index(rest, "---")
	if end < 0 {
		return SkillMeta{}, mtime
	}
	fmStr := strings.TrimSpace(rest[:end])

	var meta SkillMeta
	_ = yaml.Unmarshal([]byte(fmStr), &meta)
	return meta, mtime
}

// listInstalledSlugs returns the directory names in skills/.
func listInstalledSlugs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() {
			slugs = append(slugs, e.Name())
		}
	}
	return slugs, nil
}

// catalogBySlug returns the catalog entry for slug or nil.
func catalogBySlug(slug string) *CatalogSkill {
	for i := range marketplaceCatalog {
		if marketplaceCatalog[i].Slug == slug {
			return &marketplaceCatalog[i]
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/skills/installed
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSkillsInstalled(w http.ResponseWriter, r *http.Request) {
	dir := skillsDir()
	slugs, err := listInstalledSlugs(dir)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read skills dir: %v", err), http.StatusInternalServerError)
		return
	}

	skills := make([]InstalledSkill, 0, len(slugs))
	for _, slug := range slugs {
		meta, mtime := parseSkillMeta(filepath.Join(dir, slug))
		if meta.Name == "" {
			meta.Name = slug
		}
		sk := InstalledSkill{
			Slug:      slug,
			SkillMeta: meta,
		}
		if !mtime.IsZero() {
			sk.LastInvokedAt = mtime.UTC().Format(time.RFC3339)
		}
		// Check for update: if catalog has a different version string, flag it.
		if cat := catalogBySlug(slug); cat != nil && cat.Version != "" && cat.Version != "bundled" && cat.Version != meta.Version {
			sk.UpdateAvailable = true
		}
		skills = append(skills, sk)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SkillsInstalledReply{
		Skills:    skills,
		SkillsDir: dir,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/skills/available
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSkillsAvailable(w http.ResponseWriter, r *http.Request) {
	dir := skillsDir()
	installedSlugs, _ := listInstalledSlugs(dir)
	installedSet := make(map[string]bool, len(installedSlugs))
	for _, sl := range installedSlugs {
		installedSet[sl] = true
	}

	catalog := make([]CatalogSkill, len(marketplaceCatalog))
	copy(catalog, marketplaceCatalog)
	for i := range catalog {
		catalog[i].Installed = installedSet[catalog[i].Slug]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SkillsAvailableReply{Skills: catalog})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/skills/install
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	var req SkillInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" {
		http.Error(w, "body must be {\"slug\":\"<name>\"}", http.StatusBadRequest)
		return
	}

	cat := catalogBySlug(req.Slug)
	if cat == nil {
		http.Error(w, fmt.Sprintf("unknown skill %q — not in catalog", req.Slug), http.StatusNotFound)
		return
	}

	dir := skillsDir()
	destDir := filepath.Join(dir, req.Slug)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf("cannot create skill directory: %v", err), http.StatusInternalServerError)
		return
	}

	skillMD := fmt.Sprintf("---\nname: %s\ndescription: |\n  %s\ntype: %s\nwhen-to-use: |\n  %s\nversion: %s\n---\n\n# %s\n\n%s\n",
		cat.Name,
		strings.ReplaceAll(cat.Description, "\n", "\n  "),
		cat.Type,
		strings.ReplaceAll(cat.WhenToUse, "\n", "\n  "),
		cat.Version,
		cat.Name,
		cat.Description,
	)
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		http.Error(w, fmt.Sprintf("cannot write SKILL.md: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"slug": req.Slug,
		"dir":  destDir,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/skills/uninstall
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSkillsUninstall(w http.ResponseWriter, r *http.Request) {
	var req SkillUninstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" {
		http.Error(w, "body must be {\"slug\":\"<name>\"}", http.StatusBadRequest)
		return
	}

	// Reject path traversal
	if strings.Contains(req.Slug, "/") || strings.Contains(req.Slug, "..") {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}

	dir := skillsDir()
	destDir := filepath.Join(dir, req.Slug)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf("skill %q is not installed", req.Slug), http.StatusNotFound)
		return
	}

	if err := os.RemoveAll(destDir); err != nil {
		http.Error(w, fmt.Sprintf("cannot remove skill directory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"slug": req.Slug,
	})
}
