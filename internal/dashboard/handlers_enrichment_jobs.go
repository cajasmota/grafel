package dashboard

// handlers_enrichment_jobs.go — Enrichment job dispatch endpoints (#1244).
//
//	POST /api/enrichments/{group}/trigger   — enqueue a job for one entity
//	GET  /api/enrichments/{group}/jobs      — list all jobs for group
//	POST /api/enrichments/{group}/jobs/{id}/cancel — cancel a queued/running job
//
// The actual agent invocation is stubbed inside internal/jobs — it logs
// "would invoke agent" but does not call an external process. Real MCP
// agent wiring is a follow-up. All endpoints return JSON; the status
// progression is queued → running → done | failed.

import (
	"net/http"

	"github.com/cajasmota/archigraph/internal/jobs"
)

// handleEnrichmentTrigger — POST /api/enrichments/{group}/trigger
//
// Query params:
//
//	subject_id  — required; the entity ID to enrich (e.g. "flow::checkout")
//	kind        — optional; defaults to "describe_entity"
//
// Returns 202 with the job record.
func (s *Server) handleEnrichmentTrigger(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	subjectID := r.URL.Query().Get("subject_id")
	if subjectID == "" {
		writeErr(w, http.StatusBadRequest, "subject_id query parameter required")
		return
	}

	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "describe_entity"
	}

	if s.jobQueue == nil {
		writeErr(w, http.StatusServiceUnavailable, "job queue not initialised")
		return
	}

	id, err := s.jobQueue.Enqueue(group, subjectID, kind)
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, "job queue full: "+err.Error())
		return
	}

	job, _ := s.jobQueue.Get(id)
	writeJSON(w, http.StatusAccepted, jobToWire(job))
}

// handleEnrichmentJobs — GET /api/enrichments/{group}/jobs
//
// Returns all jobs for the group, newest-first.
func (s *Server) handleEnrichmentJobs(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	if s.jobQueue == nil {
		writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}, "total": 0})
		return
	}

	list := s.jobQueue.ListForGroup(group)
	wire := make([]map[string]any, len(list))
	for i, j := range list {
		wire[i] = jobToWire(j)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  wire,
		"total": len(wire),
	})
}

// handleEnrichmentJobCancel — POST /api/enrichments/{group}/jobs/{jobId}/cancel
//
// Cancels a queued or running job. No-op for done/failed jobs.
func (s *Server) handleEnrichmentJobCancel(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	jobID := r.PathValue("jobId")
	if group == "" || jobID == "" {
		writeErr(w, http.StatusBadRequest, "group and jobId required")
		return
	}

	if s.jobQueue == nil {
		writeErr(w, http.StatusServiceUnavailable, "job queue not initialised")
		return
	}

	job, ok := s.jobQueue.Get(jobID)
	if !ok {
		writeErr(w, http.StatusNotFound, "job not found: "+jobID)
		return
	}
	if job.Group != group {
		writeErr(w, http.StatusNotFound, "job not found in group "+group)
		return
	}

	s.jobQueue.Cancel(jobID)
	updated, _ := s.jobQueue.Get(jobID)
	writeJSON(w, http.StatusOK, jobToWire(updated))
}

// jobToWire converts a jobs.Job to the JSON wire shape exposed to the frontend.
func jobToWire(j jobs.Job) map[string]any {
	out := map[string]any{
		"id":         j.ID,
		"subject_id": j.SubjectID,
		"kind":       j.Kind,
		"group":      j.Group,
		"status":     j.Status,
		"queued_at":  j.QueuedAt,
	}
	if j.Error != "" {
		out["error"] = j.Error
	}
	if j.StartedAt != nil {
		out["started_at"] = j.StartedAt
	}
	if j.FinishedAt != nil {
		out["finished_at"] = j.FinishedAt
	}
	return out
}
