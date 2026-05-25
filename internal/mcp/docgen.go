package mcp

// docgen.go — 6 MCP tools for docgen-via-local-staging (epic #2207, issue #2214).
//
// Tools:
//   archigraph_docgen_start_run  — create/resume a staging run for a group
//   archigraph_docgen_status     — inspect an in-flight staging run
//   archigraph_docgen_validate   — lint frontmatter + links (read-only)
//   archigraph_docgen_promote    — atomic promote staging → canonical
//   archigraph_docgen_abort      — cancel and delete a staging run
//   archigraph_docgen_list       — enumerate canonical doc files for a group
//
// Staging layout:
//   <project_root>/.archigraph/staging/<run_id>/
//
// Canonical layout:
//   ~/.archigraph/docs/<group>/

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/gitmeta"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Per-group in-flight run lock
// ---------------------------------------------------------------------------

// docgenRunInfo tracks a single in-flight docgen run.
type docgenRunInfo struct {
	RunID       string `json:"run_id"`
	StagingPath string `json:"staging_path"`
	Group       string `json:"group"`
}

// docgenRunRegistry is the global registry of in-flight docgen runs keyed by
// group name. Guarded by docgenMu.
var (
	docgenMu          sync.Mutex
	docgenRunsByGroup = map[string]*docgenRunInfo{}
)

// ---------------------------------------------------------------------------
// SSG-scaffolding signatures (physical OUTPUT DISCIPLINE guard)
// ---------------------------------------------------------------------------

// ssgSignatures are file/dir names that indicate SSG scaffolding. If any of
// these exist at the root of the staging directory, promote is refused.
var ssgSignatures = []string{
	".vitepress",
	".docusaurus",
	"mkdocs.yml",
	"sphinx",
	"config.ts",
	"config.js",
	"package.json",
}

// ---------------------------------------------------------------------------
// Helper: detect git project root
// ---------------------------------------------------------------------------

// projectRootFromCWD returns the git top-level for cwd, or an error when cwd
// is not inside a git repo (and --no-git opt-in was not requested).
func projectRootFromCWD(cwd string, noGit bool) (string, error) {
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	topLevel := gitmeta.RunGit(cwd, "rev-parse", "--show-toplevel")
	if topLevel == "" {
		if noGit {
			// Not a git repo, but caller opted in — use cwd directly.
			return cwd, nil
		}
		return "", fmt.Errorf("cwd %q is not inside a git repository; use no_git=true to override", cwd)
	}
	return topLevel, nil
}

// ---------------------------------------------------------------------------
// Helper: staging directory path
// ---------------------------------------------------------------------------

// stagingDirPath returns <project_root>/.archigraph/staging/<run_id>.
func stagingDirPath(projectRoot, runID string) string {
	return filepath.Join(projectRoot, ".archigraph", "staging", runID)
}

// canonicalDocsPath returns ~/.archigraph/docs/<group>.
func canonicalDocsPath(group string) (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
	}
	return filepath.Join(home, ".archigraph", "docs", group), nil
}

// ---------------------------------------------------------------------------
// Helper: safe path within staging dir (no path traversal)
// ---------------------------------------------------------------------------

// safeStagingPath returns the absolute path of relPath relative to stagingDir,
// or an error if the cleaned path escapes stagingDir.
func safeStagingPath(stagingDir, relPath string) (string, error) {
	if relPath == "" {
		return stagingDir, nil
	}
	// Block any absolute path.
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("path traversal blocked: absolute path %q not allowed", relPath)
	}
	joined := filepath.Join(stagingDir, relPath)
	cleaned := filepath.Clean(joined)
	if !strings.HasPrefix(cleaned+string(filepath.Separator), stagingDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal blocked: %q escapes staging directory", relPath)
	}
	return cleaned, nil
}

// ---------------------------------------------------------------------------
// Helper: generate run_id
// ---------------------------------------------------------------------------

// newRunID generates a timestamped run ID, e.g. "2026-05-25-a3b4c5d6".
func newRunID() string {
	now := time.Now().UTC()
	// 8 hex chars from timestamp nanoseconds for uniqueness.
	h := sha256.Sum256([]byte(fmt.Sprintf("%d", now.UnixNano())))
	short := hex.EncodeToString(h[:])[:8]
	return fmt.Sprintf("%s-%s", now.Format("2006-01-02"), short)
}

// ---------------------------------------------------------------------------
// Helper: file SHA-256
// ---------------------------------------------------------------------------

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// archigraph_docgen_start_run
// ---------------------------------------------------------------------------

// handleDocgenStartRun implements archigraph_docgen_start_run.
//
// Parameters:
//
//	group  string  — required; target group name
//	resume bool    — when true, return existing in-flight run instead of error
//	no_git bool    — when true, skip git-repo requirement and use cwd directly
//	cwd    string  — optional caller working directory
func (s *Server) handleDocgenStartRun(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	group := argString(req, "group", "")
	if group == "" {
		return mcpapi.NewToolResultError("group is required for archigraph_docgen_start_run"), nil
	}
	resume := argBool(req, "resume", true) // default: resume if in-flight
	noGit := argBool(req, "no_git", false)
	cwd := s.inferCWD(req)

	projectRoot, err := projectRootFromCWD(cwd, noGit)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	docgenMu.Lock()
	defer docgenMu.Unlock()

	// Per-group lock: check for existing in-flight run.
	if existing, ok := docgenRunsByGroup[group]; ok {
		if !resume {
			return mcpapi.NewToolResultError(fmt.Sprintf(
				"docgen run already in progress for group %q (run_id=%s); call with resume=true or abort first",
				group, existing.RunID,
			)), nil
		}
		// Resume: return the existing run.
		return jsonResult(map[string]any{
			"run_id":       existing.RunID,
			"staging_path": existing.StagingPath,
			"resumed":      true,
		}), nil
	}

	// New run.
	runID := newRunID()
	stagingPath := stagingDirPath(projectRoot, runID)
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("create staging dir: %v", err)), nil
	}

	info := &docgenRunInfo{
		RunID:       runID,
		StagingPath: stagingPath,
		Group:       group,
	}
	docgenRunsByGroup[group] = info

	return jsonResult(map[string]any{
		"run_id":       runID,
		"staging_path": stagingPath,
		"resumed":      false,
	}), nil
}

// ---------------------------------------------------------------------------
// archigraph_docgen_status
// ---------------------------------------------------------------------------

// handleDocgenStatus implements archigraph_docgen_status.
//
// Parameters:
//
//	run_id string — required
func (s *Server) handleDocgenStatus(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	runID := argString(req, "run_id", "")
	if runID == "" {
		return mcpapi.NewToolResultError("run_id is required for archigraph_docgen_status"), nil
	}

	// Locate the run info. We check in-memory registry first for the staging
	// path; if not present we attempt to find the staging dir under any known
	// project root derived from cwd.
	stagingPath, err := resolveStagingPath(s, req, runID)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	// Walk the staging directory and compute SHAs.
	type fileEntry struct {
		RelPath string `json:"rel_path"`
		SHA256  string `json:"sha256"`
		Size    int64  `json:"size_bytes"`
	}
	var passes []fileEntry

	err = filepath.WalkDir(stagingPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stagingPath, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sha, _ := fileSHA256(path)
		passes = append(passes, fileEntry{
			RelPath: filepath.ToSlash(rel),
			SHA256:  sha,
			Size:    info.Size(),
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return mcpapi.NewToolResultError(fmt.Sprintf("walk staging dir: %v", err)), nil
	}

	shaPerFile := make(map[string]string, len(passes))
	for _, f := range passes {
		shaPerFile[f.RelPath] = f.SHA256
	}

	return jsonResult(map[string]any{
		"run_id":          runID,
		"staging_path":    stagingPath,
		"passes_complete": passes,
		"sha_per_file":    shaPerFile,
		"file_count":      len(passes),
	}), nil
}

// resolveStagingPath returns the staging path for a given run_id. It checks
// the in-memory registry first; if not found it tries to locate the directory
// on disk from the cwd-derived project root.
func resolveStagingPath(s *Server, req mcpapi.CallToolRequest, runID string) (string, error) {
	docgenMu.Lock()
	for _, info := range docgenRunsByGroup {
		if info.RunID == runID {
			path := info.StagingPath
			docgenMu.Unlock()
			return path, nil
		}
	}
	docgenMu.Unlock()

	// Not in memory — attempt disk lookup via cwd.
	noGit := argBool(req, "no_git", false)
	cwd := s.inferCWD(req)
	projectRoot, err := projectRootFromCWD(cwd, noGit)
	if err != nil {
		return "", fmt.Errorf("run_id %q not found in active runs and cannot determine project root: %w", runID, err)
	}
	candidate := stagingDirPath(projectRoot, runID)
	if _, statErr := os.Stat(candidate); statErr != nil {
		return "", fmt.Errorf("run_id %q not found (checked %s)", runID, candidate)
	}
	return candidate, nil
}

// ---------------------------------------------------------------------------
// archigraph_docgen_validate
// ---------------------------------------------------------------------------

// FrontmatterError describes a YAML frontmatter parsing problem in a staging file.
type FrontmatterError struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// CrossLinkError describes a broken relative link within the staging directory.
type CrossLinkError struct {
	File   string `json:"file"`
	Link   string `json:"link"`
	Reason string `json:"reason"`
}

// handleDocgenValidate implements archigraph_docgen_validate.
// This is read-only; it never modifies files.
//
// Parameters:
//
//	run_id string — required
func (s *Server) handleDocgenValidate(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	runID := argString(req, "run_id", "")
	if runID == "" {
		return mcpapi.NewToolResultError("run_id is required for archigraph_docgen_validate"), nil
	}

	stagingPath, err := resolveStagingPath(s, req, runID)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	fmErrors, linkErrors, err := validateStagingDir(stagingPath)
	if err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("validate: %v", err)), nil
	}

	summary := "ok"
	if len(fmErrors) > 0 || len(linkErrors) > 0 {
		summary = fmt.Sprintf("%d frontmatter error(s), %d cross-link error(s)", len(fmErrors), len(linkErrors))
	}

	return jsonResult(map[string]any{
		"run_id":             runID,
		"staging_path":       stagingPath,
		"frontmatter_errors": fmErrors,
		"cross_link_errors":  linkErrors,
		"summary":            summary,
		"has_errors":         len(fmErrors) > 0 || len(linkErrors) > 0,
	}), nil
}

// validateStagingDir walks the staging directory and validates all .md files.
// It returns frontmatter errors and cross-link errors as structured lists.
// This function is read-only.
func validateStagingDir(stagingPath string) ([]FrontmatterError, []CrossLinkError, error) {
	var fmErrors []FrontmatterError
	var linkErrors []CrossLinkError

	// Collect all .md files first (for cross-link resolution).
	mdFiles := map[string]bool{} // rel paths (slash-separated)
	err := filepath.WalkDir(stagingPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stagingPath, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(rel), ".md") {
			mdFiles[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	// Validate each .md file.
	for relSlash := range mdFiles {
		relNative := filepath.FromSlash(relSlash)
		absPath := filepath.Join(stagingPath, relNative)

		fmErrs, lkErrs := validateMarkdownFile(absPath, relSlash, stagingPath, mdFiles)
		fmErrors = append(fmErrors, fmErrs...)
		linkErrors = append(linkErrors, lkErrs...)
	}

	return fmErrors, linkErrors, nil
}

// validateMarkdownFile parses YAML frontmatter and checks relative links for
// a single markdown file.
func validateMarkdownFile(absPath, relSlash, stagingPath string, knownFiles map[string]bool) ([]FrontmatterError, []CrossLinkError) {
	var fmErrors []FrontmatterError
	var linkErrors []CrossLinkError

	f, err := os.Open(absPath)
	if err != nil {
		fmErrors = append(fmErrors, FrontmatterError{
			File:    relSlash,
			Line:    0,
			Message: "cannot open: " + err.Error(),
		})
		return fmErrors, linkErrors
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	inFrontmatter := false
	frontmatterDone := false
	var fmLines []string

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if lineNum == 1 {
			if line == "---" {
				inFrontmatter = true
				continue
			}
			// No frontmatter; skip frontmatter validation for this file.
			frontmatterDone = true
		}

		if inFrontmatter {
			if line == "---" || line == "..." {
				inFrontmatter = false
				frontmatterDone = true
				// Basic YAML validation: check for tabs (not allowed in YAML).
				for i, fmLine := range fmLines {
					if strings.ContainsRune(fmLine, '\t') {
						fmErrors = append(fmErrors, FrontmatterError{
							File:    relSlash,
							Line:    i + 2, // offset from file start
							Message: "frontmatter line contains a tab character (YAML requires spaces)",
						})
					}
					// Check for bare colons without spaces (common YAML mistake).
					if colonIdx := strings.Index(fmLine, ":"); colonIdx > 0 {
						rest := fmLine[colonIdx+1:]
						if len(rest) > 0 && rest[0] != ' ' && rest[0] != '\n' && rest[0] != '\r' {
							fmErrors = append(fmErrors, FrontmatterError{
								File:    relSlash,
								Line:    i + 2,
								Message: fmt.Sprintf("frontmatter key may be malformed: %q (colon not followed by space)", fmLine),
							})
						}
					}
				}
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}

		if !frontmatterDone {
			continue
		}

		// Scan for markdown links: [text](link) and [text][ref].
		// We look for relative links only (no http:// or https:// prefix).
		lkErrs := extractAndValidateLinks(line, lineNum, relSlash, stagingPath, knownFiles)
		linkErrors = append(linkErrors, lkErrs...)
	}

	if inFrontmatter {
		// Frontmatter never closed.
		fmErrors = append(fmErrors, FrontmatterError{
			File:    relSlash,
			Line:    lineNum,
			Message: "frontmatter block opened with '---' but never closed",
		})
	}

	return fmErrors, linkErrors
}

// extractAndValidateLinks finds markdown links in a line and checks relative
// ones against the set of known files in the staging directory.
func extractAndValidateLinks(line string, lineNum int, relSlash, stagingPath string, knownFiles map[string]bool) []CrossLinkError {
	var errors []CrossLinkError

	// Simple state-machine scan for (url) and [ref] link targets.
	i := 0
	for i < len(line) {
		// Look for ']('
		if line[i] == ']' && i+1 < len(line) && line[i+1] == '(' {
			j := i + 2
			for j < len(line) && line[j] != ')' {
				j++
			}
			if j < len(line) {
				target := line[i+2 : j]
				// Strip anchor.
				if idx := strings.Index(target, "#"); idx >= 0 {
					target = target[:idx]
				}
				target = strings.TrimSpace(target)
				if target != "" && !isAbsoluteURL(target) {
					lkErr := checkRelativeLink(target, relSlash, stagingPath, knownFiles, lineNum)
					if lkErr != nil {
						errors = append(errors, *lkErr)
					}
				}
				i = j + 1
				continue
			}
		}
		i++
	}
	return errors
}

// isAbsoluteURL returns true for http://, https://, mailto:, etc.
func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "mailto:") ||
		strings.HasPrefix(s, "ftp://") ||
		strings.HasPrefix(s, "#") // anchor-only links are fine
}

// checkRelativeLink resolves a relative link from the perspective of the given
// file (relSlash). Returns a CrossLinkError if the target doesn't exist in the
// staging directory.
func checkRelativeLink(target, fromRelSlash, stagingPath string, knownFiles map[string]bool, lineNum int) *CrossLinkError {
	// Resolve relative to the directory of the source file.
	fromDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(fromRelSlash)))
	var resolved string
	if strings.HasPrefix(target, "/") {
		// Absolute path from staging root.
		resolved = strings.TrimPrefix(target, "/")
	} else {
		if fromDir == "." {
			resolved = target
		} else {
			resolved = fromDir + "/" + target
		}
	}
	// Clean the path.
	resolved = filepath.ToSlash(filepath.Clean(resolved))
	// Check if it resolves outside staging (path traversal in links).
	if strings.HasPrefix(resolved, "../") || resolved == ".." {
		return &CrossLinkError{
			File:   fromRelSlash,
			Link:   target,
			Reason: fmt.Sprintf("link at line %d escapes staging directory (resolved: %s)", lineNum, resolved),
		}
	}
	// Check existence — try with and without .md extension.
	if knownFiles[resolved] {
		return nil
	}
	// Try appending .md if the link doesn't already have an extension.
	if !strings.Contains(filepath.Base(resolved), ".") {
		if knownFiles[resolved+".md"] {
			return nil
		}
	}
	// Check directly on disk (for non-.md files like images).
	diskPath := filepath.Join(stagingPath, filepath.FromSlash(resolved))
	if _, err := os.Stat(diskPath); err == nil {
		return nil
	}
	return &CrossLinkError{
		File:   fromRelSlash,
		Link:   target,
		Reason: fmt.Sprintf("line %d: target not found in staging directory (resolved: %s)", lineNum, resolved),
	}
}

// ---------------------------------------------------------------------------
// archigraph_docgen_promote
// ---------------------------------------------------------------------------

// handleDocgenPromote implements archigraph_docgen_promote.
// Atomic two-step rename: existing canonical → .previous-<timestamp>,
// staging → canonical. SSG scaffolding is blocked.
//
// Parameters:
//
//	run_id string — required
//	force  bool   — skip validation check (default false)
func (s *Server) handleDocgenPromote(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	runID := argString(req, "run_id", "")
	if runID == "" {
		return mcpapi.NewToolResultError("run_id is required for archigraph_docgen_promote"), nil
	}
	force := argBool(req, "force", false)

	// Find the run.
	docgenMu.Lock()
	var runInfo *docgenRunInfo
	for _, info := range docgenRunsByGroup {
		if info.RunID == runID {
			runInfo = info
			break
		}
	}
	docgenMu.Unlock()

	if runInfo == nil {
		// Try disk-based lookup.
		stagingPath, err := resolveStagingPath(s, req, runID)
		if err != nil {
			return mcpapi.NewToolResultError(err.Error()), nil
		}
		// We don't know the group from disk alone — require it as a param.
		group := argString(req, "group", "")
		if group == "" {
			return mcpapi.NewToolResultError("group is required when run is not in active registry"), nil
		}
		runInfo = &docgenRunInfo{RunID: runID, StagingPath: stagingPath, Group: group}
	}

	stagingPath := runInfo.StagingPath
	group := runInfo.Group

	// 1. SSG-scaffolding guard.
	for _, sig := range ssgSignatures {
		checkPath := filepath.Join(stagingPath, sig)
		if _, err := os.Stat(checkPath); err == nil {
			return mcpapi.NewToolResultError(fmt.Sprintf(
				"promote refused: SSG scaffolding detected (%s exists in staging directory). "+
					"Staging must contain only raw markdown and assets, not a static site generator project.",
				sig,
			)), nil
		}
	}

	// 2. Validate (unless --force).
	if !force {
		fmErrors, linkErrors, err := validateStagingDir(stagingPath)
		if err != nil {
			return mcpapi.NewToolResultError(fmt.Sprintf("pre-promote validation: %v", err)), nil
		}
		if len(fmErrors) > 0 || len(linkErrors) > 0 {
			return mcpapi.NewToolResultError(fmt.Sprintf(
				"promote refused: %d frontmatter error(s), %d cross-link error(s). "+
					"Fix errors or use force=true to bypass.",
				len(fmErrors), len(linkErrors),
			)), nil
		}
	}

	// 3. Canonical docs path.
	canonicalPath, err := canonicalDocsPath(group)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("create docs parent dir: %v", err)), nil
	}

	// 4. Count files to be promoted.
	var filesMoved []string
	_ = filepath.WalkDir(stagingPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(stagingPath, path)
		if err == nil {
			filesMoved = append(filesMoved, filepath.ToSlash(rel))
		}
		return nil
	})

	// 5. Atomic rename step 1: rotate existing canonical to .previous-<ts>.
	previousPath := ""
	if _, err := os.Stat(canonicalPath); err == nil {
		// Existing canonical; rename it to .previous-<timestamp>.
		ts := time.Now().UTC().Format("20060102T150405Z")
		previousPath = canonicalPath + ".previous-" + ts
		if err := os.Rename(canonicalPath, previousPath); err != nil {
			return mcpapi.NewToolResultError(fmt.Sprintf("rotate canonical: %v", err)), nil
		}
	}

	// 6. Atomic rename step 2: staging → canonical.
	if err := os.Rename(stagingPath, canonicalPath); err != nil {
		// If step 2 fails, attempt to restore from previous (best-effort).
		if previousPath != "" {
			_ = os.Rename(previousPath, canonicalPath)
		}
		return mcpapi.NewToolResultError(fmt.Sprintf("promote staging to canonical: %v", err)), nil
	}

	// 7. Clean up any .previous-* directories older than 7 days.
	cleanupOldPrevious(filepath.Dir(canonicalPath), group, 7*24*time.Hour)

	// 8. Release the per-group lock.
	docgenMu.Lock()
	delete(docgenRunsByGroup, group)
	docgenMu.Unlock()

	return jsonResult(map[string]any{
		"run_id":         runID,
		"canonical_path": canonicalPath,
		"previous_path":  previousPath,
		"files_moved":    filesMoved,
		"file_count":     len(filesMoved),
	}), nil
}

// cleanupOldPrevious removes .previous-* directories under docsDir that are
// older than maxAge. Errors are silently ignored (best-effort cleanup).
func cleanupOldPrevious(docsParentDir, group string, maxAge time.Duration) {
	prefix := group + ".previous-"
	entries, err := os.ReadDir(docsParentDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(docsParentDir, e.Name()))
		}
	}
}

// ---------------------------------------------------------------------------
// archigraph_docgen_abort
// ---------------------------------------------------------------------------

// handleDocgenAbort implements archigraph_docgen_abort.
// Deletes the staging directory and releases the per-group lock.
//
// Parameters:
//
//	run_id string — required
func (s *Server) handleDocgenAbort(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	runID := argString(req, "run_id", "")
	if runID == "" {
		return mcpapi.NewToolResultError("run_id is required for archigraph_docgen_abort"), nil
	}

	docgenMu.Lock()
	var runInfo *docgenRunInfo
	var foundGroup string
	for grp, info := range docgenRunsByGroup {
		if info.RunID == runID {
			runInfo = info
			foundGroup = grp
			break
		}
	}
	docgenMu.Unlock()

	stagingPath := ""
	group := foundGroup

	if runInfo != nil {
		stagingPath = runInfo.StagingPath
	} else {
		// Try disk lookup.
		var err error
		stagingPath, err = resolveStagingPath(s, req, runID)
		if err != nil {
			// Nothing to abort — return success (idempotent).
			return jsonResult(map[string]any{
				"run_id":  runID,
				"aborted": false,
				"note":    "run not found; nothing to abort",
			}), nil
		}
		group = argString(req, "group", "")
	}

	// Delete staging directory.
	var removeErr string
	if stagingPath != "" {
		if err := os.RemoveAll(stagingPath); err != nil {
			removeErr = err.Error()
		}
	}

	// Release per-group lock.
	docgenMu.Lock()
	if group != "" {
		delete(docgenRunsByGroup, group)
	}
	docgenMu.Unlock()

	result := map[string]any{
		"run_id":       runID,
		"staging_path": stagingPath,
		"aborted":      removeErr == "",
	}
	if removeErr != "" {
		result["error"] = removeErr
	}
	return jsonResult(result), nil
}

// ---------------------------------------------------------------------------
// archigraph_docgen_list
// ---------------------------------------------------------------------------

// handleDocgenList implements archigraph_docgen_list.
// Read-only enumeration of ~/.archigraph/docs/<group>/.
//
// Parameters:
//
//	group string — required
func (s *Server) handleDocgenList(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	group := argString(req, "group", "")
	if group == "" {
		return mcpapi.NewToolResultError("group is required for archigraph_docgen_list"), nil
	}

	canonicalPath, err := canonicalDocsPath(group)
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}

	type docEntry struct {
		RelPath  string `json:"rel_path"`
		AbsPath  string `json:"abs_path"`
		Size     int64  `json:"size_bytes"`
		Modified string `json:"modified"`
	}

	var docs []docEntry

	err = filepath.WalkDir(canonicalPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(canonicalPath, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		docs = append(docs, docEntry{
			RelPath:  filepath.ToSlash(rel),
			AbsPath:  path,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return mcpapi.NewToolResultError(fmt.Sprintf("list canonical docs: %v", err)), nil
	}

	if docs == nil {
		docs = []docEntry{}
	}

	return jsonResult(map[string]any{
		"group":          group,
		"canonical_path": canonicalPath,
		"files":          docs,
		"file_count":     len(docs),
	}), nil
}
