package dashboard

// handlers_updates.go — Update / Version-management surface (OPERATIONS_PROMPT.md §6)
//
// Routes registered in server.go:
//
//	GET  /api/updates/check        — poll latest GitHub release, compare to current build
//	POST /api/updates/apply        — run `archigraph update`, stream progress via SSE
//	POST /api/updates/refresh-rules — run `archigraph update --refresh-rules-lite`, SSE

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/version"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// UpdateCheckReply is returned by GET /api/updates/check.
type UpdateCheckReply struct {
	// Current binary info
	CurrentVersion string `json:"current_version"`
	CurrentCommit  string `json:"current_commit"`
	CurrentBuiltAt string `json:"current_built_at"`

	// Latest GitHub release (empty when fetch failed or no release exists)
	LatestVersion string `json:"latest_version"`
	LatestTag     string `json:"latest_tag"`
	LatestBody    string `json:"latest_body"`     // release notes (markdown)
	LatestHTMLURL string `json:"latest_html_url"` // link to GitHub release page
	PublishedAt   string `json:"published_at,omitempty"`

	// Derived
	UpdateAvailable bool   `json:"update_available"`
	FetchError      string `json:"fetch_error,omitempty"` // non-empty when GitHub fetch failed
	CheckedAt       string `json:"checked_at"`
}

// ghRelease is the minimal subset of the GitHub releases API response we need.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/updates/check
// ─────────────────────────────────────────────────────────────────────────────

const ghReleasesURL = "https://api.github.com/repos/cajasmota/archigraph/releases/latest"

func (s *Server) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	reply := UpdateCheckReply{
		CurrentVersion: version.Version,
		CurrentCommit:  version.Commit,
		CurrentBuiltAt: version.Date,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	// Fetch latest release from GitHub (5-second timeout — cheap read).
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ghReleasesURL, nil)
	if err == nil {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		// Use GITHUB_TOKEN if available to raise rate-limit ceiling.
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}

		var httpClient = &http.Client{Timeout: 5 * time.Second}
		resp, err2 := httpClient.Do(req)
		if err2 != nil {
			reply.FetchError = err2.Error()
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var rel ghRelease
				if json.NewDecoder(resp.Body).Decode(&rel) == nil && !rel.Draft && !rel.Prerelease {
					reply.LatestTag = rel.TagName
					reply.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
					reply.LatestBody = rel.Body
					reply.LatestHTMLURL = rel.HTMLURL
					reply.PublishedAt = rel.PublishedAt
					reply.UpdateAvailable = isNewerVersion(reply.LatestVersion, version.Version)
				}
			} else if resp.StatusCode == http.StatusNotFound {
				// No releases yet — not an error.
				reply.FetchError = "no releases published yet"
			} else {
				reply.FetchError = fmt.Sprintf("GitHub API returned HTTP %d", resp.StatusCode)
			}
		}
	} else {
		reply.FetchError = err.Error()
	}

	writeJSON(w, http.StatusOK, reply)
}

// isNewerVersion is a best-effort semver comparison: returns true when
// latestStr is lexicographically greater than currentStr, ignoring pre-release
// suffixes. Handles the common case where current="0.0.0-dev".
func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}
	// In dev builds the current version is 0.0.0-dev; any real release is newer.
	if strings.HasSuffix(current, "-dev") {
		return latest != ""
	}
	// Strip pre-release suffix for comparison.
	stripPre := func(v string) string {
		if idx := strings.IndexByte(v, '-'); idx >= 0 {
			return v[:idx]
		}
		return v
	}
	return stripPre(latest) > stripPre(current)
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/updates/apply — run `archigraph update`, stream via SSE
// POST /api/updates/refresh-rules — run `archigraph update --refresh-rules-lite`
// ─────────────────────────────────────────────────────────────────────────────

func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	s.streamUpdate(w, r, false)
}

func (s *Server) handleUpdatesRefreshRules(w http.ResponseWriter, r *http.Request) {
	s.streamUpdate(w, r, true)
}

// updateRunFunc is a function that runs the update command and returns
// its combined stdout+stderr output and exit error. Overridable in tests.
type updateRunFunc func(ctx context.Context, args []string) ([]byte, error)

// defaultUpdateRunner runs `<self> update [args...]` as a subprocess.
func defaultUpdateRunner(ctx context.Context, args []string) ([]byte, error) {
	selfExe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve executable: %w", err)
	}
	cmd := exec.CommandContext(ctx, selfExe, args...)
	return cmd.CombinedOutput()
}

// streamUpdate runs `archigraph update [--refresh-rules-lite]` and streams
// stdout/stderr via SSE. The update binary is run as a subprocess so this
// dashboard process itself is not replaced mid-stream.
//
// SSE event types:
//
//	event: connected   data: {"refresh_rules_only": bool}
//	event: output      data: {"line": "..."}
//	event: done        data: {"exit_code": 0}
//	event: error       data: {"message": "..."}
func (s *Server) streamUpdate(w http.ResponseWriter, r *http.Request, refreshRulesOnly bool) {
	s.streamUpdateWith(w, r, refreshRulesOnly, defaultUpdateRunner)
}

func (s *Server) streamUpdateWith(w http.ResponseWriter, r *http.Request, refreshRulesOnly bool, runner updateRunFunc) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	connData := fmt.Sprintf(`{"refresh_rules_only":%v}`, refreshRulesOnly)
	writeSSEEvent(w, "connected", connData)
	flusher.Flush()

	args := []string{"update"}
	if refreshRulesOnly {
		args = append(args, "--refresh-rules-lite")
	}

	out, runErr := runner(r.Context(), args)

	// Fan output lines as SSE events.
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		data, _ := json.Marshal(map[string]string{"line": line})
		writeSSEEvent(w, "output", string(data))
		flusher.Flush()
	}

	exitCode := 0
	if runErr != nil {
		if ee, ok2 := runErr.(*exec.ExitError); ok2 {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
	}
	writeSSEEvent(w, "done", fmt.Sprintf(`{"exit_code":%d}`, exitCode))
	flusher.Flush()
}

// jsonStr is a no-op identity to make inline string literals readable.
func jsonStr(s string) string { return s }
