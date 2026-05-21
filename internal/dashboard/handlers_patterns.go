package dashboard

// handlers_patterns.go — Pattern store REST surface (issue #1189)
//
//	GET    /api/patterns/{group}           — list (filters: needs_attention, status, confidence_min)
//	GET    /api/patterns/{group}/{id}      — single pattern detail
//	PUT    /api/patterns/{group}/{id}      — update pattern fields
//	DELETE /api/patterns/{group}/{id}      — delete a pattern
//	POST   /api/patterns/{group}/gc        — GC dry-run or execute
//	POST   /api/patterns/{group}/export    — export approved patterns to a CLAUDE.md path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cajasmota/archigraph/internal/agentpatterns"
)

// ---------------------------------------------------------------------------
// Route helpers
// ---------------------------------------------------------------------------

// groupPatternsDir returns the directory where patterns.json is stored for a group.
func groupPatternsDir(groupName string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".archigraph", "groups", groupName+"-patterns")
}

// loadAgentPatterns reads the strongly-typed Pattern slice via the
// agentpatterns package (shares the same on-disk format as the CLI).
func loadAgentPatterns(dir string) ([]agentpatterns.Pattern, error) {
	return agentpatterns.Load(dir)
}

// ---------------------------------------------------------------------------
// GET /api/patterns/{group}
// ---------------------------------------------------------------------------

// handlePatterns returns the list of agent-learned patterns for a group,
// enriched with stats and optional query-string filters.
//
// Query params:
//
//	needs_attention=true  — only rejected / low-confidence / stale patterns
//	status=active|candidate|rejected
//	confidence_min=0.0–1.0
func (s *Server) handlePatterns(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	// --- query-string filters -------------------------------------------------
	q := r.URL.Query()

	if q.Get("needs_attention") == "true" {
		patterns = filterNeedsAttention(patterns)
	}

	if status := q.Get("status"); status != "" {
		patterns = filterByStatus(patterns, status)
	}

	if confMin := q.Get("confidence_min"); confMin != "" {
		if f, err := strconv.ParseFloat(confMin, 64); err == nil {
			patterns = filterConfidenceMin(patterns, f)
		}
	}

	// --- build stats over ALL unfiltered patterns (re-load for counts) --------
	all, _ := loadAgentPatterns(dir)
	stats := buildPatternStats(all)

	// --- serialise list -------------------------------------------------------
	rows := make([]map[string]any, 0, len(patterns))
	for _, p := range patterns {
		rows = append(rows, patternToRow(p))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"patterns": rows,
		"count":    len(rows),
		"stats":    stats,
	})
}

// ---------------------------------------------------------------------------
// GET /api/patterns/{group}/{id}
// ---------------------------------------------------------------------------

func (s *Server) handlePatternDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	p := agentpatterns.ByID(patterns, id)
	if p == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("pattern %q not found", id))
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// ---------------------------------------------------------------------------
// PUT /api/patterns/{group}/{id}
// ---------------------------------------------------------------------------

// handlePatternUpdate merges the JSON body into the stored pattern. Only
// mutable fields are applied — the ID cannot change.
func (s *Server) handlePatternUpdate(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	existing := agentpatterns.ByID(patterns, id)
	if existing == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("pattern %q not found", id))
		return
	}

	// Decode patch as raw map so we only update provided fields.
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Apply allowed top-level scalar fields.
	updated := *existing

	if v, ok := patch["confidence"]; ok {
		var f float64
		if err := json.Unmarshal(v, &f); err == nil {
			updated.Confidence = f
		}
	}
	if v, ok := patch["is_candidate"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			updated.IsCandidate = b
		}
	}
	if v, ok := patch["approval_note"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			updated.ApprovalNote = s
		}
	}
	if v, ok := patch["reject_reason"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			updated.RejectReason = s
			if s != "" {
				updated.RejectTimestamp = time.Now().Unix()
			} else {
				updated.RejectTimestamp = 0
			}
		}
	}
	if v, ok := patch["steps"]; ok {
		var steps []string
		if err := json.Unmarshal(v, &steps); err == nil {
			updated.Steps = steps
		}
	}
	if v, ok := patch["trigger"]; ok {
		var t agentpatterns.Trigger
		if err := json.Unmarshal(v, &t); err == nil {
			updated.Trigger = t
		}
	}
	if v, ok := patch["category"]; ok {
		var c agentpatterns.Category
		if err := json.Unmarshal(v, &c); err == nil {
			updated.Category = c
		}
	}
	if v, ok := patch["anti_patterns"]; ok {
		var aps []agentpatterns.AntiPattern
		if err := json.Unmarshal(v, &aps); err == nil {
			updated.AntiPatterns = aps
		}
	}

	patterns = agentpatterns.Upsert(patterns, updated)
	if err := agentpatterns.Save(dir, patterns); err != nil {
		s.auditor.Err("pattern_update", group+"/"+id, nil, err.Error())
		writeErr(w, http.StatusInternalServerError, "save patterns: "+err.Error())
		return
	}
	s.auditor.OK("pattern_update", group+"/"+id, nil)

	writeJSON(w, http.StatusOK, &updated)
}

// ---------------------------------------------------------------------------
// DELETE /api/patterns/{group}/{id}
// ---------------------------------------------------------------------------

func (s *Server) handlePatternDelete(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	id := r.PathValue("id")
	if group == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "group and id required")
		return
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	found := agentpatterns.ByID(patterns, id)
	if found == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("pattern %q not found", id))
		return
	}

	out := make([]agentpatterns.Pattern, 0, len(patterns)-1)
	for _, p := range patterns {
		if p.ID != id {
			out = append(out, p)
		}
	}
	if err := agentpatterns.Save(dir, out); err != nil {
		s.auditor.Err("pattern_delete", group+"/"+id, nil, err.Error())
		writeErr(w, http.StatusInternalServerError, "save patterns: "+err.Error())
		return
	}
	s.auditor.OK("pattern_delete", group+"/"+id, nil)

	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ---------------------------------------------------------------------------
// POST /api/patterns/{group}/gc
// ---------------------------------------------------------------------------

// handlePatternGC runs garbage collection on candidate patterns older than
// the configured candidate_decay_days threshold.
//
// Body (optional):
//
//	{ "dry_run": true }   — default; inspect what would be pruned
//	{ "dry_run": false }  — actually prune
func (s *Server) handlePatternGC(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	var req struct {
		DryRun bool `json:"dry_run"`
	}
	req.DryRun = true // safe default
	_ = json.NewDecoder(r.Body).Decode(&req)

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	cfg, err := agentpatterns.LoadConfig(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}

	cutoff := time.Now().Add(-time.Duration(cfg.CandidateDecayDays) * 24 * time.Hour).Unix()

	var keep, pruned []agentpatterns.Pattern
	for _, p := range patterns {
		if p.IsCandidate && p.LastValidated > 0 && p.LastValidated < cutoff {
			pruned = append(pruned, p)
		} else {
			keep = append(keep, p)
		}
	}

	if !req.DryRun && len(pruned) > 0 {
		if err := agentpatterns.Save(dir, keep); err != nil {
			writeErr(w, http.StatusInternalServerError, "save patterns: "+err.Error())
			return
		}
	}

	pruneRows := make([]map[string]any, 0, len(pruned))
	for _, p := range pruned {
		pruneRows = append(pruneRows, patternToRow(p))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dry_run":             req.DryRun,
		"pruned_count":        len(pruned),
		"pruned":              pruneRows,
		"remaining_count":     len(keep),
		"candidate_decay_days": cfg.CandidateDecayDays,
	})
}

// ---------------------------------------------------------------------------
// POST /api/patterns/{group}/export
// ---------------------------------------------------------------------------

// handlePatternExport writes the approved pattern block to a CLAUDE.md file.
//
// Body:
//
//	{ "file": "/absolute/path/to/CLAUDE.md" }   — explicit target
//	{ "repo": "/absolute/path/to/repo" }         — writes to <repo>/CLAUDE.md
func (s *Server) handlePatternExport(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	var req struct {
		File string `json:"file"`
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	target := req.File
	if target == "" {
		if req.Repo == "" {
			writeErr(w, http.StatusBadRequest, "pass file or repo in request body")
			return
		}
		target = filepath.Join(req.Repo, "CLAUDE.md")
	}

	dir := groupPatternsDir(group)
	patterns, err := loadAgentPatterns(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load patterns: "+err.Error())
		return
	}

	if err := agentpatterns.UpsertFile(target, patterns, agentpatterns.ExportOptions{}); err != nil {
		writeErr(w, http.StatusInternalServerError, "export: "+err.Error())
		return
	}

	// Count approved patterns.
	approved := 0
	for _, p := range patterns {
		if !p.IsCandidate {
			approved++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"exported": approved,
		"target":   target,
	})
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// patternStatus derives a display status string from a Pattern.
func patternStatus(p agentpatterns.Pattern) string {
	if p.RejectTimestamp > 0 {
		return "rejected"
	}
	if p.IsCandidate {
		return "candidate"
	}
	return "active"
}

// patternStale returns true if the pattern has not been applied in >90 days.
func patternStale(p agentpatterns.Pattern) bool {
	if p.LastApplied == 0 {
		return false
	}
	return time.Now().Unix()-p.LastApplied > int64(90*24*3600)
}

// patternNeedsAttention returns true if the pattern requires human review.
func patternNeedsAttention(p agentpatterns.Pattern) bool {
	if p.RejectTimestamp > 0 {
		return true
	}
	if p.Confidence < 0.3 {
		return true
	}
	return patternStale(p)
}

// patternToRow converts a Pattern to a map suitable for list/detail JSON.
func patternToRow(p agentpatterns.Pattern) map[string]any {
	trigger := ""
	if p.Trigger.NaturalLanguage != "" {
		trigger = p.Trigger.NaturalLanguage
	}

	lastSeen := ""
	if p.LastApplied > 0 {
		lastSeen = time.Unix(p.LastApplied, 0).UTC().Format(time.RFC3339)
	} else if p.LastValidated > 0 {
		lastSeen = time.Unix(p.LastValidated, 0).UTC().Format(time.RFC3339)
	}

	return map[string]any{
		"id":               p.ID,
		"kind":             p.Kind,
		"category":         string(p.Category),
		"trigger":          trigger,
		"confidence":       p.Confidence,
		"observations":     p.Observations,
		"last_seen":        lastSeen,
		"status":           patternStatus(p),
		"is_candidate":     p.IsCandidate,
		"needs_attention":  patternNeedsAttention(p),
		"stale":            patternStale(p),
		"reject_reason":    p.RejectReason,
		"approval_note":    p.ApprovalNote,
		"steps":            p.Steps,
		"anti_patterns":    p.AntiPatterns,
		"exemplars":        p.Exemplars,
		"touches":          p.Touches,
		"scope":            p.Scope,
		"convergence_count": p.ConvergenceCount,
	}
}

// buildPatternStats computes the stats header counts.
func buildPatternStats(patterns []agentpatterns.Pattern) map[string]any {
	total := len(patterns)
	var pending, rejected, stale, needsAttention int
	for _, p := range patterns {
		if p.IsCandidate && p.RejectTimestamp == 0 {
			pending++
		}
		if p.RejectTimestamp > 0 {
			rejected++
		}
		if patternStale(p) {
			stale++
		}
		if patternNeedsAttention(p) {
			needsAttention++
		}
	}
	return map[string]any{
		"total":           total,
		"pending_review":  pending,
		"rejected":        rejected,
		"stale":           stale,
		"needs_attention": needsAttention,
	}
}

// filterNeedsAttention keeps only patterns that need human review.
func filterNeedsAttention(patterns []agentpatterns.Pattern) []agentpatterns.Pattern {
	out := make([]agentpatterns.Pattern, 0)
	for _, p := range patterns {
		if patternNeedsAttention(p) {
			out = append(out, p)
		}
	}
	return out
}

// filterByStatus keeps patterns matching the given status string.
func filterByStatus(patterns []agentpatterns.Pattern, status string) []agentpatterns.Pattern {
	out := make([]agentpatterns.Pattern, 0)
	for _, p := range patterns {
		if patternStatus(p) == status {
			out = append(out, p)
		}
	}
	return out
}

// filterConfidenceMin keeps patterns with confidence >= min.
func filterConfidenceMin(patterns []agentpatterns.Pattern, min float64) []agentpatterns.Pattern {
	out := make([]agentpatterns.Pattern, 0)
	for _, p := range patterns {
		if p.Confidence >= min {
			out = append(out, p)
		}
	}
	return out
}

// loadPatterns reads patterns.json from a directory as raw maps (kept for
// internal compat).
func loadPatterns(dir string) ([]map[string]any, error) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "patterns.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env struct {
		Patterns []map[string]any `json:"patterns"`
	}
	if json.Unmarshal(data, &env) == nil && env.Patterns != nil {
		return env.Patterns, nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// patternTitle extracts a title from a pattern map (uses trigger.natural_language).
func patternTitle(p map[string]any) string {
	if t, ok := p["title"].(string); ok && t != "" {
		return t
	}
	if trigger, ok := p["trigger"].(map[string]any); ok {
		if nl, ok := trigger["natural_language"].(string); ok {
			return nl
		}
	}
	if id, ok := p["id"].(string); ok {
		return id
	}
	return ""
}

// patternDescription extracts a description from a pattern map.
func patternDescription(p map[string]any) string {
	if d, ok := p["description"].(string); ok {
		return d
	}
	if steps, ok := p["steps"].([]interface{}); ok && len(steps) > 0 {
		if s, ok := steps[0].(string); ok {
			return s
		}
	}
	return ""
}
