package daemon

// stale_reindex_state.go — #6167 review round 3, blocker 1.
//
// The outstanding set for the format migration has to survive DISPATCH, not
// just the moment of admission. Round 2 rebuilt it from requests.ListPending,
// which cannot work: ListPending skips any request that has an ack
// (requests/requests.go:236), and the drain loop applies-and-acks within ~2s
// while the index it dispatches runs 42s–4m53s. The user restarts BECAUSE the
// daemon is busy indexing, i.e. essentially always after the ack — so the
// reconcile saw an empty queue and re-admitted everything. Measured: 12
// duplicate full reindexes across 6 mid-index restarts, the same magnitude as
// with no fix at all.
//
// So the guard keeps its OWN durable marker, whose lifetime is the migration's
// lifetime rather than the request's: written when a repo is admitted, removed
// when its graph goes current. It also carries the attempt count and the
// give-up flag, which makes the failure history durable — without that, a
// restart-prone environment resets `attempts` on every restart and a genuinely
// broken repo is never marked failed and never surfaced, which is exactly the
// silent drop this whole arm exists to eliminate.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// migrationStateFile is the per-repo marker's filename, stored alongside
// graph.fb in the repo's ref-scoped state dir.
const migrationStateFile = "migration.json"

// migrationState is one repo's durable auto-migration record.
type migrationState struct {
	// RepoPath is the absolute repo path this record describes. Stored so a
	// glob-based reconcile can recover the repo identity from the file alone.
	RepoPath string `json:"repo_path"`
	// AdmittedAt is when the guard last admitted this repo. Zero means "not
	// currently holding a slot" — the record then exists only to carry
	// Attempts/Failed.
	AdmittedAt time.Time `json:"admitted_at,omitempty"`
	// Attempts counts admissions that did not end in a current graph. Reset to
	// zero (by deleting the record) the moment the repo goes current.
	Attempts int `json:"attempts,omitempty"`
	// Failed is set once Attempts reaches the cap; the repo is then never
	// admitted again and is reported to the user instead.
	Failed bool `json:"failed,omitempty"`
}

// migrationStatePath is repoPath's marker location.
func migrationStatePath(repoPath string) string {
	return filepath.Join(StateDirForRepo(repoPath), migrationStateFile)
}

// writeMigrationState persists st atomically. Best-effort, like the rest of the
// status plane: a failure means the next heartbeat retries.
func writeMigrationState(repoPath string, st migrationState) error {
	st.RepoPath = repoPath
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := migrationStatePath(repoPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, b, 0o644)
}

// clearMigrationState removes repoPath's marker — the repo is current, so both
// its slot and its accumulated attempt count are released.
func clearMigrationState(repoPath string) {
	_ = os.Remove(migrationStatePath(repoPath))
}

// readMigrationStateAt loads a marker by file path. A malformed or unreadable
// record reports ok=false and is treated as absent rather than fatal.
func readMigrationStateAt(path string) (migrationState, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return migrationState{}, false
	}
	var st migrationState
	if json.Unmarshal(b, &st) != nil || st.RepoPath == "" {
		return migrationState{}, false
	}
	return st, true
}

// discoverMigrationStates finds every migration marker under the store root,
// mirroring discoverRequestsDirs' glob over the <slug>/refs/<ref>/ layout.
func discoverMigrationStates(root string) ([]migrationState, error) {
	if root == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "refs", "*", migrationStateFile))
	if err != nil {
		return nil, err
	}
	out := make([]migrationState, 0, len(matches))
	for _, m := range matches {
		if st, ok := readMigrationStateAt(m); ok {
			out = append(out, st)
		}
	}
	return out, nil
}
