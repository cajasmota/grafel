// v2_source.go — shared "source peek" endpoint for WebUI v2 (#4499).
//
// The dashboard renders file:line refs all over the place (Taint paths, Flow
// cards, Paths "defined-in", Security findings, IaC). Each was a dead string.
// This endpoint is the get_source equivalent for the UI: given a repo-relative
// file path and a target line, it returns a window of source (or the whole file
// when small) read from the indexed repo's working tree, plus a language hint
// for client-side syntax highlighting.
//
//	GET /api/v2/groups/{id}/source?file=<rel>&line=<n>&context=<n>&repo=<slug>
//
// Resolution:
//   - The repo root comes from the group config (DashRepo.Path). A file may be
//     relative to any repo in the group; when ?repo=<slug> is given we pin to
//     that repo, otherwise we try each repo root and use the first that holds
//     the file on disk.
//   - Path traversal is guarded: the resolved absolute path MUST stay within the
//     repo root (after symlink-free Clean) or the request is rejected.
//   - language is derived from the file extension (a hint only; the client maps
//     it to a highlighter grammar).
//
// The window is [line-context, line+context] clamped to the file bounds; when
// the file is small (<= maxWholeFileLines) the whole file is returned so the
// reader has full context. line==0 (no recorded position) returns the file head.

package dashboard

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------------

// v2SourceLine is one returned source line with its absolute (1-based) number.
type v2SourceLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// v2SourceReply is the response for GET /api/v2/groups/{id}/source.
type v2SourceReply struct {
	// File is the repo-relative path that was read (as resolved).
	File string `json:"file"`
	// Repo is the slug of the repo the file was found in.
	Repo string `json:"repo"`
	// Language is the highlighter hint derived from the extension (e.g. "ts").
	Language string `json:"language"`
	// Line is the target line the caller asked to center on (echoed back).
	Line int `json:"line"`
	// StartLine is the 1-based number of the first returned line.
	StartLine int `json:"start_line"`
	// EndLine is the 1-based number of the last returned line.
	EndLine int `json:"end_line"`
	// TotalLines is the file's full line count (so the UI can show "x of N").
	TotalLines int `json:"total_lines"`
	// Truncated is true when the returned window is a slice of a larger file.
	Truncated bool `json:"truncated"`
	// Lines is the returned window.
	Lines []v2SourceLine `json:"lines"`
}

const (
	// v2SourceDefaultContext is the default number of lines shown around the
	// target line when the file is too large to return whole.
	v2SourceDefaultContext = 40
	// v2SourceMaxContext bounds the caller-supplied context to keep responses
	// reasonable.
	v2SourceMaxContext = 400
	// v2SourceWholeFileLines is the threshold below which the entire file is
	// returned (so small files render with full context, centered on the line).
	v2SourceWholeFileLines = 600
	// v2SourceMaxBytes caps the file size we will read to avoid serving huge
	// generated/minified blobs.
	v2SourceMaxBytes = 8 << 20 // 8 MiB
)

// ---------------------------------------------------------------------------
// GET /api/v2/groups/{id}/source
// ---------------------------------------------------------------------------

// handleV2Source serves a window of source from the indexed repo working tree
// so the WebUI can open a "source peek" modal for any file:line ref.
func (s *Server) handleV2Source(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeV2Err(w, http.StatusBadRequest, "group_required", "group id required")
		return
	}

	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	q := r.URL.Query()
	rawFile := strings.TrimSpace(q.Get("file"))
	if rawFile == "" {
		writeV2Err(w, http.StatusBadRequest, "file_required", "file query param required")
		return
	}

	line := 0
	if v := strings.TrimSpace(q.Get("line")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			line = n
		}
	}

	context := v2SourceDefaultContext
	if v := strings.TrimSpace(q.Get("context")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 {
			context = n
		}
	}
	if context > v2SourceMaxContext {
		context = v2SourceMaxContext
	}

	wantRepo := strings.TrimSpace(q.Get("repo"))

	abs, repoSlug, relFile, ok := resolveSourcePath(grp, rawFile, wantRepo)
	if !ok {
		writeV2Err(w, http.StatusNotFound, "source_not_found",
			"file not found in any repo of this group (or outside repo root)")
		return
	}

	info, statErr := os.Stat(abs)
	if statErr != nil || info.IsDir() {
		writeV2Err(w, http.StatusNotFound, "source_not_found", "file does not exist")
		return
	}
	if info.Size() > v2SourceMaxBytes {
		writeV2Err(w, http.StatusRequestEntityTooLarge, "source_too_large",
			"file exceeds the maximum size for source peek")
		return
	}

	all, readErr := readAllLines(abs)
	if readErr != nil {
		writeV2Err(w, http.StatusInternalServerError, "source_read_failed", readErr.Error())
		return
	}

	total := len(all)
	start, end := sourceWindow(total, line, context)

	out := make([]v2SourceLine, 0, end-start+1)
	for i := start; i <= end && i <= total; i++ {
		out = append(out, v2SourceLine{Number: i, Text: all[i-1]})
	}

	writeV2JSON(w, http.StatusOK, v2OK(v2SourceReply{
		File:       relFile,
		Repo:       repoSlug,
		Language:   languageFromExt(relFile),
		Line:       line,
		StartLine:  start,
		EndLine:    end,
		TotalLines: total,
		Truncated:  start > 1 || end < total,
		Lines:      out,
	}))
}

// resolveSourcePath maps a (possibly repo-relative) file to an absolute path on
// disk within a repo of grp, guarding against path traversal. It returns the
// absolute path, the owning repo slug, the cleaned repo-relative path, and ok.
//
// When wantRepo is set we only consider that repo; otherwise we try each repo
// root and accept the first where the file exists on disk and stays within the
// root.
func resolveSourcePath(grp *DashGroup, rawFile, wantRepo string) (abs, slug, rel string, ok bool) {
	if grp == nil {
		return "", "", "", false
	}
	// Normalise the requested path: strip any leading separators and clean it.
	// An absolute incoming path is treated as repo-relative by taking its
	// cleaned form (we never read outside a repo root).
	clean := filepath.Clean("/" + filepath.ToSlash(rawFile))
	clean = strings.TrimPrefix(clean, "/")

	tryRepo := func(repo *DashRepo) (string, string, bool) {
		if repo == nil || repo.Path == "" {
			return "", "", false
		}
		root, err := filepath.Abs(repo.Path)
		if err != nil {
			return "", "", false
		}
		candidate := filepath.Join(root, filepath.FromSlash(clean))
		// Traversal guard: the joined path must remain within root.
		rootWithSep := root + string(os.PathSeparator)
		if candidate != root && !strings.HasPrefix(candidate, rootWithSep) {
			return "", "", false
		}
		if st, e := os.Stat(candidate); e != nil || st.IsDir() {
			return "", "", false
		}
		return candidate, clean, true
	}

	if wantRepo != "" {
		if repo, found := grp.Repos[wantRepo]; found {
			if a, rl, good := tryRepo(repo); good {
				return a, wantRepo, rl, true
			}
		}
		return "", "", "", false
	}

	for s, repo := range grp.Repos {
		if a, rl, good := tryRepo(repo); good {
			return a, s, rl, true
		}
	}
	return "", "", "", false
}

// sourceWindow computes the inclusive [start,end] 1-based line window to return.
// Small files are returned whole; otherwise a window of ±context around line is
// returned (clamped to file bounds). line<=0 anchors at the file head.
func sourceWindow(total, line, context int) (start, end int) {
	if total <= 0 {
		return 1, 0
	}
	if total <= v2SourceWholeFileLines {
		return 1, total
	}
	if line <= 0 {
		end = 1 + context*2
		if end > total {
			end = total
		}
		return 1, end
	}
	start = line - context
	if start < 1 {
		start = 1
	}
	end = line + context
	if end > total {
		end = total
	}
	return start, end
}

// readAllLines reads a file into a slice of lines (newline-stripped). It uses a
// large scanner buffer so long minified lines do not error.
func readAllLines(abs string) ([]string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// languageFromExt maps a file extension to a highlighter language hint. The
// client maps these onto its highlighter grammars; unknown extensions return
// "text".
func languageFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if m, ok := sourceLangByExt[ext]; ok {
		return m
	}
	// Fall back to extension-less basenames we recognise.
	switch strings.ToLower(filepath.Base(path)) {
	case "dockerfile":
		return "docker"
	case "makefile":
		return "makefile"
	}
	return "text"
}

// sourceLangByExt maps common source extensions to highlighter language hints.
var sourceLangByExt = map[string]string{
	".ts":      "typescript",
	".tsx":     "tsx",
	".js":      "javascript",
	".jsx":     "jsx",
	".mjs":     "javascript",
	".cjs":     "javascript",
	".go":      "go",
	".py":      "python",
	".rb":      "ruby",
	".java":    "java",
	".kt":      "kotlin",
	".kts":     "kotlin",
	".cs":      "csharp",
	".cpp":     "cpp",
	".cc":      "cpp",
	".cxx":     "cpp",
	".hpp":     "cpp",
	".h":       "c",
	".c":       "c",
	".rs":      "rust",
	".php":     "php",
	".swift":   "swift",
	".scala":   "scala",
	".sql":     "sql",
	".sh":      "bash",
	".bash":    "bash",
	".zsh":     "bash",
	".yaml":    "yaml",
	".yml":     "yaml",
	".json":    "json",
	".toml":    "toml",
	".xml":     "xml",
	".html":    "html",
	".css":     "css",
	".scss":    "scss",
	".md":      "markdown",
	".tf":      "hcl",
	".hcl":     "hcl",
	".proto":   "protobuf",
	".graphql": "graphql",
	".gql":     "graphql",
	".vue":     "vue",
}
