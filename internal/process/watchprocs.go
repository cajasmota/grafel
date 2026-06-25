package process

import (
	"path/filepath"
	"strings"
)

// WatchProc is a live `grafel watch <repo>` process: its PID, the absolute
// executable backing it (when resolvable), and the repo path it targets (the
// `watch <repo>` argument). Used by the daemon to reap stale-version / orphan
// watchers (#5632).
type WatchProc struct {
	PID  int
	Exe  string
	Repo string
}

// ListWatchProcesses returns every live `grafel watch <repo>` process visible
// on this host. The platform-specific enumeration lives in
// watchprocs_unix.go (darwin/linux, via ps/proc) and watchprocs_other.go
// (Windows and unsupported platforms, which return ErrUnsupported). Callers
// treat any error — including ErrUnsupported — as "could not enumerate" and
// skip reaping rather than failing.
func ListWatchProcesses() ([]WatchProc, error) {
	return listWatchProcesses()
}

// parseWatchArgs extracts the repo argument from a tokenized `grafel watch
// <repo> [flags]` command line and reports whether this is a watch invocation
// at all. argv[0] is the program; we look for the `watch` subcommand followed
// by the first non-flag token (the repo path). Returns ("", false) when the
// command is not a `grafel watch` invocation or has no repo argument.
func parseWatchArgs(argv []string) (repo string, ok bool) {
	// Find the `watch` subcommand token.
	watchIdx := -1
	for i := 1; i < len(argv); i++ {
		if argv[i] == "watch" {
			watchIdx = i
			break
		}
	}
	if watchIdx < 0 {
		return "", false
	}
	// The repo is the first non-flag token after `watch`. Flags (e.g.
	// --interval, --group) and their values are skipped conservatively: a
	// `--flag=value` token is a single skip; a bare `--flag` consumes the next
	// token as its value. The repo is a path, never a flag.
	for i := watchIdx + 1; i < len(argv); i++ {
		tok := argv[i]
		if strings.HasPrefix(tok, "-") {
			if !strings.Contains(tok, "=") {
				i++ // bare flag consumes its value
			}
			continue
		}
		return tok, true
	}
	return "", false
}

// absRepo normalizes a repo argument to a cleaned absolute path when possible,
// falling back to a cleaned relative path. Best-effort: the watcher's matching
// against the daemon's managed set is done on cleaned absolute paths.
func absRepo(repo string) string {
	if repo == "" {
		return ""
	}
	if abs, err := filepath.Abs(repo); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(repo)
}
