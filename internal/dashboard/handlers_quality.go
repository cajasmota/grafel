package dashboard

// handlers_quality.go — Quality surface HTTP handlers.
//
// Ports the two `archigraph quality` CLI subcommands to a REST API so the
// web Quality page can surface "is my graph good?" without a terminal.
//
// Routes registered in server.go:
//
//	GET  /api/quality/orphans/{group}  — orphan audit for a group
//	GET  /api/quality/fixtures         — list golden fixture names
//	POST /api/quality/recall           — recall measurement against a fixture
//
// All three run in-process (no daemon socket hop needed): the dashboard
// server IS the daemon process, so calling audit.AuditPath directly is safe.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/quality"
	"github.com/cajasmota/archigraph/internal/quality/audit"
	"github.com/cajasmota/archigraph/internal/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// OrphanAuditReply is the wire shape for GET/POST /api/quality/orphans/{group}.
type OrphanAuditReply struct {
	Group string `json:"group"`
	// AuditedAt is the RFC3339 timestamp of the run. Empty string means the
	// audit has NEVER been run for this group — the client must render an
	// empty/never-run state rather than treating the zero-valued numbers below
	// as a real measurement.
	AuditedAt string `json:"audited_at"`
	// HasRun is true only when a real audit has been persisted for this group.
	// It is the authoritative "is this data real?" flag for the client.
	HasRun  bool              `json:"has_run"`
	Total   OrphanTotals      `json:"total"`
	PerRepo []RepoOrphanStats `json:"per_repo"`
	PerKind []KindStat        `json:"per_kind"`
	// HealthScore is the composite graph-health score (0–100, higher is better)
	// from quality.CompositeScoreFromPcts using the REAL orphan + bug rates.
	// It is distinct from Fidelity: Health is a composite (orphans + bug-rate +
	// recall), Fidelity is extraction correctness (100 − bug_rate).
	HealthScore int `json:"health_score"`
	// Fidelity is 100 − bug_rate (the owner-defined primary quality number),
	// expressed as a 0–1 ratio to match the Landing card. Null when unknown.
	Fidelity *float64 `json:"fidelity"`
	// BugRatePct is the unresolved-import rate (0–100); fidelity = 100 − this.
	BugRatePct float64 `json:"bug_rate_pct"`
	// Recommendations is the punch list from the audit engine.
	Recommendations []RecommendationItem `json:"recommendations"`
}

// OrphanTotals rolls up aggregate counts across the whole group.
type OrphanTotals struct {
	Entities   int     `json:"entities"`
	Orphans    int     `json:"orphans"`
	OrphanRate float64 `json:"orphan_rate"`
}

// RepoOrphanStats is a per-repo summary row for the result table.
type RepoOrphanStats struct {
	Slug       string  `json:"slug"`
	Path       string  `json:"path"`
	Entities   int     `json:"entities"`
	Orphans    int     `json:"orphans"`
	OrphanRate float64 `json:"orphan_rate"`
	RiskScore  int     `json:"risk_score"`
}

// KindStat is one row in the per-kind breakdown (e.g. Function 12.3%).
type KindStat struct {
	Kind string `json:"kind"`
	// Entities is the TOTAL number of entities of this kind (not the orphan
	// count). Orphans holds the orphaned subset.
	Entities int `json:"entities"`
	// Orphans is the number of entities of this kind with no inbound edge.
	Orphans int `json:"orphans"`
	// Count is retained for backward compatibility; it mirrors Entities so old
	// clients keep working. New clients should read Entities/Orphans.
	Count      int     `json:"count"`
	OrphanRate float64 `json:"orphan_rate"`
}

// RecommendationItem mirrors audit.Recommendation for the wire format.
type RecommendationItem struct {
	Priority                    int    `json:"priority"`
	Issue                       string `json:"issue"`
	AffectedRepos               int    `json:"affected_repos"`
	RecoverableEntitiesEstimate int    `json:"recoverable_entities_estimate"`
}

// FixturesReply is the wire shape for GET /api/quality/fixtures.
type FixturesReply struct {
	Fixtures []string `json:"fixtures"`
}

// RecallRequest is the body for POST /api/quality/recall.
type RecallRequest struct {
	Fixture string `json:"fixture"`
	Group   string `json:"group,omitempty"`
}

// RecallReply is the wire shape for POST /api/quality/recall.
type RecallReply struct {
	Fixture              string              `json:"fixture"`
	EntityRecall         float64             `json:"entity_recall"`
	RelationshipRecall   float64             `json:"relationship_recall"`
	EntityExpected       int                 `json:"entity_expected"`
	EntityFound          int                 `json:"entity_found"`
	RelationshipExpected int                 `json:"relationship_expected"`
	RelationshipFound    int                 `json:"relationship_found"`
	ForbiddenHits        int                 `json:"forbidden_hits"`
	MissingEntities      []RecallMissingItem `json:"missing_entities,omitempty"`
	MissingRelationships []RecallRelItem     `json:"missing_relationships,omitempty"`
}

// RecallMissingItem is a missing entity in a recall report.
type RecallMissingItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"source_file,omitempty"`
}

// RecallRelItem is a missing or forbidden relationship in a recall report.
type RecallRelItem struct {
	From         string `json:"from"`
	FromKind     string `json:"from_kind,omitempty"`
	Kind         string `json:"kind"`
	To           string `json:"to"`
	ToKind       string `json:"to_kind,omitempty"`
	FromResolved bool   `json:"from_resolved"`
	ToResolved   bool   `json:"to_resolved"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/quality/orphans/{group}
// ─────────────────────────────────────────────────────────────────────────────

// handleQualityOrphans (GET) returns the LAST PERSISTED orphan audit for the
// group. It does NOT run the (expensive) audit — running is an explicit user
// action via POST. When no audit has ever been persisted, it returns a
// never-run reply (HasRun=false, AuditedAt="") so the client can render an
// honest empty state instead of treating zero-valued defaults as a real
// measurement (#1574).
func (s *Server) handleQualityOrphans(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}
	// Confirm the group exists so a typo 404s rather than silently returning a
	// never-run state.
	if _, err := repoPathsForGroup(groupName); err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}

	if cached, ok := loadOrphanAudit(s.daemonRoot(), groupName); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}
	// Never run: honest empty state, no fake numbers.
	writeJSON(w, http.StatusOK, OrphanAuditReply{
		Group:    groupName,
		HasRun:   false,
		PerRepo:  []RepoOrphanStats{},
		PerKind:  []KindStat{},
		Fidelity: nil,
	})
}

// handleRunQualityOrphans (POST) runs the orphan audit against every repo in
// the requested group, persists the result, and returns it.
func (s *Server) handleRunQualityOrphans(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	// Resolve the group's repo paths from the registry.
	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	// We audit each repo separately by calling audit.AuditPath with corpus=false,
	// then merge the results into a group-level summary.
	var allRepos []*audit.RepoReport
	for _, rp := range repoPaths {
		rep, aErr := audit.AuditPath(rp.Path, false)
		if aErr != nil {
			// Non-fatal: surface the error as an empty stub repo.
			allRepos = append(allRepos, &audit.RepoReport{
				Path:   rp.Path,
				Errors: []string{aErr.Error()},
			})
			continue
		}
		if len(rep.Repos) > 0 {
			allRepos = append(allRepos, rep.Repos[0])
		}
	}

	reply := buildOrphanAuditReply(groupName, allRepos)
	reply.HasRun = true
	reply.AuditedAt = time.Now().UTC().Format(time.RFC3339)

	// Persist so the next GET returns the real numbers (and so a fresh page load
	// shows the last run rather than re-running the heavy audit on mount).
	if err := saveOrphanAudit(s.daemonRoot(), groupName, reply); err != nil {
		// Persistence failure is non-fatal: still return the live result.
		_ = err
	}

	writeJSON(w, http.StatusOK, reply)
}

// buildOrphanAuditReply converts raw audit.RepoReport slice into the wire reply.
func buildOrphanAuditReply(group string, repos []*audit.RepoReport) OrphanAuditReply {
	reply := OrphanAuditReply{
		Group:   group,
		PerRepo: []RepoOrphanStats{},
		PerKind: []KindStat{},
	}

	// Aggregate totals and build per-repo rows.
	kindEntities := map[string]int{}
	kindOrphans := map[string]int{}
	totalImports := 0
	goodImports := 0

	for _, rr := range repos {
		if rr == nil {
			continue
		}
		reply.Total.Entities += rr.Entities
		reply.Total.Orphans += rr.Orphans

		totalImports += rr.ImportsTotal
		goodImports += rr.ImportsToIDFormat[audit.ImportFormatHex] +
			rr.ImportsToIDFormat[audit.ImportFormatExtQualified]

		slug := filepath.Base(rr.Path)
		rate := 0.0
		if rr.Entities > 0 {
			rate = float64(rr.Orphans) / float64(rr.Entities)
		}
		reply.PerRepo = append(reply.PerRepo, RepoOrphanStats{
			Slug:       slug,
			Path:       rr.Path,
			Entities:   rr.Entities,
			Orphans:    rr.Orphans,
			OrphanRate: rate,
			RiskScore:  rr.RiskScore,
		})

		// Accumulate kind histograms. TopKinds = total entities of each kind;
		// TopOrphanKinds = orphaned subset of each kind.
		for _, kv := range rr.TopKinds {
			kindEntities[kv.Key] += kv.Count
		}
		for _, kv := range rr.TopOrphanKinds {
			kindOrphans[kv.Key] += kv.Count
		}
	}

	// Compute total orphan rate.
	if reply.Total.Entities > 0 {
		reply.Total.OrphanRate = float64(reply.Total.Orphans) / float64(reply.Total.Entities)
	}

	// Per-kind breakdown: real TOTAL entities + orphan subset + orphan rate.
	for kind, total := range kindEntities {
		orphaned := kindOrphans[kind]
		rate := 0.0
		if total > 0 {
			rate = float64(orphaned) / float64(total)
		}
		reply.PerKind = append(reply.PerKind, KindStat{
			Kind:       kind,
			Entities:   total,
			Orphans:    orphaned,
			Count:      total, // back-compat mirror
			OrphanRate: rate,
		})
	}
	sort.Slice(reply.PerKind, func(i, j int) bool {
		// Order by orphan rate first, then by entity count, then name — so the
		// most-orphaned kinds bubble up but populated kinds never read as 0.
		if reply.PerKind[i].OrphanRate != reply.PerKind[j].OrphanRate {
			return reply.PerKind[i].OrphanRate > reply.PerKind[j].OrphanRate
		}
		if reply.PerKind[i].Entities != reply.PerKind[j].Entities {
			return reply.PerKind[i].Entities > reply.PerKind[j].Entities
		}
		return reply.PerKind[i].Kind < reply.PerKind[j].Kind
	})

	// Health + fidelity from the REAL measured rates (not an avg risk score).
	orphanPct := reply.Total.OrphanRate * 100
	bugPct := 0.0
	if totalImports > 0 {
		bugPct = 100.0 * float64(totalImports-goodImports) / float64(totalImports)
	}
	reply.BugRatePct = bugPct
	cr := quality.CompositeScoreFromPcts(orphanPct, bugPct, 0)
	reply.HealthScore = int(cr.Score + 0.5)
	fid := fidelityFromBugRate(bugPct)
	reply.Fidelity = &fid

	return reply
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/quality/fixtures
// ─────────────────────────────────────────────────────────────────────────────

// handleQualityFixtures lists the available golden fixtures bundled with the
// binary. We locate them relative to the binary's source tree (development)
// or via the ARCHIGRAPH_FIXTURES_DIR env override.
func (s *Server) handleQualityFixtures(w http.ResponseWriter, _ *http.Request) {
	dir, err := goldenFixturesDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("locate fixtures: %v", err))
		return
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("read fixtures dir: %v", err))
		return
	}

	var names []string
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Only list directories that contain expected.json.
		if _, err2 := os.Stat(filepath.Join(dir, e.Name(), "expected.json")); err2 == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	writeJSON(w, http.StatusOK, FixturesReply{Fixtures: names})
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /api/quality/recall
// ─────────────────────────────────────────────────────────────────────────────

// handleQualityRecall is a lightweight wrapper: it looks up the fixture path,
// then calls the existing quality.Evaluate + Index pipeline via the daemon's
// QualityAuditRecall hook, returning a structured RecallReply.
//
// NOTE: running the full indexer inside an HTTP handler takes several seconds
// for larger fixtures. The client should show a loading state.
func (s *Server) handleQualityRecall(w http.ResponseWriter, r *http.Request) {
	var req RecallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Fixture == "" {
		writeErr(w, http.StatusBadRequest, "fixture required")
		return
	}
	// Sanitize: no path traversal.
	if strings.Contains(req.Fixture, "..") || strings.ContainsAny(req.Fixture, "/\\") {
		writeErr(w, http.StatusBadRequest, "invalid fixture name")
		return
	}

	if s.recallRunner == nil {
		writeErr(w, http.StatusServiceUnavailable, "recall runner not wired (daemon required)")
		return
	}

	// recallRunner returns a JSON-encoded quality.JSONReport.
	raw, err := s.recallRunner(req.Fixture)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("recall: %v", err))
		return
	}

	// Unmarshal into a local shape that mirrors quality.JSONReport fields.
	var jr struct {
		Fixture              string  `json:"fixture"`
		EntityExpected       int     `json:"entity_expected"`
		EntityFound          int     `json:"entity_found"`
		EntityRecall         float64 `json:"entity_recall"`
		RelationshipExpected int     `json:"relationship_expected"`
		RelationshipFound    int     `json:"relationship_found"`
		RelationshipRecall   float64 `json:"relationship_recall"`
		ForbiddenHits        int     `json:"forbidden_hits"`
		MissingEntities      []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			File string `json:"source_file,omitempty"`
		} `json:"missing_entities,omitempty"`
		MissingRelationships []struct {
			From         string `json:"from"`
			FromKind     string `json:"from_kind,omitempty"`
			Kind         string `json:"kind"`
			To           string `json:"to"`
			ToKind       string `json:"to_kind,omitempty"`
			FromResolved bool   `json:"from_resolved"`
			ToResolved   bool   `json:"to_resolved"`
		} `json:"missing_relationships,omitempty"`
	}
	if err2 := json.Unmarshal(raw, &jr); err2 != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("decode recall report: %v", err2))
		return
	}

	reply := RecallReply{
		Fixture:              jr.Fixture,
		EntityRecall:         jr.EntityRecall,
		RelationshipRecall:   jr.RelationshipRecall,
		EntityExpected:       jr.EntityExpected,
		EntityFound:          jr.EntityFound,
		RelationshipExpected: jr.RelationshipExpected,
		RelationshipFound:    jr.RelationshipFound,
		ForbiddenHits:        jr.ForbiddenHits,
	}
	for _, me := range jr.MissingEntities {
		reply.MissingEntities = append(reply.MissingEntities, RecallMissingItem{
			Name: me.Name,
			Kind: me.Kind,
			File: me.File,
		})
	}
	for _, mr := range jr.MissingRelationships {
		reply.MissingRelationships = append(reply.MissingRelationships, RecallRelItem{
			From:         mr.From,
			FromKind:     mr.FromKind,
			Kind:         mr.Kind,
			To:           mr.To,
			ToKind:       mr.ToKind,
			FromResolved: mr.FromResolved,
			ToResolved:   mr.ToResolved,
		})
	}

	writeJSON(w, http.StatusOK, reply)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// repoRef is a (slug, path) pair.
type repoRef struct {
	Slug string
	Path string
}

// repoPathsForGroup resolves every repo path for the named group by loading
// the per-group config file from the registry.
func repoPathsForGroup(groupName string) ([]repoRef, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name != groupName {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			return nil, err
		}
		out := make([]repoRef, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			out = append(out, repoRef{Slug: r.Slug, Path: r.Path})
		}
		return out, nil
	}
	return nil, fmt.Errorf("group %q not found in registry", groupName)
}

// GoldenFixturesDir returns the absolute path to the bundled golden fixture
// directory. Resolution order:
//  1. ARCHIGRAPH_FIXTURES_DIR env override (useful in tests)
//  2. Source-relative path from the current file (works in `go run` + tests)
//  3. Sibling of the binary at install time
//
// Exported so cmd/archigraph can call it when building the recall runner
// without duplicating the resolution logic.
func GoldenFixturesDir() (string, error) {
	return goldenFixturesDir()
}

func goldenFixturesDir() (string, error) {
	if override := os.Getenv("ARCHIGRAPH_FIXTURES_DIR"); override != "" {
		return override, nil
	}
	// Source-relative: works when running from the repo.
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		// handlers_quality.go lives at internal/dashboard/; golden at
		// internal/quality/golden/
		candidate := filepath.Join(filepath.Dir(thisFile), "..", "quality", "golden")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate, nil
		}
	}
	// Fallback: next to the binary.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate binary: %w", err)
	}
	candidate := filepath.Join(filepath.Dir(exe), "golden")
	if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("could not locate golden fixtures directory (set ARCHIGRAPH_FIXTURES_DIR)")
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /api/quality/composite/{group}
// ─────────────────────────────────────────────────────────────────────────────

// CompositeScoreReply is the wire shape for GET /api/quality/composite/{group}.
type CompositeScoreReply struct {
	Group string `json:"group"`
	// Score is the composite health score (0–100, higher is better).
	Score float64 `json:"score"`
	// Grade is the letter grade (A–F) derived from Score.
	Grade string `json:"grade"`
	// OrphanRatePct is the fraction of entities with no inbound edges * 100.
	OrphanRatePct float64 `json:"orphan_rate_pct"`
	// BugRatePct is the fraction of unresolved import edges * 100.
	BugRatePct float64 `json:"bug_rate_pct"`
	// RecallMissPct is always 0 for live-graph measurements (no golden fixture).
	RecallMissPct float64 `json:"recall_miss_pct"`
	// Entities is the total entity count across all repos in the group.
	Entities int `json:"entities"`
	// Repos is the number of repos measured.
	Repos int `json:"repos"`
}

// handleQualityComposite computes the composite graph-health score for the
// requested group and returns it as JSON. The handler runs the orphan audit
// in-process (same as handleQualityOrphans) and then applies the composite
// formula from internal/quality.CompositeScoreFromPcts.
func (s *Server) handleQualityComposite(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	repoPaths, err := repoPathsForGroup(groupName)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q: %v", groupName, err))
		return
	}
	if len(repoPaths) == 0 {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("group %q has no repos", groupName))
		return
	}

	// Audit each repo and accumulate totals.
	totalEntities := 0
	totalOrphans := 0
	totalImports := 0
	goodImports := 0
	repos := 0

	for _, rp := range repoPaths {
		rep, aErr := audit.AuditPath(rp.Path, false)
		if aErr != nil || len(rep.Repos) == 0 {
			continue
		}
		rr := rep.Repos[0]
		repos++
		totalEntities += rr.Entities
		totalOrphans += rr.Orphans
		totalImports += rr.ImportsTotal
		goodImports += rr.ImportsToIDFormat[audit.ImportFormatHex] +
			rr.ImportsToIDFormat[audit.ImportFormatExtQualified]
	}

	orphanPct := 0.0
	if totalEntities > 0 {
		orphanPct = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}
	bugPct := 0.0
	if totalImports > 0 {
		bugPct = 100.0 * float64(totalImports-goodImports) / float64(totalImports)
	}

	cr := quality.CompositeScoreFromPcts(orphanPct, bugPct, 0)
	writeJSON(w, http.StatusOK, CompositeScoreReply{
		Group:         groupName,
		Score:         cr.Score,
		Grade:         cr.Grade,
		OrphanRatePct: cr.OrphanRatePct,
		BugRatePct:    cr.BugRatePct,
		RecallMissPct: cr.RecallMissPct,
		Entities:      totalEntities,
		Repos:         repos,
	})
}
