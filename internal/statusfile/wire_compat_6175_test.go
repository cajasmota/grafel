package statusfile

// wire_compat_6175_test.go — #6175: the migration-state JSON keys are a CROSS-
// BINARY contract, and nothing pinned them.
//
// Writer and reader are symmetric in-process, so renaming a tag is invisible to
// every other test in the tree: `grafel status` unmarshals whatever this package
// marshals. The contract that matters is between DIFFERENT builds — a sidecar
// written by one grafel and read by another during an upgrade, which is exactly
// the situation the format migration exists for. These tests grade the bytes.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestReindexKeysOnTheWire pins the JSON key names, and pins that each is
// omitted when false — the property an older binary's forward-compatibility
// rests on, since it must be able to ignore what it does not know.
func TestReindexKeysOnTheWire(t *testing.T) {
	b, err := json.Marshal(&File{
		RepoPath:               "/repo/x",
		ReindexRequired:        true,
		ReindexMigrationFailed: true,
		ReindexNotAccepted:     true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, key := range []string{
		`"reindex_required":true`,
		`"reindex_migration_failed":true`,
		`"reindex_not_accepted":true`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("wire format is missing %s — a reader built from a different revision will not see this state:\n%s", key, got)
		}
	}

	b2, err := json.Marshal(&File{RepoPath: "/repo/x"})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(b2), "reindex_not_accepted") {
		t.Errorf("reindex_not_accepted must be omitted when false:\n%s", string(b2))
	}
}

// TestUnknownKeysAreIgnoredOnRead is the backward half, and the reason
// #6175 needed no version gate: a sidecar written by a NEWER grafel carrying a
// migration state this build has never heard of must parse cleanly and leave the
// fields it does understand intact — not error, and not zero the file.
func TestUnknownKeysAreIgnoredOnRead(t *testing.T) {
	raw := []byte(`{
		"repo_path": "/repo/x",
		"reindex_required": true,
		"reindex_not_accepted": true,
		"reindex_some_future_state": true,
		"future_object": {"a": 1}
	}`)
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("a sidecar from a newer grafel must still parse: %v", err)
	}
	if f.RepoPath != "/repo/x" || !f.ReindexRequired || !f.ReindexNotAccepted {
		t.Errorf("known fields were lost alongside the unknown ones: %+v", f)
	}
}

// TestMissingNotAcceptedKeyReadsAsFalse is the other upgrade direction: a
// sidecar written by a grafel that predates #6175 has no such key, and must read
// as "not in that state" rather than as anything surprising. That is what makes
// the older-binary fallback (count it as in-progress, exactly as before) the
// behaviour on both sides of an upgrade.
func TestMissingNotAcceptedKeyReadsAsFalse(t *testing.T) {
	var f File
	if err := json.Unmarshal([]byte(`{"repo_path":"/repo/x","reindex_required":true}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.ReindexNotAccepted {
		t.Error("a pre-#6175 sidecar must not read as not-accepted")
	}
	if !f.ReindexRequired {
		t.Error("the pre-existing state must survive unchanged")
	}
}
