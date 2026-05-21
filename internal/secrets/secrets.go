// Package secrets — hardcoded-secret detector for source files.
//
// ScanPath walks every file under root and flags lines that appear to contain
// hardcoded credentials.  A line is suppressed when:
//   - it carries an opt-out comment:  // archigraph: ignore-secret
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
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
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

const ignoreComment = "archigraph: ignore-secret"

// ─────────────────────────────────────────────────────────────────────────────
// Scanner
// ─────────────────────────────────────────────────────────────────────────────

// ScanPath walks root and returns all secret findings.
// maxFileBytes limits the size of a single file to scan (0 = 512 KB default).
func ScanPath(root string, maxFileBytes int64) ([]Finding, error) {
	if maxFileBytes <= 0 {
		maxFileBytes = 512 * 1024
	}

	var findings []Finding

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
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

		// Skip test files.
		if isTestFile(rel) {
			return nil
		}

		// Skip large files.
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}

		ff, err := scanFile(path, rel)
		if err != nil {
			return nil // skip unreadable files silently
		}
		findings = append(findings, ff...)
		return nil
	})

	return findings, err
}

// scanFile reads one file and returns all findings in it.
func scanFile(path, rel string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
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
