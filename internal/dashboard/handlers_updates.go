package dashboard

// handlers_updates.go — Update / Version-management surface (OPERATIONS_PROMPT.md §6)
//
// Routes registered in server.go:
//
//	GET  /api/updates/check        — poll latest GitHub release, compare to current build
//	POST /api/updates/apply        — run `grafel update`, stream progress via SSE
//	POST /api/updates/refresh-rules — run `grafel update --refresh-rules-lite`, SSE

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/executil"
	"github.com/cajasmota/grafel/internal/version"
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

const ghReleasesURL = "https://api.github.com/repos/cajasmota/grafel/releases/latest"

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

// isNewerVersion is a best-effort semver comparison: returns true when latest
// names a strictly newer release than current, ignoring pre-release suffixes.
// Handles the common case where current="0.0.0-dev".
//
// #6070 (same defect class as the install version guard, one function away):
// this used to compare the two strings BYTE-WISE, and its callers hand it two
// differently-shaped strings — handleUpdatesCheck passes a v-STRIPPED GitHub
// tag ("0.2.1") against a raw, v-PREFIXED version.Version ("v0.2.0"). Because
// '0' (0x30) sorts before 'v' (0x76), `"0.2.1" > "v0.2.0"` is false, so the
// dashboard's update banner never appeared on ANY release build — the only
// builds that can be updated. Byte-wise comparison also got double-digit
// components backwards ("0.10.0" > "0.9.0" is false). Both are fixed by
// normalising the 'v' and comparing numerically, component by component.
func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}
	// In dev builds the current version is 0.0.0-dev; any real release is newer.
	if strings.HasSuffix(current, "-dev") {
		return true
	}
	return compareReleases(releaseComponents(latest), releaseComponents(current)) > 0
}

// releaseComponents reduces a version string to its numeric dotted components,
// tolerating a leading 'v' and discarding any pre-release / build suffix.
//
//	"v0.2.1"                → [0 2 1]
//	"0.10.0"                → [0 10 0]
//	"v0.1.9-82-gf2fb8c315"  → [0 1 9]
//
// Non-numeric components stop the parse: a version we cannot read numerically
// yields a short (or empty) component list, which compareReleases treats as
// "not newer" rather than guessing.
func releaseComponents(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	// Drop pre-release / build metadata.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// compareReleases returns >0 when a is newer than b, <0 when older, 0 when the
// two name the same release. Missing trailing components read as zero, so
// "1.2" and "1.2.0" compare equal.
func compareReleases(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/updates/apply — run `grafel update`, stream via SSE
// POST /api/updates/refresh-rules — run `grafel update --refresh-rules-lite`
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

// newUpdateCmd builds the `<self> [args...]` subprocess command.
//
// executil.NoWindow is mandatory here: this spawn happens inside the DAEMON
// process (POST /api/updates/apply and /api/updates/refresh-rules), and since
// #6320 the daemon runs with no console of its own. Windows therefore
// allocates a fresh console for any console-subsystem child — grafel.exe is
// one — and clicking "update" in the dashboard would pop a terminal window.
// CLI-invoked spawns (internal/cli/update.go) correctly do NOT do this: they
// already run attached to the user's real console. See #6325.
func newUpdateCmd(ctx context.Context, args []string) (*exec.Cmd, error) {
	selfExe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve executable: %w", err)
	}
	cmd := exec.CommandContext(ctx, selfExe, args...)
	executil.NoWindow(cmd)
	return cmd, nil
}

// defaultUpdateRunner runs `<self> update [args...]` as a subprocess.
func defaultUpdateRunner(ctx context.Context, args []string) ([]byte, error) {
	cmd, err := newUpdateCmd(ctx, args)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// streamUpdate runs `grafel update [--refresh-rules-lite]` and streams
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
