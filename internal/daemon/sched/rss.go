package sched

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PredictRSS returns a predicted peak RSS contribution (in MB) for
// indexing repoPath. It walks the repo, sums source-file bytes, and
// applies a rough multiplier that matches measured archigraph behaviour
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
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "dist" || name == "build" || name == ".archigraph" {
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

// RSSHistory is the on-disk record of per-repo measured peak RSS.
// Persisted at ~/.archigraph/repo-rss-history.json (or wherever the
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

// Predict returns the historical peak (in MB) or 0 if no record exists.
func (h *RSSHistory) Predict(repoPath string) int64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.data[repoPath].PeakRSSMB
}

// Record updates the running peak for a repo. Persists synchronously
// so a daemon crash doesn't lose the budget calibration.
func (h *RSSHistory) Record(repoPath string, peakMB int64) {
	if h == nil || repoPath == "" {
		return
	}
	h.mu.Lock()
	prev := h.data[repoPath]
	// Use a moving-max: a one-off spike sets the budget, smaller runs
	// don't shrink it. This is conservative on purpose — the cap is
	// safer when it slightly over-estimates.
	if peakMB > prev.PeakRSSMB {
		prev.PeakRSSMB = peakMB
	}
	prev.LastIndex = time.Now().UTC()
	h.data[repoPath] = prev
	tmp := h.path + ".tmp"
	b, _ := json.MarshalIndent(h.data, "", "  ")
	h.mu.Unlock()
	if h.path == "" {
		return
	}
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, h.path)
	}
}
