package sched

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/daemon/walk"
)

// PredictRSS returns a predicted peak RSS contribution (in MB) for
// indexing repoPath. It walks the repo, sums source-file bytes, and
// applies a rough multiplier that matches measured grafel behaviour
// on the real-fixture benchmark (post-#639): peak RSS ≈ 50–80× source
// bytes. We use 70× for the cheap predictor; per-repo history (when
// available) overrides this.
//
// The walk skips common non-source directories (.git, node_modules,
// vendor, dist, build) to keep the estimate close to what the extractor
// actually loads.
func PredictRSS(repoPath string) int64 {
	if repoPath == "" {
		return 0
	}
	var sourceBytes int64
	_ = filepath.WalkDir(repoPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Use the extended hard-coded skip list (issue #805).
			if walk.IsHardcodedSkip(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// Only count source-ish files (rough heuristic — purposely
		// inclusive so the predictor errs on the high side and the cap
		// is conservative).
		switch ext := strings.ToLower(filepath.Ext(p)); ext {
		case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
			".py", ".rs", ".java", ".kt", ".scala", ".rb", ".php",
			".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp",
			".cs", ".swift", ".m", ".mm", ".sh", ".bash", ".zsh",
			".json", ".yaml", ".yml", ".toml", ".proto", ".sql",
			".md", ".markdown":
			if info, err := d.Info(); err == nil {
				sourceBytes += info.Size()
			}
		}
		return nil
	})
	// 70× source bytes / 1MB. Use 64-bit math to avoid overflow on
	// large repos.
	mb := sourceBytes * 70 / (1024 * 1024)
	if mb < 1 {
		mb = 1 // every job costs at least 1MB so an empty repo doesn't get a free pass.
	}
	return mb
}

// rssHistoryTTL bounds how long a recorded peak may govern admission (#6107
// review). RSSHistoryEntry has always carried LastIndex and nothing ever read
// it, so there was no expiry path at all: an entry written months ago against
// extractor code that no longer exists still beat the live predictor on every
// admission decision. Past the TTL the honest answer is "no history", which
// falls back to PredictRSS and lets the next completed run re-measure.
const rssHistoryTTL = 30 * 24 * time.Hour

// rssHistoryRelaxDivisor controls how fast a recorded peak walks back toward
// smaller observations. See Record.
const rssHistoryRelaxDivisor = 2

// RSSHistory is the on-disk record of per-repo measured peak RSS.
// Persisted at ~/.grafel/repo-rss-history.json (or wherever the
// daemon layout points). Atomically replaced on update.
type RSSHistory struct {
	path string
	mu   sync.Mutex
	data map[string]RSSHistoryEntry
}

// RSSHistoryEntry is one repo's record.
type RSSHistoryEntry struct {
	PeakRSSMB int64     `json:"peak_rss_mb"`
	LastIndex time.Time `json:"last_index"`
}

// LoadRSSHistory reads the history file. A missing file is not an
// error — the daemon just starts with empty history and the predictor
// is used until enough runs have been recorded.
func LoadRSSHistory(path string) *RSSHistory {
	h := &RSSHistory{path: path, data: map[string]RSSHistoryEntry{}}
	if path == "" {
		return h
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	_ = json.Unmarshal(b, &h.data)
	return h
}

// Predict returns the historical peak (in MB), or 0 when there is no record or
// the record has aged out (rssHistoryTTL). A 0 sends the caller to PredictRSS.
func (h *RSSHistory) Predict(repoPath string) int64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.data[repoPath]
	// A zero LastIndex is a file written before the field was populated, or
	// hand-edited. Honour the peak rather than discarding calibration over a
	// missing timestamp — unknown age is not the same as known-stale.
	if !e.LastIndex.IsZero() && time.Since(e.LastIndex) > rssHistoryTTL {
		return 0
	}
	return e.PeakRSSMB
}

// Record updates the running peak for a repo. Persists synchronously
// so a daemon crash doesn't lose the budget calibration.
func (h *RSSHistory) Record(repoPath string, peakMB int64) {
	if h == nil || repoPath == "" {
		return
	}
	h.mu.Lock()
	prev := h.data[repoPath]
	// A spike is adopted IMMEDIATELY and in full: under-estimating is the
	// dangerous direction, because the budget exists to stop concurrent
	// indexes exhausting the host.
	//
	// #6107 review: what changed is the other direction. This was a pure
	// moving max, so a single outlier — a contended host, a pathological
	// commit, a Darwin ru_maxrss inflated by MADV_FREE pages the kernel had
	// not yet reclaimed (measured up to 1.94x live heap on a sawtooth
	// allocation profile, the shape an extractor has) — governed admission for
	// the life of the file, with no path back short of deleting it by hand.
	// That is not a conservative estimate but an unfalsifiable one, and once it
	// exceeds BudgetMB it pins the repo to the solo admit_oversize path
	// permanently, serialising it against every other repo. Smaller
	// measurements now walk the figure halfway back each time, so it converges
	// on reality within a few runs and never drops below what was just seen.
	switch {
	case peakMB > prev.PeakRSSMB:
		prev.PeakRSSMB = peakMB
	case prev.PeakRSSMB > peakMB:
		prev.PeakRSSMB -= (prev.PeakRSSMB - peakMB) / rssHistoryRelaxDivisor
		if prev.PeakRSSMB < peakMB {
			prev.PeakRSSMB = peakMB
		}
	}
	prev.LastIndex = time.Now().UTC()
	h.data[repoPath] = prev
	b, _ := json.MarshalIndent(h.data, "", "  ")
	h.mu.Unlock()
	if h.path == "" {
		return
	}
	// #6018: unique temp name. Errors stay deliberately discarded — this is a
	// best-effort budget calibration and a miss only costs a re-measure — but a
	// TORN history file would be read back as truth on the next start.
	_ = atomicfile.WriteFile(h.path, b, 0o600)
}
