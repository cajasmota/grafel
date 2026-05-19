package dashboard

// handlers_docs.go — Docs Portal endpoints
//
//	GET /api/docs/{group}                          — doc tree
//	GET /api/docs/{group}/{path}?include=hovercards — doc page

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// DocTreeNode is one file/directory in the docs tree.
type DocTreeNode struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	IsDir    bool          `json:"is_dir"`
	Children []DocTreeNode `json:"children,omitempty"`
	Size     int64         `json:"size,omitempty"`
	ModTime  time.Time     `json:"mod_time,omitempty"`
}

// handleDocTree — GET /api/docs/{group}
func (s *Server) handleDocTree(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	docPaths, err := groupDocPaths(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	tree := []DocTreeNode{}
	recent := []map[string]any{}
	seen := map[string]bool{}

	for repoSlug, docPath := range docPaths {
		if docPath == "" {
			continue
		}
		if _, err := os.Stat(docPath); err != nil {
			continue
		}
		repoTree := buildDocTree(docPath, docPath, repoSlug)
		if repoTree != nil {
			tree = append(tree, *repoTree)
		}
		// Collect recent docs (last 5 modified markdown files per repo).
		repoRecent := recentDocs(docPath, repoSlug, 5)
		for _, rf := range repoRecent {
			key := rf["path"].(string)
			if !seen[key] {
				seen[key] = true
				recent = append(recent, rf)
			}
		}
	}

	// Sort recent by mod_time desc.
	sort.Slice(recent, func(i, j int) bool {
		ti, _ := recent[i]["mod_time"].(time.Time)
		tj, _ := recent[j]["mod_time"].(time.Time)
		return ti.After(tj)
	})
	if len(recent) > 10 {
		recent = recent[:10]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tree":         tree,
		"recent_files": recent,
	})
}

// handleDocPage — GET /api/docs/{group}/{path...}
func (s *Server) handleDocPage(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	// The {path...} wildcard captures everything after /api/docs/{group}/
	rawPath := r.PathValue("path")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	includeHovercards := r.URL.Query().Get("include") == "hovercards"

	docPaths, err := groupDocPaths(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Find the file across registered doc directories.
	for _, docPath := range docPaths {
		if docPath == "" {
			continue
		}
		// Sanitize: prevent path traversal.
		clean := filepath.Clean("/" + rawPath)
		target := filepath.Join(docPath, clean)
		if !strings.HasPrefix(target, docPath) {
			continue
		}
		data, err := os.ReadFile(target)
		if err != nil {
			continue
		}
		content := string(data)

		// Build nav breadcrumbs.
		breadcrumbs := buildBreadcrumbs(rawPath)

		resp := map[string]any{
			"markdown":    content,
			"nav_tree":    []any{},
			"breadcrumbs": breadcrumbs,
		}

		if includeHovercards {
			// Pre-resolve backticked symbols to entity cards.
			grp, gErr := s.graphs.GetGroup(group)
			if gErr == nil {
				hovercards := resolveHovercards(content, grp)
				resp["hovercards"] = hovercards
			}
		}

		writeJSON(w, http.StatusOK, resp)
		return
	}

	writeErr(w, http.StatusNotFound, "doc not found: "+rawPath)
}

// groupDocPaths returns a map of repo-slug -> docs root path for a group.
func groupDocPaths(groupName string) (map[string]string, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name != groupName {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			return nil, err
		}
		out := map[string]string{}
		for _, r := range cfg.Repos {
			// Convention: look for a "docs" dir under the repo path.
			docsDir := filepath.Join(r.Path, "docs")
			if _, err := os.Stat(docsDir); err == nil {
				out[r.Slug] = docsDir
				continue
			}
			// Fall back to the repo root itself (some repos put .md files at root).
			out[r.Slug] = r.Path
		}
		return out, nil
	}
	return nil, errNotFound(groupName)
}

func errNotFound(group string) error {
	return &notFoundErr{msg: "group not found: " + group}
}

type notFoundErr struct{ msg string }

func (e *notFoundErr) Error() string { return e.msg }

// buildDocTree walks a directory and produces a DocTreeNode tree.
func buildDocTree(root, dir, repoSlug string) *DocTreeNode {
	info, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	rel, _ := filepath.Rel(root, dir)
	if rel == "." {
		rel = ""
	}
	node := &DocTreeNode{
		Name:    filepath.Base(dir),
		Path:    repoSlug + "/" + rel,
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
	if !info.IsDir() {
		return node
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return node
	}
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files and dirs.
		if strings.HasPrefix(name, ".") {
			continue
		}
		child := buildDocTree(root, filepath.Join(dir, name), repoSlug)
		if child != nil {
			node.Children = append(node.Children, *child)
		}
	}
	return node
}

// recentDocs returns the N most recently modified markdown files under dir.
func recentDocs(dir, repoSlug string, n int) []map[string]any {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, fileInfo{path: repoSlug + "/" + rel, modTime: info.ModTime()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	if len(files) > n {
		files = files[:n]
	}
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]any{
			"path":     f.path,
			"mod_time": f.modTime,
		})
	}
	return out
}

// buildBreadcrumbs constructs a breadcrumb trail from a slash-separated path.
func buildBreadcrumbs(path string) []map[string]any {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	crumbs := make([]map[string]any, 0, len(parts))
	built := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if built != "" {
			built += "/"
		}
		built += p
		crumbs = append(crumbs, map[string]any{
			"label": p,
			"path":  built,
		})
	}
	return crumbs
}

// resolveHovercards extracts backtick-wrapped symbols from markdown and
// resolves them to entity card payloads.
func resolveHovercards(content string, grp *DashGroup) map[string]any {
	// Extract all `symbols` from the markdown.
	symbols := extractBacktickSymbols(content)
	cards := map[string]any{}
	for _, sym := range symbols {
		if _, ok := cards[sym]; ok {
			continue
		}
		_, entity := findEntity(grp, sym)
		if entity == nil {
			continue
		}
		repo := ""
		for _, r := range sortedRepos(grp) {
			for i := range r.Doc.Entities {
				if r.Doc.Entities[i].ID == entity.ID {
					repo = r.Slug
					break
				}
			}
			if repo != "" {
				break
			}
		}
		cards[sym] = map[string]any{
			"id":          dashPrefixedID(repo, entity.ID),
			"label":       entity.Name,
			"kind":        dashStripScopePrefix(entity.Kind),
			"source_file": entity.SourceFile,
			"start_line":  entity.StartLine,
			"repo":        repo,
		}
	}
	return cards
}

// extractBacktickSymbols finds all `symbol` occurrences in a markdown string.
func extractBacktickSymbols(s string) []string {
	var out []string
	seen := map[string]bool{}
	for {
		i := strings.Index(s, "`")
		if i < 0 {
			break
		}
		s = s[i+1:]
		j := strings.Index(s, "`")
		if j < 0 {
			break
		}
		sym := strings.TrimSpace(s[:j])
		s = s[j+1:]
		if sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, sym)
	}
	return out
}
