package daemon

import (
	"encoding/json"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

func writeRebuildRequest(t *testing.T, group, token string) string {
	t.Helper()
	payload, err := json.Marshal(proto.RebuildArgs{Group: group, ProgressToken: token})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	id, err := requests.Write(requestsDirForGroup(group), requests.Record{
		Kind:    requests.KindRebuild,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("write request: %v", err)
	}
	return id
}

// TestRebuildRequestPending_TrueWhileQueued: our token's KindRebuild request is
// pending until it is acked (drained by the engine).
func TestRebuildRequestPending_TrueWhileQueued(t *testing.T) {
	t.Setenv(EnvRoot, t.TempDir())
	const group, token = "g", "tok-123"

	if pending, err := RebuildRequestPending(group, token); err != nil || pending {
		t.Fatalf("pending before write = (%v,%v); want (false,nil)", pending, err)
	}

	id := writeRebuildRequest(t, group, token)
	if pending, err := RebuildRequestPending(group, token); err != nil || !pending {
		t.Fatalf("pending after write = (%v,%v); want (true,nil)", pending, err)
	}

	// Ack it (as the engine does after draining) → no longer pending.
	dir := requestsDirForGroup(group)
	if err := requests.WriteAck(dir, id, requests.Ack{ID: id, Status: requests.StatusOK}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if pending, err := RebuildRequestPending(group, token); err != nil || pending {
		t.Fatalf("pending after ack = (%v,%v); want (false,nil)", pending, err)
	}
}

// TestRebuildRequestPending_ScopedToOurToken: a concurrent rebuild of the SAME
// group under a DIFFERENT token must not be mistaken for ours.
func TestRebuildRequestPending_ScopedToOurToken(t *testing.T) {
	t.Setenv(EnvRoot, t.TempDir())
	const group = "g"
	writeRebuildRequest(t, group, "other-token")

	if pending, err := RebuildRequestPending(group, "ours"); err != nil || pending {
		t.Fatalf("pending for our token = (%v,%v); want (false,nil) — a different token's request is not ours", pending, err)
	}
	if pending, err := RebuildRequestPending(group, "other-token"); err != nil || !pending {
		t.Fatalf("pending for other token = (%v,%v); want (true,nil)", pending, err)
	}
}
