package daemon

import (
	"encoding/json"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/daemon/requests"
)

// rebuild_request_status.go — exported read helper over the serve→engine
// KindRebuild request queue (ADR-0024 PR6 / epic #5729). It lets an
// out-of-process client (the wizard, internal/cli) detect when a split-mode
// group rebuild it enqueued has been drained+acked by the engine — the
// AUTHORITATIVE, race-free completion signal for a fire-and-forget Rebuild RPC.
//
// Why this and not "all repos' graph.fb advanced": a group rebuild can finish
// with one repo failing / empty / skipped, whose graph.fb never advances. A
// per-repo "all advanced" predicate would then never fire and the caller would
// hang until its overall timeout. The request-ack signal fires the moment the
// engine finishes OUR rebuild (success OR partial), so the caller stops
// promptly and classifies the per-repo result from the status plane instead.

// RebuildRequestPending reports whether a KindRebuild request carrying
// progressToken is still queued for group (i.e. not yet drained+acked by the
// engine). The token scopes the check to OUR specific rebuild — the same token
// the caller passed as proto.RebuildArgs.ProgressToken — so a concurrent
// rebuild of the same group (a different token) is never mistaken for ours.
//
// Semantics:
//   - true  → our rebuild request is still on disk and unacked: the engine has
//     not finished it yet.
//   - false → no matching pending request: the engine has drained+acked it
//     (ListPending excludes acked requests), so our group rebuild is DONE
//     (whether every repo succeeded is a separate status-plane classification).
//
// A missing requests dir is not an error (ListPending returns nothing). Callers
// MUST only rely on a false result AFTER the enqueue RPC has returned (the
// request is on disk before the RPC replies), otherwise a poll that races ahead
// of the write would see false spuriously.
func RebuildRequestPending(group, progressToken string) (bool, error) {
	recs, err := requests.ListPending(requestsDirForGroup(group))
	if err != nil {
		return false, err
	}
	for _, rec := range recs {
		if rec.Kind != requests.KindRebuild {
			continue
		}
		var args proto.RebuildArgs
		if err := json.Unmarshal(rec.Payload, &args); err != nil {
			// A torn/foreign payload we can't decode is not ours — skip it
			// rather than failing the whole check.
			continue
		}
		if args.ProgressToken == progressToken {
			return true, nil
		}
	}
	return false, nil
}
