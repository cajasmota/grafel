package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/statusfile"
)

// resolveRepoForCwd maps a working directory to the registered repo that
// contains it, using a longest-path-prefix match across every repo in every
// registered fleet group. This never touches the daemon or the network — it
// only reads the local registry.json + per-group fleet configs, so it is
// itself poll-safe (bounded by a handful of small local file reads).
//
// A statusline/CLI invocation is typically made from somewhere inside a
// repo's working tree, not necessarily its root, so an exact-match-only
// resolver would fail for the common case.
func resolveRepoForCwd(cwd string) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	absCwd = filepath.Clean(absCwd)

	// NOTE: deliberately no filepath.EvalSymlinks here — statusfile.PathFor
	// (the writer's side) hashes filepath.Abs+Clean without resolving
	// symlinks, so resolving them here would make this resolver disagree
	// with the writer on macOS, where t.TempDir()/some mount points are
	// symlinked (e.g. /var -> /private/var), and produce a path whose
	// status-file hash never matches what the engine wrote.
	groups, err := registry.Groups()
	if err != nil {
		return "", fmt.Errorf("status --json: load registry: %w", err)
	}

	best := ""
	for _, g := range groups {
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue
		}
		for _, r := range cfg.Repos {
			repoAbs, err := filepath.Abs(r.Path)
			if err != nil {
				continue
			}
			repoAbs = filepath.Clean(repoAbs)
			if absCwd != repoAbs && !strings.HasPrefix(absCwd, repoAbs+string(filepath.Separator)) {
				continue
			}
			// Longest prefix wins (handles nested repos, however unlikely).
			if len(repoAbs) > len(best) {
				best = repoAbs
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("status --json: %q is not inside any registered repo", cwd)
	}
	return best, nil
}

// statusJSONResult is the shape `grafel status --json` prints. When the
// engine has never written a status file for this repo (never indexed, or
// its daemon is down) Status is "unknown" and every other field is the zero
// value — a well-formed, machine-parseable "I don't know" rather than an
// error or a hang.
type statusJSONResult struct {
	Status string `json:"status"` // "ok" | "unknown"
	*statusfile.File
}

// runStatusJSON implements `grafel status --json`: the poll-safe,
// cwd-scoped status-plane read (#5725 core, #5729-W1). It resolves cwd to a
// registered repo, then reads ONLY the on-disk statusfile sidecar — no
// daemon dial, no RPC, no socket — so it returns promptly (well under the
// 50ms budget) and can never hang, even while the daemon is mid-index for
// this repo.
func runStatusJSON(w io.Writer, cwd string) error {
	repoPath, err := resolveRepoForCwd(cwd)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)

	f, err := statusfile.Read(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return enc.Encode(statusJSONResult{Status: "unknown"})
		}
		// A corrupt/unreadable file is still "unknown" from a poll-safe
		// reader's point of view — never propagate a hard error for a
		// best-effort observability read.
		return enc.Encode(statusJSONResult{Status: "unknown"})
	}
	return enc.Encode(statusJSONResult{Status: "ok", File: f})
}
