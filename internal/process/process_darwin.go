//go:build darwin

package process

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// FindByName returns all running processes whose command name or exe
// path contains nameSubstr (case-insensitive). On macOS we use a single
// `ps -eo pid,ppid,comm` invocation — one shell-out is acceptable since
// callers use this at most once at startup (selfdefense / doctor).
func FindByName(nameSubstr string) ([]Info, error) {
	out, err := exec.Command("ps", "-eo", "pid,ppid,comm").Output()
	if err != nil {
		// Fallback: ps aux (truncates long paths).
		out, err = exec.Command("ps", "aux").Output()
		if err != nil {
			return nil, fmt.Errorf("ps: %w", err)
		}
		return parsePsAux(out, nameSubstr), nil
	}
	return parsePsEo(out, nameSubstr), nil
}

// Kill sends SIGTERM to the process with the given PID.
func Kill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("FindProcess(%d): %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal(%d, SIGTERM): %w", pid, err)
	}
	return nil
}

// ForceKill sends SIGKILL to the process with the given PID. Unlike Kill
// (SIGTERM), a process cannot ignore or handle this — used for #5710
// pidfile reclaim, where the target daemon is alive but not responding on
// its socket (e.g. wedged inside a stalled RPC) and a graceful SIGTERM
// cannot be trusted to take effect promptly.
func ForceKill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("FindProcess(%d): %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("signal(%d, SIGKILL): %w", pid, err)
	}
	return nil
}

// CPUPercent returns the instantaneous %cpu of pid via `ps -o %cpu= -p <pid>`.
// On macOS there is no /proc, so a single ps shell-out is the portable approach.
func CPUPercent(pid int) (float64, error) {
	out, err := exec.Command("ps", "-o", "%cpu=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps -o %%cpu= -p %d: %w", pid, err)
	}
	pct, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse cpu%%: %w", err)
	}
	return pct, nil
}

// CPUTimeSeconds returns pid's CUMULATIVE CPU time (user+system) in seconds
// since the process started, via `ps -o cputime= -p <pid>`. It is the uniform
// cross-platform primitive a caller diffs over a wall-clock interval to derive
// an instantaneous CPU percentage (mirrors the linux /proc-stat reader's
// semantics). Deliberately NOT a percentage — see the linux implementation's
// doc for why the cumulative form is the portable contract.
//
// `ps -o cputime` prints `[[DD-]HH:]MM:SS[.ss]` (e.g. "0:00.42", "12:34",
// "1-02:03:04"); parseCPUTime handles every shape. Returns 0 + error on any
// shell-out or parse failure.
func CPUTimeSeconds(pid int) (float64, error) {
	out, err := exec.Command("ps", "-o", "cputime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps -o cputime= -p %d: %w", pid, err)
	}
	secs, perr := parseCPUTime(strings.TrimSpace(string(out)))
	if perr != nil {
		return 0, fmt.Errorf("parse cputime %q: %w", strings.TrimSpace(string(out)), perr)
	}
	return secs, nil
}

// parseCPUTime parses the `ps -o cputime` duration format
// `[[DD-]HH:]MM:SS[.ss]` into total seconds. The days segment (if present) is
// separated from the clock by a '-'; the clock has 2 (MM:SS) or 3 (HH:MM:SS)
// colon-separated components, the last of which may carry a fractional part.
func parseCPUTime(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty cputime")
	}
	var days float64
	clock := s
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		d, err := strconv.ParseFloat(s[:dash], 64)
		if err != nil {
			return 0, fmt.Errorf("bad days %q: %w", s[:dash], err)
		}
		days = d
		clock = s[dash+1:]
	}
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("unexpected clock %q", clock)
	}
	var hours, mins, secs float64
	if len(parts) == 3 {
		h, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("bad hours %q: %w", parts[0], err)
		}
		hours = h
		parts = parts[1:]
	}
	m, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("bad minutes %q: %w", parts[0], err)
	}
	mins = m
	sc, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, fmt.Errorf("bad seconds %q: %w", parts[1], err)
	}
	secs = sc
	return days*86400 + hours*3600 + mins*60 + secs, nil
}

// RSSBytes returns the resident-set size of pid in bytes via `ps -o rss= -p <pid>`.
func RSSBytes(pid int) (uint64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps -o rss= -p %d: %w", pid, err)
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse rss: %w", err)
	}
	return kb * 1024, nil
}

// parsePsEo parses `ps -eo pid,ppid,comm` output into Info records that
// contain nameSubstr in the command name (case-insensitive).
func parsePsEo(out []byte, nameSubstr string) []Info {
	needle := strings.ToLower(nameSubstr)
	var result []Info
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // header
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[2]
		if !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		result = append(result, Info{PID: pid, PPID: ppid, Name: name, Exe: name})
	}
	return result
}

// parsePsAux parses `ps aux` output. Field layout:
// USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND...
func parsePsAux(out []byte, nameSubstr string) []Info {
	needle := strings.ToLower(nameSubstr)
	var result []Info
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		exe := fields[10]
		result = append(result, Info{PID: pid, PPID: 0, Name: exe, Exe: exe})
	}
	return result
}
