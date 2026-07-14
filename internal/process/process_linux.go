//go:build linux

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// FindByName returns all running processes whose command name (or full
// exe path) contains nameSubstr (case-insensitive). It reads /proc
// directly — no shell-out required.
func FindByName(nameSubstr string) ([]Info, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("readdir /proc: %w", err)
	}
	needle := strings.ToLower(nameSubstr)
	var result []Info
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a PID directory
		}
		info, err := readProcInfo(pid)
		if err != nil {
			continue // process may have exited; skip silently
		}
		nameLow := strings.ToLower(info.Name)
		exeLow := strings.ToLower(info.Exe)
		if strings.Contains(nameLow, needle) || strings.Contains(exeLow, needle) {
			result = append(result, info)
		}
	}
	return result, nil
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

// CPUPercent on Linux does NOT return a percentage despite its name — it
// returns pid's CUMULATIVE CPU-seconds since start ((utime+stime)/clk_tck from
// /proc/<pid>/stat). This is the platform inconsistency documented on the
// package-level API surface: darwin's CPUPercent returns instantaneous %cpu,
// linux's returns cumulative seconds. It is kept ONLY for the existing
// hot-loop watchdog, which caches and diffs it itself. New callers wanting a
// real, portable percentage must use CPUTimeSeconds (identical semantics on
// both platforms) and diff over wall-clock. Returns 0 on error.
func CPUPercent(pid int) (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	// Format: pid (comm) state ppid ... utime stime cutime cstime ...
	// utime is field index 13, stime is 14 (0-based after splitting).
	// comm may contain spaces; find the last ')' to split safely.
	raw := string(data)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[rp+2:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("not enough fields in /proc/%d/stat", pid)
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	clkTck := float64(100) // sysconf(_SC_CLK_TCK) — 100 on virtually all Linux
	totalSec := (utime + stime) / clkTck
	// Return total CPU-seconds — callers that need a percent should compute
	// a delta over a wall-clock interval. For the single-call watchdog use
	// case this is the accumulated value; callers should cache and diff.
	return totalSec, nil
}

// CPUTimeSeconds returns pid's CUMULATIVE CPU time (user+system) in seconds
// since the process started, read from /proc/<pid>/stat. This is the uniform
// cross-platform primitive a caller diffs over a wall-clock interval to derive
// an instantaneous CPU percentage (see the darwin/other implementations for
// the matching semantics). It is deliberately NOT a percentage — the whole
// point of the type is that its meaning is identical on every platform, unlike
// CPUPercent (which is instantaneous %cpu on darwin but cumulative-ish on
// linux and unsupported elsewhere). Returns 0 + error on any read/parse
// failure.
func CPUTimeSeconds(pid int) (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	// Format: pid (comm) state ppid ... utime stime cutime cstime ...
	// comm may contain spaces AND parentheses; split at the LAST ')' so the
	// numeric fields after it align regardless of the comm's contents.
	raw := string(data)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[rp+2:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("not enough fields in /proc/%d/stat", pid)
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	const clkTck = float64(100) // sysconf(_SC_CLK_TCK) — 100 on virtually all Linux
	return (utime + stime) / clkTck, nil
}

// RSSBytes returns the resident-set size of pid in bytes by reading
// /proc/<pid>/status. Returns 0 on error.
func RSSBytes(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, fmt.Errorf("read /proc/%d/status: %w", pid, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, _ := strconv.ParseUint(fields[1], 10, 64)
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("/proc/%d/status: VmRSS not found", pid)
}

// readProcInfo reads PID, PPID, name, and exe from /proc/<pid>/.
func readProcInfo(pid int) (Info, error) {
	base := fmt.Sprintf("/proc/%d", pid)

	// exe symlink — may fail if we lack permissions; that's OK.
	exe, _ := os.Readlink(filepath.Join(base, "exe"))

	// comm — short name, max 15 bytes.
	commData, err := os.ReadFile(filepath.Join(base, "comm"))
	if err != nil {
		return Info{}, err
	}
	name := strings.TrimSpace(string(commData))

	// stat — for PPID.
	statData, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return Info{}, err
	}
	raw := string(statData)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return Info{}, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[rp+2:])
	var ppid int
	if len(fields) >= 2 {
		ppid, _ = strconv.Atoi(fields[1])
	}

	return Info{PID: pid, PPID: ppid, Name: name, Exe: exe}, nil
}
