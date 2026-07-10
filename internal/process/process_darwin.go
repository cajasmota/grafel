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
