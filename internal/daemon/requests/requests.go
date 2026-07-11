// Package requests implements the serve→engine control-plane request-file
// queue (ADR-0024 Phase 1 / PR4, epic #5729).
//
// In split mode (GRAFEL_SPLIT_MODE=1) serve holds the MCP surface but has no
// scheduler — the engine child owns that. Mutating tools / reindex triggers
// that today reach the scheduler via an in-process call cannot do so across
// the process boundary. This package gives serve a durable, crash-safe way
// to hand a mutation/trigger to the engine: serve drops a request record as
// one JSON file under a repo's `requests/` directory (a sibling of
// repair.json / enrichment-candidates.json under
// internal/daemon.StateDirForRepo); the engine's drain loop periodically
// lists pending requests, applies each via the SAME in-process logic that
// already runs in monolith mode, writes an ack, and deletes the request.
//
// Crash-safety and exactly-once are both built from one primitive: atomic
// write-to-tmp + fsync + rename (mirroring internal/mcp/repair.go's
// writeRepairFile, the pattern this package generalizes). A request is
// either fully written-and-renamed or entirely absent — never observed
// half-written. A request that already has a matching ack is treated as
// already handled, so a crash between "ack written" and "request deleted"
// cannot cause a double-apply on redrain.
package requests

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Kind enumerates the mutation/trigger classes that can be queued. Producers
// gate on internal/daemon.SplitModeEnabled(); when it's false the existing
// in-process/direct call path is used instead and no request is written
// (mutually exclusive by construction — see internal/daemon/service.go's
// Index method for the reference wiring).
type Kind string

const (
	// KindReindex asks the engine to enqueue a (repo, ref, commit) onto its
	// scheduler — the request-file equivalent of an in-process
	// sched.Scheduler.EnqueueRefCommit call.
	KindReindex Kind = "reindex"
	// KindSubmitRepair mirrors internal/mcp's submit_repair tool. NOTE: as of
	// PR4, submit_repair does NOT route through this queue in practice — it
	// already writes repair.json directly (internal/mcp/repair.go
	// writeRepairFile) and the engine picks up repair.json lazily on its next
	// scheduled index pass, so it is already cross-process safe with no
	// producer change required. The kind is defined here so the package is a
	// complete, reusable primitive if a future need arises for an
	// acknowledged (not just fire-and-forget) repair submission.
	KindSubmitRepair Kind = "submit_repair"
	// KindDocgenApply mirrors grafel_docgen_apply. Like KindSubmitRepair,
	// today's handlers (handleApplyDocSemantics, handleApplyDocgenRepairs)
	// already write their sidecars directly and are consumed lazily by the
	// engine — no producer change required for PR4.
	KindDocgenApply Kind = "docgen_apply"
	// KindEnrichmentEnqueue mirrors grafel_enrichments submit/reject. Same
	// note as above: internal/mcp/candidates.go's appendResolution /
	// appendRejection already write directly and are consumed lazily.
	KindEnrichmentEnqueue Kind = "enrichment_enqueue"
)

// Status is the terminal state an Ack records for a drained request.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// requestSuffix / ackSuffix name the on-disk file extensions. Using a
// distinct, unambiguous suffix (rather than bare ".json") keeps ListPending's
// directory scan from ever mistaking an ack for a request or vice versa.
const (
	requestSuffix = ".request.json"
	ackSuffix     = ".ack.json"
)

// Record is one queued serve→engine request.
type Record struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	RepoPath  string          `json:"repo_path"`
	Ref       string          `json:"ref,omitempty"`
	Commit    string          `json:"commit,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Ack is the small result sidecar the consumer (engine) writes after
// applying a Record. serve polls for it with the same os.ReadFile model the
// status plane already uses.
type Ack struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	Err       string    `json:"err,omitempty"`
	AppliedAt time.Time `json:"applied_at"`
}

// Write assigns rec.ID (if empty) and rec.CreatedAt (if zero), then persists
// it to dir/<id>.request.json atomically: marshal → write to a unique tmp
// file in dir → fsync → rename. A crash before the rename leaves only a
// stray tmp file, which ListPending never observes as a request (it only
// globs the *.request.json suffix, and a bare rename is atomic on the same
// filesystem). Returns the (possibly newly-generated) ID.
func Write(dir string, rec Record) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("requests: dir is empty")
	}
	if rec.Kind == "" {
		return "", fmt.Errorf("requests: kind is required")
	}
	if rec.ID == "" {
		rec.ID = uuid.NewString()
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	finalPath := filepath.Join(dir, rec.ID+requestSuffix)
	if err := atomicWrite(dir, finalPath, data); err != nil {
		return "", err
	}
	return rec.ID, nil
}

// atomicWrite writes data to a unique tmp file under dir, fsyncs it, closes
// it, then renames it onto finalPath. Mirrors internal/mcp/repair.go's
// writeRepairFile tmp+fsync+rename pattern verbatim.
func atomicWrite(dir, finalPath string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup on any error path; a no-op once renamed.
		_ = os.Remove(tmpName)
	}()
	if _, err := io.WriteString(tmp, string(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, finalPath)
}

// ackPath / requestPath compute the on-disk sidecar paths for an id.
func ackPath(dir, id string) string     { return filepath.Join(dir, id+ackSuffix) }
func requestPath(dir, id string) string { return filepath.Join(dir, id+requestSuffix) }

// ListPending returns the requests in dir that are ready to be drained,
// oldest (by CreatedAt) first. Two classes of file are deliberately
// excluded, both non-fatal to the scan:
//
//   - Torn/invalid JSON (a request file that was never atomically written by
//     Write — e.g. a test simulating a crash, or filesystem corruption): the
//     entry is skipped and left on disk for operator inspection; it never
//     causes ListPending to error or the drain to abort.
//   - Already-acked requests (a matching <id>.ack.json exists): this is the
//     exactly-once guard for a crash between ApplyAndAck's ack-write and its
//     delete — the request is excluded from pending and (best-effort)
//     the stale request file is removed so a later drain sees a clean dir.
//
// A missing dir is not an error — it simply has nothing pending.
func ListPending(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= len(requestSuffix) || name[len(name)-len(requestSuffix):] != requestSuffix {
			continue
		}
		id := name[:len(name)-len(requestSuffix)]

		// Exactly-once guard: a matching ack means this request was already
		// applied by a prior drain that crashed before deleting it.
		if _, ok, ackErr := ReadAck(dir, id); ackErr == nil && ok {
			_ = os.Remove(requestPath(dir, id)) // best-effort catch-up cleanup
			continue
		}

		data, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			continue // removed between ReadDir and ReadFile — benign race
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			// Torn/invalid file — never act on it, never error the drain.
			continue
		}
		if rec.ID == "" {
			rec.ID = id
		}
		out = append(out, rec)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// WriteAck persists ack atomically to dir/<id>.ack.json (same tmp+fsync+
// rename discipline as Write). Stamps AppliedAt if zero.
func WriteAck(dir, id string, ack Ack) error {
	if dir == "" || id == "" {
		return fmt.Errorf("requests: dir/id must not be empty")
	}
	ack.ID = id
	if ack.AppliedAt.IsZero() {
		ack.AppliedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ack, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(dir, ackPath(dir, id), data)
}

// ReadAck reads dir/<id>.ack.json. ok is false (with a nil error) when no ack
// exists yet — the normal "still pending" case a poller checks in a loop.
func ReadAck(dir, id string) (Ack, bool, error) {
	data, err := os.ReadFile(ackPath(dir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return Ack{}, false, nil
		}
		return Ack{}, false, err
	}
	var ack Ack
	if err := json.Unmarshal(data, &ack); err != nil {
		return Ack{}, false, fmt.Errorf("requests: parse ack %s: %w", id, err)
	}
	return ack, true, nil
}

// Delete removes dir/<id>.request.json. Absent file is not an error (it may
// already have been removed by a prior partial ApplyAndAck or by
// ListPending's stale-cleanup).
func Delete(dir, id string) error {
	if err := os.Remove(requestPath(dir, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ApplyAndAck is the standard consumer sequence: apply(rec) using the
// existing in-process logic (e.g. sched.Scheduler.EnqueueRefCommit), then
// write an ack recording the outcome, then delete the request file — in that
// order, so a crash at any point leaves the on-disk state safely resumable:
//
//   - crash before apply returns: request still pending, unacked → redrained
//     and re-applied (apply must be idempotent — true for Enqueue-style
//     triggers, which already dedup/coalesce).
//   - crash after ack write, before delete: ListPending's ack-guard excludes
//     the request from the next drain — no double-apply.
//   - crash after delete: fully done, nothing to redrain.
//
// A non-nil error from apply is recorded in the ack as StatusError (not
// returned as a hard failure) so the request is still consumed exactly once;
// ApplyAndAck itself only returns an error if writing the ack or deleting the
// request fails (an on-disk problem, not an application-level one).
func ApplyAndAck(dir string, rec Record, apply func(Record) error) error {
	applyErr := apply(rec)

	ack := Ack{Status: StatusOK}
	if applyErr != nil {
		ack.Status = StatusError
		ack.Err = applyErr.Error()
	}
	if err := WriteAck(dir, rec.ID, ack); err != nil {
		return fmt.Errorf("requests: write ack for %s: %w", rec.ID, err)
	}
	if err := Delete(dir, rec.ID); err != nil {
		return fmt.Errorf("requests: delete request %s: %w", rec.ID, err)
	}
	return nil
}
