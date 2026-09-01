// Package secrets — hardcoded-secret detector for source files.
//
// ScanPath walks every file under root and flags lines that appear to contain
// hardcoded credentials.  A line is suppressed when:
//   - it carries an opt-out comment:  // grafel: ignore-secret
//   - the file lives under a test directory (/test/, /tests/, /testdata/, *.test.*)
//
// Severity grades
//
//	Critical  — AWS access keys, private-key blocks
//	High      — GitHub tokens, JWT strings
//	Medium    — generic high-entropy assignment (key=<entropy>), password= patterns
//	Low       — other keyword matches without a strong entropy signal
//
// The suggested env-var name is derived from the matched variable name when
// visible (e.g. STRIPE_SECRET_KEY → STRIPE_SECRET_KEY) or synthesised from
// the pattern type.
package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/cajasmota/grafel/internal/safeio"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// Severity grades a finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Finding is one detected secret occurrence.
type Finding struct {
	// File is the path relative to the scan root.
	File string `json:"file"`
	// Line is the 1-based line number.
	Line int `json:"line"`
	// Kind is a short human-readable label for the pattern that matched.
	Kind string `json:"kind"`
	// MaskedValue is the matched value with the middle redacted (e.g. AKIA****ABCD).
	MaskedValue string `json:"masked_value"`
	// Severity is critical | high | medium | low.
	Severity Severity `json:"severity"`
	// SuggestedEnvVar is the recommended env-var name to replace this value.
	SuggestedEnvVar string `json:"suggested_env_var"`
}

// FileRollup aggregates findings per file.
type FileRollup struct {
	File     string    `json:"file"`
	Count    int       `json:"count"`
	Severity Severity  `json:"severity"` // highest severity across all findings in this file
	Findings []Finding `json:"findings"`
}

// Report is the top-level output of a scan.
type Report struct {
	// Root is the directory that was scanned.
	Root string `json:"root"`
	// TotalFindings is the count of all findings.
	TotalFindings int `json:"total_findings"`
	// BySeverity is the count per severity level.
	BySeverity map[string]int `json:"by_severity"`
	// Files contains per-file rollups (sorted by file path).
	Files []FileRollup `json:"files"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Pattern registry
// ─────────────────────────────────────────────────────────────────────────────

type pattern struct {
	name     string
	re       *regexp.Regexp
	severity Severity
	envHint  string // fallback suggested env var when no variable name is extractable
}

// Group 1 captures the secret value in every pattern below.
var patterns = []pattern{
	{
		name:     "aws_access_key",
		re:       regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),
		severity: SeverityCritical,
		envHint:  "AWS_ACCESS_KEY_ID",
	},
	{
		name:     "aws_secret_key",
		re:       regexp.MustCompile(`(?i)(?:aws[_\-]?secret[_\-]?(?:access[_\-]?)?key)\s*[=:]\s*["']?([A-Za-z0-9/+]{40})["']?`),
		severity: SeverityCritical,
		envHint:  "AWS_SECRET_ACCESS_KEY",
	},
	{
		name:     "private_key_block",
		re:       regexp.MustCompile(`(-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----)`),
		severity: SeverityCritical,
		envHint:  "PRIVATE_KEY",
	},
	{
		name:     "github_token",
		re:       regexp.MustCompile(`(ghp_[A-Za-z0-9]{36})`),
		severity: SeverityHigh,
		envHint:  "GITHUB_TOKEN",
	},
	{
		name:     "github_oauth_token",
		re:       regexp.MustCompile(`(gho_[A-Za-z0-9]{36})`),
		severity: SeverityHigh,
		envHint:  "GITHUB_TOKEN",
	},
	{
		name:     "github_app_token",
		re:       regexp.MustCompile(`(ghs_[A-Za-z0-9]{36})`),
		severity: SeverityHigh,
		envHint:  "GITHUB_APP_TOKEN",
	},
	{
		name:     "jwt_token",
		re:       regexp.MustCompile(`(eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})`),
		severity: SeverityHigh,
		envHint:  "JWT_SECRET",
	},
	{
		name:     "stripe_secret_key",
		re:       regexp.MustCompile(`(sk_live_[A-Za-z0-9]{24,})`),
		severity: SeverityHigh,
		envHint:  "STRIPE_SECRET_KEY",
	},
	{
		name:     "stripe_publishable_key",
		re:       regexp.MustCompile(`(pk_live_[A-Za-z0-9]{24,})`),
		severity: SeverityMedium,
		envHint:  "STRIPE_PUBLISHABLE_KEY",
	},
	{
		name:     "sendgrid_api_key",
		re:       regexp.MustCompile(`(SG\.[A-Za-z0-9_-]{22,}\.[A-Za-z0-9_-]{43,})`),
		severity: SeverityHigh,
		envHint:  "SENDGRID_API_KEY",
	},
	{
		name:     "slack_token",
		re:       regexp.MustCompile(`(xox[baprs]-[A-Za-z0-9-]{10,})`),
		severity: SeverityHigh,
		envHint:  "SLACK_TOKEN",
	},
	{
		name:     "generic_api_key",
		re:       regexp.MustCompile(`(?i)(?:api[_\-]?key|apikey|api[_\-]?token)\s*[=:]\s*["']?([A-Za-z0-9_\-]{16,64})["']?`),
		severity: SeverityMedium,
		envHint:  "API_KEY",
	},
	{
		name:     "generic_secret",
		re:       regexp.MustCompile(`(?i)(?:secret[_\-]?key|client[_\-]?secret|app[_\-]?secret)\s*[=:]\s*["']?([A-Za-z0-9_\-+/]{16,64})["']?`),
		severity: SeverityMedium,
		envHint:  "SECRET_KEY",
	},
	{
		name:     "password_assignment",
		re:       regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[=:]\s*["']([^"'\s]{8,})["']`),
		severity: SeverityMedium,
		envHint:  "PASSWORD",
	},
}

// varNameRe extracts a variable name that precedes the assignment.
var varNameRe = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9_]{2,})\s*[=:]`)

// ─────────────────────────────────────────────────────────────────────────────
// File-path suppression
// ─────────────────────────────────────────────────────────────────────────────

// testPathSegments are directory names that indicate test/fixture code.
var testPathSegments = []string{
	"/test/", "/tests/", "/testdata/", "/__tests__/",
	"/spec/", "/specs/", "/fixtures/", "/mocks/",
	"/e2e/", "/integration/", "/fakes/",
}

// isTestFile returns true when the file path indicates test/fixture code.
func isTestFile(rel string) bool {
	norm := filepath.ToSlash(rel)
	// directory segments
	for _, seg := range testPathSegments {
		if strings.Contains("/"+norm, seg) {
			return true
		}
	}
	// test file suffixes: foo.test.go, foo_test.go, foo.spec.ts …
	base := filepath.Base(norm)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, ".test") ||
		strings.HasSuffix(stem, ".spec") || strings.HasSuffix(stem, "-test") {
		return true
	}
	// e.g. foo.test.js
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Binary / non-text skip
// ─────────────────────────────────────────────────────────────────────────────

// skipExt is a set of file extensions we never scan.
var skipExtSet = func() map[string]bool {
	exts := []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico",
		".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z",
		".exe", ".dll", ".so", ".dylib", ".a", ".o",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".mp3", ".mp4", ".ogg", ".wav", ".avi",
		".db", ".sqlite", ".sqlite3",
		".bin", ".dat", ".class", ".pyc",
	}
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		m[e] = true
	}
	return m
}()

func skipFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return skipExtSet[ext]
}

// ─────────────────────────────────────────────────────────────────────────────
// Entropy helper
// ─────────────────────────────────────────────────────────────────────────────

const entropyThreshold = 3.4

// shannonEntropy computes the Shannon entropy of a string.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int)
	for _, c := range s {
		freq[c]++
	}
	n := float64(len([]rune(s)))
	var h float64
	for _, c := range freq {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// highEntropy returns true when the string has Shannon entropy > threshold and
// length > 16 and looks like it contains non-trivial characters.
func highEntropy(s string) bool {
	if len(s) < 16 {
		return false
	}
	// Must contain at least some variety (not all same class).
	hasLower, hasUpper, hasDigit := false, false, false
	for _, r := range s {
		if unicode.IsLower(r) {
			hasLower = true
		} else if unicode.IsUpper(r) {
			hasUpper = true
		} else if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	varieties := 0
	if hasLower {
		varieties++
	}
	if hasUpper {
		varieties++
	}
	if hasDigit {
		varieties++
	}
	if varieties < 2 {
		return false
	}
	return shannonEntropy(s) > entropyThreshold
}

// highEntropyAssignment detects lines like:
//
//	SOME_VAR = "aBcD1234eFgH5678"
//
// where the RHS is a high-entropy string not already caught by named patterns.
var highEntropyLineRe = regexp.MustCompile(`(?i)(?:secret|key|token|password|credential|auth|passwd|pwd|api)\s*[=:]\s*["']([^"'\s]{16,})["']`)

// ─────────────────────────────────────────────────────────────────────────────
// Value masking
// ─────────────────────────────────────────────────────────────────────────────

// maskValue replaces the middle of a secret with asterisks, keeping the first
// and last 4 characters so the report is useful without leaking the full value.
// Strings shorter than 8 chars are fully masked.
func maskValue(v string) string {
	r := []rune(v)
	n := len(r)
	if n < 8 {
		return strings.Repeat("*", n)
	}
	if n == 8 {
		prefix := string(r[:4])
		return prefix + strings.Repeat("*", 4)
	}
	prefix := string(r[:4])
	suffix := string(r[n-4:])
	return prefix + strings.Repeat("*", n-8) + suffix
}

// ─────────────────────────────────────────────────────────────────────────────
// Env-var name suggestion
// ─────────────────────────────────────────────────────────────────────────────

// suggestEnvVar derives a suggested env-var name from the assignment context.
// It prefers extracting the variable name from the line; falls back to the
// pattern hint.
func suggestEnvVar(line, hint string) string {
	// Try to find "SOME_IDENTIFIER =" on the left of the match.
	m := varNameRe.FindStringSubmatch(line)
	if len(m) > 1 {
		candidate := strings.ToUpper(m[1])
		// Skip common language keywords that look like assignments.
		skip := map[string]bool{
			"IF": true, "FOR": true, "WHILE": true, "RETURN": true,
			"CONST": true, "LET": true, "VAR": true, "DEF": true,
			"TRUE": true, "FALSE": true, "NIL": true, "NULL": true,
		}
		if !skip[candidate] && len(candidate) >= 3 {
			return candidate
		}
	}
	return hint
}

// ─────────────────────────────────────────────────────────────────────────────
// Suppression constants
// ─────────────────────────────────────────────────────────────────────────────

const ignoreComment = "grafel: ignore-secret"

// ─────────────────────────────────────────────────────────────────────────────
// Scanner
// ─────────────────────────────────────────────────────────────────────────────

// Skip is one file ScanPath did NOT read, and why (#6483).
//
// It is modelled on walk.SkipEntry — the same shape, the same human-facing
// kind vocabulary (safeio.Kind: named-pipe / device / socket / other) — rather
// than inventing a second vocabulary for the same idea one package over.
type Skip struct {
	// Path is the absolute path of the file that was not read. ErrWouldBlock
	// is returned BARE by safeio, carrying no path of its own, so this is set
	// from the walk's own context rather than parsed out of the error text.
	Path string `json:"path"`
	// Rel is Path relative to the scan root — what a caller displays.
	Rel string `json:"rel"`
	// Reason is one of SkipNotRegular, SkipWouldBlock, SkipTooLarge,
	// SkipLineTooLong.
	Reason string `json:"reason"`
	// Kind names the entry type for SkipNotRegular ("named-pipe", "device",
	// "socket", "other"); empty for the other reasons.
	Kind string `json:"kind,omitempty"`
}

// The closed skip vocabulary. It is deliberately FOUR reasons, not eight.
//
// The extension denylist (skipFile) and isTestFile are by-design filters that
// would contribute thousands of entries on any real repo and drown the signal
// — the same argument maxSecretSkipReports already makes for the stderr line.
// Ordinary ENOENT/EACCES is likewise excluded: files are deleted between
// readdir and open all the time on a live tree.
const (
	// SkipNotRegular: safeio refused a named pipe, device or socket.
	SkipNotRegular = "not_regular"
	// SkipWouldBlock: the open would have blocked (TOCTOU FIFO swap).
	SkipWouldBlock = "would_block"
	// SkipTooLarge: the file was over maxFileBytes. This one was previously
	// reported NOWHERE, not even on stderr, and it is the dangerous one: the
	// dashboard exposes max_size as a query parameter, so max_size=1024 could
	// return an unqualified "clean" for a repo that was almost entirely
	// unread.
	SkipTooLarge = "too_large"
	// SkipLineTooLong: bufio.Scanner refused a line over
	// bufio.MaxScanTokenSize (64 KB), so the file was read only up to that
	// line. It is a FOURTH reason rather than a Kind on SkipTooLarge because
	// it is a different fact: too_large means "not opened at all, because of
	// a cap the CALLER chose", while line_too_long means "opened, partly
	// read, stopped at a hard limit inside bufio that no caller can raise".
	// It is also the most reachable of the four: 64 KB is one eighth of the
	// default file cap, and skipFile denies no .json, .lock, .map or
	// minified .js — the files that actually carry >64 KB lines.
	SkipLineTooLong = "line_too_long"
)

// ScanResult is what ScanPath returns.
//
// It is a struct rather than a third ([]Finding, []Skip, error) return value
// on purpose: a third return is trivially `_`-discarded, which is the exact
// failure mode #6483 fixes. A struct forces every call site to be touched to
// compile, so the compiler performs the audit.
type ScanResult struct {
	// Findings are the secrets that were found.
	Findings []Finding
	// Skipped are the files that were NOT read. A caller that reports
	// "clean" without consulting this is answering a question it did not ask:
	// "I did not check this file" and "I checked it and it was clean" are not
	// interchangeable answers from a secret scanner.
	Skipped []Skip
	// Unread are the directories the walk could not OPEN at all (#6534).
	//
	// It is a second list rather than a fifth Skip reason because it is a
	// different fact with a different shape. Skip names one file the scanner
	// declined to read and can say why; an unreadable directory names no file
	// at all — WalkDir hands over the directory path and no listing — so the
	// only honest thing to report is that a subtree of unknown size was never
	// looked at.
	//
	// Keeping the two apart also keeps the four-reason vocabulary closed: a
	// caller that renders Skipped per-file does not suddenly have to handle
	// an entry whose Path is a directory and whose Kind is meaningless.
	Unread []UnreadDir
}

// UnreadCount is the number of directories the scan could not open.
//
// It is the count the owner's decision on #6534 requires the result to carry.
// It counts DIRECTORIES, not files: the file population under an unopenable
// directory is unknowable by construction, and inventing an estimate would be
// a worse lie than the silent drop it replaces.
func (r ScanResult) UnreadCount() int { return len(r.Unread) }

// Complete reports whether the scan actually read the whole tree.
//
// This is the distinct non-clean outcome #6534 asks for. "Found nothing" is
// Complete() && len(Findings) == 0; "looked at nothing" is !Complete(). A
// caller that renders those two identically is telling a user a tree is safe
// on the strength of files it never opened.
func (r ScanResult) Complete() bool { return len(r.Unread) == 0 }

// UnreadDir is one directory the walk could not open (#6534).
//
// It carries no Reason: there is exactly one condition that produces it —
// fs.ErrPermission on a directory — and a single-valued field is noise.
type UnreadDir struct {
	// Path is the absolute path of the directory that could not be opened.
	Path string `json:"path"`
	// Rel is Path relative to the scan root — what a caller displays.
	Rel string `json:"rel"`
}

// ScanPath walks root and returns all secret findings, plus every file the
// walk refused to read (#6483).
// maxFileBytes limits the size of a single file to scan (0 = 512 KB default).
func ScanPath(root string, maxFileBytes int64) (ScanResult, error) {
	if maxFileBytes <= 0 {
		maxFileBytes = 512 * 1024
	}

	var findings []Finding
	var skipped []Skip
	var unread []UnreadDir

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// An entry the walk itself could not read — most often an
			// EACCES directory, which is N unread FILES, not one.
			//
			// This used to return nil unreported (#6483's comment argued the
			// case: the entry can name no file, and the count it implies is
			// unknown). #6534 overrules that: a scanner whose caller cannot
			// tell "scanned a tree with no secrets" from "scanned a tree with
			// a subtree it could not open" is actively misleading, because
			// users act on a clean secrets result. The unknowable file count
			// is not a reason to report nothing — it is a reason to report
			// the directory and let Complete() carry the verdict.
			//
			// The fs.ErrPermission gate is the one that carries weight: it
			// keeps the ordinary mid-walk ENOENT silent (files and dirs
			// vanish under a live tree all the time), which is what stops
			// this becoming the permanent, unactionable noise the old
			// comment feared.
			//
			// The `d == nil || d.IsDir()` half is DEFENCE IN DEPTH, not a
			// live discriminator, and the next reader should not mistake it
			// for one. fs.WalkDirFunc's contract enumerates exactly two
			// cases in which fn is called with a non-nil err:
			//
			//   1. the initial Stat/Lstat of root fails — "d set to nil";
			//   2. a directory's ReadDir fails — "d set to a DirEntry
			//      describing the DIRECTORY".
			//
			// There is no third case, so `d != nil && !d.IsDir()` cannot
			// co-occur with a non-nil err, and dropping `d.IsDir()` from
			// this condition is an EQUIVALENT MUTANT under the current
			// suite — measured, not assumed: a mode-000 FILE lstats fine and
			// never reaches this callback with an error at all (it fails
			// later, inside scanFile's safeio.Open, which is where
			// TestScanPathDoesNotReportUnreadableFileAsSkip observes it).
			// Dangling symlinks and symlinks into an unreadable directory
			// were probed too and produce no error callback either, because
			// WalkDir does not follow symlinks. The conjunct is kept so that
			// a future walk change, or a caller passing a different
			// fs.FS-backed walker, cannot silently start counting files as
			// directories — the claim "this counts DIRECTORIES, not files"
			// stays true by construction rather than by the callers' good
			// behaviour.
			//
			// The d == nil arm, by contrast, IS live and is its own bug fix.
			// When LSTAT(root) fails with EACCES — a repo inside a
			// non-searchable parent — WalkDir calls fn once with d nil and
			// then returns whatever fn returned. Since this callback returns
			// nil, WalkDir returns nil, and before this arm existed ScanPath
			// handed back an empty ScanResult and a nil error: a completely
			// unscanned repo rendering as clean, which is the #6534 defect
			// with the root standing in for the subtree.
			if errors.Is(err, fs.ErrPermission) && (d == nil || d.IsDir()) {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				unread = append(unread, UnreadDir{Path: path, Rel: rel})
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden dirs and common non-source dirs.
			if strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || name == "dist" || name == "build" ||
				name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(root, path)

		// Skip binary / non-text files.
		if skipFile(path) {
			return nil
		}

		// NOTE (#6416): there is deliberately NO entry-type gate here, even
		// though this is the second independent WalkDir in grafel that
		// branches only on d.IsDir(). The guard lives at the single place that
		// opens the file — scanFile's safeio.Open — because a stat gate here
		// could only ever duplicate it: it would be unfalsifiable (removing it
		// changes no observable behaviour, so no test can pin it) while still
		// leaving the stat/open race that only the open site can close.
		//
		// It matters more on this path than on the index path: ScanPath is
		// reachable from the daemon's MCP secrets tool and from an HTTP
		// dashboard handler, so a caller who never runs an index can wedge a
		// daemon goroutine. Neither guard below helps — d.Info().Size() is 0
		// for a FIFO so the size check passes, and skipFile is only an
		// extension denylist.

		// Skip test files.
		if isTestFile(rel) {
			return nil
		}

		// Skip large files. Reported to the caller (#6483): the size cap is
		// caller-supplied — the dashboard takes it straight off a query
		// string — so a silent drop here lets a caller manufacture a "clean"
		// repo by shrinking the cap.
		info, err := d.Info()
		if err != nil {
			// The entry vanished between readdir and stat. Ordinary on a live
			// tree; not a skip worth reporting.
			return nil
		}

		// The gate must weigh the bytes that will actually be READ (#6507).
		//
		// d.Info() is an LSTAT: for a symlink it reports the link's own size,
		// a few dozen bytes, never the target's. scanFile then opens with
		// safeio.FollowSymlinks and reads the target in full — so before this
		// re-stat the cap gated nothing at all on a symlinked entry, and a
		// link to an arbitrarily large file sailed through any max_size.
		//
		// The consequence was worse than a bypass, because it landed the
		// entry in the WRONG skip bucket. A ~70 KB target under max_size=1024
		// was opened, read, and reported as line_too_long — a bufio limit no
		// caller can raise — when the truthful answer was too_large, the cap
		// the caller itself chose and can raise. Both readings mislead: that
		// nothing can be done, or that the file was within budget.
		//
		// Statting the TARGET rather than skipping symlinks outright: the
		// scanner follows them deliberately, and monorepo and vendor link
		// farms are the ordinary case, not the exotic one — refusing them
		// would trade a size-gate bug for a coverage hole. The extra stat is
		// paid only on symlinked entries, so the common path is unchanged.
		//
		// info itself is NOT reassigned: its Mode() is handed to
		// classifyScanSkip, which documents that it receives the walk's lstat
		// mode and does its own follow-stat on the error path only.
		sizeInfo := info
		if info.Mode()&os.ModeSymlink != 0 {
			ti, terr := os.Stat(path)
			if terr != nil {
				// Dangling or unreachable target. The same class as the
				// vanished-entry case above — an ordinary condition on a live
				// tree, not a file withheld from the caller — so it stays
				// silent rather than manufacturing a skip. safeio.Open would
				// fail on it a moment later anyway.
				return nil
			}
			sizeInfo = ti
		}

		// IsRegular, because the gate weighs BYTES THAT WILL BE READ and a
		// non-regular target has none. os.Stat on a symlink to a DIRECTORY
		// answers with the directory's own size — a filesystem artefact, 64
		// bytes on APFS but at least 4096 on ext4 and larger for a directory
		// with many entries — which meets any modest cap. Without this guard
		// the entry came back too_large with Kind empty, telling the caller to
		// raise max_size for something no cap will ever make scannable: the
		// same wrong-bucket defect #6507 is about, on the path #6507's own fix
		// introduced.
		//
		// It FALLS THROUGH rather than skipping here, so the naming stays with
		// the single authority that already owns it: safeio.Open refuses the
		// entry and classifyScanSkip reports not_regular with Kind=directory.
		// An early skip at this site would have to re-derive that vocabulary,
		// and would reintroduce exactly the duplicate entry-type gate the
		// NOTE (#6416) above rules out.
		if sizeInfo.Mode().IsRegular() && sizeInfo.Size() > maxFileBytes {
			skipped = append(skipped, Skip{Path: path, Rel: rel, Reason: SkipTooLarge})
			return nil
		}

		ff, err := scanFile(path, rel)
		if err != nil {
			// Ordinary unreadable files (deleted mid-walk, no permission)
			// stay silent. The two errors that mean "this file was refused
			// for being non-regular" are announced by scanFile's
			// reportSecretScanSkip before we get here (#6416), and since
			// #6483 they also reach the CALLER through ScanResult.Skipped —
			// stderr is the daemon log for both the MCP and dashboard entry
			// points, and neither client ever reads it.
			if sk, ok := classifyScanSkip(path, rel, info.Mode(), err); ok {
				skipped = append(skipped, sk)
			}
			// bufio.ErrTooLong is the one error here that is a PARTIAL read
			// rather than a failed open, so ff is kept. Everything scanFile
			// matched before the oversized line was matched against real
			// bytes; discarding it turns a found secret into a silent
			// "clean", which is the same defect #6483 fixes one layer up.
			//
			// The other errors deliberately drop ff. safeio.Open failures
			// (ErrNotRegular, ErrWouldBlock) never read a byte, so ff is
			// empty anyway; a mid-read I/O error means the bytes themselves
			// are in doubt, and a finding attributed to a line number the
			// scanner may have mis-tracked is worse than the skip entry that
			// tells the caller to look again.
			if keepPartialFindings(err) {
				findings = append(findings, ff...)
			}
			return nil
		}
		findings = append(findings, ff...)
		return nil
	})

	return ScanResult{Findings: findings, Skipped: skipped, Unread: unread}, err
}

// keepPartialFindings decides whether the findings scanFile collected BEFORE
// it failed may still be reported (#6505).
//
// Only bufio.ErrTooLong qualifies, and the asymmetry is the whole point:
//
//   - ErrTooLong is a PARTIAL READ of a file safeio opened perfectly well.
//     Every line before the overlong one was delivered exactly once and matched
//     against real bytes, and the line counter is not perturbed by the line the
//     scanner refused — so both the findings and their line numbers are sound.
//     Dropping them turns a found credential into a silent "clean", which is the
//     defect #6483 fixes one layer up and #6505 filed against this exact site.
//
//   - safeio.Open failures (ErrNotRegular, ErrWouldBlock) never read a byte, so
//     ff is empty and keeping it would be a no-op — which is precisely why a
//     mutant that widens this predicate to `return true` is INVISIBLE
//     end-to-end and must be pinned here instead.
//
//   - A mid-read I/O error puts the bytes themselves in doubt, and with them
//     the line numbers. A finding attributed to a line the scanner may have
//     mis-tracked is worse than the skip entry telling the caller to look
//     again, so those are dropped even though ff may be non-empty.
//
// It is a named predicate rather than an inline errors.Is for the reason
// TestClassifyScanSkipReportsWouldBlock gives for its own arm: no portable
// fixture can provoke a mid-read I/O error through os/filepath, so the
// distinction is unfalsifiable end-to-end and would otherwise be pinned
// nowhere.
//
// Extraction moved the assertion; it did not close the gap. The CALL SITE above
// is still unpinned — replacing `if keepPartialFindings(err)` with `if true`
// leaves the package green — because the one error that would tell the two
// apart cannot be reached from a test. That mutant is equivalent under the
// suite, not dead, and TestKeepPartialFindingsIsExclusiveToErrTooLong says so
// in as many words.
func keepPartialFindings(err error) bool {
	return errors.Is(err, bufio.ErrTooLong)
}

// classifyScanSkip maps a scanFile error onto the closed skip vocabulary, or
// reports ok=false for the ordinary errors that must stay silent.
//
// Two of its three arms are safeio's own sentinels, the same pair
// reportSecretScanSkip gates on: safeio has only ErrNotRegular and
// ErrWouldBlock, and no concurrency error — openSlots is a bounded wait, not
// fail-fast. The third comes from bufio rather than safeio, because the
// scanner can stop early on a file safeio opened perfectly well. There is no
// ErrTooLarge sentinel: that gate lives in the walk above, which never calls
// scanFile at all.
func classifyScanSkip(path, rel string, mode os.FileMode, err error) (Skip, bool) {
	switch {
	case errors.Is(err, safeio.ErrNotRegular):
		// mode comes from WalkDir's lstat, so a symlink TO a fifo reads as
		// ModeSymlink here. safeio.Open followed the link to make its
		// decision, so re-stat once (error path only) to name what it saw.
		kind := safeio.Kind(mode)
		if fi, serr := os.Stat(path); serr == nil {
			kind = safeio.Kind(fi.Mode())
		}
		return Skip{Path: path, Rel: rel, Reason: SkipNotRegular, Kind: kind}, true
	case errors.Is(err, safeio.ErrWouldBlock):
		// ErrWouldBlock is returned bare, with no path in it. Path is set
		// from the walk's own context rather than scraped from the message.
		return Skip{Path: path, Rel: rel, Reason: SkipWouldBlock}, true
	case errors.Is(err, bufio.ErrTooLong):
		// The file WAS opened and partially read; the scanner gave up at a
		// line over bufio.MaxScanTokenSize. Kind stays empty — this is a
		// regular file, and naming its entry type would say nothing.
		return Skip{Path: path, Rel: rel, Reason: SkipLineTooLong}, true
	}
	return Skip{}, false
}

// ─────────────────────────────────────────────────────────────────────────────
// Skip reporting (#6416)
// ─────────────────────────────────────────────────────────────────────────────

// secretSkip* back the always-on report below.
var (
	secretSkipMu   sync.Mutex
	secretSkipSeen map[string]bool
	secretSkipOut  io.Writer = os.Stderr
)

// maxSecretSkipReports caps the report the same way walk.IrregularSkipReport,
// reportAliasSkip and reportGoModSkip cap theirs: a warning long enough to
// scroll past reports nothing. The cap earns more here than at the
// name-chosen read sites: those see at most a handful of paths per repo, while
// ScanPath walks a whole tree and a tree full of device nodes would otherwise
// produce a line per entry.
const maxSecretSkipReports = 16

// setSecretSkipOutput redirects the report for tests and returns a restore
// func. Test-only helper.
func setSecretSkipOutput(w io.Writer) func() {
	secretSkipMu.Lock()
	prev := secretSkipOut
	secretSkipOut = w
	secretSkipSeen = nil
	secretSkipMu.Unlock()
	return func() {
		secretSkipMu.Lock()
		secretSkipOut = prev
		secretSkipSeen = nil
		secretSkipMu.Unlock()
	}
}

// reportSecretScanSkip says out loud that a file was refused for being a FIFO,
// device or socket.
//
// It exists because ScanPath's walk mapped BOTH safeio.ErrNotRegular and
// safeio.ErrWouldBlock to `return nil // skip unreadable files silently`. The
// hang was closed but the skip was announced nowhere, so `mkfifo creds.go`
// produced a scan that reported "no secrets found" having never looked at
// creds.go — a file the user can see in their own tree. That is precisely the
// #6338 shape this PR invokes as its own rationale, and the shape the walker
// half of this PR was changed to stop producing.
//
// It matters more here than at the extractor read sites. ScanPath answers "is
// this repo clean?" for a human, and it is reachable from the daemon's MCP
// secrets tool (internal/mcp/secrets_tools.go) and an HTTP dashboard handler
// (internal/dashboard/handlers_secrets.go), so a caller who never runs an index
// can get a clean bill of health for a tree that was only partly read.
//
// CHANNEL NOTE (updated by #6483). This is the stderr channel, and it is no
// longer the ONLY one: ScanPath now also returns the skip to its caller in
// ScanResult.Skipped, which is what the MCP tool and the dashboard handler
// surface in their payloads. Both channels are kept — stderr is the operator's
// view of a daemon that is walking a tree, the return value is the asking
// client's view — and they are deliberately capped differently: this report is
// capped at maxSecretSkipReports and deduped by path, while the returned list
// is not, because a caller that renders it can decide for itself.
//
// Only ErrNotRegular / ErrWouldBlock are reported. ENOENT is the ordinary case
// on a walk — files are deleted between readdir and open all the time — and
// announcing it would bury the signal.
func reportSecretScanSkip(path string, err error) {
	if !errors.Is(err, safeio.ErrNotRegular) && !errors.Is(err, safeio.ErrWouldBlock) {
		return
	}
	secretSkipMu.Lock()
	if secretSkipSeen == nil {
		secretSkipSeen = map[string]bool{}
	}
	if secretSkipSeen[path] || len(secretSkipSeen) >= maxSecretSkipReports {
		secretSkipMu.Unlock()
		return
	}
	secretSkipSeen[path] = true
	last := len(secretSkipSeen) == maxSecretSkipReports
	w := secretSkipOut
	secretSkipMu.Unlock()

	fmt.Fprintf(w, "grafel: skipped secret scan of %v — not read because reading one can block forever; this file was NOT checked for secrets (#6416)\n", withPath(path, err))
	if last {
		fmt.Fprintf(w, "grafel: further secret-scan skips suppressed after %d\n", maxSecretSkipReports)
	}
}

// withPath makes a skip line attributable.
//
// safeio's two reportable errors are not shaped alike: ErrNotRegular is wrapped
// with the path and the entry kind, but ErrWouldBlock is returned BARE from
// openWithDeadline's two deadline arms. Printing it unadorned gives "skipped
// secret scan of safeio: open would block", which names no file and so tells a
// user nothing they can act on — the same silence the report exists to end.
// Only the bare form is decorated, so ErrNotRegular's own wording is left alone
// rather than printing its path twice.
func withPath(path string, err error) error {
	if errors.Is(err, safeio.ErrWouldBlock) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return err
}

// scanFile reads one file and returns all findings in it.
func scanFile(path, rel string) ([]Finding, error) {
	// safeio.Open, not os.Open (#6416). This is the ONE guard on this path:
	// os.Open on a FIFO named `creds.go` waits for a writer that never comes,
	// and ScanPath's walk hands the path straight here. safeio refuses
	// anything that is not a regular file and opens with O_NONBLOCK plus an
	// fstat on the descriptor, so a path swapped under it cannot hang either.
	f, err := safeio.Open(path, safeio.FollowSymlinks)
	if err != nil {
		reportSecretScanSkip(path, err)
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Opt-out comment suppresses this line.
		if strings.Contains(line, ignoreComment) {
			continue
		}

		// Try named patterns.
		matched := false
		for _, p := range patterns {
			m := p.re.FindStringSubmatch(line)
			if len(m) < 2 {
				continue
			}
			value := m[1]
			// Skip obviously placeholder / example values.
			if isPlaceholder(value) {
				continue
			}
			findings = append(findings, Finding{
				File:            rel,
				Line:            lineNum,
				Kind:            p.name,
				MaskedValue:     maskValue(value),
				Severity:        p.severity,
				SuggestedEnvVar: suggestEnvVar(line, p.envHint),
			})
			matched = true
			break // one finding per line is enough; most severe pattern wins
		}

		if matched {
			continue
		}

		// Entropy-based catch-all: look for high-entropy assignments.
		em := highEntropyLineRe.FindStringSubmatch(line)
		if len(em) >= 2 {
			value := em[1]
			if !isPlaceholder(value) && highEntropy(value) {
				findings = append(findings, Finding{
					File:            rel,
					Line:            lineNum,
					Kind:            "high_entropy_secret",
					MaskedValue:     maskValue(value),
					Severity:        SeverityMedium,
					SuggestedEnvVar: suggestEnvVar(line, "SECRET"),
				})
			}
		}
	}

	return findings, scanner.Err()
}

// isPlaceholder returns true for values that are clearly example / template
// tokens rather than real secrets.
func isPlaceholder(v string) bool {
	lower := strings.ToLower(v)

	// Whole-string or leading placeholder markers.
	prefixMarkers := []string{
		"your_", "your-", "<your", "put_your", "put-your",
		"enter_your", "insert_here", "replace_me", "todo_",
	}
	for _, m := range prefixMarkers {
		if strings.HasPrefix(lower, m) {
			return true
		}
	}

	// Words that flag the whole value as a placeholder only when the value
	// is predominantly composed of that word (not just a suffix).
	wholeWordMarkers := []string{
		"changeme", "placeholder", "fixme",
		"xxxxxxxxx", "aaaaaaaaa",
	}
	for _, w := range wholeWordMarkers {
		if strings.Contains(lower, w) {
			return true
		}
	}

	// "example" only flags when the value starts or ends with it, or is
	// separated by a non-alphanumeric boundary — not an embedded suffix like
	// AKIAIOSFODNN7EXAMPLE (which is a well-known test key, handled below).
	if lower == "example" || strings.HasPrefix(lower, "example") || strings.HasSuffix(lower, "_example") {
		return true
	}

	// Well-known AWS documentation test key.
	if strings.EqualFold(v, "AKIAIOSFODNN7EXAMPLE") || strings.EqualFold(v, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY") {
		return true
	}

	// Common fake/test/mock/dummy/sample patterns — only when the whole value
	// is test-like (starts with or is predominantly these words).
	shortFakeWords := []string{"fake", "dummy", "mock_", "sample_", "test_", "_fake", "_dummy", "_test", "_mock"}
	for _, w := range shortFakeWords {
		if strings.HasPrefix(lower, w) || strings.HasSuffix(lower, w) {
			return true
		}
	}

	// Numeric-only sequences that look like example values.
	numericish := []string{"1234567890", "0987654321", "11111111", "00000000"}
	for _, n := range numericish {
		if strings.Contains(lower, n) {
			return true
		}
	}

	// All-same-char sequences are placeholders (e.g. "aaaaaaaaaaaaaaaa").
	if len(v) > 0 {
		first := v[0]
		allSame := true
		for i := 1; i < len(v); i++ {
			if v[i] != first {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Report builder
// ─────────────────────────────────────────────────────────────────────────────

// BuildReport converts a flat []Finding into a structured Report.
func BuildReport(root string, findings []Finding) Report {
	bySeverity := map[string]int{
		string(SeverityCritical): 0,
		string(SeverityHigh):     0,
		string(SeverityMedium):   0,
		string(SeverityLow):      0,
	}
	for _, f := range findings {
		bySeverity[string(f.Severity)]++
	}

	// Group by file.
	fileMap := map[string][]Finding{}
	for _, f := range findings {
		fileMap[f.File] = append(fileMap[f.File], f)
	}
	files := make([]string, 0, len(fileMap))
	for k := range fileMap {
		files = append(files, k)
	}
	sort.Strings(files)

	rollups := make([]FileRollup, 0, len(files))
	for _, file := range files {
		ff := fileMap[file]
		highest := lowestSeverity()
		for _, f := range ff {
			if severityRank(f.Severity) > severityRank(highest) {
				highest = f.Severity
			}
		}
		rollups = append(rollups, FileRollup{
			File:     file,
			Count:    len(ff),
			Severity: highest,
			Findings: ff,
		})
	}

	return Report{
		Root:          root,
		TotalFindings: len(findings),
		BySeverity:    bySeverity,
		Files:         rollups,
	}
}

// SeverityRank returns a numeric rank for a severity level (higher = more severe).
// Used by callers that need to filter by minimum severity threshold.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	}
	return 0
}

func severityRank(s Severity) int { return SeverityRank(s) }

func lowestSeverity() Severity { return SeverityLow }
