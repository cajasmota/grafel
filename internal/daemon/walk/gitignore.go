// Package walk provides directory-level skip logic for the indexer.
//
// Three layers are combined at walk-time (P0 → P2 priority):
//
//	Layer 1 (P0) — .gitignore semantics at walk-time: reads the repo's
//	               root .gitignore (and nested .gitignore files) and
//	               skips entire directory subtrees that match.
//	Layer 2 (P1) — extended hard-coded skip list: a curated set of
//	               well-known build/cache directory names (iOS Pods,
//	               Android Gradle output, JS dist, Python __pycache__, etc.)
//	Layer 3 (P2) — .archigraphignore: gitignore-syntax overlay file at
//	               the repo root for user-defined skips (test fixtures,
//	               vendored code that IS committed, etc.)
//
// All layers produce a SkipResult that records which rule caused the skip
// so the --print-skipped flag can report it.
package walk

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ErrIgnoreFileTimeout is returned by ParseIgnoreFile when the underlying
// open(2) or read does not complete within the deadline. The caller should
// treat it like a missing ignore file — proceed without ignore rules.
var ErrIgnoreFileTimeout = errors.New("walk: ParseIgnoreFile timed out (fsevents kernel stall?)")

// openSlotSem bounds the number of concurrently-outstanding open workers
// inside openWithDeadline. The non-blocking open path below already prevents
// indefinite wedges in the common case, but on platforms where O_NONBLOCK
// is silently ignored by the kernel (or on a path type that doesn't honour
// it) a worker could still block. The semaphore caps the worst case at
// `cap(openSlotSem)` simultaneously leaked goroutines for the lifetime of
// the daemon — preventing the unbounded accumulation reported in #1723.
//
// We size the semaphore generously: real reindexes touch ~10^3 directories
// but the vast majority complete in microseconds, so the semaphore is only
// load-bearing when something is genuinely wedged. 64 is plenty.
var openSlotSem = make(chan struct{}, 64)

// openWithDeadline opens path and returns the file or an error.
//
// #1723: the previous version of this function spawned an unbounded worker
// goroutine per call and "let it leak" on timeout. In real reindexes
// (especially when the daemon is fsnotify-watching the same tree being
// walked) thousands of workers accumulated, the kernel ran out of resources,
// and the daemon became unresponsive. The new implementation:
//
//  1. lstats the path first — quick, non-blocking, and lets us bail out
//     immediately if the path doesn't exist or isn't a regular file.
//     Ignore files MUST be regular files; anything else (FIFO, socket,
//     device) is treated as inaccessible (ErrIgnoreFileTimeout).
//  2. uses O_NONBLOCK on the open(2) — POSIX guarantees this returns
//     without blocking. On macOS fsevents kernel stalls (issue #1721) this
//     is the actual fix: the open returns immediately instead of wedging.
//  3. caps the total concurrent worker-goroutine count via a semaphore.
//     If the semaphore is saturated (because earlier workers wedged on a
//     non-O_NONBLOCK-honouring path) we surface ErrIgnoreFileTimeout
//     immediately — no further leaks possible.
//  4. still wraps the syscall in a deadline-bounded goroutine, but the
//     bounded semaphore guarantees the leak count is at most
//     cap(openSlotSem).
//
// The returned *os.File is ready for normal blocking reads (we clear
// O_NONBLOCK after a successful open). Callers must Close it.
func openWithDeadline(path string, timeout time.Duration) (*os.File, error) {
	// Step 1: lstat. If the path doesn't exist or isn't a regular file
	// we don't need to open it at all.
	fi, err := os.Lstat(path)
	if err != nil {
		// Preserve os.IsNotExist semantics for callers.
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		// Special files (FIFO, socket, device, symlink-to-nothing) cannot
		// be ignore files. Treat as inaccessible — same as a kernel stall.
		return nil, ErrIgnoreFileTimeout
	}

	// Step 2: try to acquire a worker slot without blocking. If we can't,
	// the daemon is already saturated with leaked workers — bail rather
	// than make it worse.
	select {
	case openSlotSem <- struct{}{}:
	default:
		return nil, ErrIgnoreFileTimeout
	}

	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// Step 3: non-blocking open. POSIX guarantees open(2) with
		// O_NONBLOCK returns without blocking — this is the actual leak
		// fix for the macOS fsevents kernel-stall case.
		fd, oerr := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if oerr != nil {
			// EWOULDBLOCK / EAGAIN means "would block" — treat as stall.
			if errors.Is(oerr, syscall.EWOULDBLOCK) || errors.Is(oerr, syscall.EAGAIN) {
				ch <- result{err: ErrIgnoreFileTimeout}
				<-openSlotSem
				return
			}
			ch <- result{err: &os.PathError{Op: "open", Path: path, Err: oerr}}
			<-openSlotSem
			return
		}

		// Clear O_NONBLOCK so subsequent Read(2) calls behave normally on
		// the returned file. Failure here is non-fatal; regular files
		// don't actually need this cleared but we do it for hygiene.
		if flags, ferr := fcntlGetFl(fd); ferr == nil && (flags&syscall.O_NONBLOCK) != 0 {
			_ = fcntlSetFl(fd, flags&^syscall.O_NONBLOCK)
		}

		f := os.NewFile(uintptr(fd), path)
		ch <- result{f: f}
		<-openSlotSem
	}()

	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(timeout):
		// Defensive: open with O_NONBLOCK should never block, but if the
		// platform/kernel doesn't honour it we still cap total leaks via
		// the semaphore. The slot will be released by the worker when it
		// eventually unblocks.
		return nil, ErrIgnoreFileTimeout
	}
}

// fcntlGetFl wraps fcntl(fd, F_GETFL).
func fcntlGetFl(fd int) (int, error) {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(flags), nil
}

// fcntlSetFl wraps fcntl(fd, F_SETFL, flags).
func fcntlSetFl(fd int, flags int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFL), uintptr(flags))
	if errno != 0 {
		return errno
	}
	return nil
}

// parseIgnoreReader parses gitignore-syntax rules from r, anchored to dir
// with the given source label. It is the I/O-free core of ParseIgnoreFile.
func parseIgnoreReader(dir, source string, r io.Reader) (*IgnoreFile, error) {
	ig := &IgnoreFile{Dir: dir, Source: source}
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Trailing spaces are ignored unless escaped.
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}

		pat := ignorePattern{raw: line, lineNum: lineNum}

		// Negation: leading "!"
		if strings.HasPrefix(line, "!") {
			pat.negate = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
			line = line[1:]
		}

		// Directory-only: trailing "/"
		if strings.HasSuffix(line, "/") {
			pat.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		// Anchoring: pattern is anchored if it contains a "/" anywhere
		// except at the very end (already stripped) or at the very start.
		if strings.HasPrefix(line, "/") {
			pat.anchored = true
			line = line[1:]
		} else if strings.Contains(line, "/") {
			pat.anchored = true
		}

		pat.pattern = line
		ig.patterns = append(ig.patterns, pat)
	}
	return ig, scanner.Err()
}

// SkipResult holds the outcome for a single directory-level skip check.
type SkipResult struct {
	// Skip is true when the directory should not be entered.
	Skip bool
	// Rule is a human-readable description of the matching rule, e.g.
	// ".gitignore line 23" or "hardcoded".
	Rule string
}

// IgnoreFile is a parsed ignore file (either .gitignore or .archigraphignore).
// It contains the ordered list of patterns from the file, anchored to the
// directory that contains the file.
type IgnoreFile struct {
	// Dir is the directory that owns this ignore file (patterns are
	// relative to this directory).
	Dir string
	// Source identifies the file ("gitignore", "archigraphignore").
	Source string
	// patterns holds the compiled rules in declaration order.
	patterns []ignorePattern
}

// ignorePattern is one line from an ignore file.
type ignorePattern struct {
	// raw is the original line (for error messages).
	raw string
	// lineNum is 1-based line number in the source file.
	lineNum int
	// negate is true for lines starting with "!" — they un-ignore matches.
	negate bool
	// dirOnly is true when the pattern ends with "/" — it only matches dirs.
	dirOnly bool
	// anchored is true when the pattern contains a "/" before the final
	// "/" or before the end of string (gitignore anchoring rule).
	anchored bool
	// pattern is the normalised glob pattern (slashes, no leading /, etc.)
	pattern string
}

// ParseIgnoreFile reads a .gitignore-style file and returns a parsed
// IgnoreFile anchored to dir. If path does not exist, an empty (no-op)
// IgnoreFile is returned with no error.
//
// #1721: the open(2) is performed on a worker goroutine with a 5 s deadline to
// prevent the caller from hanging when the daemon holds fsnotify watchers on
// the same source tree (macOS fsevents kernel stall; same class as #1678).
// On timeout, an empty IgnoreFile is returned with ErrIgnoreFileTimeout — the
// walker treats this identically to a missing file and proceeds without
// ignore-rule coverage for that path (safe: upper skip layers still apply).
func ParseIgnoreFile(dir, path, source string) (*IgnoreFile, error) {
	const deadline = 5 * time.Second

	f, err := openWithDeadline(path, deadline)
	if os.IsNotExist(err) {
		return &IgnoreFile{Dir: dir, Source: source}, nil
	}
	if errors.Is(err, ErrIgnoreFileTimeout) {
		// Return empty ignore file — safe, see function doc.
		return &IgnoreFile{Dir: dir, Source: source}, ErrIgnoreFileTimeout
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Run the actual scan on a worker goroutine so that a blocking Read
	// (rare, but possible on some FS + kernel combos) is also bounded.
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	type parseOut struct {
		ig  *IgnoreFile
		err error
	}
	resCh := make(chan parseOut, 1)
	go func() {
		ig, err := parseIgnoreReader(dir, source, f)
		resCh <- parseOut{ig: ig, err: err}
	}()

	select {
	case out := <-resCh:
		return out.ig, out.err
	case <-ctx.Done():
		return &IgnoreFile{Dir: dir, Source: source}, ErrIgnoreFileTimeout
	}
}

// MatchDir reports whether an absolute directory path should be skipped
// according to this IgnoreFile. relPath is the path of the directory
// relative to the repo root (using forward slashes, no leading slash).
//
// Returns (skip=true, lineNum) when the last matching pattern is a
// positive (non-negated) rule.
func (ig *IgnoreFile) MatchDir(relPath string) (bool, int) {
	if len(ig.patterns) == 0 {
		return false, 0
	}

	// The path relative to the IgnoreFile's own dir.
	pathFromDir := relPath
	if ig.Dir != "" {
		dirRel, err := filepath.Rel(ig.Dir, filepath.Join(ig.Dir, relPath))
		if err == nil {
			pathFromDir = filepath.ToSlash(dirRel)
		}
	}
	if pathFromDir == "." {
		return false, 0
	}

	base := filepath.Base(relPath)
	// Normalise to forward slashes.
	pathFromDir = filepath.ToSlash(pathFromDir)

	matched := false
	matchedLine := 0

	for _, pat := range ig.patterns {
		var ok bool
		if pat.anchored {
			// Anchored: match against path-from-dir using full path match.
			ok = matchGlob(pat.pattern, pathFromDir)
			if !ok {
				// Also try matching against path segments (e.g. "android/build")
				ok = matchGlobPrefix(pat.pattern, pathFromDir)
			}
		} else {
			// Un-anchored: match the basename or any path component.
			ok = matchGlob(pat.pattern, base)
			if !ok {
				// Try full path too (for patterns like "**/.gradle").
				ok = matchGlob(pat.pattern, pathFromDir)
			}
		}
		if ok {
			if pat.negate {
				matched = false
				matchedLine = pat.lineNum
			} else {
				matched = true
				matchedLine = pat.lineNum
			}
		}
	}
	return matched, matchedLine
}

// matchGlob matches pat against s using gitignore-compatible glob semantics.
// "**" matches any sequence of path components.
func matchGlob(pat, s string) bool {
	// Fast path: exact match.
	if pat == s {
		return true
	}
	// Handle "**" by splitting on it and matching prefix/suffix.
	if strings.Contains(pat, "**") {
		return matchDoubleStarGlob(pat, s)
	}
	// Standard filepath.Match (handles *, ?, [range]).
	ok, err := filepath.Match(pat, s)
	if err != nil {
		return false
	}
	return ok
}

// matchGlobPrefix reports whether pat matches a prefix of path
// (so that "android/build" matches a directory named "android/build"
// even when called on nested paths like "android/build/intermediates").
func matchGlobPrefix(pat, path string) bool {
	// Try matching pat against each prefix of path.
	parts := strings.Split(path, "/")
	for i := 1; i <= len(parts); i++ {
		prefix := strings.Join(parts[:i], "/")
		if ok, _ := filepath.Match(pat, prefix); ok {
			return true
		}
	}
	return false
}

// matchDoubleStarGlob implements "**" glob matching.
// "**" matches zero or more path segments.
func matchDoubleStarGlob(pat, s string) bool {
	// Split on "**"
	parts := strings.SplitN(pat, "**", 2)
	prefix, suffix := parts[0], parts[1]

	// Trim leading/trailing slashes on suffix.
	suffix = strings.TrimPrefix(suffix, "/")

	if prefix == "" && suffix == "" {
		return true // "**" matches everything
	}

	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/")
		if !strings.HasPrefix(s, prefix) {
			return false
		}
		s = strings.TrimPrefix(s, prefix)
		s = strings.TrimPrefix(s, "/")
	}

	if suffix == "" {
		return true
	}

	// "**" can match zero or more segments. Try suffix against every
	// suffix of s.
	sParts := strings.Split(s, "/")
	for i := 0; i <= len(sParts); i++ {
		tail := strings.Join(sParts[i:], "/")
		if ok, _ := filepath.Match(suffix, tail); ok {
			return true
		}
	}
	return false
}

// IgnoreStack manages a stack of IgnoreFile values as the walker descends
// into subdirectories. On entry to each directory, call Push with any
// .gitignore/.archigraphignore found there. On exit call Pop.
type IgnoreStack struct {
	files []*IgnoreFile
}

// Push adds an IgnoreFile to the top of the stack (may be nil — Push
// handles nil silently so callers don't need to branch).
func (s *IgnoreStack) Push(ig *IgnoreFile) {
	if ig != nil {
		s.files = append(s.files, ig)
	}
}

// Pop removes the most-recently pushed IgnoreFile.
func (s *IgnoreStack) Pop() {
	if len(s.files) > 0 {
		s.files = s.files[:len(s.files)-1]
	}
}

// Match checks all stacked IgnoreFiles against a directory and returns
// the first (last-wins / most-specific) matching result. A negated
// pattern in a child IgnoreFile overrides a positive match in a parent.
// Returns (skip=true, "source:lineN") when the final decision is to skip.
func (s *IgnoreStack) Match(relPath string) (bool, string) {
	// Walk the stack from bottom (root) to top (most-nested).
	matched := false
	rule := ""
	for _, ig := range s.files {
		ok, line := ig.MatchDir(relPath)
		if ok {
			matched = true
			rule = ruleLabel(ig.Source, line)
		} else if line != 0 {
			// Negated match resets.
			matched = false
			rule = ""
		}
	}
	return matched, rule
}

func ruleLabel(source string, line int) string {
	if line > 0 {
		return source + " line " + itoa(line)
	}
	return source
}

// itoa is a minimal int-to-string without importing strconv at package level.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
