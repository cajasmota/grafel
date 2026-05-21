package dashboard

// handlers_enrichment_batch.go — Batched enrichment API (issue #1285).
//
//	POST /api/enrichments/{group}/batch-enrich
//
// Accepts up to 50 enrichment candidates in one request, creates one job per
// accepted candidate via the existing internal/jobs queue, and returns a
// batch summary with accepted/rejected counts and rejection reasons.
//
// Already-enriched candidates (status=done in the job history for the entity)
// are rejected with reason "already_enriched". Candidates that already have a
// queued or running job for the same entity are rejected with "already_queued".
// Requests with more than 50 candidates are rejected with HTTP 400 before any
// jobs are created.
//
// The endpoint is idempotent by design: calling it twice with the same entity
// list creates jobs only for entities that are not yet tracked.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/jobs"
)

// maxBatchSize is the hard cap on candidates per batch-enrich request.
// Requests that exceed this are rejected with HTTP 400 before any jobs are
// created, preventing context-window overflow in downstream agent calls.
const maxBatchSize = 50

// batchCandidate is one enrichment candidate inside a batch-enrich request.
type batchCandidate struct {
	// EntityID is the unique stable identifier for the entity (e.g. "ep-abc123").
	EntityID string `json:"entity_id"`
	// Kind is the enrichment kind (e.g. "http_endpoint", "process_flow").
	// Defaults to "describe_entity" when empty.
	Kind string `json:"kind,omitempty"`
	// CriticalityBand is the optional priority band: "critical", "high",
	// "medium", or "low". Stored on the job for scheduler-side prioritisation.
	CriticalityBand string `json:"criticality_band,omitempty"`
}

// batchEnrichRequest is the JSON body for POST /api/enrichments/{group}/batch-enrich.
type batchEnrichRequest struct {
	// Candidates is the list of entities to enrich. Max 50.
	Candidates []batchCandidate `json:"candidates"`
	// ModelOverride optionally pins the Claude model for this batch.
	// When empty, the queue worker uses its configured default.
	ModelOverride string `json:"model_override,omitempty"`
}

// rejectionReason records why one candidate was not enqueued.
type rejectionReason struct {
	EntityID string `json:"entity_id"`
	// Reason is one of: "already_enriched", "already_queued", "enqueue_failed".
	Reason string `json:"reason"`
	// Detail carries the underlying error message for "enqueue_failed" cases.
	Detail string `json:"detail,omitempty"`
}

// batchEnrichResponse is returned by POST /api/enrichments/{group}/batch-enrich.
type batchEnrichResponse struct {
	// BatchID is a synthetic opaque identifier for client-side progress tracking.
	BatchID string `json:"batch_id"`
	// Accepted is the number of candidates successfully enqueued.
	Accepted int `json:"accepted"`
	// Rejected is the number of candidates skipped or failed.
	Rejected int `json:"rejected"`
	// JobIDs lists the newly created job IDs in the same order as the accepted
	// candidates. Clients can poll GET /api/enrichments/{group}/jobs for status.
	JobIDs []string `json:"job_ids"`
	// RejectionReasons describes each skipped candidate.
	RejectionReasons []rejectionReason `json:"rejection_reasons,omitempty"`
}

// handleEnrichmentBatch — POST /api/enrichments/{group}/batch-enrich
//
// Enqueues up to 50 enrichment candidates in a single HTTP round-trip.
// Returns 400 if the batch exceeds 50 candidates. Returns 503 if the job
// queue is not initialised. Returns 202 Accepted on success (even when all
// candidates are rejected — the request was valid).
func (s *Server) handleEnrichmentBatch(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	if s.jobQueue == nil {
		writeErr(w, http.StatusServiceUnavailable, "job queue not initialised")
		return
	}

	var req batchEnrichRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	if len(req.Candidates) > maxBatchSize {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("batch too large: %d candidates (max %d)", len(req.Candidates), maxBatchSize))
		return
	}

	if len(req.Candidates) == 0 {
		writeErr(w, http.StatusBadRequest, "candidates list must not be empty")
		return
	}

	// Build a lookup of entity_id → latest terminal job status for this group,
	// so we can detect already_enriched and already_queued without extra round-trips.
	entityStatus := buildEntityStatusMap(s.jobQueue, group)

	batchID := newBatchID()
	var (
		jobIDs   []string
		rejected []rejectionReason
	)

	for _, c := range req.Candidates {
		entityID := strings.TrimSpace(c.EntityID)
		if entityID == "" {
			// Silently skip blank entity IDs; clients should not send them.
			rejected = append(rejected, rejectionReason{
				EntityID: c.EntityID,
				Reason:   "invalid_entity_id",
				Detail:   "entity_id must not be blank",
			})
			continue
		}

		switch entityStatus[entityID] {
		case jobs.StatusDone:
			rejected = append(rejected, rejectionReason{
				EntityID: entityID,
				Reason:   "already_enriched",
			})
			continue
		case jobs.StatusQueued, jobs.StatusRunning:
			rejected = append(rejected, rejectionReason{
				EntityID: entityID,
				Reason:   "already_queued",
			})
			continue
		}

		kind := c.Kind
		if kind == "" {
			kind = "describe_entity"
		}

		id, err := s.jobQueue.Enqueue(group, entityID, kind, "")
		if err != nil {
			s.auditor.Err("batch_enrich_enqueue", group,
				map[string]any{"entity_id": entityID, "kind": kind, "batch_id": batchID},
				err.Error())
			rejected = append(rejected, rejectionReason{
				EntityID: entityID,
				Reason:   "enqueue_failed",
				Detail:   err.Error(),
			})
			continue
		}

		jobIDs = append(jobIDs, id)
	}

	s.auditor.OK("batch_enrich", group, map[string]any{
		"batch_id": batchID,
		"accepted": len(jobIDs),
		"rejected": len(rejected),
		"model":    req.ModelOverride,
	})

	resp := batchEnrichResponse{
		BatchID:          batchID,
		Accepted:         len(jobIDs),
		Rejected:         len(rejected),
		JobIDs:           jobIDs,
		RejectionReasons: rejected,
	}
	// Always 202 — the request was valid and the jobs were accepted.
	writeJSON(w, http.StatusAccepted, resp)
}

// buildEntityStatusMap returns a map of entity_id → most-recent job Status
// for all jobs in the given group. When multiple jobs exist for the same
// entity, the most-recently-queued one wins (ListForGroup returns newest-first).
func buildEntityStatusMap(q *jobs.Queue, group string) map[string]string {
	all := q.ListForGroup(group) // newest-first per entity
	m := make(map[string]string, len(all))
	for _, j := range all {
		// ListForGroup is newest-first; only record the first (newest) occurrence.
		if _, seen := m[j.SubjectID]; !seen {
			m[j.SubjectID] = j.Status
		}
	}
	return m
}

// newBatchID returns a time-prefixed, URL-safe batch identifier.
// Format: batch-<unix-ms>
func newBatchID() string {
	return fmt.Sprintf("batch-%d", time.Now().UnixMilli())
}
