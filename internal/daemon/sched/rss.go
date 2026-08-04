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

// A note on expiry, because its ABSENCE is now a deliberate decision (#6107
// round-2 review). A 30-day TTL on these entries was tried and removed. It
// made things strictly worse for two compounding reasons:
//
//  1. The fallback is worse than the stale data. Predict returning 0 sends
//     predictedFor to PredictRSS, which is 70x source bytes — measured at 4516
//     MB for the grafel repo itself, i.e. 2.2x a 16 GiB host's 2048 MB budget.
//     So expiry did not restore a neutral estimate, it guaranteed permanent
//     solo admit_oversize for any repo whose history aged out. Discarding a
//     real measurement in favour of a heuristic that over-estimates by 3-9x is
//     a downgrade, however stale the measurement.
//
//  2. Expiry and re-measurement run at incompatible rates. Record is fed only
//     by full subprocess indexes (runIndex skips cfg.Index entirely when the
//     incremental path succeeds, and incremental is default-ON and fully
//     in-process), so an actively-maintained repo can go months without a new
//     sample. A TTL that fires in 30 days against a series that updates in
//     months expires almost everything and re-measures almost nothing.
//
// The round-1 concern the TTL was meant to answer — one contaminated sample
// governing admission forever — is addressed at the source instead: only
// child_maxrss is ever recorded (see runIndex), so the contaminating measure is
// no longer PRODUCED. That is a claim about new writes and says nothing about
// entries already on disk, so every entry now carries the src that produced it
// and LoadRSSHistory marks the ones that are not child_maxrss.
//
// MARKS, not drops — and that distinction was measured, not assumed. Dropping
// them was tried and reintroduced the exact hazard the TTL was removed for.
// On the development host:
//
//	<a large monorepo>: stored peak 1593 MB, untagged
//	PredictRSS for the same repo:      10374 MB
//	BudgetMB on a 16 GiB host:          2048 MB
//
// Dropping the entry sends predictedFor to PredictRSS, i.e. from 1593 (under
// budget, admits concurrently) to 10374 (5.07x budget, permanent solo
// admit_oversize until the next FULL index, which may be months away because
// incremental is default-ON). Identical in shape to the TTL failure, on the
// largest repo on the machine — the one epic #5954 cares most about.
//
// It is also not a trade of accuracy for throughput. The daemon has indexed
// that repo on a 16 GiB host without OOM, so its true peak cannot be anywhere
// near 10374 MB (63% of RAM for one child). Between two figures that are both
// wrong, the stored one is closer, so dropping it makes the prediction worse in
// BOTH dimensions. Honesty is served by making the provenance visible — the src
// rides through to `grafel status` — not by deleting the number and silently
// substituting a worse one.
//
// A conditional drop ("discard only when PredictRSS is lower") was rejected for
// a sharper reason: the sampler that wrote these entries fails by reporting
// ZERO or near-zero — that is this whole issue — so legacy values are biased
// LOW. A rule keyed on "keep the smaller estimate" would preferentially retain
// exactly the entries that under-predict, which is the OOM direction.
//
// What remains permanent is either a legitimate measurement or a marked legacy
// figure that any single full index replaces outright (see Record). LastIndex is
// still written and read by nothing; it is on-disk forensics.

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
	// Src names the measure PeakRSSMB is, using the same vocabulary as the
	// completion line's peak_rss_src. Only peakSrcChildMaxRSS is admissible;
	// LoadRSSHistory drops anything else, including entries written before this
	// field existed (empty Src), because those predate the gate and can be the
	// daemon-RSS-delta this issue exists to stop trusting. Untagged is not
	// "unknown but probably fine" — every untagged entry was written by code
	// that had no gate at all.
	Src string `json:"src,omitempty"`
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
	// Normalise provenance on load. Anything not stamped child_maxrss — an
	// entry predating the field, or any other src — becomes an explicit
	// legacy_unverified. The entry is KEPT and still governs admission: see the
	// retention argument above, which is measured rather than assumed. What
	// this buys is that the value is self-describing from here on, surfaces as
	// legacy in `grafel status`, and is replaced outright by the first real
	// measurement instead of being decayed against for several.
	for repo, e := range h.data {
		if e.Src != peakSrcChildMaxRSS {
			e.Src = peakSrcLegacyUnverified
			h.data[repo] = e
		}
	}
	return h
}

// Predict returns the historical peak (in MB) or 0 if no record exists. Age is
// deliberately not consulted — see the note on expiry above.
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
	// A legacy figure is not a measurement, so the first real one REPLACES it
	// rather than being averaged against it. Decaying toward reality from a
	// fabricated starting point would take ~7 full indexes to converge, and
	// full indexes are exactly what is rare here (incremental is default-ON and
	// records nothing). One full index is enough to make the entry real.
	case prev.Src != peakSrcChildMaxRSS:
		prev.PeakRSSMB = peakMB
	case peakMB > prev.PeakRSSMB:
		prev.PeakRSSMB = peakMB
	case prev.PeakRSSMB > peakMB:
		prev.PeakRSSMB -= (prev.PeakRSSMB - peakMB) / rssHistoryRelaxDivisor
		if prev.PeakRSSMB < peakMB {
			prev.PeakRSSMB = peakMB
		}
	}
	prev.LastIndex = time.Now().UTC()
	// Stamp the measure. runIndex is the only caller and it is gated on
	// peakSrc == peakSrcChildMaxRSS, so this records what that gate already
	// guarantees — but it records it ON DISK, where the next process can check
	// it instead of trusting that every past build had the same gate.
	prev.Src = peakSrcChildMaxRSS
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
