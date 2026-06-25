// Package watch — quarantine_content.go
//
// T3 content-signature trash detection (epic #5394, issue #5620) — the third
// detector wired into QuarantineTracker, signal="content".
//
// The static skip list (skip.go) + .gitignore filter catch KNOWN trash. The Q1
// churn detector (quarantine.go) catches dirs that *thrash*. The Q4 value
// detector catches dirs that are expensive-to-index ∧ unused. This file catches
// the residual long tail the others miss: a directory whose *content* is
// overwhelmingly machine-generated — minified bundles, vendored blobs,
// lockfiles, `@generated`/`DO NOT EDIT` output — even when it churns little and
// is occasionally referenced. Indexing such a dir burns parse time for no
// meaningful AST.
//
// Mechanism:
//
//	For each surviving watcher event we opportunistically FINGERPRINT the event's
//	own file (a cheap, byte-capped sample) and classify it generated/not. Per
//	directory we accumulate a tally (generated, total). When a dir has gathered
//	enough samples AND its generated-share crosses a conservative bar, we trip —
//	the caller (Observe) quarantines it with signal="content".
//
// SAFETY (non-negotiable, epic #5394):
//   - We never read whole files — only a capped prefix (default 64 KiB).
//   - We fingerprint at most ONE file per event (the event's file), and freeze a
//     dir's accumulator after a sample cap (default 64) so the work per dir is
//     bounded regardless of event volume.
//   - A conservative DOUBLE gate: a minimum sample count (default 8) AND a high
//     generated-share (default 0.90). A normal source directory with a couple of
//     generated files (a lockfile, one minified vendor file) stays well under the
//     share bar and is never quarantined.
//   - Reuses the shared quarantine set, so pins, persistence, quiet-sweep heal,
//     and recover-on-query all apply unchanged.
//   - Env kill-switch + tunables, mirroring the Q1 pattern.
package watch

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Content-detection tuning. All env-overridable (see contentConfig).
//
// Defaults are deliberately conservative: a directory must yield at least
// defaultContentMinSamples (8) fingerprinted files AND a generated-share of at
// least defaultContentSharePct (90%) to be quarantined — far above a normal
// source dir that happens to contain a lockfile or a vendored bundle.
const (
	defaultContentMinSamples  = 8
	defaultContentSharePct    = 90 // percent; 90 == 0.90 generated-share
	defaultContentSampleBytes = 64 * 1024
	defaultContentMaxLine     = 5000 // a single line longer than this ⇒ minified
	defaultContentMaxSamples  = 64   // freeze a dir's accumulator after this many
)

// contentConfig holds the resolved (env-aware) content-detector thresholds.
type contentConfig struct {
	disabled    bool
	minSamples  int
	sharePct    int
	sampleBytes int
	maxLine     int
	maxSamples  int
}

var (
	contentCfgOnce sync.Once
	contentCfg     contentConfig
)

// loadContentConfig parses the env once. Recognised vars:
//
//	GRAFEL_QUARANTINE_CONTENT_DISABLE=1        — turn the T3 detector off
//	GRAFEL_QUARANTINE_CONTENT_MIN_SAMPLES=<n>  — min fingerprinted files to decide
//	GRAFEL_QUARANTINE_CONTENT_SHARE_PCT=<n>    — generated-share %, 1..100
//	GRAFEL_QUARANTINE_CONTENT_SAMPLE_BYTES=<n> — bytes read per file (prefix cap)
//	GRAFEL_QUARANTINE_CONTENT_MAX_LINE=<n>     — single-line length ⇒ minified
//	GRAFEL_QUARANTINE_CONTENT_MAX_SAMPLES=<n>  — per-dir sample cap (freeze)
func loadContentConfig() contentConfig {
	contentCfgOnce.Do(func() {
		c := contentConfig{
			minSamples:  defaultContentMinSamples,
			sharePct:    defaultContentSharePct,
			sampleBytes: defaultContentSampleBytes,
			maxLine:     defaultContentMaxLine,
			maxSamples:  defaultContentMaxSamples,
		}
		if v := os.Getenv("GRAFEL_QUARANTINE_CONTENT_DISABLE"); v == "1" || v == "true" {
			c.disabled = true
		}
		c.minSamples = envPosInt("GRAFEL_QUARANTINE_CONTENT_MIN_SAMPLES", c.minSamples)
		if v := os.Getenv("GRAFEL_QUARANTINE_CONTENT_SHARE_PCT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				c.sharePct = n
			}
		}
		c.sampleBytes = envPosInt("GRAFEL_QUARANTINE_CONTENT_SAMPLE_BYTES", c.sampleBytes)
		c.maxLine = envPosInt("GRAFEL_QUARANTINE_CONTENT_MAX_LINE", c.maxLine)
		c.maxSamples = envPosInt("GRAFEL_QUARANTINE_CONTENT_MAX_SAMPLES", c.maxSamples)
		contentCfg = c
	})
	return contentCfg
}

func envPosInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// dirContent is the per-directory fingerprint accumulator.
type dirContent struct {
	generated int  // # files classified generated
	total     int  // # files fingerprinted
	frozen    bool // sample cap reached: stop re-sampling, verdict fixed
	tripped   bool // already tripped (don't re-trip / re-quarantine)
	// seen tracks distinct file basenames sampled so the same file rewritten
	// repeatedly counts once (churn is Q1's job, not ours).
	seen map[string]struct{}
}

// contentDetector accumulates per-(repo,dir) content fingerprints. It is only
// ever touched under QuarantineTracker.mu, so it needs no lock of its own.
type contentDetector struct {
	cfg contentConfig
	// byDir[repo][relDir] → accumulator.
	byDir map[string]map[string]*dirContent
}

func newContentDetector() *contentDetector {
	return &contentDetector{
		cfg:   loadContentConfig(),
		byDir: make(map[string]map[string]*dirContent),
	}
}

// observeContentLocked fingerprints the event's file, folds it into the dir's
// accumulator, and reports whether the dir's content now crosses the
// generated-share threshold. Returns (detail, true) on a trip. Called under
// q.mu from Observe; all state lives in the detector so #5619 (value) never
// collides.
//
// It is a no-op (false) when: the detector is disabled, the file is not a
// regular sampleable file, or the dir hasn't gathered enough evidence yet.
func (q *QuarantineTracker) observeContentLocked(repo, rel, path string, now time.Time) (detail string, trip bool) {
	cd := q.contentLocked()
	if cd == nil {
		return "", false
	}
	return cd.observe(repo, rel, path)
}

// contentLocked lazily initialises the content detector (kill-switch aware).
func (q *QuarantineTracker) contentLocked() *contentDetector {
	if q.content == nil {
		cd := newContentDetector()
		if cd.cfg.disabled {
			// Leave q.content nil so we keep cheaply short-circuiting, but
			// remember the decision via a disabled detector sentinel.
			q.content = cd
			return nil
		}
		q.content = cd
	}
	if q.content.cfg.disabled {
		return nil
	}
	return q.content
}

// observe folds one file's fingerprint into the (repo,dir) accumulator and
// returns a trip when the conservative double-gate is satisfied.
func (cd *contentDetector) observe(repo, rel, path string) (detail string, trip bool) {
	byDir := cd.byDir[repo]
	if byDir == nil {
		byDir = make(map[string]*dirContent)
		cd.byDir[repo] = byDir
	}
	dc := byDir[rel]
	if dc == nil {
		dc = &dirContent{seen: make(map[string]struct{})}
		byDir[rel] = dc
	}
	if dc.tripped || dc.frozen {
		return "", false
	}

	base := filepath.Base(path)
	if _, dup := dc.seen[base]; dup {
		// Same file again (a churning generated file). Count it once.
		return "", false
	}

	gen, ok := classifyFile(path, cd.cfg)
	if !ok {
		// Not a regular sampleable file (dir, unreadable, empty) — ignore.
		return "", false
	}
	dc.seen[base] = struct{}{}
	dc.total++
	if gen {
		dc.generated++
	}

	if dc.total >= cd.cfg.maxSamples {
		dc.frozen = true
	}

	// Conservative double gate: enough samples AND high generated-share.
	if dc.total < cd.cfg.minSamples {
		return "", false
	}
	if dc.generated*100 < cd.cfg.sharePct*dc.total {
		return "", false
	}
	dc.tripped = true
	detail = strconv.Itoa(dc.generated) + "/" + strconv.Itoa(dc.total) +
		" files generated (≥" + strconv.Itoa(cd.cfg.sharePct) + "%)"
	return detail, true
}

// dropContentDir clears a dir's content accumulator (called when a dir is
// un-quarantined so it re-evaluates cleanly). Best-effort; nil-safe.
func (cd *contentDetector) dropContentDir(repo, rel string) {
	if cd == nil {
		return
	}
	if m := cd.byDir[repo]; m != nil {
		delete(m, rel)
	}
}

// ---- the content fingerprint ----

// lockfileNames are exact basenames that are always machine-managed manifests.
var lockfileNames = map[string]struct{}{
	"package-lock.json":   {},
	"npm-shrinkwrap.json": {},
	"yarn.lock":           {},
	"pnpm-lock.yaml":      {},
	"bun.lockb":           {},
	"composer.lock":       {},
	"cargo.lock":          {}, // matched case-insensitively below
	"go.sum":              {},
	"poetry.lock":         {},
	"pdm.lock":            {},
	"pipfile.lock":        {},
	"gemfile.lock":        {},
	"packages.lock.json":  {},
	"flake.lock":          {},
}

// classifyFile reads a capped prefix of path and reports whether it looks
// machine-generated. The second return is false when path is not a regular,
// non-empty, readable file (caller ignores those — they are not evidence).
func classifyFile(path string, cfg contentConfig) (generated bool, ok bool) {
	// Lockfile / build-manifest by basename (no read needed).
	if isLockfileName(filepath.Base(path)) {
		return true, true
	}

	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return false, false
	}

	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()

	buf := make([]byte, cfg.sampleBytes)
	n, _ := f.Read(buf)
	if n == 0 {
		return false, false
	}
	sample := buf[:n]

	return looksGenerated(sample, fi.Size(), cfg), true
}

func isLockfileName(base string) bool {
	low := strings.ToLower(base)
	_, ok := lockfileNames[low]
	return ok
}

// generatedMarkers are case-insensitive substrings that explicitly declare a
// file machine-generated. We scan only the sampled prefix (markers are by
// convention at the very top of a file).
var generatedMarkers = [][]byte{
	[]byte("@generated"),
	[]byte("do not edit"),
	[]byte("code generated"),
	[]byte("autogenerated"),
	[]byte("auto-generated"),
	[]byte("automatically generated"),
	[]byte("this file is generated"),
	[]byte("generated by"),
}

// looksGenerated applies the content signatures to a sampled prefix:
//   - binary-ish: a NUL byte in the prefix
//   - explicit @generated / DO NOT EDIT markers
//   - minified / single-huge-line: max line length over cfg.maxLine
//   - low line density over a large file (few newlines across many bytes)
//   - low-entropy / highly repetitive prefix (a small distinct-byte alphabet)
func looksGenerated(sample []byte, fullSize int64, cfg contentConfig) bool {
	// Binary-ish.
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}

	// Explicit markers (case-insensitive, prefix scan).
	low := bytes.ToLower(sample)
	for _, m := range generatedMarkers {
		if bytes.Contains(low, m) {
			return true
		}
	}

	// Line-shape analysis.
	maxLine, lines := maxLineLen(sample)
	if maxLine >= cfg.maxLine {
		return true // a minified bundle / one giant line
	}
	// Low line density across a large file: average line far longer than any
	// human source, measured over the sampled prefix.
	if len(sample) >= 4096 {
		avg := len(sample) / (lines + 1)
		if avg >= 600 {
			return true
		}
	}

	// Low-entropy / repetitive: a tiny distinct-byte alphabet over a sizable
	// sample is characteristic of encoded blobs / repeated boilerplate.
	if len(sample) >= 1024 && distinctBytes(sample) <= 16 {
		return true
	}

	return false
}

// maxLineLen returns the longest line length and the line count in sample.
func maxLineLen(sample []byte) (max, lines int) {
	cur := 0
	for _, b := range sample {
		if b == '\n' {
			if cur > max {
				max = cur
			}
			cur = 0
			lines++
			continue
		}
		cur++
	}
	if cur > max {
		max = cur
	}
	return max, lines
}

// distinctBytes counts the number of distinct byte values in sample.
func distinctBytes(sample []byte) int {
	var seen [256]bool
	count := 0
	for _, b := range sample {
		if !seen[b] {
			seen[b] = true
			count++
		}
	}
	return count
}
