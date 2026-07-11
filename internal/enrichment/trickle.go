// trickle.go — the streaming, chunked write path for enrichment candidates
// (#5720, #5729). WriteCandidates (candidates.go) is correct but
// double-materializes: mergeDiscoveredAt reads the ENTIRE prior sidecar,
// decodes it into a full []Candidate (every field — Evidence, ScoreBreakdown,
// Context, ...), builds a second full-size merged slice, then
// json.MarshalIndent encodes the WHOLE thing into one in-memory byte slice
// before the atomic rename. On a large graph (~423MB prior sidecar) this
// chain of three full-size materializations is exactly what drove Pass 6's
// ~11GB peak (issue #5720).
//
// The background enrichment worker never calls WriteCandidates. Instead it
// uses CandidateAppender, which:
//   - loads ONLY a lightweight id -> discovered_at index from the prior file
//     via a streaming, one-object-at-a-time JSON decode (loadDiscoveredAtIndex),
//     never holding a full Candidate (or the full prior byte slice) in memory;
//   - streams new candidates to a temp file batch-by-batch, releasing each
//     batch's memory back to the GC as soon as it's flushed.
//
// Peak heap attributable to this path is bounded by (chunk size × avg
// candidate size) + (distinct-ID count × ~2 short strings) — a small
// constant, independent of the total candidates-file size on disk.
package enrichment

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// discoveredAtOnly is the minimal shape decoded from the prior on-disk
// candidates file when building the discovered_at index. Decoding into this
// narrow struct (instead of the full Candidate, which additionally carries
// Context/PromptTemplate/QualificationSignals/ScoreBreakdown/...) keeps the
// per-record decode allocation small regardless of how much other data a
// prior Candidate record carries.
type discoveredAtOnly struct {
	ID           string `json:"id"`
	DiscoveredAt string `json:"discovered_at"`
}

// loadDiscoveredAtIndex opens path and streams it into an id -> discovered_at
// map, tolerating both the {"version":N,"candidates":[...]} envelope and the
// legacy bare-array shape. Returns an empty (non-nil) map on any read/parse
// error or missing file — the trickle write is best-effort idempotence, not
// correctness-critical (a cold discovered_at just means new candidates get a
// fresh timestamp, same as issue #53's original fallback behavior).
func loadDiscoveredAtIndex(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()
	return loadDiscoveredAtIndexFromReader(f)
}

// loadDiscoveredAtIndexFromReader does the actual streaming decode. Split out
// from loadDiscoveredAtIndex so tests can wrap an instrumented io.Reader (e.g.
// one that records the largest single Read() request) to assert the loader
// never slurps the whole input in one shot the way os.ReadFile does.
func loadDiscoveredAtIndexFromReader(r io.Reader) map[string]string {
	out := map[string]string{}
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return out
	}
	if delim, ok := tok.(json.Delim); ok && delim == '{' {
		// Object envelope: scan keys until we find "candidates", skipping
		// (streaming-decode, not slurping) every other key's value.
		found := false
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return out
			}
			key, _ := keyTok.(string)
			if key == "candidates" {
				found = true
				break
			}
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return out
			}
		}
		if !found {
			return out
		}
		// Consume the '[' that opens the candidates array.
		if _, err := dec.Token(); err != nil {
			return out
		}
	}
	// Either we just consumed "candidates": [ above, or the bare-array form's
	// leading '[' was already consumed by the first dec.Token() call — either
	// way we're now positioned to decode array elements one at a time.
	for dec.More() {
		var c discoveredAtOnly
		if err := dec.Decode(&c); err != nil {
			break
		}
		if c.ID != "" && c.DiscoveredAt != "" {
			out[c.ID] = c.DiscoveredAt
		}
	}
	return out
}

// CandidateAppender streams candidates into <grafelDir>/enrichment-candidates.json
// in bounded-size batches instead of assembling the full merged set in memory
// at once. See the package doc comment above for why this exists (#5720).
//
// Usage:
//
//	a, err := NewCandidateAppender(grafelDir)
//	...
//	for each chunk of candidates:
//	    a.AppendChunk(chunk)
//	a.Close() // atomically publishes the file
//
// On any error, or if the caller decides to abandon the run (e.g. a
// superseding Schedule() cancelled this job), call Abort() instead of Close()
// so the temp file is removed and the prior on-disk candidates file is left
// untouched — a cancelled trickle run must never leave a stale-and-truncated
// candidates file, and must never write over a fresher concurrent run's
// output (the Scheduler in background.go guarantees only one writer for a
// given repo is ever active at a time).
type CandidateAppender struct {
	grafelDir string
	tmpPath   string
	f         *os.File
	count     int
	priorAt   map[string]string
	closed    bool
}

// NewCandidateAppender opens (creates) the temp file and loads the prior
// discovered_at index. Safe to call even when no prior candidates file
// exists (first-ever index of a repo).
func NewCandidateAppender(grafelDir string) (*CandidateAppender, error) {
	if err := os.MkdirAll(grafelDir, 0o755); err != nil {
		return nil, fmt.Errorf("enrichment: mkdir %s: %w", grafelDir, err)
	}
	path := candidatesPath(grafelDir)
	priorAt := loadDiscoveredAtIndex(path)

	// os.CreateTemp (not a fixed name) so two appenders for the SAME repo
	// opened concurrently — e.g. the background worker plus a manually
	// triggered `grafel index`/`rebuild` on that repo — never collide on
	// the same tmp path and clobber/truncate each other's in-progress file.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".trickle.tmp-*")
	if err != nil {
		return nil, fmt.Errorf("enrichment: create tmp: %w", err)
	}
	tmp := f.Name()
	if _, err := f.WriteString(fmt.Sprintf(`{"version":%d,"candidates":[`, CandidatesSchemaVersion)); err != nil {
		f.Close()
		os.Remove(tmp)
		return nil, fmt.Errorf("enrichment: write header: %w", err)
	}
	return &CandidateAppender{
		grafelDir: grafelDir,
		tmpPath:   tmp,
		f:         f,
		priorAt:   priorAt,
	}, nil
}

// AppendChunk writes one batch of candidates to the temp file. The caller's
// slice is free to be discarded/reused immediately after this call returns —
// nothing about it is retained by the appender.
func (a *CandidateAppender) AppendChunk(cs []Candidate) error {
	for _, c := range cs {
		if prior, ok := a.priorAt[c.ID]; ok {
			c.DiscoveredAt = prior
		}
		if a.count > 0 {
			if _, err := a.f.WriteString(","); err != nil {
				return fmt.Errorf("enrichment: append: %w", err)
			}
		}
		data, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("enrichment: marshal candidate %s: %w", c.ID, err)
		}
		if _, err := a.f.Write(data); err != nil {
			return fmt.Errorf("enrichment: append: %w", err)
		}
		a.count++
	}
	return nil
}

// Count returns the number of candidates appended so far.
func (a *CandidateAppender) Count() int { return a.count }

// Abort discards the in-progress temp file without touching the published
// candidates file. Used when a run is cancelled or fails partway through.
func (a *CandidateAppender) Abort() {
	if a.closed {
		return
	}
	a.closed = true
	a.f.Close()
	os.Remove(a.tmpPath)
}

// Close finalizes the temp file and atomically renames it over the published
// candidates file. Not safe to call twice; a second call is a no-op error.
func (a *CandidateAppender) Close() error {
	if a.closed {
		return fmt.Errorf("enrichment: appender already closed")
	}
	a.closed = true
	if _, err := a.f.WriteString("]}"); err != nil {
		a.f.Close()
		os.Remove(a.tmpPath)
		return fmt.Errorf("enrichment: write footer: %w", err)
	}
	if err := a.f.Close(); err != nil {
		os.Remove(a.tmpPath)
		return fmt.Errorf("enrichment: close tmp: %w", err)
	}
	if err := os.Rename(a.tmpPath, candidatesPath(a.grafelDir)); err != nil {
		os.Remove(a.tmpPath)
		return fmt.Errorf("enrichment: rename: %w", err)
	}
	return nil
}
