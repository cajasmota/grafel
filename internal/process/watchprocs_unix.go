//go:build darwin || linux

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// listWatchProcesses enumerates live `grafel watch <repo>` processes on
// darwin/linux via a single `ps` invocation that yields the FULL argument
// vector (so we can read the `watch <repo>` arg) and the command path (so we
// can read the executable backing the watcher). One shell-out is acceptable:
// the daemon calls this at most once per reaper sweep (every few minutes).
//
// We deliberately use `ps` on BOTH platforms here (rather than /proc on Linux):
// the format is identical, the parsing is shared, and the volume is tiny. A ps
// failure returns an error, which the caller treats as "could not enumerate"
// and skips reaping.
func listWatchProcesses() ([]WatchProc, error) {
	// pid + the full command line (with argv). `args` is the full command with
	// arguments on both BSD (macOS) and GNU/Linux ps. We request it last so any
	// embedded spaces in the path stay on the same line as the rest of argv.
	out, err := exec.Command("ps", "-ax", "-o", "pid=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parseWatchPsArgs(string(out)), nil
}

// parseWatchPsArgs parses `ps -ax -o pid=,args=` output into the subset of
// processes that are `grafel watch <repo>` invocations. Each line is
// `<pid> <argv0> <argv1> ...`. argv0 is the executable path (used as Exe), and
// the repo is the first non-flag token after the `watch` subcommand.
func parseWatchPsArgs(out string) []WatchProc {
	var result []WatchProc
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		argv := fields[1:]
		exe := argv[0]
		// Cheap pre-filter: the executable basename must look like grafel and
		// the argv must contain a `watch` token. This avoids parsing every
		// process on the box.
		if !exeBaseNameIsGrafel(baseName(exe)) {
			continue
		}
		repo, ok := parseWatchArgs(argv)
		if !ok {
			continue
		}
		result = append(result, WatchProc{PID: pid, Exe: exe, Repo: absRepo(repo)})
	}
	return result
}

// baseName returns the last path element of p without importing path/filepath
// at the call site (keeps the hot parse loop dependency-light). It mirrors
// filepath.Base for the unix separator.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
