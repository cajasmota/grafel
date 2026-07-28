package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// dead_letters.go — surfaces a dead-lettered KindRebuild to the status plane so
// it is OBSERVABLE (doctor / status readers) instead of vanishing into a single
// engine log line (epic #5729 issue #29, defect c). When ApplyAndAckBounded
// exhausts a rebuild's attempt budget the request file is deleted, so without
// this record there would be no on-disk trace an operator could inspect.
//
// The record is a small JSON sidecar under <root>/dead-letters/<id>.json. The
// engine is the sole writer; a reader (doctor) globs the directory. Missing
// directory is not an error — it simply means nothing has ever been
// dead-lettered.

// DeadLetter is one observable dead-letter record. It is deliberately
// self-describing (group + attempts + reason + timestamp) so a doctor/status
// reader can render it without cross-referencing the now-deleted request file.
type DeadLetter struct {
	ID       string    `json:"id"`
	Group    string    `json:"group"`
	Kind     string    `json:"kind"`
	Attempts int       `json:"attempts"`
	Reason   string    `json:"reason"`
	DeadAt   time.Time `json:"dead_at"`
}

// deadLettersDir is the engine-global directory holding dead-letter sidecars,
// a sibling of the per-repo/per-group requests dirs under the store root.
func deadLettersDir(root string) string {
	return filepath.Join(root, "dead-letters")
}

// deadLetterFromRec builds the observable record for a rebuild that exhausted
// its attempt budget.
func deadLetterFromRec(group string, rec requests.Record) DeadLetter {
	return DeadLetter{
		ID:       rec.ID,
		Group:    group,
		Kind:     string(rec.Kind),
		Attempts: rec.Attempts,
		Reason:   fmt.Sprintf("rebuild dead-lettered after %d attempts without completing", maxRebuildAttempts),
		DeadAt:   time.Now().UTC(),
	}
}

// recordDeadLetter persists dl to <root>/dead-letters/<id>.json (best-effort
// atomic-ish write). root == "" is a no-op (nothing to key the store off).
func recordDeadLetter(root string, dl DeadLetter) error {
	if root == "" {
		return nil
	}
	dir := deadLettersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(dl, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(dir, dl.ID+".json")
	// #6018: unique temp name — dead letters are recorded from several
	// concurrent daemon workers.
	return atomicfile.WriteFile(final, data, 0o644)
}

// ReadDeadLetters returns every dead-letter record under root, newest first. A
// missing directory yields an empty slice (not an error) — the normal "nothing
// dead-lettered" case a doctor/status reader handles. Torn/unreadable files are
// skipped rather than failing the whole scan.
func ReadDeadLetters(root string) ([]DeadLetter, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(deadLettersDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]DeadLetter, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(deadLettersDir(root), e.Name()))
		if rerr != nil {
			continue
		}
		var dl DeadLetter
		if err := json.Unmarshal(data, &dl); err != nil {
			continue
		}
		out = append(out, dl)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DeadAt.After(out[j].DeadAt)
	})
	return out, nil
}
