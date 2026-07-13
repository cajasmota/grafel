package requests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWrite_CreatesRequestFileAtomically covers the happy path: Write assigns
// an ID when none is given, stamps CreatedAt, and produces a single
// well-formed <id>.request.json file with no stray tmp file left behind
// (ADR-0024 PR4, epic #5729).
func TestWrite_CreatesRequestFileAtomically(t *testing.T) {
	dir := t.TempDir()

	id, err := Write(dir, Record{
		Kind:     KindReindex,
		RepoPath: "/repo",
		Ref:      "main",
		Commit:   "abc123",
		Payload:  json.RawMessage(`{"async":true}`),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id == "" {
		t.Fatal("Write: expected a non-empty generated ID")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var requestFiles, tmpFiles int
	for _, e := range entries {
		switch {
		case filepath.Ext(e.Name()) == ".json" && !e.IsDir():
			requestFiles++
		default:
			tmpFiles++
		}
	}
	if requestFiles != 1 {
		t.Fatalf("expected exactly 1 request file, got %d (entries=%v)", requestFiles, entries)
	}
	if tmpFiles != 0 {
		t.Fatalf("expected no stray tmp/other files, got %d (entries=%v)", tmpFiles, entries)
	}

	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 pending record, got %d", len(recs))
	}
	if recs[0].ID != id {
		t.Fatalf("ID mismatch: got %q want %q", recs[0].ID, id)
	}
	if recs[0].Kind != KindReindex {
		t.Fatalf("Kind mismatch: got %q", recs[0].Kind)
	}
	if recs[0].RepoPath != "/repo" || recs[0].Ref != "main" || recs[0].Commit != "abc123" {
		t.Fatalf("unexpected record: %+v", recs[0])
	}
	if recs[0].CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be stamped")
	}
}

// TestListPending_SkipsTornRequestFile is the crash-safety regression: a
// half-written request file (never atomically renamed into place by Write,
// simulating a crash mid-write) must never be acted on. ListPending skips it
// silently rather than erroring the whole drain.
func TestListPending_SkipsTornRequestFile(t *testing.T) {
	dir := t.TempDir()

	// A well-formed request, so we can prove the torn file doesn't poison the
	// rest of the drain.
	goodID, err := Write(dir, Record{Kind: KindReindex, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Simulate a crash mid-write: a request file with truncated/invalid JSON,
	// written directly (bypassing Write's atomic tmp+rename).
	tornPath := filepath.Join(dir, "torn-request.request.json")
	if err := os.WriteFile(tornPath, []byte(`{"kind":"reindex","repo_p`), 0o644); err != nil {
		t.Fatalf("write torn file: %v", err)
	}

	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending must not error on a torn file: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly the 1 good record (torn file skipped), got %d", len(recs))
	}
	if recs[0].ID != goodID {
		t.Fatalf("unexpected record survived: %+v", recs[0])
	}

	// The torn file itself is left alone (not deleted, not applied) so an
	// operator can inspect it; it must simply never be treated as pending.
	if _, err := os.Stat(tornPath); err != nil {
		t.Fatalf("torn file should still exist untouched: %v", err)
	}
}

// TestAck_WritesAtomicallyAndDeletesRequest covers the consumer side: Ack
// writes <id>.ack.json atomically and Delete removes the original request
// file. After both, ListPending no longer reports the request and ReadAck
// reports the ack.
func TestAck_WritesAtomicallyAndDeletesRequest(t *testing.T) {
	dir := t.TempDir()
	id, err := Write(dir, Record{Kind: KindSubmitRepair, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := WriteAck(dir, id, Ack{Status: StatusOK}); err != nil {
		t.Fatalf("WriteAck: %v", err)
	}
	ack, ok, err := ReadAck(dir, id)
	if err != nil {
		t.Fatalf("ReadAck: %v", err)
	}
	if !ok {
		t.Fatal("expected ack to be found")
	}
	if ack.Status != StatusOK {
		t.Fatalf("unexpected ack status: %+v", ack)
	}
	if ack.AppliedAt.IsZero() {
		t.Fatal("expected AppliedAt to be stamped")
	}

	if err := Delete(dir, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 pending after delete, got %d", len(recs))
	}
}

// TestListPending_SkipsAlreadyAckedRequest is the exactly-once /
// crash-between-ack-and-delete regression: if the consumer crashes after
// writing the ack but before deleting the request file, a second drain must
// NOT re-apply the request. ListPending treats a request with a matching ack
// as already handled and excludes it (best-effort cleans up the stale
// request file too, so a THIRD drain sees a clean directory).
func TestListPending_SkipsAlreadyAckedRequest(t *testing.T) {
	dir := t.TempDir()
	id, err := Write(dir, Record{Kind: KindReindex, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := WriteAck(dir, id, Ack{Status: StatusOK}); err != nil {
		t.Fatalf("WriteAck: %v", err)
	}
	// Deliberately do NOT call Delete — simulates the crash window.

	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected already-acked request to be excluded from pending, got %d: %+v", len(recs), recs)
	}
}

// TestApplyAndAck_IdempotentUnderRedrain exercises the full consumer
// helper: applying the same drained record twice (simulating a second drain
// pass after a crash between apply and delete) must not re-run the apply
// function once the first pass has acked, because the second ListPending
// excludes the already-acked request.
func TestApplyAndAck_IdempotentUnderRedrain(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, Record{Kind: KindReindex, RepoPath: "/repo", Ref: "main", Commit: "sha1"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	applyCount := 0
	apply := func(rec Record) error {
		applyCount++
		return nil
	}

	// First drain: apply, ack, delete.
	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 pending record, got %d", len(recs))
	}
	if err := ApplyAndAck(dir, recs[0], apply); err != nil {
		t.Fatalf("ApplyAndAck: %v", err)
	}
	if applyCount != 1 {
		t.Fatalf("expected apply to run once, ran %d times", applyCount)
	}

	// Second drain over the same directory: nothing pending, apply not
	// called again.
	recs2, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending (2nd): %v", err)
	}
	if len(recs2) != 0 {
		t.Fatalf("expected 0 pending on redrain, got %d", len(recs2))
	}
	if applyCount != 1 {
		t.Fatalf("expected apply to still have run exactly once after redrain, ran %d times", applyCount)
	}
}

// TestApplyAndAck_CrashBetweenAckAndDelete simulates the exactly-once guard
// directly: after ApplyAndAck's ack write but before its delete, a second
// caller draining the same directory must not see the request as pending
// (guards double-apply if the consumer process dies mid-ApplyAndAck).
func TestApplyAndAck_CrashBetweenAckAndDelete(t *testing.T) {
	dir := t.TempDir()
	id, err := Write(dir, Record{Kind: KindDocgenApply, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Manually reproduce the crash window: ack written, delete not yet run.
	if err := WriteAck(dir, id, Ack{Status: StatusOK}); err != nil {
		t.Fatalf("WriteAck: %v", err)
	}

	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("request must not be re-drained once acked, got %d pending", len(recs))
	}
}

// TestApplyAndAck_DeletesAckAfterSuccess is the ack-GC regression (PR6
// prerequisite gap #2, epic #5729): a successful ApplyAndAck must not leave
// the <id>.ack.json sidecar behind once the request file it was guarding is
// gone, otherwise a busy split repo accumulates ~150-byte ack files
// unboundedly. This does not reopen the crash-between-ack-and-delete hole
// (see TestApplyAndAck_CrashBetweenAckAndDelete): the ack only needs to
// exist between the ack write and the request delete, both of which have
// already happened by the time ApplyAndAck deletes it.
func TestApplyAndAck_DeletesAckAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	id, err := Write(dir, Record{Kind: KindReindex, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ApplyAndAck(dir, Record{ID: id, Kind: KindReindex, RepoPath: "/repo"}, func(Record) error {
		return nil
	}); err != nil {
		t.Fatalf("ApplyAndAck: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, id+ackSuffix)); !os.IsNotExist(err) {
		t.Fatalf("expected ack file to be removed after successful apply, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, id+requestSuffix)); !os.IsNotExist(err) {
		t.Fatalf("expected request file to be removed after successful apply, stat err=%v", err)
	}

	// A redrain over the now-empty dir must not error and must find nothing
	// pending — the whole point of Delete-then-deleteAck is that no residue
	// (request OR ack) can trip up the next drain pass.
	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 pending after ack-GC, got %d", len(recs))
	}
}

// TestWriteAck_ErrorStatusRoundTrips ensures a failed apply is reported
// faithfully through the ack so the producer (serve) can surface the error
// to the caller instead of silently treating it as success.
func TestWriteAck_ErrorStatusRoundTrips(t *testing.T) {
	dir := t.TempDir()
	id, err := Write(dir, Record{Kind: KindEnrichmentEnqueue, RepoPath: "/repo"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := WriteAck(dir, id, Ack{Status: StatusError, Err: "boom"}); err != nil {
		t.Fatalf("WriteAck: %v", err)
	}
	ack, ok, err := ReadAck(dir, id)
	if err != nil || !ok {
		t.Fatalf("ReadAck: ok=%v err=%v", ok, err)
	}
	if ack.Status != StatusError || ack.Err != "boom" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

// TestListPending_OrdersByCreatedAt guards a small but real usability
// property: requests are drained oldest-first so a burst of enqueues is
// processed in submission order.
func TestListPending_OrdersByCreatedAt(t *testing.T) {
	dir := t.TempDir()

	older := Record{Kind: KindReindex, RepoPath: "/repo", CreatedAt: time.Now().Add(-time.Minute)}
	newer := Record{Kind: KindReindex, RepoPath: "/repo", CreatedAt: time.Now()}

	newerID, err := Write(dir, newer)
	if err != nil {
		t.Fatalf("Write newer: %v", err)
	}
	olderID, err := Write(dir, older)
	if err != nil {
		t.Fatalf("Write older: %v", err)
	}

	recs, err := ListPending(dir)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].ID != olderID || recs[1].ID != newerID {
		t.Fatalf("expected oldest-first ordering, got %s then %s", recs[0].ID, recs[1].ID)
	}
}
